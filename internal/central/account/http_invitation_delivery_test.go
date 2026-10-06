package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Scanner-only fixtures; real codecs, authorization and snapshots are covered
// by the owned PostgreSQL/HTTP tests.
type invitationRows struct {
	values                          [][]any
	index, closed                   int
	scanError, rowError, closeError error
	onClose                         func()
}

func (r *invitationRows) Next() bool {
	if r.index == len(r.values) {
		return false
	}
	r.index++
	return true
}
func (r *invitationRows) Scan(dest ...any) error {
	if r.scanError != nil {
		return r.scanError
	}
	for i, value := range r.values[r.index-1] {
		target := reflect.ValueOf(dest[i]).Elem()
		if value == nil {
			target.SetZero()
		} else {
			target.Set(reflect.ValueOf(value))
		}
	}
	return nil
}
func (r *invitationRows) Err() error {
	if r.closed > 0 && r.closeError != nil {
		return r.closeError
	}
	return r.rowError
}
func (r *invitationRows) Close() {
	r.closed++
	if r.onClose != nil {
		r.onClose()
	}
}
func invitationRow(t *testing.T) []any {
	t.Helper()
	id, e := foundation.NewID[struct{}]()
	if e != nil {
		t.Fatal(e)
	}
	at := time.Date(2026, 10, 6, 1, 2, 3, 456789000, time.UTC)
	return []any{id.String(), "invite@example.test", int64(1), at, at.Add(24 * time.Hour), id.String(), &at,
		[]string{id.String(), id.String(), id.String(), id.String(), id.String(), id.String()}, true, true, false,
		"enqueue_pending", int64(0), int64(1), c.DeliveryReason(""), int64(0), "", "", "", int64(0), "", nil, nil, false, false}
}
func invitationAttempt(t *testing.T, result *string) []any {
	v := invitationRow(t)
	v[10] = true
	v[11] = "sending"
	v[12] = int64(1)
	v[13] = int64(2)
	v[15] = int64(1)
	v[16], v[17], v[18] = v[0], v[0], v[5]
	v[19] = int64(1)
	v[20] = "data"
	channel := "smtp"
	v[21] = &channel
	if result != nil {
		v[11] = *result
		v[20] = "closed"
		v[22] = result
		v[23] = true
		v[24] = true
	}
	return v
}
func TestHTTPInvitationDeliveryProjectionAndSchema(t *testing.T) {
	ring, _, _ := testRings(t)
	f := &SystemHTTPFacade{pagination: ring}
	rows := &invitationRows{values: [][]any{invitationRow(t), invitationRow(t)}}
	page, e := f.httpScanInvitationList(context.Background(), rows, 1)
	if e != nil || len(page.Items) != 1 || page.NextCursor == "" || rows.closed != 1 {
		t.Fatal("page/sentinel", e)
	}
	item := page.Items[0]
	d := item.LatestDelivery
	if d.Status.Phase != "enqueue_pending" || d.Status.Attempts != 0 || d.Status.Version != 1 || d.Channel != nil || d.AttemptResult != nil {
		t.Fatal("unenqueued facts", d)
	}
	wire, e := json.Marshal(httpInvitation{item.ID, item.Email, item.Version, item.CreatedAt, item.ExpiresAt, httpInvitationDelivery{d.Status, d.AcceptedAt, d.Channel, d.AttemptResult}})
	if e != nil {
		t.Fatal(e)
	}
	var object map[string]json.RawMessage
	if e = json.Unmarshal(wire, &object); e != nil {
		t.Fatal(e)
	}
	directoryRequireFields(t, object, []string{"id", "email", "version", "created_at", "expires_at", "latest_delivery"})
	var delivery map[string]json.RawMessage
	if e = json.Unmarshal(object["latest_delivery"], &delivery); e != nil {
		t.Fatal(e)
	}
	required := []string{"job_id", "accepted_at", "phase", "attempts", "version", "channel", "attempt_result"}
	directoryRequireFields(t, delivery, required)
	if string(delivery["channel"]) != "null" || string(delivery["attempt_result"]) != "null" || string(delivery["attempts"]) != `"0"` || string(delivery["version"]) != `"1"` || string(delivery["accepted_at"]) != `"2026-10-06T01:02:03.456789Z"` {
		t.Fatal("wire scalars", string(wire))
	}
	expected, e := f.httpNextCursor("invitations", item.CreatedAt.Time(), item.ID.String())
	if e != nil || expected != page.NextCursor {
		t.Fatal("invitation cursor changed", e)
	}
	raw, e := os.ReadFile("../../../api/openapi/account.json")
	if e != nil {
		t.Fatal(e)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties           map[string]json.RawMessage
				Required             []string
				AdditionalProperties bool
			}
		}
	}
	if e = json.Unmarshal(raw, &doc); e != nil {
		t.Fatal(e)
	}
	schema := doc.Components.Schemas["InvitationDelivery"]
	if !reflect.DeepEqual(schema.Required, required) || schema.AdditionalProperties || len(schema.Properties) != 8 {
		t.Fatal("delivery schema shape")
	}
	for _, field := range []string{"channel", "attempt_result"} {
		var property struct{ Type []string }
		if e = json.Unmarshal(schema.Properties[field], &property); e != nil || !reflect.DeepEqual(property.Type, []string{"string", "null"}) {
			t.Fatal("nullable required fact", field, e)
		}
	}
	boundary, e := csrfNewBoundary("https://example.test")
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	(&accountHTTP{csrf: boundary}).httpHandler().ServeHTTP(w, httptest.NewRequest("HEAD", "https://example.test/api/v1/system/invitations", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "GET, POST" || w.Body.Len() != 0 {
		t.Fatal("invitation method changed")
	}
	empty, e := f.httpScanInvitationList(context.Background(), &invitationRows{}, 25)
	if e != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != "" {
		t.Fatal("empty list", e)
	}
}
func TestHTTPInvitationDeliveryCurrentAttemptAndLegacy(t *testing.T) {
	for _, result := range []string{"sent", "failed", "unknown", "cancelled"} {
		t.Run(result, func(t *testing.T) {
			row := invitationAttempt(t, &result)
			if result == "unknown" {
				row[11] = "retry_wait"
			}
			channel := "log"
			row[21] = &channel
			item, e := httpScanInvitation(&invitationRows{values: [][]any{row}, index: 1})
			if e != nil {
				t.Fatal(e)
			}
			d := item.LatestDelivery
			if d.Channel == nil || *d.Channel != "backend_log" || d.AttemptResult == nil || string(*d.AttemptResult) != result {
				t.Fatal("actual attempt facts", d)
			}
		})
	}
	for _, phase := range []string{"pending", "cancelled", "processing", "unknown"} {
		row := invitationRow(t)
		row[10] = true
		row[11] = phase
		item, e := httpScanInvitation(&invitationRows{values: [][]any{row}, index: 1})
		want := phase
		if phase == "processing" {
			want = "unknown"
		}
		if e != nil || item.LatestDelivery.Status.Phase != want || item.LatestDelivery.Channel != nil || item.LatestDelivery.AttemptResult != nil {
			t.Fatal("legacy/no attempt", phase, e)
		}
	}
	row := invitationAttempt(t, nil)
	row[24] = true // Joined I/O before the final transaction is not an outcome.
	item, e := httpScanInvitation(&invitationRows{values: [][]any{row}, index: 1})
	if e != nil || item.LatestDelivery.AttemptResult != nil {
		t.Fatal("joined guessed a result", e)
	}
}
func TestHTTPInvitationDeliveryBadRowsAndCancellationReturnZero(t *testing.T) {
	ring, _, _ := testRings(t)
	f := &SystemHTTPFacade{pagination: ring}
	bad := map[string]func([]any){
		"id": func(r []any) { r[0] = "invalid" }, "version": func(r []any) { r[2] = int64(0) }, "job": func(r []any) { r[5] = "invalid" },
		"created": func(r []any) { r[3] = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }, "expires": func(r []any) { r[4] = time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC) },
		"accepted": func(r []any) { at := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC); r[6] = &at }, "missing-accepted": func(r []any) { r[6] = nil },
		"source": func(r []any) { r[7] = []string{"invalid"} }, "provenance": func(r []any) { r[8] = false }, "mapping": func(r []any) { r[9] = false },
		"phase": func(r []any) { r[11] = "invalid" }, "negative": func(r []any) { r[12] = int64(-1) }, "too-many": func(r []any) { r[12] = int64(7) },
		"job-version": func(r []any) { r[13] = int64(0) }, "reason": func(r []any) { r[14] = c.DeliveryReason("private text") },
		"fence": func(r []any) { r[15] = int64(-1) }, "missing-attempt": func(r []any) { r[17] = "" }, "wrong-job": func(r []any) { r[18] = "invalid" },
		"wrong-fence": func(r []any) { r[19] = int64(2) }, "protocol": func(r []any) { r[20] = "invalid" }, "channel": func(r []any) { s := "configured"; r[21] = &s },
		"result": func(r []any) { s := "ok"; r[22] = &s }, "not-closed": func(r []any) { r[20] = "data" }, "not-terminal": func(r []any) { r[23] = false }, "not-joined": func(r []any) { r[24] = false },
	}
	for name, mutate := range bad {
		for pos := range 3 {
			t.Run(name+"/"+strconv.Itoa(pos), func(t *testing.T) {
				result := "sent"
				v := invitationAttempt(t, &result)
				mutate(v)
				values := [][]any{invitationRow(t), invitationRow(t), invitationRow(t)}
				values[pos] = v
				rows := &invitationRows{values: values}
				out, e := f.httpScanInvitationList(context.Background(), rows, 2)
				if !hasFaultCode(e, foundation.DependencyUnavailable) || out.Items != nil || out.NextCursor != "" || rows.closed != 1 {
					t.Fatal("bad row leaked candidate", e, rows.closed)
				}
			})
		}
	}
	for _, mode := range []string{"scan", "rows", "after-close", "cancel", "cursor"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rows := &invitationRows{values: [][]any{invitationRow(t), invitationRow(t)}}
			facade := f
			switch mode {
			case "scan":
				rows.scanError = errors.New("private scan")
			case "rows":
				rows.rowError = errors.New("private rows")
			case "after-close":
				rows.closeError = errors.New("private close")
			case "cancel":
				rows.onClose = cancel
			case "cursor":
				facade = &SystemHTTPFacade{}
			}
			out, e := facade.httpScanInvitationList(ctx, rows, 1)
			if e == nil || out.Items != nil || out.NextCursor != "" || rows.closed != 1 {
				t.Fatal("failed scan leaked candidate", e)
			}
		})
	}
}

