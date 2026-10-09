//go:build integration

package skill_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

// Project/Creation/lifecycle records are explicit canonical test seeds, not
// Project.Create/BeginArchive/BeginDelete acceptance. Every lifecycle decision
// below still goes through the real same-Store Project LifecycleAuthority.
// Object methods remain the previously disclosed controlled D05 boundary.
type skillStopProjects struct {
	skill.ProjectPorts
	lifecycle *project.LifecycleAuthority
}

func (p skillStopProjects) ValidateLifecycleInTx(ctx context.Context, tx f.Tx, actor id.Actor, cause pc.LifecycleCause, participant pc.ParticipantName, phase pc.OperationPhase) error {
	return p.lifecycle.ValidateLifecycleInTx(ctx, tx, actor, cause, participant, phase)
}

func skillStopManifest(t *testing.T) pc.RequiredManifest {
	t.Helper()
	manifest, e := pc.NewRequiredManifest([]pc.ParticipantRegistration{
		{Name: pc.SkillsParticipant, ContractVersion: 1, OwnerModule: "skills", ReferenceKinds: []pc.ReferenceKind{"skill-initialization", "skill-package-reader", "skill-object-cleanup"}},
		{Name: pc.ArtifactObjectParticipant, ContractVersion: 1, OwnerModule: "artifact-object", ReferenceKinds: []pc.ReferenceKind{"object"}, CleanupAfter: []pc.ParticipantName{pc.SkillsParticipant}},
		{Name: pc.SecretParticipant, ContractVersion: 1, OwnerModule: "secret", ReferenceKinds: []pc.ReferenceKind{"secret"}, CleanupAfter: []pc.ParticipantName{pc.SkillsParticipant}},
		{Name: pc.OutboxParticipant, ContractVersion: 1, OwnerModule: "outbox", CleanupAfter: []pc.ParticipantName{pc.ArtifactObjectParticipant, pc.SecretParticipant, pc.SkillsParticipant}},
		{Name: pc.AuditParticipant, ContractVersion: 1, OwnerModule: "audit", CleanupAfter: []pc.ParticipantName{pc.OutboxParticipant}},
	})
	if e != nil {
		t.Fatal(e)
	}
	return manifest
}

func skillStopAuthority(t *testing.T, p *skillPG, store project.Store, manifest pc.RequiredManifest) *skill.Authority {
	t.Helper()
	lifecycle, e := project.NewLifecycleAuthority(store, manifest, nil)
	if e != nil {
		t.Fatal(e)
	}
	a, e := skill.NewAuthority(store, skillStopProjects{p.projects, lifecycle})
	if e != nil {
		t.Fatal(e)
	}
	return a
}

