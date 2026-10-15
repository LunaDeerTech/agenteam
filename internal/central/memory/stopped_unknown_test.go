package memory

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// This controls transaction return values, not SQL/authorization behavior.
// The real recovery entry points must keep the mutation's Unknown even when
// their one observation fails before its callback can read any durable fact.
type stoppedUnknownStore struct {
	Store
	t        *testing.T
	mode     string
	cancel   context.CancelFunc
	calls    int
	original f.CommitResult
}

func (s *stoppedUnknownStore) WithinTx(ctx context.Context, cause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.calls++
	switch s.calls {
	case 1:
		s.original = f.UnknownResult(testID[f.TransactionAttempt](s.t), cause)
		if s.mode == "cancelled" {
			s.cancel()
		}
		return s.original
	case 2:
		if !sameStoppedRecoveryCause(cause, s.original.Cause()) {
			s.t.Fatal("observation changed the original target")
		}
		if s.mode == "cancelled" {
			if !errors.Is(ctx.Err(), context.Canceled) {
				s.t.Fatal("observation did not receive original cancellation")
			}
			return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(ctx.Err()))
		}
		if s.mode == "observer-unknown" {
			return f.UnknownResult(testID[f.TransactionAttempt](s.t), cause)
		}
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
	default:
		s.t.Fatal("recovery retried a transaction after failed observation")
		return f.NotCommittedResult(nil)
	}
}

func sameStoppedRecoveryCause(a, b f.TransactionCause) bool {
	x, y := a.Details(), b.Details()
	return a.Validate() == nil && b.Validate() == nil && x.Kind == f.RecoveryCause && y.Kind == f.RecoveryCause && x.Owner == "memory" && x.Owner == y.Owner && x.RecoveryRunID == y.RecoveryRunID && x.CheckpointRef == y.CheckpointRef
}

func (s *stoppedUnknownStore) requireOriginal(t *testing.T, err error) {
	t.Helper()
	var fault *f.Fault
	var original commitOutcome
	if !errors.As(err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.CauseID != s.original.AttemptID().String() || fault.RetryHint != "lookup" || !errors.As(err, &original) {
		t.Fatal("failed observation replaced original Unknown")
	}
	if original.result.AttemptID() != s.original.AttemptID() || !sameStoppedRecoveryCause(original.result.Cause(), s.original.Cause()) || s.calls != 2 {
		t.Fatal("original transaction identity was lost or retried")
	}
}

type stoppedUnknownTool struct{ memoryJoinGate }

func (*stoppedUnknownTool) RequireStoppedMemoryToolInTx(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID, tc.OperationID, oc.ProcessID) error {
	return nil
}

func TestMemoryStoppedRecoveryPreservesMutationUnknown(t *testing.T) {
	for _, path := range []string{"model-retired", "resolution-retired", "retain-failure"} {
		for _, failure := range []string{"read-failed", "cancelled", "observer-unknown"} {
			t.Run(path+"/"+failure, func(t *testing.T) {
				base, cancel := context.WithCancel(context.Background())
				defer cancel()
				store := &stoppedUnknownStore{t: t, mode: failure, cancel: cancel}
				authority, resolution, _ := stoppedResolutionControl(t)
				domain := &Service{store: store, calls: map[*call]struct{}{}, changed: make(chan struct{})}
				models := &Models{domain: domain, authority: authority}
				// No Resolver/Model/Secret ports are installed: any dispatch or
				// retirement beyond the original transaction boundary fails.
				var err error
				switch path {
				case "model-retired":
					row := resolution.row
					snapshot := testID[mc.Snapshot](t)
					row.Snapshot = &snapshot
					request, requestErr := row.request(authority)
					if requestErr != nil {
						t.Fatal(requestErr)
					}
					mapping, _ := row.mapping()
					w := &stoppedModelWitness{authority: authority, row: row, current: authority.process, request: request, mapping: mapping}
					w.active.Store(true)
					ctx := context.WithValue(base, stoppedModelKey{}, w)
					attempt := mc.AttemptIdentity{CallID: row.Call, InvocationID: testID[mc.Invocation](t), ProcessID: *row.Process, AttemptIndex: 1, Fence: 1}
					err = models.markStoppedModelRetired(ctx, w, attempt)
					if w.row.Retired {
						t.Fatal("failed observation changed the original in-memory owner")
					}
				case "resolution-retired":
					ctx := context.WithValue(base, stoppedResolutionKey{}, resolution)
					after := resolution.row
					after.Retired = true
					err = models.changeStoppedResolution(ctx, resolution, after, nil, nil, nil)
					if resolution.row.Retired {
						t.Fatal("failed observation published a new cleanup state")
					}
				case "retain-failure":
					domain.deps.Tools = &stoppedUnknownTool{memoryJoinGate{allowed: true}}
					namespace := c.Namespace{ProjectID: resolution.row.Project, AgentID: resolution.row.Agent}
					actor, _ := i.NewAgentRun(namespace.ProjectID, namespace.AgentID, resolution.row.Execution)
					var code f.Code
					var found bool
					code, found, err = (&Runtime{domain: domain, models: models}).LookupRetainFailure(base, actor, namespace, resolution.row.Operation, c.RetainInput{Content: "original retain"})
					if found || code != "" || len(domain.calls) != 0 {
						t.Fatal("failed observation invented a canonical failure or leaked the read call")
					}
				}
				store.requireOriginal(t, err)
			})
		}
	}
}
