//go:build integration

package model_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func newID[T any](t *testing.T) f.ID[T] {
	t.Helper()
	v, e := f.NewID[T]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func requireCode(t *testing.T, e error, want f.Code) {
	t.Helper()
	var ff *f.Fault
	if !errors.As(e, &ff) || ff.Code != want {
		t.Fatalf("safe code got %v, want %s", e, want)
	}
}
func recoveryCause(t *testing.T) f.TransactionCause {
	t.Helper()
	v, e := f.NewRecoveryCause("model.fixture", newID[struct{}](t).String(), "")
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func waitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("owned fixture checkpoint not reached")
	}
}

type sharedStore interface {
	model.Store
	audit.Store
}
type liveProcess struct{ id oc.ProcessID }

func (p liveProcess) CurrentProcess() oc.ProcessID { return p.id }
func (p liveProcess) ConfirmStopped(context.Context, oc.ProcessID) error {
	return f.NewFault(f.ResourceBusy, f.NotStarted)
}

type fixture struct {
	db             *pgfixture.Database
	raw            *postgres.Store
	store          sharedStore
	accounts       *account.Authority
	authority      *model.Authority
	service        *model.Service
	secrets        *secret.Service
	aud            *audit.Service
	events         *outbox.Service
	runtime        *outbox.Runtime
	deps           model.Dependencies
	admin, regular id.Actor
}

func testKeys(t *testing.T) (account.Keyring, cursor.Keyring, secret.Keyring) {
	t.Helper()
	b64 := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	ck, e := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, b64(1)))
	if e != nil {
		t.Fatal(e)
	}
	sk, e := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, b64(2)), ck)
	if e != nil {
		t.Fatal(e)
	}
	dk, e := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, b64(3)), ck, sk)
	if e != nil {
		t.Fatal(e)
	}
	ak, e := account.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, b64(4)), ck, sk, dk)
	if e != nil {
		t.Fatal(e)
	}
	return ak, ck, sk
}
func newDatabase(t *testing.T) *pgfixture.Database {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	m, e := postgres.NewMigrator(db.Config(t, nil))
	if e != nil {
		t.Fatal(e)
	}
	if r := m.Migrate(testContext(t)); !r.Migrated {
		t.Fatal("continuous approved migration required", r.Fault)
	}
	conn := db.Connect(t)
	var installed bool
	if e = conn.QueryRow(testContext(t), `SELECT to_regclass('agenteam_model.commands') IS NOT NULL`).Scan(&installed); e != nil || !installed {
		t.Fatal("approved Model migration not installed", e)
	}
	return db
}
func openStore(t *testing.T, cfg postgres.Config) *postgres.Store {
	t.Helper()
	s, e := postgres.Open(testContext(t), cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := s.ForceClose(ctx); e != nil {
			t.Error(e)
		}
	})
	return s
}
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := newDatabase(t)
	raw := openStore(t, db.Config(t, nil))
	v := assemble(t, db, raw, raw)
	v.admin = v.human(t, "admin")
	v.regular = v.human(t, "user")
	return v
}
func assemble(t *testing.T, db *pgfixture.Database, raw *postgres.Store, store sharedStore) *fixture {
	t.Helper()
	ak, ck, sk := testKeys(t)
	accounts, e := account.NewAuthority(store, ak)
	if e != nil {
		t.Fatal(e)
	}
	if e = accounts.Initialize(testContext(t)); e != nil {
		t.Fatal(e)
	}
	authority, e := model.NewAuthority(store, model.Authorizations{Sessions: accounts, System: accounts})
	if e != nil {
		t.Fatal(e)
	}
	aud, e := audit.New(store, ck, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Models: authority})
	if e != nil {
		t.Fatal(e)
	}
	if e = aud.CheckStorage(testContext(t)); e != nil {
		t.Fatal(e)
	}
	router, e := model.NewSecretUsageRouter(authority, accounts)
	if e != nil {
		t.Fatal(e)
	}
	secrets, e := secret.New(store, sk, aud, secret.Authorizations{Sessions: accounts, System: accounts, Usage: router, AccountWrites: accounts})
	if e != nil {
		t.Fatal(e)
	}
	if e = secrets.Initialize(testContext(t)); e != nil {
		t.Fatal(e)
	}
	catalog := ec.NewCatalog()
	typed, e := model.DefineEvents(catalog)
	if e != nil {
		t.Fatal(e)
	}
	events, e := outbox.New(store, catalog, outbox.Authorizations{Producers: map[ec.StableName]oc.ProducerAuthority{model.ModelProducer: authority}, Sessions: accounts, System: accounts, Audit: aud, Cursors: ck, Processes: liveProcess{newID[oc.Process](t)}})
	if e != nil {
		t.Fatal(e)
	}
	runtime, e := outbox.NewRuntime(events, nil, outbox.Options{})
	if e != nil {
		t.Fatal(e)
	}
	if e = runtime.Initialize(testContext(t)); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		runtime.StopClaims()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := runtime.Drain(ctx); e != nil {
			_ = runtime.Force(ctx)
			t.Error(e)
		}
	})
	deps := model.Dependencies{Secret: secrets, Audit: aud, Events: events, ConfigurationEvents: typed, Cursors: ck}
	service, e := model.New(store, authority, deps)
	if e != nil {
		t.Fatal(e)
	}
	if e = service.Initialize(testContext(t)); e != nil {
		t.Fatal(e)
	}
	return &fixture{db: db, raw: raw, store: store, accounts: accounts, authority: authority, service: service, secrets: secrets, aud: aud, events: events, runtime: runtime, deps: deps}
}

