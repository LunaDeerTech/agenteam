package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// This controlled callback has no Name or aggregate lifecycle methods. Real
// Skills/Object cleanup and persisted parent progress are exercised by PG.
type cleanupSkillsPort struct {
	call func(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error)
}

func (p *cleanupSkillsPort) Cleanup(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef, cp *c.CleanupCheckpoint) (c.CleanupReport, error) {
	return p.call(ctx, actor, cause, scope, cp)
}

func cleanupSkillsFixture(t *testing.T, store Store, port *cleanupSkillsPort) (*LifecycleCleanupDriver, *Authority, lifecycleCleanupRound) {
	t.Helper()
	legacy, round := cleanupDriverFixture(t, store, nil)
	projects, err := NewAuthority(store, AuthorityDependencies{Sessions: sessionFunc(func(context.Context, f.Tx, identity.Actor) error {
		t.Error("cleanup borrowed Human session authority")
		return nil
	}), Lifecycle: legacy.state.authority})
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewLifecycleCleanupDriverForSkills(store, projects, legacy.state.processes, port)
	if err != nil {
		t.Fatal(err)
	}
	round.participant = c.SkillsParticipant
	return d, projects, round
}

func cleanupSkillsChild(t *testing.T, round lifecycleCleanupRound, state c.CleanupState, cp *c.CleanupCheckpoint) c.CleanupReport {
	t.Helper()
	details := c.CleanupDetails{State: state, Checkpoint: cp}
	if state == c.CleanupPending {
		details.SafeReason = c.ReasonWorkPending
	}
	report, err := c.NewCleanupReport(c.SkillsParticipant, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}, details)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestLifecycleCleanupSkillsConstructionAndDomain(t *testing.T) {
	store, port := &stopRoundGateStore{}, &cleanupSkillsPort{}
	d, projects, _ := cleanupSkillsFixture(t, store, port)
	if d.state.authority != projects.state().lifecycle || d.state.skills != port || d.state.participant != nil || d.state.audit != nil {
		t.Fatal("Skills substep changed authority or bound downstream ports")
	}
	if _, aggregate := any(port).(c.ProjectLifecycleParticipant); aggregate {
		t.Fatal("controlled Skills domain became an aggregate participant")
	}
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"missing-projects", func() error {
			_, err := NewLifecycleCleanupDriverForSkills(store, nil, d.state.processes, port)
			return err
		}},
		{"foreign-store", func() error {
			_, err := NewLifecycleCleanupDriverForSkills(&stopRoundGateStore{}, projects, d.state.processes, port)
			return err
		}},
		{"typed-nil-domain", func() error {
			_, err := NewLifecycleCleanupDriverForSkills(store, projects, d.state.processes, (*cleanupSkillsPort)(nil))
			return err
		}},
		{"invalid-process", func() error {
			_, err := NewLifecycleCleanupDriverForSkills(store, projects, &testProcess{}, port)
			return err
		}},
		{"missing-lifecycle", func() error {
			p, err := NewAuthority(store, AuthorityDependencies{Sessions: projects.state().sessions})
			if err != nil {
				return err
			}
			_, err = NewLifecycleCleanupDriverForSkills(store, p, d.state.processes, port)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatal("invalid Skills assembly accepted")
			}
		})
	}
	r := skillsCleanupRecord(t)
	name, replay, err := d.state.cleanupProgress(r)
	if err != nil || name != c.SkillsParticipant || replay {
		t.Fatal("Skills domain selection", name, replay, err)
	}
	for _, stage := range []string{"outbox", "audit", "final"} {
		t.Run(stage, func(t *testing.T) {
			r.cleanupStage = stage
			if _, _, err := d.state.cleanupProgress(r); err == nil {
				t.Fatal("Skills-only owner advanced downstream")
			}
		})
	}
	current, next, err := cleanupStages(c.SkillsParticipant)
	if err != nil || current != "domains" || next != "domains" || store.transactions != 0 {
		t.Fatal("Skills domain changed parent stage or performed construction I/O", err)
	}
}

