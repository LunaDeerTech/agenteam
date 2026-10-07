package model

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const projectCredentialWriteBudget = 30 * time.Second
const projectCredentialReadBudget = 2 * time.Second

type projectCredentialResource uint8

const (
	credentialNoResource projectCredentialResource = iota
	credentialCollection
	credentialDetail
	credentialLookup
)

type projectCredentialBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (id.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type projectCredentialHTTP struct {
	writes   sc.HumanWriteCommands
	boundary projectCredentialBoundary
}

// NewProjectCredentialHTTPHandler retains the root's existing Secret service
// and formal browser boundary. Constructors do not discover authority or do I/O.
func NewProjectCredentialHTTPHandler(writes sc.HumanWriteCommands, boundary *account.HTTPBoundary) (http.Handler, error) {
	if nilPort(writes) || boundary == nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &projectCredentialHTTP{writes, boundary}, nil
}
func projectCredentialPath(path string) (projectCredentialResource, string, string) {
	const prefix = "/api/v1/projects/"
	if !strings.HasPrefix(path, prefix) {
		return credentialNoResource, "", ""
	}
	project, rest, ok := strings.Cut(strings.TrimPrefix(path, prefix), "/")
	if !ok || project == "" {
		return credentialNoResource, "", ""
	}
	if rest == "model-credentials" {
		return credentialCollection, project, ""
	}
	if rest == "model-credential-commands/lookup" {
		return credentialLookup, project, ""
	}
	collection, target, ok := strings.Cut(rest, "/")
	if ok && collection == "model-credentials" && target != "" && !strings.Contains(target, "/") {
		return credentialDetail, project, target
	}
	return credentialNoResource, "", ""
}
func HandlesProjectCredentialHTTPPath(path string) bool {
	k, _, _ := projectCredentialPath(path)
	return k != credentialNoResource
}
func credentialPattern(k projectCredentialResource) string {
	const prefix = "/api/v1/projects/{project_id}/"
	switch k {
	case credentialCollection:
		return prefix + "model-credentials"
	case credentialDetail:
		return prefix + "model-credentials/{credential_id}"
	case credentialLookup:
		return prefix + "model-credential-commands/lookup"
	}
	return "unknown_route"
}

// The capability adapter is controller-only. Business writes retain the
// original tracked writer and its stateOf/committed/code ownership chain.
type credentialIOWriter struct {
	http.ResponseWriter
	read, write func(time.Time) error
	flush       func() error
}

func (w *credentialIOWriter) prepare() bool {
	current := w.ResponseWriter
	for depth := 0; depth < 64 && !nilPort(current); depth++ {
		if w.read == nil {
			if v, ok := current.(interface{ SetReadDeadline(time.Time) error }); ok {
				w.read = v.SetReadDeadline
			}
		}
		if w.write == nil {
			if v, ok := current.(interface{ SetWriteDeadline(time.Time) error }); ok {
				w.write = v.SetWriteDeadline
			}
		}
		if w.flush == nil {
			switch v := current.(type) {
			case interface{ FlushError() error }:
				w.flush = v.FlushError
			case http.Flusher:
				w.flush = func() error { v.Flush(); return nil }
			}
		}
		v, ok := current.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return w.read != nil && w.write != nil && w.flush != nil
		}
		current = v.Unwrap()
	}
	return false
}
func credentialSafeDeadline(call func(time.Time) error, at time.Time) (err error) {
	defer func() {
		if recover() != nil {
			err = http.ErrAbortHandler
		}
	}()
	if call == nil {
		return http.ErrNotSupported
	}
	return call(at)
}
func (w *credentialIOWriter) SetReadDeadline(at time.Time) error {
	return credentialSafeDeadline(w.read, at)
}
func (w *credentialIOWriter) SetWriteDeadline(at time.Time) error {
	return credentialSafeDeadline(w.write, at)
}
func (w *credentialIOWriter) FlushError() error {
	if w.flush == nil {
		return http.ErrNotSupported
	}
	return w.flush()
}

type credentialRequestIO struct {
	controller   *http.ResponseController
	ctx          context.Context
	body         io.ReadCloser
	closed       bool
	closeErr     error
	stop         func() bool
	callbackDone chan struct{}
	callbackErr  error // Only consumed after stop succeeds or callbackDone joins.
}

