//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// The real paired Agent/Skills/Variables/terminal prefix is inherited. Accepted
// Delete and unrelated participants remain controlled prerequisites. No lease,
// Execution, Model network request or default full registry is manufactured.
func TestProjectSecretCleanup(t *testing.T) {
	t.Run("head-batches-and-original-return", testProjectSecretCleanupReturn)
	t.Run("original-unknown-readonly-confirmation", testProjectSecretCleanupUnknown)
	t.Run("formal-provider-reference-remains-pending", testProjectSecretCleanupReference)
}

type secretCleanupPGTap struct {
	pc.ProjectLifecycleParticipant
	calls  atomic.Int64
	mu     sync.Mutex
	ctx    context.Context
	actor  i.Actor
	cause  pc.LifecycleCause
	scope  pc.ScopeRef
	report pc.CleanupReport
	err    error
}

func (s *secretCleanupPGTap) Cleanup(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, cp *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	s.calls.Add(1)
	r, err := s.ProjectLifecycleParticipant.Cleanup(ctx, actor, cause, scope, cp)
	s.mu.Lock()
	s.ctx, s.actor, s.cause, s.scope = context.WithoutCancel(ctx), actor, cause, scope
	s.report, s.err = r, err
	s.mu.Unlock()
	return r, err
}

type secretCleanupPG struct {
	*agentVariablesTerminalPG
	secret                                *secretCleanupPGTap
	parent                                []byte
	preservedRows, participants, provider string
	credential                            sc.CredentialRef
	quantity                              int
}

func newSecretCleanupPG(t *testing.T, quantity int, referenced bool) *secretCleanupPG {
	t.Helper()
	v := &secretCleanupPG{quantity: quantity}
	v.agentVariablesTerminalPG = newAgentVariablesTerminalPGConfigured(t, false, false, false, true, func(prefix *agentVariablesTerminalPG) {
		v.agentVariablesTerminalPG = prefix
		scope, err := i.InProject(v.p2.project.ID)
		outboxCleanupRequire(t, err)
		for n := 0; n < quantity; n++ {
			v.credential = v.createCredential(t, v.p2.base.ownerBrowser.actor, scope, fmt.Sprintf("project-cleanup-%d", n))
		}
		other, err := i.InProject(v.p2.base.project.ID)
		outboxCleanupRequire(t, err)
		v.createCredential(t, v.p2.base.ownerBrowser.actor, other, "other-project-cleanup")
		v.createCredential(t, v.p2.base.adminBrowser.actor, i.SystemScope(), "system-cleanup")
		if referenced {
			v.bindProvider(t)
		}
	})
	if v.p2.lifecycle == nil {
		t.Fatal("Secret fixture lost the original concrete lifecycle owner")
	}
	participant, err := project.NewSecretLifecycleParticipant(v.p2.lifecycle, v.deps.d04)
	outboxCleanupRequire(t, err)
	v.secret = &secretCleanupPGTap{ProjectLifecycleParticipant: participant}
	return v
}

func (v *secretCleanupPG) createCredential(t *testing.T, actor i.Actor, scope i.Scope, key string) sc.CredentialRef {
	t.Helper()
	owners := []string{actor.Details().UserID}
	if scope.Details().Kind == i.ProjectScope {
		owners = append([]string{scope.Details().ProjectID}, owners...)
	}
	command, err := f.NewCommandIdentity("secret", owners, string(sc.Create), f.IdempotencyKey(key))
	outboxCleanupRequire(t, err)
	material, err := sc.NewSecretMaterial([]byte("owned-project-cleanup-material"))
	outboxCleanupRequire(t, err)
	defer material.Destroy()
	written, err := v.deps.d04.ExecuteWrite(ctxFor(t), sc.WriteRequest{Actor: actor, Scope: scope, Identity: command, Kind: sc.Create, Purpose: sc.Model, Value: material})
	outboxCleanupRequire(t, err)
	if written.Deleted || written.Metadata.Version != 1 || written.Metadata.Purpose != sc.Model || !written.Metadata.CredentialRef.Details().Scope.Equal(scope) {
		t.Fatal("formal D04 creation returned a different owner or revision")
	}
	return written.Metadata.CredentialRef
}