// Owned SQL supplies durable User/Session facts. Every permission decision is
// made by the real D07 Authority; no test authorizer grants access.
func (v *fixture) human(t *testing.T, role string) id.Actor {
	t.Helper()
	uid := newID[id.User](t)
	name := "m" + uid.String()[24:]
	_, e := v.raw.Exec(testContext(t), `INSERT INTO agenteam_account.users(id,email,username,display_name,role,password_phc,password_version,version,auth_sequence,initial_password_suggestion,theme) VALUES($1,$2,$3,'Model fixture',$4,'owned-fixture-no-login',1,1,1,false,'system')`, uid.String(), name+"@example.test", name, role)
	if e != nil {
		t.Fatal(e)
	}
	return v.session(t, uid)
}
func (v *fixture) session(t *testing.T, uid id.UserID) id.Actor {
	t.Helper()
	sid := newID[id.Session](t)
	verifier := sha256.Sum256([]byte(sid.String()))
	_, e := v.raw.Exec(testContext(t), `INSERT INTO agenteam_account.sessions(id,user_id,token_verifier,csrf_kid,idle_seconds,absolute_expires_at) VALUES($1,$2,$3,'a',3600,clock_timestamp()+interval '1 day')`, sid.String(), uid.String(), verifier[:])
	if e != nil {
		t.Fatal(e)
	}
	actor, e := id.NewHuman(uid, sid)
	if e != nil {
		t.Fatal(e)
	}
	return actor
}
func (v *fixture) meta(t *testing.T, key string) mc.CommandMeta {
	t.Helper()
	return mc.CommandMeta{Actor: v.admin, Scope: id.SystemScope(), Key: f.IdempotencyKey(key)}
}
func (v *fixture) provider(t *testing.T, protocol mc.Protocol, ref *sc.CredentialRef) mc.ProviderView {
	t.Helper()
	receipt, e := v.service.CreateProvider(testContext(t), mc.CreateProviderRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), Input: mc.ProviderInput{Name: "Provider metadata", Protocol: protocol, BaseURL: "https://provider.example/api", Enabled: true, CredentialRef: ref, Options: json.RawMessage(`{}`)}})
	if e != nil {
		t.Fatal("create Provider", e)
	}
	pid, _ := f.ParseID[mc.Provider](receipt.ResourceID)
	out, e := v.service.GetProvider(testContext(t), v.admin, pid)
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func (v *fixture) model(t *testing.T, provider mc.ProviderView, kind mc.ModelType) mc.ModelView {
	t.Helper()
	input := mc.ModelInput{Name: "Model metadata", ProviderModelID: "explicit-model", Type: kind, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`)}
	if kind == mc.ChatModel {
		input.Capabilities.StructuredOutputModes = []string{"json_schema"}
	}
	receipt, e := v.service.CreateModel(testContext(t), mc.CreateModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ProviderID: provider.ID, Input: input})
	if e != nil {
		t.Fatal(e)
	}
	mid, _ := f.ParseID[mc.Model](receipt.ResourceID)
	out, e := v.service.GetModel(testContext(t), v.admin, mid)
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func (v *fixture) credential(t *testing.T, purpose sc.Purpose) sc.Metadata {
	t.Helper()
	material, e := sc.NewSecretMaterial([]byte("owned-test-credential-never-log"))
	if e != nil {
		t.Fatal(e)
	}
	defer material.Destroy()
	identity, e := f.NewCommandIdentity("secret", []string{v.admin.Details().UserID}, string(sc.Create), f.IdempotencyKey(newID[struct{}](t).String()))
	if e != nil {
		t.Fatal(e)
	}
	result, e := v.secrets.ExecuteWrite(testContext(t), sc.WriteRequest{Actor: v.admin, Scope: id.SystemScope(), Identity: identity, Kind: sc.Create, Purpose: purpose, Value: material})
	if e != nil {
		t.Fatal(e)
	}
	return result.Metadata
}
func (v *fixture) deleteCredential(t *testing.T, metadata sc.Metadata) error {
	t.Helper()
	identity, e := f.NewCommandIdentity("secret", []string{v.admin.Details().UserID}, string(sc.Delete), f.IdempotencyKey(newID[struct{}](t).String()))
	if e != nil {
		t.Fatal(e)
	}
	_, e = v.secrets.ExecuteWrite(testContext(t), sc.WriteRequest{Actor: v.admin, Scope: id.SystemScope(), Identity: identity, Kind: sc.Delete, Purpose: metadata.Purpose, Ref: metadata.CredentialRef, ExpectedVersion: metadata.Version})
	return e
}
func (v *fixture) facts(t *testing.T) (providers, models, commands, audits, events int64) {
	t.Helper()
	e := v.raw.QueryRow(testContext(t), `SELECT (SELECT count(*) FROM agenteam_model.providers),(SELECT count(*) FROM agenteam_model.models),(SELECT count(*) FROM agenteam_model.commands),(SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='model'),(SELECT count(*) FROM agenteam_outbox.events WHERE producer='model')`).Scan(&providers, &models, &commands, &audits, &events)
	if e != nil {
		t.Fatal(e)
	}
	return
}

func TestModelB01CRUDCurrentAuthorityAndCanonicalReplay(t *testing.T) {
	v := newFixture(t)
	ctx := testContext(t)
	input := mc.ProviderInput{Name: "first", Protocol: mc.OpenAIChat, BaseURL: "https://provider.example/api", Enabled: true, Options: json.RawMessage(`{}`)}
	request := mc.CreateProviderRequest{CommandMeta: v.meta(t, "create-provider"), Input: input}
	regular := request
	regular.Actor = v.regular
	_, e := v.service.CreateProvider(ctx, regular)
	requireCode(t, e, f.Forbidden)
	receipt, e := v.service.CreateProvider(ctx, request)
	if e != nil {
		t.Fatal(e)
	}
	pid, _ := f.ParseID[mc.Provider](receipt.ResourceID)
	provider, e := v.service.GetProvider(ctx, v.admin, pid)
	if e != nil {
		t.Fatal(e)
	}
	uid, _ := f.ParseID[id.User](v.admin.Details().UserID)
	request.Actor = v.session(t, uid)
	again, e := v.service.CreateProvider(ctx, request)
	if e != nil || again != receipt {
		t.Fatal("same User/new Session receipt", e)
	}
	different := request
	different.Input.Name = "different"
	_, e = v.service.CreateProvider(ctx, different)
	requireCode(t, e, f.IdempotencyKeyReused)
	changed := provider.Input.Clone()
	changed.Protocol = mc.AnthropicMessages
	_, e = v.service.UpdateProvider(ctx, mc.UpdateProviderRequest{CommandMeta: v.meta(t, "immutable-protocol"), ID: pid, ExpectedVersion: 1, Input: changed})
	requireCode(t, e, f.InvalidArgument)
	changed = provider.Input.Clone()
	changed.Name = "updated"
	update := mc.UpdateProviderRequest{CommandMeta: v.meta(t, "provider-update"), ID: pid, ExpectedVersion: 1, Input: changed}
	updated, e := v.service.UpdateProvider(ctx, update)
	if e != nil || updated.Version != 2 {
		t.Fatal(e)
	}
	same, e := v.service.UpdateProvider(ctx, update)
	if e != nil || same != updated {
		t.Fatal("expected-version preceded receipt", e)
	}
	modelView := v.model(t, provider, mc.ChatModel)
	badModel := modelView.Input.Clone()
	badModel.Type = mc.EmbeddingModel
	_, e = v.service.UpdateModel(ctx, mc.UpdateModelRequest{CommandMeta: v.meta(t, "immutable-type"), ID: modelView.ID, ExpectedVersion: 1, Input: badModel})
	requireCode(t, e, f.InvalidArgument)
	modelChange := modelView.Input.Clone()
	modelChange.Name = "Updated model metadata"
	modelUpdated, e := v.service.UpdateModel(ctx, mc.UpdateModelRequest{CommandMeta: v.meta(t, "update-model"), ID: modelView.ID, ExpectedVersion: 1, Input: modelChange})
	if e != nil || modelUpdated.Version != 2 {
		t.Fatal("update model", e)
	}
	_, e = v.service.DeleteProvider(ctx, mc.DeleteProviderRequest{CommandMeta: v.meta(t, "provider-has-models"), ID: pid, ExpectedVersion: 2})
	requireCode(t, e, f.InvalidState)
	_, e = v.service.DeleteModel(ctx, mc.DeleteModelRequest{CommandMeta: v.meta(t, "delete-model"), ID: modelView.ID, ExpectedVersion: 2})
	if e != nil {
		t.Fatal(e)
	}
	_, e = v.service.DeleteProvider(ctx, mc.DeleteProviderRequest{CommandMeta: v.meta(t, "delete-provider"), ID: pid, ExpectedVersion: 2})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = v.service.GetProvider(ctx, v.admin, pid); e == nil {
		t.Fatal("deleted Provider returned")
	}
	lookup, e := v.service.LookupCommand(ctx, model.LookupCommandRequest{Meta: request.CommandMeta, Command: "provider.create"})
	if e != nil || !lookup.Found || lookup.Receipt == nil || *lookup.Receipt != receipt {
		t.Fatal("historical receipt", e)
	}
	_, e = v.raw.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, request.Actor.Details().SessionID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = v.service.CreateProvider(ctx, request)
	requireCode(t, e, f.SessionRevoked)
	p, m, c, a, ev := v.facts(t)
	if p != 0 || m != 0 || c != 6 || a != 6 || ev != 6 {
		t.Fatalf("unexpected durable totals %d/%d/%d/%d/%d", p, m, c, a, ev)
	}
}

func TestModelB01PlatformSelectionAndAtomicReplacement(t *testing.T) {
	v := newFixture(t)
	ctx := testContext(t)
	empty, e := v.service.GetPlatformSelection(ctx, v.admin)
	if e != nil || empty.Version != 1 || empty.Configured != nil {
		t.Fatal("technical singleton invented a model", e)
	}
	embed := v.model(t, v.provider(t, mc.OpenAIEmbeddings, nil), mc.EmbeddingModel)
	memory := v.model(t, v.provider(t, mc.OpenAIChat, nil), mc.ChatModel)
	rerank := v.model(t, v.provider(t, mc.JinaRerank, nil), mc.RerankerModel)
	selection := mc.PlatformSelection{ID: empty.ID, Version: 1, Embedding: embed.ID, Memory: memory.ID, Reranker: &rerank.ID}
	receipt, e := v.service.UpdatePlatformSelection(ctx, mc.UpdatePlatformSelectionRequest{CommandMeta: v.meta(t, "select"), ExpectedVersion: 1, Selection: selection})
	if e != nil || receipt.Version != 2 {
		t.Fatal(e)
	}
	_, e = v.service.DeleteModel(ctx, mc.DeleteModelRequest{CommandMeta: v.meta(t, "cannot-clear-required"), ID: embed.ID, ExpectedVersion: 1})
	requireCode(t, e, f.InvalidState)
	replacement := v.model(t, v.provider(t, mc.OpenAIEmbeddings, nil), mc.EmbeddingModel)
	replaced, e := v.service.DeleteModel(ctx, mc.DeleteModelRequest{CommandMeta: v.meta(t, "replace-embedding"), ID: embed.ID, ExpectedVersion: 1, Replacement: &replacement.ID})
	if e != nil || replaced.AffectedReferences != 1 {
		t.Fatal("atomic replacement", e)
	}
	got, e := v.service.GetPlatformSelection(ctx, v.admin)
	if e != nil || got.Configured == nil || got.Configured.Embedding != replacement.ID || got.Version != 3 {
		t.Fatal("canonical selector not replaced", e)
	}
	_, e = v.service.DeleteModel(ctx, mc.DeleteModelRequest{CommandMeta: v.meta(t, "clear-optional"), ID: rerank.ID, ExpectedVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	got, e = v.service.GetPlatformSelection(ctx, v.admin)
	if e != nil || got.Configured == nil || got.Configured.Reranker != nil || got.Version != 4 {
		t.Fatal("optional selector not cleared", e)
	}
	var refs, embeddingEvents int
	if e = v.raw.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_model.references WHERE owner_kind='platform_selector' AND owner_id=$1 AND owner_version=4),(SELECT count(*) FROM agenteam_outbox.events WHERE event_type='model.embedding_selection_changed')`, empty.ID).Scan(&refs, &embeddingEvents); e != nil || refs != 2 || embeddingEvents != 2 {
		t.Fatal("reference/event facts", refs, embeddingEvents, e)
	}
	project, owner := newID[id.Project](t).String(), newID[struct{}](t).String()
	_, e = v.raw.Exec(ctx, `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,project_id,model_id,owner_version) VALUES('agent',$1,'approval_model',$2,$3,1)`, owner, project, memory.ID.String())
	if e != nil {
		t.Fatal(e)
	}
	_, e = v.service.DeleteModel(ctx, mc.DeleteModelRequest{CommandMeta: v.meta(t, "unbound-owner"), ID: memory.ID, ExpectedVersion: 1})
	requireCode(t, e, f.DependencyUnbound)
	if _, e = v.service.GetModel(ctx, v.admin, memory.ID); e != nil {
		t.Fatal("unbound owner partially deleted model", e)
	}
}

