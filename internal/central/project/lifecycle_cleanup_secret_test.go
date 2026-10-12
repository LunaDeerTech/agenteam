package project

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestLifecycleCleanupSecretConstructionAndOrder(t *testing.T) {
	legacy, round, a, b := terminalDriverFixture(t)
	st := legacy.state
	projects, err := NewAuthority(st.store, AuthorityDependencies{Sessions: sessionFunc(func(context.Context, f.Tx, i.Actor) error { t.Fatal("Human authorization used"); return nil }), Lifecycle: st.authority})
	if err != nil {
		t.Fatal(err)
	}
	secret := &cleanupDriverPort{name: c.SecretParticipant}
	driver, err := NewLifecycleCleanupDriverWithSecret(st.store, projects, st.processes, st.agents, st.skills, st.variables, a, b, secret)
	if err != nil || driver.state.secret != secret || driver.state.authority != st.authority {
		t.Fatal("fixed owner construction", err)
	}
	for _, bad := range []c.ProjectLifecycleParticipant{nil, (*cleanupDriverPort)(nil), &cleanupDriverPort{name: c.OutboxParticipant}} {
		if _, err := NewLifecycleCleanupDriverWithSecret(st.store, projects, st.processes, st.agents, st.skills, st.variables, a, b, bad); err == nil {
			t.Fatal("invalid Secret binding")
		}
	}
	for _, state := range []string{"parent-pending", "secret-required", "secret-pending", "secret-completed", "legacy"} {
		t.Run(state, func(t *testing.T) {
			r := secretCleanupRecord(t)
			use := driver
			want, replay := c.SecretParticipant, false
			switch state {
			case "parent-pending":
				skillsCleanupSet(r, c.SkillsParticipant, "stopped", "pending")
				want = c.SkillsParticipant
			case "secret-pending":
				skillsCleanupSet(r, c.SecretParticipant, "stopped", "pending")
			case "secret-completed":
				skillsCleanupSet(r, c.SecretParticipant, "stopped", "completed")
				replay = true
			case "legacy":
				use = legacy
				want = c.SkillsParticipant
			}
			got, actual, err := use.state.cleanupProgress(r)
			if err != nil || got != want || actual != replay {
				t.Fatal("fixed stage bypass", got, actual, err)
			}
			if r.cleanupStage != "domains" {
				t.Fatal("advanced to unrelated domain")
			}
		})
	}
	if current, next, err := cleanupStages(c.SecretParticipant); err != nil || current != "domains" || next != "domains" {
		t.Fatal("Secret advanced Outbox")
	}
	round.participant = c.SecretParticipant
	if err := cleanupRoundReport(round, c.CleanupReport{}); err == nil {
		t.Fatal("empty provider result accepted")
	}
}

type secretParentRead struct {
	postgres.SQLExecutor
	cp               *c.CleanupCheckpoint
	entered, release chan struct{}
}

