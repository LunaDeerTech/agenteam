package app

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

type egress interface {
	StopAdmission()
	Drain(context.Context) error
	ForceClose(context.Context) error
	Status() outbound.PolicyStatus
}
type outboundRuntime struct {
	policy  *outbound.PolicyService
	client  *outbound.Client
	stopped atomic.Bool
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

func createOutbound(cfg config.Config, db database, auditing *audit.Service, authority *account.Authority) (*outboundRuntime, error) {
	store, ok := db.(outbound.Store)
	if !ok || authority == nil {
		return nil, errors.New("OUTBOUND_STORE_UNAVAILABLE")
	}
	policy, err := outbound.NewPolicyService(store, auditing, outbound.Authorizations{Sessions: authority, System: authority})
	if err != nil {
		return nil, err
	}
	client, err := outbound.NewClient(policy, cfg.OutboundTrust(), nil)
	if err != nil {
		return nil, err
	}
	return &outboundRuntime{policy: policy, client: client}, nil
}