type rejectingAudit struct {
	inner   ac.Appender
	enabled atomic.Bool
}

func (a *rejectingAudit) AppendInTx(ctx context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) (ac.AppendReceipt, error) {
	r, err := a.inner.AppendInTx(ctx, tx, e, k)
	if err == nil && a.enabled.Load() && ac.ModelAction(e.Fields().Action) {
		return ac.AppendReceipt{}, f.NewFault(f.InvalidState, f.NotStarted)
	}
	return r, err
}

type observedEvents struct {
	inner            oc.Appender
	enabled          atomic.Bool
	once             sync.Once
	entered, release chan struct{}
}

func (o *observedEvents) PrepareAppend(ctx context.Context, a id.Actor, e ec.Event) (oc.AppendPlan, error) {
	p, err := o.inner.PrepareAppend(ctx, a, e)
	if err == nil && o.entered != nil {
		o.once.Do(func() {
			close(o.entered)
			select {
			case <-o.release:
			case <-ctx.Done():
			}
		})
	}
	return p, err
}
func (o *observedEvents) AppendEventInTx(ctx context.Context, tx f.Tx, a id.Actor, e ec.Event, p oc.AppendPlan) (oc.AppendReceipt, error) {
	r, err := o.inner.AppendEventInTx(ctx, tx, a, e, p)
	if err == nil && o.enabled.Load() {
		return oc.AppendReceipt{}, f.NewFault(f.InvalidState, f.NotStarted)
	}
	return r, err
}
func (v *fixture) withDeps(t *testing.T, d model.Dependencies) *model.Service {
	t.Helper()
	s, e := model.New(v.store, v.authority, d)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestModelB01SecretReferencesAndAllEffectsRollback(t *testing.T) {
	v := newFixture(t)
	ctx := testContext(t)
	credential := v.credential(t, sc.Model)
	provider := v.provider(t, mc.OpenAIChat, &credential.CredentialRef)
	if e := v.deleteCredential(t, credential); e == nil {
		t.Fatal("retained Secret deleted")
	}
	other := v.credential(t, sc.Model)
	input := provider.Input.Clone()
	input.CredentialRef = &other.CredentialRef
	receipt, e := v.service.UpdateProvider(ctx, mc.UpdateProviderRequest{CommandMeta: v.meta(t, "replace-secret"), ID: provider.ID, ExpectedVersion: 1, Input: input})
	if e != nil || receipt.Version != 2 {
		t.Fatal(e)
	}
	if e = v.deleteCredential(t, credential); e != nil {
		t.Fatal("released old Secret still retained", e)
	}
	if e = v.deleteCredential(t, other); e == nil {
		t.Fatal("new Secret not retained")
	}
	_, e = v.service.DeleteProvider(ctx, mc.DeleteProviderRequest{CommandMeta: v.meta(t, "delete-provider"), ID: provider.ID, ExpectedVersion: 2})
	if e != nil {
		t.Fatal(e)
	}
	if e = v.deleteCredential(t, other); e != nil {
		t.Fatal(e)
	}
	wrong := v.credential(t, sc.MCP)
	request := mc.CreateProviderRequest{CommandMeta: v.meta(t, "wrong-purpose"), Input: mc.ProviderInput{Name: "wrong", Protocol: mc.OpenAIChat, BaseURL: "https://provider.example/api", Enabled: true, CredentialRef: &wrong.CredentialRef, Options: json.RawMessage(`{}`)}}
	_, e = v.service.CreateProvider(ctx, request)
	requireCode(t, e, f.Forbidden)
	for _, stage := range []string{"audit", "event"} {
		t.Run(stage, func(t *testing.T) {
			retained := v.credential(t, sc.Model)
			p0, m0, c0, a0, e0 := v.facts(t)
			d := v.deps
			var service *model.Service
			if stage == "audit" {
				reject := &rejectingAudit{inner: v.aud}
				reject.enabled.Store(true)
				d.Audit = reject
			} else {
				reject := &observedEvents{inner: v.events}
				reject.enabled.Store(true)
				d.Events = reject
			}
			service = v.withDeps(t, d)
			valid := request
			valid.CommandMeta = v.meta(t, "rollback-"+stage)
			valid.Input.CredentialRef = &retained.CredentialRef
			_, err := service.CreateProvider(testContext(t), valid)
			if err == nil {
				t.Fatal("injected post-effect error swallowed")
			}
			p1, m1, c1, a1, e1 := v.facts(t)
			if p0 != p1 || m0 != m1 || c0 != c1 || a0 != a1 || e0 != e1 {
				t.Fatal("failed effects escaped transaction")
			}
			if err = v.deleteCredential(t, retained); err != nil {
				t.Fatal("failed command retained its Secret reference", err)
			}
		})
	}
}

func TestModelB01CursorAndScopeRemainCurrentlyAuthorized(t *testing.T) {
	v := newFixture(t)
	ctx := testContext(t)
	for range 4 {
		v.provider(t, mc.OpenAIChat, nil)
	}
	page, e := v.service.ListProviders(ctx, v.admin, model.SystemQuery{Limit: 1})
	if e != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatal(e)
	}
	v.provider(t, mc.OpenAIChat, nil)
	uid, _ := f.ParseID[id.User](v.admin.Details().UserID)
	nextSession := v.session(t, uid)
	next, e := v.service.ListProviders(ctx, nextSession, model.SystemQuery{Limit: 100, Cursor: page.NextCursor})
	if e != nil || len(next.Items) != 3 {
		t.Fatal("watermark/limit/Session binding", e, len(next.Items))
	}
	_, e = v.service.ListProviders(ctx, v.regular, model.SystemQuery{Limit: 1, Cursor: page.NextCursor})
	requireCode(t, e, f.Forbidden)
	_, e = v.service.ListProviders(ctx, v.admin, model.SystemQuery{Limit: 1, Cursor: page.NextCursor + "x"})
	requireCode(t, e, f.CursorInvalid)
	_, e = v.raw.Exec(ctx, `UPDATE agenteam_account.users SET role='user',version=version+1 WHERE id=$1`, uid.String())
	if e != nil {
		t.Fatal(e)
	}
	_, e = v.service.ListProviders(ctx, nextSession, model.SystemQuery{Limit: 1, Cursor: page.NextCursor})
	requireCode(t, e, f.Forbidden)
}

