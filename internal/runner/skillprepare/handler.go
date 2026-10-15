// Package skillprepare consumes an authenticated operation's short-lived GET
// grant. It does not authorize Skills, resolve Mounts or execute package scripts.
package skillprepare

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type prepareHandle interface {
	PrepareSkill(context.Context, filesystem.SkillPrepareRequest) (filesystem.SkillPrepareResult, error)
	Drain(context.Context) error
}
type Handler struct {
	ensure    func(context.Context, workspace.Identity) (prepareHandle, error)
	transport func() *http.Transport
}

func New(manager *workspace.Manager) (*Handler, error) {
	if runtime.GOOS != "linux" {
		return nil, filesystem.ErrUnsupported
	}
	if manager == nil {
		return nil, workspace.ErrInvalid
	}
	return &Handler{ensure: func(ctx context.Context, id workspace.Identity) (prepareHandle, error) {
		return manager.Ensure(ctx, id)
	}, transport: func() *http.Transport {
		// Keep the normal deployment proxy and system trust roots. Each operation
		// owns a fresh transport; no redirect, ambient cookie jar or connection reuse.
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.Proxy = http.ProxyFromEnvironment
		t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		t.DisableCompression = true
		t.DisableKeepAlives = true
		t.MaxConnsPerHost = 1
		t.ResponseHeaderTimeout = 15 * time.Second
		return t
	}}, nil
}
func (*Handler) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "skill_prepare_handler") }
func (*Handler) MarshalJSON() ([]byte, error) { return []byte(`"skill_prepare_handler"`), nil }
func (*Handler) LogValue() slog.Value         { return slog.StringValue("skill_prepare_handler") }
func (h *Handler) Accepts(r p.Request) bool {
	return h != nil && h.ensure != nil && h.transport != nil && r.OperationName == p.SkillPrepareOperation && r.OperationRevision == p.SkillPrepareRevision
}
func terminal(r p.Request, code p.Code) p.Response {
	outcome := p.Failure
	if code == p.CancelledCode || code == p.Timeout {
		outcome = p.Cancelled
	}
	message := code.SafeMessage()
	return p.Response{RequestID: r.RequestID, OperationID: r.OperationID, Outcome: outcome, Code: &code, SafeMessage: &message}
}
func failure(r p.Request, err error) p.Response {
	code := p.TransferFailed
	switch {
	case errors.Is(err, context.Canceled):
		code = p.CancelledCode
	case errors.Is(err, context.DeadlineExceeded):
		code = p.Timeout
	case errors.Is(err, filesystem.ErrOutside) || errors.Is(err, workspace.ErrOutside):
		code = p.PathOutsideWorkspace
	case errors.Is(err, filesystem.ErrConflict) || errors.Is(err, workspace.ErrConflict):
		code = p.Conflict
	case errors.Is(err, filesystem.ErrUnsupported) || errors.Is(err, workspace.ErrUnsupported):
		code = p.UnsupportedOperation
	}
	return terminal(r, code)
}