func TestLifecycleCleanupSkillsCheckpointSeparatesChild(t *testing.T) {
	_, _, round := cleanupSkillsFixture(t, &stopRoundGateStore{}, &cleanupSkillsPort{})
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}
	// Deliberately opaque here: only the real Skills provider may interpret it.
	raw := []byte("provider-owned-exact-progress")
	childCP, err := c.NewCleanupCheckpoint(c.SkillsParticipant, round.cause, scope, 1, raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		state c.CleanupState
		cp    *c.CleanupCheckpoint
	}{
		{"pending-without-checkpoint", c.CleanupPending, nil},
		{"completed-without-checkpoint", c.CleanupCompleted, nil},
		{"pending-with-original-checkpoint", c.CleanupPending, &childCP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := cleanupSkillsChild(t, round, tc.state, tc.cp)
			parent, err := skillsParentCleanupReport(round, child)
			if err != nil || parent.Details().State != c.CleanupPending || parent.Details().SafeReason != c.ReasonWorkPending || parent.Details().Checkpoint.Schema() != 2 {
				t.Fatal("child completed the parent or lost pending checkpoint", err)
			}
			if err = cleanupRoundReport(round, child); err == nil {
				t.Fatal("unwrapped child consumed as aggregate report")
			}
			data, actual, err := decodeSkillsCleanupCheckpoint(parent.Details().Checkpoint, round.cause, scope)
			if err != nil || data.State != tc.state || (actual == nil) != (tc.cp == nil) {
				t.Fatal("lost explicit child state", err)
			}
			if actual != nil && (!actual.Matches(c.SkillsParticipant, round.cause, scope) || actual.Schema() != 1 || !bytes.Equal(actual.Bytes(), raw)) {
				t.Fatal("rewrote original provider checkpoint")
			}
		})
	}
	parent, err := skillsParentCleanupReport(round, cleanupSkillsChild(t, round, c.CleanupCompleted, nil))
	if err != nil {
		t.Fatal(err)
	}
	valid := parent.Details().Checkpoint.Bytes()
	for _, tc := range []struct {
		name   string
		schema f.Version
		edit   func([]byte) []byte
	}{
		{"provider-schema-is-not-parent", 1, func(b []byte) []byte { return b }},
		{"unknown-field", 2, func(b []byte) []byte {
			return append(append([]byte{}, b[:len(b)-1]...), []byte(",\"agent_completed\":true}")...)
		}},
		{"duplicate-field", 2, func(b []byte) []byte {
			return append(append([]byte{}, b[:len(b)-1]...), []byte(",\"skills_state\":\"completed\"}")...)
		}},
		{"null-checkpoint", 2, func(b []byte) []byte {
			return append(append([]byte{}, b[:len(b)-1]...), []byte(",\"skills_checkpoint\":null}")...)
		}},
		{"wrong-project", 2, func(b []byte) []byte {
			return bytes.Replace(b, []byte(round.claim.project.String()), []byte(testID[identity.Project](t).String()), 1)
		}},
		{"wrong-version", 2, func(b []byte) []byte {
			var data skillsCleanupCheckpoint
			if err := json.Unmarshal(b, &data); err != nil {
				t.Fatal(err)
			}
			data.Version++
			raw, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			return raw
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp, err := c.NewCleanupCheckpoint(c.SkillsParticipant, round.cause, scope, tc.schema, tc.edit(valid))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = decodeSkillsCleanupCheckpoint(&cp, round.cause, scope); err == nil {
				t.Fatal("invalid parent checkpoint accepted")
			}
		})
	}
}

