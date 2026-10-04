//go:build integration

package outbox_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// A historical, already committed typed fact is seeded with its original
// database created_at. We do not disable the immutable-event trigger or derive
// old age from the producer's different occurred_at clock.
func historicalEvent(t *testing.T, f *fixture, e event.Event, created time.Time, handler event.StableName) oc.DeliveryID {
	t.Helper()
	h := e.Header()
	raw, err := e.HeaderJSON()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := oc.SemanticDigest(f.actor, e)
	if err != nil {
		t.Fatal(err)
	}
	actor, _ := oc.StableActor(f.actor)
	var project any
	if h.Scope.Kind == event.ProjectScope {
		project = h.Scope.ProjectID.String()
	}
	f.sql(t, `INSERT INTO agenteam_outbox.events(id,producer,event_type,schema_version,scope,project_id,aggregate_type,aggregate_id,aggregate_version,occurred_at,header,payload,semantic_digest,stable_actor,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,$14,$15)`, h.EventID.String(), string(e.Summary().Producer), string(h.EventType), int64(h.SchemaVersion), string(h.Scope.Kind), project, string(h.AggregateType), h.AggregateID.String(), int64(*h.AggregateVersion), h.OccurredAt.Time(), string(raw), e.PayloadBytes(), digest.String(), actor, created)
	if handler == "" {
		return oc.DeliveryID{}
	}
	d := id[oc.Delivery](t)
	f.sql(t, `INSERT INTO agenteam_outbox.deliveries(id,event_id,handler_id,scope,project_id,created_at) VALUES($1,$2,$3,$4,$5,$6)`, d.String(), h.EventID.String(), string(handler), string(h.Scope.Kind), project, created)
	return d
}

