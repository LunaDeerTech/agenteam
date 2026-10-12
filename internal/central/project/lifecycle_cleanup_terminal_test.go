package project

import (
	"context"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type terminalClaimReadStore struct {
	Store
	claim            lifecycleStopClaim
	entered, release chan struct{}
}

func (s *terminalClaimReadStore) InTx(f.Tx) (postgres.SQLExecutor, error) { return s, nil }
func (s *terminalClaimReadStore) QueryRow(context.Context, string, ...any) postgres.Row {
	return rowFunc(func(out ...any) error {
		if s.entered != nil {
			close(s.entered)
			<-s.release
		}
		*out[0].(*string) = s.claim.project.String()
		*out[1].(*string) = s.claim.process.String()
		*out[2].(*string) = s.claim.attempt
		*out[3].(*int64) = s.claim.fence
		*out[4].(*string) = s.claim.phase
		return nil
	})
}

func TestLifecycleCleanupTerminalUnappliedKeepsOriginalClaim(t *testing.T) {
	for _, mode := range []string{"original-read-return", "second-local-driver", "next-explicit-run"} {
		t.Run(mode, func(t *testing.T) {
			d, round, _, _ := terminalDriverFixture(t)
			round.terminalStep = true
			round.terminalConfirm = true
			round.terminalUnapplied = true
			old := round
			d.state.owners[round.claim.operation] = lifecycleCleanupOwner{project: round.claim.project, claim: &old.claim, terminal: &old}
			if mode == "second-local-driver" {
				other, _, _, _ := terminalDriverFixture(t)
				other.state.process = d.state.process
				hasCode(t, other.state.canJoin(context.Background(), &round.claim), f.ResourceBusy)
				return
			}
			if mode == "next-explicit-run" {
				old.terminalUnappliedConfirmed = true
				next := round
				next.claim.attempt = testID[struct{}](t).String()
				next.claim.fence++
				next.terminalConfirm = false
				next.terminalUnapplied = false
				selected, err := d.state.selectTerminalRound(&next, &round.claim, round.checkpoint, round.operationVersion, round.rowVersion)
				if err != nil || !selected || next.terminalConfirm || next.claim.attempt == round.claim.attempt {
					t.Fatal("confirmed owner did not select a new explicit attempt", err)
				}
				return
			}
			store := &terminalClaimReadStore{claim: round.claim, entered: make(chan struct{}), release: make(chan struct{})}
			d.state.store = store
			var once sync.Once
			free := func() { once.Do(func() { close(store.release) }) }
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				free()
				if !joined {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("claim read still running")
					}
				}
			})
			go func() { done <- d.state.finishTerminalUnapplied(ctx, f.NewTx(), round) }()
			select {
			case <-store.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("original claim read not entered")
			}
			cancel()
			select {
			case <-done:
				joined = true
				t.Fatal("cancel replaced original read")
			default:
			}
			free()
			select {
			case err := <-done:
				joined = true
				if err == nil {
					t.Fatal("cancelled read accepted")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("original read did not return")
			}
			if store.claim.phase != "running" {
				t.Fatal("unapplied confirmation published a terminal claim")
			}
			// Any Exec would dispatch the nil embedded Store and fail the test.
		})
	}
}

type cleanupTerminalPort struct {
	ac.ProjectTerminalCleanupDomain
	id   c.ProjectTerminalOwnerIdentity
	with func(context.Context, c.ProjectTerminalCleanupRequest, c.ProjectTerminalCleanupCall) (f.CommitResult, error)
}

