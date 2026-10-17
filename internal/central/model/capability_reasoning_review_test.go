package model

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// This checks the production Agent selection boundary with the existing owned
// row/transaction fixture. The PostgreSQL and wire seam is exercised separately.
func TestCapabilityReasoningReviewAgentSelection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		choice *string
		empty  bool
		want   f.Code
	}{
		{name: "declared-custom-value", choice: capabilityReviewChoice("vendor-depth")},
		{name: "unselected-adjustable-model"},
		{name: "unselected-fixed-model", empty: true},
		{name: "display-name-is-not-value", choice: capabilityReviewChoice("Deep"), want: f.CapabilityUnsupported},
		{name: "undeclared-value", choice: capabilityReviewChoice("high"), want: f.CapabilityUnsupported},
		{name: "fixed-model-rejects-explicit-value", choice: capabilityReviewChoice("vendor-depth"), empty: true, want: f.CapabilityUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := newSelectionFixture(t, false)
			x.caps.Reasoning = true
			if !tc.empty {
				x.caps.ReasoningEfforts = []mc.ReasoningEffort{{Name: "Deep", Value: "vendor-depth"}}
			}
			x.request.ReasoningEffort = tc.choice
			plan, err := x.service.DiscoverConfigurationSelection(context.Background(), x.request)
			if tc.want != "" {
				requireCode(t, err, tc.want)
				if plan.Validate() == nil {
					t.Fatal("rejected selection issued a plan")
				}
				return
			}
			if err != nil {
				t.Fatal("valid selection refused", err)
			}
			x.store.tx, x.store.locks = f.NewTx(), plan.RequiredLocks()
			facts, err := x.service.RequireConfigurationSelectionInTx(context.Background(), x.store.tx, x.request, plan)
			if err != nil || facts.ModelID != x.request.ModelID {
				t.Fatal("final selection refused", err)
			}
			if !tc.empty {
				facts.Capabilities.ReasoningEfforts[0].Name = "changed result"
				facts.Capabilities.ReasoningEfforts[0].Value = "changed-result"
				if got := plan.Details().Candidate.Capabilities.ReasoningEfforts[0]; got.Name != "Deep" || got.Value != "vendor-depth" {
					t.Fatal("returned name/value aliases the issued plan")
				}
			}
		})
	}
}

func capabilityReviewChoice(value string) *string { return &value }
