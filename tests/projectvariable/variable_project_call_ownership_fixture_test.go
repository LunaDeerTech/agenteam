//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
)

type variableOwnershipDependencies struct {
	ordinary  pv.Dependencies
	secrets   pv.SecretDependencies
	d04       *secret.Service
	lifecycle *project.LifecycleAuthority
}

// These are the existing nonempty D10/D04 ports, sharing one actual Store and
// Project authority. The ProcessGuard comes from Runtime.Initialize, never
// from this construction or a same-DSN replacement.
func variableOwnershipAssemble(store *hookStore, accounts *account.Authority, keys cursor.Keyring, manifest pc.RequiredManifest, process objc.ProcessID) (out variableOwnershipDependencies, err error) {
	authority, err := pv.NewAuthority(store)
	if err != nil {
		return out, err
	}
	out.lifecycle, err = project.NewLifecycleAuthority(store, manifest, nil)
	if err != nil {
		return out, err
	}
	facts, err := secret.NewProjectAuditAuthority(store)
	if err != nil {
		return out, err
	}
	projects, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts, Lifecycle: out.lifecycle, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.ProjectVariableProducer: authority, ac.SecretProducer: facts}})
	if err != nil {
		return out, err
	}
	writes, err := pv.NewSecretWriteAuthority(store, projects)
	if err != nil {
		return out, err
	}
	aud, err := audit.New(store, keys, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: projects})
	if err != nil {
		return out, err
	}
	keyring, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))), keys)
	if err != nil {
		return out, err
	}
	projectSecrets, err := project.NewSecretAuthority(projects)
	if err != nil {
		return out, err
	}
	out.d04, err = secret.New(store, keyring, aud, secret.Authorizations{Sessions: accounts, System: accounts, Projects: projectSecrets, Usage: accounts, AccountWrites: accounts, ProjectVariables: writes})
	if err != nil {
		return out, err
	}
	catalog := event.NewCatalog()
	ordinaryEvents, err := vc.RegisterVariableEvents(catalog)
	if err != nil {
		return out, err
	}
	secretEvents, err := vc.RegisterSecretVariableEvents(catalog)
	if err != nil {
		return out, err
	}
	outProcess, err := f.ParseID[oc.Process](process.String())
	if err != nil {
		return out, err
	}
	box, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{vc.VariableProducer: authority}, Sessions: accounts, System: accounts, Projects: projects, Audit: aud, Cursors: keys, Processes: fixtureProcess{outProcess}})
	if err != nil {
		return out, err
	}
	out.ordinary = pv.Dependencies{Authority: authority, Projects: projects, Events: box, VariableEvents: ordinaryEvents, Audit: aud, Activity: accounts, Cursors: keys}
	out.secrets = pv.SecretDependencies{Authority: authority, Writes: writes, Secrets: out.d04, Projects: projects, Events: box, VariableEvents: secretEvents, Audit: aud, Activity: accounts, Cursors: keys}
	return out, nil
}

type variableOwnershipFixture struct {
	p2      *skillInstallationFixture
	deps    variableOwnershipDependencies
	guard   *object.ProcessGuard
	process objc.ProcessID
	pair    *pv.ProjectCallOwnership
}

