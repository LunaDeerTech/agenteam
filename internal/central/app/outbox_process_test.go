//go:build integration

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	objectcontract "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type guardOutboxPayload struct {
	Version int `json:"version"`
}
type guardOutboxHandler struct {
	store   *postgres.Store
	issuer  oc.PlanIssuer
	entered chan struct{}
	release <-chan struct{}
}

func (h *guardOutboxHandler) Prepare(_ context.Context, e event.Event) (oc.HandlerPlan, error) {
	d, _ := oc.EventDigest(e)
	deps, err := oc.NewDependencies(h.issuer, d, nil, nil)
	if err != nil {
		return oc.HandlerPlan{}, err
	}
	return oc.NewHandlerPlan("guard.handler", e, deps)
}
func (h *guardOutboxHandler) ValidateInTx(_ context.Context, _ foundation.Tx, e event.Event, p oc.HandlerPlan) error {
	d, _ := oc.EventDigest(e)
	if !p.Dependencies().Matches(h.issuer, d) {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	return nil
}
func (h *guardOutboxHandler) HandleInTx(ctx context.Context, tx foundation.Tx, e event.Event, _ oc.HandlerPlan) oc.Result {
	x, err := h.store.InTx(tx)
	if err != nil {
		return oc.Retry(oc.Unavailable)
	}
	if _, err = x.Exec(ctx, `INSERT INTO outbox_guard_effect(id) VALUES($1)`, e.Header().EventID.String()); err != nil {
		return oc.Retry(oc.Unavailable)
	}
	if h.entered != nil {
		close(h.entered)
		<-h.release
	} // Explicitly non-cooperative fixture only.
	return oc.Ack(oc.DigestBytes([]byte("guard-current-generation")))
}

type guardProducer struct{ issuer oc.PlanIssuer }

func (p guardProducer) binding(a identity.Actor, s event.Summary) foundation.Digest {
	actor, _ := oc.StableActor(a)
	raw, _ := json.Marshal(struct {
		Actor   string
		Summary event.Summary
	}{actor, s})
	return oc.DigestBytes(raw)
}
func (p guardProducer) DiscoverAppend(_ context.Context, a identity.Actor, s event.Summary) (oc.Dependencies, error) {
	return oc.NewDependencies(p.issuer, p.binding(a, s), nil, nil)
}
func (p guardProducer) ValidateAppendInTx(_ context.Context, _ foundation.Tx, a identity.Actor, s event.Summary, d oc.Dependencies, _ oc.Stage) error {
	if !d.Matches(p.issuer, p.binding(a, s)) || a.Details().Kind != identity.Human {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	return nil
}
func guardOutbox(t *testing.T, store *postgres.Store, objects *objectAssembly, entered chan struct{}, release <-chan struct{}) (*outbox.Service, *outbox.Runtime, event.EventType[guardOutboxPayload]) {
	t.Helper()
	cat := event.NewCatalog()
	typ, err := event.DefineEvent(cat, event.Definition[guardOutboxPayload]{Schema: event.Schema{Producer: "guard", EventType: "guard.changed", AggregateType: "guard", Version: 1}, Codec: event.JSONCodec[guardOutboxPayload]{}, Validate: func(p guardOutboxPayload) error {
		if p.Version != 1 {
			return errors.New("invalid fixture version")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := outbox.New(store, cat, outbox.Authorizations{Processes: outboxProcessAuthority{objects}, Producers: map[event.StableName]oc.ProducerAuthority{"guard": guardProducer{oc.NewPlanIssuer()}}})
	if err != nil {
		t.Fatal(err)
	}
	h := &guardOutboxHandler{store: store, issuer: oc.NewPlanIssuer(), entered: entered, release: release}
	r, err := outbox.NewRuntime(svc, []oc.HandlerDefinition{{ID: "guard.handler", Subscriptions: []oc.Subscription{{EventType: "guard.changed", Versions: []uint32{1}}}, Effect: oc.CanonicalConverge, Ordering: oc.VersionGuarded, Handler: h}}, outbox.Options{PollInterval: time.Millisecond, RetryBase: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Initialize(databaseTestContext(t)); err != nil {
		t.Fatal(err)
	}
	return svc, r, typ
}
func guardConfiguration(t *testing.T, env []string) config.Config {
	t.Helper()
	values := map[string]string{}
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		values[k] = v
	}
	c, e := config.Load(func(k string) (string, bool) { v, ok := values[k]; return v, ok }, env)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func guardEnvironment(t *testing.T, db *pgfixture.Database) []string {
	t.Helper()
	objects, err := objectfixture.Environment(databaseTestContext(t), db.Name)
	if err != nil {
		t.Fatal(err)
	}
	return append(objects, `AGENTEAM_CENTRAL_SECRET_KEYRING={"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`, `AGENTEAM_CENTRAL_CURSOR_KEYRING={"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`, "AGENTEAM_CENTRAL_DATABASE_URL="+db.Fixture.URL(db.Name), "AGENTEAM_CENTRAL_DATABASE_CA_FILE="+db.Fixture.CAFile, "AGENTEAM_CENTRAL_HTTP_ADDR=127.0.0.1:0")
}
func guardID[K any](t *testing.T) foundation.ID[K] {
	t.Helper()
	id, e := foundation.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func TestOutboxGuardOwnedChild(t *testing.T) {
	if os.Getenv("AGENTEAM_OUTBOX_GUARD_CHILD") != "1" {
		return
	}
	descriptor, err := pgfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = descriptor.Config(os.Getenv("AGENTEAM_OUTBOX_GUARD_DATABASE"), nil); err != nil {
		t.Fatal(err)
	}
	cfg := guardConfiguration(t, os.Environ())
	store, err := postgres.Open(databaseTestContext(t), cfg.Database())
	if err != nil {
		t.Fatal(err)
	}
	m, err := postgres.NewMigrator(cfg.Database())
	if err != nil {
		t.Fatal(err)
	}
	if r := m.Migrate(databaseTestContext(t)); !r.Migrated {
		t.Fatal(r.Fault)
	}
	if _, err = store.Exec(databaseTestContext(t), `CREATE TABLE outbox_guard_effect(id uuid PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	auditor, err := initializeSecurity(databaseTestContext(t), cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := initializeObjects(databaseTestContext(t), cfg, store, auditor)
	if err != nil {
		t.Fatal(err)
	}
	objects := resource.(*objectAssembly)
	release := make(chan struct{})
	entered := make(chan struct{})
	svc, r, typ := guardOutbox(t, store, objects, entered, release)
	if err = r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	at, _ := foundation.NewInstant(time.Now())
	version := foundation.Version(1)
	e, err := event.NewEvent(typ, event.Header{EventID: guardID[event.EventIdentity](t), EventType: "guard.changed", SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.SystemScope}, AggregateType: "guard", AggregateID: guardID[event.Aggregate](t), AggregateVersion: &version}, guardOutboxPayload{1})
	if err != nil {
		t.Fatal(err)
	}
	actor, _ := identity.NewHuman(guardID[identity.User](t), guardID[identity.Session](t))
	plan, err := svc.PrepareAppend(databaseTestContext(t), actor, e)
	if err != nil {
		t.Fatal(err)
	}
	cause, _ := foundation.NewRecoveryCause("guard.fixture", e.Header().EventID.String(), "")
	result := store.WithinTx(databaseTestContext(t), cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := store.AcquireAll(ctx, tx, plan.Locks()); e != nil {
			return e
		}
		_, err := svc.AppendEventInTx(ctx, tx, actor, e, plan)
		return err
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("real callback did not enter")
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "outbox_callback_entered", "process_id": objects.process.String()})
	input := bufio.NewReader(os.Stdin)
	line, err := input.ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "force" {
		t.Fatal("missing force barrier")
	}
	owned := &resources{db: observedDatabase{store}, objectService: objects, outboxService: r}
	forced, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	start := time.Now()
	cleanupResources(forced, owned, nil)
	cancel()
	if r.Joined() || time.Since(start) > 600*time.Millisecond {
		t.Fatal("unjoined callback or force budget misreported")
	}
	fmt.Fprintln(os.Stdout, `{"event":"outbox_force_returned"}`)
	// Parent observes the real flock while this process and callback remain alive,
	// then kills this owned child. No deadline is treated as a death certificate.
	_, _ = input.ReadString('\n')
	close(release)
	t.Fatal("unexpected graceful path in kill fixture")
}
func TestOutboxGuardActualCallbackForceAndExactKillRecovery(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	environment := guardEnvironment(t, db)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := &fixtureProcess{cmd: exec.Command(exe, "-test.run=^TestOutboxGuardOwnedChild$", "-test.timeout=30s"), stdout: newEventLog(), stderr: newEventLog(), done: make(chan struct{})}
	p.cmd.Env = append(environment, "PATH="+os.Getenv("PATH"), pgfixture.Env+"="+os.Getenv(pgfixture.Env), objectfixture.Env+"="+os.Getenv(objectfixture.Env), "AGENTEAM_OUTBOX_GUARD_CHILD=1", "AGENTEAM_OUTBOX_GUARD_DATABASE="+db.Name)
	p.cmd.Stdout, p.cmd.Stderr = p.stdout, p.stderr
	p.stdin, err = p.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		_ = p.stdin.Close()
		_ = p.cmd.Process.Kill()
		select {
		case <-p.done:
		case <-time.After(time.Second):
			t.Error("owned outbox child did not join")
		}
	})
	line := p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "outbox_callback_entered" })
	old, err := foundation.ParseID[objectcontract.Process](line["process_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.stdin.Write([]byte("force\n")); err != nil {
		t.Fatal(err)
	}
	p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "outbox_force_returned" })
	if !strings.Contains(p.stdout.String(), `"event":"database_force_started"`) {
		t.Fatal("expired force skipped actual DB close")
	}
	cfg := guardConfiguration(t, environment)
	store, err := postgres.Open(databaseTestContext(t), cfg.Database())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = store.ForceClose(ctx)
	})
	var state string
	var effects int
	if err = store.QueryRow(databaseTestContext(t), `SELECT (SELECT state FROM agenteam_object.process_claims WHERE process_id=$1),(SELECT count(*) FROM outbox_guard_effect)`, old.String()).Scan(&state, &effects); err != nil || state != "claimed" || effects != 0 {
		t.Fatal("live callback force released guard or committed effect", state, effects, err)
	}
	auditor, err := initializeSecurity(databaseTestContext(t), cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := initializeObjects(databaseTestContext(t), cfg, store, auditor)
	if err != nil {
		t.Fatal(err)
	}
	observer := resource.(*objectAssembly)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = observer.Force(ctx)
	})
	var fault *foundation.Fault
	if err = observer.guard.ConfirmStopped(databaseTestContext(t), old); !errors.As(err, &fault) || fault.Code != foundation.ResourceBusy {
		t.Fatal("live exact flock accepted after API force", err)
	}
	_, runtime, _ := guardOutbox(t, store, observer, nil, nil)
	// Initialization scans the real old processing row, but a living exact owner
	// stays protected even though its database transaction already rolled back.
	if err = runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = runtime.Force(ctx)
	})
	if err = runtime.Recover(databaseTestContext(t)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = store.QueryRow(databaseTestContext(t), `SELECT count(*) FROM agenteam_outbox.attempts`).Scan(&n); err != nil || n != 1 {
		t.Fatal("stole live old owner")
	}
	if err = p.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		t.Fatal("SIGKILL did not join child")
	}
	var exit *exec.ExitError
	if !errors.As(p.err, &exit) {
		t.Fatal("no actual child exit")
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("not actual SIGKILL")
	}
	if err = observer.guard.ConfirmStopped(databaseTestContext(t), old); err != nil {
		t.Fatal("exact OS death not recognized", err)
	}
	waitAppDatabase(t, db, `SELECT count(*)=1 FROM agenteam_outbox.processed`)
	runtime.StopClaims()
	if err = runtime.Drain(databaseTestContext(t)); err != nil {
		t.Fatal(err)
	}
	if err = store.QueryRow(databaseTestContext(t), `SELECT count(*) FROM outbox_guard_effect`).Scan(&n); err != nil || n != 1 {
		t.Fatal("recovery did not commit exactly one effect")
	}
	if err = observer.Drain(databaseTestContext(t)); err != nil {
		t.Fatal(err)
	}
	if err = store.QueryRow(databaseTestContext(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, observer.process.String()).Scan(&state); err != nil || state != "stopped" {
		t.Fatal("actual join failed to finalize shared guard")
	}
}
