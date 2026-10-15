//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

const composerPrompt = "Discuss the original release notes."
const composerTypedText = "Review these original sources: "
const composerFirstReply = "The original release needs a reversible rollout."
const composerReplacementReply = "The revised release keeps the rollback available."
const composerFinalReply = "I reviewed the typed references and historical reply."
const composerTitle = "Composer discussion"
const composerSummary = "Typed references and the original file were reviewed."
const composerURL = "https://example.test/composer-reference"
const composerUploadText = "Original browser upload: release notes.\n中文文件正文。\n"

type composerArtifact struct {
	Reference ac.ArtifactRef `json:"artifact_ref"`
	Kind      ac.Kind        `json:"kind"`
	Name      string         `json:"name"`
	CreatedBy string         `json:"created_by_user_id"`
	Object    struct {
		Media string     `json:"media_type"`
		Size  f.Progress `json:"byte_size"`
		SHA   f.Digest   `json:"sha256"`
	} `json:"object"`
}

type composerHTTPRecord struct {
	method, path, key string
	body, response    []byte
	status            int
}
type composerRequestKey struct{}

// This observer changes only delivery after a complete original success body.
// It creates no domain facts and never repeats a mutation on behalf of the UI.
type composerWebObservation struct {
	mu                                sync.Mutex
	v                                 *executionSchedulerApp
	prefix                            string
	refs                              []c.Reference
	records                           []*composerHTTPRecord
	started                           c.Receipt
	uploaded                          composerArtifact
	droppedUpload, droppedSend        bool
	checkpointStarted, checkpointDone bool
	detail                            c.Detail
	failure                           string
}

