//go:build integration && linux

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type projectCreationAppResult struct {
	State     pc.CreationResultState `json:"state"`
	Project   *pc.ProjectRef         `json:"project,omitempty"`
	Operation *pc.CreationOperation  `json:"operation,omitempty"`
	StatusURL string                 `json:"status_url,omitempty"`
}

type projectCreationAppPublication struct {
	creation, skill, packageHash, snapshot string
	packageSize                            int64
}

// The inherited setup retires before public app.Run. Its old Project is only
// Account/Model fixture preparation: every Project asserted here is created by
// the default HTTP handler and its actual Skill initializer, not that fixture.
// App cancellation, original Run return, guard flock and failure retention use
// the existing owner; no timeout is treated as a successful local Join.
func TestProjectCreationApp(t *testing.T) {
	if !t.Run("default-create-and-configure", func(t *testing.T) {
		agentProjectAppHandoff(t, func(v *agentProjectApp) {
			owner := v.setup.p2.base.ownerBrowser
			project := id[i.Project](t)
			key := f.IdempotencyKey("default-app-project-create")
			input := pc.CreateProjectRequest{ProjectID: project, Name: "Default_App_Project", Description: "Created through the default App."}
			body := jsonBytes(t, input)
			response := v.http.request(t, owner, http.MethodPost, "/api/v1/projects", body, key, false)
			ready := projectCreationAppReady(t, response, http.StatusCreated, input, owner.actor)
			publication := projectCreationAppPublished(t, v.setup, v.ctx, project, key, v.process)

			// This is a replay of a known complete response, followed by the
			// real status endpoint. It does not simulate delivery loss/Unknown.
			replayed := v.http.request(t, owner, http.MethodPost, "/api/v1/projects", body, key, false)
			projectCreationAppReady(t, replayed, http.StatusCreated, input, owner.actor)
			if !bytes.Equal(response.body, replayed.body) {
				t.Fatal("same Project identity changed the original ready projection")
			}
			statusPath := "/api/v1/project-creations/" + publication.creation
			status := v.http.request(t, owner, http.MethodGet, statusPath, nil, "", false)
			projectCreationAppReady(t, status, http.StatusOK, input, owner.actor)
			if !bytes.Equal(status.body, response.body) {
				t.Fatal("original creation status differs from ready")
			}
			head := v.http.request(t, owner, http.MethodHead, statusPath, nil, "", false)
			if head.status != http.StatusOK || len(head.body) != 0 || head.header.Get("Content-Length") != strconv.Itoa(len(status.body)) {
				t.Fatal("original creation HEAD differs from GET")
			}
			agentAppProblem(t, v.http.request(t, v.setup.p2.base.otherBrowser, http.MethodGet, statusPath, nil, "", false), http.StatusNotFound, f.NotFound)
			changed := input
			changed.Description = "Different intent with the same original key."
			agentAppProblem(t, v.http.request(t, owner, http.MethodPost, "/api/v1/projects", jsonBytes(t, changed), key, false), http.StatusConflict, f.IdempotencyKeyReused)
			if after := projectCreationAppPublished(t, v.setup, v.ctx, project, key, v.process); after != publication {
				t.Fatal("replay, status or rejected identity changed publication facts")
			}

			projectCreationAppPackage(t, v, ready, publication)
			projectCreationAppConsumers(t, v, project)
			v.requestStop()
			select {
			case <-v.done:
			case <-v.ctx.Done():
				t.Fatal("Project creation App did not actually return")
			}
			agentAppRequire(t, v.runErr)
			projectCreationAppReturned(t, v.setup, v.ctx, project, v.process, 2, 4)
			t.Log("PROJECT_CREATION_APP default_http=true same_key_status=true actual_skills_object=true package_bytes=true new_project_agent=true ordinary_and_secret=true original_run_returned=true")
		})
	}) {
		return
	}
	t.Run("create-through-browser", projectCreationAppBrowser)
}

