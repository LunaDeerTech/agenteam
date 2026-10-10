//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type secretVariableRootHeldCall struct {
	kind string
	workRootHeldCall
}

type secretVariableRootHeldStore struct {
	*postgres.Store
	hold    bool
	entered chan secretVariableRootHeldCall
	release chan struct{}
	once    sync.Once
}

func (s *secretVariableRootHeldStore) unhold() { s.once.Do(func() { close(s.release) }) }

// This observer preserves the original BEGIN, callback and CommitResult.
// Its selected calls cannot leave the actual transaction until release, even
// after cancellation; it neither authorizes them nor manufactures a result.
func (s *secretVariableRootHeldStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	kind := ""
	if s.hold {
		if cause.Kind() == f.RecoveryCause && cause.Details().Owner == "projectvariable.secret_get" {
			kind = "read"
		}
		if cause.Kind() == f.CommandsCause && cause.Details().Primary.Command() == string(vc.SecretDeleteCommand) {
			switch cause.Details().Primary.Key() {
			case "secret-root-held-lookup":
				kind = "lookup"
			case "secret-root-held-delete":
				kind = "delete"
			}
		}
	}
	if kind == "" {
		return s.Store.WithinTx(ctx, cause, fn)
	}
	returned := make(chan struct{})
	defer close(returned)
	return s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		x, err := s.Store.InTx(tx)
		if err != nil {
			return err
		}
		var pid int32
		if err = x.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
			return err
		}
		s.entered <- secretVariableRootHeldCall{kind, workRootHeldCall{pid, ctx.Done(), returned}}
		<-s.release
		return fn(ctx, tx)
	})
}

type secretVariableRootFixture struct {
	t            *testing.T
	db           *pgfixture.Database
	cfg          config.Config
	core         *account.Service
	owned        *resources
	assembly     *accountAssembly
	variables    *projectSecretVariablesAssembly
	store        *secretVariableRootHeldStore
	objects      *objectAssembly
	output       *eventLog
	done         chan struct{}
	signals      chan os.Signal
	accountDrain chan struct{}
	result       error
	address      string
}

