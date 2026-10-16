package outboundhttp

import (
	"encoding/json"
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

const maxJSONBytes = 1 << 20

type updateInput struct {
	ExpectedVersion foundation.Version `json:"expected_version"`
	Mode            outbound.RuleMode  `json:"rule_mode"`
	Rules           json.RawMessage    `json:"rules"`
}

type policyOutput struct {
	Version foundation.Version `json:"version"`
	Mode    outbound.RuleMode  `json:"rule_mode"`
	Rules   json.RawMessage    `json:"rules"`
}

func decodeUpdate(w http.ResponseWriter, r *http.Request, actor identity.Actor) (outbound.CommandMeta, outbound.RuleMode, outbound.Rules, error) {
	var empty outbound.CommandMeta
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 {
		return empty, "", outbound.Rules{}, fault(foundation.InvalidArgument)
	}
	key, err := foundation.ParseIdempotencyKey(keys[0])
	if err != nil || actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return empty, "", outbound.Rules{}, fault(foundation.InvalidArgument)
	}
	command, err := foundation.NewCommandIdentity("outbound-policy-v2", []string{actor.Details().UserID}, "update", key)
	if err != nil {
		return empty, "", outbound.Rules{}, fault(foundation.InvalidArgument)
	}
	var input updateInput
	if err = httpapi.DecodeJSON(w, r, &input, maxJSONBytes); err != nil {
		return empty, "", outbound.Rules{}, err
	}
	if input.ExpectedVersion.Validate() != nil || !input.Mode.Valid() {
		return empty, "", outbound.Rules{}, fault(foundation.InvalidArgument)
	}
	// Keep the original field bytes, including whitespace, for the engine's
	// independent 1 MiB limit and its sole canonicalization contract.
	rules, err := outbound.DecodeRules(input.Rules)
	if err != nil {
		return empty, "", outbound.Rules{}, err
	}
	meta := outbound.CommandMeta{Identity: command, ExpectedVersion: input.ExpectedVersion}
	if requestID := httpapi.RequestID(r.Context()); requestID.Validate() == nil {
		meta.HTTPTraceID = requestID.String()
	}
	return meta, input.Mode, rules, nil
}

func encodePolicy(policy outbound.Policy) ([]byte, error) {
	if policy.Version().Validate() != nil || !policy.Mode().Valid() || !policy.Rules().Valid() {
		return nil, outputFault()
	}
	return encodeOutput(policyOutput{Version: policy.Version(), Mode: policy.Mode(), Rules: policy.Rules().JSON()})
}

func encodeUpdate(result outbound.UpdateResult) ([]byte, error) {
	if result.Version.Validate() != nil || result.RuleCount.Validate() != nil || result.RuleCount > 512 || result.AuditID.Validate() != nil || result.CreatedAt.Validate() != nil {
		return nil, outputFault()
	}
	return encodeOutput(result)
}

func encodeOutput(dto any) ([]byte, error) {
	body, err := json.Marshal(dto)
	if err != nil || len(body) > maxJSONBytes {
		return nil, outputFault()
	}
	return body, nil
}

func outputFault() error {
	// The command may already be committed; an invalid projection cannot say
	// that a write was not started, and it must never publish candidate data.
	return foundation.NewFault(foundation.DependencyUnavailable, foundation.Unknown)
}