func (p *cleanupTerminalPort) ProjectTerminalCleanupIdentity() c.ProjectTerminalOwnerIdentity {
	return p.id
}
func (p *cleanupTerminalPort) WithProjectTerminalCleanup(ctx context.Context, req c.ProjectTerminalCleanupRequest, fn c.ProjectTerminalCleanupCall) (f.CommitResult, error) {
	return p.with(ctx, req, fn)
}
func terminalDriverFixture(t *testing.T) (*LifecycleCleanupDriver, lifecycleCleanupRound, *cleanupTerminalPort, *cleanupTerminalPort) {
	t.Helper()
	d, _, round, _, _ := cleanupVariablesFixture(t)
	a := &cleanupTerminalPort{id: c.ProjectTerminalOwnerIdentity{Kind: c.ProjectTerminalAgentOwner, ProcessID: d.state.process.String(), InstanceID: testID[struct{}](t).String()}}
	b := &cleanupTerminalPort{id: c.ProjectTerminalOwnerIdentity{Kind: c.ProjectTerminalVariablesOwner, ProcessID: d.state.process.String(), InstanceID: testID[struct{}](t).String()}}
	d.state.terminalAgents, d.state.terminalVariables = a, b
	round.checkpoint = cleanupVariablesReady(t, round)
	report, err := variablesParentCleanupReport(round, cleanupSkillsChild(t, round, c.CleanupCompleted, nil))
	if err != nil {
		t.Fatal(err)
	}
	round.checkpoint = report.Details().Checkpoint
	return d, round, a, b
}
func TestLifecycleCleanupTerminalSelection(t *testing.T) {
	for _, mode := range []string{"variables-pending", "ready", "receipt", "original-unknown", "wrong-original-checkpoint", "old-constructor"} {
		t.Run(mode, func(t *testing.T) {
			d, round, a, b := terminalDriverFixture(t)
			cp := round.checkpoint
			actual := round.claim
			actual.phase = "terminal"
			switch mode {
			case "variables-pending":
				r := round
				r.checkpoint = cleanupVariablesReady(t, round)
				report, e := variablesParentCleanupReport(r, cleanupSkillsChild(t, round, c.CleanupPending, nil))
				if e != nil {
					t.Fatal(e)
				}
				cp = report.Details().Checkpoint
			case "receipt":
				report, e := terminalCleanupReport(round, a.id, b.id, "42")
				if e != nil {
					t.Fatal(e)
				}
				cp = report.Details().Checkpoint
			case "original-unknown", "wrong-original-checkpoint":
				old := round
				if mode == "wrong-original-checkpoint" {
					old.checkpoint = cleanupVariablesReady(t, round)
				}
				d.state.owners[round.claim.operation] = lifecycleCleanupOwner{project: round.claim.project, claim: &old.claim, terminal: &old}
				actual.phase = "running"
			case "old-constructor":
				d.state.terminalAgents = nil
				_, _, replay, e := d.state.selectDomainStep(cp, round.cause, round.claim.project)
				if e != nil || !replay {
					t.Fatal("old Variables completion changed", e)
				}
				return
			}
			selected, err := d.state.selectTerminalRound(&round, &actual, cp, round.operationVersion, round.rowVersion)
			if mode == "wrong-original-checkpoint" {
				if err == nil {
					t.Fatal("Unknown original checkpoint drift accepted")
				}
				return
			}
			if err != nil || selected != (mode != "variables-pending") {
				t.Fatal("incorrect terminal selection", selected, err)
			}
			if mode == "receipt" && (!round.terminalConfirm || round.terminalUnapplied) {
				t.Fatal("receipt reissued deletion")
			}
			if mode == "original-unknown" && (!round.terminalConfirm || !round.terminalUnapplied || round.claim != actual) {
				t.Fatal("Unknown rollback replaced original claim")
			}
			if mode == "ready" && (round.terminalConfirm || round.agentStep || round.variablesStep) {
				t.Fatal("explicit terminal round mixed earlier domain")
			}
		})
	}
}
func TestLifecycleCleanupTerminalRejectsOtherFamilies(t *testing.T) {
	d, round, _, _ := terminalDriverFixture(t)
	round.terminalStep = true
	w := &lifecycleCleanupScope{state: d.state, round: round, open: true}
	ctx := context.WithValue(context.Background(), lifecycleCleanupScopeKey{}, w)
	for _, name := range []string{"agent", "skills", "variables"} {
		t.Run(name, func(t *testing.T) {
			var err error
			switch name {
			case "agent":
				err = d.state.authority.requireAgentCleanupStepInTx(ctx, f.NewTx(), round.cause, round.claim.project)
			case "skills":
				err = d.state.authority.requireSkillsCleanupClaimInTx(ctx, f.NewTx(), round.cause, round.claim.project)
			case "variables":
				err = d.state.authority.requireVariablesCleanupClaimInTx(ctx, f.NewTx(), round.cause, round.claim.project)
			}
			hasCode(t, err, f.Forbidden)
		})
	}
}
func TestLifecycleCleanupTerminalOriginalOwnerReturn(t *testing.T) {
	for _, mode := range []string{"unknown", "false-commit", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			d, round, a, b := terminalDriverFixture(t)
			report, e := terminalCleanupReport(round, a.id, b.id, "71")
			if e != nil {
				t.Fatal(e)
			}
			round.checkpoint = report.Details().Checkpoint
			round.claim.phase = "terminal"
			round.terminalStep, round.terminalConfirm = true, true
			cause, _ := f.NewRecoveryCause("terminal-owner-control", round.claim.operation.String(), round.claim.project.String())
			unknown := f.UnknownResult(testID[f.TransactionAttempt](t), cause)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			var saved context.Context
			a.with = func(ctx context.Context, req c.ProjectTerminalCleanupRequest, fn c.ProjectTerminalCleanupCall) (f.CommitResult, error) {
				if !req.Confirmation || req.NotApplied {
					t.Error("receipt confirmation mode lost")
				}
				return fn(ctx)
			}
			b.with = func(ctx context.Context, req c.ProjectTerminalCleanupRequest, _ c.ProjectTerminalCleanupCall) (f.CommitResult, error) {
				saved = ctx
				close(entered)
				<-release
				if mode == "false-commit" {
					return f.CommittedResult(), nil
				}
				return unknown, commitError(unknown)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				free()
				if !joined {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("original owner still running")
					}
				}
			})
			go func() {
				settled, err := d.state.runTerminalStep(ctx, round)
				if settled {
					t.Error("unconfirmed owner settled lifecycle")
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("second original owner not entered")
			}
			if mode == "cancelled" {
				cancel()
			}
			select {
			case <-done:
				joined = true
				t.Fatal("cancel replaced actual owner return")
			default:
			}
			free()
			var err error
			select {
			case err = <-done:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("original owner failed to return")
			}
			if err == nil {
				t.Fatal("unconfirmed callback accepted")
			}
			if mode != "false-commit" {
				r, ok := UnknownAttempt(err)
				if !ok || r.AttemptID() != unknown.AttemptID() {
					t.Fatal("lost original attempt", err)
				}
			}
			w, _ := saved.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
			if w == nil || w.open {
				t.Fatal("saved callback scope remained open")
			}
		})
	}
}
