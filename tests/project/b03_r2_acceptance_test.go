//go:build integration

package project_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestProjectB03R2AcceptReplayAndCurrentProgress(t *testing.T) {
	f := newR2Fixture(t)
	owner := f.human(t, "owner-alpha", "user")
	manifest, _ := f.registry.Manifest()
	for _, command := range []c.CommandName{c.ArchiveCommand, c.DeleteCommand} {
		t.Run(string(command), func(t *testing.T) {
			p, _, _ := f.create(t, owner, "Accept-"+string(command))
			intent := newR2Intent(t, p, command, "accept")
			before := f.resetActivity(t, owner)
			result, err := intent.invoke(ctxFor(t), f.service, owner)
			if err != nil {
				t.Fatal("accept", r2SafeError(err))
			}
			f.assertAccepted(t, intent, owner, result, manifest)
			accepted := f.snapshot(t, p.ID, owner)
			if !accepted.Activity.After(before) {
				t.Fatal("acceptance did not Touch")
			}
			unbound := f.lifecycleService(t, func(d *project.Dependencies) { d.LifecycleRegistry = nil })
			for range 2 {
				replayed, err := intent.invoke(ctxFor(t), unbound, owner)
				if err != nil || replayed.Operation.ID != result.Operation.ID {
					t.Fatal("receipt checked later registry/version", err)
				}
			}
			queried, err := unbound.GetLifecycle(ctxFor(t), owner, p.ID, result.Operation.ID)
			if err != nil || queried.Operation.ID != result.Operation.ID {
				t.Fatal("lifecycle query", err)
			}
			lookup, err := unbound.LookupCommand(ctxFor(t), owner, intent.lookup())
			if err != nil || lookup.State != c.LookupCommitted || lookup.Result.Lifecycle.Operation.ID != result.Operation.ID {
				t.Fatal("lookup", err)
			}
			if got := f.snapshot(t, p.ID, owner); got != accepted {
				t.Fatal("read/replay wrote facts or Touch")
			}
			changed := intent
			v := foundation.Version(99)
			changed.meta.ExpectedVersion = &v
			_, err = changed.invoke(ctxFor(t), unbound, owner)
			requireCode(t, err, foundation.IdempotencyKeyReused)
			_, err = f.service.CreateProject(ctxFor(t), owner, meta(t, "occupied-name", nil), c.CreateProjectRequest{ProjectID: id[identity.Project](t), Name: p.Name})
			requireCode(t, err, foundation.ResourceBusy)
			if command == c.DeleteCommand {
				_, err = f.service.GetProject(ctxFor(t), owner, p.ID)
				requireCode(t, err, foundation.ProjectNotActive)
			}
			// Test-owned progress is only a projection fixture, not a stop runtime.
			if _, err = f.raw.Exec(ctxFor(t), `UPDATE agenteam_project.lifecycle_operations SET state='stopping',version=2,updated_at=clock_timestamp() WHERE id=$1`, result.Operation.ID.String()); err != nil {
				t.Fatal(err)
			}
			refs := []c.PendingRef{}
			for range 101 {
				refs = append(refs, c.PendingRef{Participant: c.ArtifactObjectParticipant, Kind: "object", ID: id[c.ResourceIdentity](t)})
			}
			raw, _ := json.Marshal(refs)
			if _, err = f.raw.Exec(ctxFor(t), `UPDATE agenteam_project.lifecycle_participants SET stop_state='pending',safe_pending_refs=$2::jsonb,version=2 WHERE operation_id=$1 AND participant_name='artifact-object'`, result.Operation.ID.String(), raw); err != nil {
				t.Fatal(err)
			}
			progress, err := intent.invoke(ctxFor(t), unbound, owner)
			if err != nil || progress.Operation.State != c.OperationStopping || progress.Operation.Version != 2 || len(progress.Operation.PendingResources) != 100 || !progress.Operation.PendingRefsTruncated {
				t.Fatal("replay returned accepted snapshot or lost cap", err)
			}
			lookup, err = unbound.LookupCommand(ctxFor(t), owner, intent.lookup())
			if err != nil || lookup.Result.Lifecycle.Operation.Version != 2 {
				t.Fatal("lookup returned stale snapshot", err)
			}
			if !f.snapshot(t, p.ID, owner).Activity.Equal(accepted.Activity) {
				t.Fatal("progress poll touched activity")
			}
			refs[100].Kind = "unregistered"
			raw, _ = json.Marshal(refs)
			if _, err = f.raw.Exec(ctxFor(t), `UPDATE agenteam_project.lifecycle_participants SET safe_pending_refs=$2::jsonb WHERE operation_id=$1 AND participant_name='artifact-object'`, result.Operation.ID.String(), raw); err != nil {
				t.Fatal(err)
			}
			_, err = unbound.GetLifecycle(ctxFor(t), owner, p.ID, result.Operation.ID)
			requireCode(t, err, foundation.DependencyUnavailable)
		})
	}
}