func TestLifecycleCleanupSkillsOriginalReturnAndClaim(t *testing.T) {
	for _, mode := range []string{"completed", "completed-with-error", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			store := &retirementPrepStore{tx: f.NewTx(), live: true, held: map[string]f.LockMode{}}
			port := &cleanupSkillsPort{}
			d, _, round := cleanupSkillsFixture(t, store, port)
			scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}
			raw := []byte("original opaque child checkpoint")
			cp, err := c.NewCleanupCheckpoint(c.SkillsParticipant, round.cause, scope, 1, raw)
			if err != nil {
				t.Fatal(err)
			}
			prior, err := skillsParentCleanupReport(round, cleanupSkillsChild(t, round, c.CleanupPending, &cp))
			if err != nil {
				t.Fatal(err)
			}
			round.checkpoint = prior.Details().Checkpoint
			store.claim = &round.claim
			store.required = []f.LockRequest{projectLock(round.claim.project, f.Shared)}
			store.held[store.required[0].Key.Canonical()] = f.Exclusive
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			var saved context.Context
			failure := errors.New("controlled child error after completed result")
			completed := cleanupSkillsChild(t, round, c.CleanupCompleted, nil)
			port.call = func(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, target c.ScopeRef, got *c.CleanupCheckpoint) (c.CleanupReport, error) {
				saved = ctx
				if got == nil || got.Schema() != 1 || !bytes.Equal(got.Bytes(), raw) || cause != round.cause || !target.Equal(scope) || actor.Details().CauseRef != cause.OperationID.String() {
					t.Error("domain lost original actor/cause/provider checkpoint")
				}
				if err := d.state.authority.requireSkillsCleanupClaimInTx(ctx, store.tx, cause, target.ProjectID); err != nil {
					t.Error("controlled original claim rejected", err)
				}
				if err := d.state.authority.requireAuditCleanupClaimInTx(ctx, store.tx, cause, target.ProjectID); err == nil {
					t.Error("Skills borrowed Audit family")
				}
				close(entered)
				<-release
				if mode == "completed-with-error" {
					return completed, failure
				}
				return completed, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			type outcome struct {
				report c.CleanupReport
				err    error
			}
			returned := make(chan outcome, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				free()
				if !joined {
					select {
					case <-returned:
					case <-time.After(5 * time.Second):
						t.Error("Skills callback did not return")
					}
				}
			})
			go func() { report, err := d.state.invoke(ctx, round); returned <- outcome{report, err} }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("Skills callback not entered")
			}
			if mode == "cancelled" {
				cancel()
			}
			select {
			case <-returned:
				joined = true
				t.Fatal("cancellation fabricated callback return")
			default:
			}
			free()
			var got outcome
			select {
			case got = <-returned:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("Skills callback not returned")
			}
			if mode == "completed" {
				if got.err != nil || got.report.Details().State != c.CleanupPending {
					t.Fatal("Skills completed parent", got.err)
				}
				data, child, err := decodeSkillsCleanupCheckpoint(got.report.Details().Checkpoint, round.cause, scope)
				if err != nil || data.State != c.CleanupCompleted || child != nil {
					t.Fatal("lost exact child completion", err)
				}
			} else {
				want := failure
				if mode == "cancelled" {
					want = context.Canceled
				}
				if !errors.Is(got.err, want) || got.report.Details().State != "" {
					t.Fatal("failed child published progress", got.err)
				}
			}
			hasCode(t, d.state.authority.requireSkillsCleanupClaimInTx(context.WithoutCancel(saved), store.tx, round.cause, round.claim.project), f.Forbidden)
			hasCode(t, d.state.authority.requireSkillsCleanupClaimInTx(context.Background(), store.tx, round.cause, round.claim.project), f.Forbidden)
		})
	}
}

func TestLifecycleCleanupSkillsUnknownStartKeepsOwner(t *testing.T) {
	cause, err := f.NewJobCause("skills-driver-control", testID[c.Operation](t).String(), testID[struct{}](t).String())
	if err != nil {
		t.Fatal(err)
	}
	unknown := f.UnknownResult(testID[f.TransactionAttempt](t), cause)
	store := &stopRoundGateStore{commit: unknown}
	d, _, round := cleanupSkillsFixture(t, store, &cleanupSkillsPort{call: func(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error) {
		t.Error("unconfirmed claim started Skills cleanup")
		return c.CleanupReport{}, nil
	}})
	err = d.Run(context.Background(), round.claim.project, round.claim.operation)
	hasCode(t, err, f.CommitUnknown)
	observed, ok := UnknownAttempt(err)
	if !ok || observed.AttemptID() != unknown.AttemptID() || len(d.state.owners) != 1 || store.transactions != 1 {
		t.Fatal("lost original Unknown owner")
	}
	d.Stop()
	if err = d.Drain(context.Background()); err != nil || len(d.state.owners) != 1 {
		t.Fatal("local drain erased unknown owner", err)
	}
}
