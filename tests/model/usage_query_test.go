//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

func TestUsageReaderNonemptyPagination(t *testing.T) {
	v := newUsageFixture(t, true)
	first := v.success(t, json.RawMessage(`{"prompt_tokens":0,"completion_tokens":2,"total_tokens":2}`), nil)
	v.success(t, nil, nil)
	tool := mc.Consumer{Kind: mc.ToolConsumer, ProjectID: v.project.ID, Purpose: mc.ApprovalAuto, OperationID: newID[struct{}](t).String()}
	withoutExecution := v.success(t, json.RawMessage(`{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}`), &tool)
	q := uc.Query{Filter: uc.Filter{ProjectID: v.project.ID}, Limit: 1}
	var firstCursor string
	t.Run("nonempty-pages-and-current-new-session", func(t *testing.T) {
		seen := map[mc.InvocationID]bool{}
		uid, _ := f.ParseID[id.User](v.owner.Details().UserID)
		oldSession := v.session(t, uid)
		page, e := v.ledger.List(testContext(t), oldSession, q)
		if e != nil || len(page.Items) != 1 || page.NextCursor == "" {
			t.Fatal("first real page", e)
		}
		firstCursor = page.NextCursor
		seen[page.Items[0].ID] = true
		fresh := v.session(t, uid)
		next := q
		next.Cursor = page.NextCursor
		v.logoutSession(t, oldSession)
		rejected, e := v.ledger.List(testContext(t), oldSession, next)
		if usageErrorCode(e) != f.SessionRevoked || len(rejected.Items) != 0 || rejected.NextCursor != "" {
			t.Fatal("logged-out Session continued signed page", e)
		}
		page, e = v.ledger.List(testContext(t), fresh, next)
		if e != nil || len(page.Items) != 1 || page.NextCursor == "" {
			t.Fatal("same stable user new Session continuation", e)
		}
		_, keys, _ := testKeys(t)
		scope, _ := id.InProject(v.project.ID)
		order := "started_at:desc,id:desc"
		binding := cursor.Binding{Scope: scope, Order: order, QueryDigest: resolutionDigest(struct {
			Format int
			User   string
			Kind   string
			Filter uc.Filter
			Group  uc.GroupBy
			Order  string
		}{1, fresh.Details().UserID, "list", q.Filter, "", order})}
		firstPosition, e := keys.Verify(firstCursor, binding)
		if e != nil || len(firstPosition.Scalars) != 6 {
			t.Fatal("signed first position", e)
		}
		secondPosition, e := keys.Verify(page.NextCursor, binding)
		if e != nil || len(secondPosition.Scalars) != 6 {
			t.Fatal("signed continuation position", e)
		}
		for _, i := range []int{0, 1, 4, 5} {
			if firstPosition.Scalars[i].Kind() != secondPosition.Scalars[i].Kind() || firstPosition.Scalars[i].Value() != secondPosition.Scalars[i].Value() {
				t.Fatal("continuation changed first upper bound or default effective time range")
			}
		}
		from, ef := f.ParseInstant(firstPosition.Scalars[4].Value())
		to, et := f.ParseInstant(firstPosition.Scalars[5].Value())
		if ef != nil || et != nil || to.Time().Sub(from.Time()) != 30*24*time.Hour {
			t.Fatal("default signed range is not thirty days")
		}
		seen[page.Items[0].ID] = true
		next.Cursor, next.Limit = page.NextCursor, 2
		page, e = v.ledger.List(testContext(t), fresh, next)
		if e != nil || len(page.Items) != 1 || page.NextCursor != "" {
			t.Fatal("different-limit final continuation", e)
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatal("duplicate page")
			}
			seen[item.ID] = true
		}
		if len(seen) != 3 {
			t.Fatal("lost page rows")
		}
	})
	t.Run("consumer-without-execution-does-not-create-summary", func(t *testing.T) {
		if withoutExecution.event.Value.Consumer.ExecutionID != nil {
			t.Fatal("fixture unexpectedly has Execution")
		}
		var count int
		if e := v.raw.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_model.execution_usage_summaries WHERE project_id=$1`, v.project.ID.String()).Scan(&count); e != nil || count != 2 {
			t.Fatal("no-Execution call created a summary row", e)
		}
	})
	t.Run("all-filter-dimensions", func(t *testing.T) {
		value := first.event.Value
		kind, purpose, status := value.Consumer.Kind, value.Consumer.Purpose, uc.Succeeded
		from, _ := f.NewInstant(value.StartedAt.Time().Add(-time.Second))
		to, _ := f.NewInstant(time.Now().Add(time.Second))
		filter := uc.Filter{ProjectID: v.project.ID, ConsumerKind: &kind, AgentID: value.Consumer.AgentID, ExecutionID: value.Consumer.ExecutionID, Purpose: &purpose, ProviderID: &value.Identity.ProviderID, ModelID: &value.Identity.ModelID, Status: &status, From: &from, To: &to}
		page, e := v.ledger.List(testContext(t), v.owner, uc.Query{Filter: filter, Limit: 100})
		if e != nil || len(page.Items) != 1 || page.Items[0].ID != value.ID {
			t.Fatal("combined exact filters", e)
		}
		filter.MeetingID = newID[struct{}](t).String()
		page, e = v.ledger.List(testContext(t), v.owner, uc.Query{Filter: filter, Limit: 100})
		if e != nil || len(page.Items) != 0 {
			t.Fatal("Meeting filter ignored", e)
		}
	})
	t.Run("eight-group-keys-and-null-groups", func(t *testing.T) {
		for _, by := range []uc.GroupBy{uc.ByConsumer, uc.ByAgent, uc.ByModel, uc.ByProvider, uc.ByExecution, uc.ByMeeting, uc.ByPurpose, uc.ByDay} {
			t.Run(string(by), func(t *testing.T) {
				query := uc.AggregateQuery{Filter: q.Filter, GroupBy: by, Limit: 1}
				var confirmed, known, unknown mc.TokenCount
				seen := map[string]bool{}
				for n := 0; n < 10; n++ {
					page, e := v.ledger.Aggregate(testContext(t), v.owner, query)
					if e != nil || len(page.Items) == 0 {
						t.Fatal("nonempty group page", e)
					}
					for _, item := range page.Items {
						key := string(resolutionJSON(item.Key))
						if seen[key] {
							t.Fatal("group duplicated")
						}
						seen[key] = true
						confirmed += item.Summary.ConfirmedInvocations
						known += item.Summary.Input.KnownCount
						unknown += item.Summary.Input.UnknownCount
					}
					if page.NextCursor == "" {
						break
					}
					query.Cursor = page.NextCursor
				}
				if confirmed != 3 || known != 2 || unknown != 1 {
					t.Fatal("group total truncated or null denominator", confirmed, known, unknown)
				}
			})
		}
	})
	t.Run("cursor-binding-and-tamper", func(t *testing.T) {
		bad := q
		bad.Cursor = firstCursor + "x"
		page, e := v.ledger.List(testContext(t), v.owner, bad)
		if usageErrorCode(e) != f.CursorInvalid || len(page.Items) != 0 {
			t.Fatal("tampered cursor", e)
		}
		bad.Cursor = firstCursor
		status := uc.Failed
		bad.Filter.Status = &status
		_, e = v.ledger.List(testContext(t), v.owner, bad)
		requireCode(t, e, f.CursorInvalid)
		other := v.createProject(t, v.owner)
		bad = q
		bad.Cursor = firstCursor
		bad.Filter.ProjectID = other.ID
		_, e = v.ledger.List(testContext(t), v.owner, bad)
		requireCode(t, e, f.CursorInvalid)
		_, e = v.ledger.Aggregate(testContext(t), v.owner, uc.AggregateQuery{Filter: q.Filter, GroupBy: uc.ByModel, Cursor: firstCursor, Limit: 1})
		requireCode(t, e, f.CursorInvalid)
	})
	t.Run("explicit-history-range-versus-default-thirty-days", func(t *testing.T) {
		// A marked statistical backfill row tests only Reader time selection;
		// it is not evidence of an actual wire exchange forty days ago.
		historical, _ := f.NewInstant(usageNow(t, v).Time().Add(-40 * 24 * time.Hour))
		usageStatisticalRows(t, v, first, 1, &historical)
		defaultQuery := q
		defaultQuery.Limit = 100
		page, e := v.ledger.List(testContext(t), v.owner, defaultQuery)
		if e != nil || len(page.Items) != 3 {
			t.Fatal("default thirty-day range admitted historical backfill", e)
		}
		from, _ := f.NewInstant(historical.Time().Add(-time.Second))
		to, _ := f.NewInstant(historical.Time().Add(time.Second))
		explicit := q
		explicit.Filter.From, explicit.Filter.To = &from, &to
		page, e = v.ledger.List(testContext(t), v.owner, explicit)
		if e != nil || len(page.Items) != 1 || page.Items[0].StartedAt != historical {
			t.Fatal("explicit historical range missed safe backfill", e)
		}
	})
	t.Run("archived-owner-read-and-deleting-rejection", func(t *testing.T) {
		v.gate(t, pc.Archived)
		page, e := v.ledger.List(testContext(t), v.owner, q)
		if e != nil || len(page.Items) != 1 {
			t.Fatal("archived read denied", e)
		}
		v.project, e = v.projectService.GetProject(testContext(t), v.owner, v.project.ID)
		if e != nil {
			t.Fatal(e)
		}
		v.gate(t, pc.Deleting)
		page, e = v.ledger.List(testContext(t), v.owner, q)
		if e == nil || len(page.Items) != 0 {
			t.Fatal("deleting content read")
		}
	})
}

func TestUsageExecutionSummaryRebuild(t *testing.T) {
	v := newUsageFixture(t, true)
	first := v.success(t, json.RawMessage(`{"prompt_tokens":3,"completion_tokens":0,"total_tokens":3}`), nil)
	second := v.nextExecutionCall(t, first)
	v.reserve(t, second)
	v.authorize(t, second)
	v.startWire(t, second, wire.JSONResponse)
	if _, e := second.exchange.Result(testContext(t)); e != nil {
		t.Fatal(e)
	}
	v.observeWire(t, second)
	v.finishWire(t, second, uc.Succeeded, nil)
	execution := *first.call.Consumer.ExecutionID
	check := func(t *testing.T) uc.ExecutionSummary {
		t.Helper()
		s, e := v.ledger.GetExecutionSummary(testContext(t), v.owner, v.project.ID, execution)
		if e != nil || s.Summary.ConfirmedInvocations != 2 || s.Summary.Succeeded != 2 || s.Summary.Input.Sum == nil || *s.Summary.Input.Sum != 6 || s.Summary.Output.Sum == nil || *s.Summary.Output.Sum != 0 || s.Summary.Reasoning.Sum != nil || s.Summary.Reasoning.UnknownCount != 2 {
			t.Fatal("exact execution projection", e)
		}
		return s
	}
	t.Run("canonical-totals-and-unknown-execution", func(t *testing.T) {
		s := check(t)
		page, e := v.ledger.Aggregate(testContext(t), v.owner, uc.AggregateQuery{Filter: uc.Filter{ProjectID: v.project.ID, ExecutionID: &execution}, GroupBy: uc.ByExecution, Limit: 100})
		if e != nil || len(page.Items) != 1 {
			t.Fatal(e)
		}
		a := page.Items[0].Summary
		a.AsOf = s.Summary.AsOf
		if !reflect.DeepEqual(a, s.Summary) {
			t.Fatal("canonical aggregate disagrees")
		}
		_, e = v.ledger.GetExecutionSummary(testContext(t), v.owner, v.project.ID, newID[id.Execution](t))
		requireCode(t, e, f.NotFound)
		other := v.createProject(t, v.owner)
		_, e = v.ledger.GetExecutionSummary(testContext(t), v.owner, other.ID, execution)
		requireCode(t, e, f.NotFound)
	})
	t.Run("repair-missing-and-valid-but-wrong-projection", func(t *testing.T) {
		if _, e := v.raw.Exec(testContext(t), `DELETE FROM agenteam_model.execution_usage_summaries WHERE execution_id=$1`, execution.String()); e != nil {
			t.Fatal(e)
		}
		s, e := v.ledger.RebuildExecutionSummary(testContext(t), v.owner, v.project.ID, execution)
		if e != nil || s.Version != 1 {
			t.Fatal("missing repair", e)
		}
		check(t)
		again, e := v.ledger.RebuildExecutionSummary(testContext(t), v.owner, v.project.ID, execution)
		if e != nil || again.Version != s.Version {
			t.Fatal("identical rebuild advanced version", e)
		}
		if _, e = v.raw.Exec(testContext(t), `UPDATE agenteam_model.execution_usage_summaries SET input_sum=1 WHERE execution_id=$1`, execution.String()); e != nil {
			t.Fatal(e)
		}
		s, e = v.ledger.RebuildExecutionSummary(testContext(t), v.owner, v.project.ID, execution)
		if e != nil || s.Version != 2 {
			t.Fatal("wrong projection repair", e)
		}
		check(t)
	})
	t.Run("ordinary-concurrent-final-and-rebuild", func(t *testing.T) {
		third := v.nextExecutionCall(t, first)
		v.reserve(t, third)
		v.authorize(t, third)
		v.startWire(t, third, wire.JSONResponse)
		if _, e := third.exchange.Result(testContext(t)); e != nil {
			t.Fatal(e)
		}
		v.observeWire(t, third)
		wireClose(t, third.exchange)
		third.event.Sequence++
		third.event.Action = uc.FinalizeAction
		third.event.Joined = true
		third.event.Value.Final = &uc.Final{Status: uc.Succeeded, Version: 1, FinishedAt: usageNow(t, v)}
		r := v.stage(t, third.event)
		plan, e := v.ledger.DiscoverInvocation(testContext(t), r)
		if e != nil {
			t.Fatal(e)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		errs := make(chan error, 2)
		go func() {
			defer wg.Done()
			_, result := v.applyPlan(t, r, plan, false, false)
			if result.State() != f.Committed {
				errs <- result.Fault()
			}
		}()
		go func() {
			defer wg.Done()
			_, e := v.ledger.RebuildExecutionSummary(testContext(t), v.owner, v.project.ID, execution)
			if e != nil {
				errs <- e
			}
		}()
		wg.Wait()
		close(errs)
		for e := range errs {
			t.Fatal(e)
		}
		s, e := v.ledger.GetExecutionSummary(testContext(t), v.owner, v.project.ID, execution)
		if e != nil || s.Summary.ConfirmedInvocations != 3 || s.Summary.Succeeded != 3 || *s.Summary.Total.Sum != 9 {
			t.Fatal("lost concurrent final", e)
		}
	})
}

func usageStatisticalDigests(identity uc.InvocationIdentity, initiator id.Actor, value uc.Invocation) (f.Digest, f.Digest) {
	value = value.Clone()
	value.LiveProviderID, value.LiveModelID = nil, nil
	header := struct {
		Format    int
		Identity  uc.InvocationIdentity
		Initiator id.ActorDetails
		Model     mc.ModelIdentity
		StartedAt f.Instant
	}{1, identity, usageStable(initiator), value.Identity, value.StartedAt}
	return resolutionDigest(header), resolutionDigest(struct {
		Header any
		Value  uc.Invocation
	}{header, value})
}

// Clearly marked synthetic statistical data: these rows are not wire evidence.
func usagePerformanceRows(t *testing.T, v *usageFixture, c *usageCase, n int) {
	t.Helper()
	usageStatisticalRows(t, v, c, n, nil)
}
func usageStatisticalRows(t *testing.T, v *usageFixture, c *usageCase, n int, at *f.Instant) {
	t.Helper()
	var raw []byte
	if e := v.raw.QueryRow(testContext(t), `SELECT to_jsonb(i) FROM agenteam_model.invocations i WHERE id=$1`, c.event.Value.ID.String()).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var template map[string]json.RawMessage
	if json.Unmarshal(raw, &template) != nil {
		t.Fatal("statistics template")
	}
	rows := make([]map[string]json.RawMessage, 0, n)
	for range n {
		record := make(map[string]json.RawMessage, len(template))
		for key, value := range template {
			record[key] = value
		}
		identity := c.event.Identity.Clone()
		identity.Attempt.CallID = newID[mc.Call](t)
		identity.Attempt.InvocationID = newID[mc.Invocation](t)
		value := c.event.Value.Clone()
		value.CallID = identity.Attempt.CallID
		value.ID = identity.Attempt.InvocationID
		value.LiveProviderID = nil
		value.LiveModelID = nil
		if at != nil {
			delta := at.Time().Sub(value.StartedAt.Time())
			value.StartedAt = *at
			dispatched, _ := f.NewInstant(value.DispatchedAt.Time().Add(delta))
			value.DispatchedAt = &dispatched
			value.Final.FinishedAt, _ = f.NewInstant(value.Final.FinishedAt.Time().Add(delta))
			record["started_at"] = resolutionJSON(value.StartedAt)
			record["dispatched_at"] = resolutionJSON(value.DispatchedAt)
			record["finished_at"] = resolutionJSON(value.Final.FinishedAt)
			record["updated_at"] = resolutionJSON(value.Final.FinishedAt)
		}
		header, final := usageStatisticalDigests(identity, v.owner, value)
		record["id"] = resolutionJSON(value.ID)
		record["call_id"] = resolutionJSON(value.CallID)
		record["header_digest"] = resolutionJSON(header)
		record["final_digest"] = resolutionJSON(final)
		rows = append(rows, record)
	}
	if _, e := v.raw.Exec(testContext(t), `INSERT INTO agenteam_model.invocations SELECT * FROM jsonb_populate_recordset(NULL::agenteam_model.invocations,$1::jsonb)`, resolutionJSON(rows)); e != nil {
		t.Fatal("synthetic statistics load", e)
	}
	if _, e := v.raw.Exec(testContext(t), `ANALYZE agenteam_model.invocations`); e != nil {
		t.Fatal("statistics fixture analyze", e)
	}
}
func TestUsageQueryBudgetAndSafety(t *testing.T) {
	v := newUsageFixture(t, true)
	c := v.success(t, json.RawMessage(`{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}`), nil)
	usagePerformanceRows(t, v, c, 1200)
	q := uc.Query{Filter: uc.Filter{ProjectID: v.project.ID}, Limit: 100}
	t.Run("full-statistical-set-and-real-index-plan", func(t *testing.T) {
		started := time.Now()
		page, e := v.ledger.Aggregate(testContext(t), v.owner, uc.AggregateQuery{Filter: q.Filter, GroupBy: uc.ByProvider, Limit: 1})
		if e != nil || len(page.Items) != 1 || page.Items[0].Summary.ConfirmedInvocations != 1201 || *page.Items[0].Summary.Input.Sum != 1201 || time.Since(started) > 2*time.Second {
			t.Fatal("statistical set incomplete or budget", e)
		}
		t.Logf("synthetic full aggregate: confirmed=1201 input_sum=1201 elapsed=%s budget=2s", time.Since(started))
		rows, e := v.raw.Query(testContext(t), `EXPLAIN (FORMAT TEXT) SELECT id FROM agenteam_model.invocations WHERE project_id=$1 ORDER BY started_at DESC,id DESC LIMIT 20`, v.project.ID.String())
		if e != nil {
			t.Fatal(e)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if e = rows.Scan(&line); e != nil {
				t.Fatal(e)
			}
			plan.WriteString(line)
		}
		rows.Close()
		if !strings.Contains(plan.String(), "model_invocation_project_page") {
			t.Fatalf("expected keyset index plan: %s", plan.String())
		}
		t.Log(plan.String())
	})
	t.Run("whole-call-budget-includes-lock-wait", func(t *testing.T) {
		held, release, done := make(chan struct{}), make(chan struct{}), make(chan f.CommitResult, 1)
		go func() {
			done <- v.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
				k, _ := f.ProjectLock(v.project.ID.String())
				if e := v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: k, Mode: f.Exclusive}}); e != nil {
					return e
				}
				close(held)
				<-release
				return nil
			})
		}()
		waitSignal(t, held)
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		started := time.Now()
		page, e := v.ledger.List(ctx, v.owner, q)
		cancel()
		close(release)
		if (<-done).State() != f.Committed {
			t.Fatal("lock owner failed")
		}
		if e == nil || len(page.Items) != 0 || page.NextCursor != "" || time.Since(started) > time.Second {
			t.Fatal("budget omitted lock wait")
		}
	})
	for _, name := range []string{"list", "aggregate"} {
		t.Run(name+"-actual-two-second-service-deadline", func(t *testing.T) {
			caller := testContext(t)
			reached := 0
			var remaining time.Duration
			v.storeView.mu.Lock()
			v.storeView.afterRead = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
				if cause.Details().Owner != "model.usage."+name {
					return nil
				}
				reached++
				user, _ := f.UserLock(v.owner.Details().UserID)
				project, _ := f.ProjectLock(v.project.ID.String())
				if e := v.raw.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared}}); e != nil {
					t.Fatal("deadline probe outside authorized transaction", e)
				}
				deadline, ok := ctx.Deadline()
				remaining = time.Until(deadline)
				if !ok || remaining <= 0 || remaining > 2*time.Second || caller.Err() != nil {
					t.Fatal("service did not pass its own two-second context")
				}
				<-ctx.Done()
				if !errors.Is(ctx.Err(), context.DeadlineExceeded) || caller.Err() != nil {
					t.Fatal("service budget extended or caller supplied deadline")
				}
				return ctx.Err()
			}
			v.storeView.mu.Unlock()
			started := time.Now()
			var zero bool
			var e error
			if name == "list" {
				var result uc.Page
				result, e = v.ledger.List(caller, v.owner, q)
				zero = reflect.DeepEqual(result, uc.Page{})
			} else {
				var result uc.AggregatePage
				result, e = v.ledger.Aggregate(caller, v.owner, uc.AggregateQuery{Filter: q.Filter, GroupBy: uc.ByProvider, Limit: 1})
				zero = reflect.DeepEqual(result, uc.AggregatePage{})
			}
			elapsed := time.Since(started)
			v.storeView.mu.Lock()
			actual := v.storeView.actual.State()
			v.storeView.afterRead = nil
			v.storeView.mu.Unlock()
			if reached != 1 || e == nil || !zero || caller.Err() != nil || actual != f.NotCommitted || elapsed < 1800*time.Millisecond || elapsed > 4*time.Second {
				t.Fatal("service deadline exposed partial result or retried", elapsed, actual, e)
			}
			t.Logf("real authorized SQL completed; service_budget=2s remaining=%s elapsed=%s caller_live=true actual=%s zero_DTO=true", remaining, elapsed, actual)
		})
	}
	t.Run("oversized-cursor-and-safe-projections", func(t *testing.T) {
		bad := q
		bad.Cursor = strings.Repeat("a", 8193)
		page, e := v.ledger.List(testContext(t), v.owner, bad)
		if usageErrorCode(e) != f.CursorInvalid || len(page.Items) != 0 {
			t.Fatal("oversized cursor accepted")
		}
		page, e = v.ledger.List(testContext(t), v.owner, q)
		if e != nil {
			t.Fatal(e)
		}
		for _, value := range []any{page, e, c.event.Value} {
			for _, format := range []string{"%v", "%+v", "%#v"} {
				text := fmt.Sprintf(format, value)
				if strings.Contains(text, "usage canonical input") || strings.Contains(text, "fixture.test") || strings.Contains(text, "owned-test-credential") {
					t.Fatal("private material in log")
				}
			}
		}
		raw, _ := json.Marshal(page)
		if strings.Contains(string(raw), string(c.call.Input.Digest)) || strings.Contains(string(raw), "fixture.test") {
			t.Fatal("private data in read DTO")
		}
	})
	t.Run("bad-persistent-json-and-numeric-overflow-fail-safely", func(t *testing.T) {
		var original []byte
		if e := v.raw.QueryRow(testContext(t), `SELECT consumer_data FROM agenteam_model.invocations WHERE id=$1`, c.event.Value.ID.String()).Scan(&original); e != nil {
			t.Fatal(e)
		}
		if _, e := v.raw.Exec(testContext(t), `UPDATE agenteam_model.invocations SET consumer_data=consumer_data||'{"unknown_field":true}'::jsonb WHERE id=$1`, c.event.Value.ID.String()); e != nil {
			t.Fatal(e)
		}
		_, e := v.ledger.Aggregate(testContext(t), v.owner, uc.AggregateQuery{Filter: q.Filter, GroupBy: uc.ByProvider, Limit: 1})
		requireCode(t, e, f.InternalError)
		if _, e = v.raw.Exec(testContext(t), `UPDATE agenteam_model.invocations SET consumer_data=$2,final_digest=$3 WHERE id=$1`, c.event.Value.ID.String(), original, resolutionDigest("wrong-final")); e != nil {
			t.Fatal(e)
		}
		_, e = v.ledger.Aggregate(testContext(t), v.owner, uc.AggregateQuery{Filter: q.Filter, GroupBy: uc.ByProvider, Limit: 1})
		requireCode(t, e, f.InternalError)
		value := c.event.Value.Clone()
		maximum := mc.TokenCount(9223372036854775807)
		value.Usage.InputTokens = &maximum
		_, final := usageStatisticalDigests(c.event.Identity, v.owner, value)
		if _, e = v.raw.Exec(testContext(t), `UPDATE agenteam_model.invocations SET input_tokens=$2,final_digest=$3 WHERE id=$1`, c.event.Value.ID.String(), maximum, final); e != nil {
			t.Fatal(e)
		}
		page, e := v.ledger.Aggregate(testContext(t), v.owner, uc.AggregateQuery{Filter: q.Filter, GroupBy: uc.ByProvider, Limit: 1})
		if usageErrorCode(e) != f.InvalidState || len(page.Items) != 0 {
			t.Fatal("numeric aggregate overflow wrapped", e)
		}
	})
}