func TestProjectB03R2AtomicRollbackAtAuditEventAndTouch(t *testing.T) {
	f := newR2Fixture(t)
	owner := f.human(t, "owner-alpha", "user")
	manifest, _ := f.registry.Manifest()
	for _, command := range []c.CommandName{c.ArchiveCommand, c.DeleteCommand} {
		t.Run(string(command), func(t *testing.T) {
			p, _, _ := f.create(t, owner, "Atomic-"+string(command))
			intent := newR2Intent(t, p, command, "atomic")
			for _, where := range []string{"audit", "event", "touch"} {
				t.Run(where, func(t *testing.T) {
					service := f.lifecycleService(t, func(d *project.Dependencies) {
						switch where {
						case "audit":
							action := ac.ProjectArchiveAccepted
							if command == c.DeleteCommand {
								action = ac.ProjectDeleteAccepted
							}
							d.Audit = failAudit{f.aud, action}
						case "event":
							d.Events = failEvent{f.events}
						case "touch":
							d.Activity = r2FailActivity{f.accounts}
						}
					})
					before := f.resetActivity(t, owner)
					_, err := intent.invoke(ctxFor(t), service, owner)
					requireCode(t, err, foundation.DependencyUnavailable)
					f.assertNoAcceptance(t, intent, owner, before)
					if f.snapshot(t, p.ID, owner).Plans != 1 {
						t.Fatal("failed acceptance lost original planned command")
					}
				})
			}
			result, err := intent.invoke(ctxFor(t), f.service, owner)
			if err != nil {
				t.Fatal("same plan recovery", r2SafeError(err))
			}
			f.assertAccepted(t, intent, owner, result, manifest)
		})
	}
}