func secretVariableRootCheck(t *testing.T, stage string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var fault *f.Fault
	if errors.As(err, &fault) {
		t.Fatalf("Secret root %s: %s/%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
	}
	t.Fatalf("Secret root %s failed", stage)
}

func newSecretVariableRootFixture(t *testing.T, hold bool) *secretVariableRootFixture {
	t.Helper()
	v := &secretVariableRootFixture{t: t, db: pgfixture.NewDatabase(t), output: newEventLog(), done: make(chan struct{}), signals: make(chan os.Signal, 1), accountDrain: make(chan struct{}),
		store: &secretVariableRootHeldStore{hold: hold, entered: make(chan secretVariableRootHeldCall, 3), release: make(chan struct{})}}
	v.cfg = guardConfiguration(t, append(guardEnvironment(t, v.db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN=https://root.example.test", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s"))
	deps := dependencies{
		open: func(ctx context.Context, cfg postgres.Config) (database, error) {
			raw, err := postgres.Open(ctx, cfg)
			if err != nil {
				return nil, err
			}
			v.store.Store = raw
			return v.store, nil
		},
		observeAccount: func(core *account.Service) { v.core = core },
		bind: func(ctx context.Context, cfg config.Config, db database, owned *resources, deps *dependencies) error {
			v.owned = owned
			if err := bindAccounts(ctx, cfg, db, owned, deps); err != nil {
				return err
			}
			assembly := owned.accounts().(*accountAssembly)
			assembly.mu.Lock()
			assembly.runtime = &workRootAccountDrain{accountRuntime: assembly.runtime, entered: v.accountDrain}
			assembly.mu.Unlock()
			return nil
		},
	}
	logger, err := logging.New(logging.Central, slog.LevelInfo, v.output)
	secretVariableRootCheck(t, "logger", err)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { v.result = run(ctx, v.cfg, logger, v.signals, deps); close(v.done) }()
	t.Cleanup(func() {
		if v.store != nil {
			v.store.unhold()
		}
		cancel()
		select {
		case <-v.done:
		case <-time.After(7 * time.Second):
			t.Error("original Secret root did not return during cleanup")
		}
	})
	startup, cancelStartup := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancelStartup()
	for v.address == "" {
		select {
		case event := <-v.output.events:
			if event["event"] == "listening" {
				v.address, _ = event["listen_address"].(string)
			}
		case <-v.done:
			secretVariableRootCheck(t, "startup", v.result)
			t.Fatal("root returned before listening")
		case <-startup.Done():
			t.Fatal("root did not reach listening")
		}
	}
	// Listening synchronizes the original construction; only these installed
	// owners may supply the production HTTP and shutdown behavior.
	v.assembly = v.owned.accounts().(*accountAssembly)
	v.assembly.mu.Lock()
	v.variables, _ = v.assembly.secretVariables.(*projectSecretVariablesAssembly)
	v.assembly.mu.Unlock()
	v.objects, _ = v.owned.objects().(*objectAssembly)
	if v.core == nil || v.variables == nil || v.objects == nil {
		t.Fatal("root omitted a concrete owner")
	}
	return v
}

func (v *secretVariableRootFixture) login(ctx context.Context) (id.Actor, string, string) {
	t := v.t
	t.Helper()
	login, err := fixtureAccountLoginResponse(ctx, v.cfg, v.core)
	secretVariableRootCheck(t, "formal login", err)
	var material sc.SecretMaterial
	useErr := login.UseCookie(func(raw []byte) error { var err error; material, err = sc.NewSecretMaterial(raw); return err })
	closeErr := login.Close(ctx)
	defer material.Destroy()
	secretVariableRootCheck(t, "login material", useErr)
	secretVariableRootCheck(t, "login Close", closeErr)
	actor, err := v.core.Authenticate(ctx, material)
	secretVariableRootCheck(t, "current Session", err)
	session, err := v.core.GetSession(ctx, material)
	secretVariableRootCheck(t, "CSRF", err)
	defer session.CSRF.Destroy()
	var cookie, csrf string
	secretVariableRootCheck(t, "cookie", material.Use(func(raw []byte) error { cookie = string(raw); return nil }))
	secretVariableRootCheck(t, "CSRF material", session.CSRF.Use(func(raw []byte) error { csrf = string(raw); return nil }))
	return actor, cookie, csrf
}

func (v *secretVariableRootFixture) claim() *os.File {
	v.t.Helper()
	claim, err := os.OpenFile(filepath.Join(v.cfg.Objects().SpoolDirectory()+".processes", v.objects.process.String()+".claim"), os.O_RDWR, 0)
	secretVariableRootCheck(v.t, "process claim", err)
	v.t.Cleanup(func() { _ = claim.Close() })
	return claim
}

func (v *secretVariableRootFixture) joined(claim *os.File) {
	t := v.t
	t.Helper()
	select {
	case <-v.done:
	case <-time.After(7 * time.Second):
		t.Fatal("original root did not join")
	}
	secretVariableRootCheck(t, "original root result", v.result)
	if !v.variables.Joined() || !v.assembly.Joined() || !v.owned.producersJoined() {
		t.Fatal("root returned without original Secret and Account retirement")
	}
	secretVariableRootCheck(t, "retired claim", syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	secretVariableRootCheck(t, "retired claim unlock", syscall.Flock(int(claim.Fd()), syscall.LOCK_UN))
	conn := v.db.Connect(t)
	var state string
	secretVariableRootCheck(t, "process fact", conn.QueryRow(databaseTestContext(t), "SELECT state FROM agenteam_object.process_claims WHERE process_id=$1", v.objects.process.String()).Scan(&state))
	var absent bool
	secretVariableRootCheck(t, "database borrowers", conn.QueryRow(databaseTestContext(t), "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')").Scan(&absent))
	if state != "stopped" || !absent {
		t.Fatal("root retained original guard or database borrowers")
	}
}

func TestProjectSecretVariablesDefaultRoot(t *testing.T) {
	t.Run("existing-project-protocol-and-routing", func(t *testing.T) {
		v := newSecretVariableRootFixture(t, false)
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		actor, cookie, csrf := v.login(ctx)
		v.assembly.mu.Lock()
		projects := v.assembly.projects.(*projectCommandWork)
		skills := v.assembly.skills.(*skillWork)
		v.assembly.mu.Unlock()
		meta := func() f.CommandMeta {
			return f.CommandMeta{RequestID: guardID[f.Request](t), IdempotencyKey: f.IdempotencyKey(guardID[struct{}](t).String())}
		}
		blockedID := guardID[id.Project](t)
		blocked, err := projects.service.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: blockedID, Name: "secret-root-existing"})
		var fault *f.Fault
		if !errors.As(err, &fault) || fault.Code != f.DependencyUnbound || fault.CommitState != f.NotCommitted || blocked.State != "" || blocked.Project != nil || blocked.Operation != nil {
			t.Fatal("default Project initializer was enabled")
		}
		conn := v.db.Connect(t)
		var facts int64
		secretVariableRootCheck(t, "unbound zero facts", conn.QueryRow(ctx, `SELECT
		 (SELECT count(*) FROM agenteam_project.projects WHERE id=$1 OR owner_user_id=$2 AND normalized_name='secret-root-existing')+
		 (SELECT count(*) FROM agenteam_project.creations WHERE project_id=$1)+
		 (SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1)+
		 (SELECT count(*) FROM agenteam_skill.initializations WHERE project_id=$1)+
		 (SELECT count(*) FROM agenteam_skill.skills WHERE project_id=$1)+
		 (SELECT count(*) FROM agenteam_skill.revisions WHERE project_id=$1)+
		 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1)+
		 (SELECT count(*) FROM agenteam_skill.object_attempts WHERE project_id=$1)+
		 (SELECT count(*) FROM agenteam_object.uploads WHERE project_id=$1)+
		 (SELECT count(*) FROM agenteam_object.objects WHERE project_id=$1)+
		 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1)+
		 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1)`, blockedID.String(), actor.Details().UserID).Scan(&facts))
		if facts != 0 {
			t.Fatal("unbound Create wrote domain facts")
		}
		// Reuse only the accepted test-only fixture: real Project/Skills/Object
		// ports and original Store/Auditor. Never install its initializer in root.
		fixture := rootCompositionProjectFixture(t, v.cfg, v.owned, v.objects, skills.service)
		work := &projectCommandWork{service: fixture}
		t.Cleanup(func() {
			if !work.Joined() {
				if err := work.Drain(ctx); err != nil {
					t.Error("test-only Project fixture did not drain")
				}
			}
		})
		projectID := guardID[id.Project](t)
		created, createErr := fixture.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: projectID, Name: "secret-root-existing"})
		drainErr := work.Drain(ctx)
		secretVariableRootCheck(t, "test-only Project Create", createErr)
		secretVariableRootCheck(t, "test-only Project actual Drain", drainErr)
		if !work.Joined() || created.Validate() != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != projectID {
			t.Fatal("test-only fixture did not establish an existing initialized Project")
		}
		var initialized bool
		secretVariableRootCheck(t, "initialization binding", conn.QueryRow(ctx, `SELECT p.initialized_at IS NOT NULL AND c.state='completed' AND s.phase='published'
		 AND c.protected_skill_id=s.skill_id AND c.protected_revision=s.revision AND c.id=s.creation_id
		 FROM agenteam_project.projects p JOIN agenteam_project.creations c ON c.id=p.creation_id
		 JOIN agenteam_skill.initializations s ON s.project_id=p.id WHERE p.id=$1`, projectID.String()).Scan(&initialized))
		if !initialized {
			t.Fatal("real Project confirmation facts missing")
		}
		client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 10 * time.Second}
		defer client.CloseIdleConnections()
		const canary = "ROOT_SECRET_MATERIAL_CANARY_723f"
		digest := sha256.Sum256([]byte(canary))
		forbidden := []string{canary, base64.StdEncoding.EncodeToString([]byte(canary)), hex.EncodeToString(digest[:])}
		call := func(method, path, body, key string, status int) ([]byte, http.Header) {
			t.Helper()
			r, err := http.NewRequestWithContext(ctx, method, "http://"+v.address+path, strings.NewReader(body))
			secretVariableRootCheck(t, "HTTP request", err)
			r.Host = "root.example.test"
			r.Header.Set("Origin", "https://root.example.test")
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			r.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: cookie})
			if method != http.MethodGet && method != http.MethodHead {
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-CSRF-Token", csrf)
				r.Header.Set("Idempotency-Key", key)
			}
			response, err := client.Do(r)
			secretVariableRootCheck(t, "HTTP transport", err)
			raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
			closeErr := response.Body.Close()
			secretVariableRootCheck(t, "HTTP body", readErr)
			secretVariableRootCheck(t, "HTTP original Close", closeErr)
			if response.StatusCode != status || len(raw) > 1<<20 {
				var problem struct {
					Code f.Code `json:"code"`
				}
				_ = json.Unmarshal(raw, &problem)
				t.Fatalf("root HTTP status=%d want=%d code=%s", response.StatusCode, status, problem.Code.Safe())
			}
			if len(response.Header.Values("X-Request-ID")) != 1 {
				t.Fatal("root request identity not singular")
			}
			headerBytes, err := json.Marshal(response.Header)
			secretVariableRootCheck(t, "HTTP headers", err)
			for _, text := range forbidden {
				if bytes.Contains(raw, []byte(text)) || bytes.Contains(headerBytes, []byte(text)) || strings.Contains(v.output.String(), text) {
					t.Fatal("Secret material leaked to response or actual root log")
				}
			}
			return raw, response.Header.Clone()
		}
		base := "/api/v1/projects/" + projectID.String()
		secretID, ordinaryID := guardID[id.ProjectVariable](t), guardID[id.ProjectVariable](t)
		collection := base + "/secret-variables"
		detail := collection + "/" + secretID.String()
		body := `{"request":{"variable_id":"` + secretID.String() + `","name":"ROOT_SECRET","description":"safe","value":"` + canary + `"}}`
		raw, _ := call("POST", collection, body, "root-create", 200)
		var receipt vc.SecretVariableMutation
		secretVariableRootCheck(t, "Secret create receipt", json.Unmarshal(raw, &receipt))
		if receipt.Validate() != nil || receipt.Fields().Variable.Fields().ID != secretID || receipt.Fields().Variable.Fields().Version != 1 {
			t.Fatal("root returned invalid Secret create receipt")
		}
		get, _ := call("GET", detail, "", "", 200)
		head, headers := call("HEAD", detail, "", "", 200)
		if len(head) != 0 || headers.Get("Content-Length") != strconv.Itoa(len(get)) {
			t.Fatal("root Secret HEAD representation mismatch")
		}
		raw, _ = call("GET", collection, "", "", 200)
		var page f.Page[vc.SecretVariable]
		secretVariableRootCheck(t, "Secret page", json.Unmarshal(raw, &page))
		if len(page.Items) != 1 || page.Items[0].Fields().ID != secretID {
			t.Fatal("root list omitted Secret")
		}
		ordinary := base + "/variables"
		ordinaryBody := `{"request":{"variable_id":"` + ordinaryID.String() + `","name":"ROOT_PUBLIC","description":"safe","value":"ordinary-readable"}}`
		call("POST", ordinary, ordinaryBody, "ordinary-create", 200)
		raw, _ = call("GET", ordinary+"/"+ordinaryID.String(), "", "", 200)
		var variable vc.Variable
		secretVariableRootCheck(t, "ordinary detail", json.Unmarshal(raw, &variable))
		if variable.Validate() != nil || variable.Fields().Value != "ordinary-readable" {
			t.Fatal("Secret binding changed ordinary value reads")
		}
		call("GET", ordinary+"/"+secretID.String(), "", "", 404)
		call("GET", collection+"/"+ordinaryID.String(), "", "", 404)
		raw, _ = call("PATCH", detail, `{"expected_version":"1","request":{"description":"changed"}}`, "root-update", 200)
		secretVariableRootCheck(t, "Secret update", json.Unmarshal(raw, &receipt))
		if receipt.Validate() != nil || !receipt.Fields().Changed || receipt.Fields().Variable.Fields().Version != 2 {
			t.Fatal("root update did not bind D04 and Owner")
		}
		raw, _ = call("DELETE", detail, `{"expected_version":"2"}`, "root-delete", 200)
		secretVariableRootCheck(t, "Secret delete", json.Unmarshal(raw, &receipt))
		if receipt.Validate() != nil || receipt.Fields().Deleted == nil || receipt.Fields().Deleted.Version != 3 {
			t.Fatal("root delete lost its receipt")
		}
		raw, _ = call("POST", collection+"/commands/lookup", `{"command":"project.secret_variable.delete","target_id":"`+secretID.String()+`","expected_version":"2"}`, "root-delete", 200)
		var lookup vc.SecretVariableCommandLookup
		secretVariableRootCheck(t, "identity-only Lookup", json.Unmarshal(raw, &lookup))
		if lookup.Validate() != nil || lookup.Status() != vc.SecretLookupCommitted || lookup.Receipt() == nil || lookup.Receipt().Fields().Deleted == nil || lookup.Receipt().Fields().Deleted.ID != secretID {
			t.Fatal("root identity-only history did not restore deleted receipt")
		}
		var history, audits, events, commands, receipts int
		secretVariableRootCheck(t, "same-transaction producer facts", conn.QueryRow(ctx, `SELECT
		 (SELECT count(*) FROM agenteam_projectvariable.secret_history WHERE project_id=$1),
		 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action IN ('project.secret_variable.create','project.secret_variable.update','project.secret_variable.delete')),
		 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.secret_variable_changed'),
		 (SELECT count(*) FROM agenteam_projectvariable.secret_commands WHERE project_id=$1),
		 (SELECT count(*) FROM agenteam_secret.project_variable_receipts WHERE project_id=$1)`, projectID.String()).Scan(&history, &audits, &events, &commands, &receipts))
		if history != 3 || audits != 3 || events != 3 || commands != 3 || receipts != 3 {
			t.Fatal("root omitted or duplicated Secret facts through its shared providers")
		}
		client.CloseIdleConnections()
		claim := v.claim()
		v.signals <- syscall.SIGTERM
		v.joined(claim)
		for _, text := range forbidden {
			if strings.Contains(v.output.String(), text) {
				t.Fatal("Secret material leaked in the final root log")
			}
		}
	})
	t.Run("stop-drain-original-calls", func(t *testing.T) {
		v := newSecretVariableRootFixture(t, true)
		actor, _, _ := v.login(databaseTestContext(t))
		projectID, target := guardID[id.Project](t), guardID[id.ProjectVariable](t)
		version := f.Version(1)
		query, err := vc.NewSecretVariableCommandLookupRequest(vc.SecretVariableCommandLookupFields{ProjectID: projectID, Command: vc.SecretDeleteCommand, TargetID: target, ExpectedVersion: &version, IdempotencyKey: "secret-root-held-lookup"})
		secretVariableRootCheck(t, "Lookup input", err)
		meta := f.CommandMeta{RequestID: guardID[f.Request](t), IdempotencyKey: "secret-root-held-delete", ExpectedVersion: &version}
		done := make(chan error, 3)
		go func() {
			_, err := v.variables.service.GetSecretVariable(context.Background(), actor, projectID, target)
			done <- err
		}()
		go func() {
			_, err := v.variables.service.LookupSecretVariableCommand(context.Background(), actor, query)
			done <- err
		}()
		go func() {
			_, err := v.variables.service.DeleteSecretVariable(context.Background(), actor, meta, projectID, target)
			done <- err
		}()
		var calls []secretVariableRootHeldCall
		seen := map[string]bool{}
		pids := map[int32]bool{}
		for range 3 {
			select {
			case call := <-v.store.entered:
				if seen[call.kind] || pids[call.pid] || call.pid <= 0 {
					t.Fatal("barrier did not identify three distinct actual calls")
				}
				seen[call.kind], pids[call.pid] = true, true
				calls = append(calls, call)
			case <-done:
				t.Fatal("Secret call returned before its real transaction barrier")
			case <-time.After(5 * time.Second):
				t.Fatal("Secret transaction barrier not reached")
			}
		}
		if !seen["read"] || !seen["lookup"] || !seen["delete"] {
			t.Fatal("wrong Secret call families observed")
		}
		conn := v.db.Connect(t)
		for _, call := range calls {
			var active bool
			secretVariableRootCheck(t, "actual held transaction", conn.QueryRow(databaseTestContext(t), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid=$1 AND xact_start IS NOT NULL)", call.pid).Scan(&active))
			if !active {
				t.Fatal("barrier had no actual database transaction")
			}
		}
		claim := v.claim()
		v.signals <- syscall.SIGTERM
		for _, call := range calls {
			select {
			case <-call.cancelled:
			case <-time.After(2 * time.Second):
				t.Fatal("root did not cancel all Secret calls")
			}
			select {
			case <-call.returned:
				t.Fatal("original held transaction returned before release")
			default:
			}
		}
		if v.variables.Joined() || v.owned.producersJoined() {
			t.Fatal("cancellation pretended original call join")
		}
		select {
		case <-v.accountDrain:
			t.Fatal("Account retired before Secret calls joined")
		default:
		}
		if err := syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
			if err == nil {
				_ = syscall.Flock(int(claim.Fd()), syscall.LOCK_UN)
			}
			t.Fatal("root released its original process guard before Secret join")
		}
		v.store.unhold()
		for range 3 {
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancelled held call became successful")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("released original call did not return")
			}
		}
		v.joined(claim)
		select {
		case <-v.accountDrain:
		default:
			t.Fatal("Account was never actually drained")
		}
		// These transactions intentionally precede authorization of an absent
		// Project. The first subtest proves actual successful root HTTP commands.
		if _, err := v.variables.service.GetSecretVariable(context.Background(), actor, projectID, target); err == nil {
			t.Fatal("retired root admitted a new Secret call")
		}
	})
}
