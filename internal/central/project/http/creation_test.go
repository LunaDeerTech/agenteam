package projecthttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type creationServices struct {
	create        func(context.Context, id.Actor, f.CommandMeta, pc.CreateProjectRequest) (pc.CreationResult, error)
	get           func(context.Context, id.Actor, pc.CreationID) (pc.CreationResult, error)
	creates, gets atomic.Int32
}

func (s *creationServices) CreateProject(ctx context.Context, actor id.Actor, meta f.CommandMeta, request pc.CreateProjectRequest) (pc.CreationResult, error) {
	s.creates.Add(1)
	if s.create != nil {
		return s.create(ctx, actor, meta, request)
	}
	p := wireProject()
	return pc.CreationResult{State: pc.CreationReady, Project: &p}, nil
}
func (s *creationServices) GetCreation(ctx context.Context, actor id.Actor, creation pc.CreationID) (pc.CreationResult, error) {
	s.gets.Add(1)
	if s.get != nil {
		return s.get(ctx, actor, creation)
	}
	return creationAccepted(pc.CreationInitializing), nil
}
func creationFixture() (*projectCreationHTTP, *handlerBoundary, *creationServices) {
	b, s := &handlerBoundary{}, &creationServices{}
	return &projectCreationHTTP{s, b}, b, s
}
func creationAccepted(state pc.CreationState) pc.CreationResult {
	op := pc.CreationOperation{ID: wireID[pc.Creation](), ProjectID: wireID[id.Project](), State: state, Version: 1, CreatedAt: wireInstant(), UpdatedAt: wireInstant()}
	if state == pc.CreationFailed {
		op.SafeReason = pc.ReasonOperationFailed
	}
	return pc.CreationResult{State: pc.CreationPending, Operation: &op}
}
func creationInput() string {
	return `{"project_id":"` + wireID[id.Project]().String() + `","name":"Project","description":"test"}`
}

func TestProjectCreationHTTPInputAndScope(t *testing.T) {
	t.Run("original-meta", func(t *testing.T) {
		h, _, s := creationFixture()
		s.create = func(_ context.Context, a id.Actor, m f.CommandMeta, p pc.CreateProjectRequest) (pc.CreationResult, error) {
			if !a.Equal(wireActor()) || pc.ValidateCommandMeta(pc.CreateCommand, m) != nil || m.IdempotencyKey != "original-key" || m.ExpectedVersion != nil || p.ProjectID != wireID[id.Project]() || p.Name != "Project" || p.Description != "test" {
				t.Fatal("original command changed")
			}
			return creationAccepted(pc.CreationAccepted), nil
		}
		w := newHandlerWriter()
		if updateServe(h, w, updateRequest("POST", projectsPath, creationInput())) || w.Code != 202 || s.creates.Load() != 1 {
			t.Fatal("valid original input rejected")
		}
	})
	for name, body := range map[string]string{
		"owner":            strings.TrimSuffix(creationInput(), "}") + `,"owner_user_id":"` + wireID[id.User]().String() + `"}`,
		"expected-version": strings.TrimSuffix(creationInput(), "}") + `,"expected_version":"1"}`,
		"missing-id":       `{"name":"Project"}`, "null-description": `{"project_id":"` + wireID[id.Project]().String() + `","name":"Project","description":null}`,
		"invalid-name": strings.Replace(creationInput(), `"Project"`, `"Project name"`, 1),
		"duplicate":    strings.TrimSuffix(creationInput(), "}") + `,"name":"Project"}`,
		"alias":        strings.Replace(creationInput(), `"name"`, `"Name"`, 1), "trailing": creationInput() + `{}`,
		"body-limit": `{"project_id":"` + wireID[id.Project]().String() + `","name":"Project","description":"` + strings.Repeat("x", creationBodyLimit) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			h, _, s := creationFixture()
			w := newHandlerWriter()
			if updateServe(h, w, updateRequest("POST", projectsPath, body)) || w.Code < 400 || s.creates.Load() != 0 {
				t.Fatal("invalid body reached domain")
			}
		})
	}
	t.Run("current-boundary", func(t *testing.T) {
		h, b, s := creationFixture()
		b.check = func(http.ResponseWriter, *http.Request) error { return f.NewFault(f.Forbidden, f.NotStarted) }
		w := newHandlerWriter()
		if updateServe(h, w, updateRequest("POST", projectsPath, creationInput())) || w.Code != 403 || b.auths.Load() != 0 || s.creates.Load() != 0 {
			t.Fatal("boundary bypass")
		}
	})
	t.Run("duplicate-key", func(t *testing.T) {
		h, _, s := creationFixture()
		r := updateRequest("POST", projectsPath, creationInput())
		r.Header.Add("Idempotency-Key", "second-key")
		w := newHandlerWriter()
		if updateServe(h, w, r) || w.Code != 400 || s.creates.Load() != 0 {
			t.Fatal("ambiguous intent accepted")
		}
	})
}

