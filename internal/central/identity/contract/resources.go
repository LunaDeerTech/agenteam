package contract

import "github.com/LunaDeerTech/agenteam/internal/central/foundation"

// Tool marks a Registry identity, not a stable key or model-visible tool name.
// D18/D20 own registration, revisions, bindings and current scope validation.
type Tool struct{}

// Mount marks an AgentMount relationship, not a Runner or workspace path.
// The owning directory must establish its current Project and Agent membership.
type Mount struct{}

// ProjectVariable marks either an ordinary or a Secret project variable.
// A valid ID does not establish type=secret, access, or a Credential identity.
type ProjectVariable struct{}

// ToolID inherits Foundation's UUIDv7 scalar contract. It grants no capability.
type ToolID = foundation.ID[Tool]

// MountID is the sole canonical Mount ID; parsing does not resolve a workspace.
type MountID = foundation.ID[Mount]

// ProjectVariableID identifies a variable across value updates. Secret whitelist
// consumers must still validate the current record through its owning domain.
type ProjectVariableID = foundation.ID[ProjectVariable]
