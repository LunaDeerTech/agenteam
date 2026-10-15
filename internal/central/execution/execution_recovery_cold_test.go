package execution

import (
	"context"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type executionRecoveryDeathProof struct {
	stopped  bool
	confirms int
}

func (*executionRecoveryDeathProof) RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error {
	panic("owner confirmation must happen outside a transaction")
}

func (p *executionRecoveryDeathProof) ConfirmStopped(ctx context.Context, _ oc.ProcessID) error {
	p.confirms++
	if !p.stopped {
		return fault(f.ResourceBusy)
	}
	return ctx.Err()
}

// These controls exercise the pre-transaction death gate. The production
// reader subsequently rechecks the complete original ledger under its locks;
// native process death and the waiting-to-successor chain remain integration
// requirements, not claims made by this fixture.
func TestExecutionRecoveryColdOwnerProofRequiresPhysicalReturn(t *testing.T) {
	for _, mode := range []string{"foreign-live-closed", "foreign-dead", "local-running", "local-closed-owned", "local-closed-returned", "local-foreign-instance", "missing-current", "changed-current"} {
		t.Run(mode, func(t *testing.T) {
			proof := &executionRecoveryDeathProof{stopped: mode == "foreign-dead"}
			process := newTestID[oc.Process](t)
			instance := newTestID[projectCallInstance](t)
			current := projectCallRecord{ID: newTestID[projectCallID](t), Project: newTestID[i.Project](t), Process: process, Instance: instance, Lane: projectLaneDirect, Identity: "original-start", Digest: projectInvocationDigest("original-start"), Phase: "running"}
			old := current
			old.ID = newTestID[projectCallID](t)
			old.Phase = "business_closed"
			owners := []projectCallRecord{current, old}
			p := &projectCallState{process: process, instance: instance, proof: proof, owners: map[f.ID[projectCallID]]*projectCallEntry{}}
			s := &directTextState{projectCalls: p}
			switch mode {
			case "foreign-live-closed", "foreign-dead":
				owners[1].Process = newTestID[oc.Process](t)
				// Several original calls from one old process require one real
				// death confirmation, never a closed-phase inference.
				duplicate := owners[1]
				duplicate.ID = newTestID[projectCallID](t)
				owners = append(owners, duplicate)
			case "local-running":
				owners[1].Phase = "running"
			case "local-closed-owned":
				p.owners[old.ID] = &projectCallEntry{record: old}
			case "local-foreign-instance":
				owners[1].Instance = newTestID[projectCallInstance](t)
			case "missing-current":
				owners = owners[1:]
			case "changed-current":
				owners[0].Instance = newTestID[projectCallInstance](t)
			}
			err := s.confirmRecoveryCallOwners(context.Background(), current, owners)
			wantSuccess := mode == "foreign-dead" || mode == "local-closed-returned"
			if (err == nil) != wantSuccess {
				t.Fatal("cold takeover bypassed original physical owner", err)
			}
			if mode == "foreign-dead" && proof.confirms != 1 {
				t.Fatal("death confirmation did not use the original process")
			}
		})
	}
}

func TestExecutionRecoveryColdJoinedProofCannotMoveBetweenCalls(t *testing.T) {
	facts, _ := runtimeModelCandidate(t)
	owner := &directTextState{}
	run := &directTextCall{owner: owner, snapshot: facts.Snapshot, round: facts.Round, started: true, allJoined: true, toolPending: true, toolFacts: &toolCallCandidate{}, invocation: &directTextInvocation{}}
	if joinedLoopCheckpoint(run) {
		t.Fatal("public checkpoint-shaped data replaced an actual Model join")
	}
	proof := &executionColdCheckpoint{owner: owner, run: run, snapshot: run.snapshot.Digest(), round: run.round.Digest()}
	run.cold = proof
	if joinedLoopCheckpoint(run) {
		t.Fatal("unverified private candidate admitted")
	}
	proof.verified = true
	if !joinedLoopCheckpoint(run) {
		t.Fatal("exact verified checkpoint rejected")
	}
	other := *run
	if joinedLoopCheckpoint(&other) {
		t.Fatal("copied caller reused original join proof")
	}
	proof.round = c.TriggerInputDigest([]byte("other-round"))
	if joinedLoopCheckpoint(run) {
		t.Fatal("new round reused old checkpoint proof")
	}
	proof.round = run.round.Digest()
	run.allJoined = false
	if joinedLoopCheckpoint(run) {
		t.Fatal("cold proof bypassed a later unjoined borrower")
	}
}

func TestExecutionRecoveryAssociatedColdStartUsesOriginalDriver(t *testing.T) {
	for _, status := range []c.Status{c.Running, c.Waiting} {
		t.Run(string(status), func(t *testing.T) {
			project, dispatch := associatedTestIdentity(t)
			starts := 0
			s := associatedTestExecutor(associatedExecutorOps{
				start: func(_ context.Context, execution i.ExecutionID) (c.DirectTextReceipt, error) {
					starts++
					if execution != dispatch.ExecutionID {
						t.Fatal("restoration changed original execution")
					}
					return c.DirectTextReceipt{}, fault(f.ResourceBusy)
				},
				owners: func(*associatedCall) associatedOwners { return associatedOwners{} },
			}, 1, 0).state
			s.direct = &DirectTextDriver{state: &directTextState{multi: &multiRoundState{}}}
			job := &associatedCall{project: project, dispatch: associatedIdentity{AssociatedDispatch: dispatch}}
			out := s.startPrepared(context.Background(), job, associatedFacts{status: status})
			if starts != 1 || !out.forget || out.err == nil || out.status != status {
				t.Fatal("persistent checkpoint did not reach original Start or lost retry boundary")
			}
		})
	}
}