// This Provider belongs to A and is created while A is active, by its real
// Human owner through the normal Model command. The combined Agent/Skills/
// Variables participant retires Agent references, not Provider configuration;
// Secret's declared predecessor is that combined participant. Consequently this
// surviving formal Model reference must produce Pending at the real Secret gate.
func (v *secretCleanupPG) bindProvider(t *testing.T) {
	t.Helper()
	b := v.p2.base
	authority, err := model.NewAuthority(b.tracked, model.Authorizations{Sessions: b.accounts, System: b.accounts, Projects: v.projects})
	outboxCleanupRequire(t, err)
	aud, err := audit.New(b.tracked, b.keys, audit.Authorizations{Sessions: b.accounts, System: b.accounts, Accounts: b.accounts, Projects: v.projects, Models: authority})
	outboxCleanupRequire(t, err)
	usage, err := model.NewSecretUsageRouter(authority, b.accounts)
	outboxCleanupRequire(t, err)
	projects, err := project.NewSecretAuthority(v.projects)
	outboxCleanupRequire(t, err)
	keys, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))), b.keys)
	outboxCleanupRequire(t, err)
	// This separately composed real Model usage port creates only the formal
	// reference. Cleanup always uses the original fixed v.deps.d04 Service.
	secrets, err := secret.New(b.tracked, keys, aud, secret.Authorizations{Sessions: b.accounts, System: b.accounts, Projects: projects, Usage: usage, AccountWrites: b.accounts})
	outboxCleanupRequire(t, err)
	outboxCleanupRequire(t, secrets.Initialize(ctxFor(t)))
	t.Cleanup(secrets.StopMaintenance)
	catalog := event.NewCatalog()
	types, err := model.DefineEvents(catalog)
	outboxCleanupRequire(t, err)
	box, err := outbox.New(b.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{model.ModelProducer: authority}, Sessions: b.accounts, System: b.accounts, Projects: v.projects, Audit: aud, Cursors: b.keys, Processes: fixtureProcess{captureProviderProcesses{guard: v.guard}.CurrentProcess()}})
	outboxCleanupRequire(t, err)
	models, err := model.New(b.tracked, authority, model.Dependencies{Secret: secrets, Audit: aud, Events: box, ConfigurationEvents: types, Cursors: b.keys})
	outboxCleanupRequire(t, err)
	outboxCleanupRequire(t, models.Initialize(ctxFor(t)))
	scope, err := i.InProject(v.p2.project.ID)
	outboxCleanupRequire(t, err)
	created, err := models.CreateProvider(ctxFor(t), mc.CreateProviderRequest{CommandMeta: mc.CommandMeta{Actor: b.ownerBrowser.actor, Scope: scope, Key: "secret-cleanup-provider"}, Input: mc.ProviderInput{Name: "retained-cleanup-provider", Protocol: mc.OpenAIChat, BaseURL: "https://unused-cleanup.example/v1", Enabled: true, CredentialRef: &v.credential, Options: json.RawMessage(`{}`)}})
	outboxCleanupRequire(t, err)
	v.provider = created.ResourceID
	var bound bool
	outboxCleanupRequire(t, b.raw.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_model.providers p JOIN agenteam_secret.secret_references r ON r.owner_id::text=p.id::text AND r.credential_id::text=p.credential_id::text WHERE p.id::text=$1 AND p.project_id::text=$2 AND r.project_id::text=$2 AND r.consumer='model')`, v.provider, v.p2.project.ID.String()).Scan(&bound))
	if !bound {
		t.Fatal("formal Provider did not retain its exact Project credential reference")
	}
}

func (v *secretCleanupPG) driver(t *testing.T) *project.LifecycleCleanupDriver {
	t.Helper()
	// Construct the original terminal ports once through the existing fixture.
	if v.agentTerminal == nil {
		v.agentVariablesTerminalPG.driver(t)
	}
	d, err := project.NewLifecycleCleanupDriverWithSecret(v.p2.base.tracked, v.projects, captureProviderProcesses{guard: v.guard}, v.consumer, agentRetirementSkillsDiagnostic{SkillsCleanupDomain: v.tap, t: t}, v.variables, v.agentTerminal, v.variableTerminal, v.secret)
	outboxCleanupRequire(t, err)
	v.drivers = append(v.drivers, d)
	return d
}

func (v *secretCleanupPG) ready(t *testing.T, d *project.LifecycleCleanupDriver) {
	t.Helper()
	v.agentVariablesTerminalPG.ready(t, d)
	call := v.run(t, d)
	outboxCleanupWait(t, call.done, "original combined terminal return before Secret")
	outboxCleanupRequire(t, call.err)
	v.parent = bytes.Clone(v.receipt(t).Raw)
	if v.secret.calls.Load() != 0 {
		t.Fatal("Secret ran in the same round as its original terminal predecessor")
	}
	v.preservedRows = v.preservedSnapshot(t)
	v.participants = v.otherCleanupSnapshot(t)
	if counts := v.secretCounts(t); counts != [6]int{v.quantity + 1, v.quantity, 5, 2*v.quantity + 6, secretCleanupBoolCount(v.provider != ""), 0} {
		t.Fatal("formal D04 heads, original receipts or references differ", counts)
	}
	v.state(t, "required", "terminal")
	// Current canonical completion is insufficient without the original live
	// Secret claim. This direct call has no private scope and deletes nothing.
	v.refuseDirect(t, context.Background(), f.Forbidden)
}

func secretCleanupBoolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (v *secretCleanupPG) secretCounts(t *testing.T) [6]int {
	t.Helper()
	var n [6]int
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_secret.secrets WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_secret.secret_command_receipts WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_secret.project_variable_receipts WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_secret.secret_payloads WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_secret.secret_references WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE project_id=$1)`, v.p2.project.ID.String()).Scan(&n[0], &n[1], &n[2], &n[3], &n[4], &n[5]))
	return n
}

