package contract

import (
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func ptrVersion(v f.Version) *f.Version { return &v }
func TestResolveBindingIncludesFullActorAndSource(t *testing.T) {
	x := setup(t)
	r := x.resolve()
	a, e := id.NewHuman(fresh[id.User](t), fresh[id.Session](t))
	must(t, e)
	r.Actor = a
	b, e := ResolveBinding(r)
	must(t, e)
	u, e := f.ParseID[id.User](a.Details().UserID)
	must(t, e)
	r.Actor, e = id.NewHuman(u, fresh[id.Session](t))
	must(t, e)
	other, e := ResolveBinding(r)
	must(t, e)
	if b == other {
		t.Fatal("session omitted from plan binding")
	}
	r.Source = ""
	reject(t, r.Validate())
}

func TestMeetingSummaryResolveExactCallAndBinding(t *testing.T) {
	for _, purpose := range []Purpose{MeetingSummaryInitial, MeetingSummaryUpdate} {
		s := meetingSelectionRequest(t, purpose)
		actor, _ := id.NewHuman(fresh[id.User](t), fresh[id.Session](t))
		owner, _ := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, fresh[Call](t).String())
		r := ResolveRequest{Actor: actor, Consumer: s.Consumer, Purpose: purpose, Source: CurrentSelectionSource, Selection: &s.Selection, LeaseOwner: owner}
		must(t, r.Validate())
		binding, e := ResolveBinding(r)
		must(t, e)
		for name, change := range map[string]func(*ResolveRequest){
			"purpose": func(c *ResolveRequest) { c.Purpose = AgentGeneration },
			"execution-owner": func(c *ResolveRequest) {
				c.LeaseOwner, _ = sc.NewCredentialLeaseOwner(sc.ExecutionOwner, fresh[id.Execution](t).String())
			},
			"serving": func(c *ResolveRequest) { c.Source = ServingSnapshotSource },
			"model":   func(c *ResolveRequest) { c.ModelRef = ptr(fresh[Model](t)) },
		} {
			t.Run(name, func(t *testing.T) { c := r.Clone(); change(&c); reject(t, c.Validate()) })
		}
		for _, change := range []func(*ResolveRequest){
			func(c *ResolveRequest) { c.Consumer.MeetingID = fresh[struct{}](t).String() },
			func(c *ResolveRequest) { c.Consumer.OperationID = fresh[struct{}](t).String() },
			func(c *ResolveRequest) { c.Selection.Version = ptrVersion(1) },
			func(c *ResolveRequest) {
				c.LeaseOwner, _ = sc.NewCredentialLeaseOwner(sc.ModelCallOwner, fresh[Call](t).String())
			},
		} {
			c := r.Clone()
			change(&c)
			got, e := ResolveBinding(c)
			must(t, e)
			if got == binding {
				t.Fatal("request fact omitted from binding")
			}
		}
		if text := fmt.Sprintf("%+v", r); strings.Contains(text, r.Consumer.MeetingID) || text != "model_resolve_request" {
			t.Fatal("unsafe formatting")
		}
	}
}
func TestServingResolutionKeepsOldConfigAndNewCallOwner(t *testing.T) {
	x := setup(t)
	r := x.resolve()
	r.Consumer = Consumer{Kind: KnowledgeConsumer, ProjectID: x.c.ProjectID, Purpose: KnowledgeEmbedding, OperationID: fresh[struct{}](t).String()}
	r.Purpose = KnowledgeEmbedding
	r.Actor, _ = id.NewHuman(fresh[id.User](t), fresh[id.Session](t))
	r.Source = ServingSnapshotSource
	r.ModelRef = nil
	r.Selection = nil
	r.ServingSnapshotID = &x.snapshot.ID
	r.ServingGenerationID = fresh[struct{}](t).String()
	r.LeaseOwner, _ = sc.NewCredentialLeaseOwner(sc.ModelCallOwner, x.call.String())
	must(t, r.Validate())
	b, e := ResolveBinding(r)
	must(t, e)
	c := r.Clone()
	c.ServingGenerationID = fresh[struct{}](t).String()
	b2, e := ResolveBinding(c)
	must(t, e)
	if b == b2 {
		t.Fatal("generation omitted")
	}
	c = r.Clone()
	c.Selection = &SelectionRef{Kind: "platform", Selector: EmbeddingSelector}
	reject(t, c.Validate())
	c = r.Clone()
	c.LeaseOwner = x.owner
	reject(t, c.Validate())
	c = r.Clone()
	c.ReasoningEffort = "high"
	reject(t, c.Validate())
}