func projectCreationAppReady(t *testing.T, response variableHTTPResponse, status int, input pc.CreateProjectRequest, actor i.Actor) pc.ProjectRef {
	t.Helper()
	var result projectCreationAppResult
	agentAppRequire(t, agentWebDecode(response.body, &result))
	if response.status != status || result.State != pc.CreationReady || result.Project == nil || result.Operation != nil || result.StatusURL != "" || response.header.Get("Location") != "" {
		t.Fatal("default Project HTTP did not complete the requested ready response", response.status)
	}
	p := *result.Project
	if p.Validate() != nil || p.ID != input.ProjectID || p.OwnerUserID.String() != actor.Details().UserID || p.Name != input.Name || p.Description != input.Description || p.Lifecycle != pc.Active || p.Version != 1 || p.CurrentSprintID != nil {
		t.Fatal("ready Project omitted its current Owner or original fields")
	}
	return p
}

// Observe the original cross-domain publication without writing any ready,
// receipt, Skill, Object or process facts. Delivery state and read leases are
// intentionally excluded from the immutable publication snapshot.
func projectCreationAppPublished(t *testing.T, a *agentCreateFixture, ctx context.Context, project i.ProjectID, key f.IdempotencyKey, process string) projectCreationAppPublication {
	t.Helper()
	var out projectCreationAppPublication
	var exact bool
	err := a.p2.base.raw.QueryRow(ctx, `SELECT c.id::text,z.skill_id::text,z.package_sha256,z.byte_size,
 p.initialized_at IS NOT NULL AND p.lifecycle='active' AND p.version=1
 AND p.current_sprint_id IS NULL AND c.state='completed' AND c.command_key=$2
 AND c.owner_user_id=p.owner_user_id AND p.owner_user_id::text=$3
 AND c.protected_skill_id=z.skill_id AND c.protected_revision=z.revision
 AND z.creation_id=c.id AND z.phase='published' AND z.revision=1
 AND z.bundle_id='builtin.add-skills.v1' AND s.protected AND s.serving
 AND s.current_revision=1 AND s.project_id=p.id AND s.creation_id=c.id
 AND r.project_id=p.id AND r.skill_id=s.id AND r.revision=1
 AND r.object_id=z.object_id AND r.object_version=o.version
 AND o.scope='project' AND o.project_id=p.id AND o.partition_id=p.id
 AND o.state='available' AND NOT o.cleaning AND o.byte_size=z.byte_size
 AND 'sha256:'||encode(o.sha256,'hex')=z.package_sha256
 AND u.project_id=p.id AND u.object_id=o.id AND u.owner_kind='skill_revision'
 AND u.state='committed' AND u.disposition='attached'
 AND attempt.process_id::text=$4 AND claim.process_id=attempt.process_id
 AND claim.phase='terminal' AND claim.work_id=c.id
 AND (SELECT count(*) FROM agenteam_object.object_references x WHERE x.object_id=o.id
      AND x.partition_id=p.id AND x.owner_kind='skill_revision' AND x.kind='canonical'
      AND x.upload_id=u.id AND x.owner_id=u.owner_id)=1
 AND (SELECT count(*) FROM agenteam_audit.audit_records x WHERE x.project_id=p.id
      AND x.producer='project' AND x.resource_id=c.id AND x.action='project.create.accepted')=1
 AND (SELECT count(*) FROM agenteam_audit.audit_records x WHERE x.project_id=p.id
      AND x.producer='project' AND x.resource_id=c.id AND x.action='project.create.completed')=1,
 jsonb_build_object('project',to_jsonb(p),'creation',to_jsonb(c),
  'initialization',to_jsonb(z),'skill',to_jsonb(s),'revision',to_jsonb(r),
  'object',to_jsonb(o),'upload',to_jsonb(u),'attempt',to_jsonb(attempt),'claim',to_jsonb(claim))::text
 FROM agenteam_project.projects p
 JOIN agenteam_project.creations c ON c.id=p.creation_id
 JOIN agenteam_skill.initializations z ON z.project_id=p.id
 JOIN agenteam_skill.skills s ON s.id=z.skill_id
 JOIN agenteam_skill.revisions r ON r.id=z.revision_id
 JOIN agenteam_object.objects o ON o.id=z.object_id
 JOIN agenteam_object.uploads u ON u.id=z.upload_id
 JOIN agenteam_skill.object_attempts attempt ON attempt.attempt_id=z.current_attempt_id
 JOIN agenteam_project.work_claims claim ON claim.project_id=p.id AND claim.work_kind='creation'
 WHERE p.id=$1`, project.String(), string(key), a.p2.base.ownerBrowser.actor.Details().UserID, process).Scan(&out.creation, &out.skill, &out.packageHash, &out.packageSize, &exact, &out.snapshot)
	agentAppRequire(t, err)
	if !exact || out.packageSize <= 0 || out.packageSize > 262144 {
		t.Fatal("default Project initialization lacks its exact original publication")
	}
	return out
}

