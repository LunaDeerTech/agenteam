package agent

import (
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func retirementAdvancedAnchor(t *testing.T) (c.AgentConfig, c.ProjectRetirementPlan) {
	t.Helper()
	plan := retirementPlan(t)
	fields := plan.Change.Before.Fields()
	// Keep real nonempty capability lists in the immutable configuration, so a
	// matching version alone cannot stand in for its complete original fields.
	fields.AllowedToolIDs = []i.ToolID{retirementPreparationID[i.Tool](t)}
	fields.AllowedMountIDs = []i.MountID{retirementPreparationID[i.Mount](t)}
	fields.AllowedSecretVariableIDs = []i.ProjectVariableID{retirementPreparationID[i.ProjectVariable](t)}
	base, err := c.NewAgentConfig(fields)
	if err != nil {
		t.Fatal(err)
	}
	fields.Core.Version += 2
	fields.Core.UpdatedAt, err = f.NewInstant(fields.Core.UpdatedAt.Time().Add(2 * time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	plan.Change.Before, err = c.NewAgentConfig(fields)
	if err != nil {
		t.Fatal(err)
	}
	plan.Change.Request.ExpectedVersion = fields.Core.Version
	at := fields.Core.UpdatedAt
	plan.BeforeUpdatedAt = &at
	plan.BeforeDigest, err = c.ProjectRetirementBeforeDigest(plan.Change.Before)
	if err != nil || plan.Validate() != nil {
		t.Fatal("invalid independent current Before", err)
	}
	return base, plan
}

func TestAgentProjectRetirementCompleteBeforeAnchor(t *testing.T) {
	t.Run("exact-command", func(t *testing.T) {
		plan := retirementPlan(t)
		if err := validateRetirementCommandBefore(plan.Change.Before, plan); err != nil {
			t.Fatal("legacy exact command rejected", err)
		}
	})
	t.Run("actual-version-and-time", func(t *testing.T) {
		base, plan := retirementAdvancedAnchor(t)
		original := base.Fields()
		if err := validateRetirementCommandBefore(base, plan); err != nil {
			t.Fatal("unchanged complete configuration could not use its actual scalar anchor", err)
		}
		got, err := retirementBeforeFromCommand(base, plan.Change.Request.ExpectedVersion, plan.BeforeUpdatedAt)
		if err != nil || !sameValue(got, plan.Change.Before) || !sameValue(base.Fields(), original) {
			t.Fatal("reconstruction changed the immutable command or lost complete current fields", err)
		}
	})
	for _, name := range []string{"body", "tool-allowlist", "mount-allowlist", "secret-allowlist", "created-at"} {
		t.Run(name, func(t *testing.T) {
			base, plan := retirementAdvancedAnchor(t)
			fields := plan.Change.Before.Fields()
			switch name {
			case "body":
				fields.Core.Instructions += " changed outside assignment"
			case "tool-allowlist":
				fields.AllowedToolIDs = []i.ToolID{}
			case "mount-allowlist":
				fields.AllowedMountIDs = []i.MountID{}
			case "secret-allowlist":
				fields.AllowedSecretVariableIDs = []i.ProjectVariableID{}
			case "created-at":
				var err error
				fields.Core.CreatedAt, err = f.NewInstant(fields.Core.CreatedAt.Time().Add(-time.Microsecond))
				if err != nil {
					t.Fatal(err)
				}
			}
			var err error
			plan.Change.Before, err = c.NewAgentConfig(fields)
			if err != nil {
				t.Fatal(err)
			}
			// Even a valid digest of the changed public Before is insufficient.
			plan.BeforeDigest, err = c.ProjectRetirementBeforeDigest(plan.Change.Before)
			if err != nil || plan.Validate() != nil {
				t.Fatal("invalid drift fixture", err)
			}
			requireCode(t, validateRetirementCommandBefore(base, plan), f.VersionConflict)
		})
	}
}

func TestAgentProjectRetirementAnchorRejectsScalarDrift(t *testing.T) {
	for _, name := range []string{"missing-time", "same-version", "older-version", "same-time", "older-time", "wrong-digest"} {
		t.Run(name, func(t *testing.T) {
			base, plan := retirementAdvancedAnchor(t)
			fields := plan.Change.Before.Fields()
			switch name {
			case "missing-time":
				plan.BeforeUpdatedAt = nil
			case "same-version":
				fields.Core.Version = base.Fields().Core.Version
			case "older-version":
				// Keep the attempted version valid while making its immutable
				// source newer, instead of failing at a malformed zero version.
				baseFields := base.Fields()
				baseFields.Core.Version = fields.Core.Version + 1
				var err error
				base, err = c.NewAgentConfig(baseFields)
				if err != nil {
					t.Fatal(err)
				}
			case "same-time":
				fields.Core.UpdatedAt = base.Fields().Core.UpdatedAt
			case "older-time":
				baseFields := base.Fields()
				var err error
				baseFields.Core.UpdatedAt, err = f.NewInstant(fields.Core.UpdatedAt.Time().Add(time.Microsecond))
				if err != nil {
					t.Fatal(err)
				}
				base, err = c.NewAgentConfig(baseFields)
				if err != nil {
					t.Fatal(err)
				}
			}
			var err error
			plan.Change.Before, err = c.NewAgentConfig(fields)
			if err != nil {
				t.Fatal(err)
			}
			plan.Change.Request.ExpectedVersion = fields.Core.Version
			if plan.BeforeUpdatedAt != nil {
				at := fields.Core.UpdatedAt
				plan.BeforeUpdatedAt = &at
			}
			if name != "wrong-digest" {
				plan.BeforeDigest, err = c.ProjectRetirementBeforeDigest(plan.Change.Before)
			} else {
				plan.BeforeDigest, err = c.ProjectRetirementBeforeDigest(base)
			}
			if err != nil {
				t.Fatal(err)
			}
			if validateRetirementCommandBefore(base, plan) == nil {
				t.Fatal("nonmonotonic or unbound scalar anchor accepted")
			}
		})
	}
}
