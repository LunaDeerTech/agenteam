package meeting

import (
	"context"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestDecisionInputCannotSmuggleAuthorityOrDuplicateFields(t *testing.T) {
	valid, err := ParseDecisionInput([]byte(`{"question":"Use the proposed schedule?","options":["accept","revise"]}`))
	if err != nil || len(valid.Options) != 2 {
		t.Fatal("valid decision input", err)
	}
	for _, raw := range []string{
		`{"question":"first","question":"second"}`,
		`{"question":"Use it?","execution_id":"00000000-0000-4000-8000-000000000001"}`,
		`{"question":"Use it?","options":["yes","yes"]}`,
		`{"question":"   "}`,
		`{"question":"Use it?"} {}`,
	} {
		if _, err := ParseDecisionInput([]byte(raw)); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
}

func TestDecisionAuthorityRejectsCallerConstructedWitness(t *testing.T) {
	var authority Authority
	if err := authority.RequireMeetingDecisionCallInTx(context.Background(), f.Tx{}, ec.MeetingDecisionCall{}, ec.Trigger{}, "", "create"); err == nil {
		t.Fatal("DTO granted decision control")
	}
	if _, err := authority.DiscoverMeetingDecisionResult(context.Background(), tc.RunnerResultBinding{}); err == nil {
		t.Fatal("invalid result planned")
	}
}

func TestDecisionResultOnlyPublishesExplicitAnswerOrSkip(t *testing.T) {
	id, err := f.NewID[c.Decision]()
	if err != nil {
		t.Fatal(err)
	}
	answer := "Use the smaller batch"
	for _, result := range []c.DecisionResult{{DecisionID: id, Version: 2, Status: "answered", Answer: &answer}, {DecisionID: id, Version: 2, Status: "skipped"}} {
		if result.Validate() != nil {
			t.Fatal("resolved decision rejected")
		}
	}
	for _, result := range []c.DecisionResult{{DecisionID: id, Version: 1, Status: "pending"}, {DecisionID: id, Version: 2, Status: "cancelled"}, {DecisionID: id, Version: 2, Status: "answered"}, {DecisionID: id, Version: 2, Status: "skipped", Answer: &answer}} {
		if result.Validate() == nil {
			t.Fatal("unresolved or ambiguous decision published")
		}
	}
}
