package meeting

import (
	"context"
	"errors"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type summaryTransitionStore struct {
	Store
	t        *testing.T
	tx       f.Tx
	input    SummaryInput
	state    string
	released *time.Time
	queries  int
	txs      int
	locks    []f.LockRequest
}

func (s *summaryTransitionStore) WithinTx(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.txs++
	if err := fn(ctx, s.tx); err != nil {
		var known *f.Fault
		if errors.As(err, &known) {
			return f.NotCommittedResult(known)
		}
		return f.NotCommittedResult(fault(f.InternalError).WithCause(err))
	}
	return f.CommittedResult()
}
func (s *summaryTransitionStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if tx != s.tx {
		s.t.Fatal("transition observation changed the original transaction")
	}
	s.locks = locks
	return nil
}
func (s *summaryTransitionStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		s.t.Fatal("transition observation borrowed a foreign transaction")
	}
	return s, nil
}

type summaryTransitionRow struct{ store *summaryTransitionStore }

func (r summaryTransitionRow) Scan(dest ...any) error {
	if len(dest) != 2 {
		r.store.t.Fatal("transition observation requested extra mutable state")
	}
	*dest[0].(*string), *dest[1].(**time.Time) = r.store.state, r.store.released
	return nil
}
func (s *summaryTransitionStore) QueryRow(_ context.Context, sql string, args ...any) postgres.Row {
	s.queries++
	if sql != `SELECT status,released_at FROM agenteam_meeting.summary_generations WHERE id=$1` || len(args) != 1 || args[0] != s.input.GenerationID.String() {
		s.t.Fatal("transition observation lost the original generation")
	}
	return summaryTransitionRow{s}
}

type summaryTransitionPlan struct{ locks []f.LockRequest }

func (p *summaryTransitionPlan) RequiredLocks() []f.LockRequest { return p.locks }

type summaryTransitionRetention struct {
	mc.ServingSnapshotRetention
	t       *testing.T
	store   *summaryTransitionStore
	witness *summarySnapshotWitness
	plan    *summaryTransitionPlan
	applied bool
	reads   int
}

func (s *summaryTransitionRetention) ServingSnapshotRetentionAppliedInTx(ctx context.Context, tx f.Tx, request mc.ServingSnapshotRetentionRequest, plan mc.ServingSnapshotRetentionPlan) (bool, error) {
	s.reads++
	w, _ := ctx.Value(summarySnapshotKey{}).(*summarySnapshotWitness)
	want, err := mc.ServingSnapshotRetentionBinding(s.witness.request)
	actual, actualErr := mc.ServingSnapshotRetentionBinding(request)
	if tx != s.store.tx || w != s.witness || !w.active || plan != s.plan || err != nil || actualErr != nil || actual != want {
		s.t.Fatal("observation replaced the original private request or plan")
	}
	for _, needed := range s.plan.locks {
		found := false
		for _, held := range s.store.locks {
			found = found || held.Key.Canonical() == needed.Key.Canonical() && held.Mode == needed.Mode
		}
		if !found {
			s.t.Fatal("observation dropped an original retention lock")
		}
	}
	return s.applied, nil
}

func TestSummaryTerminalObservesOriginalUnknownBeforeDifferentTarget(t *testing.T) {
	for _, test := range []struct {
		name, original, requested, stored   string
		unknown, applied, released, cleared bool
		code                                f.Code
	}{
		{"failed-release-before-cancel", "failed", "cancelled", "failed", true, true, true, true, f.ResourceBusy},
		{"same-original-cancel", "cancelled", "cancelled", "cancelled", true, true, true, true, ""},
		{"release-unconfirmed", "failed", "cancelled", "failed", true, false, true, false, f.CommitUnknown},
		{"wrong-terminal", "failed", "cancelled", "cancelled", true, true, true, false, f.CommitUnknown},
		{"no-release-fact", "failed", "cancelled", "failed", true, true, false, false, f.CommitUnknown},
		{"original-owner-running", "failed", "cancelled", "failed", false, true, true, false, f.ResourceBusy},
	} {
		t.Run(test.name, func(t *testing.T) {
			authority, owner, _, original := summaryOwnerFixture(t)
			store := &summaryTransitionStore{t: t, tx: f.NewTx(), input: owner.input, state: test.stored}
			if test.released {
				at := time.Now().UTC()
				store.released = &at
			}
			model := &SummaryModel{store: store, deps: SummaryModelDependencies{Authority: authority}, transitions: make(map[c.SummaryGenerationID]*summaryTransition)}
			request := mc.ServingSnapshotRetentionRequest{Actor: original.Actor, Owner: mc.ServingSnapshotOwner{Kind: mc.MeetingConsumer, ProjectID: owner.input.ProjectID, GenerationID: owner.input.GenerationID.String()}, SnapshotID: original.SnapshotID, OwnerVersion: 2, Action: mc.ReleaseServingSnapshot}
			witness := &summarySnapshotWitness{model: model, input: owner.input, request: request, active: true}
			plan := &summaryTransitionPlan{locks: []f.LockRequest{projectLock(owner.input.ProjectID, f.Exclusive)}}
			retention := &summaryTransitionRetention{t: t, store: store, witness: witness, plan: plan, applied: test.applied}
			model.deps.Retention = retention
			transition := &summaryTransition{witness: witness, plan: plan, target: test.original, unknown: test.unknown}
			model.transitions[owner.input.GenerationID] = transition
			err := model.summaryTerminal(context.Background(), owner.input, test.requested, func(context.Context, f.Tx, postgres.SQLExecutor) error {
				t.Fatal("uncertain original transition executed a new target callback")
				return nil
			})
			var known *f.Fault
			if test.code == "" && err != nil || test.code != "" && (!errors.As(err, &known) || known.Code != test.code) {
				t.Fatal("unexpected original observation outcome", err)
			}
			if test.cleared != (model.transitions[owner.input.GenerationID] == nil) || witness.active == test.cleared {
				t.Fatal("uncertain or confirmed private transition lifetime changed")
			}
			want := 0
			if test.unknown {
				want = 1
			}
			if retention.reads != want || store.txs != want || store.queries != want {
				t.Fatal("observation did not use exactly its original read-only transaction")
			}
			// Other Retention/Model methods and Store writes are deliberately
			// unbound: any rediscovery, release, Begin or SQL write fails here.
		})
	}
}
