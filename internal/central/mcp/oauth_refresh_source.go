package mcp

import (
	"context"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// These fields identify an already admitted caller. They are never accepted
// from a request DTO: require and read retain that caller's private witness.
type oauthRefreshCaller struct {
	user, session, command                 string
	execution, operation, attempt, request string
	runtime, startup                       string
}

func (c oauthRefreshCaller) valid() bool {
	human := c.user != "" && c.session != "" && c.command != "" && c.execution == "" && c.operation == "" && c.attempt == "" && c.request == "" && c.runtime == "" && c.startup == ""
	agent := c.user == "" && c.session == "" && c.command == "" && c.execution != "" && c.operation != "" && c.attempt != "" && c.request != "" && c.runtime != "" && c.startup == ""
	startup := c.user == "" && c.session == "" && c.command == "" && c.execution == "" && c.operation == "" && c.attempt == "" && c.request == "" && c.runtime == "" && c.startup != ""
	if !human && !agent && !startup {
		return false
	}
	for _, raw := range []string{c.user, c.session, c.execution, c.operation, c.attempt, c.request, c.runtime, c.startup} {
		if raw != "" {
			if _, err := f.ParseID[f.Request](raw); err != nil {
				return false
			}
		}
	}
	return c.command == "" || f.Digest(c.command).Validate() == nil
}

type oauthCredentialSource struct {
	service           *Service
	credential        string
	binding           RuntimeConnectionBinding
	connectionVersion f.Version
	caller            oauthRefreshCaller
	call              outbound.CallContext
	locks             []f.LockRequest
	require           func(context.Context, f.Tx) error
	read              func(context.Context) (sc.SecretMaterial, error)
}

type oauthTokenSnapshot struct {
	version, connectionVersion f.Version
	issuer, resource, request  string
	expires                    time.Time
	refreshable                bool
}

func (a oauthTokenSnapshot) equal(b oauthTokenSnapshot) bool {
	return a.version == b.version && a.connectionVersion == b.connectionVersion && a.issuer == b.issuer && a.resource == b.resource && a.request == b.request && a.expires.Equal(b.expires) && a.refreshable == b.refreshable
}

func (s *oauthCredentialSource) tokenInTx(ctx context.Context, x postgres.SQLExecutor) (oauthTokenSnapshot, error) {
	var v oauthTokenSnapshot
	err := x.QueryRow(ctx, `SELECT t.secret_version,t.issuer,t.resource,t.last_request_id,COALESCE(t.expires_at,'epoch'::timestamptz),t.refreshable,r.connection_version
 FROM agenteam_mcp.oauth_tokens t JOIN agenteam_mcp.oauth_requests r ON r.id=t.last_request_id
 WHERE t.credential_id=$1 AND t.project_id=$2 AND t.connection_id=$3 AND t.config_id=$4 AND t.config_revision=$5`, s.credential, s.binding.ProjectID.String(), s.binding.ConnectionID, s.binding.ConfigID, int64(s.binding.ConfigRevision)).Scan(&v.version, &v.issuer, &v.resource, &v.request, &v.expires, &v.refreshable, &v.connectionVersion)
	if err != nil {
		return v, err
	}
	if v.version.Validate() != nil || v.connectionVersion.Validate() != nil || v.issuer == "" || v.resource == "" {
		return v, fail(f.InvalidState)
	}
	return v, nil
}

func (s *oauthCredentialSource) token(ctx context.Context) (oauthTokenSnapshot, error) {
	var v oauthTokenSnapshot
	locks, err := mergeLocks(s.locks, []f.LockRequest{catalogLock(f.Shared)})
	if err != nil {
		return v, err
	}
	cause, _ := f.NewRecoveryCause("mcp.oauth.read", s.credential, "")
	result := s.service.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.service.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if err := s.require(ctx, tx); err != nil {
			return err
		}
		x, err := s.service.store.InTx(tx)
		if err != nil {
			return err
		}
		v, err = s.tokenInTx(ctx, x)
		return err
	})
	return v, commit(result)
}

// Read the metadata on both sides of the original Secret read. Reading only
// the later version could otherwise CAS a stale refresh token over a newer
// bundle installed concurrently by another original caller.
func (s *Service) readOAuthCredential(ctx context.Context, source *oauthCredentialSource) (sc.SecretMaterial, error) {
	if ctx == nil || source == nil || source.service != s || source.require == nil || source.read == nil || !source.caller.valid() || source.binding.ProjectID.Validate() != nil || source.binding.ConfigRevision.Validate() != nil {
		return sc.SecretMaterial{}, fail(f.Forbidden)
	}
	if _, err := f.ParseID[sc.Credential](source.credential); err != nil {
		return sc.SecretMaterial{}, fail(f.Forbidden)
	}
	before, err := source.token(ctx)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	material, err := source.read(ctx)
	defer material.Destroy()
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	after, err := source.token(ctx)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	if !before.equal(after) {
		return sc.SecretMaterial{}, fail(f.ConfirmationStale)
	}
	access, info, err := protocol.OAuthAccess(material, before.resource, time.Now())
	if err == nil {
		if info.Issuer != before.issuer || info.Resource != before.resource {
			access.Destroy()
			return sc.SecretMaterial{}, fail(f.ConfirmationStale)
		}
		return access, nil
	}
	var rejected *protocol.Error
	if !errors.As(err, &rejected) || rejected.Code != protocol.AuthenticationRequired || !info.Refreshable || !before.refreshable || info.Issuer != before.issuer || info.Resource != before.resource {
		return sc.SecretMaterial{}, err
	}
	return s.refreshOAuthCredential(ctx, source, before, material)
}