func newVariableOwnershipFixture(t *testing.T) *variableOwnershipFixture {
	t.Helper()
	v := &variableOwnershipFixture{}
	var installer *toolruntime.InstallAuthority
	var retired sync.Once
	retire := func() {
		retired.Do(func() {
			if installer == nil {
				return
			}
			installer.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := installer.Drain(ctx); err != nil || !installer.Joined() {
				t.Error("Variables fixture original installer did not join")
				select {} // Preserve its providers under the original global test deadline.
			}
		})
	}
	v.p2 = newAgentOwnershipInstallationFixture(t, func() bool { return v.pair == nil || v.pair.Joined() }, func(store *hookStore, projects *project.Authority, guard *object.ProcessGuard, process objc.ProcessID) (*skill.Authority, error) {
		v.guard, v.process = guard, process
		var err error
		installer, err = toolruntime.NewInstallAuthority(store, guard, process)
		if err != nil {
			return nil, err
		}
		t.Cleanup(retire)
		return skill.NewAuthorityWithInstallExecution(store, projects, installer)
	})
	t.Cleanup(retire)
	var err error
	v.deps, err = variableOwnershipAssemble(v.p2.base.tracked, v.p2.base.accounts, v.p2.base.keys, v.p2.manifest, v.process)
	if v.deps.d04 != nil {
		t.Cleanup(v.deps.d04.StopMaintenance)
	}
	if err != nil {
		t.Fatal("Variables original dependency graph", agentOwnershipSafeCode(err))
	}
	if err = v.deps.d04.Initialize(ctxFor(t)); err != nil {
		t.Fatal("Variables D04 initialize", agentOwnershipSafeCode(err))
	}
	v.pair, err = pv.NewProjectCallOwnership(v.p2.base.tracked, v.deps.ordinary, v.deps.secrets, v.guard, v.process)
	if err != nil {
		t.Fatal("Variables owned pair", agentOwnershipSafeCode(err))
	}
	t.Cleanup(func() { variableOwnershipDrain(t, v.pair) })
	v.installHooks(t)
	return v
}

// Never let inherited DB cleanup delete resources underneath an unjoined
// original caller. This failure-only wait is bounded by the unchanged Go 6m /
// outer 540+60s deadline; it is not a successful local bounded Join.
func variableOwnershipDrain(t *testing.T, pair *pv.ProjectCallOwnership) {
	t.Helper()
	if pair == nil {
		return
	}
	pair.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := pair.Drain(ctx); err != nil || !pair.Joined() {
		t.Error("Variables original pair remains unjoined", agentOwnershipSafeCode(err))
		select {}
	}
}

type variableOwnershipCallKey struct{}
type variableOwnershipObserveKey struct{}
type variableOwnershipBarrier struct {
	once     sync.Once
	entered  chan struct{}
	release  *phaseRelease
	ctx      context.Context
	pid      int32
	physical f.CommitResult
}

func variableOwnershipNewBarrier() *variableOwnershipBarrier {
	return &variableOwnershipBarrier{entered: make(chan struct{}), release: newPhaseRelease()}
}
func (b *variableOwnershipBarrier) hold(ctx context.Context, pid int32, physical f.CommitResult) {
	if b == nil {
		return
	}
	b.once.Do(func() { b.ctx, b.pid, b.physical = ctx, pid, physical; close(b.entered); <-b.release.done })
}

type variableOwnershipCall struct {
	mu                                                  sync.Mutex
	done                                                chan struct{}
	err                                                 error
	callID                                              string
	registration                                        f.CommitResult
	businessCalls                                       int
	businessAt                                          int
	registrationUnknown                                 bool
	unknownAttempt                                      f.ID[f.TransactionAttempt]
	noncePhysical                                       f.CommitResult
	register, business, nonce, confirmation, checkpoint *variableOwnershipBarrier
	cancel                                              context.CancelFunc
}

func variableOwnershipStart(t *testing.T, call *variableOwnershipCall, fn func(context.Context) error) *variableOwnershipCall {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	call.cancel, call.done = cancel, make(chan struct{})
	if call.registrationUnknown {
		call.unknownAttempt = id[f.TransactionAttempt](t)
	}
	ctx = context.WithValue(ctx, variableOwnershipCallKey{}, call)
	go func() { defer close(call.done); call.err = fn(ctx) }()
	t.Cleanup(func() {
		cancel()
		for _, b := range []*variableOwnershipBarrier{call.register, call.business, call.nonce, call.confirmation, call.checkpoint} {
			if b != nil {
				b.release.release()
			}
		}
		variableOwnershipWait(t, call.done)
	})
	return call
}
func variableOwnershipWait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("Variables original return still pending; retaining original owners")
		<-done
	}
}
func (c *variableOwnershipCall) await(t *testing.T, b *variableOwnershipBarrier) {
	t.Helper()
	select {
	case <-b.entered:
	case <-c.done:
		t.Fatal("Variables original call returned before required barrier", agentOwnershipSafeCode(c.err))
	case <-time.After(5 * time.Second):
		t.Fatal("Variables original barrier not reached")
	}
}
func (c *variableOwnershipCall) held(t *testing.T, b *variableOwnershipBarrier, canceled bool) {
	t.Helper()
	if b.ctx != nil && (b.ctx.Err() != nil) != canceled {
		t.Fatal("Variables original cancellation scope")
	}
	select {
	case <-c.done:
		t.Fatal("Variables held original returned early")
	default:
	}
}