func TestProjectB03R2OwnerSessionDependenciesAndGates(t *testing.T) {
	f := newR2Fixture(t)
	owner := f.human(t, "owner-alpha", "user")
	other := f.human(t, "other-owner", "user")
	admin := f.human(t, "admin", "admin")
	p, _, _ := f.create(t, owner, "Guards")
	intent := newR2Intent(t, p, c.DeleteCommand, "guard-delete")
	before := f.resetActivity(t, owner)
	for _, actor := range []identity.Actor{other, admin} {
		_, err := intent.invoke(ctxFor(t), f.service, actor)
		requireCode(t, err, foundation.NotFound)
		_, err = f.service.LookupCommand(ctxFor(t), actor, intent.lookup())
		requireCode(t, err, foundation.NotFound)
	}
	missingRegistry := f.lifecycleService(t, func(d *project.Dependencies) { d.LifecycleRegistry = nil })
	_, err := intent.invoke(ctxFor(t), missingRegistry, owner)
	requireCode(t, err, foundation.DependencyUnbound)
	authority, err := project.NewAuthority(f.store, project.AuthorityDependencies{Sessions: f.accounts})
	if err != nil {
		t.Fatal(err)
	}
	missingRoutes := f.lifecycleService(t, func(d *project.Dependencies) { d.Authority = authority })
	_, err = intent.invoke(ctxFor(t), missingRoutes, owner)
	requireCode(t, err, foundation.DependencyUnbound)
	stale := intent
	stale.confirmation.NormalizedCurrentPath = "owner-alpha/other"
	_, err = stale.invoke(ctxFor(t), f.service, owner)
	requireCode(t, err, foundation.ConfirmationStale)
	badVersion := intent
	v := foundation.Version(2)
	badVersion.meta.ExpectedVersion = &v
	_, err = badVersion.invoke(ctxFor(t), f.service, owner)
	requireCode(t, err, foundation.VersionConflict)
	f.assertNoAcceptance(t, intent, owner, before)
	// A nonnil zero registry is a construction defect, not optional unbinding.
	_, err = project.New(f.store, project.Dependencies{Authority: f.authority, Activity: f.accounts, Audit: f.aud, Events: f.events, ProjectEvents: f.typed, Initializer: f.skills, Processes: f.process, Cursors: f.keys, LifecycleRegistry: &project.LifecycleRegistry{}}, project.DefaultConfig())
	requireCode(t, err, foundation.DependencyUnbound)
	result, err := intent.invoke(ctxFor(t), f.service, owner)
	if err != nil {
		t.Fatal(err)
	}
	// Missing routes and registry do not invalidate an already accepted receipt.
	unbound := f.lifecycleService(t, func(d *project.Dependencies) { d.Authority = authority; d.LifecycleRegistry = nil })
	if _, err = intent.invoke(ctxFor(t), unbound, owner); err != nil {
		t.Fatal("replay rechecked route", err)
	}
	for _, actor := range []identity.Actor{other, admin} {
		_, err = f.service.GetLifecycle(ctxFor(t), actor, p.ID, result.Operation.ID)
		requireCode(t, err, foundation.NotFound)
	}
	if _, err = f.raw.Exec(ctxFor(t), `UPDATE agenteam_account.sessions SET absolute_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, owner.Details().SessionID); err != nil {
		t.Fatal(err)
	}
	_, err = intent.invoke(ctxFor(t), unbound, owner)
	requireCode(t, err, foundation.Unauthenticated)
	_, err = f.service.GetLifecycle(ctxFor(t), owner, p.ID, result.Operation.ID)
	requireCode(t, err, foundation.Unauthenticated)
	_, err = f.service.LookupCommand(ctxFor(t), owner, intent.lookup())
	requireCode(t, err, foundation.Unauthenticated)
	if _, err = f.raw.Exec(ctxFor(t), `UPDATE agenteam_account.sessions SET absolute_expires_at=clock_timestamp()+interval '1 hour',revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, owner.Details().SessionID); err != nil {
		t.Fatal(err)
	}
	_, err = intent.invoke(ctxFor(t), unbound, owner)
	requireCode(t, err, foundation.SessionRevoked)
	// A separate owner's real failed initialization never accepts lifecycle.
	f.skills.setMode("failed")
	target := id[identity.Project](t)
	creationMeta := meta(t, "uninitialized", nil)
	_, err = f.service.CreateProject(ctxFor(t), other, creationMeta, c.CreateProjectRequest{ProjectID: target, Name: "Pending"})
	var failure *foundation.Fault
	if !errors.As(err, &failure) || failure.Code != foundation.DependencyUnavailable || failure.CommitState != foundation.Committed {
		t.Fatal("failed initialization must retain the accepted creation", err)
	}
	creation, err := f.service.LookupCommand(ctxFor(t), other, c.CommandLookupRequest{ProjectID: target, Command: c.CreateCommand, Key: creationMeta.IdempotencyKey})
	if err != nil || creation.State != c.LookupCommitted || creation.Result == nil || creation.Result.Creation == nil || creation.Result.Creation.Operation == nil {
		t.Fatal("failed initialization lost its receipt", err)
	}
	failed := creation.Result.Creation.Operation
	var initialized bool
	var creationID string
	if err = f.raw.QueryRow(ctxFor(t), `SELECT initialized_at IS NOT NULL,creation_id::text FROM agenteam_project.projects WHERE id=$1`, target.String()).Scan(&initialized, &creationID); err != nil || initialized || creationID != failed.ID.String() || failed.State != c.CreationFailed || failed.SafeReason != c.ReasonOperationFailed || failure.CauseID != failed.ID.String() {
		t.Fatal("failed initialization canonical state mismatch", err)
	}
	one := foundation.Version(1)
	_, err = f.service.BeginArchive(ctxFor(t), other, meta(t, "uninitialized-archive", &one), target)
	requireCode(t, err, foundation.ProjectNotActive)
}

