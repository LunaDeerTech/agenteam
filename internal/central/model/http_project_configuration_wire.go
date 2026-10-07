package model

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const configurationBodyBytes = 1 << 20
const configurationOutputBytes = 1024

type configurationProviderInput httpProviderInput
type configurationModelInput httpModelInput

type configurationCreateProvider struct {
	Input *configurationProviderInput `json:"input"`
}
type configurationUpdateProvider struct {
	Expected f.Version                   `json:"expected_version"`
	Input    *configurationProviderInput `json:"input"`
}
type configurationDeleteProvider struct {
	Expected f.Version `json:"expected_version"`
}
type configurationCreateModel struct {
	Provider mc.ProviderID            `json:"provider_id"`
	Input    *configurationModelInput `json:"input"`
}
type configurationUpdateModel struct {
	Expected f.Version                `json:"expected_version"`
	Input    *configurationModelInput `json:"input"`
}
type configurationDeleteModel struct {
	Expected    f.Version       `json:"expected_version"`
	Replacement json.RawMessage `json:"replacement"`
}
type configurationLookupInput struct {
	Command string `json:"command"`
}

type configurationRequest struct {
	meta        mc.CommandMeta
	kind        string
	lookup      bool
	providerID  mc.ProviderID
	modelID     mc.ModelID
	expected    f.Version
	provider    *mc.ProviderInput
	model       *mc.ModelInput
	replacement *mc.ModelID
}

