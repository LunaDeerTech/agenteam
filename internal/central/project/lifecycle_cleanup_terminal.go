package project

import (
	"bytes"
	"context"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// The explicit terminal constructor completes only agent-skills-variables.
// It does not run downstream owners, bind a registry, or delete the Project.
func NewLifecycleCleanupDriverWithTerminal(store Store, projects *Authority, processes oc.ProcessAuthority, agents ac.ProjectRetirementConsumer, skills SkillsCleanupDomain, variables pvc.ProjectCleanupDomain, terminalAgents ac.ProjectTerminalCleanupDomain, terminalVariables c.ProjectTerminalCleanupDomain) (*LifecycleCleanupDriver, error) {
	if nilPort(terminalAgents) || nilPort(terminalVariables) {
		return nil, fault(f.DependencyUnbound)
	}
	d, err := NewLifecycleCleanupDriverWithVariables(store, projects, processes, agents, skills, variables)
	if err != nil {
		return nil, err
	}
	a, b := terminalAgents.ProjectTerminalCleanupIdentity(), terminalVariables.ProjectTerminalCleanupIdentity()
	if a.Validate() != nil || b.Validate() != nil || a.Kind != c.ProjectTerminalAgentOwner || b.Kind != c.ProjectTerminalVariablesOwner || a.ProcessID != d.state.process.String() || b.ProcessID != d.state.process.String() {
		return nil, fault(f.DependencyUnbound)
	}
	d.state.terminalAgents, d.state.terminalVariables = terminalAgents, terminalVariables
	return d, nil
}
func terminalParticipantCompleted(r *lifecycleRecord) bool {
	if r == nil || r.operation.Action != c.Delete || r.operation.State != c.OperationCleaning || r.cleanupStage != "domains" {
		return false
	}
	found := false
	for _, row := range r.participants {
		if row.stop != "stopped" {
			return false
		}
		if row.name == c.SkillsParticipant {
			found = row.cleanup == "completed"
		}
	}
	return found
}
func sameTerminalClaim(a, b lifecycleStopClaim) bool {
	a.phase = "running"
	b.phase = "running"
	return a == b
}
func (st *lifecycleCleanupState) selectTerminalRound(round *lifecycleCleanupRound, actual *lifecycleStopClaim, cp *c.CleanupCheckpoint, version f.Version, rowVersion int64) (bool, error) {
	if cp == nil {
		return false, nil
	}
	if cp.Schema() == 6 {
		receipt, err := decodeTerminalCleanupCheckpoint(cp, round.cause, round.claim.project)
		if err != nil {
			return false, err
		}
		if actual == nil || actual.phase != "terminal" || !receipt.matchesClaim(*actual) {
			return false, fault(f.ResourceBusy)
		}
		round.claim = *actual
		round.checkpoint = cp
		round.operationVersion = version
		round.rowVersion = rowVersion
		round.terminalStep = true
		round.terminalConfirm = true
		return true, nil
	}
	if cp.Schema() != 5 {
		return false, nil
	}
	progress, _, err := decodeVariablesCleanupCheckpoint(cp, round.cause, round.claim.project)
	if err != nil {
		return false, err
	}
	if progress.State != c.CleanupCompleted {
		return false, nil
	}
	round.terminalStep = true
	round.checkpoint = cp
	st.mu.Lock()
	old := st.owners[round.claim.operation]
	st.mu.Unlock()
	if old.terminal != nil && actual != nil && actual.process == st.process && sameTerminalClaim(old.terminal.claim, *actual) {
		previous := old.terminal.checkpoint
		if previous == nil || previous.Schema() != 5 || !bytes.Equal(previous.Bytes(), cp.Bytes()) {
			return false, fault(f.Forbidden)
		}
		if old.terminal.terminalUnappliedConfirmed {
			return true, nil
		}
		round.claim = *actual
		round.terminalConfirm = true
		round.terminalUnapplied = true
		round.operationVersion = version
		round.rowVersion = rowVersion
	}
	return true, nil
}
func (st *lifecycleCleanupState) runTerminalStep(ctx context.Context, round lifecycleCleanupRound) (settled bool, err error) {
	run, end := context.WithTimeout(ctx, 2*time.Second)
	defer end()
	w := &lifecycleCleanupScope{state: st, round: round, open: true}
	run = context.WithValue(run, lifecycleCleanupScopeKey{}, w)
	defer func() {
		w.mu.Lock()
		w.open = false
		w.mu.Unlock()
		if recover() != nil {
			settled = false
			err = unavailable(nil)
		}
	}()
	scope, e := i.InProject(round.claim.project)
	if e != nil {
		return false, invalid()
	}
	actor, e := st.registration.Actor(round.claim.operation.String(), scope)
	if e != nil {
		return false, invalid()
	}
	a, b := st.terminalAgents.ProjectTerminalCleanupIdentity(), st.terminalVariables.ProjectTerminalCleanupIdentity()
	var anchor *ac.ProjectRetirementPlan
	if round.terminalConfirm && !round.terminalUnapplied {
		receipt, e := decodeTerminalCleanupCheckpoint(round.checkpoint, round.cause, round.claim.project)
		if e != nil {
			return false, e
		}
		a, b = receipt.AgentOwner, receipt.VariablesOwner
	}
	reqA := c.ProjectTerminalCleanupRequest{Actor: actor, Cause: round.cause, ProjectID: round.claim.project, Owner: a, Confirmation: round.terminalConfirm, NotApplied: round.terminalUnapplied}
	reqB := reqA
	reqB.Owner = b
	locks := reqA.RequiredLocks()
	if !round.terminalConfirm {
		_, progress, e := decodeVariablesCleanupCheckpoint(round.checkpoint, round.cause, round.claim.project)
		if e != nil {
			return st.agentStepError(ctx, round, e)
		}
		if saved := progress.Applied; saved != nil {
			plan, e := st.terminalAgents.RestoreProjectTerminalAnchor(run, reqA, saved.Agent, saved.Version, saved.Command, saved.Digest, saved.BeforeUpdatedAt)
			if e != nil {
				return st.agentStepError(ctx, round, portError(e))
			}
			if e = agentCleanupPlanMatches(plan, actor, round); e != nil {
				return st.agentStepError(ctx, round, e)
			}
			more, e := st.terminalAgents.ProjectTerminalAnchorLocks(plan.Clone())
			if e != nil {
				return st.agentStepError(ctx, round, portError(e))
			}
			locks = append(locks, more...)
			anchor = &plan
		}
	}
	locks, e = oc.NormalizeLocks(locks)
	if e != nil || len(locks) > 1024 {
		return false, invalid()
	}
	validated := false
	result, e := st.terminalAgents.WithProjectTerminalCleanup(run, reqA, func(run context.Context) (f.CommitResult, error) {
		return st.terminalVariables.WithProjectTerminalCleanup(run, reqB, func(run context.Context) (f.CommitResult, error) {
			result := st.store.WithinTx(run, round.claim.cause(), func(txctx context.Context, tx f.Tx) error {
				if e := st.store.AcquireAll(txctx, tx, locks); e != nil {
					return portError(e)
				}
				if round.terminalConfirm {
					if round.terminalUnapplied {
						if e := st.terminalAgents.RequireProjectTerminalUnappliedInTx(txctx, tx, reqA); e != nil {
							return portError(e)
						}
						if e := st.terminalVariables.RequireProjectTerminalUnappliedInTx(txctx, tx, reqB); e != nil {
							return portError(e)
						}
						if e := st.finishTerminalUnapplied(txctx, tx, round); e != nil {
							return e
						}
					} else {
						if e := st.terminalAgents.RequireProjectTerminalRetiredInTx(txctx, tx, reqA); e != nil {
							return portError(e)
						}
						if e := st.terminalVariables.RequireProjectTerminalRetiredInTx(txctx, tx, reqB); e != nil {
							return portError(e)
						}
					}
				} else {
					if anchor != nil {
						if e := st.terminalAgents.RequireProjectTerminalAnchorRetiredInTx(txctx, tx, anchor.Clone()); e != nil {
							return portError(e)
						}
					}
					if e := st.terminalAgents.RetireProjectTerminalInTx(txctx, tx, reqA); e != nil {
						return portError(e)
					}
					if e := st.terminalVariables.RetireProjectTerminalInTx(txctx, tx, reqB); e != nil {
						return portError(e)
					}
					x, e := st.store.InTx(tx)
					if e != nil {
						return portError(e)
					}
					var transaction string
					if e = x.QueryRow(txctx, `SELECT pg_current_xact_id()::text`).Scan(&transaction); e != nil {
						return unavailable(e)
					}
					report, e := terminalCleanupReport(round, a, b, transaction)
					if e != nil {
						return e
					}
					if e = st.finishInTx(txctx, tx, round, report, nil); e != nil {
						return e
					}
				}
				validated = true
				return portError(txctx.Err())
			})
			return result, commitError(result)
		})
	})
	if e != nil {
		return false, portError(e)
	}
	if e = commitError(result); e != nil {
		return false, e
	}
	if !validated {
		return false, unavailable(nil)
	}
	if e = run.Err(); e != nil {
		return false, portError(e)
	}
	return true, nil
}
func (st *lifecycleCleanupState) finishTerminalUnapplied(ctx context.Context, tx f.Tx, round lifecycleCleanupRound) error {
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	actual, err := loadLifecycleStopClaim(ctx, x, round.claim.operation)
	if err != nil {
		return err
	}
	if actual == nil || *actual != round.claim || actual.phase != "running" {
		return fault(f.ResourceBusy)
	}
	// The retained original claim remains running. Only the original driver
	// learns that this read-only confirmation has actually returned.
	return portError(ctx.Err())
}