func (v *secretCleanupPG) secretSnapshot(t *testing.T) string {
	t.Helper()
	var receipts string
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id),'[]')::text FROM agenteam_secret.secret_command_receipts r`).Scan(&receipts))
	return v.variablesCleanupPG.d04Snapshot(t) + "\n" + receipts
}

// Diagnose only which fixed table snapshot changed; ciphertext, identities,
// keys and nonce values are never logged. The full byte comparison stays below.
func secretCleanupChangedTables(t *testing.T, before, after string) []string {
	t.Helper()
	left, leftReceipts, leftOK := strings.Cut(before, "\n")
	right, rightReceipts, rightOK := strings.Cut(after, "\n")
	var a, b []json.RawMessage
	if !leftOK || !rightOK || json.Unmarshal([]byte(left), &a) != nil || json.Unmarshal([]byte(right), &b) != nil || len(a) != 7 || len(b) != 7 {
		t.Fatal("invalid private Secret snapshot diagnostic shape")
	}
	names := []string{"secret_master_registry", "secret_control", "secret_payloads", "secrets", "project_variable_receipts", "secret_references", "secret_leases"}
	var changed []string
	for n, name := range names {
		if !bytes.Equal(a[n], b[n]) {
			changed = append(changed, name)
		}
	}
	if leftReceipts != rightReceipts {
		changed = append(changed, "secret_command_receipts")
	}
	return changed
}

func (v *secretCleanupPG) preservedSnapshot(t *testing.T) string {
	t.Helper()
	var raw string
	// Compare ciphertext/metadata only. No read/decrypt operation is needed.
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY version) FROM agenteam_secret.secret_master_registry x),
 (SELECT jsonb_agg(to_jsonb(x)) FROM agenteam_secret.secret_control x),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY payload_id) FROM agenteam_secret.secret_payloads x WHERE project_id IS DISTINCT FROM $1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_secret.secrets x WHERE project_id IS DISTINCT FROM $1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_secret.secret_command_receipts x WHERE project_id IS DISTINCT FROM $1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_secret.project_variable_receipts x WHERE project_id IS DISTINCT FROM $1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY credential_id,consumer,owner_id) FROM agenteam_secret.secret_references x WHERE project_id IS DISTINCT FROM $1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_secret.secret_leases x WHERE project_id IS DISTINCT FROM $1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_agent.project_calls x),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_projectvariable.project_calls x),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_model.providers x)
 )::text`, v.p2.project.ID.String()).Scan(&raw))
	return raw
}