type invitationBudgetStore struct {
	directoryQueryFailureStore
	deadline            time.Time
	txCalls, queryCalls int
	result              foundation.CommitState
	queryHook           func(context.Context) error
}

func (s *invitationBudgetStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	s.txCalls++
	r := s.directoryQueryFailureStore.WithinTx(ctx, cause, fn)
	s.result = r.State()
	return r
}
func (s *invitationBudgetStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		panic("invitation left authorized transaction")
	}
	return s, nil
}
func (s *invitationBudgetStore) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	s.queryCalls++
	s.deadline, _ = ctx.Deadline()
	s.query, s.args = query, args
	if s.queryHook != nil {
		return nil, s.queryHook(ctx)
	}
	return nil, errors.New("owned query failure")
}
func TestHTTPInvitationDeliveryLocalBudgetAndLockUnion(t *testing.T) {
	for _, budget := range []time.Duration{time.Second, 10 * time.Second} {
		actor := httpTestActor(t)
		store := &invitationBudgetStore{directoryQueryFailureStore: directoryQueryFailureStore{actor: actor}}
		authority, e := NewAuthority(store, testKeys(t))
		if e != nil {
			t.Fatal(e)
		}
		state := &serviceState{store: store, deps: Dependencies{Authority: authority}, operations: map[*operation]bool{}, changed: make(chan struct{})}
		ring, _, _ := testRings(t)
		f := &SystemHTTPFacade{core: &Service{data: func() *serviceState { return state }}, pagination: ring}
		ctx, cancel := context.WithTimeout(context.Background(), budget)
		before := time.Now()
		out, e := f.ListInvitations(ctx, actor, HTTPListRequest{Limit: 1})
		cancel()
		want := budget
		if want > 3*time.Second {
			want = 3 * time.Second
		}
		if !hasFaultCode(e, foundation.DependencyUnavailable) || out.Items != nil || out.NextCursor != "" || store.checks != 1 || len(store.locks) != 3 || len(state.operations) != 0 || store.deadline.Before(before.Add(want-20*time.Millisecond)) || store.deadline.After(before.Add(want+20*time.Millisecond)) {
			t.Fatal("local budget/current authority/lock union", e)
		}
		if !reflect.DeepEqual(store.args, []any{nil, nil, 2}) || strings.Count(store.query, "clock_timestamp()") != 1 || strings.Contains(store.query, "smtp_settings") || strings.Contains(store.query, "token_verifier") {
			t.Fatal("statement boundary or unsafe projection")
		}
	}
}

