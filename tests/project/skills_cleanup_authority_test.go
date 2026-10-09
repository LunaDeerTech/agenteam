//go:build integration

package project_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

const skillsCleanupPrerequisite c.ParticipantName = "skills-prerequisite"

// The extra predecessor is test-owned declaration metadata, not a production
// participant. Actual participant calls are prohibited by r2Registry. The test
// proves Project's current facts, not that Skills/Object performed cleanup.
func skillsCleanupEntries(version foundation.Version) []c.ParticipantRegistration {
	entries := r2Entries(version, true)
	for i := range entries {
		if entries[i].Name == c.SkillsParticipant {
			entries[i].CleanupAfter = []c.ParticipantName{skillsCleanupPrerequisite}
		}
		if entries[i].Name == c.ArtifactObjectParticipant || entries[i].Name == c.SecretParticipant {
			entries[i].CleanupAfter = []c.ParticipantName{c.SkillsParticipant}
		}
		if entries[i].Name == c.OutboxParticipant {
			entries[i].CleanupAfter = append(entries[i].CleanupAfter, skillsCleanupPrerequisite)
		}
	}
	return append(entries, c.ParticipantRegistration{Name: skillsCleanupPrerequisite, ContractVersion: version, OwnerModule: "test-prerequisite"})
}

func newSkillsCleanupFixture(t *testing.T) *r3Fixture {
	t.Helper()
	f := newR3Fixture(t)
	f.registry = r2Registry(t, skillsCleanupEntries(1))
	manifest, _ := f.registry.Manifest()
	var err error
	f.facts, err = project.NewLifecycleAuthority(f.store, manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.authority, err = project.NewAuthority(f.store, project.AuthorityDependencies{Sessions: f.accounts, Routes: f.accounts, Lifecycle: f.facts})
	if err != nil {
		t.Fatal(err)
	}
	f.service = f.lifecycleService(t, nil)
	return f
}

func skillsCleanupPhase(t *testing.T, f *r3Fixture, o c.LifecycleOperation) {
	t.Helper()
	// This is an explicit persisted phase stimulus following real BeginDelete;
	// no claim is made that a production lifecycle worker reached it.
	f.sql(t, `UPDATE agenteam_project.lifecycle_participants SET stop_state='stopped',cleanup_state=CASE WHEN participant_name=$2 THEN 'completed' ELSE 'required' END WHERE operation_id=$1`, o.ID.String(), string(skillsCleanupPrerequisite))
	f.sql(t, `UPDATE agenteam_project.lifecycle_operations SET state='cleaning',cleanup_stage='domains',version=version+1,updated_at=clock_timestamp() WHERE id=$1`, o.ID.String())
}

func skillsCleanupCheck(t *testing.T, f *r3Fixture, a *project.LifecycleAuthority, o c.LifecycleOperation, actor identity.Actor, request c.LifecycleCause) error {
	t.Helper()
	before := f.snapshot(t, o.ProjectID, f.owner)
	var privateBefore, privateAfter string
	query := `SELECT jsonb_build_array((SELECT to_jsonb(o) FROM agenteam_project.lifecycle_operations o WHERE id=$1),(SELECT jsonb_agg(to_jsonb(p) ORDER BY participant_name) FROM agenteam_project.lifecycle_participants p WHERE operation_id=$1))::text`
	if err := f.raw.QueryRow(ctxFor(t), query, o.ID.String()).Scan(&privateBefore); err != nil {
		t.Fatal(err)
	}
	err := f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
		return a.ValidateLifecycleInTx(ctx, tx, actor, request, c.SkillsParticipant, c.CleanupPhase)
	})
	if e := f.raw.QueryRow(ctxFor(t), query, o.ID.String()).Scan(&privateAfter); e != nil {
		t.Fatal(e)
	}
	if privateBefore != privateAfter || f.snapshot(t, o.ProjectID, f.owner) != before {
		t.Fatal("cleanup authority changed persistent facts/activity")
	}
	return err
}