func projectCreationAppPackage(t *testing.T, v *agentProjectApp, project pc.ProjectRef, publication projectCreationAppPublication) {
	t.Helper()
	path := variableHTTPPath(project.ID, "/skills")
	listing := v.http.request(t, v.setup.p2.base.ownerBrowser, http.MethodGet, path, nil, "", false)
	var directory struct {
		Items []sc.Metadata `json:"items"`
	}
	agentAppRequire(t, agentWebDecode(listing.body, &directory))
	if listing.status != http.StatusOK || len(directory.Items) != 1 || directory.Items[0].ProjectID != project.ID || directory.Items[0].ID.String() != publication.skill || !directory.Items[0].Protected || directory.Items[0].CurrentRevision != 1 {
		t.Fatal("new Project cannot read its actual protected Skill")
	}
	response := v.http.request(t, v.setup.p2.base.ownerBrowser, http.MethodGet, path+"/"+publication.skill+"/revisions/1/package", nil, "", false)
	if response.status != http.StatusOK || int64(len(response.body)) != publication.packageSize || fmt.Sprintf("sha256:%x", sha256.Sum256(response.body)) != publication.packageHash {
		t.Fatal("actual MinIO package bytes differ from the published revision")
	}
}

func projectCreationAppModel(t *testing.T, v *agentProjectApp, project i.ProjectID) mc.ModelID {
	t.Helper()
	// Configuration only. Neither endpoint may invoke the example provider.
	provider := map[string]any{"name": "creation-provider", "protocol": "openai-chat-completions", "base_url": "https://creation.example/v1", "enabled": true, "credential_ref": nil, "options": map[string]any{}}
	type receipt struct {
		Kind     string        `json:"kind"`
		Resource string        `json:"resource_id"`
		Version  f.Version     `json:"version"`
		Affected mc.TokenCount `json:"affected_references"`
	}
	create := func(path, kind string, body any, key f.IdempotencyKey) receipt {
		t.Helper()
		response := v.http.request(t, v.setup.p2.base.ownerBrowser, http.MethodPost, variableHTTPPath(project, path), jsonBytes(t, body), key, false)
		var out receipt
		agentAppRequire(t, agentWebDecode(response.body, &out))
		if response.status != http.StatusOK || out.Kind != kind || out.Version != 1 || out.Affected != 0 {
			t.Fatal("new Project model configuration did not return its original receipt")
		}
		return out
	}
	p := create("/model-providers", "provider.create", map[string]any{"input": provider}, "creation-provider")
	providerID, err := f.ParseID[mc.Provider](p.Resource)
	agentAppRequire(t, err)
	model := map[string]any{"name": "creation-chat", "provider_model_id": "creation-chat", "type": "chat", "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{}, "capabilities": map[string]any{"tool_calls": true, "parallel_tool_calls": false, "streaming": false, "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "reasoning_efforts": []string{}, "structured_output_modes": []string{}, "context_length": nil, "max_output": nil}}
	m := create("/models", "model.create", map[string]any{"provider_id": providerID, "input": model}, "creation-model")
	modelID, err := f.ParseID[mc.Model](m.Resource)
	agentAppRequire(t, err)
	return modelID
}