// Original hookStore callbacks retain their actual physical Tx and result.
// A business barrier is armed only by this call's observed registration cause;
// it can never accidentally hold the new first (registration) transaction.
func (v *variableOwnershipFixture) installHooks(t *testing.T) {
	t.Helper()
	s := v.p2.base.tracked
	var mu sync.Mutex
	calls := make(map[string]*variableOwnershipCall)
	s.mu.Lock()
	s.after = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		call, _ := ctx.Value(variableOwnershipCallKey{}).(*variableOwnershipCall)
		if call == nil {
			return nil
		}
		if cause.Details().Owner == "projectvariable-call-register" {
			call.mu.Lock()
			call.callID = cause.Details().RecoveryRunID
			call.mu.Unlock()
			mu.Lock()
			calls[cause.Details().RecoveryRunID] = call
			mu.Unlock()
		}
		if cause.Details().Owner == "secret-nonce" && call.nonce != nil {
			mu.Lock()
			calls[cause.Details().RecoveryRunID] = call
			mu.Unlock()
			x, err := s.InTx(tx)
			if err != nil {
				return err
			}
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			call.nonce.hold(ctx, pid, f.CommitResult{})
		}
		return nil
	}
	s.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		mu.Lock()
		call := calls[cause.Details().RecoveryRunID]
		mu.Unlock()
		if call == nil {
			return
		}
		switch cause.Details().Owner {
		case "projectvariable-call-register":
			call.mu.Lock()
			call.registration = result
			call.mu.Unlock()
			call.register.hold(nil, 0, result)
		case "projectvariable-call-return":
			call.checkpoint.hold(nil, 0, result)
		case "secret-nonce":
			call.mu.Lock()
			call.noncePhysical = result
			call.mu.Unlock()
		}
	}
	s.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		mu.Lock()
		call := calls[cause.Details().RecoveryRunID]
		mu.Unlock()
		if call != nil && cause.Details().Owner == "projectvariable-call-register" && call.registrationUnknown && result.State() == f.Committed {
			return f.UnknownResult(call.unknownAttempt, cause) // Explicit post-COMMIT ack projection only.
		}
		return result
	}
	s.beforeLocks = func(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
		call, _ := ctx.Value(variableOwnershipCallKey{}).(*variableOwnershipCall)
		if call == nil || len(locks) < 2 {
			return nil
		} // metadata retirement has only P EX.
		call.mu.Lock()
		registered := call.registration.State() == f.Committed
		if registered {
			call.businessCalls++
		}
		selected := registered && call.business != nil && call.businessCalls == call.businessAt
		call.mu.Unlock()
		if !selected {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var pid int32
		if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		call.business.hold(ctx, pid, f.CommitResult{})
		return nil
	}
	s.mu.Unlock()
	t.Cleanup(func() {
		s.mu.Lock()
		s.after, s.afterResult, s.transformResult, s.beforeLocks = nil, nil, nil, nil
		s.mu.Unlock()
	})
}

func (v *variableOwnershipFixture) phase(t *testing.T, p pc.ProjectRef, action pc.LifecycleAction) (pc.LifecycleCause, pc.ScopeRef, i.Actor) {
	t.Helper()
	// This is the existing explicit STOPPING prerequisite, not a claim that a
	// Variables provider was registered as a complete lifecycle participant.
	staged := &variableLifecycleFixture{variableHTTPFixture: v.p2.base, manifest: v.p2.manifest, lifecycle: v.deps.lifecycle}
	cause, scope, actor, result := staged.stage(ctxFor(t), t, p, action, nil)
	if result.State() != f.Committed {
		t.Fatal("Variables real stopping prerequisite", agentOwnershipSafeCode(result.Fault()))
	}
	return cause, scope, actor
}