func TestProjectCreationHTTPResponsesAndRouting(t *testing.T) {
	for _, state := range []pc.CreationState{pc.CreationAccepted, pc.CreationInitializing, pc.CreationFailed} {
		t.Run(string(state), func(t *testing.T) {
			h, _, s := creationFixture()
			result := creationAccepted(state)
			s.create = func(context.Context, id.Actor, f.CommandMeta, pc.CreateProjectRequest) (pc.CreationResult, error) {
				return result, nil
			}
			s.get = func(_ context.Context, _ id.Actor, c pc.CreationID) (pc.CreationResult, error) {
				if c != result.Operation.ID {
					t.Fatal("status target changed")
				}
				return result, nil
			}
			location := creationPrefix + result.Operation.ID.String()
			for _, method := range []string{"POST", "GET", "HEAD"} {
				path, body := location, ""
				code := 200
				if method == "POST" {
					path, body, code = projectsPath, creationInput(), 202
				}
				w := newHandlerWriter()
				if updateServe(h, w, updateRequest(method, path, body)) || w.Code != code {
					t.Fatal("status projection")
				}
				if method == "POST" && w.Header().Get("Location") != location || method != "POST" && w.Header().Get("Location") != "" {
					t.Fatal("Location mismatch")
				}
				if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Length") == "" {
					t.Fatal("missing complete representation headers")
				}
				if method == "HEAD" {
					if w.Body.Len() != 0 {
						t.Fatal("HEAD body")
					}
					continue
				}
				v := wireObject(t, w.Body.Bytes())
				op := v["operation"].(map[string]any)
				if v["state"] != "accepted" || v["status_url"] != location || v["project"] != nil || op["state"] != string(state) || op["version"] != "1" {
					t.Fatal("lost pending/failed identity")
				}
			}
		})
	}
	t.Run("ready-current-project", func(t *testing.T) {
		h, _, s := creationFixture()
		p := wireProject()
		p.Name = "Renamed"
		p.NormalizedName = "renamed"
		p.Lifecycle = pc.Archived
		p.ArchivedAt = wirePtr(wireInstant())
		s.create = func(context.Context, id.Actor, f.CommandMeta, pc.CreateProjectRequest) (pc.CreationResult, error) {
			return pc.CreationResult{State: pc.CreationReady, Project: &p}, nil
		}
		w := newHandlerWriter()
		if updateServe(h, w, updateRequest("POST", projectsPath, creationInput())) || w.Code != 201 {
			t.Fatal("current completed replay rejected")
		}
		v := wireObject(t, w.Body.Bytes())
		if v["state"] != "ready" || v["operation"] != nil || v["status_url"] != nil || v["project"].(map[string]any)["name"] != "Renamed" {
			t.Fatal("ready projection")
		}
	})
	t.Run("route-ownership", func(t *testing.T) {
		for _, method := range []string{"GET", "HEAD"} {
			if HandlesCreationRequest(method, projectsPath) {
				t.Fatal("stole existing list")
			}
		}
		if !HandlesCreationRequest("POST", projectsPath) || !HandlesCreationRequest("GET", creationPrefix+wireID[pc.Creation]().String()) {
			t.Fatal("missed creation route")
		}
		for _, path := range []string{httpDetailPath, httpDetailPath + "/commands/lookup", httpDetailPath + "/delete", creationPrefix, creationPrefix + "a/b"} {
			if HandlesCreationRequest("POST", path) {
				t.Fatal("stole unrelated route")
			}
		}
		if h, e := NewCreationHTTPHandler(nil, &account.HTTPBoundary{}); h != nil || e == nil {
			t.Fatal("nil service")
		}
		if h, e := NewCreationHTTPHandler(&project.Service{}, nil); h != nil || e == nil {
			t.Fatal("nil boundary")
		}
	})
}

