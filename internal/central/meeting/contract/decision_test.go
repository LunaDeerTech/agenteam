package contract

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"testing"
)

func TestRespondDecisionRequiresExplicitVersionedHumanChoice(t *testing.T) {
	answer := "Proceed with option B"
	r := RespondDecisionRequest{ProjectID: fresh[i.Project](t), MeetingID: fresh[Meeting](t), DecisionID: fresh[Decision](t), Action: "answer", ExpectedVersion: 1, Answer: &answer, Meta: f.CommandMeta{RequestID: fresh[f.Request](t), IdempotencyKey: "decision-answer"}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*RespondDecisionRequest){
		func(v *RespondDecisionRequest) { v.ExpectedVersion = 0 },
		func(v *RespondDecisionRequest) { v.Action = "resume" },
		func(v *RespondDecisionRequest) { v.Answer = nil },
		func(v *RespondDecisionRequest) { v.Action = "skip" },
		func(v *RespondDecisionRequest) { blank := " \n"; v.Answer = &blank },
	} {
		bad := r
		change(&bad)
		if bad.Validate() == nil {
			t.Fatal("ambiguous response accepted")
		}
	}
	r.Action = "skip"
	r.Answer = nil
	if err := r.Validate(); err != nil {
		t.Fatal("explicit skip rejected", err)
	}
}