func (v *variableOwnershipFixture) project(t *testing.T, name string) pc.ProjectRef {
	t.Helper()
	request := pc.CreateProjectRequest{ProjectID: id[i.Project](t), Name: name, Description: "owned Variables prerequisite"}
	result, err := v.p2.creator.CreateProject(ctxFor(t), v.p2.base.ownerBrowser.actor, meta(t, id[struct{}](t).String(), nil), request)
	if err != nil || result.State != pc.CreationReady || result.Project == nil {
		t.Fatal("Variables real Project/Skill initialization", agentOwnershipSafeCode(err))
	}
	return *result.Project
}

func variableOwnershipSnapshot(t *testing.T, raw *postgres.Store, project vc.ProjectID, nonce bool) string {
	t.Helper()
	var snapshot string
	err := raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'variables',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_projectvariable.variables x WHERE project_id=$1),
 'commands',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_projectvariable.commands x WHERE project_id=$1),
 'secret_commands',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_projectvariable.secret_commands x WHERE project_id=$1),
 'history',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_projectvariable.history x WHERE project_id=$1),
 'secret_history',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_projectvariable.secret_history x WHERE project_id=$1),
 'generation',(SELECT to_jsonb(x) FROM agenteam_projectvariable.project_generations x WHERE project_id=$1),
 'secret_generation',(SELECT to_jsonb(x) FROM agenteam_projectvariable.secret_project_generations x WHERE project_id=$1),
 'receipts',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_secret.project_variable_receipts x WHERE project_id=$1),
 'audit',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_audit.audit_records x WHERE project_id=$1),
 'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_outbox.events x WHERE project_id=$1),
 'nonce',CASE WHEN $2 THEN (SELECT jsonb_agg(to_jsonb(x) ORDER BY version) FROM agenteam_secret.secret_master_registry x) ELSE 'null'::jsonb END)::text`, project.String(), nonce).Scan(&snapshot)
	if err != nil {
		t.Fatal("Variables private fact snapshot", agentOwnershipSafeCode(err))
	}
	return snapshot // Never put this value in diagnostics.
}

func variableOwnershipRow(t *testing.T, raw *postgres.Store, callID, project, process, lane, class, phase string) {
	t.Helper()
	var valid bool
	err := raw.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.project_calls c JOIN agenteam_object.process_claims p ON p.process_id=c.process_id WHERE c.id=$1 AND c.project_id=$2 AND c.process_id=$3 AND c.lane=$4 AND c.class=$5 AND c.phase=$6 AND c.instance_id IS NOT NULL AND c.registered_at IS NOT NULL AND ((c.phase='running' AND c.closed_at IS NULL) OR (c.phase='business_closed' AND c.closed_at>=c.registered_at)))`, callID, project, process, lane, class, phase).Scan(&valid)
	if err != nil || !valid {
		t.Fatal("Variables exact registered tuple/phase", agentOwnershipSafeCode(err))
	}
}

func variableOwnershipCloseRow(t *testing.T, v *variableOwnershipFixture, call *variableOwnershipCall, p vc.ProjectID, lane, class string) {
	t.Helper()
	key, _ := f.ProjectLock(p.String())
	// Explicit legal terminal-row injection. This is a real SQL fence test for
	// a live original call, NEVER a foreign-death/complete participant proof.
	v.p2.base.tx(t, []f.LockRequest{{Key: key, Mode: f.Exclusive}}, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		var instance string
		if err := x.QueryRow(ctx, `SELECT instance_id::text FROM agenteam_projectvariable.project_calls WHERE id=$1 AND project_id=$2 AND process_id=$3 AND lane=$4 AND class=$5 AND phase='running'`, call.callID, p.String(), v.process.String(), lane, class).Scan(&instance); err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_projectvariable.project_calls SET phase='business_closed',closed_at=GREATEST(clock_timestamp(),registered_at) WHERE id=$1 AND project_id=$2 AND process_id=$3 AND instance_id=$4 AND lane=$5 AND class=$6 AND phase='running'`, call.callID, p.String(), v.process.String(), instance, lane, class)
		if err == nil && tag.RowsAffected() != 1 {
			return f.NewFault(f.InvalidState, f.NotStarted)
		}
		return err
	})
}
