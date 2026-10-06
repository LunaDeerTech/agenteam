package usage

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

type queryPosition struct {
	upperAt, lastAt, from, to f.Instant
	upperID, lastID           string
	groupNull                 bool
	group                     string
}

// A committed read still needs a live caller before publishing its candidate.
// Cancellation never rewrites a known commit (including a rebuilt projection)
// into a rollback, nor obscures an Unknown transaction's original identity.
func completedRead(ctx context.Context, result f.CommitResult) error {
	if err := commitError(result); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return f.NewFault(f.DependencyUnavailable, f.Committed).WithCause(err)
	}
	return nil
}

func cursorBinding(a id.Actor, filter uc.Filter, group uc.GroupBy) cursor.Binding {
	order := "started_at:desc,id:desc"
	kind := "list"
	if group != "" {
		kind = "aggregate"
		order = "group:nulls_first,asc"
	}
	b, _ := json.Marshal(struct {
		Format int
		User   string
		Kind   string
		Filter uc.Filter
		Group  uc.GroupBy
		Order  string
	}{1, a.Details().UserID, kind, filter, group, order})
	scope, _ := id.InProject(filter.ProjectID)
	return cursor.Binding{Scope: scope, QueryDigest: hash(b), Order: order}
}
func (s *Service) position(token string, binding cursor.Binding, filter uc.Filter, group bool, now f.Instant) (queryPosition, error) {
	p := queryPosition{}
	if token == "" {
		if filter.From != nil {
			p.from, p.to = *filter.From, *filter.To
		} else {
			p.to = now
			p.from, _ = f.NewInstant(now.Time().Add(-30 * 24 * time.Hour))
		}
		return p, nil
	}
	v, e := s.state().deps.Cursors.Verify(token, binding)
	if e != nil {
		return p, e
	}
	if len(v.Scalars) != 6 || v.OrderGeneration != nil {
		return p, fault(f.CursorInvalid)
	}
	parseTime := func(i int) (f.Instant, error) {
		if v.Scalars[i].Kind() != "instant" {
			return f.Instant{}, fault(f.CursorInvalid)
		}
		x, e := f.ParseInstant(v.Scalars[i].Value())
		if e != nil {
			return f.Instant{}, fault(f.CursorInvalid)
		}
		return x, nil
	}
	p.upperAt, e = parseTime(0)
	if e != nil {
		return p, e
	}
	if v.Scalars[1].Kind() != "uuid" {
		return p, fault(f.CursorInvalid)
	}
	p.upperID = v.Scalars[1].Value()
	if group {
		p.from, e = parseTime(2)
		if e != nil {
			return p, e
		}
		p.to, e = parseTime(3)
		if e != nil {
			return p, e
		}
		if v.Scalars[4].Kind() != "integer" || v.Scalars[4].Value() != "0" && v.Scalars[4].Value() != "1" || v.Scalars[5].Kind() != "text" {
			return p, fault(f.CursorInvalid)
		}
		p.groupNull = v.Scalars[4].Value() == "1"
		p.group = v.Scalars[5].Value()
		if p.groupNull && p.group != "" {
			return p, fault(f.CursorInvalid)
		}
	} else {
		p.lastAt, e = parseTime(2)
		if e != nil {
			return p, e
		}
		if v.Scalars[3].Kind() != "uuid" {
			return p, fault(f.CursorInvalid)
		}
		p.lastID = v.Scalars[3].Value()
		p.from, e = parseTime(4)
		if e != nil {
			return p, e
		}
		p.to, e = parseTime(5)
		if e != nil {
			return p, e
		}
		if p.lastAt.Time().After(p.upperAt.Time()) || p.lastAt == p.upperAt && p.lastID > p.upperID {
			return p, fault(f.CursorInvalid)
		}
	}
	if !p.from.Time().Before(p.to.Time()) || filter.From != nil && (p.from != *filter.From || p.to != *filter.To) || p.upperAt.Time().Before(p.from.Time()) || !p.upperAt.Time().Before(p.to.Time()) {
		return p, fault(f.CursorInvalid)
	}
	if !group && (p.lastAt.Time().Before(p.from.Time()) || !p.lastAt.Time().Before(p.to.Time())) {
		return p, fault(f.CursorInvalid)
	}
	return p, nil
}
func (s *Service) signPosition(p queryPosition, b cursor.Binding, group bool) (string, error) {
	at, _ := cursor.Instant(p.upperAt)
	key, _ := cursor.UUID(p.upperID)
	from, _ := cursor.Instant(p.from)
	to, _ := cursor.Instant(p.to)
	scalars := []cursor.Scalar{at, key}
	if group {
		flag := int64(0)
		if p.groupNull {
			flag = 1
		}
		value, _ := cursor.Text(p.group)
		scalars = append(scalars, from, to, cursor.Integer(flag), value)
	} else {
		last, _ := cursor.Instant(p.lastAt)
		lastID, _ := cursor.UUID(p.lastID)
		scalars = append(scalars, last, lastID, from, to)
	}
	return s.state().deps.Cursors.Sign(b, cursor.Position{Scalars: scalars})
}
func filterSQL(q uc.Filter, p queryPosition) (string, []any) {
	args := []any{q.ProjectID.String(), p.from.Time(), p.to.Time()}
	parts := []string{`project_id=$1`, `started_at>=$2`, `started_at<$3`}
	add := func(column string, value any) {
		args = append(args, value)
		parts = append(parts, column+"=$"+strconv.Itoa(len(args)))
	}
	if q.ConsumerKind != nil {
		add("consumer_kind", string(*q.ConsumerKind))
	}
	if q.AgentID != nil {
		add("agent_id", q.AgentID.String())
	}
	if q.ExecutionID != nil {
		add("execution_id", q.ExecutionID.String())
	}
	if q.MeetingID != "" {
		add("meeting_id", q.MeetingID)
	}
	if q.Purpose != nil {
		add("purpose", string(*q.Purpose))
	}
	if q.ProviderID != nil {
		add("historical_provider_id", q.ProviderID.String())
	}
	if q.ModelID != nil {
		add("historical_model_id", q.ModelID.String())
	}
	if q.Status != nil {
		add("final_status", string(*q.Status))
	}
	if p.upperID != "" {
		args = append(args, p.upperAt.Time(), p.upperID)
		parts = append(parts, `(started_at,id)<=($`+strconv.Itoa(len(args)-1)+`,$`+strconv.Itoa(len(args))+`)`)
	}
	return strings.Join(parts, " AND "), args
}

