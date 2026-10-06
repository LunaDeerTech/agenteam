package model

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type managementStore struct {
	Store
	t                                      *testing.T
	tx                                     f.Tx
	model                                  mc.ModelID
	actor                                  id.Actor
	selection                              selectionRecord
	platform                               []referenceRecord
	counts                                 [7]f.Progress
	total                                  f.Progress
	version                                f.Version
	result                                 f.CommitState
	txs, acquires, held, queries, sessions int
	locks                                  []f.LockRequest
	acquireErr, inTxErr, heldErr, sqlErr   error
	afterCommit                            func()
	deadline                               time.Time
}

func (s *managementStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.txs++
	s.tx = f.NewTx()
	if cause.Details().Owner != "model.management-impact" {
		s.t.Fatal("wrong read cause")
	}
	var ok bool
	s.deadline, ok = ctx.Deadline()
	if !ok || time.Until(s.deadline) > 3*time.Second {
		s.t.Fatal("missing library read deadline")
	}
	if err := fn(ctx, s.tx); err != nil {
		var ff *f.Fault
		if !errors.As(err, &ff) {
			s.t.Fatal("untyped error", err)
		}
		return f.NotCommittedResult(ff)
	}
	if s.result == f.Unknown {
		return f.UnknownResult(mustID[f.TransactionAttempt](s.t), cause)
	}
	if s.result == f.NotCommitted {
		return f.NotCommittedResult(fault(f.ResourceBusy))
	}
	if s.afterCommit != nil {
		s.afterCommit()
	}
	return f.CommittedResult()
}
func (s *managementStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.acquires++
	if tx != s.tx {
		s.t.Fatal("foreign Tx")
	}
	s.locks = append([]f.LockRequest(nil), locks...)
	want := []f.LockRequest{userLock(s.actor.Details().UserID), aggregateLock(f.ModelConfigAggregate, s.model.String(), f.Shared), systemLock("model-platform-selection", f.Shared), systemLock("model-references", f.Exclusive)}
	if len(locks) != len(want) {
		s.t.Fatal("incomplete lock union")
	}
	for i, lock := range locks {
		if lock.Key.Canonical() != want[i].Key.Canonical() || lock.Mode != want[i].Mode {
			s.t.Fatal("incomplete lock union")
		}
	}
	return s.acquireErr
}
func (s *managementStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		s.t.Fatal("foreign Store/Tx")
	}
	return s, s.inTxErr
}
func (s *managementStore) RequireHeldLocks(_ context.Context, tx f.Tx, want []f.LockRequest) error {
	s.held++
	if tx != s.tx {
		s.t.Fatal("foreign held Tx")
	}
	for _, w := range want {
		found := false
		for _, h := range s.locks {
			if h.Key.Canonical() == w.Key.Canonical() && (w.Mode == h.Mode || h.Mode == f.Exclusive) {
				found = true
			}
		}
		if !found {
			s.t.Fatal("unheld lock")
		}
	}
	return s.heldErr
}
func (s *managementStore) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	s.queries++
	if s.sessions != 1 || s.held < 1 {
		s.t.Fatal("SQL before same-Tx authority")
	}
	return scanFunc(func(dst ...any) error {
		if s.sqlErr != nil {
			return s.sqlErr
		}
		switch {
		case strings.Contains(q, "SELECT m.version"):
			if len(args) != 1 || args[0] != s.model.String() || !strings.Contains(q, "p.scope='system'") {
				s.t.Fatal("wrong model scope")
			}
			*dst[0].(*f.Version) = s.version
		case strings.Contains(q, "FROM agenteam_model.platform_selection"):
			r := s.selection
			*dst[0].(*string) = r.ID
			*dst[1].(*f.Version) = r.Version
			*dst[2].(*bool) = r.Configured
			for i, v := range []string{r.Embedding, r.Memory, r.Reranker, r.Image} {
				if v != "" {
					v := v
					*dst[i+3].(**string) = &v
				}
			}
		case strings.Contains(q, "LIMIT 5"):
			if strings.Contains(q, "owner_id=$") || len(args) != 0 {
				s.t.Fatal("platform query failed to cover extra owners")
			}
			raw, err := json.Marshal(s.platform)
			if err != nil {
				return err
			}
			*dst[0].(*[]byte) = raw
		case strings.Contains(q, "WITH bounded AS MATERIALIZED"):
			if !strings.Contains(q, "WHERE model_id=$1 LIMIT 10001)") || len(args) != 1 || args[0] != s.model.String() {
				s.t.Fatal("aggregation has no bounded input")
			}
			*dst[0].(*f.Progress) = s.total
			for i := range s.counts {
				*dst[i+1].(*f.Progress) = s.counts[i]
			}
		default:
			s.t.Fatal("unexpected management query")
		}
		return nil
	})
}
func managementFixture(t *testing.T) (*Service, *managementStore, id.Actor, mc.ModelID) {
	t.Helper()
	actor := testActor(t)
	key := mustID[mc.Model](t)
	store := &managementStore{t: t, actor: actor, model: key, version: 9007199254740993, selection: selectionRecord{ID: mustID[struct{}](t).String(), Version: 1}, platform: []referenceRecord{}}
	a, err := NewAuthority(store, Authorizations{Sessions: sessionFunc(func(_ context.Context, tx f.Tx, a id.Actor) error {
		store.sessions++
		if tx != store.tx || !a.Equal(actor) || store.held < 1 {
			t.Fatal("authority left read locks")
		}
		return nil
	}), System: systemFunc(func(_ context.Context, tx f.Tx, a id.Actor, intent id.AccessIntent) (id.AccessGrant, error) {
		if tx != store.tx || intent != id.Read {
			t.Fatal("wrong current scope")
		}
		return validGrant(a, intent)
	})})
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(store, a, testDependencies(t))
	if err != nil {
		t.Fatal(err)
	}
	return s, store, actor, key
}