func TestProjectCreationHTTPRejectsMismatchedProjection(t *testing.T) {
	other, _ := f.ParseID[id.Project]("01900000-0000-7000-8000-000000000002")
	for _, kind := range []string{"other-project", "other-owner", "other-creation", "invalid-union"} {
		t.Run(kind, func(t *testing.T) {
			value := creationAccepted(pc.CreationAccepted)
			target, creation := wireID[id.Project](), wireID[pc.Creation]()
			switch kind {
			case "other-project":
				value.Operation.ProjectID = other
			case "other-owner":
				p := wireProject()
				p.OwnerUserID, _ = f.ParseID[id.User](other.String())
				value = pc.CreationResult{State: pc.CreationReady, Project: &p}
			case "other-creation":
				value.Operation.ID, _ = f.ParseID[pc.Creation](other.String())
			case "invalid-union":
				p := wireProject()
				value.Project = &p
			}
			if raw, url, err := encodeCreation(context.Background(), wireActor(), target, creation, value); err == nil || raw != nil || url != "" {
				t.Fatal("mismatched receipt exposed")
			}
		})
	}
}

func TestProjectCreationHTTPOriginalReturnAndUnknown(t *testing.T) {
	t.Run("fault-unchanged", func(t *testing.T) {
		h, _, s := creationFixture()
		original := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(errors.New("private-provider-text"))
		s.create = func(context.Context, id.Actor, f.CommandMeta, pc.CreateProjectRequest) (pc.CreationResult, error) {
			return pc.CreationResult{}, original
		}
		w := newHandlerWriter()
		if updateServe(h, w, updateRequest("POST", projectsPath, creationInput())) || w.Code != 503 {
			t.Fatal("unknown erased")
		}
		v := wireObject(t, w.Body.Bytes())
		if v["code"] != "COMMIT_UNKNOWN" || v["commit_state"] != "unknown" || strings.Contains(w.Body.String(), "private-provider-text") {
			t.Fatal("fault changed/leaked")
		}
	})
	t.Run("cancel-waits-original-call", func(t *testing.T) {
		h, _, s := creationFixture()
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var aborted bool
		s.create = func(context.Context, id.Actor, f.CommandMeta, pc.CreateProjectRequest) (pc.CreationResult, error) {
			close(entered)
			<-release
			return creationAccepted(pc.CreationAccepted), nil
		}
		w := newHandlerWriter()
		r := updateRequest("POST", projectsPath, creationInput()).WithContext(ctx)
		body := &handlerBody{Reader: strings.NewReader(creationInput())}
		r.Body = body
		t.Cleanup(func() {
			cancel()
			once.Do(func() { close(release) })
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("original handler did not return")
			}
		})
		go func() { aborted = updateServe(h, w, r); close(done) }()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("domain entry not reached")
		}
		cancel()
		select {
		case <-done:
			t.Fatal("handler abandoned original call")
		default:
		}
		once.Do(func() { close(release) })
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("original return missing")
		}
		if !aborted || w.Body.Len() != 0 || body.closes.Load() != 1 {
			t.Fatal("published after cancel or lost original body")
		}
	})
}

func TestProjectCreationHTTPDecodeRetainsValidation(t *testing.T) {
	var meta f.CommandMeta
	var input pc.CreateProjectRequest
	var err error
	httpapi.WithRequestID(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { meta, input, err = decodeCreation(w, r, "original-key") })).ServeHTTP(httptest.NewRecorder(), updateRequest("POST", projectsPath, `{"project_id":"`+wireID[id.Project]().String()+`","name":"Project"}`))
	if err != nil || pc.ValidateCommandMeta(pc.CreateCommand, meta) != nil || input.Validate() != nil || input.Description != "" {
		t.Fatal("optional description or real metadata lost", err)
	}
}