func (v *secretCleanupPG) otherCleanupSnapshot(t *testing.T) string {
	t.Helper()
	var raw string
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_agg(to_jsonb(r) ORDER BY participant_name)::text FROM agenteam_project.lifecycle_participants r WHERE operation_id=$1 AND participant_name NOT IN ('agent-skills-variables','secret')`, v.cause.OperationID.String()).Scan(&raw))
	return raw
}

func (v *secretCleanupPG) state(t *testing.T, want, phase string) {
	t.Helper()
	var lifecycle, operation, stage, parent, cleanup, claim string
	var schema *int64
	var checkpoint, parentCheckpoint []byte
	var count int64
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT p.lifecycle,o.state,o.cleanup_stage,a.cleanup_state,a.checkpoint,s.cleanup_state,s.checkpoint_schema,s.checkpoint,s.attempt_count,w.phase FROM agenteam_project.projects p JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id JOIN agenteam_project.lifecycle_participants a ON a.operation_id=o.id AND a.participant_name='agent-skills-variables' JOIN agenteam_project.lifecycle_participants s ON s.operation_id=o.id AND s.participant_name='secret' JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id WHERE p.id=$1 AND o.id=$2`, v.p2.project.ID.String(), v.cause.OperationID.String()).Scan(&lifecycle, &operation, &stage, &parent, &parentCheckpoint, &cleanup, &schema, &checkpoint, &count, &claim))
	if lifecycle != "deleting" || operation != "cleaning" || stage != "domains" || parent != "completed" || !bytes.Equal(parentCheckpoint, v.parent) || cleanup != want || claim != phase {
		t.Fatal("Secret changed its actual predecessor, domain stage or original claim", cleanup, claim)
	}
	if want == "pending" || want == "completed" {
		var cp sc.CleanupCheckpoint
		outboxCleanupRequire(t, json.Unmarshal(checkpoint, &cp))
		if schema == nil || *schema != 1 || cp.ProjectID != v.p2.project.ID || cp.OperationID.String() != v.cause.OperationID.String() || count < 1 {
			t.Fatal("Secret persisted checkpoint did not bind its original operation")
		}
	} else if schema != nil || len(checkpoint) != 0 {
		t.Fatal("uncompleted first Secret call fabricated progress")
	}
	if v.preservedSnapshot(t) != v.preservedRows || v.otherCleanupSnapshot(t) != v.participants || v.metadataSnapshot(t, v.p2.base.project.ID) != v.foreignVariables || v.foreignSnapshot(t) != v.foreign {
		t.Fatal("Secret cleanup changed another owner, Project, System credential or key/nonce facts")
	}
	actual, err := v.guard.CurrentProcess()
	outboxCleanupRequire(t, err)
	if actual != v.process {
		t.Fatal("original guard retired before Secret's original return")
	}
}

func (v *secretCleanupPG) matches(c f.TransactionCause) bool {
	d := c.Details()
	return c.Kind() == f.RecoveryCause && d.Owner == "secret-project-cleanup" && d.RecoveryRunID == v.cause.OperationID.String() && d.CheckpointRef == ""
}

type secretCleanupDeleteObservation struct {
	atomic.Int64
	before, after atomic.Int64
}