func (s *secretParentRead) QueryRow(_ context.Context, _ string, args ...any) postgres.Row {
	return rowFunc(func(out ...any) error {
		if len(args) != 2 || args[1] != string(c.SkillsParticipant) {
			return errors.New("wrong parent query")
		}
		if s.entered != nil {
			close(s.entered)
			<-s.release
		}
		*out[0].(*int64), *out[1].(*int64) = 7, 1
		schema := int64(s.cp.Schema())
		*out[2].(**int64) = &schema
		*out[3].(*[]byte) = s.cp.Bytes()
		return nil
	})
}
func secretParentFixture(t *testing.T) (*LifecycleCleanupDriver, lifecycleCleanupRound, *secretParentRead, lifecycleStopClaim) {
	t.Helper()
	d, base, a, b := terminalDriverFixture(t)
	report, err := terminalCleanupReport(base, a.id, b.id, "42")
	if err != nil {
		t.Fatal(err)
	}
	actual := base.claim
	actual.phase = "terminal"
	round := base
	round.participant = c.SecretParticipant
	round.checkpoint = nil
	round.claim.attempt = testID[struct{}](t).String()
	round.claim.fence++
	d.state.secret = &cleanupDriverPort{name: c.SecretParticipant}
	return d, round, &secretParentRead{cp: report.Details().Checkpoint}, actual
}
func TestLifecycleCleanupSecretParentHandoff(t *testing.T) {
	for _, mode := range []string{"first-confirmation", "confirmed-exact", "changed-claim", "changed-receipt", "unconfirmed-marker", "continued-batch", "count-with-old-claim", "stale-confirmation"} {
		t.Run(mode, func(t *testing.T) {
			d, round, reader, actual := secretParentFixture(t)
			first := round
			selected, err := d.state.selectSecretParent(context.Background(), reader, &first, &actual, 0, round.operationVersion, nil)
			if err != nil || !selected || !first.secretParentStep || !first.terminalConfirm || first.participant != c.SkillsParticipant || first.claim != actual {
				t.Fatal("first Secret call did not select original terminal confirmation", err)
			}
			if mode == "first-confirmation" {
				return
			}
			confirmed := &first
			attempts := int64(0)
			good := mode == "confirmed-exact" || mode == "continued-batch"
			switch mode {
			case "changed-claim":
				actual.attempt = testID[struct{}](t).String()
			case "changed-receipt":
				raw := bytes.Replace(reader.cp.Bytes(), []byte(`"transaction_id":"42"`), []byte(`"transaction_id":"43"`), 1)
				cp, e := c.NewCleanupCheckpoint(c.SkillsParticipant, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}, 6, raw)
				if e != nil {
					t.Fatal(e)
				}
				reader.cp = &cp
			case "unconfirmed-marker":
				confirmed.secretParentStep = false
			case "continued-batch":
				attempts = 1
				actual = round.claim
				confirmed = nil
			case "count-with-old-claim":
				attempts = 1
				confirmed = nil
			case "stale-confirmation":
				attempts = 1
				actual = round.claim
			}
			selected, err = d.state.selectSecretParent(context.Background(), reader, &round, &actual, attempts, round.operationVersion, confirmed)
			if good {
				if err != nil || selected || round.secretParentCheckpoint == nil || !bytes.Equal(round.secretParentCheckpoint.Bytes(), reader.cp.Bytes()) {
					t.Fatal("valid handoff lost exact parent", err)
				}
			} else if err == nil {
				t.Fatal("unconfirmed or changed parent authorized Secret")
			}
		})
	}
}

func TestLifecycleCleanupSecretOriginalReturn(t *testing.T) {
	for _, mode := range []string{"pending", "completed", "unknown", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			d, round, _, _ := secretParentFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			ctx, cancel := context.WithCancel(context.Background())
			type outcome struct {
				report c.CleanupReport
				err    error
			}
			done := make(chan outcome, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				free()
				if !joined {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("original provider did not return")
					}
				}
			})
			cause, e := f.NewRecoveryCause("secret-project-cleanup", round.claim.operation.String(), "")
			if e != nil {
				t.Fatal(e)
			}
			unknown := f.UnknownResult(testID[f.TransactionAttempt](t), cause)
			var saved context.Context
			d.state.secret = &cleanupDriverPort{name: c.SecretParticipant, call: func(got context.Context, actor i.Actor, actual c.LifecycleCause, scope c.ScopeRef, cp *c.CleanupCheckpoint) (c.CleanupReport, error) {
				saved = got
				if actual != round.cause || scope.ProjectID != round.claim.project || cp != nil || actor.Details().CauseRef != round.claim.operation.String() {
					t.Error("original call input changed")
				}
				close(entered)
				<-release
				if mode == "unknown" {
					return c.CleanupReport{}, commitError(unknown)
				}
				op, _ := f.ParseID[sc.LifecycleOperation](round.claim.operation.String())
				return secretCleanupReport(actual, scope, sc.CleanupReport{Completed: mode != "pending", Checkpoint: sc.CleanupCheckpoint{ProjectID: scope.ProjectID, OperationID: op}})
			}}
			go func() { r, e := d.state.invoke(ctx, round); done <- outcome{r, e} }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("provider not entered")
			}
			if mode == "cancelled" {
				cancel()
			}
			select {
			case <-done:
				joined = true
				t.Fatal("original call replaced by cancellation")
			default:
			}
			free()
			var out outcome
			select {
			case out = <-done:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("released provider not returned")
			}
			witness := saved.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
			if witness.open {
				t.Fatal("saved scope revived")
			}
			if mode == "unknown" {
				attempt, ok := UnknownAttempt(out.err)
				if !ok || attempt.AttemptID() != unknown.AttemptID() || out.report.Details().State != "" {
					t.Fatal("original Unknown lost", out.err)
				}
			} else if mode == "cancelled" {
				if out.err == nil || out.report.Details().State != "" {
					t.Fatal("cancelled report published")
				}
			} else {
				want := c.CleanupCompleted
				if mode == "pending" {
					want = c.CleanupPending
				}
				if out.err != nil || out.report.Details().State != want || out.report.Details().Checkpoint == nil {
					t.Fatal("provider report altered", out.err)
				}
			}
		})
	}
}
