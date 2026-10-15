package meeting

import (
	"math"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

func TestContributionRestartKeepsCanonicalLineage(t *testing.T) {
	request := launchRequest(summaryTestID[i.Project](t), summaryTestID[c.Meeting](t), summaryTestID[c.Turn](t), contributionRecord{id: summaryTestID[c.Contribution](t), participant: summaryTestID[c.Participant](t), agent: summaryTestID[i.Agent](t), generation: 3, attempt: 2})
	original := ec.Summary{ID: summaryTestID[i.Execution](t), ProjectID: request.ProjectID, AgentID: request.AgentID, Trigger: request.Trigger, Status: ec.Failed}
	retry, err := restartLineage(request, original, "retry-contribution")
	if err != nil || *retry.ContributionGeneration != 3 || *retry.ContributionAttempt != 3 || retry.RetryOf == nil || *retry.RetryOf != original.ID || retry.RegenerateOf != nil {
		t.Fatal("retry must preserve generation and point at the failed Execution", err)
	}
	original.Status = ec.Succeeded
	regenerate, err := restartLineage(request, original, "regenerate-contribution")
	if err != nil || *regenerate.ContributionGeneration != 4 || *regenerate.ContributionAttempt != 1 || regenerate.RegenerateOf == nil || *regenerate.RegenerateOf != original.ID || regenerate.RetryOf != nil {
		t.Fatal("regeneration must start a fresh canonical attempt", err)
	}
	for _, state := range []ec.Status{ec.Created, ec.Preparing, ec.Running, ec.Waiting, ec.Cancelled} {
		original.Status = state
		for _, action := range []string{"retry-contribution", "regenerate-contribution"} {
			if _, err = restartLineage(request, original, action); err == nil {
				t.Fatal("non-restartable Execution accepted", state, action)
			}
		}
	}
	maximum := f.Version(math.MaxInt64)
	original.Status = ec.Failed
	request.Lineage.ContributionAttempt = &maximum
	if _, err = restartLineage(request, original, "retry-contribution"); err == nil {
		t.Fatal("attempt overflow accepted")
	}
	request.Lineage.ContributionGeneration = &maximum
	original.Status = ec.Succeeded
	if _, err = restartLineage(request, original, "regenerate-contribution"); err == nil {
		t.Fatal("generation overflow accepted")
	}
	if contributionTurnActive("completed", request) || contributionTurnActive("finalizing", request) {
		t.Fatal("ordinary launch reopened a historical or finalizing Turn")
	}
	request.Lineage = regenerate
	if !contributionTurnActive("completed", request) || contributionTurnActive("cancelled", request) || contributionTurnActive("finalizing", request) {
		t.Fatal("replacement Turn gate lost its exact lifecycle scope")
	}
}