func (o *composerWebObservation) modify(response *http.Response) error {
	record, _ := response.Request.Context().Value(composerRequestKey{}).(*composerHTTPRecord)
	if record == nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closed := response.Body.Close()
	if err != nil || closed != nil || len(raw) > 1<<20 || response.ContentLength >= 0 && response.ContentLength != int64(len(raw)) {
		o.mu.Lock()
		o.failure = "original response incomplete"
		o.mu.Unlock()
		return errors.New("original composer response incomplete")
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	o.mu.Lock()
	defer o.mu.Unlock()
	record.status, record.response = response.StatusCode, append([]byte(nil), raw...)
	if response.StatusCode != http.StatusOK {
		return nil
	}
	if record.method == http.MethodPost && record.path == o.prefix+"/meetings" {
		if json.Unmarshal(raw, &o.started) != nil || o.started.MeetingID.Validate() != nil || o.started.TurnID == nil {
			o.failure = "original Start receipt invalid"
			return errors.New(o.failure)
		}
	}
	if record.method == http.MethodPost && record.path == o.prefix+"/artifacts/upload-commands/complete" && !o.droppedUpload {
		var body struct {
			Artifact composerArtifact `json:"artifact"`
		}
		if json.Unmarshal(raw, &body) != nil || body.Artifact.Reference.ProjectID != o.v.a || body.Artifact.Reference.ArtifactID.Validate() != nil || body.Artifact.Reference.FileID.Validate() != nil || body.Artifact.Kind != ac.UserUpload {
			o.failure = "original upload receipt invalid"
			return errors.New(o.failure)
		}
		o.uploaded, o.droppedUpload = body.Artifact, true
		return errors.New("owned committed upload delivery interrupted")
	}
	if record.method == http.MethodPost && strings.HasSuffix(record.path, "/turns") && !o.droppedSend {
		var receipt c.Receipt
		if json.Unmarshal(raw, &receipt) != nil || receipt.MeetingID != o.started.MeetingID || receipt.TurnID == nil {
			o.failure = "original Send receipt invalid"
			return errors.New(o.failure)
		}
		o.droppedSend = true
		return errors.New("owned committed Send delivery interrupted")
	}
	return nil
}

func (o *composerWebObservation) record(request *http.Request) (*http.Request, error) {
	path := request.URL.Path
	mutation := request.Method == http.MethodPost || request.Method == http.MethodPut
	read := request.Method == http.MethodGet && strings.HasPrefix(path, o.prefix+"/artifacts/")
	if !strings.HasPrefix(path, o.prefix+"/") || !mutation && !read {
		return request, nil
	}
	record := &composerHTTPRecord{method: request.Method, path: path, key: request.Header.Get("Idempotency-Key")}
	if mutation {
		if request.Body == nil {
			return nil, errors.New("original command body absent")
		}
		raw, err := io.ReadAll(io.LimitReader(request.Body, (64<<10)+1))
		closed := request.Body.Close()
		if err != nil || closed != nil || len(raw) > 64<<10 {
			return nil, errors.New("original command body incomplete")
		}
		record.body = raw
		request.Body = io.NopCloser(bytes.NewReader(raw))
	}
	o.mu.Lock()
	if len(o.records) >= 256 {
		o.failure = "bounded browser request ledger overflow"
		o.mu.Unlock()
		return nil, errors.New("browser request limit")
	}
	o.records = append(o.records, record)
	o.mu.Unlock()
	return request.WithContext(context.WithValue(request.Context(), composerRequestKey{}, record)), nil
}

// The nonce checkpoint is an observation barrier before the UI purges Meeting.
// Its identities come from complete original HTTP receipts, not browser IDs.
func (o *composerWebObservation) checkpoint(ctx context.Context) error {
	o.mu.Lock()
	if o.checkpointStarted || o.failure != "" || !o.droppedSend || !o.droppedUpload {
		o.mu.Unlock()
		return errors.New("checkpoint admission")
	}
	o.checkpointStarted = true
	meetingID, uploaded := o.started.MeetingID, o.uploaded
	o.mu.Unlock()
	response, err := o.v.request(ctx, http.MethodGet, o.prefix+"/meetings/"+meetingID.String(), nil, "")
	if err != nil || response.status != 200 {
		return errors.New("checkpoint original Detail read")
	}
	var detail c.Detail
	if json.Unmarshal(response.body, &detail) != nil || detail.Session.ID != meetingID || detail.Session.ProjectID != o.v.a || detail.Session.Status != "active" || detail.Session.Deleting || detail.Session.Title == nil || *detail.Session.Title != composerTitle || detail.Session.Summary == nil || detail.Session.Summary.Version != 2 || detail.Session.Summary.PendingUpdate || detail.Session.Summary.Facts != composerSummary || len(detail.Turns) != 2 || len(detail.References) != 1 || detail.References[0].Kind != "file" || detail.References[0].ID != uploaded.Reference.ArtifactID.String() {
		return errors.New("checkpoint canonical Meeting state")
	}
	for _, turn := range detail.Turns {
		if turn.Status != "completed" || len(turn.Contributions) != 2 {
			return errors.New("checkpoint completed Turn")
		}
	}
	first, next := detail.Turns[0].Contributions, detail.Turns[1].Contributions
	if first[0].Message == nil || first[1].Message == nil || next[0].Message == nil || next[1].Message == nil || len(first[1].History) != 2 || first[1].History[0].Message == nil || first[1].History[1].Message == nil || first[1].Generation != 2 || first[1].History[0].Generation != 1 || first[1].ExecutionID == nil || first[1].History[0].RegenerateOf != nil || first[1].History[1].RegenerateOf == nil || *first[1].History[1].RegenerateOf != first[1].History[0].ExecutionID || first[1].History[1].ExecutionID != *first[1].ExecutionID || first[1].History[1].Message.ID != first[1].Message.ID || next[0].Message.ReplyTo == nil {
		return errors.New("checkpoint original message lineage")
	}
	text := func(message *c.MessageView) string {
		var out strings.Builder
		for _, node := range message.Content {
			if node.Type == "text" {
				out.WriteString(node.Text)
			}
		}
		return out.String()
	}
	if text(first[0].Message) != composerPrompt || text(first[1].History[0].Message) != composerFirstReply || text(first[1].Message) != composerReplacementReply || text(next[1].Message) != composerFinalReply || *next[0].Message.ReplyTo != first[1].History[0].Message.ID || *next[0].Message.ReplyTo == first[1].Message.ID || !strings.Contains(text(next[0].Message), composerTypedText) {
		return errors.New("checkpoint historical Reply or original text")
	}
	want := []c.Reference{o.refs[0], o.refs[1], {Kind: "link", ID: composerURL}, {Kind: "file", ID: uploaded.Reference.ArtifactID.String()}}
	position, mentions := 0, 0
	for _, node := range next[0].Message.Content {
		if node.Type == "text" {
			continue
		}
		if node.Type == "mention" {
			if mentions != 0 || position != 0 || node.ParticipantID != first[1].ParticipantID.String() || node.AgentID != "" {
				return errors.New("checkpoint original participant mention")
			}
			mentions++
			continue
		}
		if mentions != 1 || position >= len(want) || node.Reference == nil || node.Type != want[position].Kind || node.Reference.Kind != want[position].Kind || node.Reference.ID != want[position].ID {
			return errors.New("checkpoint typed node order or scope")
		}
		position++
	}
	if mentions != 1 || position != len(want) {
		return errors.New("checkpoint missing typed nodes")
	}
	var input []byte
	err = o.v.admin.QueryRow(ctx, `SELECT i.input_data FROM agenteam_meeting.launch_intents l JOIN agenteam_meeting.inputs i ON i.id=l.input_id WHERE l.contribution_id=$1 AND l.generation=1 AND l.attempt=1`, next[1].ID.String()).Scan(&input)
	var frozen meeting.FrozenContext
	if err != nil || json.Unmarshal(input, &frozen) != nil || frozen.Validate() != nil || bytes.Contains(input, []byte("MEETING_REFERENCE_PRIVATE_BODY")) || bytes.Contains(input, []byte(composerUploadText)) {
		return errors.New("checkpoint original captured context")
	}
	matched := false
	for _, m := range frozen.Messages {
		if m.Message.ID == next[0].Message.ID {
			a, _ := json.Marshal(m.Message)
			b, _ := json.Marshal(next[0].Message)
			matched = bytes.Equal(a, b)
		}
	}
	if !matched {
		return errors.New("checkpoint Send was not captured by original Execution")
	}
	response, err = o.v.request(ctx, http.MethodGet, o.prefix+"/artifacts/"+uploaded.Reference.ArtifactID.String(), nil, "")
	var metadata struct {
		Artifact composerArtifact `json:"artifact"`
	}
	if err != nil || response.status != 200 || json.Unmarshal(response.body, &metadata) != nil || metadata.Artifact != uploaded || uploaded.CreatedBy != o.v.actor.Details().UserID || uploaded.Name != "composer-notes.txt" || uploaded.Object.Media != "text/plain" || uploaded.Object.Size != f.Progress(len([]byte(composerUploadText))) || uploaded.Object.SHA.String() != fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(composerUploadText))) || uploaded.Reference.ArtifactID.String() == uploaded.Reference.FileID.String() {
		return errors.New("checkpoint original uploaded Metadata")
	}
	var uploadExact bool
	err = o.v.admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_artifact.artifacts a JOIN agenteam_artifact.upload_intents u ON u.id=a.id JOIN agenteam_artifact.commands c ON c.artifact_id=a.id WHERE a.project_id=$1::uuid AND a.id=$2::uuid AND a.kind='user_upload' AND u.state='consumed' AND c.path='upload' AND c.kind='user_upload' AND c.state='completed' AND (SELECT count(*) FROM agenteam_object.upload_attempts x WHERE x.object_id=a.object_id)=1)`, o.v.a.String(), uploaded.Reference.ArtifactID.String()).Scan(&uploadExact)
	if err != nil || !uploadExact {
		return errors.New("checkpoint original upload receipt or attempt")
	}
	o.mu.Lock()
	o.detail = detail
	o.checkpointDone = true
	o.mu.Unlock()
	return nil
}

func TestMeetingM3ComposerOwnerWeb(t *testing.T) {
	meetingM3ConfigurationPreflight(t)
	dist, harness, evidence, chromium := os.Getenv("AGENTEAM_MEETING_WEB_DIST"), os.Getenv("AGENTEAM_MEETING_COMPOSER_HARNESS"), os.Getenv("AGENTEAM_MEETING_WEB_EVIDENCE"), os.Getenv("AGENTEAM_AUTH_WEB_CHROMIUM")
	for _, path := range []string{dist, harness, evidence, chromium} {
		if !filepath.IsAbs(path) {
			t.Fatal("exact owned Composer browser paths required")
		}
	}
	for _, path := range []string{filepath.Join(dist, "index.html"), filepath.Join(harness, "meeting-composer.config.js"), filepath.Join(harness, "e2e/meeting-composer.spec.ts"), filepath.Join(harness, "node_modules/@playwright/test/cli.js"), chromium} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatal("frozen Composer assets, harness or executable absent")
		}
	}
	manifest, err := os.ReadFile(filepath.Join(harness, "node_modules/@playwright/test/package.json"))
	workAppCheck(t, "composer-playwright-manifest", err)
	var version struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(manifest, &version) != nil || version.Version != "1.56.1" {
		t.Fatal("locked Playwright 1.56.1 required")
	}
	private, err := os.MkdirTemp("/tmp", "agenteam-mc-web-")
	workAppCheck(t, "composer-private", err)
	t.Cleanup(func() { workAppCheck(t, "composer-private-cleanup", os.RemoveAll(private)) })
	evidence = filepath.Join(evidence, t.Name())
	workAppCheck(t, "composer-evidence", os.Mkdir(evidence, 0700))
	w, listener := newM1WorkflowWithOptions(t, nil, false)
	v := w.app
	meetingM3VerifyOwnedDatabase(t, v)
	firstSummary := string(workAppJSON(t, map[string]string{"title": composerTitle, "goals": "Review the original release", "decisions": "", "unresolved": "", "facts": "The initial discussion is complete."}))
	finalSummary := string(workAppJSON(t, map[string]string{"goals": "Review the original sources", "decisions": "Keep the original file", "unresolved": "", "facts": composerSummary}))
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{meetingM3Reply(t, 1, composerFirstReply), meetingM3Reply(t, 2, firstSummary), meetingM3Reply(t, 3, composerReplacementReply), meetingM3Reply(t, 4, composerFinalReply), meetingM3Reply(t, 5, finalSummary)}})
	workAppCheck(t, "composer-original-peer", err)
	defer func() {
		if t.Failed() {
			meetingM3FailureObservation(t, w, scenario)
		}
	}()
	model := meetingM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	agents := meetingM3CreateAgents(t, v, model)
	refs := meetingM3MetadataResources(t, v)
	v.store.releaseDirectory()
	o := &composerWebObservation{v: v, prefix: "/api/v1/projects/" + v.a.String(), refs: refs}
	defer func() {
		if t.Failed() {
			o.mu.Lock()
			t.Logf("MEETING_COMPOSER_FACTS checkpoint_started=%t checkpoint_done=%t original_requests=%d observation=%q", o.checkpointStarted, o.checkpointDone, len(o.records), o.failure)
			o.mu.Unlock()
		}
	}()
	checkpointPath := "/__m3-composer/checkpoint/" + guardID[struct{}](t).String()
	backend, err := url.Parse(v.address)
	workAppCheck(t, "composer-backend", err)
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = transport
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxy.ModifyResponse = o.modify
	proxy.ErrorHandler = func(out http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(out, "Owned response unavailable", http.StatusBadGateway)
	}
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		if r.URL.Path == checkpointPath {
			cookie, e := r.Cookie("agenteam_local_session")
			if r.Method != http.MethodPost || r.Header.Get("Origin") != w.origin || e != nil || cookie.Value != v.cookie || r.ContentLength > 0 {
				http.Error(out, "Invalid checkpoint", http.StatusBadRequest)
				return
			}
			if err := o.checkpoint(r.Context()); err != nil {
				o.mu.Lock()
				o.failure = err.Error()
				o.mu.Unlock()
				http.Error(out, "Original facts did not match", http.StatusConflict)
				return
			}
			out.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			request, err := o.record(r)
			if err != nil {
				http.Error(out, "Invalid original request", http.StatusBadRequest)
				return
			}
			proxy.ServeHTTP(out, request)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(out, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			skillWebServeAsset(out, r, dist)
			return
		}
		http.ServeFile(out, r, filepath.Join(dist, "index.html"))
	})}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				t.Error("Composer proxy did not join")
				_ = server.Close()
			}
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("Composer original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	defer stop()
	session, err := v.request(v.ctx, http.MethodGet, "/api/v1/session", nil, "")
	workAppCheck(t, "composer-original-session", err)
	var identity struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if session.status != 200 || json.Unmarshal(session.body, &identity) != nil || identity.User.Username == "" {
		t.Fatal("Composer original Human Session absent")
	}
	material := map[string]any{"origin": w.origin, "cookie": v.cookie, "project_id": v.a.String(), "route": "/" + identity.User.Username + "/runtime-app-a/meetings", "agent_name": "meeting-agent-1", "agent_id": agents[0].String(), "prompt": composerPrompt, "typed_text": composerTypedText, "first_reply": composerFirstReply, "replacement_reply": composerReplacementReply, "final_reply": composerFinalReply, "first_title": composerTitle, "final_title": composerTitle, "final_summary": composerSummary, "reference_url": composerURL, "task": map[string]string{"id": refs[0].ID, "label": refs[0].Label, "milestone": "Meeting reference milestone", "sprint": "Meeting reference sprint"}, "document": map[string]string{"id": refs[1].ID, "label": refs[1].Label}, "upload": map[string]string{"name": "composer-notes.txt", "media_type": "text/plain", "text": composerUploadText}, "checkpoint_path": checkpointPath}
	raw := workAppJSON(t, material)
	workAppCheck(t, "composer-material", os.WriteFile(filepath.Join(private, "composer-material.json"), raw, 0600))
	clear(raw)
	delete(material, "cookie")
	composerRunBrowser(t, w, harness, private, evidence, chromium, true)
	composerRunBrowser(t, w, harness, private, evidence, chromium, false)
	raw, err = os.ReadFile(filepath.Join(private, "composer-result.json"))
	workAppCheck(t, "composer-result", err)
	var result struct {
		Completed bool   `json:"completed"`
		Meeting   string `json:"meeting"`
		Artifact  string `json:"artifact_id"`
	}
	if len(raw) > 64<<10 || json.Unmarshal(raw, &result) != nil || !result.Completed {
		t.Fatal("Composer browser incomplete")
	}
	o.mu.Lock()
	done, failure, started, uploaded := o.checkpointDone, o.failure, o.started, o.uploaded
	o.mu.Unlock()
	if !done || failure != "" || result.Meeting != started.MeetingID.String() || result.Artifact != uploaded.Reference.ArtifactID.String() {
		t.Fatal("Composer original checkpoint or browser identity differs", failure)
	}
	composerVerifyRequests(t, o)
	composerVerifyDeleted(t, o)
	stop()
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "composer-runtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "composer-original-app-wait")
	workAppCheck(t, "composer-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.meetingRuntime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("Composer original owners did not join")
	}
	w.joinedPeer(t, scenario, 5, 5)
	meetingM3GuardReleased(t, v.workAppFixture)
	t.Log("MEETING_COMPOSER_WEB original_owner=true typed_nodes=true history_reply=true original_upload=true unknown_original_lookup=true pinned_file_preview=true archive_restore_delete=true original_owners_joined=true external_model=false network_commit_fault=false")
}

func composerVerifyRequests(t *testing.T, o *composerWebObservation) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	byPath := map[string][]*composerHTTPRecord{}
	for _, record := range o.records {
		byPath[record.method+" "+record.path] = append(byPath[record.method+" "+record.path], record)
	}
	one := func(method, path string) *composerHTTPRecord {
		values := byPath[method+" "+path]
		if len(values) != 1 || values[0].status != 200 || values[0].key == "" {
			t.Fatal("Composer original command count/status/key differs", method, path, len(values))
		}
		return values[0]
	}
	meetingPath := o.prefix + "/meetings/" + o.started.MeetingID.String()
	one(http.MethodPost, o.prefix+"/meetings")
	for _, pair := range [][2]string{{o.prefix + "/artifacts/upload-commands/complete", o.prefix + "/artifacts/upload-commands/lookup"}, {meetingPath + "/turns", meetingPath + "/turns/lookup"}} {
		original, lookup := one(http.MethodPost, pair[0]), one(http.MethodPost, pair[1])
		if original.key != lookup.key || !bytes.Equal(original.body, lookup.body) {
			t.Fatal("Composer Unknown lookup changed original intent")
		}
	}
	begin := one(http.MethodPost, o.prefix+"/artifacts/upload-commands/begin")
	payload := one(http.MethodPut, o.prefix+"/artifacts/upload-commands/payload")
	complete := one(http.MethodPost, o.prefix+"/artifacts/upload-commands/complete")
	if begin.key != payload.key || begin.key != complete.key || !bytes.Equal(begin.body, complete.body) || string(payload.body) != composerUploadText {
		t.Fatal("Composer original file or upload command changed")
	}
	controls := byPath[http.MethodPost+" "+meetingPath+"/controls"]
	actions := []string{"regenerate-contribution", "archive", "unarchive"}
	if len(controls) != len(actions) {
		t.Fatal("Composer control mutation duplicated", len(controls))
	}
	for n, record := range controls {
		var body struct {
			Action string `json:"action"`
		}
		if record.status != 200 || record.key == "" || json.Unmarshal(record.body, &body) != nil || body.Action != actions[n] {
			t.Fatal("Composer original control order differs")
		}
	}
	one(http.MethodPost, meetingPath+"/references")
	one(http.MethodPost, meetingPath+"/deletion")
	for _, lookup := range byPath[http.MethodPost+" "+meetingPath+"/deletion/lookup"] {
		original := byPath[http.MethodPost+" "+meetingPath+"/deletion"][0]
		if lookup.status != 200 || lookup.key != original.key || !bytes.Equal(lookup.body, original.body) {
			t.Fatal("Composer Delete lookup changed original intent")
		}
	}
	metadataPath := o.prefix + "/artifacts/" + o.uploaded.Reference.ArtifactID.String()
	reads := byPath[http.MethodGet+" "+metadataPath]
	content := byPath[http.MethodGet+" "+artifactOwnerHTTPReadPath(o.uploaded.Reference)]
	if len(reads) == 0 || len(content) == 0 {
		t.Fatal("Composer file preview bypassed original metadata/content reads")
	}
	for _, record := range reads {
		var value struct {
			Artifact composerArtifact `json:"artifact"`
		}
		if record.status != 200 || json.Unmarshal(record.response, &value) != nil || value.Artifact != o.uploaded {
			t.Fatal("Composer preview Metadata changed its original reference")
		}
	}
	for _, record := range content {
		var value struct {
			Artifact  composerArtifact `json:"artifact"`
			Text      string           `json:"text"`
			Offset    f.Progress       `json:"offset"`
			Next      f.Progress       `json:"next_offset"`
			Truncated bool             `json:"truncated"`
			Invalid   bool             `json:"invalid_utf8"`
		}
		if record.status != 200 || json.Unmarshal(record.response, &value) != nil || value.Artifact != o.uploaded || value.Text != composerUploadText || value.Offset != 0 || value.Next != f.Progress(len([]byte(composerUploadText))) || value.Truncated || value.Invalid {
			t.Fatal("Composer preview did not read original uploaded bytes")
		}
	}
}

func composerVerifyDeleted(t *testing.T, o *composerWebObservation) {
	t.Helper()
	v := o.v
	var remaining, executions, succeeded, calls, retired, released, files int
	var sameTransaction bool
	err := v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_meeting.sessions WHERE id=$2::text)+(SELECT count(*) FROM agenteam_meeting.messages WHERE meeting_id=$2::text)+(SELECT count(*) FROM agenteam_meeting.launch_intents WHERE meeting_id=$2::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text AND launch_request#>>'{trigger,meeting_id}'=$2::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text AND launch_request#>>'{trigger,meeting_id}'=$2::text AND status='succeeded'),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id=$1::uuid),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id=$1::uuid AND retired),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.project_id=$1::uuid AND l.released),
 (SELECT count(*) FROM agenteam_artifact.artifacts WHERE project_id=$1::uuid),
 EXISTS(SELECT 1 FROM agenteam_meeting.deletion_requests d JOIN agenteam_meeting.lifecycle_events e ON e.meeting_id=d.meeting_id AND e.event_type='meeting.deleted' JOIN agenteam_outbox.events o ON o.id::text=e.event_id JOIN agenteam_audit.audit_records a ON a.operation_id::text=d.id AND a.action='meeting.delete.completed' WHERE d.meeting_id=$2::text AND d.xmin=e.xmin AND d.xmin=o.xmin AND d.xmin=a.xmin AND d.deleted_records=(a.metadata->>'deleted_rows')::bigint AND d.deleted_records>0)`, v.a.String(), o.started.MeetingID.String()).Scan(&remaining, &executions, &succeeded, &calls, &retired, &released, &files, &sameTransaction)
	workAppCheck(t, "composer-original-delete-postimage", err)
	if remaining != 0 || executions != 3 || succeeded != 3 || calls != 5 || retired != 5 || released != 5 || files != 2 || !sameTransaction {
		t.Fatalf("Composer original postimage remaining=%d executions=%d succeeded=%d calls=%d retired=%d released=%d artifacts=%d same_tx=%t", remaining, executions, succeeded, calls, retired, released, files, sameTransaction)
	}
	response, err := v.request(v.ctx, http.MethodGet, o.prefix+"/meetings/"+o.started.MeetingID.String(), nil, "")
	workAppCheck(t, "composer-deleted-original-read", err)
	if response.status != 404 {
		t.Fatal("Composer deleted Meeting still readable")
	}
	response, err = v.request(v.ctx, http.MethodGet, o.prefix+"/artifacts/"+o.uploaded.Reference.ArtifactID.String(), nil, "")
	workAppCheck(t, "composer-preserved-original-file", err)
	var metadata struct {
		Artifact composerArtifact `json:"artifact"`
	}
	if response.status != 200 || json.Unmarshal(response.body, &metadata) != nil || metadata.Artifact != o.uploaded {
		t.Fatal("Composer deletion changed the independent uploaded Artifact")
	}
}

