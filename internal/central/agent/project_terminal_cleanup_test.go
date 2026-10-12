package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

type agentTerminalAuthorityControl struct {
	call func(context.Context, f.Tx, pc.ProjectTerminalCleanupRequest) error
}

func (a agentTerminalAuthorityControl) RequireProjectTerminalCleanupInTx(ctx context.Context, tx f.Tx, req pc.ProjectTerminalCleanupRequest) error {
	return a.call(ctx, tx, req)
}
func terminalAgentFixture(t *testing.T) (*ownershipControl, *ProjectTerminalCleanup, pc.ProjectTerminalCleanupRequest) {
	t.Helper()
	h := newOwnershipControl(t)
	r, err := NewProjectRetirement(h.deps.Authority, &project.Authority{}, retirementDependencies())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewProjectTerminalCleanup(h.pair, r)
	if err != nil {
		t.Fatal(err)
	}
	actor, cause, scope := h.lifecycle(pc.Delete)
	req := pc.ProjectTerminalCleanupRequest{Actor: actor, Cause: cause, ProjectID: scope.ProjectID, Owner: owner.ProjectTerminalCleanupIdentity()}
	if err = req.Validate(); err != nil {
		t.Fatal(err)
	}
	return h, owner, req
}
func TestAgentTerminalCleanupOriginalPairAndScope(t *testing.T) {
	h, owner, req := terminalAgentFixture(t)
	for _, mode := range []string{"nil-pair", "other-authority", "public-context", "closed-context", "wrong-mode", "foreign-instance"} {
		t.Run(mode, func(t *testing.T) {
			var err error
			switch mode {
			case "nil-pair":
				_, err = NewProjectTerminalCleanup(nil, owner.retirement)
			case "other-authority":
				r, _ := retirementConsumer(t, h.store)
				_, err = NewProjectTerminalCleanup(h.pair, r)
			case "public-context":
				err = owner.RetireProjectTerminalInTx(context.Background(), f.NewTx(), req)
			case "closed-context":
				w := &agentTerminalScope{owner: owner, req: req}
				err = owner.RetireProjectTerminalInTx(context.WithValue(context.Background(), agentTerminalScopeKey{}, w), f.NewTx(), req)
			case "wrong-mode":
				r := req
				r.Confirmation = true
				err = owner.RetireProjectTerminalInTx(context.Background(), f.NewTx(), r)
			case "foreign-instance":
				r := req
				r.Owner.InstanceID = retirementPreparationID[struct{}](t).String()
				_, err = owner.WithProjectTerminalCleanup(context.Background(), r, func(context.Context) (f.CommitResult, error) {
					t.Error("foreign owner entered callback")
					return f.CommittedResult(), nil
				})
			}
			if err == nil {
				t.Fatal("invalid owner/scope accepted")
			}
			if len(h.store.causes) != 0 {
				t.Fatal("invalid scope started SQL")
			}
		})
	}
}
func TestAgentTerminalCleanupProjectIsolation(t *testing.T) {
	for _, mode := range []string{"local-service", "local-directory", "retained-original", "other-project"} {
		t.Run(mode, func(t *testing.T) {
			h, owner, req := terminalAgentFixture(t)
			p := req.ProjectID
			if mode == "other-project" {
				p = retirementPreparationID[i.Project](t)
			}
			set := h.pair.state.service.state.calls
			if mode == "local-directory" {
				set = h.pair.state.directory.state.calls
			}
			_, call, done, err := set.beginProject(context.Background(), p, readCall)
			if err != nil {
				t.Fatal(err)
			}
			defer done()
			if mode == "retained-original" {
				done()
				id := retirementPreparationID[projectCallIdentity](t)
				h.pair.state.owners[id] = &projectCallOwner{record: projectCallRecord{Project: p}, call: call, returned: true}
				defer delete(h.pair.state.owners, id)
			}
			if owner.pair.terminalPending(req.ProjectID) != (mode != "other-project") {
				t.Fatal("Project-specific original ownership lost")
			}
		})
	}
}
func TestAgentTerminalCleanupPreflightOriginalReturn(t *testing.T) {
	for _, mode := range []string{"unknown", "rollback", "false-commit", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			h, owner, req := terminalAgentFixture(t)
			cause, _ := f.NewRecoveryCause("terminal-control", req.Cause.OperationID.String(), req.ProjectID.String())
			unknown := f.UnknownResult(retirementPreparationID[f.TransactionAttempt](t), cause)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			h.store.before = func(context.Context, f.TransactionCause) *f.CommitResult {
				close(entered)
				<-release
				r := unknown
				if mode == "rollback" {
					r = f.NotCommittedResult(fault(f.InvalidState))
				}
				if mode == "false-commit" {
					r = f.CommittedResult()
				}
				return &r
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
						t.Error("original owner did not return")
					}
				}
			})
			go func() {
				_, err := owner.WithProjectTerminalCleanup(ctx, req, func(context.Context) (f.CommitResult, error) {
					t.Error("unconfirmed preflight admitted mutation")
					return f.CommittedResult(), nil
				})
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("preflight did not enter")
			}
			if mode == "cancelled" {
				cancel()
			}
			h.pair.Stop()
			if h.pair.Joined() {
				t.Fatal("original Store return replaced by stop")
			}
			select {
			case <-done:
				joined = true
				t.Fatal("owner returned before original Store")
			default:
			}
			free()
			var err error
			select {
			case err = <-done:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("Store release did not return")
			}
			if err == nil {
				t.Fatal("unconfirmed preflight succeeded")
			}
			if mode == "unknown" || mode == "cancelled" {
				r, ok := UnknownAttempt(err)
				if !ok || r.AttemptID() != unknown.AttemptID() {
					t.Fatal("lost original preflight Unknown", err)
				}
			}
			if !h.pair.Joined() {
				t.Fatal("read-only preflight retained fictitious mutation owner")
			}
		})
	}
}

