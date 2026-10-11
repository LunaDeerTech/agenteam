package agenthttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	e "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	m "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

type runnerMountTestReader struct {
	calls    int
	page     f.Page[rc.ConfigurationCandidate]
	bindings c.AgentRunnerMounts
	err      error
}

func (p *runnerMountTestReader) ListAvailableRunners(_ context.Context, _ i.Actor, _ i.ProjectID, _ f.PageRequest) (f.Page[rc.ConfigurationCandidate], error) {
	p.calls++
	return p.page, p.err
}
func (p *runnerMountTestReader) GetRunnerMounts(_ context.Context, _ i.Actor, _ i.ProjectID, _ i.AgentID) (c.AgentRunnerMounts, error) {
	p.calls++
	return p.bindings, p.err
}

func TestAgentRunnerMountReadHTTPBoundary(t *testing.T) {
	p := directoryID[i.Project](4)
	a := directoryID[i.Agent](3)
	rid := directoryID[rc.Runner](41)
	port := &runnerMountTestReader{page: f.Page[rc.ConfigurationCandidate]{Items: []rc.ConfigurationCandidate{{ID: rid, Name: "Runner", Description: "safe", Version: 1, Status: rc.Offline}}}, bindings: c.AgentRunnerMounts{ProjectID: p, AgentID: a, AgentVersion: 2, Items: []c.RunnerMountConfigurationItem{{MountID: directoryID[i.Mount](42), RunnerID: rid, Workspace: "workspace", Name: "legacy-alias"}}}}
	boundary := &directoryTestBoundary{}
	h := &runnerMountHandler{port, boundary}
	paths := []string{directoryPrefix + p.String() + "/available-runners", directoryPrefix + p.String() + "/agents/" + a.String() + "/runner-mounts"}
	for _, path := range paths {
		get, abort := directoryServe(h, httptest.NewRequest("GET", path, nil))
		if abort || get.Code != 200 || get.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("current read", get.Code, abort)
		}
		head, abort := directoryServe(h, httptest.NewRequest("HEAD", path, nil))
		if abort || head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(get.Body.Len()) {
			t.Fatal("HEAD current representation")
		}
		for _, secret := range []string{"root_path", "credential", "hello"} {
			if strings.Contains(get.Body.String(), secret) {
				t.Fatal("sensitive projection")
			}
		}
	}
	for _, path := range []string{paths[0] + "?limit=51", paths[0] + "?limit=1&limit=2", paths[1] + "?cursor=abc", directoryPrefix + "available-runners", paths[1] + "/"} {
		before := port.calls
		w, abort := directoryServe(h, httptest.NewRequest("GET", path, nil))
		if abort || w.Code != 400 && w.Code != 404 || port.calls != before {
			t.Fatal("route/query admitted", path, w.Code)
		}
	}
	before := port.calls
	w, abort := directoryServe(h, httptest.NewRequest("POST", paths[0], nil))
	if abort || w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" || port.calls != before {
		t.Fatal("method gate")
	}
	boundary.authErr = f.NewFault(f.Unauthenticated, f.NotStarted)
	w, abort = directoryServe(h, httptest.NewRequest("GET", paths[0], nil))
	if abort || w.Code != 401 || port.calls != before {
		t.Fatal("authentication gate")
	}
	boundary.authErr = nil
	port.bindings.Items = nil
	w, abort = directoryServe(h, httptest.NewRequest("GET", paths[1], nil))
	if abort || w.Code != 503 || strings.Contains(w.Body.String(), `"items"`) {
		t.Fatal("partial/missing mapping projected")
	}
	port.page.Items[0].Status = "unknown"
	w, abort = directoryServe(h, httptest.NewRequest("GET", paths[0], nil))
	if abort || w.Code != 503 {
		t.Fatal("invalid safe candidate projected")
	}
}

func TestAgentRunnerMountHTTPReceiptIntentProof(t *testing.T) {
	legacy, err := c.DecodeAgentCreate([]byte(configurationCreateJSON()))
	if err != nil {
		t.Fatal(err)
	}
	d := legacy.Fields()
	d.InstallSkillEnabled = new(bool)
	d.AllowedMountIDs = nil
	decl := []c.RunnerMountDeclaration{{RunnerID: directoryID[rc.Runner](41), Workspace: "workspace"}}
	d.RunnerMounts = &decl
	request, err := c.NewAgentCreate(d)
	if err != nil {
		t.Fatal(err)
	}
	binding := []c.RunnerMountBinding{{MountID: directoryID[i.Mount](42), WorkspaceID: directoryID[m.Workspace](43), RunnerID: decl[0].RunnerID, Workspace: decl[0].Workspace}}
	legacyFields := d
	legacyFields.RunnerMounts = nil
	legacyFields.AllowedMountIDs = []i.MountID{binding[0].MountID}
	resolved, err := c.NewAgentCreate(legacyFields)
	if err != nil {
		t.Fatal(err)
	}
	config := configurationValue(t, resolved)
	receipt, err := c.NewAgentMutation(c.AgentMutationFields{Agent: config, Changed: true, EventIDs: []e.EventID{directoryID[e.EventIdentity](44)}, RunnerMounts: &binding})
	if err != nil {
		t.Fatal(err)
	}
	project := config.Fields().Core.ProjectID
	if !configurationReceiptMatches(receipt, project, request) {
		t.Fatal("exact new receipt refused")
	}
	fields := receipt.Fields()
	fields.RunnerMounts = nil
	without, err := c.NewAgentMutation(fields)
	if err != nil {
		t.Fatal(err)
	}
	if configurationReceiptMatches(without, project, request) || configurationReceiptMatches(receipt, project, resolved) {
		t.Fatal("legacy/new profile interchangeable")
	}
	wrong := append([]c.RunnerMountBinding{}, binding...)
	wrong[0].Workspace = "other"
	fields = receipt.Fields()
	fields.RunnerMounts = &wrong
	bad, err := c.NewAgentMutation(fields)
	if err != nil {
		t.Fatal(err)
	}
	if configurationReceiptMatches(bad, project, request) {
		t.Fatal("valid shape accepted wrong original workspace")
	}
	update, err := c.NewAgentUpdate(c.AgentUpdateFields{RunnerMounts: &decl})
	if err != nil {
		t.Fatal(err)
	}
	version := f.Version(1)
	meta := f.CommandMeta{RequestID: directoryID[f.Request](45), IdempotencyKey: "runner-mount-noop", ExpectedVersion: &version}
	fields = receipt.Fields()
	fields.Changed = false
	fields.EventIDs = []e.EventID{}
	noop, err := c.NewAgentMutation(fields)
	if err != nil {
		t.Fatal(err)
	}
	if !configurationUpdateReceiptMatches(noop, project, config.Fields().Core.ID, meta, update) {
		t.Fatal("new noop intent rejected")
	}
	fields.RunnerMounts = nil
	legacyNoop, err := c.NewAgentMutation(fields)
	if err != nil {
		t.Fatal(err)
	}
	if configurationUpdateReceiptMatches(legacyNoop, project, config.Fields().Core.ID, meta, update) {
		t.Fatal("new lookup accepted legacy proof")
	}
	if raw, err := json.Marshal(request); err != nil || strings.Contains(string(raw), "mount_id") {
		t.Fatal("generated identity entered original request")
	}
}

var _ http.Handler = (*runnerMountHandler)(nil)
