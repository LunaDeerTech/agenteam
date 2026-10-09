package work

import (
	"encoding/json"
	"testing"
)

// The existing helper supplies a valid persisted record only. Acceptance of
// noncanonical nested keys is judged here against the card's persisted closed
// schema rule, independently of the author's expected-result helpers.
func TestIndependentTaskPrivateNestedKeys(t *testing.T) {
	for _, kind := range []string{"placement-case", "rank-case-alias"} {
		t.Run(kind, func(t *testing.T) {
			record, actor, _ := pureTaskRecord(t)
			raw, err := json.Marshal(record.Plan)
			if err != nil {
				t.Fatal("seed encoding failed")
			}
			var object map[string]any
			if json.Unmarshal(raw, &object) != nil {
				t.Fatal("seed object failed")
			}
			switch kind {
			case "placement-case":
				placement := object["placement"].(map[string]any)
				placement["STATE"] = placement["state"]
				delete(placement, "state")
			case "rank-case-alias":
				groups := object["groups"].([]any)
				group := groups[0].(map[string]any)
				item := group["after"].([]any)[0].(map[string]any)
				item["rank"] = item["Rank"]
			}
			raw, err = json.Marshal(object)
			if err != nil {
				t.Fatal("changed encoding failed")
			}
			plan, err := decodePrivate[taskPlan](raw, taskPlanCap, taskPlanFields)
			if err != nil {
				return
			}
			record.Plan = &plan
			if validateTaskRecord(record, actor) == nil {
				t.Fatal("noncanonical nested private key accepted through decode and record validation")
			}
		})
	}
}