func projectCreationAppConsumers(t *testing.T, v *agentProjectApp, project i.ProjectID) {
	t.Helper()
	owner := v.setup.p2.base.ownerBrowser
	model := projectCreationAppModel(t, v, project)
	request, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: id[i.Agent](t), Name: "creation-agent", ModelRef: model, InjectAgentsMD: true, ApprovalPolicy: ac.ApprovalAuto, ApprovalModelRef: &model, AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{}})
	agentAppRequire(t, err)
	var input map[string]json.RawMessage
	agentAppRequire(t, json.Unmarshal(jsonBytes(t, request), &input))
	delete(input, "add_skills_enabled")
	delete(input, "install_skill_enabled")
	const key = f.IdempotencyKey("creation-consumer-agent")
	response := v.http.request(t, owner, http.MethodPost, variableHTTPPath(project, "/agents"), jsonBytes(t, map[string]any{"request": input}), key, false)
	created, err := ac.DecodeAgentMutation(response.body)
	agentAppRequire(t, err)
	if response.status != http.StatusOK || created.Fields().Agent.Fields().Core.ProjectID != project || created.Fields().Agent.Fields().Core.ID != request.Fields().AgentID {
		t.Fatal("new Project Agent creation failed")
	}
	target := request.Fields().AgentID
	get := v.http.request(t, owner, http.MethodGet, variableHTTPPath(project, "/agents/"+target.String()+"/configuration"), nil, "", false)
	if get.status != http.StatusOK || !bytes.Equal(get.body, jsonBytes(t, created.Fields().Agent)) {
		t.Fatal("new Project Agent full configuration differs from its receipt")
	}
	facts, err := agentCreateFacts(v.ctx, v.setup.p2.base.raw, project, target, key)
	agentAppRequire(t, err)
	if facts != [16]int64{1, 1, 0, 0, 2, 1, 1, 1, 0, 0, 1, 1, 1, 1, 1, 0} {
		t.Fatal("new Project Agent omitted actual default Skill/Tool/reference publication")
	}
	command, err := ac.AgentCommandIdentity(project, ac.CreateAgentCommand, key)
	agentAppRequire(t, err)
	agentAppRequire(t, agentCreateRelations(v.ctx, v.setup.p2.base.raw, project, target, key, model, v.setup.tool.ToolID, command))

	plain := createInput(t, "CREATED_PROJECT_VALUE", "ordinary setting")
	response = v.http.request(t, owner, http.MethodPost, variableHTTPPath(project, "/variables"), jsonBytes(t, map[string]any{"request": plain}), "creation-consumer-variable", false)
	plainReceipt := mutationHTTP(t, response)
	if plainReceipt.Fields().Command != vc.CreateCommand || !plainReceipt.Fields().Changed || plainReceipt.Fields().Variable.Fields().ProjectID != project || plainReceipt.Fields().Variable.Fields().ID != plain.Fields().ID {
		t.Fatal("new Project ordinary Variable lost its actual Create receipt")
	}
	get = v.http.request(t, owner, http.MethodGet, variableHTTPPath(project, "/variables/"+plain.Fields().ID.String()), nil, "", false)
	if get.status != http.StatusOK || !bytes.Equal(get.body, jsonBytes(t, plainReceipt.Fields().Variable)) {
		t.Fatal("new Project ordinary Variable cannot be read")
	}

	const secret = "PROJECT_CREATION_PRIVATE_CANARY_8fa97"
	secretID := id[i.ProjectVariable](t)
	secretBody := []byte(secretHTTPCreateBody(t, secretID, secret))
	defer clear(secretBody)
	response = v.http.request(t, owner, http.MethodPost, variableHTTPPath(project, "/secret-variables"), secretBody, "creation-consumer-secret", false)
	secretReceipt := secretHTTPMutation(t, response)
	if bytes.Contains(response.body, []byte(secret)) || secretReceipt.Fields().Command != vc.SecretCreateCommand || !secretReceipt.Fields().Changed || secretReceipt.Fields().Variable.Fields().ProjectID != project || secretReceipt.Fields().Variable.Fields().ID != secretID {
		t.Fatal("new Project Secret creation omitted its safe projection")
	}
	get = v.http.request(t, owner, http.MethodGet, variableHTTPPath(project, "/secret-variables/"+secretID.String()), nil, "", false)
	if get.status != http.StatusOK || bytes.Contains(get.body, []byte(secret)) || !bytes.Equal(get.body, jsonBytes(t, secretReceipt.Fields().Variable)) {
		t.Fatal("new Project Secret metadata read changed or exposed private data")
	}
	var receiptCount int
	agentAppRequire(t, v.setup.p2.base.raw.QueryRow(v.ctx, `SELECT count(*)
 FROM agenteam_projectvariable.secret_commands c
 JOIN agenteam_secret.project_variable_receipts r ON r.id=c.d04_receipt_id
 JOIN agenteam_projectvariable.variables x ON (x.project_id,x.id)=(c.project_id,c.target_id)
 WHERE c.project_id=$1 AND c.target_id=$2 AND c.idempotency_key='creation-consumer-secret'
 AND c.command_name='project.secret_variable.create' AND c.receipt=$3::jsonb
 AND r.project_id=c.project_id AND r.variable_id=c.target_id AND r.command_kind=c.command_name
 AND r.user_id=c.actor_user_id AND r.credential_id=c.credential_id AND r.credential_id=x.credential_id
 AND r.effect='create' AND NOT r.deleted AND r.result_version=1
 AND c.secret_effect=r.effect AND NOT c.secret_deleted AND c.credential_version=r.result_version
 AND x.type='secret' AND x.credential_version=1 AND x.version=1 AND x.deleted_at IS NULL`, project.String(), secretID.String(), response.body).Scan(&receiptCount))
	if receiptCount != 1 {
		t.Fatal("new Project Secret command lacks its original D04 receipt")
	}
	v.logs.mu.Lock()
	leaked := bytes.Contains(v.logs.data.Bytes(), []byte(secret))
	v.logs.mu.Unlock()
	if leaked {
		t.Fatal("App logs exposed the private Secret fixture")
	}
}

