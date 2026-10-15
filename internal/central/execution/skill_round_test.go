package execution

import (
	"bytes"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func TestSkillRoundConsumesOnlyNewValidAssignmentAtCutoff(t *testing.T) {
	source, changes := roundInputSelectionControl(t)
	p := &skillRoundPlan{run: &directTextCall{request: c.PreparationRequest{ExecutionID: source.row.summary.ID}}, source: source.original.round.Fields().ID, snapshot: source.original.snapshot.Fields().ID, previous: 2, cutoff: 7, bindings: source.initial.Initial.Bindings, pending: changes}
	tr, err := buildSkillRoundTransition(p, []sc.AssignmentValidity{"", sc.AssignmentRemoved, sc.AssignmentValid})
	if err != nil {
		t.Fatal(err)
	}
	v, err := tr.roundInput(newTestID[c.Round](t), source.receipt.CreatedAt, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Dispositions) != 2 || len(v.Additions) != 1 || v.Bindings[0] != changes[2].Binding || v.Dispositions[0].Reason != "removed" || v.PreviousBindings[0] != changes[0].Binding {
		t.Fatal("cutoff or immutable prior binding changed")
	}
	raw, err := v.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := c.DecodeSkillRoundInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	control, err := roundTrip.ControlBytes()
	if err != nil {
		t.Fatal(err)
	}
	message, err := roundTrip.ModelMessage()
	if err != nil || message == nil || !bytes.Equal([]byte(message.Parts[0].Text.Text), control) {
		t.Fatal("model/transcript control mismatch")
	}
	bad := roundTrip.Clone()
	bad.AssignmentCutoff = f.Version(5)
	if bad.Validate() == nil {
		t.Fatal("post-cutoff assignment accepted")
	}
	tr, err = buildSkillRoundTransition(p, []sc.AssignmentValidity{"", sc.AssignmentRemoved, sc.AssignmentDisabled})
	if err != nil {
		t.Fatal(err)
	}
	noChange, err := tr.roundInput(newTestID[c.Round](t), source.receipt.CreatedAt, 4)
	if err != nil || noChange.ControlSequence != nil || noChange.Bindings[0] != p.bindings[0] {
		t.Fatal("revocation created a new binding/control")
	}
}
