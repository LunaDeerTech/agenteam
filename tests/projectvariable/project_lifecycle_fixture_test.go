//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

type lifecycleHoldKey struct{}

// A marker selects the original call, not a replacement Store/authority. It is
// installed below the shared hookStore before every real fixture constructor.
type variableLifecycleStore struct{ fixtureStore }
type variableLifecycleHold struct {
	mu        sync.Mutex
	entered   chan struct{}
	release   chan struct{}
	once      sync.Once
	claimed   bool
	ctx       context.Context
	pid       int32
	result    f.CommitResult
	callbacks int
}

func newLifecycleHold() *variableLifecycleHold {
	return &variableLifecycleHold{entered: make(chan struct{}), release: make(chan struct{})}
}
func (g *variableLifecycleHold) unhold() { g.once.Do(func() { close(g.release) }) }
func (s *variableLifecycleStore) WithinTx(caller context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	g, _ := caller.Value(lifecycleHoldKey{}).(*variableLifecycleHold)
	selected := false
	if g != nil {
		g.mu.Lock()
		selected = !g.claimed
		g.claimed = true
		g.mu.Unlock()
	}
	result := s.fixtureStore.WithinTx(caller, cause, func(ctx context.Context, tx f.Tx) error {
		if selected {
			x, err := s.InTx(tx)
			if err != nil {
				return err
			}
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			g.mu.Lock()
			g.ctx, g.pid = ctx, pid
			g.mu.Unlock()
			close(g.entered)
			<-g.release // Does not substitute cancellation for the original callback's return.
		}
		err := fn(ctx, tx)
		if selected {
			g.mu.Lock()
			g.callbacks++
			g.mu.Unlock()
		}
		return err
	})
	if selected {
		g.mu.Lock()
		g.result = result
		g.mu.Unlock()
	}
	return result
}

type variableLifecycleFixture struct {
	*variableHTTPFixture
	ordinary    *pv.Service
	secretOwner *pv.SecretService
	stopper     *pv.ProjectCallStopper
	manifest    pc.RequiredManifest
}

func lifecycleManifest(t *testing.T) pc.RequiredManifest {
	t.Helper()
	m, err := pc.NewRequiredManifest([]pc.ParticipantRegistration{
		{Name: pc.SkillsParticipant, ContractVersion: 1, OwnerModule: "agent-skills-variables", ReferenceKinds: []pc.ReferenceKind{"variable-call"}},
		{Name: pc.ArtifactObjectParticipant, ContractVersion: 1, OwnerModule: "artifact-object", ReferenceKinds: []pc.ReferenceKind{"object"}, CleanupAfter: []pc.ParticipantName{pc.SkillsParticipant}},
		{Name: pc.SecretParticipant, ContractVersion: 1, OwnerModule: "secret", ReferenceKinds: []pc.ReferenceKind{"secret"}, CleanupAfter: []pc.ParticipantName{pc.SkillsParticipant}},
		{Name: pc.OutboxParticipant, ContractVersion: 1, OwnerModule: "outbox", CleanupAfter: []pc.ParticipantName{pc.ArtifactObjectParticipant, pc.SecretParticipant, pc.SkillsParticipant}},
		{Name: pc.AuditParticipant, ContractVersion: 1, OwnerModule: "audit", CleanupAfter: []pc.ParticipantName{pc.OutboxParticipant}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Account and Project creation are actual services; the inherited Skills
// initialization receipt remains an explicit controlled prerequisite. This
// assembly creates one actual Project authority for BOTH Variables services and
// D04. It does not enable a production initializer or bind fake participants.
func newVariableLifecycleFixture(t *testing.T) *variableLifecycleFixture {
	t.Helper()
	db := newDatabase(t)
	raw := openStore(t, db.Config(t, nil))
	observed := &variableLifecycleStore{fixtureStore: raw}
	base := assembleVariableHTTPFixture(t, db, raw, &hookStore{fixtureStore: observed})
	store := base.tracked
	manifest := lifecycleManifest(t)
	lifecycle, err := project.NewLifecycleAuthority(store, manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	secretFacts, err := secret.NewProjectAuditAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: base.accounts, Routes: base.accounts, Lifecycle: lifecycle, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.ProjectVariableProducer: base.authority, ac.SecretProducer: secretFacts}})
	if err != nil {
		t.Fatal(err)
	}
	writes, err := pv.NewSecretWriteAuthority(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(store, base.keys, audit.Authorizations{Sessions: base.accounts, System: base.accounts, Accounts: base.accounts, Projects: projects})
	if err != nil {
		t.Fatal(err)
	}
	keyring, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))), base.keys)
	if err != nil {
		t.Fatal(err)
	}
	projectSecrets, err := project.NewSecretAuthority(projects)
	if err != nil {
		t.Fatal(err)
	}
	d04, err := secret.New(store, keyring, aud, secret.Authorizations{Sessions: base.accounts, System: base.accounts, Projects: projectSecrets, Usage: base.accounts, AccountWrites: base.accounts, ProjectVariables: writes})
	if err != nil {
		t.Fatal(err)
	}
	if err = d04.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d04.StopMaintenance)
	catalog := event.NewCatalog()
	ordinaryTypes, err := vc.RegisterVariableEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	secretTypes, err := vc.RegisterSecretVariableEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	box, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{vc.VariableProducer: base.authority}, Sessions: base.accounts, System: base.accounts, Projects: projects, Audit: aud, Cursors: base.keys, Processes: fixtureProcess{id[oc.Process](t)}})
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := pv.New(store, pv.Dependencies{Authority: base.authority, Projects: projects, Events: box, VariableEvents: ordinaryTypes, Audit: aud, Activity: base.accounts, Cursors: base.keys})
	if err != nil {
		t.Fatal(err)
	}
	secretOwner, err := pv.NewSecret(store, pv.SecretDependencies{Authority: base.authority, Writes: writes, Secrets: d04, Projects: projects, Events: box, VariableEvents: secretTypes, Audit: aud, Activity: base.accounts, Cursors: base.keys})
	if err != nil {
		ordinary.Stop()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ordinary.Stop()
		secretOwner.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := ordinary.Drain(ctx); err != nil {
			t.Error("ordinary original calls did not drain", err)
		}
		if err := secretOwner.Drain(ctx); err != nil {
			t.Error("Secret original calls did not drain", err)
		}
	})
	stopper, err := pv.NewProjectCallStopper(ordinary, secretOwner)
	if err != nil {
		t.Fatal(err)
	}
	return &variableLifecycleFixture{base, ordinary, secretOwner, stopper, manifest}
}

