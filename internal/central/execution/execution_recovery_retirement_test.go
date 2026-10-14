package execution

import (
	"context"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Provider writes are controlled transaction participants. Execution's real
// capture, immutable input, retirement authorities and rollback run here;
// these counters do not establish native Model/Secret lease retirement.
type executionRecoveryRetirement struct {
	t                    *testing.T
	authority            *Authority
	store                *executionRecoveryStore
	model                mc.ConsumerDependencies
	plan                 *executionRecoveryRetirementPlan
	context              context.Context
	request              mc.ExecutionModelRetirementRequest
	environment          pvc.EnvironmentCapture
	modelCalls, envCalls int
	modelReads, envReads int
	failEnvironment      bool
}

type executionRecoveryRetirementPlan struct{ locks []f.LockRequest }

func (p *executionRecoveryRetirementPlan) RequiredLocks() []f.LockRequest {
	return append([]f.LockRequest(nil), p.locks...)
}

func (r *executionRecoveryRetirement) DiscoverExecutionModelRetirement(ctx context.Context, request mc.ExecutionModelRetirementRequest) (mc.ExecutionModelRetirementPlan, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	r.context, r.request = ctx, request.Clone()
	var err error
	r.model, err = r.authority.Discover(ctx, request.ConsumerRequest())
	if err != nil {
		return nil, err
	}
	// An active discovery witness and a valid plan cannot retire before the
	// canonical terminal write, or lend its authority to an invocation.
	requireCode(r.t, r.authority.ValidateInTx(ctx, f.NewTx(), request.ConsumerRequest(), r.model), f.Forbidden)
	invoke := request.ConsumerRequest()
	invoke.Action = mc.InvokeConsumer
	_, err = r.authority.Discover(ctx, invoke)
	requireCode(r.t, err, f.Forbidden)
	return r.plan, nil
}

func (r *executionRecoveryRetirement) requireModel(ctx context.Context, tx f.Tx, request mc.ExecutionModelRetirementRequest, plan mc.ExecutionModelRetirementPlan) error {
	if plan != r.plan {
		return fault(f.Forbidden)
	}
	if err := r.store.RequireHeldLocks(ctx, tx, r.plan.locks); err != nil {
		return err
	}
	return r.authority.ValidateInTx(ctx, tx, request.ConsumerRequest(), r.model)
}

func (r *executionRecoveryRetirement) ReleaseExecutionModelInTx(ctx context.Context, tx f.Tx, request mc.ExecutionModelRetirementRequest, plan mc.ExecutionModelRetirementPlan) error {
	if err := r.requireModel(ctx, tx, request, plan); err != nil {
		return err
	}
	r.modelCalls++
	r.store.providerRefs--
	return nil
}

func (r *executionRecoveryRetirement) ExecutionModelRetiredInTx(ctx context.Context, tx f.Tx, request mc.ExecutionModelRetirementRequest, plan mc.ExecutionModelRetirementPlan) (bool, error) {
	if err := r.requireModel(ctx, tx, request, plan); err != nil {
		return false, err
	}
	r.modelReads++
	return r.store.providerRefs == 4, nil
}

func (r *executionRecoveryRetirement) DiscoverEnvironmentRetirement(ctx context.Context, capture pvc.EnvironmentCapture) (pvc.EnvironmentRetirementPlan, error) {
	if err := r.authority.CheckEnvironmentRetirementPlan(ctx, capture); err != nil {
		return nil, err
	}
	r.environment = capture
	return r.plan, nil
}

func (r *executionRecoveryRetirement) requireEnvironment(ctx context.Context, tx f.Tx, capture pvc.EnvironmentCapture, plan pvc.EnvironmentRetirementPlan) error {
	if plan != r.plan {
		return fault(f.Forbidden)
	}
	if err := r.store.RequireHeldLocks(ctx, tx, r.plan.locks); err != nil {
		return err
	}
	return r.authority.CheckEnvironmentRetirementInTx(ctx, tx, capture)
}

func (r *executionRecoveryRetirement) RetireEnvironmentInTx(ctx context.Context, tx f.Tx, capture pvc.EnvironmentCapture, plan pvc.EnvironmentRetirementPlan) error {
	if err := r.requireEnvironment(ctx, tx, capture, plan); err != nil {
		return err
	}
	r.envCalls++
	if r.failEnvironment {
		return fault(f.DependencyUnavailable)
	}
	r.store.providerRefs--
	return nil
}

func (r *executionRecoveryRetirement) EnvironmentRetiredInTx(ctx context.Context, tx f.Tx, capture pvc.EnvironmentCapture, plan pvc.EnvironmentRetirementPlan) (bool, error) {
	if err := r.requireEnvironment(ctx, tx, capture, plan); err != nil {
		return false, err
	}
	r.envReads++
	return r.store.providerRefs == 4, nil
}

func TestExecutionRecoveryCapturedCancellationRetiresAtomically(t *testing.T) {
	for _, mode := range []string{"commit", "retirement-failure", "commit-unknown"} {
		t.Run(mode, func(t *testing.T) {
			capture := newCompleteCaptureControl(t)
			credential := newTestID[sc.Credential](t)
			ref, err := sc.NewCredentialRef(credential, i.SystemScope())
			if err != nil {
				t.Fatal(err)
			}
			capture.resolved.Snapshot.CredentialRef = &ref
			capture.resolved.CredentialLease = &sc.CredentialLease{LeaseID: newTestID[sc.Lease](t), CredentialRef: ref}
			if err = capture.p.driver.Run(context.Background(), capture.p.execution); err != nil {
				t.Fatal(err)
			}
			s, job, ctx, store, events, _ := executionRecoveryForPreparation(t, capture.p)
			key, err := f.AggregateLock(f.CredentialRefAggregate, credential.String())
			if err != nil {
				t.Fatal(err)
			}
			retirement := &executionRecoveryRetirement{t: t, authority: capture.p.launch.authority, store: store, plan: &executionRecoveryRetirementPlan{locks: []f.LockRequest{{Key: key, Mode: f.Exclusive}}}, failEnvironment: mode == "retirement-failure"}
			s.direct.state.deps.Models, s.direct.state.deps.Environment = retirement, retirement
			stored, err := loadPreparationInput(ctx, store, job.dispatch.ExecutionID)
			if err != nil || stored == nil || store.providerRefs != 6 {
				t.Fatal("complete original capture missing", err)
			}
			beforeWrites, captureCalls := store.writes, len(capture.order)
			store.unknown = mode == "commit-unknown"
			summary, handled, err := s.recoverCancelledPreparation(ctx, job)
			if !handled || retirement.modelCalls != 1 || retirement.envCalls != 1 || len(capture.order) != captureCalls {
				t.Fatal("recovery skipped providers or recaptured original resources", err)
			}
			if mode == "retirement-failure" {
				if err == nil || store.rows[job.dispatch.ExecutionID.String()][3] != "preparing" || store.providerRefs != 6 || events.appends != 0 || store.writes != beforeWrites {
					t.Fatal("retirement failure left a partial terminal or released reference", err)
				}
			} else {
				if store.providerRefs != 4 || events.appends != 1 {
					t.Fatal("terminal did not retire both controlled providers")
				}
				if mode == "commit-unknown" {
					if err == nil || job.recovery == nil || job.recovery.unknown == nil {
						t.Fatal("unknown lost exact retirement candidate")
					}
					writes := store.writes
					summary, handled, err = s.recoverCancelledPreparation(ctx, job)
					if !handled || retirement.modelReads != 1 || retirement.envReads != 1 || retirement.modelCalls != 1 || retirement.envCalls != 1 || store.writes != writes {
						t.Fatal("unknown observation repeated retirement writers")
					}
				}
				if err != nil || summary.Status != c.Cancelled || job.recovery != nil {
					t.Fatal("captured cancellation did not settle", err)
				}
			}
			after, readErr := loadPreparationInput(ctx, store, job.dispatch.ExecutionID)
			if readErr != nil || !samePreparationInput(stored, after) {
				t.Fatal("retirement erased immutable history", readErr)
			}
			_, err = retirement.authority.Discover(retirement.context, retirement.request.ConsumerRequest())
			requireCode(t, err, f.Forbidden)
		})
	}
}