func TestProjectSkillsCleanupCurrentFacts(t *testing.T) {
	f := newSkillsCleanupFixture(t)
	for _, state := range []string{"required", "pending"} {
		t.Run(state, func(t *testing.T) {
			o, actor, _ := f.accept(t, c.Delete)
			skillsCleanupPhase(t, f, o)
			f.sql(t, `UPDATE agenteam_project.lifecycle_participants SET cleanup_state=$2 WHERE operation_id=$1 AND participant_name=$3`, o.ID.String(), state, string(c.SkillsParticipant))
			if err := skillsCleanupCheck(t, f, f.facts, o, actor, r3Cause(o)); err != nil {
				t.Fatal("current Skills cleanup rejected", err)
			}
		})
	}
	for _, mode := range []string{"accepted", "stopping", "failed_cleanup", "outbox", "audit", "final", "skills_completed", "skills_failed", "dependency_required", "dependency_pending", "dependency_failed", "not_stopped", "owner", "gate", "project_version", "request_version", "request_action", "pointer_cleared", "uninitialized", "missing_participant", "extra_participant", "participant_version", "digest", "live_receipt"} {
		t.Run(mode, func(t *testing.T) {
			o, actor, _ := f.accept(t, c.Delete)
			request := r3Cause(o)
			want := foundation.InvalidState
			if mode != "accepted" && mode != "stopping" {
				skillsCleanupPhase(t, f, o)
			}
			switch mode {
			case "accepted":
			case "stopping":
				f.phase(t, o, c.OperationStopping)
			case "failed_cleanup":
				f.sql(t, `UPDATE agenteam_project.lifecycle_operations SET state='failed',resume_phase='cleanup',safe_reason='operation_failed' WHERE id=$1`, o.ID.String())
			case "outbox", "audit", "final":
				f.sql(t, `UPDATE agenteam_project.lifecycle_operations SET cleanup_stage=$2 WHERE id=$1`, o.ID.String(), mode)
			case "skills_completed", "skills_failed":
				f.sql(t, `UPDATE agenteam_project.lifecycle_participants SET cleanup_state=$2 WHERE operation_id=$1 AND participant_name=$3`, o.ID.String(), mode[len("skills_"):], string(c.SkillsParticipant))
			case "dependency_required", "dependency_pending", "dependency_failed":
				f.sql(t, `UPDATE agenteam_project.lifecycle_participants SET cleanup_state=$2 WHERE operation_id=$1 AND participant_name=$3`, o.ID.String(), mode[len("dependency_"):], string(skillsCleanupPrerequisite))
			case "not_stopped":
				f.sql(t, `UPDATE agenteam_project.lifecycle_participants SET stop_state='pending' WHERE operation_id=$1 AND participant_name='secret'`, o.ID.String())
				want = foundation.DependencyUnavailable
			case "owner":
				f.sql(t, `UPDATE agenteam_project.lifecycle_operations SET owner_user_id=$2 WHERE id=$1`, o.ID.String(), id[identity.User](t).String())
				want = foundation.DependencyUnavailable
			case "gate":
				f.sql(t, `UPDATE agenteam_project.projects SET lifecycle='active' WHERE id=$1`, o.ProjectID.String())
				want = foundation.DependencyUnavailable
			case "project_version":
				f.sql(t, `UPDATE agenteam_project.projects SET version=version+1 WHERE id=$1`, o.ProjectID.String())
				want = foundation.DependencyUnavailable
			case "request_version":
				request.ProjectVersion++
				want = foundation.Forbidden
			case "request_action":
				request.Action = c.Archive
				want = foundation.Forbidden
			case "pointer_cleared":
				f.sql(t, `UPDATE agenteam_project.projects SET lifecycle='active',current_lifecycle_operation_id=NULL WHERE id=$1`, o.ProjectID.String())
				want = foundation.Forbidden
			case "uninitialized":
				// The real CHECK constraint forbids an uninitialized Deleting row;
				// restoring its only legal shape must still reject the old cause.
				f.sql(t, `UPDATE agenteam_project.projects SET lifecycle='active',version=1,initialized_at=NULL,current_lifecycle_operation_id=NULL WHERE id=$1`, o.ProjectID.String())
				want = foundation.Forbidden
			case "missing_participant":
				f.sql(t, `DELETE FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND participant_name=$2`, o.ID.String(), string(c.SkillsParticipant))
				want = foundation.DependencyUnavailable
			case "extra_participant":
				f.sql(t, `INSERT INTO agenteam_project.lifecycle_participants(operation_id,participant_name,contract_version,stop_state,cleanup_state,version) VALUES($1,'extra',1,'stopped','required',1)`, o.ID.String())
				want = foundation.DependencyUnavailable
			case "participant_version":
				f.sql(t, `UPDATE agenteam_project.lifecycle_participants SET contract_version=2 WHERE operation_id=$1 AND participant_name=$2`, o.ID.String(), string(c.SkillsParticipant))
				want = foundation.DependencyUnavailable
			case "digest":
				f.sql(t, `UPDATE agenteam_project.lifecycle_operations SET manifest_digest=$2 WHERE id=$1`, o.ID.String(), "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
				want = foundation.DependencyUnavailable
			case "live_receipt":
				f.sql(t, `INSERT INTO agenteam_project.deletion_receipts(operation_id,deleted_project_id,original_owner_user_id,command_key_hash,request_digest,completed_at) VALUES($1,$2,$3,$4,$4,clock_timestamp())`, o.ID.String(), o.ProjectID.String(), f.owner.Details().UserID, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
				want = foundation.DependencyUnavailable
			}
			requireCode(t, skillsCleanupCheck(t, f, f.facts, o, actor, request), want)
		})
	}
	t.Run("retained_compatible_manifest", func(t *testing.T) {
		o, actor, _ := f.accept(t, c.Delete)
		skillsCleanupPhase(t, f, o)
		entries := skillsCleanupEntries(2)
		old, err := c.NewRequiredManifest(entries)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(old.Entries())
		digest, _ := old.Digest()
		f.sql(t, `UPDATE agenteam_project.lifecycle_operations SET required_manifest=$2::jsonb,manifest_digest=$3 WHERE id=$1`, o.ID.String(), raw, digest.String())
		f.sql(t, `UPDATE agenteam_project.lifecycle_participants SET contract_version=2 WHERE operation_id=$1`, o.ID.String())
		requireCode(t, skillsCleanupCheck(t, f, f.facts, o, actor, r3Cause(o)), foundation.DependencyUnbound)
		current, _ := f.registry.Manifest()
		compatible, err := project.NewLifecycleAuthority(f.store, current, entries)
		if err != nil {
			t.Fatal(err)
		}
		if err = skillsCleanupCheck(t, f, compatible, o, actor, r3Cause(o)); err != nil {
			t.Fatal("retained version", err)
		}
		for _, field := range []string{"owner", "references", "dependencies"} {
			changed := skillsCleanupEntries(2)
			for i := range changed {
				if changed[i].Name == c.SkillsParticipant {
					switch field {
					case "owner":
						changed[i].OwnerModule = "other"
					case "references":
						changed[i].ReferenceKinds = []c.ReferenceKind{"other"}
					case "dependencies":
						changed[i].CleanupAfter = nil
					}
				}
			}
			a, err := project.NewLifecycleAuthority(f.store, current, changed)
			if err != nil {
				t.Fatal(err)
			}
			requireCode(t, skillsCleanupCheck(t, f, a, o, actor, r3Cause(o)), foundation.DependencyUnavailable)
		}
	})
	t.Run("deletion_receipt_is_not_write_permission", func(t *testing.T) {
		o, actor, _ := f.deletion(t)
		err := f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
			return f.facts.ValidateLifecycleInTx(ctx, tx, actor, r3Cause(o), c.SkillsParticipant, c.CleanupPhase)
		})
		requireCode(t, err, foundation.InvalidState)
	})
}

