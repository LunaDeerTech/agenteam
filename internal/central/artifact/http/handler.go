// Package artifacthttp exposes bounded Artifact reads and content commands to
// current Human Owners, retaining the original Service and lifecycle owner.
package artifacthttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const projectPrefix = "/api/v1/projects/"
const requestBudget = 2 * time.Second

type artifactReader interface {
	ListArtifacts(context.Context, ac.Invocation, ac.ListFilter, f.PageRequest) (artifact.Page, error)
	ReadArtifact(context.Context, ac.Invocation, ac.ArtifactRef, ac.PreviewRequest) (ac.ReadResult, error)
}
type accountBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (id.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type handler struct {
	reader   artifactReader
	commands artifactCommands
	uploads  artifactUserUploads
	metadata artifactMetadataReader
	boundary accountBoundary
}

// NewHTTPHandler retains the original caller-owned service and boundary. It
// starts no workers and creates no replacement domain or lifecycle owner.
func NewHTTPHandler(service *artifact.Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &handler{reader: service, commands: service, uploads: service, metadata: service, boundary: boundary}, nil
}

type route struct {
	project, artifact, file string
	command                 string
	upload                  string
	valid, read, metadata   bool
}

func artifactRoute(path string) route {
	if !strings.HasPrefix(path, projectPrefix) {
		return route{}
	}
	parts := strings.Split(strings.TrimPrefix(path, projectPrefix), "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] == "artifacts" {
		return route{project: parts[0], valid: true}
	}
	if len(parts) == 3 && parts[0] != "" && parts[1] == "artifacts" && parts[2] == "create-content" {
		return route{project: parts[0], command: "create-content", valid: true}
	}
	if len(parts) == 4 && parts[0] != "" && parts[1] == "artifacts" && parts[2] == "creation-commands" && (parts[3] == "lookup" || parts[3] == "cancel") {
		return route{project: parts[0], command: parts[3], valid: true}
	}
	if len(parts) == 4 && parts[0] != "" && parts[1] == "artifacts" && parts[2] == "upload-commands" {
		switch parts[3] {
		case "begin", "lookup", "payload", "complete", "cancel":
			return route{project: parts[0], upload: parts[3], valid: true}
		}
	}
	if len(parts) == 6 && parts[0] != "" && parts[1] == "artifacts" && parts[2] != "" && parts[3] == "files" && parts[4] != "" && parts[5] == "content" {
		return route{project: parts[0], artifact: parts[2], file: parts[4], valid: true, read: true}
	}
	if len(parts) == 3 && parts[0] != "" && parts[1] == "artifacts" && parts[2] != "" {
		return route{project: parts[0], artifact: parts[2], valid: true, metadata: true}
	}
	return route{}
}

func HandlesPath(path string) bool { return artifactRoute(path).valid }

func (h *handler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	var route route
	if request.URL != nil {
		route = artifactRoute(request.URL.Path)
	}
	duration := requestBudget
	if route.upload == "payload" && request.Method == http.MethodPut {
		duration = userUploadPayloadBudget
	}
	ctx, cancel := context.WithTimeout(request.Context(), duration)
	defer cancel()
	request.Pattern = ""
	if route.valid {
		request.Pattern = projectPrefix + "{project_id}/artifacts"
		if route.upload != "" {
			request.Pattern += "/upload-commands/" + route.upload
		} else if route.read {
			request.Pattern += "/{artifact_id}/files/{file_id}/content"
		} else if route.metadata {
			request.Pattern += "/{artifact_id}"
		} else if route.command == "create-content" {
			request.Pattern += "/create-content"
		} else if route.command != "" {
			request.Pattern += "/creation-commands/" + route.command
		}
	}
	r := request.WithContext(ctx)
	if route.upload == "payload" && r.Body != nil {
		r.Body = &userUploadHTTPBody{ReadCloser: r.Body}
	}
	w = abortWriter{w}
	native := &nativeWriter{ResponseWriter: w}
	budget := requestIO{controller: http.NewResponseController(native), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		err := budget.finish(normal)
		if panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !native.prepare() || budget.start() != nil {
		panic(http.ErrAbortHandler)
	}
	body, err := h.execute(w, r, route)
	// The original Service has synchronously joined its readers, Audit and Store
	// before execute returns. Cancellation is not a substitute for that return.
	if expired(ctx) || budget.closeBody() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if err != nil {
		h.boundary.WriteProblem(w, r, err)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
	}
	if expired(ctx) || budget.controller.Flush() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h *handler) execute(w http.ResponseWriter, r *http.Request, route route) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	actor, err := h.boundary.RequireHuman(r)
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if actor.Validate() != nil || actor.Details().Kind != id.Human {
		return nil, f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	if httpapi.RequestID(r.Context()).Validate() != nil {
		return nil, invalidInput()
	}
	if !route.valid {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	if route.upload != "" {
		method := http.MethodPost
		if route.upload == "payload" {
			method = http.MethodPut
		}
		if r.Method != method {
			w.Header().Set("Allow", method)
			return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
		}
		return h.executeUserUpload(w, r, route, actor)
	}
	if route.command != "" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
		}
		return h.executeCommand(w, r, route, actor)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	project, err := f.ParseID[id.Project](route.project)
	if err != nil || project.String() != route.project {
		return nil, invalidInput()
	}
	for _, name := range []string{"Content-Encoding", "Range", "If-Range"} {
		if len(r.Header.Values(name)) != 0 {
			return nil, invalidInput()
		}
	}
	if err := emptyBody(r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	attempt, err := f.NewID[ac.CallAttempt]()
	if err != nil {
		return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(err)
	}
	v, err := ac.NewInvocation(ac.InvocationDetails{Actor: actor, ProjectID: project, AttemptID: attempt})
	if err != nil {
		return nil, err
	}
	if route.metadata {
		return h.executeMetadata(r, route, v)
	}
	if !route.read {
		filter, page, err := listQuery(r)
		if err != nil {
			return nil, err
		}
		out, err := h.reader.ListArtifacts(r.Context(), v, filter, page)
		if err != nil {
			return nil, err
		}
		return encodeList(r.Context(), project, filter, page, out.Items(), out.NextCursor())
	}
	artifactID, err := f.ParseID[ac.ArtifactEntity](route.artifact)
	if err != nil || artifactID.String() != route.artifact {
		return nil, invalidInput()
	}
	file, err := f.ParseID[ac.File](route.file)
	if err != nil || file.String() != route.file {
		return nil, invalidInput()
	}
	preview, err := readQuery(r)
	if err != nil {
		return nil, err
	}
	ref := ac.ArtifactRef{ProjectID: project, ArtifactID: artifactID, FileID: file}
	out, err := h.reader.ReadArtifact(r.Context(), v, ref, preview)
	if err != nil {
		return nil, err
	}
	return encodeRead(r.Context(), ref, preview, out)
}