func TestModelManagementImpactClosedGroupsAndBoundedSQL(t *testing.T) {
	for _, tc := range []struct {
		name        string
		counts      [7]f.Progress
		total       f.Progress
		requirement ModelReplacementRequirement
		blocked     bool
		code        f.Code
	}{
		{"empty", [7]f.Progress{}, 0, "none", false, ""},
		{"optional", [7]f.Progress{3: 1, 5: 1}, 2, "optional", false, ""},
		{"all-seven", [7]f.Progress{1, 1, 1, 1, 1, 1, 1}, 7, "required", true, ""},
		{"cap", [7]f.Progress{10000}, 10000, "required", true, ""},
		{"sentinel", [7]f.Progress{10001}, 10001, "", false, f.ResourceBusy},
		{"unknown-kind-role", [7]f.Progress{}, 1, "", false, f.DependencyUnavailable},
		{"negative", [7]f.Progress{-1}, 0, "", false, f.DependencyUnavailable},
		{"wrong-total", [7]f.Progress{2}, 1, "", false, f.DependencyUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store, actor, key := managementFixture(t)
			store.counts, store.total = tc.counts, tc.total
			out, err := s.GetModelDeletionImpact(context.Background(), actor, key)
			if tc.code != "" {
				requireCode(t, err, tc.code)
				if !reflect.DeepEqual(out, ModelDeletionImpact{}) {
					t.Fatal("partial result")
				}
				return
			}
			if err != nil || out.ReferenceCount != tc.total || out.ReplacementRequirement != tc.requirement || (out.DeleteBlocker != nil) != tc.blocked || out.ReferenceGroups == nil {
				t.Fatal("wrong aggregation", err)
			}
			if store.txs != 1 || store.acquires != 1 || store.queries != 4 {
				t.Fatal("read retried or used extra SQL")
			}
			raw, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if json.Unmarshal(raw, &wire) != nil || len(wire) != 6 || wire["version"] != "9007199254740993" {
				t.Fatal("unsafe DTO")
			}
			previous := ""
			var sum f.Progress
			for _, g := range out.ReferenceGroups {
				order := g.OwnerKind + "/" + g.Role
				if order <= previous || g.Count <= 0 {
					t.Fatal("group order")
				}
				previous = order
				sum += g.Count
			}
			if sum != out.ReferenceCount {
				t.Fatal("sum")
			}
		})
	}
}

