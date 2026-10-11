//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	agenthttp "github.com/LunaDeerTech/agenteam/internal/central/agent/http"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	auditc "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	runnerservice "github.com/LunaDeerTech/agenteam/internal/central/runner/service"
)

// This is the existing real P2/Account/Object/ProcessGuard configuration
// assembly, with formal Runner management and Agent services on the same Store.
// It does not claim default App initialization, a physical workspace, an online
// Runner, nonempty Execution capture or a dispatched execution.
func TestAgentRunnerMountsIntegration(t *testing.T) {
	t.Run("owner-http-configuration-and-current-reads", func(t *testing.T) {
		v := newRunnerMountPGFixture(t)
		base := v.agent.p2.base
		owner := base.ownerBrowser
		candidatePath := variableHTTPPath(v.agent.p2.project.ID, "/available-runners")
		candidate := v.request(t, owner, http.MethodGet, candidatePath+"?limit=50", "", nil)
		var page f.Page[rc.ConfigurationCandidate]
		if candidate.status != http.StatusOK || json.Unmarshal(candidate.body, &page) != nil || page.Items == nil || len(page.Items) != 1 || page.NextCursor != "" {
			t.Fatal("ordinary Owner did not read the real Runner candidate")
		}
		wantCandidate := rc.ConfigurationCandidate{ID: v.runner.ID, Name: v.runner.Name, Description: v.runner.Description, Version: 1, Status: rc.Offline}
		if !bytes.Equal(jsonBytes(t, page.Items[0]), jsonBytes(t, wantCandidate)) || page.Items[0].Validate() != nil {
			t.Fatal("offline Runner candidate differs from the formal creation receipt")
		}
		runnerMountFields(t, candidate.body, "items")
		runnerMountFields(t, runnerMountRawItems(t, candidate.body)[0], "id", "name", "description", "version", "status")
		v.head(t, owner, candidatePath+"?limit=50", candidate)

		declarations := []ac.RunnerMountDeclaration{{RunnerID: v.runner.ID, Workspace: "w" + strings.Repeat("a", 254)}}
		request, createMeta, createCommand := v.createRequest(t, declarations)
		target := request.Fields().AgentID
		createBody := jsonBytes(t, map[string]any{"request": request})
		created := v.mutation(t, v.request(t, owner, http.MethodPost, v.collection(), createMeta.IdempotencyKey, createBody), 1, true, declarations)
		original := slices.Clone(*created.Fields().RunnerMounts)
		v.command(t, target, ac.CreateAgentCommand, createMeta, request, created)
		v.current(t, created, original, 1)
		v.agent.checkFacts(t, target, createMeta.IdempotencyKey, createCommand, [16]int64{1, 1, 1, 0, 2, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1, 0}, false)

		current, _ := v.update(t, target, "runner-mount-noop", 1, declarations, 1, false)
		if !bytes.Equal(jsonBytes(t, current.Fields().Agent), jsonBytes(t, created.Fields().Agent)) || !slices.Equal(*current.Fields().RunnerMounts, original) {
			t.Fatal("same declaration no-op changed canonical configuration or allocated identities")
		}
		v.current(t, current, original, 2)
		changedDeclarations := []ac.RunnerMountDeclaration{{RunnerID: v.runner.ID, Workspace: "other.workspace"}}
		current, _ = v.update(t, target, "runner-mount-change", 1, changedDeclarations, 2, true)
		other := (*current.Fields().RunnerMounts)[0]
		if other.MountID == original[0].MountID || other.WorkspaceID == original[0].WorkspaceID {
			t.Fatal("different workspace reused the original physical identity")
		}
		all := append(slices.Clone(original), other)
		v.current(t, current, all, 3)
		current, _ = v.update(t, target, "runner-mount-clear", 2, []ac.RunnerMountDeclaration{}, 3, true)
		v.current(t, current, all, 4)
		current, _ = v.update(t, target, "runner-mount-readd", 3, declarations, 4, true)
		if !slices.Equal(*current.Fields().RunnerMounts, original) {
			t.Fatal("re-adding the original tuple changed its frozen Mount/Workspace identities")
		}
		v.current(t, current, all, 5)

		before := v.snapshot(t, target)
		stale, err := ac.NewAgentUpdate(ac.AgentUpdateFields{RunnerMounts: &declarations})
		if err != nil {
			t.Fatal(err)
		}
		staleBody := jsonBytes(t, map[string]any{"expected_version": f.Version(3), "request": stale})
		runnerMountProblem(t, v.request(t, owner, http.MethodPatch, v.collection()+"/"+target.String()+"/configuration", "runner-mount-stale", staleBody), http.StatusConflict, f.VersionConflict)
		if v.snapshot(t, target) != before {
			t.Fatal("stale version created a new declaration plan or changed current facts")
		}
		lookupBody := jsonBytes(t, map[string]any{"command": ac.CreateAgentCommand, "request": request})
		lookupReply := v.request(t, owner, http.MethodPost, variableHTTPPath(v.agent.p2.project.ID, "/agent-commands/lookup"), createMeta.IdempotencyKey, lookupBody)
		lookup, err := ac.DecodeAgentCommandLookup(lookupReply.body)
		if lookupReply.status != http.StatusOK || err != nil || lookup.Status() != ac.AgentLookupCommitted || lookup.Receipt() == nil || !bytes.Equal(jsonBytes(t, *lookup.Receipt()), jsonBytes(t, created)) {
			t.Fatal("original full-intent/key Lookup did not return its historical v1 receipt")
		}
		replayed := v.mutation(t, v.request(t, owner, http.MethodPost, v.collection(), createMeta.IdempotencyKey, createBody), 1, true, declarations)
		if !bytes.Equal(jsonBytes(t, replayed), jsonBytes(t, created)) || v.snapshot(t, target) != before {
			t.Fatal("historical same-key replay rewrote current configuration or repeated side effects")
		}
		v.current(t, current, all, 5) // Historical acknowledgement is not current configuration.
		v.strictReadRollback(t, current)
		v.current(t, current, all, 5)

		missing, missingMeta, _ := v.createRequest(t, []ac.RunnerMountDeclaration{{RunnerID: id[rc.Runner](t), Workspace: "missing"}})
		bad, err := v.agent.agents.CreateAgent(ctxFor(t), owner.actor, missingMeta, v.agent.p2.project.ID, missing)
		requireCode(t, err, f.NotFound)
		if bad.Validate() == nil {
			t.Fatal("missing Runner returned a successful receipt")
		}
		var rejected int64
		if err := base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_agent.commands WHERE target_id::text=$1)+
 (SELECT count(*) FROM agenteam_agent.agents WHERE id::text=$1)+
 (SELECT count(*) FROM agenteam_mount.mounts WHERE agent_id::text=$1)+
 (SELECT count(*) FROM agenteam_mount.runner_workspaces WHERE agent_id::text=$1)`, missing.Fields().AgentID.String()).Scan(&rejected); err != nil || rejected != 0 {
			t.Fatal("missing candidate left an Agent plan or Mount facts", err)
		}
		for _, path := range []string{candidatePath, v.mappingPath(target)} {
			runnerMountProblem(t, v.request(t, base.otherBrowser, http.MethodGet, path, "", nil), http.StatusNotFound, f.NotFound)
		}
		otherProject, err := v.agent.p2.creator.CreateProject(ctxFor(t), owner.actor, meta(t, "runner-mount-other-project", nil), pc.CreateProjectRequest{ProjectID: id[i.Project](t), Name: "runner-mount-other", Description: "Actual second P2 scope"})
		if err != nil || otherProject.State != pc.CreationReady || otherProject.Project == nil {
			t.Fatal("formal second initialized Project", err)
		}
		runnerMountProblem(t, v.request(t, owner, http.MethodGet, variableHTTPPath(otherProject.Project.ID, "/agents/"+target.String()+"/runner-mounts"), "", nil), http.StatusNotFound, f.NotFound)
		if v.snapshot(t, target) != before {
			t.Fatal("denied candidate/current reads changed original Agent facts")
		}
		if err := base.core.Logout(ctxFor(t), account.LogoutRequest{Actor: owner.actor, Key: "runner-mount-owner-logout"}); err != nil {
			t.Fatal("actual Owner Session logout", err)
		}
		for _, path := range []string{candidatePath, v.mappingPath(target)} {
			runnerMountProblem(t, v.request(t, owner, http.MethodGet, path, "", nil), http.StatusUnauthorized, f.SessionRevoked)
		}
		runnerMountProblem(t, v.request(t, owner, http.MethodPost, variableHTTPPath(v.agent.p2.project.ID, "/agent-commands/lookup"), createMeta.IdempotencyKey, lookupBody), http.StatusUnauthorized, f.SessionRevoked)
		if v.snapshot(t, target) != before {
			t.Fatal("revoked original Session changed Agent configuration or receipts")
		}
	})

	t.Run("final-rollback-preserves-frozen-identities", func(t *testing.T) {
		v := newRunnerMountPGFixture(t)
		declarations := []ac.RunnerMountDeclaration{{RunnerID: v.runner.ID, Workspace: "rollback.workspace"}}
		request, originalMeta, identity := v.createRequest(t, declarations)
		target, base := request.Fields().AgentID, v.agent.p2.base
		marker := errors.New("runner-mount-final-real-rollback")
		var observed, returned bool
		var physical f.CommitResult
		var frozen []ac.RunnerMountBinding
		var frozenRevision int64
		store := base.tracked
		store.setAfter(func(ctx context.Context, tx f.Tx, txCause f.TransactionCause) error {
			d := txCause.Details()
			if d.Kind != f.CommandsCause || d.Primary.Canonical() != identity.Canonical() {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			var state string
			var raw, mapping []byte
			if err = x.QueryRow(ctx, `SELECT state,receipt,runner_mounts,plan_revision FROM agenteam_agent.commands WHERE project_id::text=$1 AND target_id::text=$2 AND idempotency_key=$3`, v.agent.p2.project.ID.String(), target.String(), string(originalMeta.IdempotencyKey)).Scan(&state, &raw, &mapping, &frozenRevision); err != nil {
				return err
			}
			if state != "completed" {
				return nil
			} // Original prepare/freeze commit normally.
			mutation, err := ac.DecodeAgentMutation(raw)
			if err != nil || json.Unmarshal(mapping, &frozen) != nil || len(frozen) != 1 {
				return errors.New("final mapping/receipt missing")
			}
			if err = runnerMountRelations(ctx, x, v.agent.p2.project.ID, mutation, frozen); err != nil {
				return err
			}
			counts, err := agentCreateFacts(ctx, x, v.agent.p2.project.ID, target, originalMeta.IdempotencyKey)
			if err != nil || counts != ([16]int64{1, 1, 1, 0, 2, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1, 0}) {
				return errors.New("final real transaction lacks complete Agent/Mount/Audit/Outbox facts")
			}
			observed = true
			return marker // Original Store executes normal ROLLBACK, never a fabricated CommitResult.
		})
		store.mu.Lock()
		store.afterResult = func(txCause f.TransactionCause, result f.CommitResult) {
			d := txCause.Details()
			if observed && d.Kind == f.CommandsCause && d.Primary.Canonical() == identity.Canonical() {
				physical, returned = result, true
			}
		}
		store.mu.Unlock()
		clear := func() { store.setAfter(nil); store.mu.Lock(); store.afterResult = nil; store.mu.Unlock() }
		defer clear()
		var activityBefore, activityAfter time.Time
		if err := base.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id::text=$1`, base.ownerBrowser.actor.Details().SessionID).Scan(&activityBefore); err != nil {
			t.Fatal(err)
		}
		failed, err := v.agent.agents.CreateAgent(ctxFor(t), base.ownerBrowser.actor, originalMeta, v.agent.p2.project.ID, request)
		clear()
		if !observed || !returned || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) || !errors.Is(err, marker) || failed.Validate() == nil {
			t.Fatal("real final rollback did not preserve its actual result", err)
		}
		if err := base.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id::text=$1`, base.ownerBrowser.actor.Details().SessionID).Scan(&activityAfter); err != nil || !activityBefore.Equal(activityAfter) {
			t.Fatal("rolled-back mutation changed Session activity", err)
		}
		v.agent.checkFacts(t, target, originalMeta.IdempotencyKey, identity, [16]int64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, false)
		var definitions, mappings, revision int64
		var raw []byte
		if err := base.raw.QueryRow(ctxFor(t), `SELECT runner_mounts,plan_revision,
 (SELECT count(*) FROM agenteam_mount.mounts WHERE agent_id::text=$1),
 (SELECT count(*) FROM agenteam_mount.runner_workspaces WHERE agent_id::text=$1)
 FROM agenteam_agent.commands WHERE target_id::text=$1 AND idempotency_key=$2 AND state='planned'`, target.String(), string(originalMeta.IdempotencyKey)).Scan(&raw, &revision, &definitions, &mappings); err != nil || definitions != 0 || mappings != 0 || revision != frozenRevision || !runnerMountSameJSON(raw, jsonBytes(t, frozen)) {
			t.Fatal("rollback lost frozen original mapping or persisted final Mount facts", err)
		}
		digest, err := ac.AgentCommandDigest(base.ownerBrowser.actor, originalMeta, v.agent.p2.project.ID, target, ac.CreateAgentCommand, request)
		if err != nil {
			t.Fatal(err)
		}
		lookup, err := v.agent.agents.LookupAgentCommand(ctxFor(t), base.ownerBrowser.actor, v.agent.p2.project.ID, ac.CreateAgentCommand, originalMeta.IdempotencyKey, digest)
		if err != nil || lookup.Status() != ac.AgentLookupInProgress || lookup.Receipt() != nil {
			t.Fatal("rolled-back final reported a committed receipt", err)
		}
		retried, err := v.agent.agents.CreateAgent(ctxFor(t), base.ownerBrowser.actor, originalMeta, v.agent.p2.project.ID, request)
		if err != nil || retried.Validate() != nil || retried.Fields().RunnerMounts == nil || !slices.Equal(*retried.Fields().RunnerMounts, frozen) {
			t.Fatal("original planned command failed to reuse its frozen identities", err)
		}
		v.command(t, target, ac.CreateAgentCommand, originalMeta, request, retried)
		v.current(t, retried, frozen, 1)
	})
}

type runnerMountPGFixture struct {
	agent  *agentCreateFixture
	runner rc.Snapshot
	server *httptest.Server
	done   chan bool
}

func newRunnerMountPGFixture(t *testing.T) *runnerMountPGFixture {
	t.Helper()
	a := newAgentCreateFixture(t)
	base := a.p2.base
	var ordinary bool
	if err := base.raw.QueryRow(ctxFor(t), `SELECT role='user' FROM agenteam_account.users WHERE id::text=$1`, base.ownerBrowser.actor.Details().UserID).Scan(&ordinary); err != nil || !ordinary {
		t.Fatal("configuration Owner is not an actual ordinary Account", err)
	}
	authority, err := runnerservice.NewAuthority(base.tracked, base.accounts)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := audit.New(base.tracked, base.keys, audit.Authorizations{Sessions: base.accounts, System: base.accounts, Accounts: base.accounts, Runners: authority})
	if err != nil {
		t.Fatal(err)
	}
	runners, err := runnerservice.New(authority, auditor)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		runners.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := runners.Drain(ctx); err != nil {
			t.Error("original Runner service calls did not actually join", err)
		}
	})
	intent, err := rc.NewCreate(rc.CreateRequest{RunnerID: id[rc.Runner](t), Name: "configuration-runner", Description: "Formal offline configuration candidate", Tags: []string{}, RootPath: "/runner-mount-fixture/private-root"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := runners.Execute(ctxFor(t), base.adminBrowser.actor, "runner-mount-bootstrap-create", intent)
	if err != nil {
		t.Fatal("formal bootstrap administrator Runner creation returned an error")
	}
	if created.Receipt.Validate() != nil {
		t.Fatal("formal Runner creation returned an invalid receipt")
	}
	if created.Receipt.Runner.Status != rc.Offline {
		t.Fatal("formal Runner creation did not produce an offline candidate")
	}
	// The first known commit returns one-time enrollment material. It stays
	// local to this bootstrap; only the safe Runner snapshot enters the fixture.
	if created.Material == nil {
		t.Fatal("first known Runner creation commit omitted enrollment material")
	}
	if !created.Material.Token.Valid() {
		t.Fatal("first known Runner creation commit returned an invalid enrollment token")
	}
	if created.Material.ExpiresAt.Validate() != nil {
		t.Fatal("first known Runner creation commit returned an invalid enrollment expiry")
	}
	v := &runnerMountPGFixture{agent: a, runner: created.Receipt.Runner, done: make(chan bool, 1)}
	v.server = httptest.NewUnstartedServer(nil)
	t.Cleanup(v.server.Close) // Actual HTTP return/connection retirement precedes every domain/guard cleanup.
	boundary, err := account.NewHTTPBoundary(base.core, "https://"+v.server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	reads, err := agenthttp.NewRunnerMountHTTPHandler(a.agents, boundary)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := agenthttp.NewConfigurationHTTPHandler(a.agents, boundary)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := httpapi.Handler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if agenthttp.HandlesRunnerMountPath(r.URL.Path) {
			reads.ServeHTTP(w, r)
		} else {
			writes.ServeHTTP(w, r)
		}
	}))
	v.server.Config.ErrorLog = log.New(io.Discard, "", 0)
	v.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		normal := false
		defer func() { v.done <- normal }()
		wrapped.ServeHTTP(w, r)
		normal = true
	})
	v.server.StartTLS()
	if v.server.Client().Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify {
		t.Fatal("owned certificate verification disabled")
	}
	return v
}

func (v *runnerMountPGFixture) collection() string {
	return variableHTTPPath(v.agent.p2.project.ID, "/agents")
}
func (v *runnerMountPGFixture) mappingPath(target i.AgentID) string {
	return v.collection() + "/" + target.String() + "/runner-mounts"
}
func (v *runnerMountPGFixture) createRequest(t *testing.T, declarations []ac.RunnerMountDeclaration) (ac.AgentCreate, f.CommandMeta, f.CommandIdentity) {
	t.Helper()
	original, meta, command := v.agent.request(t)
	fields := original.Fields()
	fields.RunnerMounts, fields.AllowedMountIDs = &declarations, nil
	fields.Name = "mount-agent-" + fields.AgentID.String()[24:]
	request, err := ac.NewAgentCreate(fields)
	if err != nil {
		t.Fatal(err)
	}
	return request, meta, command
}
func (v *runnerMountPGFixture) request(t *testing.T, browser variableHTTPBrowser, method, path string, key f.IdempotencyKey, body []byte) variableHTTPResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := *v.server.Client()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	origin, err := url.Parse(v.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(origin, []*http.Cookie{{Name: "__Host-agenteam_session", Value: browser.cookie, Secure: true, HttpOnly: true, Path: "/", SameSite: http.SameSiteLaxMode}})
	request, err := http.NewRequestWithContext(ctx, method, v.server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", v.server.URL)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", browser.csrf)
		request.Header.Set("Idempotency-Key", string(key))
	}
	incoming := id[f.Request](t)
	request.Header.Set("X-Request-ID", incoming.String())
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("owned Runner Mount TLS request did not return")
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	length := response.ContentLength == int64(len(raw))
	if method == http.MethodHead {
		length = len(raw) == 0 && response.ContentLength >= 0
	}
	if readErr != nil || closeErr != nil || len(raw) > 1<<20 || !length || response.TLS == nil || len(response.TLS.VerifiedChains) == 0 {
		t.Fatal("original TLS response did not finish bounded EOF/Close/length")
	}
	select {
	case normal := <-v.done:
		if !normal {
			t.Fatal("original HTTP handler aborted")
		}
	case <-ctx.Done():
		t.Fatal("original HTTP handler failed to return")
	}
	rid, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
	if err != nil || len(response.Header.Values("X-Request-ID")) != 1 || rid == incoming || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("response omitted its server-owned RequestID/security headers")
	}
	if bytes.Contains(raw, []byte(v.runner.RootPath)) {
		t.Fatal("Project response exposed Runner private root")
	}
	return variableHTTPResponse{status: response.StatusCode, header: response.Header.Clone(), body: raw}
}
func (v *runnerMountPGFixture) head(t *testing.T, owner variableHTTPBrowser, path string, get variableHTTPResponse) {
	t.Helper()
	head := v.request(t, owner, http.MethodHead, path, "", nil)
	if head.status != http.StatusOK || len(head.body) != 0 || head.header.Get("Content-Length") != strconv.Itoa(len(get.body)) || head.header.Get("Content-Type") != get.header.Get("Content-Type") {
		t.Fatal("HEAD lost the complete current GET representation metadata")
	}
}
func runnerMountProblem(t *testing.T, reply variableHTTPResponse, status int, code f.Code) {
	t.Helper()
	var problem struct {
		Code f.Code `json:"code"`
	}
	if reply.status != status || json.Unmarshal(reply.body, &problem) != nil || problem.Code != code {
		t.Fatal("Runner Mount current-authority rejection mismatch", reply.status, problem.Code)
	}
}
func runnerMountRawItems(t *testing.T, raw []byte) []json.RawMessage {
	t.Helper()
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if json.Unmarshal(raw, &page) != nil || page.Items == nil {
		t.Fatal("original safe items missing")
	}
	return page.Items
}

func runnerMountFields(t *testing.T, raw []byte, names ...string) {
	t.Helper()
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != len(names) {
		t.Fatal("safe projection field count")
	}
	for _, name := range names {
		if _, ok := fields[name]; !ok {
			t.Fatal("safe projection field absent", name)
		}
	}
}
func (v *runnerMountPGFixture) mutation(t *testing.T, response variableHTTPResponse, version f.Version, changed bool, declarations []ac.RunnerMountDeclaration) ac.AgentMutation {
	t.Helper()
	out, err := ac.DecodeAgentMutation(response.body)
	fields := out.Fields()
	if response.status != http.StatusOK || err != nil || fields.Changed != changed || fields.Agent.Fields().Core.Version != version || fields.RunnerMounts == nil || ac.MatchRunnerMountBindings(declarations, *fields.RunnerMounts, fields.Agent.Fields().AllowedMountIDs) != nil {
		t.Fatal("original configuration HTTP returned an invalid declaration receipt", response.status, err)
	}
	return out
}
func (v *runnerMountPGFixture) update(t *testing.T, target i.AgentID, key string, expected f.Version, declarations []ac.RunnerMountDeclaration, version f.Version, changed bool) (ac.AgentMutation, ac.AgentUpdate) {
	t.Helper()
	request, err := ac.NewAgentUpdate(ac.AgentUpdateFields{RunnerMounts: &declarations})
	if err != nil {
		t.Fatal(err)
	}
	meta := meta(t, key, &expected)
	body := jsonBytes(t, map[string]any{"expected_version": expected, "request": request})
	out := v.mutation(t, v.request(t, v.agent.p2.base.ownerBrowser, http.MethodPatch, v.collection()+"/"+target.String()+"/configuration", meta.IdempotencyKey, body), version, changed, declarations)
	v.command(t, target, ac.UpdateAgentCommand, meta, request, out)
	return out, request
}

func runnerMountSameJSON(a, b []byte) bool {
	x, err := cursor.CanonicalJSON(a)
	if err != nil {
		return false
	}
	y, err := cursor.CanonicalJSON(b)
	return err == nil && bytes.Equal(x, y)
}

// Observe the original completed row, its exact input/digest/frozen mapping,
// and the typed Audit and Event that belong to this particular Agent command.
func (v *runnerMountPGFixture) command(t *testing.T, target i.AgentID, name ac.CommandName, meta f.CommandMeta, request any, receipt ac.AgentMutation) {
	t.Helper()
	base, project := v.agent.p2.base, v.agent.p2.project.ID
	var commandID, user, semantic string
	var input, mapping, stored []byte
	var revision int64
	var expected *int64
	var exactPostimage bool
	err := base.raw.QueryRow(ctxFor(t), `SELECT id::text,actor_user_id::text,semantic_digest,input,runner_mounts,receipt,plan_revision,expected_version,
 after_config=receipt->'agent' AND runner_mounts=receipt->'runner_mounts'
 FROM agenteam_agent.commands WHERE project_id::text=$1 AND target_id::text=$2 AND command_name=$3 AND idempotency_key=$4 AND state='completed'`, project.String(), target.String(), string(name), string(meta.IdempotencyKey)).Scan(&commandID, &user, &semantic, &input, &mapping, &stored, &revision, &expected, &exactPostimage)
	if err != nil || revision < 1 || !exactPostimage || user != base.ownerBrowser.actor.Details().UserID || !runnerMountSameJSON(input, jsonBytes(t, request)) || !runnerMountSameJSON(stored, jsonBytes(t, receipt)) || !runnerMountSameJSON(mapping, jsonBytes(t, *receipt.Fields().RunnerMounts)) || (expected == nil) != (meta.ExpectedVersion == nil) || expected != nil && *expected != int64(*meta.ExpectedVersion) {
		t.Fatal("original immutable Agent command/receipt relation", err)
	}
	if _, err := f.ParseID[ac.AgentCommand](commandID); err != nil {
		t.Fatal("invalid original command UUID")
	}
	digest, err := ac.AgentCommandDigest(base.ownerBrowser.actor, meta, project, target, name, request)
	if err != nil || semantic != digest.String() {
		t.Fatal("original command semantic digest does not bind declarations and expected version")
	}
	identity, err := ac.AgentCommandIdentity(project, name, meta.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	causeDigest, err := cursor.Digest([]byte(identity.Canonical()))
	if err != nil {
		t.Fatal(err)
	}
	var audits, events int64
	if err = base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=$1 AND producer='agent' AND resource_id::text=$2 AND cause_ref=$3),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id::text=$1 AND producer='agent' AND aggregate_id::text=$2 AND id::text=$4)`, project.String(), target.String(), causeDigest.String(), commandID).Scan(&audits, &events); err != nil {
		t.Fatal(err)
	}
	fields := receipt.Fields()
	if !fields.Changed {
		if audits != 0 || events != 0 || len(fields.EventIDs) != 0 {
			t.Fatal("no-op emitted Audit or Outbox facts")
		}
		return
	}
	if audits != 1 || events != 1 || len(fields.EventIDs) != 1 || fields.EventIDs[0].String() != commandID {
		t.Fatal("changed Agent command lacks its exact Audit/Outbox pair")
	}
	var auditRaw, headerRaw, payloadRaw []byte
	var session string
	if err = base.raw.QueryRow(ctxFor(t), `SELECT metadata,session_id::text FROM agenteam_audit.audit_records WHERE project_id::text=$1 AND resource_id::text=$2 AND producer='agent' AND action=$3 AND cause_ref=$4 AND ordinal=0 AND actor_kind='human' AND user_id::text=$5 AND outcome='success' AND resource_kind='agent'`, project.String(), target.String(), string(name), causeDigest.String(), user).Scan(&auditRaw, &session); err != nil || session != base.ownerBrowser.actor.Details().SessionID {
		t.Fatal("Audit original current Human Session/cause", err)
	}
	metadata, err := auditc.DecodeMetadata(auditc.Action(name), auditRaw)
	if err != nil {
		t.Fatal("typed Agent Audit decode", err)
	}
	auditFields, err := metadata.AgentFields()
	wantFields, operation := []string{"allowed_mount_ids"}, ac.ConfigUpdated
	if name == ac.CreateAgentCommand {
		wantFields, operation = ac.EditableFields(), ac.ConfigCreated
	}
	if err != nil || auditFields.CommandID != commandID || auditFields.AgentID != target.String() || auditFields.Version != fields.Agent.Fields().Core.Version || !slices.Equal(auditFields.ChangedFields, wantFields) {
		t.Fatal("typed Agent Audit does not describe the original configuration change")
	}
	if err = base.raw.QueryRow(ctxFor(t), `SELECT header,payload FROM agenteam_outbox.events WHERE project_id::text=$1 AND aggregate_id::text=$2 AND producer='agent' AND id::text=$3 AND event_type='agent.config_changed' AND schema_version=1`, project.String(), target.String(), commandID).Scan(&headerRaw, &payloadRaw); err != nil {
		t.Fatal(err)
	}
	header, err := ec.DecodeHeader(headerRaw)
	var payload ac.ConfigChangedPayload
	if err != nil || ac.ValidateAgentEventHeader(header) != nil || json.Unmarshal(payloadRaw, &payload) != nil || payload.Validate() != nil || header.EventID.String() != commandID || header.Scope.ProjectID.String() != project.String() || header.AggregateID.String() != target.String() || *header.AggregateVersion != fields.Agent.Fields().Core.Version || !header.OccurredAt.Time().Equal(fields.Agent.Fields().Core.UpdatedAt.Time()) || payload.CommandID.String() != commandID || payload.ActorUserID.String() != user || payload.Operation != operation || !slices.Equal(payload.ChangedFields, wantFields) {
		t.Fatal("typed original Agent Event does not match Audit/receipt/current change")
	}
}