func TestProjectB03R2RetainsPlannedVersionsAcrossServiceRebuild(t *testing.T) {
	f := newR2Fixture(t)
	owner := f.human(t, "owner-alpha", "user")
	p, _, _ := f.create(t, owner, "Frozen")
	intent := newR2Intent(t, p, c.DeleteCommand, "frozen-plan")
	planOnly := f.lifecycleService(t, func(d *project.Dependencies) { d.Events = r2FailPrepare{f.events} })
	before := f.resetActivity(t, owner)
	_, err := intent.invoke(ctxFor(t), planOnly, owner)
	requireCode(t, err, foundation.DependencyUnavailable)
	f.assertNoAcceptance(t, intent, owner, before)
	var original string
	if err = f.raw.QueryRow(ctxFor(t), `SELECT plan::text FROM agenteam_project.commands WHERE project_id=$1 AND command_name='delete'`, p.ID.String()).Scan(&original); err != nil {
		t.Fatal(err)
	}
	latest := r2Registry(t, r2Entries(2, true))
	incompatible := f.lifecycleService(t, func(d *project.Dependencies) { d.LifecycleRegistry = latest })
	_, err = intent.invoke(ctxFor(t), incompatible, owner)
	requireCode(t, err, foundation.DependencyUnbound)
	f.assertNoAcceptance(t, intent, owner, before)
	compatible := r2Registry(t, r2Entries(2, true), r2Entries(1, false)...)
	rebuilt := f.lifecycleService(t, func(d *project.Dependencies) { d.LifecycleRegistry = compatible })
	result, err := intent.invoke(ctxFor(t), rebuilt, owner)
	if err != nil {
		t.Fatal("retained plan acceptance", r2SafeError(err))
	}
	oldManifest, _ := f.registry.Manifest()
	f.assertAccepted(t, intent, owner, result, oldManifest)
	var actual string
	if err = f.raw.QueryRow(ctxFor(t), `SELECT plan::text FROM agenteam_project.commands WHERE project_id=$1 AND command_name='delete'`, p.ID.String()).Scan(&actual); err != nil || actual != original {
		t.Fatal("service rebuild replaced original plan", err)
	}
}

func TestProjectB03R2CorruptStoredManifestCannotShrinkProgress(t *testing.T) {
	f := newR2Fixture(t)
	owner := f.human(t, "owner-alpha", "user")
	for _, kind := range []string{"digest", "manifest", "participant", "version"} {
		t.Run(kind, func(t *testing.T) {
			p, _, _ := f.create(t, owner, "Corrupt-"+kind)
			intent := newR2Intent(t, p, c.ArchiveCommand, "corrupt")
			result, err := intent.invoke(ctxFor(t), f.service, owner)
			if err != nil {
				t.Fatal(err)
			}
			q := map[string]string{"digest": `UPDATE agenteam_project.lifecycle_operations SET manifest_digest='sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' WHERE id=$1`, "manifest": `UPDATE agenteam_project.lifecycle_operations SET required_manifest='[]'::jsonb WHERE id=$1`, "participant": `DELETE FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND participant_name='secret'`, "version": `UPDATE agenteam_project.lifecycle_participants SET contract_version=2 WHERE operation_id=$1 AND participant_name='secret'`}[kind]
			if _, err = f.raw.Exec(ctxFor(t), q, result.Operation.ID.String()); err != nil {
				t.Fatal(err)
			}
			_, err = f.service.GetLifecycle(ctxFor(t), owner, p.ID, result.Operation.ID)
			requireCode(t, err, foundation.DependencyUnavailable)
			_, err = intent.invoke(ctxFor(t), f.service, owner)
			requireCode(t, err, foundation.DependencyUnavailable)
			_, err = f.service.LookupCommand(ctxFor(t), owner, intent.lookup())
			requireCode(t, err, foundation.DependencyUnavailable)
		})
	}
}