func (b *credentialRequestIO) deadlines(at time.Time) error {
	// Both safe receivers run even when the first one fails or panics.
	r := b.controller.SetReadDeadline(at)
	w := b.controller.SetWriteDeadline(at)
	return errors.Join(r, w)
}
func (b *credentialRequestIO) start() error {
	deadline, ok := b.ctx.Deadline()
	if !ok {
		return http.ErrNotSupported
	}
	if err := b.deadlines(deadline); err != nil {
		return err
	}
	b.callbackDone = make(chan struct{})
	b.stop = context.AfterFunc(b.ctx, func() { defer close(b.callbackDone); b.callbackErr = b.deadlines(time.Now()) })
	return nil
}
func (b *credentialRequestIO) closeBody() (err error) {
	if b.closed {
		return b.closeErr
	}
	b.closed = true
	defer func() {
		if recover() != nil {
			err = http.ErrAbortHandler
		}
		b.closeErr = err
	}()
	if b.body != nil {
		return b.body.Close()
	}
	return nil
}
func (b *credentialRequestIO) finish(normal bool) error {
	var abortErr, resetErr error
	if !normal {
		abortErr = b.deadlines(time.Now())
	}
	closeErr := b.closeBody()
	if b.stop != nil && !b.stop() {
		<-b.callbackDone
	}
	var ctxErr error
	if credentialExpired(b.ctx) {
		ctxErr = context.DeadlineExceeded
	}
	if normal && closeErr == nil && b.callbackErr == nil && ctxErr == nil {
		resetErr = b.deadlines(time.Time{})
	}
	return errors.Join(abortErr, closeErr, b.callbackErr, ctxErr, resetErr)
}
func credentialExpired(ctx context.Context) bool {
	deadline, ok := ctx.Deadline()
	return ctx.Err() != nil || ok && !time.Now().Before(deadline)
}
func credentialCheckBudget(ctx context.Context) {
	if credentialExpired(ctx) {
		panic(http.ErrAbortHandler)
	}
}