func TestProjectSkillsCleanupTransactions(t *testing.T) {
	f := newSkillsCleanupFixture(t)
	o, actor, _ := f.accept(t, c.Delete)
	skillsCleanupPhase(t, f, o)
	observed := &r3ObservedStore{Store: f.raw}
	manifest, _ := f.registry.Manifest()
	a, err := project.NewLifecycleAuthority(observed, manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ended foundation.Tx
	for _, mode := range []foundation.LockMode{foundation.Shared, foundation.Exclusive} {
		result := f.raw.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.raw.AcquireAll(ctx, tx, []foundation.LockRequest{r3Lock(o.ProjectID, mode)}); err != nil {
				return err
			}
			ended = tx
			return a.ValidateLifecycleInTx(ctx, tx, actor, r3Cause(o), c.SkillsParticipant, c.CleanupPhase)
		})
		if result.State() != foundation.Committed || observed.acquires != 0 || observed.transactions != 0 {
			t.Fatal("validation changed caller transaction/locks", result.Fault())
		}
	}
	requireCode(t, a.ValidateLifecycleInTx(ctxFor(t), ended, actor, r3Cause(o), c.SkillsParticipant, c.CleanupPhase), foundation.DependencyUnavailable)
	for _, mode := range []string{"missing", "wrong_project", "foreign_store"} {
		t.Run(mode, func(t *testing.T) {
			store := f.raw
			if mode == "foreign_store" {
				store = openStore(t, f.db.Config(t, nil))
			}
			result := store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if mode != "missing" {
					project := o.ProjectID
					if mode == "wrong_project" {
						project = id[identity.Project](t)
					}
					if e := store.AcquireAll(ctx, tx, []foundation.LockRequest{r3Lock(project, foundation.Shared)}); e != nil {
						return e
					}
				}
				e := a.ValidateLifecycleInTx(ctx, tx, actor, r3Cause(o), c.SkillsParticipant, c.CleanupPhase)
				requireCode(t, e, foundation.DependencyUnavailable)
				return e
			})
			if result.State() != foundation.NotCommitted {
				t.Fatal("invalid caller transaction committed")
			}
			requireCode(t, result.Fault(), foundation.InternalError)
		})
	}
	scope, _ := identity.InProject(o.ProjectID)
	role, _ := identity.RegisterService(identity.ProjectLifecycle)
	wrongCause, _ := role.Actor(id[c.Operation](t).String(), scope)
	wrongRole, _ := identity.RegisterService(identity.ObjectMaintenance)
	wrong, _ := wrongRole.Actor(o.ID.String(), scope)
	agent, _ := identity.NewAgentRun(o.ProjectID, id[identity.Agent](t), id[identity.Execution](t))
	for _, bad := range []identity.Actor{f.owner, agent, wrong, wrongCause} {
		requireCode(t, skillsCleanupCheck(t, f, a, o, bad, r3Cause(o)), foundation.Forbidden)
	}
	for _, participant := range []c.ParticipantName{c.ArtifactObjectParticipant, c.SecretParticipant, c.OutboxParticipant, c.AuditParticipant, skillsCleanupPrerequisite} {
		err := f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
			return a.ValidateLifecycleInTx(ctx, tx, actor, r3Cause(o), participant, c.CleanupPhase)
		})
		requireCode(t, err, foundation.DependencyUnbound)
	}
	t.Run("cancelled_caller", func(t *testing.T) {
		var cancelledTx foundation.Tx
		result := f.raw.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			cancelledTx = tx
			if err := f.raw.AcquireAll(ctx, tx, []foundation.LockRequest{r3Lock(o.ProjectID, foundation.Shared)}); err != nil {
				return err
			}
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			err := a.ValidateLifecycleInTx(cancelled, tx, actor, r3Cause(o), c.SkillsParticipant, c.CleanupPhase)
			if err == nil {
				t.Fatal("cancelled caller authorized")
			}
			return err
		})
		if result.State() != foundation.NotCommitted {
			t.Fatal("cancelled validation committed")
		}
		requireCode(t, a.ValidateLifecycleInTx(ctxFor(t), cancelledTx, actor, r3Cause(o), c.SkillsParticipant, c.CleanupPhase), foundation.DependencyUnavailable)
	})
	if observed.acquires != 0 || observed.transactions != 0 {
		t.Fatal("authority acquired locks or began a transaction")
	}
}