func TestProjectB03R2ConcurrentCommandsKeepOneOperation(t *testing.T) {
	f := newR2Fixture(t)
	owner := f.human(t, "owner-alpha", "user")
	for _, kind := range []string{"same-key", "different-keys", "archive-delete"} {
		t.Run(kind, func(t *testing.T) {
			p, _, _ := f.create(t, owner, "Concurrent-"+kind)
			first := newR2Intent(t, p, c.ArchiveCommand, "first")
			second := first
			if kind != "same-key" {
				second.meta.IdempotencyKey = "second"
			}
			if kind == "archive-delete" {
				second.command = c.DeleteCommand
			}
			type reply struct {
				result c.LifecycleResult
				err    error
			}
			answers := make(chan reply, 2)
			start := make(chan struct{})
			var workers sync.WaitGroup
			for _, intent := range []r2Intent{first, second} {
				workers.Go(func() { <-start; r, e := intent.invoke(ctxFor(t), f.service, owner); answers <- reply{r, e} })
			}
			close(start)
			workers.Wait()
			close(answers)
			var operation c.OperationID
			successes := 0
			for answer := range answers {
				if answer.err != nil {
					requireCode(t, answer.err, foundation.VersionConflict)
					continue
				}
				successes++
				if operation.Validate() == nil && operation != answer.result.Operation.ID {
					t.Fatal("two original operations")
				}
				operation = answer.result.Operation.ID
			}
			want := 1
			if kind == "same-key" {
				want = 2
			}
			if successes != want {
				t.Fatalf("successful calls=%d want=%d", successes, want)
			}
			s := f.snapshot(t, p.ID, owner)
			if s.Operations != 1 || s.Events != 1 || s.Audits != 1 || s.Receipts != 1 || s.Participants != 4 {
				t.Fatalf("competing acceptance facts %+v", s)
			}
		})
	}
}