func TestModelManagementCanonicalSelectionRejectsDrift(t *testing.T) {
	for _, branch := range []string{"missing-reference", "extra-owner", "wrong-version", "wrong-role", "wrong-model", "wrong-project", "wrong-effort", "fifth-row", "unconfigured-extra", "bad-singleton", "bad-version"} {
		t.Run(branch, func(t *testing.T) {
			s, store, actor, key := managementFixture(t)
			store.selection.Configured = true
			store.selection.Embedding = key.String()
			store.selection.Memory = mustID[mc.Model](t).String()
			store.platform = selectionReferences(&store.selection)
			switch branch {
			case "missing-reference":
				store.platform = store.platform[:1]
			case "extra-owner":
				store.platform[0].Owner = mustID[struct{}](t).String()
			case "wrong-version":
				store.platform[0].Version++
			case "wrong-role":
				store.platform[0].Role = "unexpected"
			case "wrong-model":
				store.platform[0].Model = mustID[mc.Model](t).String()
			case "wrong-project":
				store.platform[0].Project = mustID[id.Project](t).String()
			case "wrong-effort":
				store.platform[0].Effort = "high"
			case "fifth-row":
				store.platform = append(store.platform, store.platform[0], store.platform[0], store.platform[0])
			case "unconfigured-extra":
				store.selection.Configured = false
				store.selection.Embedding = ""
				store.selection.Memory = ""
			case "bad-singleton":
				store.selection.ID = "bad"
			case "bad-version":
				store.selection.Version = 0
			}
			out, err := s.GetModelDeletionImpact(context.Background(), actor, key)
			requireCode(t, err, f.DependencyUnavailable)
			if !reflect.DeepEqual(out, ModelDeletionImpact{}) {
				t.Fatal("drift returned data")
			}
		})
	}
}

func TestModelManagementAuthorityAndResults(t *testing.T) {
	for _, branch := range []string{"nil-context", "bad-model", "zero-service", "typed-nil", "wrong-store", "actor", "session", "grant", "acquire", "held", "executor", "missing", "unknown", "not-committed", "committed-cancel"} {
		t.Run(branch, func(t *testing.T) {
			s, store, actor, key := managementFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			code := f.DependencyUnavailable
			switch branch {
			case "nil-context":
				ctx = nil
				code = f.InvalidArgument
			case "bad-model":
				key = mc.ModelID{}
				code = f.InvalidArgument
			case "zero-service":
				s = &Service{}
				code = f.DependencyUnbound
			case "typed-nil":
				var nilStore *managementStore
				s.state().store = nilStore
				code = f.DependencyUnbound
			case "wrong-store":
				s.state().authority.state().store = &noIOStore{}
				code = f.DependencyUnbound
			case "actor":
				actor = id.Actor{}
				code = f.Unauthenticated
			case "session":
				s.state().authority.state().auth.Sessions = sessionFunc(func(context.Context, f.Tx, id.Actor) error { return fault(f.SessionRevoked) })
				code = f.SessionRevoked
			case "grant":
				s.state().authority.state().auth.System = systemFunc(func(context.Context, f.Tx, id.Actor, id.AccessIntent) (id.AccessGrant, error) {
					return id.AccessGrant{}, nil
				})
				code = f.Forbidden
			case "acquire":
				store.acquireErr = fault(f.ResourceBusy)
				code = f.ResourceBusy
			case "held":
				store.heldErr = fault(f.InvalidState)
				code = f.InvalidState
			case "executor":
				store.inTxErr = fault(f.InvalidArgument)
				code = f.InvalidArgument
			case "missing":
				store.sqlErr = pgx.ErrNoRows
				code = f.NotFound
			case "unknown":
				store.result = f.Unknown
				code = f.CommitUnknown
			case "not-committed":
				store.result = f.NotCommitted
				code = f.ResourceBusy
			case "committed-cancel":
				store.afterCommit = cancel
			}
			out, err := s.GetModelDeletionImpact(ctx, actor, key)
			requireCode(t, err, code)
			if !reflect.DeepEqual(out, ModelDeletionImpact{}) {
				t.Fatal("failure returned candidate")
			}
			if branch == "unknown" || branch == "not-committed" || branch == "committed-cancel" {
				if store.queries != 4 || store.txs != 1 {
					t.Fatal("did not execute complete read once")
				}
			} else if branch != "missing" && store.queries != 0 {
				t.Fatal("SQL before authority")
			}
		})
	}
}
