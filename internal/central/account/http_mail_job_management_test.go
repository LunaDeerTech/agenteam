package account

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// These rows exercise mapping only. Formal producers and pgx codecs are covered
// separately by the owned PostgreSQL and delivery-worker tests.
func mailManagementRow(t *testing.T) []any {
	t.Helper()
	id, e := foundation.NewID[c.MailJob]()
	if e != nil {
		t.Fatal(e)
	}
	configured := false
	return []any{id.String(), c.TestDelivery, time.Date(2026, 10, 6, 1, 2, 3, 456789000, time.UTC), true, false, "enqueue_pending", int64(0), int64(1), c.DeliveryReason(""), int64(0), "", "", "", int64(0), "", nil, nil, false, false, &configured}
}
func mailManagementAttempt(t *testing.T, phase, channel, result string) []any {
	t.Helper()
	row := mailManagementRow(t)
	id, e := foundation.NewID[struct{}]()
	if e != nil {
		t.Fatal(e)
	}
	row[4], row[5], row[6], row[7], row[9] = true, phase, int64(1), int64(2), int64(1)
	row[10], row[11], row[12], row[13], row[14], row[15] = id.String(), id.String(), row[0], int64(1), "negotiation", &channel
	if result != "" {
		row[14], row[16], row[17], row[18] = "closed", &result, true, true
	}
	return row
}
func scanMailManagementRow(row []any) (HTTPMailJobManagement, error) {
	return httpScanMailJobManagement(&invitationRows{values: [][]any{row}, index: 1})
}
func TestHTTPMailJobManagementFacts(t *testing.T) {
	for _, kind := range []c.DeliveryKind{c.InvitationDelivery, c.ResetDelivery, c.TestDelivery} {
		for _, phase := range []string{"enqueue_pending", "pending", "processing", "unknown", "cancelled"} {
			for _, configured := range []bool{false, true} {
				row := mailManagementRow(t)
				row[1], row[5], row[19] = kind, phase, &configured
				row[4] = phase != "enqueue_pending"
				item, e := scanMailManagementRow(row)
				want := phase
				if want == "processing" {
					want = "unknown"
				}
				channel := "backend_log"
				if configured {
					channel = "smtp"
				}
				if e != nil || item.Kind != kind || item.Status.Phase != want || item.Channel != channel || item.AttemptChannel != nil || item.AttemptResult != nil || item.Status.Attempts != 0 || item.Status.Version != 1 || item.CreatedAt.String() != "2026-10-06T01:02:03.456789Z" {
					t.Fatal("missing-attempt facts", e)
				}
			}
		}
	}
	for _, channel := range []string{"smtp", "log"} {
		for _, result := range []string{"sent", "failed", "unknown", "cancelled"} {
			row := mailManagementAttempt(t, result, channel, result)
			item, e := scanMailManagementRow(row)
			want := channel
			if want == "log" {
				want = "backend_log"
			}
			if e != nil || item.AttemptChannel == nil || *item.AttemptChannel != want || item.Channel != want || item.AttemptResult == nil || string(*item.AttemptResult) != result {
				t.Fatal("actual terminal facts", e)
			}
		}
	}
	// A joined I/O tail may precede terminal publication. It is not itself a result.
	for _, protocol := range []string{"negotiation", "auth", "envelope", "data", "awaiting_acceptance"} {
		for _, joined := range []bool{false, true} {
			row := mailManagementAttempt(t, "sending", "smtp", "")
			row[14], row[18] = protocol, joined
			item, e := scanMailManagementRow(row)
			if e != nil || item.AttemptResult != nil || item.AttemptChannel == nil {
				t.Fatal("nonterminal joined shape", e)
			}
		}
	}
	row := mailManagementAttempt(t, "retry_wait", "smtp", "unknown")
	item, e := scanMailManagementRow(row)
	if e != nil || item.Status.Phase != "retry_wait" || *item.AttemptResult != c.DeliveryUnknown {
		t.Fatal("retry wait lost previous unknown", e)
	}
}
func TestHTTPMailJobManagementRejectsBadFactsAndSentinel(t *testing.T) {
	bad := []struct {
		name   string
		active bool
		change func([]any)
	}{
		{"job-id", false, func(r []any) { r[0] = "not-an-id" }},
		{"kind", false, func(r []any) { r[1] = c.DeliveryKind("other") }},
		{"created", false, func(r []any) { r[2] = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{"mapping", false, func(r []any) { r[3] = false }},
		{"missing-config", false, func(r []any) { r[19] = nil }},
		{"attempts-negative", false, func(r []any) { r[6] = int64(-1) }},
		{"attempts-seven", true, func(r []any) { r[6] = int64(7) }},
		{"version-zero", false, func(r []any) { r[7] = int64(0) }},
		{"reason", false, func(r []any) { r[8] = c.DeliveryReason("private diagnostic") }},
		{"fence-negative", false, func(r []any) { r[9] = int64(-1) }},
		{"absent-job-phase", false, func(r []any) { r[5] = "pending" }},
		{"absent-job-attempts", false, func(r []any) { r[6] = int64(1) }},
		{"absent-job-version", false, func(r []any) { r[7] = int64(2) }},
		{"absent-job-reason", false, func(r []any) { r[8] = c.ReasonSent }},
		{"absent-job-fence", false, func(r []any) { r[9] = int64(1) }},
		{"existing-enqueue", false, func(r []any) { r[4] = true }},
		{"phase", true, func(r []any) { r[5] = "made-up" }},
		{"pointer-id", true, func(r []any) { r[10] = "wrong" }},
		{"pointer-missing-row", true, func(r []any) { r[11] = "" }},
		{"attempt-job", true, func(r []any) { r[12] = r[11] }},
		{"attempt-fence", true, func(r []any) { r[13] = int64(2) }},
		{"no-claims", true, func(r []any) { r[6] = int64(0) }},
		{"zero-fence", true, func(r []any) { r[9], r[13] = int64(0), int64(0) }},
		{"channel-null", true, func(r []any) { r[15] = nil }},
		{"channel-enum", true, func(r []any) { v := "backend_log"; r[15] = &v }},
		{"protocol", true, func(r []any) { r[14] = "done" }},
		{"closed-without-result", true, func(r []any) { r[14] = "closed" }},
		{"terminal-without-result", true, func(r []any) { r[17] = true }},
		{"result-not-closed", true, func(r []any) { v := "sent"; r[16], r[17], r[18] = &v, true, true }},
		{"result-not-terminal", true, func(r []any) { v := "sent"; r[14], r[16], r[18] = "closed", &v, true }},
		{"result-not-joined", true, func(r []any) { v := "sent"; r[14], r[16], r[17] = "closed", &v, true }},
		{"result-enum", true, func(r []any) { v := "retry_wait"; r[14], r[16], r[17], r[18] = "closed", &v, true, true }},
	}
	for _, phase := range []string{"claimed", "sending", "retry_wait", "sent", "failed"} {
		bad = append(bad, struct {
			name   string
			active bool
			change func([]any)
		}{"no-current-" + phase, false, func(r []any) { r[4], r[5] = true, phase }})
	}
	for _, column := range []int{11, 12, 13, 14, 15, 16, 17, 18} {
		bad = append(bad, struct {
			name   string
			active bool
			change func([]any)
		}{"orphan-column-" + string(rune('a'+column)), false, func(r []any) { a := mailManagementAttempt(t, "sent", "smtp", "sent"); r[column] = a[column] }})
	}
	ring, _, _ := testRings(t)
	f := &SystemHTTPFacade{pagination: ring}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			row := mailManagementRow(t)
			if tc.active {
				row = mailManagementAttempt(t, "sending", "smtp", "")
			}
			tc.change(row)
			for _, offset := range []int{0, 1, 2} {
				values := [][]any{mailManagementRow(t), mailManagementRow(t), mailManagementRow(t)}
				values[offset] = row
				rows := &invitationRows{values: values}
				out, e := f.httpScanMailJobManagementList(context.Background(), rows, 2)
				if e == nil || out.Items != nil || out.NextCursor != "" || rows.closed != 1 {
					t.Fatal("bad first/middle/lookahead row escaped", offset, e)
				}
			}
		})
	}
}
func TestHTTPMailJobManagementScanCloseCancelAndCursor(t *testing.T) {
	ring, _, _ := testRings(t)
	f := &SystemHTTPFacade{pagination: ring}
	for _, mode := range []string{"scan", "rows", "close", "cancel-at-close", "cursor"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rows := &invitationRows{values: [][]any{mailManagementRow(t), mailManagementRow(t)}}
			facade := f
			switch mode {
			case "scan":
				rows.scanError = errors.New("private scan")
			case "rows":
				rows.rowError = errors.New("private rows")
			case "close":
				rows.closeError = errors.New("private close")
			case "cancel-at-close":
				rows.onClose = cancel
			case "cursor":
				facade = &SystemHTTPFacade{}
			}
			out, e := facade.httpScanMailJobManagementList(ctx, rows, 1)
			if e == nil || out.Items != nil || out.NextCursor != "" || rows.closed != 1 {
				t.Fatal("failed scan retained candidates", e)
			}
		})
	}
	rows := &invitationRows{}
	out, e := f.httpScanMailJobManagementList(context.Background(), rows, 25)
	if e != nil || out.Items == nil || len(out.Items) != 0 || out.NextCursor != "" || rows.closed != 1 {
		t.Fatal("empty list", e)
	}
	first, second := mailManagementRow(t), mailManagementRow(t)
	rows = &invitationRows{values: [][]any{first, second}}
	out, e = f.httpScanMailJobManagementList(context.Background(), rows, 1)
	if e != nil || len(out.Items) != 1 || out.NextCursor == "" || rows.index != 2 || rows.closed != 1 {
		t.Fatal("valid sentinel", e)
	}
	for _, limit := range []int{0, 1, 100} {
		n, at, id, e := f.httpMailJobManagementPosition(HTTPListRequest{Limit: limit, Cursor: out.NextCursor})
		want := limit
		if want == 0 {
			want = 25
		}
		if e != nil || n != want || id != first[0] || !at.Equal(first[2].(time.Time)) {
			t.Fatal("cursor was not last returned item", e)
		}
	}
	for _, resource := range []string{"mail-jobs", "users", "invitations"} {
		old, e := f.httpNextCursor(resource, first[2].(time.Time), first[0].(string))
		if e != nil {
			t.Fatal(e)
		}
		if _, _, _, e = f.httpMailJobManagementPosition(HTTPListRequest{Cursor: old}); !hasFaultCode(e, foundation.CursorInvalid) {
			t.Fatal("old cursor admitted", resource, e)
		}
		if _, _, _, e = f.httpListPosition(resource, HTTPListRequest{Cursor: out.NextCursor}); !hasFaultCode(e, foundation.CursorInvalid) {
			t.Fatal("new cursor escaped to old domain", resource, e)
		}
	}
	for _, token := range []string{out.NextCursor + "x", strings.Repeat("x", 8193)} {
		if _, _, _, e = f.httpMailJobManagementPosition(HTTPListRequest{Cursor: token}); !hasFaultCode(e, foundation.CursorInvalid) {
			t.Fatal("invalid cursor accepted", e)
		}
	}
	binding, _ := httpMailJobManagementBinding()
	at, _ := cursor.Instant(out.Items[0].CreatedAt)
	id, _ := cursor.UUID(first[0].(string))
	generation := int64(1)
	for _, position := range []cursor.Position{{Scalars: []cursor.Scalar{id, at}}, {Scalars: []cursor.Scalar{at}}, {Scalars: []cursor.Scalar{at, id}, OrderGeneration: &generation}} {
		token, e := ring.Sign(binding, position)
		if e != nil {
			t.Fatal(e)
		}
		if _, _, _, e = f.httpMailJobManagementPosition(HTTPListRequest{Cursor: token}); !hasFaultCode(e, foundation.CursorInvalid) {
			t.Fatal("invalid position accepted", e)
		}
	}
	for _, limit := range []int{-1, 101} {
		if _, _, _, e = f.httpMailJobManagementPosition(HTTPListRequest{Limit: limit}); e == nil {
			t.Fatal("invalid limit accepted")
		}
	}
	if _, e := httpScanMailJobManagement(directoryScanFunc(func(...any) error { return pgx.ErrNoRows })); !hasFaultCode(e, foundation.NotFound) {
		t.Fatal("absent detail", e)
	}
}