func TestProjectB03R2FinalAcceptanceRechecksPathAndSession(t *testing.T) {
	f := newR2Fixture(t)
	owner := f.human(t, "owner-alpha", "user")
	for _, kind := range []string{"project-name", "username", "session"} {
		t.Run(kind, func(t *testing.T) {
			p, _, _ := f.create(t, owner, "Recheck-"+kind)
			intent := newR2Intent(t, p, c.DeleteCommand, "recheck")
			barrier := &r2PrepareBarrier{Appender: f.events, reached: make(chan struct{}), release: make(chan struct{})}
			var release sync.Once
			defer release.Do(func() { close(barrier.release) })
			service := f.lifecycleService(t, func(d *project.Dependencies) { d.Events = barrier })
			done := make(chan error, 1)
			go func() { _, err := intent.invoke(ctxFor(t), service, owner); done <- err }()
			await(t, barrier.reached)
			var want foundation.Code
			if kind == "project-name" {
				name := "Renamed"
				if _, err := f.service.UpdateProject(ctxFor(t), owner, meta(t, "rename-between-phases", &p.Version), p.ID, c.UpdateProjectRequest{Name: &name}); err != nil {
					t.Fatal(err)
				}
				want = foundation.VersionConflict
			} else {
				key, _ := foundation.UserLock(owner.Details().UserID)
				result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
					if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
						return err
					}
					if err := f.accounts.RequireCurrentSession(ctx, tx, owner); err != nil {
						return err
					}
					x, err := f.store.InTx(tx)
					if err != nil {
						return err
					}
					if kind == "username" {
						_, err = x.Exec(ctx, `UPDATE agenteam_account.users SET username='owner-changed',version=version+1 WHERE id=$1`, owner.Details().UserID)
					} else {
						_, err = x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, owner.Details().SessionID)
					}
					return err
				})
				if result.State() != foundation.Committed {
					t.Fatal("owned concurrent Account fact", result.Fault())
				}
				want = foundation.ConfirmationStale
				if kind == "session" {
					want = foundation.SessionRevoked
				}
			}
			release.Do(func() { close(barrier.release) })
			requireCode(t, <-done, want)
			s := f.snapshot(t, p.ID, owner)
			if s.Operations != 0 || s.Events != 0 || s.Audits != 0 || s.Receipts != 0 || s.Gate != "active" {
				t.Fatalf("accepted stale intent %+v", s)
			}
			if kind == "username" {
				if _, err := f.raw.Exec(ctxFor(t), `UPDATE agenteam_account.users SET username='owner-alpha',version=version+1 WHERE id=$1`, owner.Details().UserID); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestProjectB03R2MinimalReceiptIsCurrentOwnerReadOnly(t *testing.T) {
	f := newR2Fixture(t)
	owner := f.human(t, "owner-alpha", "user")
	other := f.human(t, "other-owner", "user")
	admin := f.human(t, "admin", "admin")
	target, operation := id[identity.Project](t), id[c.Operation](t)
	v := foundation.Version(1)
	m := meta(t, "original-delete", &v)
	request := c.DeleteProjectRequest{NormalizedCurrentPath: "owner-alpha/retired", Permanent: true}
	semantic, err := c.DeleteDigest(owner, m, target, request)
	if err != nil {
		t.Fatal(err)
	}
	user, _ := foundation.ParseID[identity.User](owner.Details().UserID)
	keyHash, _ := c.DeletionCommandKeyHash(target, user, m.IdempotencyKey)
	// Explicit test-owned final receipt; R2 has no finalizer and never writes it.
	if _, err = f.raw.Exec(ctxFor(t), `INSERT INTO agenteam_project.deletion_receipts(operation_id,deleted_project_id,original_owner_user_id,command_key_hash,request_digest,completed_at) VALUES($1,$2,$3,$4,$5,clock_timestamp())`, operation.String(), target.String(), user.String(), keyHash.String(), semantic.String()); err != nil {
		t.Fatal(err)
	}
	before := f.resetActivity(t, owner)
	unbound := f.lifecycleService(t, func(d *project.Dependencies) { d.LifecycleRegistry = nil })
	read, err := unbound.GetLifecycle(ctxFor(t), owner, target, operation)
	if err != nil || read.Receipt == nil || read.Receipt.Details().OperationID != operation {
		t.Fatal("minimal Get", err)
	}
	replayed, err := unbound.BeginDeleteProject(ctxFor(t), owner, m, target, request)
	if err != nil || replayed.Receipt == nil {
		t.Fatal("minimal replay", err)
	}
	lookupRequest := c.CommandLookupRequest{ProjectID: target, Command: c.DeleteCommand, Key: m.IdempotencyKey}
	lookup, err := unbound.LookupCommand(ctxFor(t), owner, lookupRequest)
	if err != nil || lookup.State != c.LookupCommitted || lookup.Result.Lifecycle.Receipt == nil {
		t.Fatal("minimal Lookup", err)
	}
	for _, actor := range []identity.Actor{other, admin} {
		_, err = unbound.GetLifecycle(ctxFor(t), actor, target, operation)
		requireCode(t, err, foundation.NotFound)
		_, err = unbound.BeginDeleteProject(ctxFor(t), actor, m, target, request)
		requireCode(t, err, foundation.NotFound)
		_, err = unbound.LookupCommand(ctxFor(t), actor, lookupRequest)
		requireCode(t, err, foundation.NotFound)
	}
	_, err = unbound.GetLifecycle(ctxFor(t), owner, target, id[c.Operation](t))
	requireCode(t, err, foundation.NotFound)
	changed := request
	changed.NormalizedCurrentPath = "owner-alpha/another"
	_, err = unbound.BeginDeleteProject(ctxFor(t), owner, m, target, changed)
	requireCode(t, err, foundation.IdempotencyKeyReused)
	lookupRequest.Command = c.ArchiveCommand
	_, err = unbound.LookupCommand(ctxFor(t), owner, lookupRequest)
	requireCode(t, err, foundation.ResourceDeleted)
	var after time.Time
	var projects, commands, operations int
	if err = f.raw.QueryRow(ctxFor(t), `SELECT (SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1),(SELECT count(*) FROM agenteam_project.projects WHERE id=$2),(SELECT count(*) FROM agenteam_project.commands WHERE project_id=$2),(SELECT count(*) FROM agenteam_project.lifecycle_operations WHERE project_id=$2)`, owner.Details().SessionID, target.String()).Scan(&after, &projects, &commands, &operations); err != nil || !after.Equal(before) || projects != 0 || commands != 0 || operations != 0 {
		t.Fatal("receipt lookup resurrected data or Touch", err)
	}
	if _, err = f.raw.Exec(ctxFor(t), `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, owner.Details().SessionID); err != nil {
		t.Fatal(err)
	}
	_, err = unbound.GetLifecycle(ctxFor(t), owner, target, operation)
	requireCode(t, err, foundation.SessionRevoked)
}

func TestProjectB03R2PlannedTypedFactsCannotForgeAcceptance(t *testing.T) {
	f := newR2Fixture(t)
	owner := f.human(t, "owner-alpha", "user")
	for _, command := range []c.CommandName{c.ArchiveCommand, c.DeleteCommand} {
		t.Run(string(command), func(t *testing.T) {
			p, _, _ := f.create(t, owner, "Forged-"+string(command))
			intent := newR2Intent(t, p, command, "planned-fact")
			planOnly := f.lifecycleService(t, func(d *project.Dependencies) { d.Events = r2FailPrepare{f.events} })
			before := f.resetActivity(t, owner)
			_, err := intent.invoke(ctxFor(t), planOnly, owner)
			requireCode(t, err, foundation.DependencyUnavailable)
			var rawHeader, rawPayload []byte
			var op, causeRef string
			err = f.raw.QueryRow(ctxFor(t), `SELECT plan->'event_header',plan->'event_payload',plan->>'operation_id',audit_cause FROM agenteam_project.commands WHERE project_id=$1 AND command_name=$2`, p.ID.String(), string(command)).Scan(&rawHeader, &rawPayload, &op, &causeRef)
			if err != nil {
				t.Fatal(err)
			}
			header, err := event.DecodeHeader(rawHeader)
			if err != nil {
				t.Fatal(err)
			}
			ev, err := f.typed.Restore(header, rawPayload)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := f.events.PrepareAppend(ctxFor(t), owner, ev)
			if err != nil {
				t.Fatal("real planned Discover must succeed", err)
			}
			result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if err := f.store.AcquireAll(ctx, tx, plan.Locks()); err != nil {
					return err
				}
				_, err := f.events.AppendEventInTx(ctx, tx, owner, ev, plan)
				return err
			})
			if result.State() != foundation.NotCommitted {
				t.Fatal("forged Event committed")
			}
			requireCode(t, result.Fault(), foundation.InvalidState)
			action, to := ac.ProjectArchiveAccepted, "archiving"
			if command == c.DeleteCommand {
				action, to = ac.ProjectDeleteAccepted, "deleting"
			}
			version := foundation.Version(1)
			metadata, err := ac.ProjectMetadata(action, ac.ProjectMetadataFields{ProjectID: p.ID.String(), InitiatorID: owner.Details().UserID, ProjectVersion: p.Version + 1, OperationID: op, OperationVersion: &version, From: "active", To: to, Action: string(command)})
			if err != nil {
				t.Fatal(err)
			}
			resource, err := ac.NewResource(ac.ProjectOperationResource, op)
			if err != nil {
				t.Fatal(err)
			}
			scope, _ := identity.InProject(p.ID)
			entry, err := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: owner, Action: action, Outcome: ac.Success, Resource: resource, Metadata: metadata})
			if err != nil {
				t.Fatal(err)
			}
			appendKey, err := ac.NewAppendKey(ac.ProjectProducer, causeRef, 0)
			if err != nil {
				t.Fatal(err)
			}
			result = f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if err := f.store.AcquireAll(ctx, tx, plan.Locks()); err != nil {
					return err
				}
				_, err := f.aud.AppendInTx(ctx, tx, entry, appendKey)
				return err
			})
			if result.State() != foundation.NotCommitted {
				t.Fatal("forged Audit committed")
			}
			requireCode(t, result.Fault(), foundation.InvalidState)
			f.assertNoAcceptance(t, intent, owner, before)
		})
	}
}

func TestProjectB03R2ArchivedPrerequisiteAcceptsDelete(t *testing.T) {
	f := newR2Fixture(t)
	owner := f.human(t, "owner-alpha", "user")
	p, _, _ := f.create(t, owner, "ArchivedFixture")
	// A test-owned archived prerequisite exercises only archived->deleting.
	// It is deliberately not evidence that any archive participant ran.
	if _, err := f.raw.Exec(ctxFor(t), `UPDATE agenteam_project.projects SET lifecycle='archived',version=2,updated_at=statement_timestamp(),archived_at=statement_timestamp() WHERE id=$1`, p.ID.String()); err != nil {
		t.Fatal(err)
	}
	p.Lifecycle, p.Version = c.Archived, 2
	intent := newR2Intent(t, p, c.DeleteCommand, "archived-delete")
	result, err := intent.invoke(ctxFor(t), f.service, owner)
	if err != nil {
		t.Fatal("archived acceptance", r2SafeError(err))
	}
	manifest, _ := f.registry.Manifest()
	f.assertAccepted(t, intent, owner, result, manifest)
}

func TestProjectB03R2SharedGateBlocksAcceptanceBeforeAnyFacts(t *testing.T) {
	f := newR2Fixture(t)
	owner := f.human(t, "owner-alpha", "user")
	p, _, _ := f.create(t, owner, "SharedGate")
	other, _, _ := f.create(t, owner, "Unaffected")
	intent := newR2Intent(t, p, c.ArchiveCommand, "blocked-acceptance")
	before := f.resetActivity(t, owner)
	key, _ := foundation.ProjectLock(p.ID.String())
	held := make(chan int32, 1)
	release := make(chan struct{})
	joined := make(chan foundation.CommitResult, 1)
	var once sync.Once
	defer once.Do(func() { close(release) })
	go func() {
		joined <- f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}); err != nil {
				return err
			}
			x, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			held <- pid
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	var holder int32
	select {
	case holder = <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("owned SH holder not established")
	}
	done := make(chan error, 1)
	go func() { _, err := intent.invoke(ctxFor(t), f.service, owner); done <- err }()
	r2ObserveWaiter(t, f, holder, key, "ExclusiveLock")
	select {
	case err := <-done:
		r2RequireSQLState(t, err, "55P03")
	case <-time.After(7 * time.Second):
		t.Fatal("acceptance lock budget did not terminate")
	}
	f.assertNoAcceptance(t, intent, owner, before)
	otherIntent := newR2Intent(t, other, c.ArchiveCommand, "other-project")
	if _, err := otherIntent.invoke(ctxFor(t), f.service, owner); err != nil {
		t.Fatal("other Project was gated", err)
	}
	once.Do(func() { close(release) })
	if result := <-joined; result.State() != foundation.Committed {
		t.Fatal("SH holder did not join", result.Fault())
	}
}