func (d configurationProviderInput) input(scope id.Scope) (mc.ProviderInput, error) {
	if scope.Validate() != nil || scope.Details().Kind != id.ProjectScope || d.Name == nil || d.Protocol == nil || d.BaseURL == nil || d.Enabled == nil || !httpObject(d.Options) {
		return mc.ProviderInput{}, fault(f.InvalidArgument)
	}
	credential, err := httpNullableID[sc.Credential](d.CredentialRef)
	if err != nil {
		return mc.ProviderInput{}, err
	}
	var ref *sc.CredentialRef
	if credential != nil {
		value, err := sc.NewCredentialRef(*credential, scope)
		if err != nil {
			return mc.ProviderInput{}, fault(f.InvalidArgument)
		}
		ref = &value
	}
	return mc.ProviderInput{Name: *d.Name, Protocol: *d.Protocol, BaseURL: *d.BaseURL, Enabled: *d.Enabled, CredentialRef: ref, Options: d.Options}, nil
}
func (d configurationModelInput) input() (mc.ModelInput, error) {
	// The existing model-field converter has no authority/scope or lookup path;
	// the complete Project request is validated before any service call below.
	return (httpModelInput)(d).input()
}
func configurationCommand(kind string) bool {
	switch kind {
	case "provider.create", "provider.update", "provider.delete", "model.create", "model.update", "model.delete":
		return true
	}
	return false
}
func (r configurationRequest) validate() error {
	if r.meta.Validate() != nil || r.meta.Scope.Details().Kind != id.ProjectScope || !configurationCommand(r.kind) {
		return fault(f.InvalidArgument)
	}
	if r.lookup {
		return nil
	}
	switch r.kind {
	case "provider.create":
		if r.provider != nil {
			return (mc.CreateProviderRequest{CommandMeta: r.meta, Input: *r.provider}).Validate()
		}
	case "provider.update":
		if r.provider != nil {
			return (mc.UpdateProviderRequest{CommandMeta: r.meta, ID: r.providerID, ExpectedVersion: r.expected, Input: *r.provider}).Validate()
		}
	case "provider.delete":
		return (mc.DeleteProviderRequest{CommandMeta: r.meta, ID: r.providerID, ExpectedVersion: r.expected}).Validate()
	case "model.create":
		if r.model != nil {
			return (mc.CreateModelRequest{CommandMeta: r.meta, ProviderID: r.providerID, Input: *r.model}).Validate()
		}
	case "model.update":
		if r.model != nil {
			return (mc.UpdateModelRequest{CommandMeta: r.meta, ID: r.modelID, ExpectedVersion: r.expected, Input: *r.model}).Validate()
		}
	case "model.delete":
		return (mc.DeleteModelRequest{CommandMeta: r.meta, ID: r.modelID, ExpectedVersion: r.expected, Replacement: r.replacement}).Validate()
	}
	return fault(f.InvalidArgument)
}
func configurationDecode(w http.ResponseWriter, r *http.Request, kind configurationHTTPResource, target string, meta mc.CommandMeta) (configurationRequest, error) {
	out := configurationRequest{meta: meta}
	var err error
	if kind == configurationProvider {
		out.providerID, err = f.ParseID[mc.Provider](target)
	}
	if kind == configurationModel {
		out.modelID, err = f.ParseID[mc.Model](target)
	}
	if err != nil {
		return configurationRequest{}, fault(f.InvalidArgument)
	}
	decode := func(dto any) error { return httpapi.DecodeJSON(w, r, dto, configurationBodyBytes) }
	switch {
	case kind == configurationLookup:
		var dto configurationLookupInput
		if err = decode(&dto); err != nil {
			return configurationRequest{}, err
		}
		out.kind, out.lookup = dto.Command, true
	case kind == configurationProviders && r.Method == http.MethodPost:
		var dto configurationCreateProvider
		if err = decode(&dto); err != nil {
			return configurationRequest{}, err
		}
		if dto.Input == nil {
			return configurationRequest{}, fault(f.InvalidArgument)
		}
		v, e := dto.Input.input(meta.Scope)
		if e != nil {
			return configurationRequest{}, e
		}
		out.kind, out.provider = "provider.create", &v
	case kind == configurationProvider && r.Method == http.MethodPut:
		var dto configurationUpdateProvider
		if err = decode(&dto); err != nil {
			return configurationRequest{}, err
		}
		if dto.Input == nil {
			return configurationRequest{}, fault(f.InvalidArgument)
		}
		v, e := dto.Input.input(meta.Scope)
		if e != nil {
			return configurationRequest{}, e
		}
		out.kind, out.expected, out.provider = "provider.update", dto.Expected, &v
	case kind == configurationProvider && r.Method == http.MethodDelete:
		var dto configurationDeleteProvider
		if err = decode(&dto); err != nil {
			return configurationRequest{}, err
		}
		out.kind, out.expected = "provider.delete", dto.Expected
	case kind == configurationModels && r.Method == http.MethodPost:
		var dto configurationCreateModel
		if err = decode(&dto); err != nil {
			return configurationRequest{}, err
		}
		if dto.Input == nil {
			return configurationRequest{}, fault(f.InvalidArgument)
		}
		v, e := dto.Input.input()
		if e != nil {
			return configurationRequest{}, e
		}
		out.kind, out.providerID, out.model = "model.create", dto.Provider, &v
	case kind == configurationModel && r.Method == http.MethodPut:
		var dto configurationUpdateModel
		if err = decode(&dto); err != nil {
			return configurationRequest{}, err
		}
		if dto.Input == nil {
			return configurationRequest{}, fault(f.InvalidArgument)
		}
		v, e := dto.Input.input()
		if e != nil {
			return configurationRequest{}, e
		}
		out.kind, out.expected, out.model = "model.update", dto.Expected, &v
	case kind == configurationModel && r.Method == http.MethodDelete:
		var dto configurationDeleteModel
		if err = decode(&dto); err != nil {
			return configurationRequest{}, err
		}
		out.replacement, err = httpNullableID[mc.Model](dto.Replacement)
		if err != nil {
			return configurationRequest{}, err
		}
		out.kind, out.expected = "model.delete", dto.Expected
	default:
		return configurationRequest{}, fault(f.InvalidArgument)
	}
	if out.validate() != nil {
		return configurationRequest{}, fault(f.InvalidArgument)
	}
	return out, nil
}

type configurationReceipt struct {
	Kind     string        `json:"kind"`
	Resource string        `json:"resource_id"`
	Version  f.Version     `json:"version"`
	Affected mc.TokenCount `json:"affected_references"`
}
type configurationObservation struct {
	Found   bool                  `json:"found"`
	Receipt *configurationReceipt `json:"receipt"`
}