// A transaction/authority seam returning real safe projections, never simulated
// SMTP, PG locking or HTTP receipts. Hooks retain the original context and tail.
type mailManagementStore struct {
	directoryQueryFailureStore
	row                                         []any
	authHook                                    func(context.Context) error
	queryHook                                   func(context.Context) error
	detailHook                                  func(context.Context) error
	after                                       func()
	unknown                                     bool
	txCalls, queryCalls, authCalls, detailCalls int
	deadline                                    time.Time
}

func (s *mailManagementStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		panic("foreign management tx")
	}
	return s, nil
}
func (s *mailManagementStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	s.txCalls++
	s.tx = foundation.NewTx()
	e := fn(ctx, s.tx)
	if s.after != nil {
		s.after()
	}
	if s.unknown {
		id, err := foundation.NewID[foundation.TransactionAttempt]()
		if err != nil {
			panic(err)
		}
		return foundation.UnknownResult(id, cause)
	}
	if e == nil {
		return foundation.CommittedResult()
	}
	var f *foundation.Fault
	if !errors.As(e, &f) {
		panic("unmapped test error")
	}
	return foundation.NotCommittedResult(f)
}
func (s *mailManagementStore) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	s.queryCalls++
	s.query, s.args = query, args
	s.deadline, _ = ctx.Deadline()
	if s.queryHook != nil {
		return nil, s.queryHook(ctx)
	}
	return nil, errors.New("owned management query failure")
}
func (s *mailManagementStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.Contains(query, "WHERE token_verifier=") {
		return directoryScanFunc(func(dest ...any) error {
			s.authCalls++
			if s.authHook != nil {
				return s.authHook(ctx)
			}
			*dest[0].(*string), *dest[1].(*string) = s.actor.Details().UserID, s.actor.Details().SessionID
			return nil
		})
	}
	if query == accountHTTPMailManagementDetailSQL {
		return directoryScanFunc(func(dest ...any) error {
			s.detailCalls++
			s.deadline, _ = ctx.Deadline()
			if s.detailHook != nil {
				if e := s.detailHook(ctx); e != nil {
					return e
				}
			}
			if s.row == nil {
				return pgx.ErrNoRows
			}
			return (&invitationRows{values: [][]any{s.row}, index: 1}).Scan(dest...)
		})
	}
	return s.directoryQueryFailureStore.QueryRow(ctx, query, args...)
}
func mailManagementFacade(t *testing.T) (*SystemHTTPFacade, *mailManagementStore, *serviceState) {
	t.Helper()
	actor := httpTestActor(t)
	s := &mailManagementStore{directoryQueryFailureStore: directoryQueryFailureStore{actor: actor}}
	keys := testKeys(t)
	authority, e := NewAuthority(s, keys)
	if e != nil {
		t.Fatal(e)
	}
	st := &serviceState{store: s, keys: keys, deps: Dependencies{Authority: authority}, operations: map[*operation]bool{}, changed: make(chan struct{})}
	ring, _, _ := testRings(t)
	f := &SystemHTTPFacade{core: &Service{data: func() *serviceState { return st }}, pagination: ring}
	return f, s, st
}
func TestHTTPMailJobManagementTransactionZeroAndBudget(t *testing.T) {
	for _, mode := range []string{"unknown-detail", "cancel-after-detail", "wrong-detail-id", "missing-detail", "query", "unknown-list"} {
		t.Run(mode, func(t *testing.T) {
			f, s, st := mailManagementFacade(t)
			s.row = mailManagementRow(t)
			id, _ := foundation.ParseID[c.MailJob](s.row[0].(string))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "unknown-detail", "unknown-list":
				s.unknown = true
			case "cancel-after-detail":
				s.after = cancel
			case "wrong-detail-id":
				s.row[0] = mailManagementRow(t)[0]
			case "missing-detail":
				s.row = nil
			}
			var e error
			if strings.Contains(mode, "list") || mode == "query" {
				var out HTTPMailJobManagementList
				out, e = f.ListMailJobManagement(ctx, s.actor, HTTPListRequest{Limit: 1})
				if out.Items != nil || out.NextCursor != "" {
					t.Fatal("list published failed candidate")
				}
			} else {
				var out HTTPMailJobManagement
				out, e = f.GetMailJobManagement(ctx, s.actor, id)
				if !reflect.DeepEqual(out, HTTPMailJobManagement{}) {
					t.Fatal("detail published failed candidate")
				}
			}
			if e == nil || len(st.operations) != 0 || s.txCalls != 1 || s.checks != 1 || len(s.locks) != 2 {
				t.Fatal("authority/lock/zero-result boundary", e)
			}
			for _, lock := range s.locks {
				if lock.Mode != foundation.Shared {
					t.Fatal("write lock in read")
				}
			}
			if strings.HasPrefix(mode, "unknown") {
				var fault *foundation.Fault
				if !errors.As(e, &fault) || fault.CommitState != foundation.Unknown || fault.Code != foundation.CommitUnknown {
					t.Fatal("Unknown result misclassified", e)
				}
			}
		})
	}
	for _, parent := range []time.Duration{time.Second, 10 * time.Second} {
		f, s, _ := mailManagementFacade(t)
		ctx, cancel := context.WithTimeout(context.Background(), parent)
		parentDeadline, _ := ctx.Deadline()
		start := time.Now()
		_, e := f.ListMailJobManagement(ctx, s.actor, HTTPListRequest{Limit: 1})
		end := time.Now()
		cancel()
		if e == nil || s.queryCalls != 1 || !reflect.DeepEqual(s.args, []any{nil, nil, 2}) {
			t.Fatal("bounded query", e)
		}
		if parent < 3*time.Second {
			if !s.deadline.Equal(parentDeadline) {
				t.Fatal("parent extended")
			}
		} else if s.deadline.Before(start.Add(3*time.Second)) || s.deadline.After(end.Add(3*time.Second)) {
			t.Fatal("facade budget not three seconds")
		}
	}
}
func TestHTTPMailJobManagementPreauthenticationDeadlineAndJoin(t *testing.T) {
	for _, endpoint := range []string{"/system/mail-jobs/management", "/system/mail-jobs/0199b4d2-5c00-7000-8000-000000000001/management"} {
		for _, duration := range []time.Duration{10 * time.Second, 75 * time.Millisecond} {
			t.Run(endpoint+"/"+duration.String(), func(t *testing.T) {
				f, s, st := mailManagementFacade(t)
				boundary, e := csrfNewBoundary("https://example.test")
				if e != nil {
					t.Fatal(e)
				}
				h := (&accountHTTP{core: f.core, system: f, csrf: boundary}).httpHandler()
				entered := make(chan context.Context, 1)
				expired := make(chan error, 1)
				release := make(chan struct{})
				joined := make(chan struct{})
				released := false
				s.authHook = func(ctx context.Context) error {
					entered <- ctx
					<-ctx.Done()
					expired <- ctx.Err()
					<-release
					return ctx.Err()
				}
				parent, cancel := context.WithTimeout(context.Background(), duration)
				parentDeadline, _ := parent.Deadline()
				r := httptest.NewRequest(http.MethodGet, "https://example.test/api/v1"+endpoint, nil).WithContext(parent)
				r.AddCookie(&http.Cookie{Name: boundary.sessionName, Value: base64.RawURLEncoding.EncodeToString(make([]byte, 32))})
				w := httptest.NewRecorder()
				t.Cleanup(func() {
					cancel()
					if !released {
						close(release)
					}
					select {
					case <-joined:
					case <-time.After(time.Second):
						t.Error("HTTP authentication tail did not join")
					}
				})
				start := time.Now()
				go func() { h.ServeHTTP(w, r); close(joined) }()
				var observed context.Context
				select {
				case observed = <-entered:
				case <-time.After(time.Second):
					t.Fatal("authentication did not begin")
				}
				entry := time.Now()
				deadline, ok := observed.Deadline()
				if !ok {
					t.Fatal("authentication lacks deadline")
				}
				if duration < 3*time.Second {
					if !deadline.Equal(parentDeadline) {
						t.Fatal("HTTP parent deadline reset")
					}
				} else if deadline.Before(start.Add(3*time.Second)) || deadline.After(entry.Add(3*time.Second)) || !deadline.Before(parentDeadline) {
					t.Fatal("HTTP local deadline not from dispatch")
				}
				select {
				case cause := <-expired:
					if cause != context.DeadlineExceeded || observed.Err() != cause {
						t.Fatal("original context did not expire")
					}
				case <-time.After(4 * time.Second):
					t.Fatal("authentication did not expire")
				}
				if duration > 3*time.Second && parent.Err() != nil {
					t.Fatal("local timeout cancelled longer parent")
				}
				st.mu.Lock()
				active := len(st.operations)
				st.mu.Unlock()
				if active != 1 {
					t.Fatal("operation disappeared before actual join")
				}
				select {
				case <-joined:
					t.Fatal("HTTP returned before authentication tail joined")
				case <-time.After(20 * time.Millisecond):
				}
				close(release)
				released = true
				select {
				case <-joined:
				case <-time.After(time.Second):
					t.Fatal("released HTTP did not join")
				}
				if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "items") || strings.Contains(w.Body.String(), "job_id") || strings.Contains(w.Body.String(), "context deadline") || len(st.operations) != 0 || s.authCalls != 1 || s.txCalls != 0 || s.queryCalls != 0 || s.detailCalls != 0 {
					t.Fatal("preauthentication cancellation published data or retried", w.Code)
				}
				httpAssertSecurityHeaders(t, w)
				t.Logf("original authentication context expired; blocked tail joined; elapsed=%s, no read transaction", time.Since(start))
			})
		}
	}
}

