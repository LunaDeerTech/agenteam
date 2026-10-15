package contract

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestSingleContributionCancellationRequiresExactTarget(t *testing.T) {
	turn, contribution := fresh[Turn](t), fresh[Contribution](t)
	r := ControlRequest{ProjectID: fresh[i.Project](t), MeetingID: fresh[Meeting](t), Action: "cancel-contribution", TurnID: &turn, ContributionID: &contribution, ExpectedVersion: 1, Meta: f.CommandMeta{RequestID: fresh[f.Request](t), IdempotencyKey: "cancel-current-contribution"}}
	if r.Validate() != nil {
		t.Fatal("complete cancellation target rejected")
	}
	for _, change := range []func(*ControlRequest){
		func(v *ControlRequest) { v.TurnID = nil },
		func(v *ControlRequest) { v.ContributionID = nil },
		func(v *ControlRequest) { v.Action = "cancel-turn" },
		func(v *ControlRequest) { v.ExpectedVersion = 0 },
	} {
		v := r
		change(&v)
		if v.Validate() == nil {
			t.Fatal("cancellation accepted an ambiguous target or absent version")
		}
	}
}
