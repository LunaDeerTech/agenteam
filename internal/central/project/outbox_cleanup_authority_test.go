package project

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func outboxCleanupRecord(t *testing.T) *lifecycleRecord {
	t.Helper()
	r := skillsCleanupRecord(t)
	r.cleanupStage = "outbox"
	for i := range r.participants {
		if r.participants[i].name != c.OutboxParticipant && r.participants[i].name != c.AuditParticipant {
			r.participants[i].cleanup = "completed"
		}
	}
	return r
}

// These are controlled canonical records, not stop/cleanup owner witnesses.
// The PG test separately reaches the real driver's original claim and D06.
func TestOutboxCleanupProgressBoundaries(t *testing.T) {
	for _, state := range []string{"required", "pending"} {
		t.Run(state, func(t *testing.T) {
			r := outboxCleanupRecord(t)
			skillsCleanupSet(r, c.OutboxParticipant, "stopped", state)
			if err := outboxCleanupProgress(r); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		edit func(*lifecycleRecord)
		code f.Code
	}{
		{"archive", func(r *lifecycleRecord) { r.operation.Action = c.Archive }, f.InvalidState},
		{"stopping", func(r *lifecycleRecord) { r.operation.State = c.OperationStopping }, f.InvalidState},
		{"failed-cleanup", func(r *lifecycleRecord) { r.operation.State, r.resume = c.OperationFailed, c.CleanupPhase }, f.InvalidState},
		{"domains", func(r *lifecycleRecord) { r.cleanupStage = "domains" }, f.InvalidState},
		{"audit", func(r *lifecycleRecord) { r.cleanupStage = "audit" }, f.InvalidState},
		{"final", func(r *lifecycleRecord) { r.cleanupStage = "final" }, f.InvalidState},
		{"unfinished-stop", func(r *lifecycleRecord) { skillsCleanupSet(r, c.SecretParticipant, "pending", "completed") }, f.DependencyUnavailable},
		{"unfinished-domain", func(r *lifecycleRecord) { skillsCleanupSet(r, c.SkillsParticipant, "stopped", "pending") }, f.InvalidState},
		{"failed-domain", func(r *lifecycleRecord) { skillsCleanupSet(r, c.ArtifactObjectParticipant, "stopped", "failed") }, f.InvalidState},
		{"outbox-completed", func(r *lifecycleRecord) { skillsCleanupSet(r, c.OutboxParticipant, "stopped", "completed") }, f.InvalidState},
		{"outbox-failed", func(r *lifecycleRecord) { skillsCleanupSet(r, c.OutboxParticipant, "stopped", "failed") }, f.InvalidState},
		{"audit-started", func(r *lifecycleRecord) { skillsCleanupSet(r, c.AuditParticipant, "stopped", "pending") }, f.InvalidState},
		{"missing-row", func(r *lifecycleRecord) { r.participants = r.participants[1:] }, f.DependencyUnavailable},
		{"unknown-row", func(r *lifecycleRecord) { r.participants[0].name = "unregistered" }, f.DependencyUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := outboxCleanupRecord(t)
			tc.edit(r)
			hasCode(t, outboxCleanupProgress(r), tc.code)
		})
	}
	hasCode(t, outboxCleanupProgress(nil), f.DependencyUnavailable)
}

func TestOutboxCleanupDiscoveryDoesNotGrantAuthority(t *testing.T) {
	a := lifecycleTestAuthority(t) // Any unexpected Store call panics.
	actor, project, cause := lifecycleTestActor(t)
	op, _ := f.ParseID[oc.LifecycleOperation](cause.OperationID.String())
	downstream, err := oc.NewLifecycleCause(oc.LifecycleDetails{ProjectID: project, OperationID: op, Action: oc.ArchiveProject, ProjectVersion: cause.ProjectVersion})
	if err != nil {
		t.Fatal(err)
	}
	request, err := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.LifecycleProject, ProjectID: project, Actor: actor, Lifecycle: downstream, LifecycleStep: oc.LifecycleCleanup})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := a.Discover(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, a.ValidateInTx(context.Background(), f.NewTx(), request, plan), f.Forbidden)
}