func TestModelB01WrongCatalogAndProjectScopeHaveNoBusinessWrites(t *testing.T) {
	v := newFixture(t)
	before := func() [5]int64 { p, m, c, a, e := v.facts(t); return [5]int64{p, m, c, a, e} }()
	d := v.deps
	var e error
	d.ConfigurationEvents, e = model.DefineEvents(ec.NewCatalog())
	if e != nil {
		t.Fatal(e)
	}
	service := v.withDeps(t, d)
	r := mc.CreateProviderRequest{CommandMeta: v.meta(t, "wrong-catalog"), Input: mc.ProviderInput{Name: "wrong catalog", Protocol: mc.OpenAIChat, BaseURL: "https://provider.example/api", Options: json.RawMessage(`{}`)}}
	if _, e = service.CreateProvider(testContext(t), r); e == nil {
		t.Fatal("different catalog accepted")
	}
	scope, _ := id.InProject(newID[id.Project](t))
	r.Scope = scope
	r.Key = "project-unbound"
	_, e = v.service.CreateProvider(testContext(t), r)
	requireCode(t, e, f.DependencyUnbound)
	for _, kind := range []string{"provider.create", "model.delete", "model.selection.update"} {
		lookup, e := v.service.LookupCommand(testContext(t), model.LookupCommandRequest{Meta: v.meta(t, "absent"), Command: kind})
		if e != nil || lookup.Found || lookup.Receipt != nil {
			t.Fatal("missing command became receipt/proof", e)
		}
	}
	p, m, c, a, ev := v.facts(t)
	if before != ([5]int64{p, m, c, a, ev}) {
		t.Fatal("wrong catalog/project wrote business facts")
	}
}

// The framing-only loopback proxy retains the real PostgreSQL writer and
// drops its actual COMMIT reply. It is adapted from the fixed e6e94c4 Project
// fixture; no transaction result or domain authority is replaced.
type modelCommitProxy struct {
	listener                          net.Listener
	upstream                          string
	reached, release, completed, quit chan struct{}
	armed                             atomic.Bool
	once                              sync.Once
	wg                                sync.WaitGroup
	mu                                sync.Mutex
	connections                       map[net.Conn]bool
	commit                            bool
}