func projectCreationAppReturned(t *testing.T, a *agentCreateFixture, ctx context.Context, project i.ProjectID, process string, agents, variables int) {
	t.Helper()
	var acount, vcount int
	var joined bool
	agentAppRequire(t, a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_agent.project_calls WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_projectvariable.project_calls WHERE project_id=$1),
 NOT EXISTS(SELECT 1 FROM agenteam_agent.project_calls WHERE project_id=$1 AND (process_id::text<>$2 OR phase<>'joined' OR joined_at IS NULL))
 AND NOT EXISTS(SELECT 1 FROM agenteam_projectvariable.project_calls WHERE project_id=$1 AND (process_id::text<>$2 OR phase<>'business_closed' OR closed_at IS NULL))
 AND NOT EXISTS(SELECT 1 FROM agenteam_skill.work WHERE project_id=$1 AND (process_id::text<>$2 OR phase<>'joined' OR joined_at IS NULL))
 AND NOT EXISTS(SELECT 1 FROM agenteam_project.work_claims WHERE project_id=$1 AND (process_id::text<>$2 OR phase<>'terminal'))`, project.String(), process).Scan(&acount, &vcount, &joined))
	if acount != agents || vcount != variables || !joined {
		t.Fatal("App actual return preceded one of its original Project consumer calls")
	}
}

// The browser uses the existing real-App owner and verified TLS bridge. Its
// HTTP front only serves frozen SPA assets; all /api requests reach app.Run.
// The old setup's Project is not the one created by the user flow below.
func projectCreationAppBrowser(t *testing.T) {
	entered, completed := false, false
	t.Cleanup(func() {
		if !entered || !completed {
			t.Error("Project creation browser original handoff incomplete")
		}
	})
	a := newAgentCreateFixture(t)
	original := a.p2.base
	process, err := a.guard.CurrentProcess()
	agentAppRequire(t, err)
	var username string
	agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id=$1`, original.ownerBrowser.actor.Details().UserID).Scan(&username))
	if original.afterOwners != nil {
		t.Fatal("original Project creation handoff already occupied")
	}
	original.afterOwners = func() {
		entered = true
		if !original.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
			t.Fatal("original Project creation setup owners have not returned")
		}
		if _, err := a.guard.CurrentProcess(); err == nil {
			t.Fatal("original Project creation setup guard remains held")
		}
		var stopped bool
		agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
		if !stopped {
			t.Fatal("original Project creation setup process has not retired")
		}
		w := newProjectCreationAppWeb(t, a, username)
		defer w.dispose(t)
		var appProcess string
		withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
			defer w.close(t)
			agentAppRequire(t, original.raw.QueryRow(ctx, `SELECT process_id::text FROM agenteam_object.process_claims WHERE process_id<>$1 AND state='claimed'`, process.String()).Scan(&appProcess))
			w.start(t, address)
			w.browser(t, ctx)
			w.close(t)
			w.verify(t, ctx, appProcess)
		})
		// The enclosing owner has now received the original Run return and
		// observed canonical process retirement. No browser claims full Registry.
		projectCreationAppReturned(t, a, ctxFor(t), w.input.ProjectID, appProcess, 0, 0)
		completed = true
		t.Log("PROJECT_CREATION_BROWSER default_app=true unique_post=true original_tls_complete=true actual_publication=true current_project_entered=true node_and_app_returned=true")
	}
}