// This observer is also called inside the actual final transaction, before its
// controlled ordinary rollback. It inserts no canonical or provider facts.
func runnerMountRelations(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, receipt ac.AgentMutation, definitions []ac.RunnerMountBinding) error {
	if receipt.Validate() != nil || receipt.Fields().RunnerMounts == nil {
		return errors.New("invalid declaration receipt")
	}
	fields := receipt.Fields().Agent.Fields()
	target, version := fields.Core.ID, fields.Core.Version
	var canonicalVersion, headVersion, countDefinitions, countMappings int64
	var forward, reverse []byte
	err := x.QueryRow(ctx, `SELECT a.version,h.owner_version,
 (SELECT count(*) FROM agenteam_mount.mounts WHERE project_id::text=$1 AND agent_id::text=$2),
 (SELECT count(*) FROM agenteam_mount.runner_workspaces WHERE project_id::text=$1 AND agent_id::text=$2),
 (SELECT coalesce(jsonb_agg(mount_id::text ORDER BY mount_id),'[]'::jsonb) FROM agenteam_agent.mount_allowlist WHERE project_id::text=$1 AND agent_id::text=$2),
 (SELECT coalesce(jsonb_agg(mount_id::text ORDER BY mount_id),'[]'::jsonb) FROM agenteam_mount.agent_mount_refs WHERE project_id::text=$1 AND agent_id::text=$2)
 FROM agenteam_agent.agents a JOIN agenteam_mount.agent_mount_heads h ON h.project_id=a.project_id AND h.agent_id=a.id
 WHERE a.project_id::text=$1 AND a.id::text=$2`, project.String(), target.String()).Scan(&canonicalVersion, &headVersion, &countDefinitions, &countMappings, &forward, &reverse)
	if err != nil {
		return err
	}
	ids, err := json.Marshal(fields.AllowedMountIDs)
	if err != nil || canonicalVersion != int64(version) || headVersion != int64(version) || countDefinitions != int64(len(definitions)) || countMappings != int64(len(definitions)) || !runnerMountSameJSON(forward, ids) || !runnerMountSameJSON(reverse, ids) {
		return errors.New("canonical/head/full-bidirectional-reference mismatch")
	}
	for _, b := range definitions {
		var matches bool
		if err = x.QueryRow(ctx, `SELECT m.project_id::text=$1 AND m.agent_id::text=$2 AND m.runner_id::text=$4 AND m.workspace=$5 AND m.name=$3 AND m.lifecycle='active' AND m.version=1
 AND w.project_id=m.project_id AND w.agent_id=m.agent_id AND w.runner_id=m.runner_id AND w.workspace=m.workspace AND w.workspace_id::text=$6
 FROM agenteam_mount.mounts m JOIN agenteam_mount.runner_workspaces w ON w.mount_id=m.id WHERE m.id::text=$3`, project.String(), target.String(), b.MountID.String(), b.RunnerID.String(), b.Workspace, b.WorkspaceID.String()).Scan(&matches); err != nil {
			return err
		}
		if !matches {
			return errors.New("stable definition/name/workspace tuple mismatch")
		}
	}
	return nil
}