func newModelCommitProxy(t *testing.T, upstream string, commit bool) *modelCommitProxy {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := &modelCommitProxy{listener: listener, upstream: upstream, reached: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{}), quit: make(chan struct{}), connections: make(map[net.Conn]bool), commit: commit}
	p.armed.Store(false)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, e := listener.Accept()
			if e != nil {
				return
			}
			p.mu.Lock()
			p.connections[client] = true
			p.mu.Unlock()
			p.wg.Add(1)
			go p.serve(client)
		}
	}()
	t.Cleanup(func() {
		p.once.Do(func() {
			close(p.quit)
			_ = p.listener.Close()
			p.mu.Lock()
			for c := range p.connections {
				_ = c.Close()
			}
			p.mu.Unlock()
			p.wg.Wait()
		})
	})
	return p
}
func (p *modelCommitProxy) serve(client net.Conn) {
	defer p.wg.Done()
	defer func() { _ = client.Close(); p.mu.Lock(); delete(p.connections, client); p.mu.Unlock() }()
	server, e := net.Dial("tcp", p.upstream)
	if e != nil {
		return
	}
	p.mu.Lock()
	p.connections[server] = true
	p.mu.Unlock()
	defer func() { _ = server.Close(); p.mu.Lock(); delete(p.connections, server); p.mu.Unlock() }()
	var header [4]byte
	if _, e = io.ReadFull(client, header[:]); e != nil {
		return
	}
	size := binary.BigEndian.Uint32(header[:])
	if size < 8 || size > 1<<20 {
		return
	}
	startup := make([]byte, int(size))
	copy(startup, header[:])
	if _, e = io.ReadFull(client, startup[4:]); e != nil {
		return
	}
	if _, e = server.Write(startup); e != nil {
		return
	}
	var held atomic.Bool
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, e := readModelPGFrame(client)
			if e != nil {
				return
			}
			if frame[0] == 'Q' && strings.EqualFold(strings.Trim(string(frame[5:]), "\x00; \t\r\n"), "COMMIT") && p.armed.CompareAndSwap(true, false) {
				held.Store(true)
				_ = client.Close()
				close(p.reached)
				select {
				case <-p.release:
				case <-p.quit:
					return
				}
				if !p.commit {
					_ = server.Close()
					close(p.completed)
					return
				}
				if _, e = server.Write(frame); e != nil {
					return
				}
				select {
				case <-p.completed:
				case <-p.quit:
				}
				return
			}
			if _, e = server.Write(frame); e != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, e := readModelPGFrame(server)
			if e != nil {
				return
			}
			if held.Load() && frame[0] == 'C' && string(frame[5:]) == "COMMIT\x00" {
				ready, e := readModelPGFrame(server)
				if e == nil && ready[0] == 'Z' && len(ready) == 6 && ready[5] == 'I' {
					close(p.completed)
				}
				return
			}
			if _, e = client.Write(frame); e != nil {
				return
			}
		}
	}()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
}

func readModelPGFrame(reader io.Reader) ([]byte, error) {
	var header [5]byte
	if _, e := io.ReadFull(reader, header[:]); e != nil {
		return nil, e
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size < 4 || size > 1<<20 {
		return nil, errors.New("invalid fixture frame")
	}
	frame := make([]byte, int(size)+1)
	copy(frame, header[:])
	_, e := io.ReadFull(reader, frame[5:])
	return frame, e
}

type commitStore struct {
	*postgres.Store
	proxy                          *modelCommitProxy
	armed, fired                   atomic.Bool
	mu                             sync.Mutex
	original                       f.CommitResult
	unknownReached, confirmRelease chan struct{}
}

func (w *commitStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	r := w.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := fn(ctx, tx); e != nil {
			return e
		}
		if !w.armed.Load() || cause.Kind() != f.CommandsCause || cause.Details().Primary.Namespace() != "model.system" || w.fired.Load() {
			return nil
		}
		x, e := w.InTx(tx)
		if e != nil {
			return e
		}
		var committed bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.commands WHERE command_identity=$1 AND phase='committed')`, cause.Details().Primary.Canonical()).Scan(&committed); e != nil {
			return e
		}
		if committed && w.fired.CompareAndSwap(false, true) {
			w.proxy.armed.Store(true)
		}
		return nil
	})
	if r.State() == f.Unknown && w.fired.Load() {
		w.mu.Lock()
		w.original = r
		w.mu.Unlock()
		if w.unknownReached != nil {
			close(w.unknownReached)
			select {
			case <-w.confirmRelease:
			case <-ctx.Done():
			}
		}
	}
	return r
}
func proxyFixture(t *testing.T, commit bool) (*fixture, *commitStore, *modelCommitProxy) {
	t.Helper()
	db := newDatabase(t)
	proxy := newModelCommitProxy(t, net.JoinHostPort("127.0.0.1", db.Fixture.Port), commit)
	u, e := url.Parse(db.Fixture.URL(db.Name))
	if e != nil {
		t.Fatal(e)
	}
	u.Host = proxy.listener.Addr().String()
	raw := openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	store := &commitStore{Store: raw, proxy: proxy}
	v := assemble(t, db, raw, store)
	v.admin = v.human(t, "admin")
	return v, store, proxy
}
func TestModelB01RealLostCommitReplyPreservesOriginalCauseAndCurrentFacts(t *testing.T) {
	for _, scenario := range []string{"late-commit", "rollback", "confirmation-timeout"} {
		t.Run(scenario, func(t *testing.T) {
			v, w, proxy := proxyFixture(t, scenario == "late-commit")
			request := mc.CreateProviderRequest{CommandMeta: v.meta(t, "unknown-provider"), Input: mc.ProviderInput{Name: "unknown", Protocol: mc.OpenAIChat, BaseURL: "https://provider.example/api", Options: json.RawMessage(`{}`)}}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			w.armed.Store(true)
			type response struct {
				receipt mc.CommandReceipt
				err     error
			}
			done := make(chan response, 1)
			go func() { r, e := v.service.CreateProvider(ctx, request); done <- response{r, e} }()
			waitSignal(t, proxy.reached)
			select {
			case <-done:
				t.Fatal("confirmation escaped the original live writer lock")
			case <-time.After(30 * time.Millisecond):
			}
			if scenario != "confirmation-timeout" {
				close(proxy.release)
				waitSignal(t, proxy.completed)
			}
			var got response
			select {
			case got = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("model command did not return")
			}
			if scenario == "late-commit" {
				if got.err != nil || got.receipt.ResourceID == "" {
					t.Fatal("original commit not confirmed", got.err)
				}
				p, _, c, a, e := v.facts(t)
				if p != 1 || c != 1 || a != 1 || e != 1 {
					t.Fatal("duplicate/lost committed effects")
				}
			} else {
				var unknown *model.UnknownCommandError
				if !errors.As(got.err, &unknown) || got.receipt != (mc.CommandReceipt{}) {
					t.Fatal("unconfirmed write returned success or rollback", got.err)
				}
				w.mu.Lock()
				original := w.original
				w.mu.Unlock()
				if unknown.AttemptID() != original.AttemptID() || unknown.Cause().Details().Primary.Canonical() != original.Cause().Details().Primary.Canonical() {
					t.Fatal("original Unknown identity changed")
				}
				if scenario == "confirmation-timeout" {
					close(proxy.release)
					waitSignal(t, proxy.completed)
				}
				p, _, c, a, e := v.facts(t)
				if p != 0 || c != 0 || a != 0 || e != 0 {
					t.Fatal("rolled-back mutation leaked facts")
				}
				lookup, lookupErr := v.service.LookupCommand(testContext(t), model.LookupCommandRequest{Meta: request.CommandMeta, Command: "provider.create"})
				if lookupErr != nil || lookup.Found || lookup.Receipt != nil {
					t.Fatal("public absent lookup", lookupErr)
				}
			}
		})
	}
}

func TestModelB01RollbackUnknownCannotAdoptDifferentRequestCommit(t *testing.T) {
	v, w, proxy := proxyFixture(t, false)
	w.unknownReached = make(chan struct{})
	w.confirmRelease = make(chan struct{})
	request := mc.CreateProviderRequest{CommandMeta: v.meta(t, "same-key-two-meanings"), Input: mc.ProviderInput{Name: "request-A", Protocol: mc.OpenAIChat, BaseURL: "https://provider.example/api", Options: json.RawMessage(`{}`)}}
	w.armed.Store(true)
	done := make(chan error, 1)
	go func() { _, e := v.service.CreateProvider(testContext(t), request); done <- e }()
	waitSignal(t, proxy.reached)
	close(proxy.release)
	waitSignal(t, proxy.completed)
	waitSignal(t, w.unknownReached)
	raw := openStore(t, v.db.Config(t, nil))
	other := assemble(t, v.db, raw, raw)
	other.admin = v.admin
	different := request
	different.Input.Name = "request-B"
	b, e := other.service.CreateProvider(testContext(t), different)
	if e != nil {
		t.Fatal("B should commit after A rollback", e)
	}
	close(w.confirmRelease)
	select {
	case e = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("A confirmation did not return")
	}
	requireCode(t, e, f.IdempotencyKeyReused)
	lookup, e := v.service.LookupCommand(testContext(t), model.LookupCommandRequest{Meta: request.CommandMeta, Command: "provider.create"})
	if e != nil || !lookup.Found || lookup.Receipt == nil || *lookup.Receipt != b {
		t.Fatal("public lookup must report B history without confirming A", e)
	}
	p, _, c, a, ev := v.facts(t)
	if p != 1 || c != 1 || a != 1 || ev != 1 {
		t.Fatal("B had duplicate Audit/Event or A committed")
	}
}

type pauseRow struct {
	inner postgres.Row
	store *discoveryStore
}

func (r pauseRow) Scan(values ...any) error {
	e := r.inner.Scan(values...)
	if errors.Is(e, pgx.ErrNoRows) && r.store.armed.CompareAndSwap(true, false) {
		close(r.store.entered)
		select {
		case <-r.store.release:
		case <-r.store.ctx.Done():
		}
	}
	return e
}

type discoveryStore struct {
	*postgres.Store
	armed            atomic.Bool
	target           string
	entered, release chan struct{}
	ctx              context.Context
}

func (s *discoveryStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	row := s.Store.QueryRow(ctx, query, args...)
	if strings.Contains(query, "FROM agenteam_model.commands") && len(args) == 1 && args[0] == s.target {
		return pauseRow{inner: row, store: s}
	}
	return row
}

func TestModelB01ConcurrentSameKeyPreparationFailureStillReplaysReceipt(t *testing.T) {
	for _, operation := range []string{"update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			v := newFixture(t)
			ctx := testContext(t)
			provider := v.provider(t, mc.OpenAIChat, nil)
			original := v.model(t, provider, mc.ChatModel)
			meta := v.meta(t, "concurrent-"+operation)
			ci, e := f.NewCommandIdentity("model.system", []string{meta.Actor.Details().UserID}, "model."+operation, meta.Key)
			if e != nil {
				t.Fatal(e)
			}
			raw := openStore(t, v.db.Config(t, nil))
			paused := &discoveryStore{Store: raw, target: ci.Canonical(), entered: make(chan struct{}), release: make(chan struct{}), ctx: ctx}
			other := assemble(t, v.db, raw, paused)
			paused.armed.Store(true)
			invoke := func(service *model.Service) (mc.CommandReceipt, error) {
				if operation == "delete" {
					return service.DeleteModel(ctx, mc.DeleteModelRequest{CommandMeta: meta, ID: original.ID, ExpectedVersion: 1})
				}
				input := original.Input.Clone()
				input.Name = "new model name"
				return service.UpdateModel(ctx, mc.UpdateModelRequest{CommandMeta: meta, ID: original.ID, ExpectedVersion: 1, Input: input})
			}
			type response struct {
				receipt mc.CommandReceipt
				err     error
			}
			done := make(chan response, 1)
			go func() { r, e := invoke(other.service); done <- response{r, e} }()
			waitSignal(t, paused.entered)
			first, e := invoke(v.service)
			if e != nil {
				t.Fatal(e)
			}
			close(paused.release)
			select {
			case second := <-done:
				if second.err != nil || second.receipt != first {
					t.Fatal("concurrent canonical receipt lost behind preparation error", second.err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("waiting request did not return")
			}
			var audits, events int
			if e = v.raw.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_audit.audit_records WHERE action=$1),(SELECT count(*) FROM agenteam_outbox.events WHERE aggregate_id=$2 AND aggregate_version=2)`, "model."+operation, original.ID.String()).Scan(&audits, &events); e != nil || audits != 1 || events != 1 {
				t.Fatal("concurrent replay duplicated effects", e)
			}
		})
	}
}