func (v *secretCleanupPG) clearHooks() {
	v.variablesCleanupPG.clearHooks()
	store := v.p2.base.tracked
	store.mu.Lock()
	store.before = nil
	store.mu.Unlock()
}

func (v *secretCleanupPG) observeDeletes(t *testing.T) *secretCleanupDeleteObservation {
	t.Helper()
	deletes := &secretCleanupDeleteObservation{}
	deletes.Store(-1)
	deletes.before.Store(-1)
	deletes.after.Store(-1)
	store := v.p2.base.tracked
	var mu sync.Mutex
	baselines := make(map[f.Tx]int64)
	read := func(ctx context.Context, tx f.Tx) (int64, error) {
		x, err := store.InTx(tx)
		if err != nil {
			return 0, err
		}
		var n int64
		err = x.QueryRow(ctx, `SELECT COALESCE(sum(n_tup_del),0)::bigint FROM pg_stat_xact_user_tables WHERE schemaname='agenteam_secret'`).Scan(&n)
		return n, err
	}
	// PostgreSQL 17 find_tabstat_entry() includes the backend's pending base
	// counts as well as its live transaction counts. Measure around this exact
	// original callback, on the same live Tx, rather than interpreting the
	// absolute value as this callback's deletes. No flush or reset is issued.
	store.mu.Lock()
	store.before = func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
		if !v.matches(c) {
			return nil
		}
		n, err := read(ctx, tx)
		if err != nil {
			return err
		}
		mu.Lock()
		baselines[tx] = n
		mu.Unlock()
		return nil
	}
	store.mu.Unlock()
	store.setAfter(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
		if !v.matches(c) {
			return nil
		}
		n, err := read(ctx, tx)
		if err != nil {
			return err
		}
		mu.Lock()
		before, observed := baselines[tx]
		delete(baselines, tx)
		mu.Unlock()
		if !observed || n < before {
			return errors.New("original Secret transaction deletion baseline is missing or decreased")
		}
		deletes.before.Store(before)
		deletes.after.Store(n)
		deletes.Store(n - before)
		return nil
	})
	t.Cleanup(func() {
		store.mu.Lock()
		store.before = nil
		store.mu.Unlock()
	})
	return deletes
}

func (v *secretCleanupPG) downstream(t *testing.T) (i.Actor, sc.LifecycleCause) {
	t.Helper()
	r, err := i.RegisterService(i.ProjectLifecycle)
	outboxCleanupRequire(t, err)
	p, err := i.InProject(v.p2.project.ID)
	outboxCleanupRequire(t, err)
	a, err := r.Actor(v.cause.OperationID.String(), p)
	outboxCleanupRequire(t, err)
	o, err := f.ParseID[sc.LifecycleOperation](v.cause.OperationID.String())
	outboxCleanupRequire(t, err)
	c, err := sc.NewLifecycleCause(o, v.cause.ProjectVersion, true)
	outboxCleanupRequire(t, err)
	return a, c
}

func (v *secretCleanupPG) refuseDirect(t *testing.T, ctx context.Context, code f.Code) {
	t.Helper()
	a, c := v.downstream(t)
	run, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	before := v.secretSnapshot(t)
	r, err := v.deps.d04.CleanupProject(run, a, c, v.p2.project.ID, nil)
	var problem *f.Fault
	if !errors.As(err, &problem) || problem.Code != code || !reflect.DeepEqual(r, sc.CleanupReport{}) || v.secretSnapshot(t) != before {
		agentRetirementDiagnostic(t, "secret-direct-without-live-issuer", err)
		t.Fatal("public lifecycle identity or empty rows bypassed the original Secret gate")
	}
}