func (v *runnerMountPGFixture) current(t *testing.T, receipt ac.AgentMutation, definitions []ac.RunnerMountBinding, completed int64) {
	t.Helper()
	base, project := v.agent.p2.base, v.agent.p2.project.ID
	config := receipt.Fields().Agent
	target := config.Fields().Core.ID
	before := v.snapshot(t, target)
	actual, err := v.agent.agents.GetAgent(ctxFor(t), base.ownerBrowser.actor, project, target)
	if err != nil || !bytes.Equal(jsonBytes(t, actual), jsonBytes(t, config)) {
		t.Fatal("current formal GetAgent differs from the current receipt", err)
	}
	get := v.request(t, base.ownerBrowser, http.MethodGet, v.collection()+"/"+target.String()+"/configuration", "", nil)
	var httpConfig ac.AgentConfig
	if get.status != http.StatusOK || json.Unmarshal(get.body, &httpConfig) != nil || !bytes.Equal(jsonBytes(t, httpConfig), jsonBytes(t, config)) {
		t.Fatal("current strict HTTP AgentConfig mismatch")
	}
	get = v.request(t, base.ownerBrowser, http.MethodGet, v.mappingPath(target), "", nil)
	var current ac.AgentRunnerMounts
	if get.status != http.StatusOK || json.Unmarshal(get.body, &current) != nil || current.Validate() != nil || current.ProjectID != project || current.AgentID != target || current.AgentVersion != config.Fields().Core.Version || len(current.Items) != len(config.Fields().AllowedMountIDs) {
		t.Fatal("current safe Mount read lost its complete Agent version/references")
	}
	runnerMountFields(t, get.body, "project_id", "agent_id", "agent_version", "items")
	rawItems := runnerMountRawItems(t, get.body)
	for n, item := range current.Items {
		runnerMountFields(t, rawItems[n], "mount_id", "runner_id", "workspace", "name")
		if item.MountID != config.Fields().AllowedMountIDs[n] || item.Name != item.MountID.String() {
			t.Fatal("current mapping identity/alias mismatch")
		}
		found := false
		for _, b := range *receipt.Fields().RunnerMounts {
			if b.MountID == item.MountID {
				found = b.RunnerID == item.RunnerID && b.Workspace == item.Workspace
			}
		}
		if !found {
			t.Fatal("current mapping lost the original full workspace bytes")
		}
	}
	v.head(t, base.ownerBrowser, v.mappingPath(target), get)
	if err = runnerMountRelations(ctxFor(t), base.raw, project, receipt, definitions); err != nil {
		t.Fatal("actual canonical Mount relationships", err)
	}
	var commands, planned, audits, events, executions, connections int64
	if err = base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_agent.commands WHERE project_id::text=$1 AND target_id::text=$2 AND state='completed'),
 (SELECT count(*) FROM agenteam_agent.commands WHERE project_id::text=$1 AND target_id::text=$2 AND state='planned'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=$1 AND resource_id::text=$2 AND producer='agent'),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id::text=$1 AND aggregate_id::text=$2 AND producer='agent'),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id::text=$1),
 (SELECT count(*) FROM agenteam_runner.connections WHERE runner_id::text=$3)`, project.String(), target.String(), v.runner.ID.String()).Scan(&commands, &planned, &audits, &events, &executions, &connections); err != nil || commands != completed || planned != 0 || audits != int64(config.Fields().Core.Version) || events != audits || executions != 0 || connections != 0 {
		t.Fatal("current configuration changed exact side-effect/Execution/connection facts", err)
	}
	if v.snapshot(t, target) != before {
		t.Fatal("current GET/HEAD/provider reads modified configuration facts")
	}
}

func (v *runnerMountPGFixture) strictReadRollback(t *testing.T, receipt ac.AgentMutation) {
	t.Helper()
	base, project := v.agent.p2.base, v.agent.p2.project.ID
	fields := receipt.Fields().Agent.Fields()
	user, _ := f.UserLock(base.ownerBrowser.actor.Details().UserID)
	projectLock, _ := f.ProjectLock(project.String())
	agentLock, _ := f.AgentLock(fields.Core.ID.String())
	locks, err := oc.NormalizeLocks([]f.LockRequest{{Key: user, Mode: f.Shared}, {Key: projectLock, Mode: f.Shared}, {Key: agentLock, Mode: f.Exclusive}})
	if err != nil {
		t.Fatal(err)
	}
	marker := errors.New("runner-mount-inconsistent-ref-real-rollback")
	checked := false
	result := base.tracked.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
		if err := base.tracked.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		x, err := base.tracked.InTx(tx)
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_mount.agent_mount_refs WHERE project_id::text=$1 AND agent_id::text=$2 AND mount_id::text=$3`, project.String(), fields.Core.ID.String(), fields.AllowedMountIDs[0].String())
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errors.New("original reverse ref absent before integrity control")
		}
		items, err := v.agent.providers.Mounts.RunnerMountBindingsInTx(ctx, tx, base.ownerBrowser.actor, project, fields.Core.ID, fields.Core.Version, fields.AllowedMountIDs)
		var fault *f.Fault
		if !errors.As(err, &fault) || fault.Code != f.VersionConflict || items != nil {
			return errors.New("same-Tx integrity reader silently returned partial/empty mappings")
		}
		checked = true
		return marker
	})
	if !checked || result.State() != f.NotCommitted || !errors.Is(result.Fault(), marker) {
		t.Fatal("controlled inconsistent reverse-ref transaction did not really roll back", result.Fault())
	}
	t.Log("same-Tx reverse-reference integrity rejection; original Store rollback restored the real ref; no Mount or Runner SQL seed")
}