func TestModelB01PreparedReferencesAndCurrentAdminAreRechecked(t *testing.T) {
	for _, scenario := range []string{"secret-deleted", "admin-revoked", "reference-added"} {
		t.Run(scenario, func(t *testing.T) {
			v := newFixture(t)
			ctx := testContext(t)
			var run func(*model.Service) error
			var intervene func()
			var inspect func()
			switch scenario {
			case "secret-deleted":
				credential := v.credential(t, sc.Model)
				request := mc.CreateProviderRequest{CommandMeta: v.meta(t, "stale-secret"), Input: mc.ProviderInput{Name: "planned reference", Protocol: mc.OpenAIChat, BaseURL: "https://provider.example/api", Enabled: true, CredentialRef: &credential.CredentialRef, Options: json.RawMessage(`{}`)}}
				run = func(s *model.Service) error { _, e := s.CreateProvider(ctx, request); return e }
				intervene = func() {
					if e := v.deleteCredential(t, credential); e != nil {
						t.Fatal("unretained Secret delete", e)
					}
				}
				inspect = func() {
					p, m, c, a, e := v.facts(t)
					if p+m+c+a+e != 0 {
						t.Fatal("stale Secret discovery caused partial write")
					}
				}
			case "admin-revoked":
				request := mc.CreateProviderRequest{CommandMeta: v.meta(t, "revoked-after-preparation"), Input: mc.ProviderInput{Name: "current admin", Protocol: mc.OpenAIChat, BaseURL: "https://provider.example/api", Options: json.RawMessage(`{}`)}}
				run = func(s *model.Service) error { _, e := s.CreateProvider(ctx, request); return e }
				intervene = func() {
					_, e := v.raw.Exec(ctx, `UPDATE agenteam_account.users SET role='user',version=version+1 WHERE id=$1`, v.admin.Details().UserID)
					if e != nil {
						t.Fatal(e)
					}
				}
				inspect = func() {
					p, m, c, a, e := v.facts(t)
					if p+m+c+a+e != 0 {
						t.Fatal("revoked admin caused partial write")
					}
				}
			case "reference-added":
				old := v.model(t, v.provider(t, mc.OpenAIEmbeddings, nil), mc.EmbeddingModel)
				next := v.model(t, v.provider(t, mc.OpenAIEmbeddings, nil), mc.EmbeddingModel)
				memory := v.model(t, v.provider(t, mc.OpenAIChat, nil), mc.ChatModel)
				state, e := v.service.GetPlatformSelection(ctx, v.admin)
				if e != nil {
					t.Fatal(e)
				}
				request := mc.DeleteModelRequest{CommandMeta: v.meta(t, "delete-racing-selector"), ID: old.ID, ExpectedVersion: 1, Replacement: &next.ID}
				run = func(s *model.Service) error { _, e := s.DeleteModel(ctx, request); return e }
				intervene = func() {
					_, e := v.service.UpdatePlatformSelection(ctx, mc.UpdatePlatformSelectionRequest{CommandMeta: v.meta(t, "new-selector"), ExpectedVersion: state.Version, Selection: mc.PlatformSelection{ID: state.ID, Version: state.Version, Embedding: old.ID, Memory: memory.ID}})
					if e != nil {
						t.Fatal(e)
					}
				}
				inspect = func() {
					current, e := v.service.GetPlatformSelection(ctx, v.admin)
					if e != nil || current.Configured == nil || current.Configured.Embedding != old.ID {
						t.Fatal("new reference lost", e)
					}
					if _, e = v.service.GetModel(ctx, v.admin, old.ID); e != nil {
						t.Fatal("stale delete removed referenced model", e)
					}
					receipt, e := v.service.DeleteModel(ctx, request)
					if e != nil || receipt.AffectedReferences != 1 {
						t.Fatal("same-key fresh plan failed", e)
					}
					current, e = v.service.GetPlatformSelection(ctx, v.admin)
					if e != nil || current.Configured.Embedding != next.ID {
						t.Fatal("replanned replacement lost selector", e)
					}
				}
			}
			gate := &observedEvents{inner: v.events, entered: make(chan struct{}), release: make(chan struct{})}
			var release sync.Once
			unblock := func() { release.Do(func() { close(gate.release) }) }
			t.Cleanup(unblock)
			d := v.deps
			d.Events = gate
			s := v.withDeps(t, d)
			done := make(chan error, 1)
			go func() { done <- run(s) }()
			waitSignal(t, gate.entered)
			intervene()
			unblock()
			select {
			case e := <-done:
				if e == nil {
					t.Fatal("stale discovery authorized success")
				}
				if scenario == "admin-revoked" {
					requireCode(t, e, f.Forbidden)
				}
			case <-ctx.Done():
				t.Fatal("writer failed to join")
			}
			inspect()
		})
	}
}

