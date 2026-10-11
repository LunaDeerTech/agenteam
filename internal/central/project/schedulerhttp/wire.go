package schedulerhttp

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

const representationLimit = 4 << 10

func invalid() *f.Fault       { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func badProjection() *f.Fault { return f.NewFault(f.InternalError, f.Unknown) }

// Explicit null is a valid unlimited value; absence is an invalid replacement.
type concurrencyInput struct {
	present bool
	value   *int64
}

func (v *concurrencyInput) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		*v = concurrencyInput{present: true}
		return nil
	}
	var version f.Version
	if err := json.Unmarshal(raw, &version); err != nil || version.Validate() != nil {
		return invalid()
	}
	limit := int64(version)
	*v = concurrencyInput{present: true, value: &limit}
	return nil
}

type updateInput struct {
	Expected    f.Version       `json:"expected_version"`
	Enabled     *bool           `json:"scheduler_enabled"`
	Concurrency json.RawMessage `json:"scheduler_max_concurrency"`
}

func decodeUpdate(w http.ResponseWriter, r *http.Request) (f.CommandMeta, pc.UpdateProjectRequest, error) {
	var wire updateInput
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || f.IdempotencyKey(keys[0]).Validate() != nil {
		return f.CommandMeta{}, pc.UpdateProjectRequest{}, invalid()
	}
	if err := httpapi.DecodeJSON(w, r, &wire, representationLimit); err != nil {
		return f.CommandMeta{}, pc.UpdateProjectRequest{}, err
	}
	// RawMessage retains missing versus explicit null through DecodeJSON's
	// strict DTO prevalidation. Validate its closed scalar shape locally.
	var concurrency concurrencyInput
	if wire.Enabled == nil || len(wire.Concurrency) == 0 || json.Unmarshal(wire.Concurrency, &concurrency) != nil || !concurrency.present {
		return f.CommandMeta{}, pc.UpdateProjectRequest{}, invalid()
	}
	meta := f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: f.IdempotencyKey(keys[0]), ExpectedVersion: &wire.Expected}
	input := pc.UpdateProjectRequest{Scheduler: &pc.ProjectSchedulerConfig{Enabled: *wire.Enabled, MaxConcurrency: concurrency.value}}
	if pc.ValidateCommandMeta(pc.UpdateCommand, meta) != nil || input.Validate() != nil {
		return f.CommandMeta{}, pc.UpdateProjectRequest{}, invalid()
	}
	return meta, input, nil
}

type currentDTO struct {
	ProjectID   pc.ProjectID `json:"project_id"`
	Version     f.Version    `json:"version"`
	Enabled     bool         `json:"scheduler_enabled"`
	Concurrency *string      `json:"scheduler_max_concurrency"`
}

type receiptDTO struct {
	ProjectID pc.ProjectID `json:"project_id"`
	Version   f.Version    `json:"version"`
}

func validProject(actor i.Actor, target pc.ProjectID, value pc.ProjectRef) bool {
	return actor.Validate() == nil && actor.Details().Kind == i.Human && target.Validate() == nil && value.Validate() == nil && value.ID == target && value.OwnerUserID.String() == actor.Details().UserID && value.Lifecycle != pc.Deleting
}

func encode(ctx context.Context, value any) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > representationLimit {
		return nil, badProjection()
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}

func encodeCurrent(ctx context.Context, actor i.Actor, target pc.ProjectID, value pc.SchedulerProject) ([]byte, error) {
	if !validProject(actor, target, value.Project) || value.Config.Validate() != nil {
		return nil, badProjection()
	}
	out := currentDTO{ProjectID: target, Version: value.Project.Version, Enabled: value.Config.Enabled}
	if value.Config.MaxConcurrency != nil {
		limit := strconv.FormatInt(*value.Config.MaxConcurrency, 10)
		out.Concurrency = &limit
	}
	return encode(ctx, out)
}

func encodeReceipt(ctx context.Context, actor i.Actor, target pc.ProjectID, meta f.CommandMeta, value pc.ProjectRef) ([]byte, error) {
	if !validProject(actor, target, value) || value.Lifecycle != pc.Active || pc.ValidateCommandMeta(pc.UpdateCommand, meta) != nil {
		return nil, badProjection()
	}
	expected := *meta.ExpectedVersion
	if value.Version != expected && (expected == f.Version(math.MaxInt64) || value.Version != expected+1) {
		return nil, badProjection()
	}
	return encode(ctx, receiptDTO{ProjectID: target, Version: value.Version})
}