type agentTerminalRow func(...any) error

func (r agentTerminalRow) Scan(out ...any) error { return r(out...) }
func TestAgentTerminalCleanupUnknownConfirmation(t *testing.T) {
	for _, mode := range []string{"committed", "not-applied", "confirmation-return"} {
		t.Run(mode, func(t *testing.T) {
			h, owner, req := terminalAgentFixture(t)
			// Only authority/SQL outcomes are controlled here. The concrete pair's
			// admission, InTx scope and both original return controls are real.
			owner.authority = agentTerminalAuthorityControl{func(ctx context.Context, tx f.Tx, r pc.ProjectTerminalCleanupRequest) error {
				return h.store.RequireHeldLocks(ctx, tx, r.RequiredLocks())
			}}
			counts := map[string]int64{"commands": 2, "project_calls": 3}
			writes := 0
			h.store.row = func(_ context.Context, _ f.Tx, q string, _ []any) postgres.Row {
				return agentTerminalRow(func(out ...any) error {
					switch {
					case strings.Contains(q, "jsonb_agg"):
						*out[0].(*[]byte) = []byte(`[]`)
					case strings.Contains(q, "SELECT EXISTS"):
						*out[0].(*bool) = false
					case strings.Contains(q, "count(*)"):
						table := "commands"
						if strings.Contains(q, "project_calls") {
							table = "project_calls"
						}
						*out[0].(*int64) = counts[table]
					default:
						return errors.New("unexpected SQL")
					}
					return nil
				})
			}
			h.store.exec = func(_ context.Context, _ f.Tx, q string, _ []any) (pgconn.CommandTag, error) {
				table := "commands"
				if strings.Contains(q, "project_calls") {
					table = "project_calls"
				}
				n := counts[table]
				counts[table] = 0
				writes++
				if n == 2 {
					return pgconn.NewCommandTag("DELETE 2"), nil
				}
				return pgconn.NewCommandTag("DELETE 3"), nil
			}
			cause, _ := f.NewRecoveryCause("terminal-test-apply", req.Cause.OperationID.String(), req.ProjectID.String())
			unknown := f.UnknownResult(retirementPreparationID[f.TransactionAttempt](t), cause)
			apply := func(ctx context.Context) (f.CommitResult, error) {
				result := h.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
					if err := h.store.AcquireAll(ctx, tx, req.RequiredLocks()); err != nil {
						return err
					}
					return owner.RetireProjectTerminalInTx(ctx, tx, req)
				})
				if result.State() != f.Committed {
					t.Error("controlled apply rejected", commitError(result))
				}
				if mode == "not-applied" {
					counts["commands"], counts["project_calls"] = 2, 3
				}
				return unknown, commitError(unknown)
			}
			_, err := owner.WithProjectTerminalCleanup(context.Background(), req, apply)
			saved, ok := UnknownAttempt(err)
			if !ok || saved.AttemptID() != unknown.AttemptID() {
				t.Fatal("original unknown lost", err)
			}
			other, err := NewProjectTerminalCleanup(h.pair, owner.retirement)
			if err != nil {
				t.Fatal(err)
			}
			other.authority = owner.authority
			if _, err = other.WithProjectTerminalCleanup(context.Background(), req, apply); err == nil {
				t.Fatal("second constructor repeated Unknown mutation")
			}
			confirm := req
			confirm.Confirmation = true
			confirm.NotApplied = mode == "not-applied"
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			original := writes
			fn := func(ctx context.Context) (f.CommitResult, error) {
				result := h.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
					if e := h.store.AcquireAll(ctx, tx, confirm.RequiredLocks()); e != nil {
						return e
					}
					if confirm.NotApplied {
						return other.RequireProjectTerminalUnappliedInTx(ctx, tx, confirm)
					}
					return other.RequireProjectTerminalRetiredInTx(ctx, tx, confirm)
				})
				if mode == "confirmation-return" {
					close(entered)
					<-release
				}
				return result, commitError(result)
			}
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				free()
				if !joined {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("confirmation did not return")
					}
				}
			})
			go func() { _, e := other.WithProjectTerminalCleanup(context.Background(), confirm, fn); done <- e }()
			if mode == "confirmation-return" {
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("confirmation not entered")
				}
				if len(owner.owners.calls) != 1 {
					t.Fatal("Unknown owner released before original confirmation")
				}
				select {
				case <-done:
					joined = true
					t.Fatal("confirmation return detached")
				default:
				}
				free()
			}
			select {
			case err = <-done:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("confirmation did not finish")
			}
			if err != nil || writes != original || len(owner.owners.calls) != 0 {
				t.Fatal("confirmation rewrote metadata or failed to release original", err, writes, original)
			}
		})
	}
}