func (v *runnerMountPGFixture) snapshot(t *testing.T, target i.AgentID) string {
	t.Helper()
	var raw string
	err := v.agent.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'agent',(SELECT to_jsonb(a) FROM agenteam_agent.agents a WHERE id::text=$1),
 'commands',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_agent.commands c WHERE target_id::text=$1),
 'definitions',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY id),'[]'::jsonb) FROM agenteam_mount.mounts m WHERE agent_id::text=$1),
 'workspaces',(SELECT coalesce(jsonb_agg(to_jsonb(w) ORDER BY mount_id),'[]'::jsonb) FROM agenteam_mount.runner_workspaces w WHERE agent_id::text=$1),
 'head',(SELECT to_jsonb(h) FROM agenteam_mount.agent_mount_heads h WHERE agent_id::text=$1),
 'forward',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY mount_id),'[]'::jsonb) FROM agenteam_agent.mount_allowlist r WHERE agent_id::text=$1),
 'reverse',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY mount_id),'[]'::jsonb) FROM agenteam_mount.agent_mount_refs r WHERE agent_id::text=$1),
 'audit',(SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='agent' AND resource_id::text=$1),
 'events',(SELECT count(*) FROM agenteam_outbox.events WHERE producer='agent' AND aggregate_id::text=$1))::text`, target.String()).Scan(&raw)
	if err != nil {
		t.Fatal("real Runner Mount snapshot unavailable", err)
	}
	return raw
}