func TestProjectSkillsCleanupCurrentFactsRemainLocked(t *testing.T) {
	f := newSkillsCleanupFixture(t)
	o, actor, _ := f.accept(t, c.Delete)
	skillsCleanupPhase(t, f, o)
	ctx, cancel := context.WithCancel(ctxFor(t))
	defer cancel()
	lock := r3Lock(o.ProjectID, foundation.Shared)
	readCause, writeCause := cause(t), cause(t)
	newOwner := id[identity.User](t)
	ready := make(chan int32, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	readDone, writeDone := make(chan struct{}), make(chan struct{})
	var readResult, writeResult foundation.CommitResult
	// Every path releases/cancels and observes both original callbacks return.
	// A completed SQL UPDATE alone cannot stand in for their transaction tails.
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		cancel()
		for _, done := range []<-chan struct{}{readDone, writeDone} {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("owned transaction did not return during cleanup")
			}
		}
	})
	go func() {
		defer close(readDone)
		readResult = f.raw.WithinTx(ctx, readCause, func(ctx context.Context, tx foundation.Tx) error {
			if err := f.raw.AcquireAll(ctx, tx, []foundation.LockRequest{lock}); err != nil {
				return err
			}
			if err := f.facts.ValidateLifecycleInTx(ctx, tx, actor, r3Cause(o), c.SkillsParticipant, c.CleanupPhase); err != nil {
				return err
			}
			x, err := f.raw.InTx(tx)
			if err != nil {
				return err
			}
			var pid int32
			if err = x.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
				return err
			}
			ready <- pid
			select {
			case <-release:
				return f.facts.ValidateLifecycleInTx(ctx, tx, actor, r3Cause(o), c.SkillsParticipant, c.CleanupPhase)
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	var holder int32
	select {
	case holder = <-ready:
	case <-readDone:
		close(writeDone) // No writer was started.
		t.Fatal("reader did not reach held current facts", readResult.Fault())
	case <-time.After(3 * time.Second):
		close(writeDone)
		t.Fatal("reader did not reach current facts")
	}
	go func() {
		defer close(writeDone)
		writeResult = f.raw.WithinTx(ctx, writeCause, func(ctx context.Context, tx foundation.Tx) error {
			if err := f.raw.AcquireAll(ctx, tx, []foundation.LockRequest{r3Lock(o.ProjectID, foundation.Exclusive)}); err != nil {
				return err
			}
			x, err := f.raw.InTx(tx)
			if err != nil {
				return err
			}
			// Test-owned owner drift: no public role/owner mutation is claimed.
			_, err = x.Exec(ctx, `UPDATE agenteam_project.projects SET owner_user_id=$2 WHERE id=$1`, o.ProjectID.String(), newOwner.String())
			return err
		})
	}()
	r2ObserveWaiter(t, f.r2Fixture, holder, lock.Key, "ExclusiveLock")
	select {
	case <-writeDone:
		t.Fatal("owner writer passed the current reader lock")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	await(t, readDone)
	await(t, writeDone)
	if readResult.State() != foundation.Committed || writeResult.State() != foundation.Committed {
		t.Fatal("original read/writer transactions did not commit")
	}
	requireCode(t, skillsCleanupCheck(t, f, f.facts, o, actor, r3Cause(o)), foundation.DependencyUnavailable)
}