// Execute consumes its private payload and synchronously joins the HTTP body,
// per-call transport and Workspace handle before any terminal is returned.
// Cancellation cannot detach a download or a partly published directory.
func (h *Handler) Execute(ctx context.Context, r p.Request) (p.Response, error) {
	if !h.Accepts(r) {
		return terminal(r, p.UnsupportedOperation), nil
	}
	defer clear(r.Payload)
	if ctx == nil || !r.RequestID.Valid() || !r.OperationID.Valid() || !r.ExecutionID.Valid() || !r.ProjectID.Valid() || !r.AgentID.Valid() || !r.Mount.MountID.Valid() || !r.Mount.WorkspaceID.Valid() || r.Environment != nil || r.IdempotencyKey != nil {
		return terminal(r, p.InvalidRequest), nil
	}
	input, err := p.DecodeSkillPreparePayload(r.Payload)
	if err != nil {
		return terminal(r, p.InvalidRequest), nil
	}
	defer func() { input.Transfer.URL = ""; clear(input.Transfer.Headers); input.Transfer = p.SkillTransfer{} }()
	expires, _ := input.Transfer.ExpiresAt.Time()
	if r.Deadline != nil {
		deadline, e := r.Deadline.Time()
		if e != nil {
			return terminal(r, p.InvalidRequest), nil
		}
		if deadline.Before(expires) {
			expires = deadline
		}
	}
	owned, cancel := context.WithDeadline(ctx, expires)
	defer cancel()
	if err = owned.Err(); err != nil {
		return failure(r, err), nil
	}
	handle, err := h.ensure(owned, workspace.Identity{ProjectID: r.ProjectID, AgentID: r.AgentID, Name: input.WorkspaceName})
	if err != nil {
		if handle != nil {
			if e := handle.Drain(context.Background()); e != nil {
				return terminal(r, p.InternalError), nil
			}
		}
		return failure(r, err), nil
	}
	if handle == nil {
		return terminal(r, p.InternalError), nil
	}
	raw, downloadErr := h.download(owned, input)
	var files []filesystem.SkillFile
	if downloadErr == nil {
		files, downloadErr = unpack(owned, raw, input)
	}
	clear(raw)
	defer func() {
		for _, file := range files {
			clear(file.Data)
		}
	}()
	result := filesystem.SkillPrepareResult{State: filesystem.WriteNotCommitted}
	if downloadErr == nil {
		result, downloadErr = handle.PrepareSkill(owned, filesystem.SkillPrepareRequest{SkillID: string(input.SkillID), RevisionID: string(input.RevisionID), ArchiveSHA256: input.ArchiveSHA256, Files: files})
	}
	closeErr := handle.Drain(context.Background())
	if result.State == filesystem.WriteCommitted && downloadErr == nil && closeErr == nil && owned.Err() == nil {
		encoded, e := p.EncodeSkillPrepareResult(p.SkillPrepareResult{SkillID: input.SkillID, RevisionID: input.RevisionID, Revision: input.Revision, TransferID: input.Transfer.ID, ArchiveSHA256: input.ArchiveSHA256, RelativePath: result.RelativePath, Reused: result.Reused, Durable: result.Durable})
		if e == nil {
			return p.Response{RequestID: r.RequestID, OperationID: r.OperationID, Outcome: p.Success, Payload: encoded}, nil
		}
	}
	if result.State == filesystem.WriteCommitted || result.State == filesystem.WriteUnknown || closeErr != nil {
		message := p.UnknownMessage
		return p.Response{RequestID: r.RequestID, OperationID: r.OperationID, Outcome: p.Unknown, SafeMessage: &message}, nil
	}
	if downloadErr != nil {
		return failure(r, downloadErr), nil
	}
	if owned.Err() != nil {
		return failure(r, owned.Err()), nil
	}
	return terminal(r, p.InternalError), nil
}
func (h *Handler) download(ctx context.Context, input p.SkillPreparePayload) ([]byte, error) {
	transport := h.transport()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, input.Transfer.URL, nil)
	if err != nil {
		return nil, errPackage
	}
	request.Header = input.Transfer.Headers.Clone()
	request.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, err
	}
	expected, _ := strconv.ParseInt(string(input.ArchiveBytes), 10, 64)
	if response.StatusCode != http.StatusOK || response.ContentLength >= 0 && response.ContentLength != expected || response.Header.Get("Content-Encoding") != "" {
		closeErr := response.Body.Close()
		if closeErr != nil {
			return nil, closeErr
		}
		return nil, errPackage
	}
	raw, readErr := readBounded(ctx, response.Body, expected)
	closeErr := response.Body.Close()
	clear(request.Header)
	request.URL = nil
	if readErr != nil || closeErr != nil {
		clear(raw)
		return nil, errors.Join(readErr, closeErr)
	}
	return raw, nil
}