func (v *secretCleanupPG) finish(t *testing.T, d *project.LifecycleCleanupDriver) {
	t.Helper()
	call := v.run(t, d)
	outboxCleanupWait(t, call.done, "original final Secret head batch")
	outboxCleanupRequire(t, call.err)
	if v.secretCounts(t) != [6]int{} {
		t.Fatal("Secret completed before all its original six metadata tables were empty")
	}
	v.state(t, "completed", "terminal")
	v.refuseDirect(t, context.Background(), f.InvalidState)
	before, calls := v.secretSnapshot(t), v.secret.calls.Load()
	call = v.run(t, d)
	outboxCleanupWait(t, call.done, "original completed Secret replay")
	outboxCleanupRequire(t, call.err)
	if v.secret.calls.Load() != calls || v.secretSnapshot(t) != before {
		t.Fatal("completed Secret replay reinvoked or changed its provider")
	}
	v.state(t, "completed", "terminal")
}

func testProjectSecretCleanupReturn(t *testing.T) {
	v := newSecretCleanupPG(t, 101, false)
	d := v.driver(t)
	v.ready(t, d)
	deletes := v.observeDeletes(t)
	v.barrier = newSchedulerStopBarrier()
	defer v.barrier.open()
	store := v.p2.base.tracked
	store.mu.Lock()
	store.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if v.matches(c) && r.State() == f.Committed {
			v.barrier.hold(r)
		}
	}
	store.mu.Unlock()
	call := v.run(t, d)
	select {
	case <-v.barrier.entered:
	case <-call.done:
		agentRetirementDiagnostic(t, "secret-before-return-barrier", call.err)
		t.Fatal("original Secret return barrier was not reached")
	case <-time.After(5 * time.Second):
		t.Fatal("original Secret return barrier was not reached")
	}
	if deletes.Load() <= 0 || v.secretCounts(t) != [6]int{2, 1, 0, 3, 0, 0} {
		t.Fatal("actual D04 head batch did not retain the exact remainder")
	}
	v.state(t, "required", "running")
	v.refuseDirect(t, context.Background(), f.ResourceBusy)
	d.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err := d.Drain(ctx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("driver drained before original Secret Store return", err)
	}
	select {
	case <-call.done:
		t.Fatal("held Secret call returned before release")
	default:
	}
	v.barrier.open()
	outboxCleanupWait(t, call.done, "cancelled original Secret return")
	if call.err == nil {
		t.Fatal("canceled original Secret cleanup returned success")
	}
	outboxCleanupRequire(t, d.Drain(ctxFor(t)))
	v.clearHooks()
	v.state(t, "required", "terminal")
	v.finish(t, v.driver(t))
	v.close(t)
	t.Log("PROJECT_SECRET_CLEANUP_RETURN formal_101_heads_receipts=true actual_combined_terminal_prefix=true original_d04_store_return_held=true exact_100_head_remainder=true other_project_system_and_nonce_retained=true empty_requires_live_gate=true secret_completed_domains=true no_new_real_lease_case=true")
}

