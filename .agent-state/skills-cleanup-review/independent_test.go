package skill

import (
	"context"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

// This probe exercises the actual Skills consumer. The author's explicit SQL
// double is reused as setup; Object responses below are deliberate controls.
// It makes no claim about PostgreSQL rollback, native D05 facts or real join.
type independentCleanupObjects struct {
	*cleanupTestObjects
	physicalResult func(context.Context, oc.CleanupResult) (oc.CleanupResult, error)
	purgeResult    func(oc.ObjectMetadataPurgeResult) (oc.ObjectMetadataPurgeResult, error)
}

func (o *independentCleanupObjects) DeleteUnreferencedWithinBudget(ctx context.Context, cause oc.ObjectCleanupCause, object oc.ObjectID) (oc.CleanupResult, error) {
	r, err := o.cleanupTestObjects.DeleteUnreferencedWithinBudget(ctx, cause, object)
	if err != nil || o.physicalResult == nil {
		return r, err
	}
	return o.physicalResult(ctx, r)
}

func (o *independentCleanupObjects) PurgeDeletedObjectMetadataInTx(ctx context.Context, tx f.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.ObjectMetadataPurgeResult, error) {
	r, err := o.cleanupTestObjects.PurgeDeletedObjectMetadataInTx(ctx, tx, cause, object, plan, locked)
	if err != nil || o.purgeResult == nil {
		return r, err
	}
	return o.purgeResult(r)
}

func TestIndependentSkillCleanupPhysicalResults(t *testing.T) {
	for _, mode := range []string{"complete", "pending", "wrong_operation", "failed", "remaining_reference", "original_unknown"} {
		t.Run(mode, func(t *testing.T) {
			s, store, original, project, scope := gatedCleanupFixture(t)
			before := *store.c
			unknown := f.NewFault(f.CommitUnknown, f.Unknown)
			deadline := time.Now().Add(time.Second)
			ctx, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			o := &independentCleanupObjects{cleanupTestObjects: original}
			o.physicalResult = func(ctx context.Context, r oc.CleanupResult) (oc.CleanupResult, error) {
				actual, ok := ctx.Deadline()
				if !ok || !actual.Equal(deadline) {
					t.Fatal("consumer renewed earlier parent budget")
				}
				switch mode {
				case "pending":
					r.State = oc.CleanupPending
				case "wrong_operation":
					r.OperationID = stateID[oc.CleanupOperation](701)
				case "failed":
					r.State = oc.CleanupFailed
				case "remaining_reference":
					r.Remaining.References = []oc.ObjectReference{{ObjectID: store.r.object}}
				case "original_unknown":
					return oc.CleanupResult{}, unknown
				}
				return r, nil
			}
			s.state().objects = o
			report, err := s.Cleanup(ctx, project.actor, project.cause, scope, nil)
			if mode == "complete" || mode == "pending" {
				want := cleanupCompleted
				if mode == "pending" {
					want = cleanupPending
				}
				if err != nil || report.Details().State != pc.CleanupPending || store.c.phase != want {
					t.Fatal("completion was not separately committed pending work", err)
				}
			} else {
				if err == nil || *store.c != before {
					t.Fatal("unproven physical result advanced durable phase", err)
				}
				if mode == "original_unknown" && err != unknown {
					t.Fatal("original provider Unknown replaced", err)
				}
			}
			if o.releases != 0 || o.physical != 1 || o.purges != 0 || !store.core || !store.native || len(store.deleted) != 0 || len(s.state().calls) != 0 {
				t.Fatal("physical stage changed anchor/history or retained returned call")
			}
		})
	}
}

func TestIndependentSkillCleanupPurgeResultAtomicity(t *testing.T) {
	for _, mode := range []string{"pending", "wrong_object", "wrong_operation", "invalid_state", "original_unknown"} {
		t.Run(mode, func(t *testing.T) {
			s, store, original, project, scope := gatedCleanupFixture(t)
			store.c.phase = cleanupCompleted
			before := *store.c
			unknown := f.NewFault(f.CommitUnknown, f.Unknown)
			o := &independentCleanupObjects{cleanupTestObjects: original}
			o.purgeResult = func(r oc.ObjectMetadataPurgeResult) (oc.ObjectMetadataPurgeResult, error) {
				switch mode {
				case "pending":
					r.State = oc.CleanupPending
					store.native = true // A legal Pending response retains D05 anchors.
				case "wrong_object":
					r.ObjectID = stateID[oc.StoredObject](702)
				case "wrong_operation":
					r.OperationID = stateID[oc.CleanupOperation](703)
				case "invalid_state":
					r.State = oc.CleanupFailed
				case "original_unknown":
					return oc.ObjectMetadataPurgeResult{}, unknown
				}
				return r, nil
			}
			s.state().objects = o
			report, err := s.Cleanup(context.Background(), project.actor, project.cause, scope, nil)
			if mode == "pending" {
				if err != nil || report.Details().State != pc.CleanupPending {
					t.Fatal("pending metadata reported complete", err)
				}
			} else if err == nil {
				t.Fatal("malformed or unconfirmed purge accepted")
			}
			if mode == "original_unknown" && err != unknown {
				t.Fatal("original provider error lost", err)
			}
			if !store.native || !store.core || store.c == nil || *store.c != before || !store.attempts[store.r.attempt.String()] || len(store.deleted) != 0 || o.releases != 0 || o.physical != 0 || o.purges != 1 {
				t.Fatal("uncommitted provider output escaped to core deletion")
			}
		})
	}
}

func TestIndependentSkillCleanupEmptyReplayRevalidates(t *testing.T) {
	for _, mode := range []string{"empty", "current_gate_denied", "held_read", "cancelled_read", "returned_unknown_work", "partial_core"} {
		t.Run(mode, func(t *testing.T) {
			s, store, o, project, scope := gatedCleanupFixture(t)
			store.core, store.native, store.c = false, false, nil
			store.row = skillRowValues{err: pgx.ErrNoRows}
			store.attempts = map[string]bool{}
			denied := f.NewFault(f.Forbidden, f.NotStarted)
			if mode == "current_gate_denied" {
				project.denied = denied
			}
			if mode == "partial_core" {
				store.attempts[store.r.attempt.String()] = true
			}
			var call *serviceCall
			if mode == "held_read" || mode == "cancelled_read" || mode == "returned_unknown_work" {
				var err error
				call, err = s.beginProjectWork(context.Background(), scope.ProjectID, packageReaderWork)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "cancelled_read" {
					call.cancel()
				}
				if mode == "returned_unknown_work" {
					work, err := s.newOwnedWork(store.r, packageReaderWork, call)
					if err != nil {
						t.Fatal(err)
					}
					s.registrationFailed(work, f.NewFault(f.CommitUnknown, f.Unknown))
					s.end(call)
					defer s.forgetOwnedWork(work)
				}
				defer s.end(call)
			}
			report, err := s.Cleanup(context.Background(), project.actor, project.cause, scope, nil)
			switch mode {
			case "empty":
				if err != nil || report.Details().State != pc.CleanupCompleted {
					t.Fatal("known empty result", err)
				}
			case "current_gate_denied":
				if err != denied {
					t.Fatal("empty replay bypassed current authority", err)
				}
			case "partial_core":
				if err == nil {
					t.Fatal("partial parent disappearance accepted")
				}
			default:
				if err != nil || report.Details().State != pc.CleanupPending {
					t.Fatal("cancel/return/map shape substituted for original retirement", err)
				}
			}
			if project.calls != 1 || o.releases+o.physical+o.purges != 0 {
				t.Fatal("empty replay touched missing Object or skipped original gate")
			}
		})
	}
}

func TestIndependentSkillCleanupMixedHistoryUnknown(t *testing.T) {
	s, store, o, project, scope := gatedCleanupFixture(t)
	store.c.phase = cleanupCompleted
	workID := stateID[skillWork](704)
	at := store.r.created.Time()
	store.work[workID.String()] = []any{workID.String(), scope.ProjectID.String(), store.r.skill.String(), stateID[oc.Process](50).String(), "package_reader", "joined", int64(1), at, &at}
	for i := 0; i < 65; i++ {
		store.attempts[stateID[oc.Attempt](800+i).String()] = true
	}
	report, err := s.Cleanup(context.Background(), project.actor, project.cause, scope, nil)
	if err != nil || report.Details().State != pc.CleanupPending || len(store.work) != 0 || len(store.attempts) != 66 {
		t.Fatal("one local batch crossed histories", err)
	}
	store.unknownAt, store.rollbackUnknown = store.txs+2, true
	_, err = s.Cleanup(context.Background(), project.actor, project.cause, scope, nil)
	if _, ok := UnknownAttempt(err); !ok || len(store.attempts) != 66 {
		t.Fatal("history Unknown replaced or advanced confirmed progress", err)
	}
	store.unknownAt = 0
	for _, remaining := range []int{34, 2, 1} {
		before := len(store.attempts)
		report, err = s.Cleanup(context.Background(), project.actor, project.cause, scope, nil)
		if err != nil || report.Details().State != pc.CleanupPending || len(store.attempts) != remaining || before-len(store.attempts) > 32 || !store.attempts[store.r.attempt.String()] || !store.core || !store.native || o.physical+o.releases+o.purges != 0 {
			t.Fatal("remaining-set recovery lost bound or current anchor", err)
		}
	}
}