func configurationProjectionError(write bool) error {
	if write {
		// A successful call may already have committed. This new HTTP-only
		// classification does not invent a physical attempt/cause or retry it.
		return f.NewFault(f.DependencyUnavailable, f.Unknown)
	}
	return fault(f.DependencyUnavailable)
}
func configurationReceiptView(kind string, receipt mc.CommandReceipt) (configurationReceipt, bool) {
	if !configurationCommand(kind) || receipt.Validate() != nil || receipt.Kind != kind || !configurationCommand(receipt.Kind) || receipt.Kind != "model.delete" && receipt.AffectedReferences != 0 {
		return configurationReceipt{}, false
	}
	// Parse the public ID strictly; receipt.Validate also verifies the contract.
	if _, err := f.ParseID[struct{}](receipt.ResourceID); err != nil {
		return configurationReceipt{}, false
	}
	return configurationReceipt{receipt.Kind, receipt.ResourceID, receipt.Version, receipt.AffectedReferences}, true
}
func configurationEncode(ctx context.Context, value any, write bool) ([]byte, error) {
	configurationCheckBudget(ctx)
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > configurationOutputBytes {
		return nil, configurationProjectionError(write)
	}
	configurationCheckBudget(ctx)
	return encoded, nil
}
func configurationEncodeReceipt(ctx context.Context, input configurationRequest, receipt mc.CommandReceipt) ([]byte, error) {
	configurationCheckBudget(ctx)
	out, ok := configurationReceiptView(input.kind, receipt)
	if !ok || input.lookup || input.validate() != nil || receipt.AffectedReferences != 0 {
		return nil, configurationProjectionError(true)
	}
	switch input.kind {
	case "provider.create", "model.create":
		if out.Version != 1 {
			return nil, configurationProjectionError(true)
		}
	default:
		target := input.modelID.String()
		if input.kind == "provider.update" || input.kind == "provider.delete" {
			target = input.providerID.String()
		}
		if input.expected == f.Version(math.MaxInt64) || out.Resource != target || out.Version != input.expected+1 {
			return nil, configurationProjectionError(true)
		}
	}
	return configurationEncode(ctx, out, true)
}
func configurationEncodeLookup(ctx context.Context, kind string, lookup CommandLookup) ([]byte, error) {
	configurationCheckBudget(ctx)
	if !configurationCommand(kind) || lookup.Found != (lookup.Receipt != nil) {
		return nil, configurationProjectionError(false)
	}
	out := configurationObservation{Found: lookup.Found}
	if lookup.Found {
		receipt, ok := configurationReceiptView(kind, *lookup.Receipt)
		if !ok {
			return nil, configurationProjectionError(false)
		}
		out.Receipt = &receipt
	}
	return configurationEncode(ctx, out, false)
}

func (configurationProviderInput) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_model_configuration_input"))
}
func (configurationProviderInput) LogValue() slog.Value {
	return slog.StringValue("project_model_configuration_input")
}

func (configurationModelInput) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_model_configuration_input"))
}
func (configurationModelInput) LogValue() slog.Value {
	return slog.StringValue("project_model_configuration_input")
}

func (configurationCreateProvider) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_model_configuration_input"))
}
func (configurationCreateProvider) LogValue() slog.Value {
	return slog.StringValue("project_model_configuration_input")
}

func (configurationUpdateProvider) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_model_configuration_input"))
}
func (configurationUpdateProvider) LogValue() slog.Value {
	return slog.StringValue("project_model_configuration_input")
}

func (configurationDeleteProvider) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_model_configuration_input"))
}
func (configurationDeleteProvider) LogValue() slog.Value {
	return slog.StringValue("project_model_configuration_input")
}

func (configurationCreateModel) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_model_configuration_input"))
}
func (configurationCreateModel) LogValue() slog.Value {
	return slog.StringValue("project_model_configuration_input")
}

func (configurationUpdateModel) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_model_configuration_input"))
}
func (configurationUpdateModel) LogValue() slog.Value {
	return slog.StringValue("project_model_configuration_input")
}

func (configurationDeleteModel) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_model_configuration_input"))
}
func (configurationDeleteModel) LogValue() slog.Value {
	return slog.StringValue("project_model_configuration_input")
}

func (configurationLookupInput) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_model_configuration_input"))
}
func (configurationLookupInput) LogValue() slog.Value {
	return slog.StringValue("project_model_configuration_input")
}

func (configurationRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_model_configuration_input"))
}
func (configurationRequest) LogValue() slog.Value {
	return slog.StringValue("project_model_configuration_input")
}