func testProjectSecretCleanupUnknown(t *testing.T) {
	v := newSecretCleanupPG(t, 101, false)
	d := v.driver(t)
	v.ready(t, d)
	store := v.p2.base.tracked
	deletes := v.observeDeletes(t)
	attempt := id[f.TransactionAttempt](t)
	var masked atomic.Bool
	var original f.TransactionCause
	store.mu.Lock()
	store.transformResult = func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
		if v.matches(c) && r.State() == f.Committed && masked.CompareAndSwap(false, true) {
			original = c
			return f.UnknownResult(attempt, c)
		}
		return r
	}
	store.mu.Unlock()
	call := v.run(t, d)
	outboxCleanupWait(t, call.done, "original D04 local acknowledgement Unknown")
	var problem, providerFault *f.Fault
	v.secret.mu.Lock()
	providerErr, report := v.secret.err, v.secret.report
	v.secret.mu.Unlock()
	if !masked.Load() || !v.matches(original) || deletes.Load() <= 0 || !errors.As(call.err, &problem) || problem.Code != f.CommitUnknown || problem.CommitState != f.Unknown || problem.CauseID != attempt.String() || !errors.As(providerErr, &providerFault) || problem != providerFault || report.Details().State != "" {
		agentRetirementDiagnostic(t, "secret-original-unknown", call.err)
		t.Fatal("D04 Unknown lost its exact original attempt/cause or exposed progress")
	}
	if v.secretCounts(t) != [6]int{2, 1, 0, 3, 0, 0} {
		t.Fatal("Unknown projection changed the original physical head deletion")
	}
	v.state(t, "required", "running")
	v.clearHooks()
	before := v.secretSnapshot(t)
	deletes = v.observeDeletes(t)
	v.barrier = newSchedulerStopBarrier()
	defer v.barrier.open()
	store.mu.Lock()
	store.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if v.matches(c) && r.State() == f.Committed {
			v.barrier.hold(r)
		}
	}
	store.mu.Unlock()
	call = v.run(t, d)
	select {
	case <-v.barrier.entered:
	case <-call.done:
		agentRetirementDiagnostic(t, "secret-confirm-before-return", call.err)
		t.Fatal("original Secret confirmation barrier not reached")
	case <-time.After(5 * time.Second):
		t.Fatal("original Secret confirmation barrier not reached")
	}
	confirmedSnapshot := v.secretSnapshot(t)
	t.Logf("PROJECT_SECRET_CONFIRMATION_COUNTS callback_before=%d callback_after=%d original_tx_deletes=%d snapshot_equal=%t", deletes.before.Load(), deletes.after.Load(), deletes.Load(), confirmedSnapshot == before)
	if deletes.Load() != 0 || confirmedSnapshot != before {
		t.Logf("PROJECT_SECRET_CONFIRMATION_DIAGNOSTIC original_tx_deletes=%d snapshot_equal=%t changed_tables=%v remaining=%v", deletes.Load(), confirmedSnapshot == before, secretCleanupChangedTables(t, before, confirmedSnapshot), v.secretCounts(t))
		t.Fatal("Unknown confirmation performed deletion or changed original ciphertext metadata")
	}
	v.state(t, "required", "running")
	v.refuseDirect(t, context.Background(), f.ResourceBusy)
	v.barrier.open()
	outboxCleanupWait(t, call.done, "actual read-only D04 confirmation return")
	outboxCleanupRequire(t, call.err)
	v.clearHooks()
	v.state(t, "pending", "terminal")
	if v.secretSnapshot(t) != before {
		t.Fatal("read-only confirmation changed its retained remainder")
	}
	v.secret.mu.Lock()
	expired := v.secret.ctx
	v.secret.mu.Unlock()
	v.refuseDirect(t, expired, f.Forbidden)
	v.finish(t, d)
	v.close(t)
	t.Log("PROJECT_SECRET_CLEANUP_UNKNOWN actual_commit_local_ackmask=true original_attempt_cause=true zero_unknown_progress=true readonly_confirmation_zero_deletes=true original_confirmation_return_held=true next_explicit_batch=true stale_scope_rejected=true secret_completed_domains=true no_network_fault=true")
}

func testProjectSecretCleanupReference(t *testing.T) {
	v := newSecretCleanupPG(t, 1, true)
	d := v.driver(t)
	v.ready(t, d)
	before, counts := v.secretSnapshot(t), v.secretCounts(t)
	deletes := v.observeDeletes(t)
	call := v.run(t, d)
	outboxCleanupWait(t, call.done, "actual formal Provider reference Pending")
	outboxCleanupRequire(t, call.err)
	if deletes.Load() != 0 || v.secretCounts(t) != counts || v.secretSnapshot(t) != before {
		t.Fatal("Secret cleanup ignored its real Provider-owned Project reference")
	}
	v.clearHooks()
	v.state(t, "pending", "terminal")
	v.secret.mu.Lock()
	expired := v.secret.ctx
	v.secret.mu.Unlock()
	v.refuseDirect(t, expired, f.Forbidden)
	v.close(t)
	t.Log("PROJECT_SECRET_CLEANUP_REFERENCE formal_human_provider_reference=true same_project_exact_owner=true actual_terminal_predecessor=true real_pending_zero_delete=true original_ciphertext_and_other_owners_retained=true no_model_request_or_execution=true no_new_real_lease_case=true")
}
