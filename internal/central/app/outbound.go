package app

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

type egress interface {
	StopAdmission()
	Drain(context.Context) error
	ForceClose(context.Context) error
	Status() outbound.PolicyStatus
}
type outboundRuntime struct {
	policy   *outbound.PolicyService
	client   *outbound.Client
	resolver outbound.Resolver // nil retains the default system resolver.
	stopped  atomic.Bool
}

func (o *outboundRuntime) StopAdmission()                  { o.stopped.Store(true); o.client.StopAdmission() }
func (o *outboundRuntime) Drain(ctx context.Context) error { return o.client.Drain(ctx) }
func (o *outboundRuntime) ForceClose(ctx context.Context) error {
	o.stopped.Store(true)
	return o.client.ForceClose(ctx)
}
func (o *outboundRuntime) Status() outbound.PolicyStatus {
	s := o.policy.Status()
	s.Available = s.Available && !o.stopped.Load()
	return s
}

func createOutbound(cfg config.Config, db database, auditing *audit.Service, authority *account.Authority, secrets *secret.Service, proxyAuthority *outbound.ProxySecretAuthority, guard *object.ProcessGuard) (*outboundRuntime, error) {
	store, ok := db.(outbound.Store)
	if !ok || authority == nil {
		return nil, errors.New("OUTBOUND_STORE_UNAVAILABLE")
	}
	policy, err := outbound.NewPolicyService(store, auditing, outbound.Authorizations{Sessions: authority, System: authority})
	if err != nil {
		return nil, err
	}
	if err = policy.BindProxy(outbound.ProxyDependencies{Processes: outboundProcessAuthority{guard}, Secrets: secrets, Authority: proxyAuthority, CurrentMACKey: cfg.AccountKeyring().OutboundProxyMACKey, MAC: cfg.AccountKeyring().OutboundProxyMAC}); err != nil {
		return nil, err
	}
	client, err := outbound.NewClient(policy, cfg.OutboundTrust(), nil)
	if err != nil {
		return nil, err
	}
	return &outboundRuntime{policy: policy, client: client}, nil
}

// This adapter retains the exact original guard through actual outbound join.
type outboundProcessAuthority struct{ guard *object.ProcessGuard }

func (a outboundProcessAuthority) CurrentProxyProcess() (string, error) {
	id, err := a.guard.CurrentProcess()
	return id.String(), err
}
func (a outboundProcessAuthority) ConfirmProxyProcessStopped(ctx context.Context, raw string) error {
	id, err := foundation.ParseID[objc.Process](raw)
	if err != nil {
		return err
	}
	return a.guard.ConfirmStopped(ctx, id)
}
