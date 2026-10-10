package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// ProjectSchedulerConfig is an explicit complete replacement. A nil limit
// means unlimited, never an unavailable configuration. It is not an HTTP DTO.
type ProjectSchedulerConfig struct {
	Enabled        bool   `json:"scheduler_enabled"`
	MaxConcurrency *int64 `json:"scheduler_max_concurrency"`
}

func (c ProjectSchedulerConfig) Validate() error {
	if c.MaxConcurrency != nil && *c.MaxConcurrency < 1 {
		return invalid("/scheduler_max_concurrency", "INVALID_LIMIT")
	}
	return nil
}
func (c ProjectSchedulerConfig) Clone() ProjectSchedulerConfig {
	if c.MaxConcurrency != nil {
		v := *c.MaxConcurrency
		c.MaxConcurrency = &v
	}
	return c
}
func (c ProjectSchedulerConfig) MarshalJSON() ([]byte, error) {
	type wire ProjectSchedulerConfig
	return checkedJSON(wire(c), c.Validate())
}
func (c *ProjectSchedulerConfig) UnmarshalJSON(raw []byte) error {
	type wire ProjectSchedulerConfig
	v, err := decodeFields[wire](raw, []string{"scheduler_enabled", "scheduler_max_concurrency"}, nil, []string{"scheduler_max_concurrency"})
	if err != nil {
		return err
	}
	next := ProjectSchedulerConfig(v)
	if err := next.Validate(); err != nil {
		return err
	}
	*c = next
	return nil
}

// SchedulerProject is current, callback-local Project data, not a grant.
// Existing ProjectRef/HTTP/command receipt encodings remain unchanged.
type SchedulerProject struct {
	Project ProjectRef
	Config  ProjectSchedulerConfig
}

func (p SchedulerProject) Clone() SchedulerProject {
	p.Project = cloneProject(p.Project)
	p.Config = p.Config.Clone()
	return p
}

type SystemSchedulerDefaults struct {
	MaxConcurrency *int64
	Version        f.Version
}

// SchedulerIntent is only the projection of the bound Scheduler authority's
// original private handoff. Constructing it cannot authorize a Service actor.
type SchedulerIntent struct {
	ProjectID  ProjectID
	AgentID    i.AgentID
	DispatchID string
	SprintID   SprintID
}

// The provider owns the durable Dispatch and its live private intent. It must
// validate the original same-Store Tx, issuer, Actor/cause, Project/Agent,
// Launch request and held lock union; a registered Service name is insufficient.
// Read validates the historical association without requiring the Task to be
// launchable now. Launch additionally validates current Agent eligibility.
// It must not call Execution or this Project gate recursively.
type SchedulerIntentAuthority interface {
	RequireSchedulerIntentInTx(context.Context, f.Tx, i.Actor, ProjectID, i.AgentID, i.AccessIntent) (SchedulerIntent, error)
}
