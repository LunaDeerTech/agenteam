package contract

import (
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestStructuredProposalsEnforceDomainBounds(t *testing.T) {
	content := Content{Procedure, "Check migration rollback", []string{"migration"}, Normal}
	for _, edit := range []func(*Content){
		func(v *Content) { v.Text = "" },
		func(v *Content) { v.Text = strings.Repeat("界", MaxMemoryBytes/3+1) },
		func(v *Content) { v.Tags = []string{strings.Repeat("x", 65)} },
		func(v *Content) { v.Tags = []string{""} },
		func(v *Content) { v.Tags = []string{"migration", "migration"} },
		func(v *Content) {
			v.Tags = []string{}
			for n := 0; n <= MaxTags; n++ {
				v.Tags = append(v.Tags, fmt.Sprint(n))
			}
		},
	} {
		bad := content.Clone()
		edit(&bad)
		if (Extraction{Candidates: []Content{bad}}).Validate() == nil || (Action{Kind: Add, Content: bad, Reason: "durable"}).Validate() == nil {
			t.Fatal("provider shape bypassed domain content bounds")
		}
	}
	extraction := Extraction{Candidates: make([]Content, MaxCandidates)}
	for n := range extraction.Candidates {
		extraction.Candidates[n] = content.Clone()
	}
	if extraction.Validate() != nil {
		t.Fatal("maximum candidate count rejected")
	}
	extraction.Candidates = append(extraction.Candidates, content)
	if extraction.Validate() == nil {
		t.Fatal("oversized candidate set accepted")
	}
	action := Action{Kind: Add, Content: content, Reason: "durable"}
	proposal := Proposal{Actions: make([]Action, MaxActions)}
	for n := range proposal.Actions {
		proposal.Actions[n] = action
	}
	if proposal.Validate() != nil {
		t.Fatal("maximum action count rejected")
	}
	proposal.Actions = append(proposal.Actions, action)
	if proposal.Validate() == nil {
		t.Fatal("oversized proposal accepted")
	}
	for _, reason := range []string{"", strings.Repeat("x", 2049)} {
		action.Reason = reason
		if action.Validate() == nil {
			t.Fatal("invalid reason bound accepted")
		}
	}
}

func TestRetainDecodeRejectsNamespaceAndAmbiguousJSON(t *testing.T) {
	for _, raw := range []string{`{"content":"lesson","agent_id":"other"}`, `{"content":"one","content":"two"}`, `{"content":null}`, `{"content":"lesson"} {}`, `{"content":"lesson","source_context":{"x":"y"}}`} {
		var v RetainInput
		if Decode([]byte(raw), &v, MaxContentBytes) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	var v RetainInput
	if err := Decode([]byte(`{"content":"先验证迁移，再更新服务"}`), &v, MaxContentBytes); err != nil || v.Validate() != nil {
		t.Fatal(err)
	}
}
func TestProposalRequiresExpectedRevisionAndUniqueTargets(t *testing.T) {
	id, _ := f.NewID[Memory]()
	good := Action{Kind: Update, TargetID: id.String(), ExpectedRevision: "3", Content: Content{Fact, "A durable fact", []string{}, Normal}, Reason: "correct stale fact"}
	if (Proposal{[]Action{good}}).Validate() != nil {
		t.Fatal("valid update rejected")
	}
	for _, v := range []Proposal{{[]Action{good, good}}, {[]Action{{Kind: Delete, TargetID: id.String(), Reason: "remove stale record"}}}, {nil}} {
		if v.Validate() == nil {
			t.Fatal("invalid proposal accepted")
		}
	}
	good.ExpectedRevision = "03"
	if good.Validate() == nil {
		t.Fatal("noncanonical revision accepted")
	}
}