type projectCreationAppWeb struct {
	*agentEditWeb
	input                 pc.CreateProjectRequest
	key                   f.IdempotencyKey
	ready                 []byte
	posts, resolves, gets int
}

func newProjectCreationAppWeb(t *testing.T, a *agentCreateFixture, username string) *projectCreationAppWeb {
	t.Helper()
	const prefix = "AGENTEAM_PROJECT_CREATION_WEB"
	dist, runtime := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence, inputHash := os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("frozen Project creation browser assets/runtime/input required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen Project creation browser index missing")
	}
	directory, err := os.MkdirTemp(runtime, "pcw-")
	agentAppRequire(t, err)
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(directory)
		}
	}()
	agentAppRequire(t, os.Chmod(directory, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	agentAppRequire(t, err)
	defer func() {
		if !published {
			_ = listener.Close()
		}
	}()
	base := &agentConfigurationWeb{a: a, mode: "create-and-enter", origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	w := &projectCreationAppWeb{agentEditWeb: &agentEditWeb{agentConfigurationWeb: base}}
	raw := jsonBytes(t, map[string]any{"mode": w.mode, "cookie": a.p2.base.ownerBrowser.cookie, "owner_id": a.p2.base.ownerBrowser.actor.Details().UserID, "username": username, "draft": map[string]string{"name": "Browser_Project", "description": "Created through the default App."}})
	agentAppRequire(t, os.WriteFile(filepath.Join(directory, "project-creation-material.json"), raw, 0600))
	clear(raw)
	published = true
	return w
}

func (w *projectCreationAppWeb) start(t *testing.T, address string) {
	t.Helper()
	w.upstream = &http.Transport{Proxy: nil, DisableKeepAlives: true}
	w.backend = httptest.NewUnstartedServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) { w.bridge(address, out, r) }))
	w.backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	w.backend.StartTLS()
	w.transport = w.backend.Client().Transport.(*http.Transport).Clone()
	if w.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("Project creation browser TLS verification disabled")
	}
	target, err := url.Parse(w.backend.URL)
	agentAppRequire(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport, proxy.ErrorLog = w.transport, log.New(io.Discard, "", 0)
	proxy.ModifyResponse = w.response
	proxy.ErrorHandler = func(out http.ResponseWriter, r *http.Request, err error) {
		w.fail(err)
		http.Error(out, "owned creation proxy failed", http.StatusBadGateway)
	}
	static := http.FileServer(http.Dir(w.dist))
	w.front = &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		w.frontWG.Add(1)
		defer w.frontWG.Done()
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if err := w.observe(r); err != nil {
				w.fail(err)
				http.Error(out, "owned creation observation failed", http.StatusInternalServerError)
				return
			}
			proxy.ServeHTTP(out, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(out, r)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		if info, err := os.Stat(filepath.Join(w.dist, clean)); err == nil && info.Mode().IsRegular() {
			static.ServeHTTP(out, r)
			return
		}
		http.ServeFile(out, r, filepath.Join(w.dist, "index.html"))
	})}
	go func() { w.serve <- w.front.Serve(w.listener) }()
}

