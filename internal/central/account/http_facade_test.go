package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func TestB04HTTPFacadeCursorUsesRealRingAndClosedBinding(t *testing.T) {
	ring, _, _ := testRings(t)
	facade := &SystemHTTPFacade{pagination: ring}
	id, e := foundation.NewID[identity.User]()
	if e != nil {
		t.Fatal(e)
	}
	date := time.Date(2026, 10, 4, 1, 2, 3, 456789000, time.UTC)
	token, e := facade.httpNextCursor("users", date, id.String())
	if e != nil {
		t.Fatal(e)
	}
	for _, limit := range []int{0, 1, 25, 100} {
		n, gotDate, gotID, e := facade.httpListPosition("users", HTTPListRequest{Cursor: token, Limit: limit})
		want := limit
		if want == 0 {
			want = 25
		}
		if e != nil || n != want || !gotDate.Equal(date) || gotID != id.String() {
			t.Fatal("cursor round trip", n, e)
		}
	}
	if _, _, _, e = facade.httpListPosition("invitations", HTTPListRequest{Cursor: token}); !hasFaultCode(e, foundation.CursorInvalid) {
		t.Fatal("cross-resource cursor accepted")
	}
	if _, _, _, e = facade.httpListPosition("users", HTTPListRequest{Cursor: token + "x"}); !hasFaultCode(e, foundation.CursorInvalid) {
		t.Fatal("tampered cursor accepted")
	}
	binding, _ := httpListBinding("users")
	instantScalar, _ := cursor.Instant(instant(date))
	uuidScalar, _ := cursor.UUID(id.String())
	generation := int64(1)
	for _, position := range []cursor.Position{{Scalars: []cursor.Scalar{uuidScalar, instantScalar}}, {Scalars: []cursor.Scalar{instantScalar, uuidScalar}, OrderGeneration: &generation}, {Scalars: []cursor.Scalar{uuidScalar}}} {
		bad, e := ring.Sign(binding, position)
		if e != nil {
			t.Fatal(e)
		}
		if _, _, _, e = facade.httpListPosition("users", HTTPListRequest{Cursor: bad}); !hasFaultCode(e, foundation.CursorInvalid) {
			t.Fatal("bad cursor shape accepted")
		}
	}
	for _, limit := range []int{-1, 101} {
		if _, _, _, e = facade.httpListPosition("users", HTTPListRequest{Limit: limit}); e == nil {
			t.Fatal("limit accepted")
		}
	}
}
func TestB04HTTPListQueryRejectsAmbiguity(t *testing.T) {
	for _, query := range []string{"limit=0", "limit=101", "limit=01", "limit=1&limit=2", "cursor=a&cursor=b", "filter=admin", "limit=+1", "cursor=", "limit=%FF", "limit=1;other=2"} {
		r := httptest.NewRequest("GET", "https://example.test/?"+query, nil)
		if _, e := httpListQuery(r); e == nil {
			t.Fatal("accepted query", query)
		}
	}
	for _, query := range []string{"", "limit=100", "cursor=signed&limit=1"} {
		r := httptest.NewRequest("GET", "https://example.test/?"+query, nil)
		if _, e := httpListQuery(r); e != nil {
			t.Fatal(query, e)
		}
	}
}

// This store can only deny identity. It proves queries cannot reach a cursor or
// result source before current Session validation; it cannot simulate success.
type httpDenyStore struct {
	Store
	locks           []foundation.LockRequest
	checks, queries int
	tx              foundation.Tx
}

func (s *httpDenyStore) WithinTx(ctx context.Context, _ foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	s.tx = foundation.NewTx()
	e := fn(ctx, s.tx)
	var f *foundation.Fault
	if !errors.As(e, &f) {
		panic("expected current-session denial")
	}
	return foundation.NotCommittedResult(f)
}
func (s *httpDenyStore) AcquireAll(_ context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	if tx != s.tx {
		panic("wrong transaction")
	}
	s.locks = append([]foundation.LockRequest(nil), locks...)
	return nil
}
func (s *httpDenyStore) RequireHeldLocks(_ context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	if tx != s.tx {
		panic("wrong transaction")
	}
	for _, want := range locks {
		found := false
		for _, got := range s.locks {
			if got.Key.Canonical() == want.Key.Canonical() && (got.Mode == want.Mode || got.Mode == foundation.Exclusive) {
				found = true
			}
		}
		if !found {
			panic("missing current user lock")
		}
	}
	s.checks++
	return nil
}
func (s *httpDenyStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		panic("wrong transaction")
	}
	return s, nil
}

type httpMissingSessionRow struct{}

