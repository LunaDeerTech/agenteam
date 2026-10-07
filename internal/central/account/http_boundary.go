package account

import (
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// HTTPBoundary shares Account's browser security rules without installing a
// second handler or accepting caller-supplied identities. Construction is pure.
type HTTPBoundary struct {
	core *Service
	csrf csrfBoundary
}

func NewHTTPBoundary(core *Service, publicOrigin string) (*HTTPBoundary, error) {
	if err := httpValidateCore(core); err != nil {
		return nil, err
	}
	b, err := csrfNewBoundary(publicOrigin)
	if err != nil {
		return nil, err
	}
	return &HTTPBoundary{core: core, csrf: b}, nil
}

func (b *HTTPBoundary) check(r *http.Request) error {
	if b == nil || b.core == nil || b.csrf.origin == "" {
		return fault(foundation.DependencyUnbound, nil)
	}
	if r == nil || r.URL == nil {
		return invalid()
	}
	if err := b.csrf.csrfCheck(r); err != nil {
		return err
	}
	if r.URL.Path != path.Clean(r.URL.Path) || strings.ContainsAny(r.URL.Path, "\\\x00") || r.URL.RawPath != "" {
		return invalid()
	}
	return nil
}

func (b *HTTPBoundary) CheckRequest(w http.ResponseWriter, r *http.Request) error {
	httpSecurityHeaders(w)
	return b.check(r)
}

// RequireHuman authenticates the current browser Session. It does not grant
// Project access; each reader must authorize the Actor in its own transaction.
func (b *HTTPBoundary) RequireHuman(r *http.Request) (identity.Actor, error) {
	if err := b.check(r); err != nil {
		return identity.Actor{}, err
	}
	actor, err := b.csrf.csrfSession(b.core, r, csrfUnsafe(r.Method))
	if err != nil {
		return identity.Actor{}, err
	}
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return identity.Actor{}, fault(foundation.Unauthenticated, nil)
	}
	return actor, nil
}

func (b *HTTPBoundary) RequireSystem(r *http.Request, intent identity.AccessIntent) (identity.Actor, error) {
	if err := b.check(r); err != nil {
		return identity.Actor{}, err
	}
	if intent != identity.Read && intent != identity.Mutate {
		return identity.Actor{}, invalid()
	}
	actor, err := b.csrf.csrfSession(b.core, r, csrfUnsafe(r.Method))
	if err != nil {
		return identity.Actor{}, err
	}
	grant, err := b.core.state().deps.Authority.AuthorizeSystem(r.Context(), foundation.Tx{}, actor, intent)
	if err != nil {
		return identity.Actor{}, err
	}
	if !grant.Matches(actor, identity.SystemScope(), intent) {
		return identity.Actor{}, fault(foundation.Forbidden, nil)
	}
	return actor, nil
}

func (b *HTTPBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	if b != nil && b.csrf.sessionName != "" && (hasFaultCode(err, foundation.Unauthenticated) || hasFaultCode(err, foundation.SessionRevoked)) {
		b.csrf.csrfClearSession(w)
	}
	// The old projector needs nonnil request/URL values. Keep the caller's
	// context (and request ID), but never project its path or query.
	copyRequest := http.Request{}
	if r != nil {
		copyRequest = *r
	}
	copyRequest.URL = &url.URL{Path: "/api/v1"}
	httpProblem(w, &copyRequest, err)
}