func TestHTTPInvitationDeliveryDeadlineCancellationAndJoin(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		parent time.Duration
	}{{"local-three-seconds", 10 * time.Second}, {"earlier-parent", 75 * time.Millisecond}} {
		t.Run(scenario.name, func(t *testing.T) {
			actor := httpTestActor(t)
			store := &invitationBudgetStore{directoryQueryFailureStore: directoryQueryFailureStore{actor: actor}}
			authority, e := NewAuthority(store, testKeys(t))
			if e != nil {
				t.Fatal(e)
			}
			state := &serviceState{store: store, deps: Dependencies{Authority: authority}, operations: map[*operation]bool{}, changed: make(chan struct{})}
			f := &SystemHTTPFacade{core: &Service{data: func() *serviceState { return state }}}
			entered := make(chan context.Context, 1)
			expired := make(chan error, 1)
			release := make(chan struct{})
			store.queryHook = func(ctx context.Context) error {
				entered <- ctx
				<-ctx.Done()
				expired <- ctx.Err()
				<-release // A cancellation signal alone does not join the caller.
				return ctx.Err()
			}
			type response struct {
				page HTTPInvitationList
				err  error
			}
			responses := make(chan response, 1)
			joined := make(chan struct{})
			parent, cancel := context.WithTimeout(context.Background(), scenario.parent)
			parentDeadline, _ := parent.Deadline()
			released := false
			t.Cleanup(func() {
				cancel()
				if !released {
					close(release)
				}
				select {
				case <-joined:
				case <-time.After(time.Second):
					t.Error("facade did not actually join after owned tail release")
				}
			})
			started := time.Now()
			go func() {
				v, e := f.ListInvitations(parent, actor, HTTPListRequest{})
				responses <- response{v, e}
				close(joined)
			}()
			var queryContext context.Context
			select {
			case queryContext = <-entered:
			case <-time.After(time.Second):
				t.Fatal("owned query did not start")
			}
			queryAt := time.Now()
			deadline, ok := queryContext.Deadline()
			if !ok {
				t.Fatal("missing query deadline")
			}
			if scenario.parent < 3*time.Second {
				if !deadline.Equal(parentDeadline) {
					t.Fatal("earlier parent deadline was reset")
				}
			} else if deadline.Before(started.Add(3*time.Second)) || deadline.After(queryAt.Add(3*time.Second)) || !deadline.Before(parentDeadline) {
				t.Fatal("local deadline not measured from facade entry")
			}
			select {
			case cause := <-expired:
				if cause != context.DeadlineExceeded || queryContext.Err() != context.DeadlineExceeded {
					t.Fatal("original context did not actually expire", cause)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("original deadline never cancelled the query")
			}
			elapsed := time.Since(started)
			if scenario.parent > 3*time.Second && parent.Err() != nil {
				t.Fatal("local cancellation consumed the longer parent")
			}
			state.mu.Lock()
			active := len(state.operations)
			state.mu.Unlock()
			if active != 1 {
				t.Fatal("cancellation falsely finished the live operation")
			}
			select {
			case <-joined:
				t.Fatal("facade returned before its original query tail joined")
			case <-time.After(20 * time.Millisecond):
			}
			close(release)
			released = true
			select {
			case <-joined:
			case <-time.After(time.Second):
				t.Fatal("released query did not join")
			}
			got := <-responses
			var fault *foundation.Fault
			if !errors.As(got.err, &fault) || fault.Code != foundation.DependencyUnavailable || fault.CommitState != foundation.NotCommitted || !errors.Is(got.err, context.DeadlineExceeded) || got.page.Items != nil || got.page.NextCursor != "" {
				t.Fatal("deadline lost its original cause/state or published candidates", got.err)
			}
			if len(state.operations) != 0 || store.txCalls != 1 || store.queryCalls != 1 || store.result != foundation.NotCommitted {
				t.Fatal("read retained its operation or started another transaction/query")
			}
			t.Logf("original query context expired after %s; blocked tail then joined; NotCommitted, zero candidates, one transaction/query", elapsed)
		})
	}
}
