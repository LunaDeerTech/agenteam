//go:build integration

package projectvariable_test

import (
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// These controls call the actual fixture helpers and public contracts without
// opening a database. They distinguish request mutation from a stored digest
// error and prove the Project time invariant, not the old SQL evaluation order.
func TestSecretOwnerReadFixtureContracts(t *testing.T) {
	t.Run("lookup-captures-original-expected", func(t *testing.T) {
		project, target := id[i.Project](t), id[i.ProjectVariable](t)
		version := f.Version(2)
		originalMeta := meta(t, "fixture-noop", &version)
		original := secretLookup(t, project, target, vc.SecretUpdateCommand, originalMeta)
		version = 3
		late := secretLookup(t, project, target, vc.SecretUpdateCommand, originalMeta)
		before, after := original.Fields(), late.Fields()
		if before.ExpectedVersion == nil || *before.ExpectedVersion != 2 || after.ExpectedVersion == nil || *after.ExpectedVersion != 3 {
			t.Fatal("actual helpers did not distinguish original and mutated request")
		}
		if before.ProjectID != after.ProjectID || before.TargetID != after.TargetID || before.Command != after.Command || before.IdempotencyKey != after.IdempotencyKey {
			t.Fatal("control changed more than the expected version")
		}
		*before.ExpectedVersion = 4
		if *original.Fields().ExpectedVersion != 2 || original.Validate() != nil || late.Validate() != nil {
			t.Fatal("valid historical lookup did not preserve its original snapshot")
		}
	})
	t.Run("actual-project-contract-rejects-inverted-times", func(t *testing.T) {
		instant := func(at time.Time) f.Instant {
			v, err := f.NewInstant(at)
			if err != nil {
				t.Fatal(err)
			}
			return v
		}
		created := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
		updated := instant(created.Add(time.Second))
		archived := updated
		ref := pc.ProjectRef{ID: id[i.Project](t), OwnerUserID: id[i.User](t), Name: "archive-fixture",
			NormalizedName: "archive-fixture", Lifecycle: pc.Archived, Version: 2,
			CreatedAt: instant(created), UpdatedAt: updated, ArchivedAt: &archived}
		if err := ref.Validate(); err != nil {
			t.Fatal("equal statement times must form a valid archived Project", err)
		}
		archived = instant(updated.Time().Add(time.Microsecond))
		if ref.Validate() == nil {
			t.Fatal("actual Project contract accepted archived_at after updated_at")
		}
	})
}
