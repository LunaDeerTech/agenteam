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
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

const configurationWriteBudget = 30 * time.Second
const configurationLookupBudget = 2 * time.Second

type configurationHTTPResource uint8

const (
	configurationNoResource configurationHTTPResource = iota
	configurationProviders
	configurationProvider
	configurationModels
	configurationModel
	configurationLookup
)

type configurationHTTPCommands interface {
	CreateProvider(context.Context, mc.CreateProviderRequest) (mc.CommandReceipt, error)
	UpdateProvider(context.Context, mc.UpdateProviderRequest) (mc.CommandReceipt, error)
	DeleteProvider(context.Context, mc.DeleteProviderRequest) (mc.CommandReceipt, error)
	CreateModel(context.Context, mc.CreateModelRequest) (mc.CommandReceipt, error)
	UpdateModel(context.Context, mc.UpdateModelRequest) (mc.CommandReceipt, error)
	DeleteModel(context.Context, mc.DeleteModelRequest) (mc.CommandReceipt, error)
	LookupCommand(context.Context, LookupCommandRequest) (CommandLookup, error)
}
type configurationHTTPBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (id.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type configurationHTTP struct {
	core     configurationHTTPCommands
	boundary configurationHTTPBoundary
}

// NewProjectConfigurationHTTPHandler captures the existing Model service and
// browser boundary. The service retains authority, locks and recovery ownership.
func NewProjectConfigurationHTTPHandler(core *Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	s := core.state()
	if s == nil || boundary == nil || nilPort(s.store) || s.authority.state() == nil ||
		nilPort(s.deps.Secret) || nilPort(s.deps.Audit) || nilPort(s.deps.Events) ||
		s.deps.Cursors.Validate() != nil || !s.deps.ConfigurationEvents.valid() {
		return nil, fault(f.DependencyUnbound)
	}
	a := s.authority.state()
	if nilPort(a.store) || !sameStore(s.store, a.store) || nilPort(a.auth.Sessions) ||
		nilPort(a.auth.System) || nilPort(a.auth.Projects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &configurationHTTP{core, boundary}, nil
}

func configurationPath(path string) (configurationHTTPResource, string, string) {
	const prefix = "/api/v1/projects/"
	if !strings.HasPrefix(path, prefix) {
		return configurationNoResource, "", ""
	}
	project, rest, ok := strings.Cut(strings.TrimPrefix(path, prefix), "/")
	if !ok || project == "" {
		return configurationNoResource, "", ""
	}
	switch rest {
	case "model-providers":
		return configurationProviders, project, ""
	case "models":
		return configurationModels, project, ""
	case "model-commands/lookup":
		return configurationLookup, project, ""
	}
	collection, target, ok := strings.Cut(rest, "/")
	if ok && target != "" && !strings.Contains(target, "/") {
		switch collection {
		case "model-providers":
			return configurationProvider, project, target
		case "models":
			return configurationModel, project, target
		}
	}
	return configurationNoResource, "", ""
}

// HandlesProjectConfigurationHTTPPath claims exact resource shapes for every
// method. The app composition delegates shared GET/HEAD to the accepted reader.
func HandlesProjectConfigurationHTTPPath(path string) bool {
	kind, _, _ := configurationPath(path)
	return kind != configurationNoResource
}
func configurationPattern(kind configurationHTTPResource) string {
	const prefix = "/api/v1/projects/{project_id}/"
	switch kind {
	case configurationProviders:
		return prefix + "model-providers"
	case configurationProvider:
		return prefix + "model-providers/{provider_id}"
	case configurationModels:
		return prefix + "models"
	case configurationModel:
		return prefix + "models/{model_id}"
	case configurationLookup:
		return prefix + "model-commands/lookup"
	}
	return "unknown_route"
}
func configurationMethod(kind configurationHTTPResource, method string) (string, bool) {
	switch kind {
	case configurationProviders, configurationModels:
		return "GET, HEAD, POST", method == http.MethodPost
	case configurationProvider, configurationModel:
		return "DELETE, GET, HEAD, PUT", method == http.MethodPut || method == http.MethodDelete
	case configurationLookup:
		return "POST", method == http.MethodPost
	}
	return "", false
}

// The capability adapter is controller-only. Business writes retain the
// original tracked writer and its stateOf/committed/code ownership chain.
type configurationIOWriter struct {
	http.ResponseWriter
	read, write func(time.Time) error
	flush       func() error
}

func (w *configurationIOWriter) prepare() bool {
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
func configurationSafeDeadline(call func(time.Time) error, at time.Time) (err error) {
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
func (w *configurationIOWriter) SetReadDeadline(at time.Time) error {
	return configurationSafeDeadline(w.read, at)
}
func (w *configurationIOWriter) SetWriteDeadline(at time.Time) error {
	return configurationSafeDeadline(w.write, at)
}
func (w *configurationIOWriter) FlushError() error {
	if w.flush == nil {
		return http.ErrNotSupported
	}
	return w.flush()
}

type configurationRequestIO struct {
	controller   *http.ResponseController
	ctx          context.Context
	body         io.ReadCloser
	closed       bool
	closeErr     error
	stop         func() bool
	callbackDone chan struct{}
	callbackErr  error // Only consumed after stop succeeds or callbackDone joins.
}

func (b *configurationRequestIO) deadlines(at time.Time) error {
	// Both safe receivers run even when the first one fails or panics.
	r := b.controller.SetReadDeadline(at)
	w := b.controller.SetWriteDeadline(at)
	return errors.Join(r, w)
}
func (b *configurationRequestIO) start() error {
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
func (b *configurationRequestIO) closeBody() (err error) {
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
func (b *configurationRequestIO) finish(normal bool) error {
	var abortErr, resetErr error
	if !normal {
		abortErr = b.deadlines(time.Now())
	}
	closeErr := b.closeBody()
	if b.stop != nil && !b.stop() {
		<-b.callbackDone
	}
	var ctxErr error
	if configurationExpired(b.ctx) {
		ctxErr = context.DeadlineExceeded
	}
	if normal && closeErr == nil && b.callbackErr == nil && ctxErr == nil {
		resetErr = b.deadlines(time.Time{})
	}
	return errors.Join(abortErr, closeErr, b.callbackErr, ctxErr, resetErr)
}
func configurationExpired(ctx context.Context) bool {
	deadline, ok := ctx.Deadline()
	return ctx.Err() != nil || ok && !time.Now().Before(deadline)
}
func configurationCheckBudget(ctx context.Context) {
	if configurationExpired(ctx) {
		panic(http.ErrAbortHandler)
	}
}

func (h *configurationHTTP) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request == nil || request.URL == nil {
		panic(http.ErrAbortHandler)
	}
	kind, _, _ := configurationPath(request.URL.Path)
	request.Pattern = configurationPattern(kind)
	_, mutation := configurationMethod(kind, request.Method)
	duration := configurationLookupBudget
	if mutation && kind != configurationLookup {
		duration = configurationWriteBudget
	}
	ctx, cancel := context.WithTimeout(request.Context(), duration)
	defer cancel()
	r := request.WithContext(ctx)
	adapter := &configurationIOWriter{ResponseWriter: writer}
	budget := configurationRequestIO{controller: http.NewResponseController(adapter), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		err := budget.finish(normal)
		if panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !adapter.prepare() || budget.start() != nil {
		panic(http.ErrAbortHandler)
	}
	// This transparent writer preserves the original middleware's code and
	// committed state. The capability adapter is controller-only.
	w := meetingSummaryAbortWriter{ResponseWriter: writer}
	body, err := h.execute(w, r)
	configurationCheckBudget(ctx)
	if budget.closeBody() != nil {
		panic(http.ErrAbortHandler)
	}
	configurationCheckBudget(ctx)
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
	configurationCheckBudget(ctx)
	if budget.controller.Flush() != nil {
		panic(http.ErrAbortHandler)
	}
	configurationCheckBudget(ctx)
	normal = true
}
func (h *configurationHTTP) execute(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	configurationCheckBudget(r.Context())
	actor, err := h.boundary.RequireHuman(r)
	if err != nil {
		return nil, err
	}
	configurationCheckBudget(r.Context())
	if actor.Validate() != nil || actor.Details().Kind != id.Human {
		return nil, fault(f.Unauthenticated)
	}
	kind, rawProject, rawTarget := configurationPath(r.URL.Path)
	if kind == configurationNoResource {
		return nil, fault(f.NotFound)
	}
	allow, method := configurationMethod(kind, r.Method)
	if !method {
		w.Header().Set("Allow", allow)
		return nil, fault(f.MethodNotAllowed)
	}
	project, err := f.ParseID[id.Project](rawProject)
	if err != nil || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, fault(f.InvalidArgument)
	}
	scope, err := id.InProject(project)
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	key, err := httpCommandKey(r)
	if err != nil {
		return nil, err
	}
	meta := mc.CommandMeta{Actor: actor, Scope: scope, Key: key}
	input, err := configurationDecode(w, r, kind, rawTarget, meta)
	if err != nil {
		return nil, err
	}
	// Validate the complete typed projection before any service or lookup call.
	if input.validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	configurationCheckBudget(r.Context())
	if kind == configurationLookup {
		result, err := h.core.LookupCommand(r.Context(), LookupCommandRequest{Meta: meta, Command: input.kind})
		if err != nil {
			return nil, err
		}
		return configurationEncodeLookup(r.Context(), input.kind, result)
	}
	result, err := h.mutate(r.Context(), input)
	if err != nil {
		return nil, err
	}
	return configurationEncodeReceipt(r.Context(), input, result)
}
func (h *configurationHTTP) mutate(ctx context.Context, r configurationRequest) (mc.CommandReceipt, error) {
	switch r.kind {
	case "provider.create":
		return h.core.CreateProvider(ctx, mc.CreateProviderRequest{CommandMeta: r.meta, Input: *r.provider})
	case "provider.update":
		return h.core.UpdateProvider(ctx, mc.UpdateProviderRequest{CommandMeta: r.meta, ID: r.providerID, ExpectedVersion: r.expected, Input: *r.provider})
	case "provider.delete":
		return h.core.DeleteProvider(ctx, mc.DeleteProviderRequest{CommandMeta: r.meta, ID: r.providerID, ExpectedVersion: r.expected})
	case "model.create":
		return h.core.CreateModel(ctx, mc.CreateModelRequest{CommandMeta: r.meta, ProviderID: r.providerID, Input: *r.model})
	case "model.update":
		return h.core.UpdateModel(ctx, mc.UpdateModelRequest{CommandMeta: r.meta, ID: r.modelID, ExpectedVersion: r.expected, Input: *r.model})
	case "model.delete":
		return h.core.DeleteModel(ctx, mc.DeleteModelRequest{CommandMeta: r.meta, ID: r.modelID, ExpectedVersion: r.expected, Replacement: r.replacement})
	}
	return mc.CommandReceipt{}, fault(f.InvalidArgument)
}