// This is the normative upstream STOPPING fixture. It is not BeginArchive/
// BeginDelete or a production phase worker, and declares no participant joined.
func (v *variableLifecycleFixture) stage(ctx context.Context, t *testing.T, project pc.ProjectRef, action pc.LifecycleAction, held func(int32)) (pc.LifecycleCause, pc.ScopeRef, i.Actor, f.CommitResult) {
	t.Helper()
	cause := pc.LifecycleCause{OperationID: id[pc.Operation](t), Action: action, ProjectVersion: project.Version + 1}
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: project.ID}
	registration, _ := i.RegisterService(i.ProjectLifecycle)
	actorScope, _ := i.InProject(project.ID)
	actor, err := registration.Actor(cause.OperationID.String(), actorScope)
	if err != nil {
		t.Fatal(err)
	}
	result := v.writeStage(ctx, project, cause, held)
	return cause, scope, actor, result
}
func (v *variableLifecycleFixture) writeStage(ctx context.Context, project pc.ProjectRef, cause pc.LifecycleCause, held func(int32)) f.CommitResult {
	raw, _ := json.Marshal(v.manifest.Entries())
	digest, _ := v.manifest.Digest()
	gate, cleanup := "archiving", "not_applicable"
	if cause.Action == pc.Delete {
		gate, cleanup = "deleting", "required"
	}
	txCause, _ := f.NewRecoveryCause("projectvariable.phase-fixture", cause.OperationID.String(), "")
	return v.raw.WithinTx(ctx, txCause, func(ctx context.Context, tx f.Tx) error {
		key, _ := f.ProjectLock(project.ID.String())
		if err := v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
			return err
		}
		x, err := v.raw.InTx(tx)
		if err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'stopping',2,$6::jsonb,$7,clock_timestamp(),clock_timestamp())`, cause.OperationID.String(), project.ID.String(), project.OwnerUserID.String(), string(cause.Action), int64(cause.ProjectVersion), raw, digest.String()); err != nil {
			return err
		}
		for _, entry := range v.manifest.Entries() {
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_participants(operation_id,participant_name,contract_version,stop_state,cleanup_state,version) VALUES($1,$2,$3,'required',$4,1)`, cause.OperationID.String(), string(entry.Name), int64(entry.ContractVersion), cleanup); err != nil {
				return err
			}
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle=$2,version=$3,current_lifecycle_operation_id=$4,updated_at=clock_timestamp() WHERE id=$1 AND version=$5 AND lifecycle='active' AND current_lifecycle_operation_id IS NULL`, project.ID.String(), gate, int64(cause.ProjectVersion), cause.OperationID.String(), int64(project.Version))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return f.NewFault(f.InvalidState, f.NotStarted)
		}
		if held != nil {
			var pid int32
			if err := x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			held(pid)
		}
		return nil
	})
}