func (w *projectCreationAppWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if r.Method != http.MethodPost || r.URL.Path != "/api/v1/projects" || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return errors.New("unexpected Project creation browser write")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (16<<10)+1))
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil || len(raw) > 16<<10 {
		return errors.New("Project creation request body did not close")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var input pc.CreateProjectRequest
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if agentWebDecode(raw, &input) != nil || input.Validate() != nil || key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 || input.Name != "Browser_Project" || input.Description != "Created through the default App." {
		return errors.New("browser changed the exact creation intent")
	}
	var count int
	if err = w.a.p2.base.raw.QueryRow(r.Context(), `SELECT count(*) FROM agenteam_project.projects WHERE id=$1`, input.ProjectID.String()).Scan(&count); err != nil || count != 0 {
		return errors.New("browser Project existed before its original POST")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.posts++
	if w.posts != 1 {
		return errors.New("browser retransmitted a Project creation")
	}
	w.input, w.key = input, key
	w.keys = append(w.keys, string(key))
	return nil
}

func (w *projectCreationAppWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	if r.Header.Get("Cache-Control") != "no-store" {
		return errors.New("original creation browser response omitted no-store")
	}
	w.mu.Lock()
	input := w.input
	w.mu.Unlock()
	if r.Request.Method == http.MethodPost {
		var result projectCreationAppResult
		if r.Request.URL.Path != "/api/v1/projects" || r.StatusCode != http.StatusCreated || agentWebDecode(raw, &result) != nil || result.State != pc.CreationReady || result.Project == nil || result.Operation != nil || result.StatusURL != "" || r.Header.Get("Location") != "" {
			return errors.New("original browser POST did not return ready")
		}
		p := result.Project
		if p.Validate() != nil || p.ID != input.ProjectID || p.OwnerUserID.String() != w.a.p2.base.ownerBrowser.actor.Details().UserID || p.Name != input.Name || p.Description != input.Description || p.Lifecycle != pc.Active || p.Version != 1 {
			return errors.New("original browser ready response changed intent or Owner")
		}
		w.mu.Lock()
		w.ready = bytes.Clone(raw)
		w.mu.Unlock()
	} else if r.Request.Method == http.MethodGet && input.ProjectID.Validate() == nil {
		path := r.Request.URL.Path
		if path == "/api/v1/projects/resolve" || path == variableHTTPPath(input.ProjectID, "") {
			var project pc.ProjectRef
			if r.StatusCode != http.StatusOK || json.Unmarshal(raw, &project) != nil || project.ID != input.ProjectID || project.OwnerUserID.String() != w.a.p2.base.ownerBrowser.actor.Details().UserID || project.Name != input.Name || project.Description != input.Description {
				return errors.New("browser navigation lost the actual created Project")
			}
			w.mu.Lock()
			if path == "/api/v1/projects/resolve" {
				w.resolves++
			} else {
				w.gets++
			}
			w.mu.Unlock()
		}
	}
	proof := map[string]any{"input_hash": w.inputHash, "request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true}
	encoded, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(w.evidence, 0700); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(w.evidence, "project-creation-create-and-enter-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600); err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func (w *projectCreationAppWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "project-creation.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	const prefix = "AGENTEAM_PROJECT_CREATION_WEB"
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_CASE="+w.mode, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err = cmd.Start(); err != nil {
		t.Fatal("owned Project creation browser did not start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	joined := false
	defer func() {
		if !joined {
			_ = cmd.Cancel()
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Env = nil
	}()
	err = <-done
	joined = true
	t.Logf("Project creation Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	w.mu.Lock()
	tokens := append([]string(nil), w.keys...)
	w.mu.Unlock()
	tokens = append(tokens, w.a.p2.base.ownerBrowser.cookie, w.a.p2.base.ownerBrowser.csrf)
	safe := output.String()
	for _, token := range tokens {
		if token != "" {
			safe = strings.ReplaceAll(safe, token, "[redacted]")
		}
	}
	t.Log(safe)
	if err != nil {
		t.Fatal("original Project creation browser failed")
	}
}

func (w *projectCreationAppWeb) verify(t *testing.T, ctx context.Context, process string) {
	t.Helper()
	// close() above joined every front and TLS handler before these reads.
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure != nil {
		t.Fatal("Project creation original HTTP observation failed", w.failure)
	}
	if w.posts != 1 || len(w.ready) == 0 || w.resolves < 1 || w.gets < 1 {
		t.Fatal("browser did not complete the unique POST and current Project navigation")
	}
	for _, tail := range w.tails {
		if !tail.returned {
			t.Fatal("Project creation TLS handler did not actually return")
		}
	}
	publication := projectCreationAppPublished(t, w.a, ctx, w.input.ProjectID, w.key, process)
	if publication.creation == "" {
		t.Fatal("browser did not publish a real creation")
	}
	raw, err := os.ReadFile(filepath.Join(w.directory, "created.json"))
	agentAppRequire(t, err)
	var observed struct {
		Status int             `json:"status"`
		Result json.RawMessage `json:"result"`
	}
	agentAppRequire(t, agentWebDecode(raw, &observed))
	var want, got projectCreationAppResult
	agentAppRequire(t, agentWebDecode(w.ready, &want))
	agentAppRequire(t, agentWebDecode(observed.Result, &got))
	if observed.Status != http.StatusCreated || !bytes.Equal(jsonBytes(t, got), jsonBytes(t, want)) {
		t.Fatal("browser result differs from the original verified response")
	}
}