func TestModelB01SecretReleaseSerializesWithActualDelete(t *testing.T) {
	v := newFixture(t)
	ctx := testContext(t)
	credential := v.credential(t, sc.Model)
	provider := v.provider(t, mc.OpenAIChat, &credential.CredentialRef)
	gate := &observedEvents{inner: v.events, entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(gate.release) }) }
	t.Cleanup(release)
	d := v.deps
	d.Events = gate
	s := v.withDeps(t, d)
	input := provider.Input.Clone()
	input.CredentialRef = nil
	done := make(chan error, 1)
	go func() {
		_, e := s.UpdateProvider(ctx, mc.UpdateProviderRequest{CommandMeta: v.meta(t, "release-reference"), ID: provider.ID, ExpectedVersion: 1, Input: input})
		done <- e
	}()
	waitSignal(t, gate.entered)
	requireCode(t, v.deleteCredential(t, credential), f.ResourceBusy)
	release()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal("release failed to join")
	}
	if e := v.deleteCredential(t, credential); e != nil {
		t.Fatal("exact released reference remained", e)
	}
	got, e := v.service.GetProvider(ctx, v.admin, provider.ID)
	if e != nil || got.Input.CredentialRef != nil || got.Version != 2 {
		t.Fatal("release did not commit canonical Provider", e)
	}
}

const modelMigrationName = "00015_model_configuration.sql"

