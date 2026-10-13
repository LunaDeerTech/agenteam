package artifacthttp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

const contentJSON = `{"name":"note.md","description":"original description","media_type":"text/markdown","content":" A世界\n"}`
const contentKey = "artifact-original-key"

type commandCall struct {
	name           string
	v              ac.Invocation
	meta           f.CommandMeta
	display        ac.Display
	media, content string
}
type testCommands struct {
	calls                           []commandCall
	create                          ac.Metadata
	lookup                          ac.CreationLookupResult
	cleanup                         oc.CleanupState
	createErr, lookupErr, cancelErr error
	afterLookup                     func()
	beforeCreate                    func(context.Context)
	beforeCancel                    func(context.Context)
}

func (p *testCommands) CreateFromContent(ctx context.Context, v ac.Invocation, meta f.CommandMeta, display ac.Display, media, content string) (ac.Metadata, error) {
	p.calls = append(p.calls, commandCall{"create", v, meta, display, media, content})
	if p.beforeCreate != nil {
		p.beforeCreate(ctx)
	}
	return p.create, p.createErr
}
func (p *testCommands) LookupCreation(_ context.Context, v ac.Invocation, meta f.CommandMeta, display ac.Display, media, content string) (ac.CreationLookupResult, error) {
	p.calls = append(p.calls, commandCall{"lookup", v, meta, display, media, content})
	result, err := p.lookup, p.lookupErr
	if p.afterLookup != nil {
		p.afterLookup()
	}
	return result, err
}
func (p *testCommands) CancelCreation(ctx context.Context, v ac.Invocation, key f.IdempotencyKey) (oc.CleanupState, error) {
	p.calls = append(p.calls, commandCall{name: "cancel", v: v, meta: f.CommandMeta{IdempotencyKey: key}})
	if p.beforeCancel != nil {
		p.beforeCancel(ctx)
	}
	return p.cleanup, p.cancelErr
}
func contentMetadata(t *testing.T) ac.Metadata {
	t.Helper()
	sum := sha256.Sum256([]byte(" A世界\n"))
	return metadataWith(t, func(d *ac.MetadataDetails) {
		d.Name = "note.md"
		d.Description = "original description"
		d.Object.MediaType = "text/markdown"
		d.Object.ByteSize = f.Progress(len(" A世界\n"))
		d.Object.SHA256, _ = f.ParseDigest("sha256:" + hex.EncodeToString(sum[:]))
	})
}
func contentHandler(t *testing.T) (*handler, *testBoundary, *testCommands) {
	t.Helper()
	h, b, _ := testHandler()
	p := &testCommands{create: contentMetadata(t), lookup: ac.CreationLookupResult{State: ac.CreationNotObserved}, cleanup: oc.CleanupCompleted}
	h.commands = p
	return h, b, p
}
func contentPath(action string) string {
	if action == "create-content" {
		return testPath(false) + "/" + action
	}
	return testPath(false) + "/creation-commands/" + action
}
func contentRequest(action, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, contentPath(action), strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", contentKey)
	return r
}
func commandResponse(t *testing.T, h *handler, r *http.Request) *testWriter {
	t.Helper()
	w := newTestWriter()
	if serveTest(h, r, w) {
		t.Fatal("unexpected HTTP abort")
	}
	return w
}

