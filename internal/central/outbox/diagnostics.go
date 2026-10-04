package outbox

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func safeDelivery(d record) (oc.SafeDelivery, error) {
	out := oc.SafeDelivery{ID: d.id, EventID: d.eventID, EventType: d.typ, SchemaVersion: d.schema, HandlerID: d.handler, Scope: d.scope, Phase: d.phase, Version: foundation.Version(d.version), Cycle: foundation.Progress(d.cycle), Attempts: foundation.Progress(d.lifetime), Reason: d.reason}
	if d.phase == oc.Pending || d.phase == oc.RetryWait {
		at, err := foundation.NewInstant(d.due)
		if err != nil {
			return oc.SafeDelivery{}, unavailable(err)
		}
		out.NextAttemptAt = &at
	}
	return out, nil
}
func (s *Service) InspectDelivery(ctx context.Context, actor identity.Actor, scope identity.Scope, id oc.DeliveryID) (oc.SafeDelivery, error) {
	var out oc.SafeDelivery
	if id.Validate() != nil {
		return out, invalid()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	plan, err := s.planHuman(ctx, actor, scope, oc.InspectProject, record{}, "")
	if err != nil {
		return out, err
	}
	locks := append(plan.locks, foundation.LockRequest{Key: deliveryLock(id), Mode: foundation.Shared})
	sc, _ := eventScope(scope)
	result := s.state().store.WithinTx(ctx, recoveryCause("outbox.inspect"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return unavailable(err)
		}
		if err := s.authorizeHuman(ctx, tx, plan, identity.Read); err != nil {
			return err
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		d, err := s.deliveryRecord(ctx, x, id)
		if err != nil {
			return err
		}
		if d.scope != sc {
			return failure(foundation.NotFound, nil)
		}
		out, err = safeDelivery(d)
		return err
	})
	if err = commitError(result); err != nil {
		return oc.SafeDelivery{}, err
	}
	return out, nil
}

type diagnosticsPosition struct {
	watermark, timeAfter time.Time
	sequence             int64
	id                   string
}

func diagnosticsBinding(actor identity.Actor, scope identity.Scope, filter oc.DiagnosticsFilter) (cursor.Binding, error) {
	raw, err := json.Marshal(struct {
		Kind, User string
		Scope      identity.ScopeDetails
		Filter     oc.DiagnosticsFilter
	}{"outbox.diagnostics", actor.Details().UserID, scope.Details(), filter})
	if err != nil {
		return cursor.Binding{}, invalid()
	}
	return cursor.Binding{Scope: scope, QueryDigest: oc.DigestBytes(raw), Order: "created_at:desc,id:desc"}, nil
}
func (s *Service) QueryDiagnostics(ctx context.Context, actor identity.Actor, scope identity.Scope, filter oc.DiagnosticsFilter, page foundation.PageRequest) (oc.DiagnosticsPage, error) {
	empty := oc.DiagnosticsPage{}
	if page.Validate() != nil || page.Limit > 100 {
		return empty, invalid()
	}
	filter, err := filter.Normalize()
	if err != nil {
		return empty, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	plan, err := s.planHuman(ctx, actor, scope, oc.InspectProject, record{}, "")
	if err != nil {
		return empty, err
	}
	keys := s.state().auth.Cursors
	if keys.Validate() != nil {
		return empty, failure(foundation.DependencyUnbound, nil)
	}
	binding, err := diagnosticsBinding(actor, scope, filter)
	if err != nil {
		return empty, err
	}
	var position diagnosticsPosition
	if page.Cursor != "" {
		p, err := keys.Verify(page.Cursor, binding)
		if err != nil {
			return empty, err
		}
		if len(p.Scalars) != 4 || p.OrderGeneration != nil || p.Scalars[0].Kind() != "instant" || p.Scalars[1].Kind() != "integer" || p.Scalars[2].Kind() != "instant" || p.Scalars[3].Kind() != "uuid" {
			return empty, failure(foundation.CursorInvalid, nil)
		}
		first, e1 := foundation.ParseInstant(p.Scalars[0].Value())
		after, e2 := foundation.ParseInstant(p.Scalars[2].Value())
		n, e3 := strconv.ParseInt(p.Scalars[1].Value(), 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || n < 0 || after.Time().After(first.Time()) {
			return empty, failure(foundation.CursorInvalid, nil)
		}
		position = diagnosticsPosition{watermark: first.Time(), timeAfter: after.Time(), sequence: n, id: p.Scalars[3].Value()}
	}
	var out oc.DiagnosticsPage
	out.Items = []oc.SafeDelivery{}
	sc, _ := eventScope(scope)
	// System has no Project; a zero typed ID formats as a zero UUID, not SQL NULL.
	project := scope.Details().ProjectID
	result := s.state().store.WithinTx(ctx, recoveryCause("outbox.diagnostics"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, plan.locks); err != nil {
			return unavailable(err)
		}
		if err := s.authorizeHuman(ctx, tx, plan, identity.Read); err != nil {
			return err
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		out.Summary, err = readDiagnosticsSummary(ctx, x, string(sc.Kind), project, filter)
		if err != nil {
			return err
		}
		if page.Cursor == "" {
			position.watermark = out.Summary.AsOf.Time()
			if err = x.QueryRow(ctx, `SELECT coalesce(max(sequence),0) FROM agenteam_outbox.events`).Scan(&position.sequence); err != nil {
				return unavailable(err)
			}
		}
		var after any
		if !position.timeAfter.IsZero() {
			after = position.timeAfter
		}
		rows, err := x.Query(ctx, `SELECT `+recordColumns+recordFrom+` WHERE d.scope=$1 AND d.project_id IS NOT DISTINCT FROM $2::uuid AND ($3='' OR d.handler_id=$3) AND ($4='' OR e.event_type=$4) AND ($5='' OR d.phase=$5) AND e.sequence<=$6 AND d.created_at<=$7 AND ($8::timestamptz IS NULL OR (d.created_at,d.id)<($8,$9::uuid)) ORDER BY d.created_at DESC,d.id DESC LIMIT $10`, string(sc.Kind), nullableUUID(project), string(filter.HandlerID), string(filter.EventType), string(filter.Phase), position.sequence, position.watermark, after, nullableUUID(position.id), page.Limit+1)
		if err != nil {
			return unavailable(err)
		}
		var records []record
		for rows.Next() {
			d, err := scanRecord(rows)
			if err != nil {
				rows.Close()
				return err
			}
			records = append(records, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return unavailable(err)
		}
		more := len(records) > page.Limit
		if more {
			records = records[:page.Limit]
		}
		for _, d := range records {
			item, err := safeDelivery(d)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, item)
		}
		if more {
			last := records[len(records)-1]
			wm, _ := foundation.NewInstant(position.watermark)
			at, _ := foundation.NewInstant(last.created)
			a, _ := cursor.Instant(wm)
			b, _ := cursor.Instant(at)
			c, _ := cursor.UUID(last.id.String())
			out.NextCursor, err = keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{a, cursor.Integer(position.sequence), b, c}})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return empty, err
	}
	return out, nil
}

// Every component is selected by one statement and therefore one MVCC
// snapshot. Numeric aggregates stay PostgreSQL numeric/text until validated by
// the exact signed-int64 public scalar decoder; no float or page-size totals.
const diagnosticsSummarySQL = `WITH clock AS MATERIALIZED (
 SELECT statement_timestamp() AS at
), bounds AS MATERIALIZED (
 SELECT at,at-$5::bigint*interval '1 millisecond' AS start FROM clock
), ev AS MATERIALIZED (
 SELECT e.id,e.event_type,e.created_at FROM agenteam_outbox.events e
 WHERE e.scope=$1 AND e.project_id IS NOT DISTINCT FROM $2::uuid AND ($4='' OR e.event_type=$4)
 AND ($3='' OR EXISTS(SELECT 1 FROM agenteam_outbox.deliveries z WHERE z.event_id=e.id AND z.handler_id=$3))
), del AS MATERIALIZED (
 SELECT d.id,d.event_id,d.handler_id,d.phase,d.safe_reason,d.last_at,d.current_attempt_id,e.event_type,e.created_at
 FROM agenteam_outbox.deliveries d JOIN ev e ON e.id=d.event_id WHERE ($3='' OR d.handler_id=$3)
), att AS MATERIALIZED (
 SELECT a.*,d.handler_id FROM agenteam_outbox.attempts a JOIN del d ON d.id=a.delivery_id CROSS JOIN bounds b
 WHERE a.started_at>=b.start AND a.started_at<b.at
), lat AS MATERIALIZED (
 SELECT handler_id,count(*) FILTER(WHERE handler_returned_at IS NOT NULL)::text AS count,
 count(*) FILTER(WHERE handler_returned_at IS NULL)::text AS unknown,
 coalesce(sum(floor(extract(epoch FROM (handler_returned_at-started_at))*1000)),0)::text AS sum_ms,
 max(floor(extract(epoch FROM (handler_returned_at-started_at))*1000))::text AS max_ms
 FROM att GROUP BY handler_id
), rates AS MATERIALIZED (
 SELECT e.event_type,count(DISTINCT e.id) FILTER(WHERE e.created_at>=b.start AND e.created_at<b.at)::text AS produced_events,
 count(DISTINCT p.delivery_id) FILTER(WHERE p.processed_at>=b.start AND p.processed_at<b.at)::text AS succeeded_deliveries
 FROM ev e CROSS JOIN bounds b LEFT JOIN del d ON d.event_id=e.id LEFT JOIN agenteam_outbox.processed p ON p.delivery_id=d.id
 GROUP BY e.event_type
), pending AS MATERIALIZED (
 SELECT count(DISTINCT event_id)::text AS n,min(created_at) AS oldest FROM del WHERE phase IN ('pending','processing','retry_wait')
)
SELECT b.at,b.start,p.n,
 (SELECT count(*)::text FROM del WHERE phase='retry_wait'),
 (SELECT count(*)::text FROM del WHERE phase='failed'),
 (SELECT count(*)::text FROM del WHERE phase='dead_letter'),
 floor(extract(epoch FROM (b.at-p.oldest))*1000)::text,
 coalesce((SELECT jsonb_agg(to_jsonb(l) ORDER BY l.handler_id) FROM (SELECT * FROM lat ORDER BY handler_id LIMIT 128) l),'[]'::jsonb),
 coalesce((SELECT jsonb_agg(to_jsonb(t) ORDER BY t.event_type) FROM (SELECT * FROM rates ORDER BY event_type LIMIT 128) t),'[]'::jsonb),
 (SELECT count(*)>128 FROM lat),(SELECT count(*)>128 FROM rates),
 coalesce((SELECT jsonb_agg(jsonb_build_object('at',to_char(q.last_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'delivery_id',q.id::text,'attempt_id',q.current_attempt_id::text,'handler_id',q.handler_id,'event_type',q.event_type,'reason',q.safe_reason) ORDER BY q.last_at DESC,q.id DESC)
 FROM (SELECT d.* FROM del d WHERE d.safe_reason IS NOT NULL AND d.last_at>=b.start AND d.last_at<b.at ORDER BY d.last_at DESC,d.id DESC LIMIT 20) q),'[]'::jsonb),
 EXISTS(SELECT 1 FROM att WHERE handler_returned_at<started_at OR handler_returned_at>b.at)
 FROM bounds b CROSS JOIN pending p`

func readDiagnosticsSummary(ctx context.Context, x postgres.SQLExecutor, kind, project string, filter oc.DiagnosticsFilter) (oc.DiagnosticsSummary, error) {
	var s oc.DiagnosticsSummary
	var at, start time.Time
	var pending, retry, failed, dead string
	var age *string
	var lat, rates, recent []byte
	var bad bool
	duration, _ := filter.Window.Duration()
	s.Window = filter.Window
	s.WindowMS = foundation.DurationMS(duration.Milliseconds())
	err := x.QueryRow(ctx, diagnosticsSummarySQL, kind, nullableUUID(project), string(filter.HandlerID), string(filter.EventType), duration.Milliseconds()).Scan(&at, &start, &pending, &retry, &failed, &dead, &age, &lat, &rates, &s.HandlerLatencyTruncated, &s.EventThroughputTruncated, &recent, &bad)
	if err != nil {
		return s, unavailable(err)
	}
	if bad {
		return s, unavailable(nil)
	}
	s.AsOf, err = foundation.NewInstant(at)
	if err != nil {
		return s, unavailable(err)
	}
	s.WindowStart, err = foundation.NewInstant(start)
	if err != nil {
		return s, unavailable(err)
	}
	targets := []*foundation.Progress{&s.PendingEvents, &s.RetryWaitDeliveries, &s.FailedDeliveries, &s.DeadLetterDeliveries}
	for i, value := range []string{pending, retry, failed, dead} {
		n, e := strconv.ParseInt(value, 10, 64)
		if e != nil || n < 0 {
			return s, unavailable(e)
		}
		*targets[i] = foundation.Progress(n)
	}
	if age != nil {
		n, e := strconv.ParseInt(*age, 10, 64)
		if e != nil || n < 0 {
			return s, unavailable(e)
		}
		v := foundation.DurationMS(n)
		s.OldestPendingAgeMS = &v
	}
	if err = json.Unmarshal(lat, &s.HandlerLatency); err != nil {
		return s, unavailable(err)
	}
	if err = json.Unmarshal(rates, &s.EventThroughput); err != nil {
		return s, unavailable(err)
	}
	if err = json.Unmarshal(recent, &s.RecentErrors); err != nil {
		return s, unavailable(err)
	}
	if err = s.Validate(); err != nil {
		return s, unavailable(err)
	}
	return s, nil
}