func (httpMissingSessionRow) Scan(...any) error { return pgx.ErrNoRows }
func (s *httpDenyStore) QueryRow(_ context.Context, query string, _ ...any) postgres.Row {
	if !strings.Contains(query, "FROM agenteam_account.sessions WHERE id=") {
		panic("query before current identity")
	}
	s.queries++
	return httpMissingSessionRow{}
}
func TestB04HTTPFacadeEveryPageRequiresCurrentSession(t *testing.T) {
	store := &httpDenyStore{}
	authority, e := NewAuthority(store, testKeys(t))
	if e != nil {
		t.Fatal(e)
	}
	st := &serviceState{store: store, deps: Dependencies{Authority: authority}, operations: map[*operation]bool{}, changed: make(chan struct{})}
	core := &Service{data: func() *serviceState { return st }}
	facade := &SystemHTTPFacade{core: core}
	actor := httpTestActor(t)
	for _, call := range []func() error{
		func() error {
			_, e := facade.ListUsers(context.Background(), actor, HTTPListRequest{Cursor: "invalid-token"})
			return e
		},
		func() error {
			_, e := facade.ListInvitations(context.Background(), actor, HTTPListRequest{Cursor: "invalid-token"})
			return e
		},
		func() error {
			_, e := facade.ListMailJobs(context.Background(), actor, HTTPListRequest{Cursor: "invalid-token"})
			return e
		},
		func() error { _, e := facade.GetAccountSettings(context.Background(), actor); return e },
		func() error { _, e := facade.GetMailJob(context.Background(), actor, c.JobID{}); return e },
	} {
		if e := call(); !hasFaultCode(e, foundation.Unauthenticated) {
			t.Fatal("current authority not first", e)
		}
	}
	if store.queries != 5 || store.checks != 5 || len(st.operations) != 0 {
		t.Fatal("authority/operation ownership", store.queries, store.checks, len(st.operations))
	}
}
func TestB04HTTPSettingsRangesAndSafeProjection(t *testing.T) {
	base := HTTPAccountSettingsUpdate{ExpectedVersion: 1, SessionIdleSeconds: 604800, SessionAbsoluteSeconds: 2592000, PasswordResetSeconds: 1800, ChallengeAfterFailures: 5}
	if httpSettingsValidate(base) != nil {
		t.Fatal("defaults rejected")
	}
	for _, modify := range []func(*HTTPAccountSettingsUpdate){func(q *HTTPAccountSettingsUpdate) { q.ExpectedVersion = 0 }, func(q *HTTPAccountSettingsUpdate) { q.SessionIdleSeconds = 899 }, func(q *HTTPAccountSettingsUpdate) { q.SessionAbsoluteSeconds = 7776001 }, func(q *HTTPAccountSettingsUpdate) { q.PasswordResetSeconds = 299 }, func(q *HTTPAccountSettingsUpdate) { q.ChallengeAfterFailures = 21 }, func(q *HTTPAccountSettingsUpdate) { q.SessionIdleSeconds = q.SessionAbsoluteSeconds + 1 }} {
		q := base
		modify(&q)
		if httpSettingsValidate(q) == nil {
			t.Fatal("invalid settings accepted")
		}
	}
	id, _ := foundation.NewID[c.SettingsRecord]()
	b, e := json.Marshal(httpSettingsDTO(c.Settings{ID: id, Version: 1, SessionIdleSeconds: 900, SessionAbsoluteSeconds: 3600, PasswordResetSeconds: 300, ChallengeAfterFailures: 1}))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(b), `"session_idle_seconds":"900"`) || !strings.Contains(string(b), `"lifetime_changes_apply_to":"newly_issued_sessions_and_tokens"`) {
		t.Fatal("settings wire", string(b))
	}
}
func TestB04HTTPUnconfigureRequiresExplicitRetryFields(t *testing.T) {
	h := &accountHTTP{}
	actor := httpTestActor(t)
	for _, body := range []string{`{"version":"1"}`, `{"version":"1","auto_retry_count":"0"}`, `{"version":"1","auto_retry_count":null,"retry_interval_seconds":"60"}`, `{"version":"1","auto_retry_count":"6","retry_interval_seconds":"60"}`, `{"version":"1","auto_retry_count":"0","retry_interval_seconds":"9"}`, `{"version":"1","auto_retry_count":"0","retry_interval_seconds":"60","host":"mail.example.test"}`, `{"version":"1","auto_retry_count":0,"retry_interval_seconds":"60"}`} {
		r := httptest.NewRequest("POST", "https://example.test/api/v1/system/smtp/unconfigure", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.httpUnconfigureSMTP(w, r, httpRequest{actor: actor, key: "unconfigure"})
		if w.Code != 400 {
			t.Fatal("bad unconfigure accepted", w.Code)
		}
	}
}
