// Package httpapi owns Central's transport boundary, not domain authorization.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type Problem struct {
	Type        string                            `json:"type"`
	Title       string                            `json:"title"`
	Status      int                               `json:"status"`
	Detail      string                            `json:"detail"`
	Instance    string                            `json:"instance"`
	Code        foundation.Code                   `json:"code"`
	RequestID   foundation.ID[foundation.Request] `json:"request_id"`
	FieldErrors []foundation.FieldError           `json:"field_errors,omitempty"`
	RetryHint   string                            `json:"retry_hint,omitempty"`
	CommitState foundation.CommitState            `json:"commit_state"`
}

type problemKind struct {
	status        int
	title, detail string
}

var problemKinds = map[foundation.Code]problemKind{
	foundation.InvalidArgument:         {400, "Invalid argument", "The request is invalid."},
	foundation.CursorInvalid:           {400, "Invalid cursor", "Restart pagination with a valid query."},
	foundation.CursorStale:             {409, "Stale cursor", "The ordering changed. Restart pagination."},
	foundation.Unauthenticated:         {401, "Authentication required", "Authentication is required."},
	foundation.SessionRevoked:          {401, "Session revoked", "Sign in again."},
	foundation.Forbidden:               {403, "Forbidden", "This action is not permitted."},
	foundation.CSRFFailed:              {403, "CSRF validation failed", "The request could not be verified."},
	foundation.OriginDenied:            {403, "Origin denied", "The request origin is not permitted."},
	foundation.NotFound:                {404, "Not found", "The requested resource was not found."},
	foundation.MethodNotAllowed:        {405, "Method not allowed", "This method is not supported for this resource."},
	foundation.ResourceDeleted:         {410, "Resource deleted", "The requested resource has been deleted."},
	foundation.VersionConflict:         {409, "Version conflict", "The resource changed. Read it again before retrying."},
	foundation.IdempotencyKeyReused:    {409, "Idempotency key reused", "This key was already used for a different command."},
	foundation.InvalidState:            {409, "Invalid state", "The resource state does not permit this action."},
	foundation.AgentBusy:               {409, "Agent busy", "The agent is currently busy."},
	foundation.ResourceBusy:            {409, "Resource busy", "The resource is currently busy."},
	foundation.ProjectNotActive:        {409, "Project not active", "The project is not active."},
	foundation.ConfirmationStale:       {409, "Confirmation stale", "Review the current state and confirm again."},
	foundation.SchemaUnsupported:       {422, "Schema unsupported", "The schema is not supported."},
	foundation.CapabilityUnsupported:   {422, "Capability unsupported", "The capability is not supported."},
	foundation.RateLimited:             {429, "Rate limited", "The request limit has been reached."},
	foundation.DependencyUnbound:       {503, "Dependency unbound", "A required capability is not bound."},
	foundation.DependencyUnavailable:   {503, "Dependency unavailable", "A required dependency is unavailable."},
	foundation.CommitUnknown:           {503, "Commit unknown", "The outcome is unknown. Look up the result using the same command identity."},
	foundation.InternalError:           {500, "Internal error", "An internal error occurred."},
	foundation.PayloadTooLarge:         {413, "Payload too large", "The request body exceeds the size limit."},
	foundation.UnsupportedMediaType:    {415, "Unsupported media type", "Use application/json with UTF-8 and no content encoding."},
	foundation.ShuttingDown:            {503, "Shutting down", "The service is stopping."},
	foundation.ObjectPayloadMissing:    {503, "Object payload missing", "The stored content is unavailable."},
	foundation.ObjectIntegrityMismatch: {502, "Object integrity mismatch", "The stored content failed integrity verification."},
	foundation.RangeNotSatisfiable:     {416, "Range not satisfiable", "The requested byte range is outside the stored content."},
}

// WriteProblem projects a domain fault to a safe RFC 9457 response. It never uses
// err.Error(), caller-provided status codes, or diagnostic causes as wire content.
// Install WithRequestID outside Recover so all responses share the context ID.
func WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	if state := stateOf(w); state != nil && state.committed {
		state.abort()
		panic(http.ErrAbortHandler)
	}
	f := faultFrom(err)
	code := f.Code.Safe()
	kind := problemKinds[code]
	p := Problem{
		Type:  "urn:agenteam:problem:" + strings.ReplaceAll(strings.ToLower(string(code)), "_", "-"),
		Title: kind.title, Status: kind.status, Detail: kind.detail,
		Instance: safePath(r), Code: code, RequestID: RequestID(r.Context()),
		CommitState: f.CommitState.Safe(),
	}
	if f.Code.Known() {
		if f.SafeMessage != "" {
			p.Detail = f.SafeMessage
		}
		for _, field := range f.FieldErrors {
			if field.Validate() == nil {
				p.FieldErrors = append(p.FieldErrors, field)
			}
		}
		if validHint(f.RetryHint) {
			p.RetryHint = f.RetryHint
		}
	}
	if code == foundation.CommitUnknown {
		p.RetryHint = "lookup"
	}
	if p.RetryHint == "" && (code == foundation.VersionConflict || code == foundation.CursorStale) {
		p.RetryHint = "reread"
	}
	if p.RequestID.Validate() != nil {
		id, idErr := foundation.NewID[foundation.Request]()
		if idErr != nil {
			writeEntropyFailure(w)
			return
		}
		p.RequestID = id
	}
	body, encodeErr := json.Marshal(p)
	if encodeErr != nil {
		writeEntropyFailure(w)
		return
	}
	if state := stateOf(w); state != nil {
		state.code = code
	}
	w.Header().Set("X-Request-ID", p.RequestID.String())
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Del("Content-Encoding")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(p.Status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func faultFrom(err error) foundation.Fault {
	var pointer *foundation.Fault
	if errors.As(err, &pointer) && pointer != nil {
		return *pointer
	}
	return *foundation.NewFault(foundation.InternalError, foundation.Unknown)
}

func safePath(r *http.Request) string {
	if r.URL == nil || !strings.HasPrefix(r.URL.Path, "/") {
		return "/"
	}
	return (&url.URL{Path: r.URL.Path}).EscapedPath()
}

func validHint(s string) bool {
	if len(s) == 0 || len(s) > 64 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// Entropy failure cannot produce a valid request identity. Fail closed without
// inventing an ID or recursively attempting to render an identity-bearing DTO.
func writeEntropyFailure(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Del("Content-Length")
	w.Header().Del("X-Request-ID")
	w.WriteHeader(http.StatusInternalServerError)
}

// WriteJSON encodes a complete DTO before committing a response. Streaming
// handlers use the native ResponseWriter/ResponseController instead.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, dto any) error {
	body, err := json.Marshal(dto)
	if err != nil {
		fault := foundation.NewFault(foundation.InternalError, foundation.Unknown).WithCause(err)
		WriteProblem(w, r, fault)
		return fault
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method == http.MethodHead || status == http.StatusNoContent || status == http.StatusNotModified {
		return nil
	}
	if _, err := w.Write(body); err != nil {
		return foundation.NewFault(foundation.InternalError, foundation.Unknown).WithCause(err)
	}
	return nil
}