type trailingScanner struct {
	row   scanner
	extra []any
}

func (r trailingScanner) Scan(dest ...any) error { return r.row.Scan(append(dest, r.extra...)...) }

func (s *Service) List(ctx context.Context, a id.Actor, q uc.Query) (uc.Page, error) {
	if e := contextError(ctx); e != nil {
		return uc.Page{}, e
	}
	if s.state() == nil {
		return uc.Page{}, fault(f.DependencyUnbound)
	}
	if q.Validate() != nil || a.Validate() != nil {
		return uc.Page{}, fault(f.InvalidArgument)
	}
	q = q.Clone()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cause, e := readCause("list")
	if e != nil {
		return uc.Page{}, e
	}
	out := uc.Page{Items: []uc.Invocation{}}
	r := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		x, e := s.authorizeReader(ctx, tx, a, q.Filter.ProjectID, nil)
		if e != nil {
			return e
		}
		now, e := dbNow(ctx, x)
		if e != nil {
			return e
		}
		binding := cursorBinding(a, q.Filter, "")
		pos, e := s.position(q.Cursor, binding, q.Filter, false, now)
		if e != nil {
			return e
		}
		where, args := filterSQL(q.Filter, pos)
		bound := `SELECT started_at AS upper_at,id AS upper_id FROM matching ORDER BY started_at DESC,id DESC LIMIT 1`
		if pos.upperID != "" {
			args = append(args, pos.upperAt.Time(), pos.upperID)
			bound = `SELECT $` + strconv.Itoa(len(args)-1) + `::timestamptz AS upper_at,$` + strconv.Itoa(len(args)) + `::uuid AS upper_id`
		}
		keyset := "true"
		if pos.lastID != "" {
			args = append(args, pos.lastAt.Time(), pos.lastID)
			keyset = `(started_at,id)<($` + strconv.Itoa(len(args)-1) + `,$` + strconv.Itoa(len(args)) + `)`
		}
		args = append(args, q.Limit+1)
		query := `WITH matching AS NOT MATERIALIZED (SELECT * FROM agenteam_model.invocations WHERE ` + where + `), first_bound AS (` + bound + `) SELECT ` + invocationColumns + `,upper_at,upper_id::text FROM matching CROSS JOIN first_bound WHERE (started_at,id)<=(upper_at,upper_id) AND ` + keyset + ` ORDER BY started_at DESC,id DESC LIMIT $` + strconv.Itoa(len(args))
		rows, e := x.Query(ctx, query, args...)
		if e != nil {
			return dbError(e)
		}
		var upper time.Time
		for rows.Next() {
			v, e := scanInvocation(trailingScanner{rows, []any{&upper, &pos.upperID}})
			if e != nil {
				rows.Close()
				return e
			}
			pos.upperAt, _ = f.NewInstant(upper)
			out.Items = append(out.Items, v.value.Clone())
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			return dbError(e)
		}
		if len(out.Items) > q.Limit {
			out.Items = out.Items[:q.Limit]
			last := out.Items[len(out.Items)-1]
			pos.lastAt, pos.lastID = last.StartedAt, last.ID.String()
			out.NextCursor, e = s.signPosition(pos, binding, false)
			if e != nil {
				return e
			}
		}
		return contextError(ctx)
	})
	if e = completedRead(ctx, r); e != nil {
		return uc.Page{}, e
	}
	return out, nil
}
func groupExpression(by uc.GroupBy) string {
	switch by {
	case uc.ByConsumer:
		return "consumer_kind"
	case uc.ByAgent:
		return "agent_id::text"
	case uc.ByModel:
		return "historical_model_id::text"
	case uc.ByProvider:
		return "historical_provider_id::text"
	case uc.ByExecution:
		return "execution_id::text"
	case uc.ByMeeting:
		return "meeting_id::text"
	case uc.ByPurpose:
		return "purpose"
	case uc.ByDay:
		return `to_char(started_at AT TIME ZONE 'UTC','YYYY-MM-DD')`
	}
	return ""
}
func groupKey(by uc.GroupBy, key *string) uc.GroupKey {
	v := uc.GroupKey{By: by}
	if by == uc.ByDay {
		v.Day = key
	} else {
		v.ID = key
	}
	return v
}
func (s *Service) Aggregate(ctx context.Context, a id.Actor, q uc.AggregateQuery) (uc.AggregatePage, error) {
	if e := contextError(ctx); e != nil {
		return uc.AggregatePage{}, e
	}
	if s.state() == nil {
		return uc.AggregatePage{}, fault(f.DependencyUnbound)
	}
	if q.Validate() != nil || a.Validate() != nil {
		return uc.AggregatePage{}, fault(f.InvalidArgument)
	}
	q = q.Clone()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cause, e := readCause("aggregate")
	if e != nil {
		return uc.AggregatePage{}, e
	}
	out := uc.AggregatePage{Items: []uc.GroupSummary{}}
	r := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		x, e := s.authorizeReader(ctx, tx, a, q.Filter.ProjectID, nil)
		if e != nil {
			return e
		}
		now, e := dbNow(ctx, x)
		if e != nil {
			return e
		}
		out.AsOf = now
		binding := cursorBinding(a, q.Filter, q.GroupBy)
		pos, e := s.position(q.Cursor, binding, q.Filter, true, now)
		if e != nil {
			return e
		}
		if q.Cursor != "" {
			var key *string
			if !pos.groupNull {
				key = &pos.group
			}
			if groupKey(q.GroupBy, key).Validate() != nil {
				return fault(f.CursorInvalid)
			}
		}
		where, args := filterSQL(q.Filter, pos)
		bound := `SELECT started_at AS upper_at,id AS upper_id FROM matching ORDER BY started_at DESC,id DESC LIMIT 1`
		if pos.upperID != "" {
			args = append(args, pos.upperAt.Time(), pos.upperID)
			bound = `SELECT $` + strconv.Itoa(len(args)-1) + `::timestamptz AS upper_at,$` + strconv.Itoa(len(args)) + `::uuid AS upper_id`
		}
		parts := aggregateParts()
		names := make([]string, len(parts))
		for i := range parts {
			names[i] = "s" + strconv.Itoa(i)
			parts[i] += " AS " + names[i]
		}
		after := "true"
		if q.Cursor != "" {
			if pos.groupNull {
				after = `group_key IS NOT NULL`
			} else {
				args = append(args, pos.group)
				after = `group_key>$` + strconv.Itoa(len(args))
			}
		}
		args = append(args, q.Limit+1)
		query := `WITH matching AS NOT MATERIALIZED (SELECT * FROM agenteam_model.invocations WHERE ` + where + `),first_bound AS (` + bound + `), bounded AS (SELECT matching.* FROM matching CROSS JOIN first_bound WHERE (started_at,id)<=(upper_at,upper_id)), grouped AS (SELECT ` + groupExpression(q.GroupBy) + ` AS group_key,` + strings.Join(parts, ",") + ` FROM bounded GROUP BY ` + groupExpression(q.GroupBy) + `) SELECT group_key,upper_at,upper_id::text,` + strings.Join(names, ",") + ` FROM grouped CROSS JOIN first_bound WHERE ` + after + ` ORDER BY group_key ASC NULLS FIRST LIMIT $` + strconv.Itoa(len(args))
		rows, e := x.Query(ctx, query, args...)
		if e != nil {
			return dbError(e)
		}
		for rows.Next() {
			var key *string
			var upper time.Time
			summary, e := scanAggregate(rows, now, &key, &upper, &pos.upperID)
			if e != nil {
				rows.Close()
				return e
			}
			pos.upperAt, _ = f.NewInstant(upper)
			g := uc.GroupSummary{Key: groupKey(q.GroupBy, key), Summary: summary}
			if g.Validate() != nil {
				rows.Close()
				return corrupt(nil)
			}
			out.Items = append(out.Items, g)
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			return dbError(e)
		}
		// Private headers are validated as well; malformed durable facts must not
		// become plausible totals merely because their numeric columns are valid.
		if pos.upperID != "" {
			bounded, bargs := filterSQL(q.Filter, pos)
			check, e := x.Query(ctx, `SELECT `+invocationColumns+` FROM agenteam_model.invocations WHERE `+bounded, bargs...)
			if e != nil {
				return dbError(e)
			}
			for check.Next() {
				if _, e = scanInvocation(check); e != nil {
					check.Close()
					return e
				}
			}
			check.Close()
			if e = check.Err(); e != nil {
				return dbError(e)
			}
		}
		if len(out.Items) > q.Limit {
			out.Items = out.Items[:q.Limit]
			last := out.Items[len(out.Items)-1].Key
			key := last.ID
			if q.GroupBy == uc.ByDay {
				key = last.Day
			}
			pos.groupNull = key == nil
			pos.group = textPtr(key)
			out.NextCursor, e = s.signPosition(pos, binding, true)
			if e != nil {
				return e
			}
		}
		return contextError(ctx)
	})
	if e = completedRead(ctx, r); e != nil {
		return uc.AggregatePage{}, e
	}
	return out, nil
}