func TestArtifactHTTPContentCommands(t *testing.T) {
	h, b, p := contentHandler(t)
	var attempts []ac.CallAttemptID
	for _, action := range []string{"create-content", "lookup", "cancel"} {
		t.Run(action, func(t *testing.T) {
			r := contentRequest(action, contentJSON)
			r.Header.Set("X-Request-ID", testID[f.Request](90).String())
			w := commandResponse(t, h, r)
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("command rejected", w.Code)
			}
			call := p.calls[len(p.calls)-1]
			d := call.v.Details()
			if d.Actor.Details() != testActor().Details() || d.ProjectID != testReference().ProjectID || d.AttemptID.Validate() != nil || d.AttemptID.String() == call.meta.RequestID.String() || d.ExecutionID != "" || d.OperationID != "" || d.ToolID != "" || d.ToolCallID != "" {
				t.Fatal("untrusted invocation or trace reused")
			}
			if call.meta.IdempotencyKey != contentKey || call.meta.RequestID.Validate() != nil || call.meta.ExpectedVersion != nil || call.display != (ac.Display{Name: "note.md", Description: "original description"}) || call.media != "text/markdown" || call.content != " A世界\n" {
				t.Fatal("original intent was rewritten")
			}
			attempts = append(attempts, d.AttemptID)
			if strings.Contains(w.Body.String(), contentKey) || strings.Contains(w.Body.String(), p.create.Details().Object.ID.String()) || strings.Contains(w.Body.String(), " A世界") {
				t.Fatal("private material exposed")
			}
		})
	}
	if b.checks != 3 || b.auths != 3 || attempts[0] == attempts[1] || attempts[1] == attempts[2] || attempts[0] == attempts[2] {
		t.Fatal("each POST needs current Human and independent attempt")
	}
	for _, state := range []ac.CreationState{ac.CreationNotObserved, ac.CreationPending, ac.CreationCommitted, ac.CreationCancelRequested, ac.CreationCancelled} {
		t.Run("lookup-"+string(state), func(t *testing.T) {
			h, _, p := contentHandler(t)
			p.lookup.State = state
			if state == ac.CreationCommitted {
				p.lookup.Artifact = &p.create
			}
			w := commandResponse(t, h, contentRequest("lookup", contentJSON))
			var out struct {
				State    string          `json:"state"`
				Artifact json.RawMessage `json:"artifact"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.State != string(state) || (string(out.Artifact) != "null") != (state == ac.CreationCommitted) || len(p.calls) != 1 || p.calls[0].name != "lookup" {
				t.Fatal("lookup state or unexpected side effect")
			}
		})
	}
	for _, state := range []ac.CreationState{ac.CreationPending, ac.CreationCancelRequested, ac.CreationCancelled} {
		t.Run("cancel-"+string(state), func(t *testing.T) {
			h, _, p := contentHandler(t)
			p.lookup.State = state
			if state == ac.CreationPending {
				p.cleanup = oc.CleanupPending
			}
			w := commandResponse(t, h, contentRequest("cancel", contentJSON))
			want := `{"state":"cancelled","cleanup":"completed"}`
			if p.cleanup == oc.CleanupPending {
				want = `{"state":"cancel_requested","cleanup":"pending"}`
			}
			if w.Code != 200 || w.Body.String() != want || len(p.calls) != 2 || p.calls[0].name != "lookup" || p.calls[1].name != "cancel" || p.calls[1].meta.IdempotencyKey != p.calls[0].meta.IdempotencyKey || p.calls[1].v.Details().Actor.Details() != p.calls[0].v.Details().Actor.Details() || p.calls[1].v.Details().ProjectID != p.calls[0].v.Details().ProjectID || p.calls[1].v.Details().AttemptID != p.calls[0].v.Details().AttemptID {
				t.Fatal("cancel escaped matched original invocation")
			}
		})
	}
}

func TestArtifactHTTPContentCancellationGate(t *testing.T) {
	t.Run("missing-lookup-then-different-create-does-not-cancel", func(t *testing.T) {
		h, _, p := contentHandler(t)
		// This local interleaving deliberately gives the old key-only Cancel a
		// newly created, different command. Its mutation is observable if the
		// HTTP gate ever proceeds after the real Lookup result was absent.
		stored := map[string]string{}
		p.afterLookup = func() { stored[contentKey] = "different-content:pending" }
		p.beforeCancel = func(context.Context) { stored[contentKey] = "different-content:cancelled" }
		w := commandResponse(t, h, contentRequest("cancel", contentJSON))
		if w.Code != 200 || w.Body.String() != `{"state":"not_observed","cleanup":null}` || len(p.calls) != 1 || stored[contentKey] != "different-content:pending" {
			t.Fatal("missing observation cancelled a later different command")
		}
	})
	for _, mode := range []string{"lookup-error", "committed", "invalid-state", "metadata-on-pending", "publish-before-cancel"} {
		t.Run(mode, func(t *testing.T) {
			h, b, p := contentHandler(t)
			wantCalls := 1
			switch mode {
			case "lookup-error":
				p.lookupErr = f.NewFault(f.IdempotencyKeyReused, f.NotCommitted)
			case "committed":
				p.lookup = ac.CreationLookupResult{State: ac.CreationCommitted, Artifact: &p.create}
			case "invalid-state":
				p.lookup.State = "foreign-state"
			case "metadata-on-pending":
				p.lookup = ac.CreationLookupResult{State: ac.CreationPending, Artifact: &p.create}
			case "publish-before-cancel":
				p.lookup.State = ac.CreationPending
				p.afterLookup = func() { p.cancelErr = f.NewFault(f.InvalidState, f.NotCommitted) }
				wantCalls = 2
			}
			w := commandResponse(t, h, contentRequest("cancel", contentJSON))
			if w.Code == 200 || b.problem == nil || len(p.calls) != wantCalls || strings.Contains(w.Body.String(), `"cleanup"`) {
				t.Fatal("unmatched or failed cancel became successful")
			}
		})
	}
}

func TestArtifactHTTPContentOriginalErrors(t *testing.T) {
	for _, action := range []string{"create-content", "lookup", "cancel"} {
		for _, commit := range []f.CommitState{f.NotCommitted, f.Unknown} {
			t.Run(action+"-"+string(commit), func(t *testing.T) {
				h, b, p := contentHandler(t)
				cause := errors.New("private-content-marker")
				original := f.NewFault(f.DependencyUnavailable, commit).WithCause(cause)
				switch action {
				case "create-content":
					p.createErr = original
				case "lookup":
					p.lookup = ac.CreationLookupResult{State: ac.CreationCommitted, Artifact: &p.create}
					p.lookupErr = original
				case "cancel":
					p.lookup.State = ac.CreationPending
					p.cleanup = oc.CleanupCompleted
					p.cancelErr = original
				}
				w := commandResponse(t, h, contentRequest(action, contentJSON))
				var got *f.Fault
				if w.Code != 503 || !errors.Is(b.problem, cause) || !errors.As(b.problem, &got) || got.CommitState != commit || strings.Contains(w.Body.String(), "private-content-marker") || strings.Contains(w.Body.String(), `"artifact"`) || strings.Contains(w.Body.String(), `"cleanup"`) {
					t.Fatal("original error/outcome lost or private result published")
				}
			})
		}
	}
}

func TestArtifactHTTPContentActualReturnAfterCancel(t *testing.T) {
	for _, action := range []string{"create-content", "cancel"} {
		t.Run(action, func(t *testing.T) {
			h, _, p := contentHandler(t)
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan bool, 1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			hold := func(ctx context.Context) { close(entered); <-ctx.Done(); <-release }
			if action == "cancel" {
				p.lookup.State = ac.CreationPending
				p.beforeCancel = hold
			} else {
				p.beforeCreate = hold
			}
			r := contentRequest(action, contentJSON).WithContext(ctx)
			w := newTestWriter()
			go func() { done <- serveTest(h, r, w) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("original command did not enter")
			}
			cancel()
			select {
			case <-done:
				t.Fatal("HTTP returned before original command tail")
			default:
			}
			// Release is deferred above only for failed assertions; explicitly
			// hand it off so completion must come from the actual Service return.
			unblock()
			select {
			case aborted := <-done:
				if !aborted || w.Body.Len() != 0 {
					t.Fatal("expired write published response")
				}
			case <-time.After(time.Second):
				t.Fatal("HTTP did not join original command return")
			}
		})
	}
}

func TestArtifactHTTPContentProjectionBinding(t *testing.T) {
	for _, mode := range []string{"project", "kind", "name", "description", "media", "size", "digest", "creator", "source"} {
		t.Run(mode, func(t *testing.T) {
			h, _, p := contentHandler(t)
			d := p.create.Details()
			switch mode {
			case "project":
				d.Reference.ProjectID = testID[id.Project](31)
				d.Object.Scope, _ = id.InProject(d.Reference.ProjectID)
			case "kind":
				d.Kind = ac.UserUpload
			case "name":
				d.Name = "different.md"
			case "description":
				d.Description = "different"
			case "media":
				d.Object.MediaType = "text/plain"
			case "size":
				d.Object.ByteSize++
			case "digest":
				d.Object.SHA256 = testMetadata().Details().Object.SHA256
			case "creator":
				d.CreatedByUserID = testID[id.User](44).String()
			case "source":
				d.ExecutionID = testID[id.Execution](45).String()
			}
			var err error
			p.create, err = ac.NewMetadata(d)
			if err != nil {
				t.Fatal("invalid negative fixture", err)
			}
			for _, action := range []string{"create-content", "lookup"} {
				p.lookup = ac.CreationLookupResult{State: ac.CreationCommitted, Artifact: &p.create}
				w := commandResponse(t, h, contentRequest(action, contentJSON))
				if w.Code != 503 || strings.Contains(w.Body.String(), `"artifact"`) {
					t.Fatal("mismatched original metadata escaped")
				}
			}
		})
	}
}