func TestOutboxDiagnosticsMultiHandlerDedupAndSystemThroughput(t *testing.T) {
	f := newFixture(t)
	svc, _ := administration(t, f, f.store, false)
	a, b := f.handler("summary.a"), f.handler("summary.b")
	for _, h := range []*handler{a, b} {
		if _, err := svc.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
			t.Fatal(err)
		}
	}
	for _, project := range []bool{false, true} {
		_, result := f.append(t, svc, f.store, f.actor, f.event(t, project, 1, "private multi handler fact"))
		state(t, result, foundation.Committed)
	}
	page, err := svc.QueryDiagnostics(ctxFor(t), f.actor, identity.SystemScope(), oc.DiagnosticsFilter{}, foundation.PageRequest{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Summary.PendingEvents != 1 || len(page.Summary.EventThroughput) != 1 || page.Summary.EventThroughput[0].ProducedEvents != 1 {
		t.Fatal("system facts were omitted, crossed scope, or duplicated by handler")
	}
	r := startRuntime(t, f, []oc.HandlerDefinition{a.definition(1), b.definition(1)}, outbox.Options{PollInterval: time.Millisecond})
	waitOutbox(t, func() bool { return f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) == 4 })
	r.StopClaims()
	if err := r.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	page, err = svc.QueryDiagnostics(ctxFor(t), f.actor, identity.SystemScope(), oc.DiagnosticsFilter{}, foundation.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Summary.PendingEvents != 0 || page.Summary.OldestPendingAgeMS != nil || page.Summary.EventThroughput[0].ProducedEvents != 1 || page.Summary.EventThroughput[0].SucceededDeliveries != 2 || len(page.Summary.HandlerLatency) != 2 {
		t.Fatal("successful delivery throughput was deduplicated as events or page count")
	}
	filtered, err := svc.QueryDiagnostics(ctxFor(t), f.actor, identity.SystemScope(), oc.DiagnosticsFilter{HandlerID: a.name}, foundation.PageRequest{Limit: 100})
	if err != nil || len(filtered.Items) != 1 || filtered.Summary.EventThroughput[0].SucceededDeliveries != 1 || len(filtered.Summary.HandlerLatency) != 1 {
		t.Fatal("handler filter did not constrain system summary and items", err)
	}
}
func TestOutboxDiagnosticsOldBacklogExactAttemptsAndQueryBudget(t *testing.T) {
	f := newFixture(t)
	// This dedicated fixture lets the API deadline, not the default 1s SQL
	// lock_timeout, end the real blocked query. Production defaults are unchanged.
	f.store = openStore(t, f.db.Config(t, map[string]string{"LOCK_TIMEOUT": "5s"}))
	f.auth.store = f.store
	svc, _ := administration(t, f, f.store, false)
	h := f.handler("statistics.handler")
	if _, err := svc.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
		t.Fatal(err)
	}
	old := f.event(t, true, 1, "old private backlog")
	historicalEvent(t, f, old, time.Now().Add(-48*time.Hour), h.name)
	for i := 0; i < 24; i++ {
		e := f.event(t, true, 1, "recent private failure")
		_, result := f.append(t, svc, f.store, f.actor, e)
		state(t, result, foundation.Committed)
		d := f.delivery(t, e, string(h.name))
		f.claim(t, d)
		if i == 0 {
			continue
		} // Actual claimed row has no observable return/latency.
		f.sql(t, `UPDATE agenteam_outbox.attempts SET handler_returned_at=started_at+interval '1200 microseconds',joined_at=started_at+interval '1300 microseconds',finished_at=clock_timestamp(),checkpoint='failed',safe_reason='handler_retry' WHERE delivery_id=$1`, d.String())
		f.sql(t, `UPDATE agenteam_outbox.deliveries SET phase='failed',safe_reason='handler_retry',last_at=clock_timestamp() WHERE id=$1`, d.String())
	}
	project, _ := foundation.ParseID[identity.Project](f.project.String())
	scope, _ := identity.InProject(project)
	page, err := svc.QueryDiagnostics(ctxFor(t), f.actor, scope, oc.DiagnosticsFilter{Phase: oc.Failed}, foundation.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	summary := page.Summary
	if summary.PendingEvents != 2 || summary.FailedDeliveries != 23 || summary.OldestPendingAgeMS == nil || int64(*summary.OldestPendingAgeMS) < int64((48*time.Hour)/time.Millisecond) || len(summary.HandlerLatency) != 1 || summary.HandlerLatency[0].Count != 23 || summary.HandlerLatency[0].Unknown != 1 || summary.HandlerLatency[0].SumMS != 23 || summary.HandlerLatency[0].MaxMS == nil || *summary.HandlerLatency[0].MaxMS != 1 || len(summary.RecentErrors) != 20 || summary.EventThroughput[0].ProducedEvents != 24 {
		t.Fatalf("real typed summary mismatch %+v", summary)
	}
	if len(page.Items) != 1 || page.Items[0].Phase != oc.Failed {
		t.Fatal("phase/limit not limited to Items")
	}
	absent, err := svc.QueryDiagnostics(ctxFor(t), f.actor, scope, oc.DiagnosticsFilter{HandlerID: "other.handler"}, foundation.PageRequest{Limit: 1})
	if err != nil || absent.Summary.PendingEvents != 0 || absent.Summary.OldestPendingAgeMS != nil || len(absent.Summary.HandlerLatency) != 0 || len(absent.Summary.RecentErrors) != 0 {
		t.Fatal("handler filter did not constrain every component")
	}
	// Impossible persisted time ordering is not a zero sample or a clipped value.
	f.sql(t, `UPDATE agenteam_outbox.attempts SET handler_returned_at=started_at-interval '1 microsecond' WHERE id=(SELECT id FROM agenteam_outbox.attempts WHERE handler_returned_at IS NOT NULL LIMIT 1)`)
	bad, err := svc.QueryDiagnostics(ctxFor(t), f.actor, scope, oc.DiagnosticsFilter{}, foundation.PageRequest{Limit: 1})
	if err == nil || len(bad.Items) != 0 || bad.Summary.Window != "" {
		t.Fatal("corrupt latency returned partial success")
	}
	f.sql(t, `UPDATE agenteam_outbox.attempts SET handler_returned_at=started_at+interval '1200 microseconds' WHERE handler_returned_at IS NOT NULL`)
	conn := f.db.Connect(t)
	block, err := conn.Begin(ctxFor(t))
	if err != nil {
		t.Fatal(err)
	}
	defer block.Rollback(context.Background())
	if _, err = block.Exec(ctxFor(t), `LOCK TABLE agenteam_outbox.events IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	bad, err = svc.QueryDiagnostics(context.Background(), f.actor, scope, oc.DiagnosticsFilter{}, foundation.PageRequest{Limit: 1})
	elapsed := time.Since(start)
	if err == nil || elapsed > 2500*time.Millisecond || elapsed < 1800*time.Millisecond || len(bad.Items) != 0 || bad.NextCursor != "" {
		t.Fatal("summary did not share one bounded two-second operation", elapsed, err)
	}
	short, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start = time.Now()
	bad, err = svc.QueryDiagnostics(short, f.actor, scope, oc.DiagnosticsFilter{}, foundation.PageRequest{Limit: 1})
	if err == nil || time.Since(start) > 500*time.Millisecond || len(bad.Items) != 0 || bad.Summary.Window != "" {
		t.Fatal("shorter caller deadline was refreshed")
	}
}
func TestOutboxDiagnosticsThroughputTruncationAndNoSubscriptionFacts(t *testing.T) {
	f := newFixture(t)
	svc, _ := administration(t, f, f.store, false)
	cat := event.NewCatalog()
	for i := 0; i < 129; i++ {
		name := event.StableName(fmt.Sprintf("type.t%03d", i))
		typ, err := event.DefineEvent(cat, event.Definition[payload]{Schema: event.Schema{Producer: "fixture", EventType: name, AggregateType: "fixture", Version: 1}, Codec: event.JSONCodec[payload]{}, Validate: func(payload) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		h := f.event(t, false, 1, "private").Header()
		h.EventType = name
		e, err := event.NewEvent(typ, h, payload{Value: "private"})
		if err != nil {
			t.Fatal(err)
		}
		historicalEvent(t, f, e, time.Now().Add(-time.Second), "")
	}
	page, err := svc.QueryDiagnostics(ctxFor(t), f.actor, identity.SystemScope(), oc.DiagnosticsFilter{}, foundation.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || len(page.Summary.EventThroughput) != 128 || !page.Summary.EventThroughputTruncated || page.Summary.HandlerLatencyTruncated || page.Summary.PendingEvents != 0 {
		t.Fatalf("no-subscription facts/truncation semantics lost: items=%d types=%d types_truncated=%v handlers_truncated=%v pending=%d", len(page.Items), len(page.Summary.EventThroughput), page.Summary.EventThroughputTruncated, page.Summary.HandlerLatencyTruncated, page.Summary.PendingEvents)
	}
	last, err := svc.QueryDiagnostics(ctxFor(t), f.actor, identity.SystemScope(), oc.DiagnosticsFilter{EventType: "type.t128"}, foundation.PageRequest{Limit: 100})
	if err != nil || len(last.Summary.EventThroughput) != 1 || last.Summary.EventThroughputTruncated || last.Summary.EventThroughput[0].ProducedEvents != 1 {
		t.Fatal("filtered truncated type inaccessible")
	}
}
