package outboundhttp

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const proxyPath = policyPath + "/proxy"

type proxyService interface {
	GetProxy(context.Context, identity.Actor) (outbound.ProxySettings, error)
	UpdateProxy(context.Context, identity.Actor, outbound.CommandMeta, outbound.ProxyUpdate) (outbound.ProxyUpdateResult, error)
}
type proxyGatewayInput struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
}
type proxyUpdateInput struct {
	ExpectedVersion  foundation.Version `json:"expected_version"`
	Mode             string             `json:"mode"`
	Gateway          json.RawMessage    `json:"gateway"`
	CredentialAction string             `json:"credential_action"`
	Password         json.RawMessage    `json:"password,omitempty"`
}

func (h *systemHTTP) proxyCall(w http.ResponseWriter, r *http.Request, actor identity.Actor) ([]byte, error) {
	service, ok := h.policy.(proxyService)
	if !ok {
		return nil, fault(foundation.DependencyUnbound)
	}
	if r.Method == http.MethodGet {
		if e := noBody(r); e != nil {
			return nil, e
		}
		if e := beforeCall(r.Context()); e != nil {
			return nil, e
		}
		result, e := service.GetProxy(r.Context(), actor)
		if e != nil {
			return nil, e
		}
		if result.Version.Validate() != nil || result.Mode != "direct" && result.Mode != "proxy" {
			return nil, outputFault()
		}
		return encodeOutput(result)
	}
	var input proxyUpdateInput
	if e := httpapi.DecodeJSON(w, r, &input, 16<<10); e != nil {
		return nil, e
	}
	if input.ExpectedVersion.Validate() != nil {
		return nil, fault(foundation.InvalidArgument)
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 {
		return nil, fault(foundation.InvalidArgument)
	}
	key, e := foundation.ParseIdempotencyKey(keys[0])
	if e != nil {
		return nil, fault(foundation.InvalidArgument)
	}
	command, e := foundation.NewCommandIdentity("outbound-proxy", []string{actor.Details().UserID}, "update", key)
	if e != nil {
		return nil, e
	}
	meta := outbound.CommandMeta{Identity: command, ExpectedVersion: input.ExpectedVersion}
	if trace := httpapi.RequestID(r.Context()); trace.Validate() == nil {
		meta.HTTPTraceID = trace.String()
	}
	update := outbound.ProxyUpdate{Mode: input.Mode, CredentialAction: input.CredentialAction}
	if len(input.Gateway) == 0 {
		return nil, fault(foundation.InvalidArgument)
	}
	if string(input.Gateway) != "null" {
		var fields map[string]json.RawMessage
		if json.Unmarshal(input.Gateway, &fields) != nil || len(fields) != 4 {
			return nil, fault(foundation.InvalidArgument)
		}
		for _, key := range []string{"protocol", "host", "port", "username"} {
			if len(fields[key]) == 0 || string(fields[key]) == "null" {
				return nil, fault(foundation.InvalidArgument)
			}
		}
		var g proxyGatewayInput
		if json.Unmarshal(input.Gateway, &g) != nil {
			return nil, fault(foundation.InvalidArgument)
		}
		update.Gateway = &outbound.ProxyGateway{Protocol: g.Protocol, Host: g.Host, Port: g.Port, Username: g.Username}
	}
	if len(input.Password) > 0 {
		defer clear(input.Password)
		var password string
		if string(input.Password) == "null" || json.Unmarshal(input.Password, &password) != nil {
			return nil, fault(foundation.InvalidArgument)
		}
		raw := []byte(password)
		password = ""
		material, e := sc.NewSecretMaterial(raw)
		clear(raw)
		if e != nil {
			return nil, fault(foundation.InvalidArgument)
		}
		defer material.Destroy()
		update.Password = &material
	}
	if e = beforeCall(r.Context()); e != nil {
		return nil, e
	}
	result, e := service.UpdateProxy(r.Context(), actor, meta, update)
	if e != nil {
		return nil, e
	}
	if result.Version.Validate() != nil || result.AuditID.Validate() != nil || result.CreatedAt.Validate() != nil {
		return nil, outputFault()
	}
	return json.Marshal(result)
}