func seedSkillStop(t *testing.T, p *skillPG, v *skillCase, manifest pc.RequiredManifest, action pc.LifecycleAction) (id.Actor, pc.LifecycleCause, pc.ScopeRef) {
	t.Helper()
	var rawSkill string
	if e := p.store.QueryRow(testContext(t), `SELECT id::text FROM agenteam_skill.skills WHERE project_id=$1`, v.request.ProjectID.String()).Scan(&rawSkill); e != nil {
		t.Fatal("real Skill publication is required before lifecycle fixture", e)
	}
	skillID, e := f.ParseID[pc.Skill](rawSkill)
	if e != nil {
		t.Fatal(e)
	}
	seedReaderReadyProject(t, p, v, skillID)
	cause := pc.LifecycleCause{OperationID: testID[pc.Operation](t), Action: action, ProjectVersion: 2}
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.request.ProjectID}
	raw, e := json.Marshal(manifest.Entries())
	if e != nil {
		t.Fatal(e)
	}
	digest, e := manifest.Digest()
	if e != nil {
		t.Fatal(e)
	}
	gate, cleanup := "archiving", "not_applicable"
	if action == pc.Delete {
		gate, cleanup = "deleting", "required"
	}
	result := p.store.WithinTx(testContext(t), testCause(t), func(ctx context.Context, tx f.Tx) error {
		key, _ := f.ProjectLock(scope.ProjectID.String())
		if e := p.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); e != nil {
			return e
		}
		x, e := p.store.InTx(tx)
		if e != nil {
			return e
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) VALUES($1,$2,$3,$4,2,'stopping',2,$5::jsonb,$6,clock_timestamp(),clock_timestamp())`, cause.OperationID.String(), scope.ProjectID.String(), v.owner.String(), string(action), raw, digest.String()); e != nil {
			return e
		}
		for _, entry := range manifest.Entries() {
			if _, e = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_participants(operation_id,participant_name,contract_version,stop_state,cleanup_state,version) VALUES($1,$2,$3,'required',$4,1)`, cause.OperationID.String(), string(entry.Name), int64(entry.ContractVersion), cleanup); e != nil {
				return e
			}
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle=$2,version=2,current_lifecycle_operation_id=$3,updated_at=clock_timestamp() WHERE id=$1`, scope.ProjectID.String(), gate, cause.OperationID.String())
		return e
	})
	requireCommitted(t, result)
	registration, _ := id.RegisterService(id.ProjectLifecycle)
	actorScope, _ := id.InProject(scope.ProjectID)
	actor, e := registration.Actor(cause.OperationID.String(), actorScope)
	if e != nil {
		t.Fatal(e)
	}
	return actor, cause, scope
}

// This barrier owns the real public initializer's actual Discard call. It
// neither ignores an in-flight SQL transaction nor substitutes a join flag.
type skillStopObjects struct {
	*controlledObjects
	ctx              context.Context
	reached, release chan struct{}
	once             sync.Once
}

func (o *skillStopObjects) PreparePayload(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, media string, size int64, digest *f.Digest, body io.ReadCloser) (oc.PreparedPayload, error) {
	o.ctx = ctx
	return o.controlledObjects.PreparePayload(ctx, actor, owner, media, size, digest, body)
}
func (o *skillStopObjects) DiscardPrepared(prepared oc.PreparedPayload) error {
	e := o.controlledObjects.DiscardPrepared(prepared)
	close(o.reached)
	<-o.release
	return e
}
func (o *skillStopObjects) finish() { o.once.Do(func() { close(o.release) }) }

func heldSkillInitialization(t *testing.T, p *skillPG, store project.Store, manifest pc.RequiredManifest) (*skillCase, *skill.Service, *skillStopObjects, <-chan error) {
	t.Helper()
	v := p.newCase(t)
	authority := skillStopAuthority(t, p, store, manifest)
	v.objects.store, v.objects.authority = store, authority
	o := &skillStopObjects{controlledObjects: v.objects, reached: make(chan struct{}), release: make(chan struct{})}
	bundle, e := skill.AddSkills(testContext(t))
	if e != nil {
		t.Fatal(e)
	}
	service, e := skill.New(skill.Dependencies{Authority: authority, Objects: o, Processes: controlledProcesses{}, ProcessID: testID[oc.Process](t), Bundle: bundle})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	returned, done := make(chan error, 1), make(chan struct{})
	t.Cleanup(func() {
		cancel()
		o.finish()
		waitSignal(t, done, "held original initializer did not actually return")
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if e := service.Drain(cleanup); e != nil {
			t.Error("original lifecycle work not joined", e)
		}
	})
	go func() {
		defer close(done)
		_, e := service.InitializeProjectSkills(ctx, v.actor, v.request)
		returned <- e
	}()
	waitSignal(t, o.reached, "real initializer did not reach original Discard")
	return v, service, o, returned
}

func skillStopJoined(t *testing.T, p *skillPG, service *skill.Service, o *skillStopObjects, returned <-chan error, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) {
	t.Helper()
	report, e := service.RequestStop(testContext(t), actor, cause, scope)
	if e != nil || !report.Matches(pc.SkillsParticipant, cause, scope) || report.Details().State != pc.StopPending || len(report.Details().ActiveRefs) != 1 || o.ctx.Err() != context.Canceled {
		t.Fatal("known stop lost the still-live original Discard", e)
	}
	if report, e = service.InspectStop(testContext(t), actor, cause, scope); e != nil || report.Details().State != pc.StopPending {
		t.Fatal("cancellation was reported as actual return", e)
	}
	var pending int
	if e = p.store.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND phase<>'joined'`, scope.ProjectID.String()).Scan(&pending); e != nil || pending != 1 {
		t.Fatal("live original work prematurely retired", e)
	}
	o.finish()
	select {
	case e = <-returned:
		if !errors.Is(e, context.Canceled) {
			t.Fatal("original cancelled retirement result lost", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("released original Discard did not return")
	}
	if report, e = service.InspectStop(testContext(t), actor, cause, scope); e != nil || report.Details().State != pc.Stopped {
		t.Fatal("actual original call did not durably retire", e)
	}
	if e = p.store.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND phase<>'joined'`, scope.ProjectID.String()).Scan(&pending); e != nil || pending != 0 {
		t.Fatal("stopped report disagrees with real durable work", e)
	}
}

// Only a successful original skill-stop callback arms the accepted full-frame
// proxy at its real backend PID. The original Store result is never replaced.
type skillStopCommitStore struct {
	*postgres.Store
	proxy     *commitproxy.Proxy
	operation pc.OperationID
	project   id.ProjectID
	armed     bool
	original  f.CommitResult
}

func (s *skillStopCommitStore) WithinTx(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
	selected := false
	result := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := callback(ctx, tx); e != nil {
			return e
		}
		d := cause.Details()
		if s.armed || s.operation.Validate() != nil || d.Kind != f.RecoveryCause || d.Owner != "skill-stop" || d.RecoveryRunID != s.operation.String() || d.CheckpointRef != s.project.String() {
			return nil
		}
		x, e := s.Store.InTx(tx)
		if e != nil {
			return e
		}
		var writer int32
		if e = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&writer); e != nil {
			return e
		}
		if e = s.proxy.Arm(writer); e != nil {
			return e
		}
		s.armed, selected = true, true
		return nil
	})
	if selected {
		s.original = result
	}
	return result
}

func TestSkillLifecycleStopPersistence(t *testing.T) {
	p := newSkillPG(t)
	manifest := skillStopManifest(t)
	for _, scenario := range []string{"archive", "delete", "accepted_phase", "wrong_version", "wrong_action", "missing_participant", "wrong_owner", "unbound"} {
		t.Run("current_authority/"+scenario, func(t *testing.T) {
			v := p.newCase(t)
			complete, e := v.service.InitializeProjectSkills(testContext(t), v.actor, v.request)
			if e != nil || complete.State != pc.InitializationCompleted {
				t.Fatal("real initialization failed", e)
			}
			action := pc.Archive
			if scenario == "delete" {
				action = pc.Delete
			}
			actor, cause, scope := seedSkillStop(t, p, v, manifest, action)
			service := p.newService(t, skillStopAuthority(t, p, p.store, manifest), v.objects)
			switch scenario {
			case "accepted_phase":
				readerMutation(t, p, v, `UPDATE agenteam_project.lifecycle_operations SET state='accepted',version=1 WHERE id=$1`, cause.OperationID.String())
			case "wrong_version":
				cause.ProjectVersion++
			case "wrong_action":
				cause.Action = pc.Delete
			case "missing_participant":
				readerMutation(t, p, v, `DELETE FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND participant_name='agent-skills-variables'`, cause.OperationID.String())
			case "wrong_owner":
				readerMutation(t, p, v, `UPDATE agenteam_project.lifecycle_operations SET owner_user_id=$2 WHERE id=$1`, cause.OperationID.String(), testID[id.User](t).String())
			case "unbound":
				service = v.service
			}
			before := len(v.objects.steps)
			report, e := service.RequestStop(testContext(t), actor, cause, scope)
			if scenario == "archive" || scenario == "delete" {
				if e != nil || !report.Matches(pc.SkillsParticipant, cause, scope) || report.Details().State != pc.Stopped {
					t.Fatal("real current lifecycle facts denied", e)
				}
			} else if e == nil || report.Matches(pc.SkillsParticipant, cause, scope) {
				t.Fatal("invalid current lifecycle facts reported stopped")
			}
			if len(v.objects.steps) != before {
				t.Fatal("lifecycle observation performed physical Object work")
			}
		})
	}
	for _, action := range []pc.LifecycleAction{pc.Archive, pc.Delete} {
		t.Run("actual_call/"+string(action), func(t *testing.T) {
			v, service, o, returned := heldSkillInitialization(t, p, p.store, manifest)
			actor, cause, scope := seedSkillStop(t, p, v, manifest, action)
			skillStopJoined(t, p, service, o, returned, actor, cause, scope)
		})
	}
	t.Run("real_parent_lock", func(t *testing.T) {
		v, service, o, returned := heldSkillInitialization(t, p, p.store, manifest)
		actor, cause, scope := seedSkillStop(t, p, v, manifest, pc.Delete)
		held, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		unlock := func() { once.Do(func() { close(release) }) }
		result := make(chan f.CommitResult, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		lockCause := testCause(t)
		t.Cleanup(func() { cancel(); unlock(); waitSignal(t, done, "independent original lock owner did not return") })
		go func() {
			defer close(done)
			result <- p.store.WithinTx(ctx, lockCause, func(ctx context.Context, tx f.Tx) error {
				key, _ := f.ProjectLock(scope.ProjectID.String())
				if e := p.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); e != nil {
					return e
				}
				close(held)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		waitSignal(t, held, "independent real Project lock was not held")
		if report, e := service.RequestStop(testContext(t), actor, cause, scope); e == nil || report.Matches(pc.SkillsParticipant, cause, scope) || o.ctx.Err() != nil {
			t.Fatal("contended current-gate transaction cancelled physical owner", e)
		}
		unlock()
		waitSignal(t, done, "independent real lock transaction did not join")
		requireCommitted(t, <-result)
		skillStopJoined(t, p, service, o, returned, actor, cause, scope)
	})
	t.Run("original_stop_commit_unknown", func(t *testing.T) {
		proxied, proxy := withCommitProxy(t, p)
		store := &skillStopCommitStore{Store: proxied.store, proxy: proxy}
		v, service, o, returned := heldSkillInitialization(t, proxied, store, manifest)
		t.Cleanup(proxy.Release)
		actor, cause, scope := seedSkillStop(t, proxied, v, manifest, pc.Delete)
		store.operation, store.project = cause.OperationID, scope.ProjectID
		report, originalErr := service.RequestStop(testContext(t), actor, cause, scope)
		waitSignal(t, proxy.Reached(), "original stop complete COMMIT frame was not held")
		original, ok := skill.UnknownAttempt(originalErr)
		if !ok || original.State() != f.Unknown || original.AttemptID() != store.original.AttemptID() || !reflect.DeepEqual(original.Cause().Details(), store.original.Cause().Details()) || report.Matches(pc.SkillsParticipant, cause, scope) || o.ctx.Err() != nil {
			t.Fatal("unconfirmed stop cancelled owner or lost original outcome", originalErr)
		}
		proxy.Release()
		waitSignal(t, proxy.Committed(), "original stop COMMIT did not finish")
		waitSignal(t, proxy.HeldJoined(), "original proxy writer did not join")
		if report, e := service.InspectStop(testContext(t), actor, cause, scope); e != nil || report.Details().State != pc.StopPending || o.ctx.Err() != nil {
			t.Fatal("inspection after original Unknown started cancellation", e)
		}
		skillStopJoined(t, proxied, service, o, returned, actor, cause, scope)
		preserved, ok := skill.UnknownAttempt(originalErr)
		if !ok || preserved.State() != f.Unknown || preserved.AttemptID() != original.AttemptID() {
			t.Fatal("later lifecycle convergence rewrote original Unknown")
		}
	})
}