type mailManagementEncodingCancel struct{ cancel context.CancelFunc }

func (v mailManagementEncodingCancel) MarshalJSON() ([]byte, error) {
	v.cancel()
	return []byte(`{"candidate":"must-not-write"}`), nil
}
func TestHTTPMailJobManagementWireRoutesAndEncoding(t *testing.T) {
	row := mailManagementRow(t)
	item, e := scanMailManagementRow(row)
	if e != nil {
		t.Fatal(e)
	}
	encoded, e := httpEncode(httpMailJobManagementDTO(item))
	if e != nil {
		t.Fatal(e)
	}
	var wire map[string]any
	if json.Unmarshal(encoded, &wire) != nil || len(wire) != 9 || wire["attempt_channel"] != nil || wire["attempt_result"] != nil || wire["attempts"] != "0" || wire["version"] != "1" {
		t.Fatal("new flat required/nullable shape")
	}
	if _, present := wire["reason"]; present {
		t.Fatal("empty reason emitted")
	}
	item.Status.Reason = c.DeliveryReason("unknown")
	encoded, e = httpEncode(httpMailJobManagementDTO(item))
	if e != nil || json.Unmarshal(encoded, &wire) != nil || len(wire) != 10 || wire["reason"] != "unknown" {
		t.Fatal("optional reason missing")
	}
	old, e := httpEncode(httpMailJobDTO(item.HTTPMailJob))
	if e != nil {
		t.Fatal(e)
	}
	var legacy map[string]any
	if json.Unmarshal(old, &legacy) != nil || len(legacy) != 7 {
		t.Fatal("legacy DTO extended")
	}
	for _, key := range []string{"kind", "attempt_channel", "attempt_result"} {
		if _, ok := legacy[key]; ok {
			t.Fatal("new field escaped legacy DTO")
		}
	}
	boundary, _ := csrfNewBoundary("https://example.test")
	h := &accountHTTP{csrf: boundary}
	for _, endpoint := range []string{"/system/mail-jobs/management", "/system/mail-jobs/0199b4d2-5c00-7000-8000-000000000001/management"} {
		for _, method := range []string{"HEAD", "POST", "PUT", "DELETE", "OPTIONS"} {
			r := httptest.NewRequest(method, "https://example.test/api/v1"+endpoint, nil)
			r.Header.Set("Origin", "https://example.test")
			w := httptest.NewRecorder()
			h.httpHandler().ServeHTTP(w, r)
			if w.Code != 405 || w.Header().Get("Allow") != "GET" {
				t.Fatal("GET-only method boundary", endpoint, method, w.Code)
			}
			httpAssertSecurityHeaders(t, w)
		}
	}
	routes := h.httpRoutes()
	bounded := 0
	for _, route := range routes {
		if httpMailJobManagementRoute(route) {
			bounded++
		}
		if route.path == "/system/mail-jobs/management" {
			if route.method != "GET" || route.authority != "admin" || route.command {
				t.Fatal("wrong collection route")
			}
		}
	}
	if bounded != 2 || httpMailJobManagementRoute(accountHTTPRoute{method: "HEAD", path: "/system/mail-jobs/management"}) {
		t.Fatal("budget escaped exact GETs")
	}
	// The collection is allowed list query syntax; detail remains query-free.
	for _, query := range []string{"limit=1", "cursor=valid&limit=25"} {
		called := false
		r := httptest.NewRequest("GET", "https://example.test/api/v1/system/mail-jobs/management?"+query, nil)
		w := httptest.NewRecorder()
		h.httpDispatch(w, r, accountHTTPRoute{method: "GET", path: "/system/mail-jobs/management", serve: func(_ http.ResponseWriter, r *http.Request, _ httpRequest) {
			called = true
			if _, e := httpListQuery(r); e != nil {
				t.Fatal(e)
			}
		}})
		if !called {
			t.Fatal("literal management list treated as detail")
		}
	}
	for _, query := range []string{"?limit=1", "?"} {
		r := httptest.NewRequest("GET", "https://example.test/api/v1/system/mail-jobs/id/management"+query, nil)
		w := httptest.NewRecorder()
		h.httpDispatch(w, r, accountHTTPRoute{method: "GET", path: "/system/mail-jobs/{id}/management", serve: func(http.ResponseWriter, *http.Request, httpRequest) { t.Fatal("detail query admitted") }})
		if w.Code != 400 {
			t.Fatal("detail query did not fail")
		}
	}
	for _, path := range []string{"/system/mail-jobs/management", "/system/mail-jobs/id/management"} {
		r := httptest.NewRequest("GET", "https://example.test"+path, strings.NewReader("{}"))
		w := httptest.NewRecorder()
		if strings.Contains(path, "/id/") {
			h.httpGetMailJobManagement(w, r, httpRequest{})
		} else {
			h.httpListMailJobManagement(w, r, httpRequest{})
		}
		if w.Code != 400 {
			t.Fatal("GET body admitted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", "https://example.test/", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	httpMailJobManagementJSON(w, r, mailManagementEncodingCancel{cancel})
	if w.Code != 503 || strings.Contains(w.Body.String(), "must-not-write") {
		t.Fatal("encoding cancellation published candidate")
	}
}
func TestHTTPMailJobManagementOpenAPIClosedSchemas(t *testing.T) {
	raw, e := os.ReadFile("../../../api/openapi/account.json")
	if e != nil {
		t.Fatal(e)
	}
	var document struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if json.Unmarshal(raw, &document) != nil {
		t.Fatal("schema JSON")
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
		Additional bool                       `json:"additionalProperties"`
	}
	if json.Unmarshal(document.Components.Schemas["MailJobManagement"], &schema) != nil || len(schema.Properties) != 10 || len(schema.Required) != 9 || schema.Additional {
		t.Fatal("closed management schema")
	}
	for _, key := range []string{"job_id", "kind", "phase", "attempts", "version", "created_at", "channel", "attempt_channel", "attempt_result"} {
		found := false
		for _, required := range schema.Required {
			found = found || key == required
		}
		if !found {
			t.Fatal("missing required", key)
		}
	}
	for _, key := range []string{"attempt_channel", "attempt_result"} {
		var field struct {
			Type []string `json:"type"`
			Enum []any    `json:"enum"`
		}
		if json.Unmarshal(schema.Properties[key], &field) != nil || !reflect.DeepEqual(field.Type, []string{"string", "null"}) || field.Enum[len(field.Enum)-1] != nil {
			t.Fatal("required nullable schema", key)
		}
	}
	for name, want := range map[string]int{"MailJob": 7, "JobAccepted": 1, "RetryAccepted": 2, "InvitationDelivery": 8} {
		var old struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Additional bool                       `json:"additionalProperties"`
		}
		if json.Unmarshal(document.Components.Schemas[name], &old) != nil || len(old.Properties) != want || old.Additional {
			t.Fatal("legacy schema shape changed", name, len(old.Properties))
		}
	}
}

func TestHTTPMailJobManagementFacadeCancelledTailJoins(t *testing.T) {
	for _, detail := range []bool{false, true} {
		t.Run(map[bool]string{false: "list", true: "detail"}[detail], func(t *testing.T) {
			f, s, st := mailManagementFacade(t)
			s.row = mailManagementRow(t)
			id, _ := foundation.ParseID[c.MailJob](s.row[0].(string))
			parent, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
			defer cancel()
			entered := make(chan context.Context, 1)
			expired := make(chan struct{})
			release := make(chan struct{})
			joined := make(chan struct{})
			released := false
			hook := func(ctx context.Context) error {
				entered <- ctx
				<-ctx.Done()
				close(expired)
				<-release
				return ctx.Err()
			}
			if detail {
				s.detailHook = hook
			} else {
				s.queryHook = hook
			}
			var got error
			var list HTTPMailJobManagementList
			var item HTTPMailJobManagement
			t.Cleanup(func() {
				cancel()
				if !released {
					close(release)
				}
				select {
				case <-joined:
				case <-time.After(time.Second):
					t.Error("facade tail did not join")
				}
			})
			go func() {
				if detail {
					item, got = f.GetMailJobManagement(parent, s.actor, id)
				} else {
					list, got = f.ListMailJobManagement(parent, s.actor, HTTPListRequest{})
				}
				close(joined)
			}()
			var observed context.Context
			select {
			case observed = <-entered:
			case <-time.After(time.Second):
				t.Fatal("read did not begin")
			}
			deadline, _ := observed.Deadline()
			want, _ := parent.Deadline()
			if !deadline.Equal(want) {
				t.Fatal("facade reset earlier parent")
			}
			select {
			case <-expired:
			case <-time.After(time.Second):
				t.Fatal("original read context did not expire")
			}
			st.mu.Lock()
			active := len(st.operations)
			st.mu.Unlock()
			if active != 1 {
				t.Fatal("transaction falsely joined at cancellation")
			}
			select {
			case <-joined:
				t.Fatal("read returned before original tail joined")
			case <-time.After(20 * time.Millisecond):
			}
			close(release)
			released = true
			select {
			case <-joined:
			case <-time.After(time.Second):
				t.Fatal("tail release did not join")
			}
			var fault *foundation.Fault
			if !errors.As(got, &fault) || fault.Code != foundation.DependencyUnavailable || fault.CommitState != foundation.NotCommitted || !errors.Is(got, context.DeadlineExceeded) || list.Items != nil || list.NextCursor != "" || !reflect.DeepEqual(item, HTTPMailJobManagement{}) || len(st.operations) != 0 || s.txCalls != 1 || s.queryCalls+s.detailCalls != 1 {
				t.Fatal("cancelled read/transaction lost zero projection or actual join", got)
			}
		})
	}
}

func TestHTTPMailJobManagementAuthenticatedRoutingAndQueries(t *testing.T) {
	f, s, _ := mailManagementFacade(t)
	s.row = mailManagementRow(t)
	job := s.row[0].(string)
	boundary, _ := csrfNewBoundary("https://example.test")
	h := (&accountHTTP{core: f.core, system: f, csrf: boundary}).httpHandler()
	request := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "https://example.test/api/v1"+path, nil)
		r.AddCookie(&http.Cookie{Name: boundary.sessionName, Value: base64.RawURLEncoding.EncodeToString(make([]byte, 32))})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		httpAssertSecurityHeaders(t, w)
		return w
	}
	// An authenticated collection hits the new SQL, not legacy UUID parsing.
	w := request("/system/mail-jobs/management?limit=1")
	if w.Code != 503 || s.queryCalls != 1 || s.query != accountHTTPMailManagementListSQL || !reflect.DeepEqual(s.args, []any{nil, nil, 2}) {
		t.Fatal("literal management route missed collection", w.Code)
	}
	if w = request("/system/mail-jobs/" + job + "/management"); w.Code != 200 || s.detailCalls != 1 {
		t.Fatal("detail route missed", w.Code)
	}
	if w = request("/system/mail-jobs/not-an-id/management"); w.Code != 400 || s.detailCalls != 1 {
		t.Fatal("invalid ID reached query", w.Code)
	}
	s.row = nil
	if w = request("/system/mail-jobs/" + job + "/management"); w.Code != 404 || s.detailCalls != 2 {
		t.Fatal("missing intent not 404", w.Code)
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=01", "limit=1&limit=2", "cursor=x&cursor=y", "cursor=", "limit=", "filter=all", "cursor=" + strings.Repeat("x", 8193)} {
		w = request("/system/mail-jobs/management?" + query)
		if w.Code != 400 || s.queryCalls != 1 || strings.Contains(w.Body.String(), "items") {
			t.Fatal("invalid query reached SQL or leaked candidates", w.Code)
		}
	}
}