func composerRunBrowser(t *testing.T, w *m1Workflow, harness, private, evidence, chromium string, list bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.app.ctx, 50*time.Second)
	defer cancel()
	args := []string{filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "meeting-composer.config.js")}
	if list {
		args = append(args, "--list")
	}
	cmd := exec.CommandContext(ctx, "node", args...)
	cmd.Dir = harness
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "TMPDIR", "TMP", "TEMP", "NODE_COMPILE_CACHE", "PWTEST_CACHE_DIR", "XDG_CACHE_HOME", "DEBUG", "PWDEBUG", "DEV_CHAT_BASE_URL", "DEV_CHAT_API_KEY", "DEV_CHAT_MODEL", "PLAYWRIGHT_NO_COPY_PROMPT":
			continue
		}
		if !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for _, name := range []string{"TMPDIR", "TMP", "TEMP", "NODE_COMPILE_CACHE", "PWTEST_CACHE_DIR", "XDG_CACHE_HOME"} {
		path := filepath.Join(private, strings.ToLower(name))
		workAppCheck(t, "composer-child-cache", os.MkdirAll(path, 0700))
		cmd.Env = append(cmd.Env, name+"="+path)
	}
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM="+chromium, "AGENTEAM_MEETING_WEB_EVIDENCE="+evidence, "AGENTEAM_MEETING_COMPOSER_CASE=inline-original")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	workAppCheck(t, "composer-browser-start", cmd.Start())
	err := cmd.Wait()
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	cmd.Env = nil
	t.Logf("MEETING_COMPOSER_BROWSER list=%t actual_wait=true pid=%d success=%t", list, cmd.Process.Pid, err == nil)
	redacted := output.String()
	for _, secret := range []string{w.app.cookie, w.app.csrf} {
		if secret != "" {
			redacted = strings.ReplaceAll(redacted, secret, "[fixture-session-redacted]")
		}
	}
	name, pattern := "composer-browser-console.log", `(?m)\b1 passed\b`
	if list {
		name, pattern = "composer-browser-list.log", `Total: 1 test in 1 file`
	}
	workAppCheck(t, "composer-browser-console", os.WriteFile(filepath.Join(evidence, name), []byte(redacted), 0600))
	if err != nil || !regexp.MustCompile(pattern).Match(output.Bytes()) {
		t.Fatal("Composer browser did not pass its exact case; see private evidence console")
	}
}