func modelMigrationFiles(t *testing.T, through string) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	names, e := fs.Glob(migrations.SQL, "*.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range names {
		if name[:5] > through {
			continue
		}
		raw, e := fs.ReadFile(migrations.SQL, name)
		if e != nil {
			t.Fatal(e)
		}
		out[name] = &fstest.MapFile{Data: raw}
	}
	return out
}
func modelMigrator(t *testing.T, db *pgfixture.Database, files fstest.MapFS) *postgres.Migrator {
	t.Helper()
	source, e := postgres.NewSource(files, nil)
	if e != nil {
		t.Fatal(e)
	}
	m, e := postgres.NewMigrator(db.Config(t, nil), source)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func migrateModel(t *testing.T, m *postgres.Migrator, version int64) {
	t.Helper()
	r := m.Migrate(testContext(t))
	if !r.Migrated || r.Fault != nil || r.Version != version {
		t.Fatalf("migration result migrated=%v version=%d fault=%v", r.Migrated, r.Version, r.Fault)
	}
}
func oldAuditFacts(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var out string
	e := conn.QueryRow(testContext(t), `SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb)::text FROM agenteam_audit.audit_records t`).Scan(&out)
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func oldModelMigrationConstraints(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var out string
	e := conn.QueryRow(testContext(t), `SELECT coalesce(jsonb_agg(jsonb_build_array(n.nspname,c.relname,k.conname,pg_get_constraintdef(k.oid)) ORDER BY n.nspname,c.relname,k.conname),'[]'::jsonb)::text FROM pg_constraint k JOIN pg_namespace n ON n.oid=k.connamespace LEFT JOIN pg_class c ON c.oid=k.conrelid WHERE n.nspname LIKE 'agenteam_%' AND n.nspname<>'agenteam_model' AND NOT(n.nspname='agenteam_audit' AND k.conname IN ('audit_records_action_check','audit_records_resource_kind_check','audit_records_producer_check','audit_records_check2','audit_records_model_contract'))`).Scan(&out)
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func seedOldModelMigrationAudit(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	_, e := conn.Exec(testContext(t), `INSERT INTO agenteam_audit.audit_records(id,scope,actor_kind,user_id,session_id,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal) VALUES($1,'system','human',$2,$3,'secret.create','success','secret',$4,'{}','sha256:'||repeat('1',64),'secret','sha256:'||repeat('2',64),0)`, newID[struct{}](t).String(), newID[id.User](t).String(), newID[id.Session](t).String(), newID[struct{}](t).String())
	if e != nil {
		t.Fatal("old Audit fixture", e)
	}
}
func modelSchemaCount(t *testing.T, conn *pgx.Conn, want int) {
	t.Helper()
	var got int
	e := conn.QueryRow(testContext(t), `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='agenteam_model' AND c.relkind='r'`).Scan(&got)
	if e != nil || got != want {
		t.Fatalf("model table count %d want %d: %v", got, want, e)
	}
}
func TestModelB01MigrationFreshUpgradeAndAtomicRetry(t *testing.T) {
	for _, scenario := range []string{"fresh", "populated-fourteen", "rollback-same-checksum"} {
		t.Run(scenario, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			conn := db.Connect(t)
			var before, oldChecks string
			if scenario != "fresh" {
				migrateModel(t, modelMigrator(t, db, modelMigrationFiles(t, "00014")), 14)
				seedOldModelMigrationAudit(t, conn)
				before = oldAuditFacts(t, conn)
				oldChecks = oldModelMigrationConstraints(t, conn)
			}
			files := modelMigrationFiles(t, "00015")
			if scenario == "rollback-same-checksum" {
				raw := append([]byte(nil), files[modelMigrationName].Data...)
				files[modelMigrationName] = &fstest.MapFile{Data: append(raw, []byte("\nSELECT model_fixture_migration_dependency();\n")...)}
				runner := modelMigrator(t, db, files)
				result := runner.Migrate(testContext(t))
				var pg *pgconn.PgError
				if result.Migrated || !errors.As(result.Fault, &pg) || pg.Code != "42883" {
					t.Fatalf("expected injected missing function, got %v", result.Fault)
				}
				modelSchemaCount(t, conn, 0)
				if before != oldAuditFacts(t, conn) || oldChecks != oldModelMigrationConstraints(t, conn) {
					t.Fatal("failed DDL changed old facts/checks")
				}
				var pending, applied, goose int
				var checksum string
				e := conn.QueryRow(testContext(t), `SELECT (SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=15 AND state='pending'),(SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=15 AND state='applied'),(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=15 AND is_applied),(SELECT checksum FROM agenteam_meta.migration_journal WHERE version=15)`).Scan(&pending, &applied, &goose, &checksum)
				if e != nil || pending != 1 || applied != 0 || goose != 0 {
					t.Fatal("journal rollback", e)
				}
				if _, e = conn.Exec(testContext(t), `CREATE FUNCTION public.model_fixture_migration_dependency() RETURNS void LANGUAGE SQL AS 'SELECT NULL::void'`); e != nil {
					t.Fatal(e)
				}
				migrateModel(t, runner, 15)
				var actual string
				if e = conn.QueryRow(testContext(t), `SELECT checksum FROM agenteam_meta.migration_journal WHERE version=15 AND state='applied'`).Scan(&actual); e != nil || actual != checksum {
					t.Fatal("retry changed checksum", e)
				}
			} else {
				migrateModel(t, modelMigrator(t, db, files), 15)
			}
			modelSchemaCount(t, conn, 5)
			if scenario != "fresh" && (before != oldAuditFacts(t, conn) || oldChecks != oldModelMigrationConstraints(t, conn)) {
				t.Fatal("upgrade changed old facts/checks")
			}
			migrateModel(t, modelMigrator(t, db, files), 15)
			var invented, applied, goose int
			e := conn.QueryRow(testContext(t), `SELECT (SELECT count(*) FROM agenteam_model.providers)+(SELECT count(*) FROM agenteam_model.models)+(SELECT count(*) FROM agenteam_model.platform_selection)+(SELECT count(*) FROM agenteam_model.commands)+(SELECT count(*) FROM agenteam_model.references),(SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=15 AND state='applied'),(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=15 AND is_applied)`).Scan(&invented, &applied, &goose)
			if e != nil || invented != 0 || applied != 1 || goose != 1 {
				t.Fatal("migration fabricated defaults or applied twice", e)
			}
		})
	}
}
func expectModelSQLState(t *testing.T, conn *pgx.Conn, state, query string, args ...any) {
	t.Helper()
	_, e := conn.Exec(testContext(t), query, args...)
	var pg *pgconn.PgError
	if !errors.As(e, &pg) || pg.Code != state {
		t.Fatalf("SQL state expected %s, got %T %v", state, e, e)
	}
}
func TestModelB01SchemaRejectsNullAndInvalidCompositeFacts(t *testing.T) {
	v := newFixture(t)
	ctx := testContext(t)
	conn := v.db.Connect(t)
	provider := v.provider(t, mc.OpenAIChat, nil)
	chat := v.model(t, provider, mc.ChatModel)
	for _, change := range []string{"scope=NULL", "scope='project',project_id=NULL", "scope='system',project_id='01900000-0000-7000-8000-000000000001'", "version=0", "provider_options='null'::jsonb", "protocol='unregistered'"} {
		state := "23514"
		if change == "scope=NULL" {
			state = "23502"
		}
		expectModelSQLState(t, conn, state, `UPDATE agenteam_model.providers SET `+change+` WHERE id=$1`, provider.ID.String())
	}
	expectModelSQLState(t, conn, "23503", `UPDATE agenteam_model.models SET type='embedding' WHERE id=$1`, chat.ID.String())
	expectModelSQLState(t, conn, "23514", `UPDATE agenteam_model.platform_selection SET configured=true`)
	expectModelSQLState(t, conn, "23514", `UPDATE agenteam_model.platform_selection SET embedding_id=$1`, chat.ID.String())
	expectModelSQLState(t, conn, "23514", `UPDATE agenteam_model.commands SET safe_receipt=NULL WHERE phase='committed'`)
	expectModelSQLState(t, conn, "23514", `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,project_id,model_id,owner_version) VALUES('agent',$1,'approval_model',NULL,$2,1)`, newID[struct{}](t).String(), chat.ID.String())
	expectModelSQLState(t, conn, "23514", `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,project_id,model_id,owner_version) VALUES('project_summary',$1,'meeting_summary',$2,$3,1)`, newID[struct{}](t).String(), newID[id.Project](t).String(), chat.ID.String())
	var auditID string
	if e := conn.QueryRow(ctx, `SELECT id::text FROM agenteam_audit.audit_records WHERE action='provider.create' AND resource_id=$1`, provider.ID.String()).Scan(&auditID); e != nil {
		t.Fatal(e)
	}
	for _, change := range []string{"metadata=metadata-'version'", "metadata=jsonb_set(metadata,'{version}','null')", "metadata=jsonb_set(metadata,'{version}','0')", "metadata=metadata||'{\"unexpected\":true}'::jsonb", "metadata=jsonb_set(metadata,'{changed_fields}','[\"created\",\"created\"]')", "producer='secret'", "action='secret.create'", "resource_kind='secret'", "ordinal=1", "outcome='failure'", "resource_id=NULL"} {
		expectModelSQLState(t, conn, "23514", `UPDATE agenteam_audit.audit_records SET `+change+` WHERE id=$1`, auditID)
	}
	// Project storage exists for future adapters, but current reads must not leak it.
	project := newID[id.Project](t).String()
	foreign := newID[mc.Provider](t)
	foreignModel := newID[mc.Model](t)
	_, e := conn.Exec(ctx, `INSERT INTO agenteam_model.providers(id,scope,project_id,name,protocol,base_url,provider_options,enabled,version,created_at,updated_at) VALUES($1,'project',$2,'future','openai-chat-completions','https://provider.example','{}',true,1,clock_timestamp(),clock_timestamp());`, foreign.String(), project)
	if e != nil {
		t.Fatal(e)
	}
	_, e = conn.Exec(ctx, `INSERT INTO agenteam_model.models(id,provider_id,type,name,provider_model_id,parameters,request_overwrite,header_overwrite,capabilities,enabled,version,created_at,updated_at) VALUES($1,$2,'chat','future','explicit','{}','{}','{}','{}',true,1,clock_timestamp(),clock_timestamp())`, foreignModel.String(), foreign.String())
	if e != nil {
		t.Fatal(e)
	}
	_, e = v.service.GetProvider(ctx, v.admin, foreign)
	requireCode(t, e, f.NotFound)
	_, e = v.service.GetModel(ctx, v.admin, foreignModel)
	requireCode(t, e, f.NotFound)
	_, e = v.service.ListModels(ctx, v.admin, foreign, model.SystemQuery{Limit: 10})
	requireCode(t, e, f.NotFound)
	page, e := v.service.ListProviders(ctx, v.admin, model.SystemQuery{Limit: 100})
	if e != nil || len(page.Items) != 1 || page.Items[0].ID != provider.ID {
		t.Fatal("System page leaked Project Provider", e)
	}
}