func (h *projectCredentialHTTP) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request == nil || request.URL == nil {
		panic(http.ErrAbortHandler)
	}
	kind, _, _ := projectCredentialPath(request.URL.Path)
	request.Pattern = credentialPattern(kind)
	duration := projectCredentialWriteBudget
	if kind == credentialLookup || request.Method == http.MethodGet || request.Method == http.MethodHead {
		duration = projectCredentialReadBudget
	}
	ctx, cancel := context.WithTimeout(request.Context(), duration)
	defer cancel()
	r := request.WithContext(ctx)
	adapter := &credentialIOWriter{ResponseWriter: writer}
	budget := credentialRequestIO{controller: http.NewResponseController(adapter), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		err := budget.finish(normal)
		if panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !adapter.prepare() {
		panic(http.ErrAbortHandler)
	}
	if budget.start() != nil {
		panic(http.ErrAbortHandler)
	}
	w := meetingSummaryAbortWriter{ResponseWriter: writer}
	body, err := h.execute(w, r)
	credentialCheckBudget(ctx)
	if budget.closeBody() != nil {
		panic(http.ErrAbortHandler)
	}
	credentialCheckBudget(ctx)
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
	credentialCheckBudget(ctx)
	if budget.controller.Flush() != nil {
		panic(http.ErrAbortHandler)
	}
	credentialCheckBudget(ctx)
	normal = true
}
func (h *projectCredentialHTTP) execute(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	credentialCheckBudget(r.Context())
	kind, rawProject, rawCredential := projectCredentialPath(r.URL.Path)
	allow := ""
	validMethod := false
	switch kind {
	case credentialCollection:
		allow = "POST"
		validMethod = r.Method == http.MethodPost
	case credentialDetail:
		allow = "DELETE, GET, HEAD, PUT"
		validMethod = r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodPut || r.Method == http.MethodDelete
	case credentialLookup:
		allow = "POST"
		validMethod = r.Method == http.MethodPost
	default:
		return nil, fault(f.NotFound)
	}
	if !validMethod {
		w.Header().Set("Allow", allow)
		return nil, fault(f.MethodNotAllowed)
	}
	actor, err := h.boundary.RequireHuman(r)
	if err != nil {
		return nil, err
	}
	credentialCheckBudget(r.Context())
	if actor.Validate() != nil || actor.Details().Kind != id.Human {
		return nil, fault(f.Unauthenticated)
	}
	project, err := f.ParseID[id.Project](rawProject)
	if err != nil || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, fault(f.InvalidArgument)
	}
	scope, _ := id.InProject(project)
	var ref sc.CredentialRef
	if kind == credentialDetail {
		credential, e := f.ParseID[sc.Credential](rawCredential)
		if e != nil {
			return nil, fault(f.InvalidArgument)
		}
		ref, _ = sc.NewCredentialRef(credential, scope)
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if err := credentialEmptyBody(r); err != nil {
			return nil, err
		}
		credentialCheckBudget(r.Context())
		metadata, err := h.writes.Metadata(r.Context(), actor, ref)
		if err != nil {
			return nil, err
		}
		return credentialEncodeMetadata(r.Context(), ref, metadata)
	}
	key, err := httpCommandKey(r)
	if err != nil {
		return nil, err
	}
	if kind == credentialLookup {
		request, err := credentialDecodeLookup(w, r, actor, scope, key)
		if err != nil {
			return nil, err
		}
		credentialCheckBudget(r.Context())
		observation, err := h.writes.LookupWriteCommand(r.Context(), request)
		if err != nil {
			return nil, err
		}
		return credentialEncodeObservation(r.Context(), request, observation)
	}
	mutation := sc.Create
	if r.Method == http.MethodPut {
		mutation = sc.Update
	} else if r.Method == http.MethodDelete {
		mutation = sc.Delete
	}
	input, err := credentialDecodeMutation(w, r, actor, scope, key, mutation, ref)
	if err != nil {
		return nil, err
	}
	defer input.Value.Destroy()
	credentialCheckBudget(r.Context())
	result, err := h.executeMutation(r.Context(), input)
	if err != nil {
		return nil, err
	}
	return credentialEncodeMutation(r.Context(), credentialLookupRequest(input), result)
}
func (h *projectCredentialHTTP) executeMutation(ctx context.Context, r sc.WriteRequest) (sc.MutationResult, error) {
	if r.Kind == sc.Create {
		return h.writes.ExecuteWrite(ctx, r)
	}
	lookup := credentialLookupRequest(r)
	observation, err := h.writes.LookupWriteCommand(ctx, lookup)
	if err != nil {
		return sc.MutationResult{}, err
	}
	if _, err = credentialObservationDTO(lookup, observation); err != nil {
		return sc.MutationResult{}, err
	}
	credentialCheckBudget(ctx)
	if !observation.Observed {
		metadata, beforeErr := h.writes.Metadata(ctx, r.Actor, r.Ref)
		if beforeErr == nil {
			if metadata.CredentialRef.Validate() != nil || !metadata.Purpose.Valid() || metadata.Version.Validate() != nil || !metadata.CredentialRef.Equal(r.Ref) {
				// A damaged successful projection is never a metadata race. Do
				// not let a later receipt observation admit a write candidate.
				return sc.MutationResult{}, unavailable(nil)
			} else if metadata.Purpose != sc.Model {
				beforeErr = fault(f.NotFound)
			} else if metadata.Version != r.ExpectedVersion {
				beforeErr = fault(f.VersionConflict)
			}
		}
		credentialCheckBudget(ctx)
		if beforeErr != nil {
			observation, err = h.writes.LookupWriteCommand(ctx, lookup)
			if err != nil {
				return sc.MutationResult{}, err
			}
			if _, err = credentialObservationDTO(lookup, observation); err != nil {
				return sc.MutationResult{}, err
			}
			if !observation.Observed {
				return sc.MutationResult{}, beforeErr
			}
		}
	}
	credentialCheckBudget(ctx)
	// A Read observation only protects Purpose/late-receipt metadata handling.
	// Every mutation success still passes the real ExecuteWrite's same-Tx Mutate
	// gate and full original material comparison, including historical replay.
	return h.writes.ExecuteWrite(ctx, r)
}
