package contract

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// MaxSkillRoundInputBytes matches the durable per-round Skill input bound.
const MaxSkillRoundInputBytes = 4 << 20

// SkillRoundInput is immutable input determined with the original successor
// commit. It never changes the captured Tool/Model/Mount/Secret Snapshot.
type SkillRoundInput struct {
	SchemaVersion    f.Version                `json:"schema_version"`
	ExecutionID      i.ExecutionID            `json:"execution_id"`
	SnapshotID       SnapshotID               `json:"snapshot_id"`
	RoundID          RoundID                  `json:"round_id"`
	SourceRoundID    RoundID                  `json:"source_round_id"`
	PreviousCutoff   f.Version                `json:"previous_cutoff"`
	AssignmentCutoff f.Version                `json:"assignment_cutoff"`
	PreviousBindings []sc.SkillBinding        `json:"previous_bindings"`
	Bindings         []sc.SkillBinding        `json:"bindings"`
	Dispositions     []SkillChangeDisposition `json:"dispositions"`
	Additions        []SkillAddition          `json:"additions"`
	ControlSequence  *f.Sequence              `json:"control_sequence"`
	CreatedAt        f.Instant                `json:"created_at"`
}

func (v SkillRoundInput) Validate() error {
	if v.SchemaVersion != 1 || v.ExecutionID.Validate() != nil || v.SnapshotID.Validate() != nil || v.RoundID.Validate() != nil || v.SourceRoundID.Validate() != nil || v.RoundID == v.SourceRoundID || v.PreviousCutoff.Validate() != nil || v.AssignmentCutoff < v.PreviousCutoff || v.CreatedAt.Validate() != nil || v.PreviousBindings == nil || v.Bindings == nil || v.Dispositions == nil || v.Additions == nil {
		return invalid()
	}
	directory := map[sc.SkillID]sc.SkillBinding{}
	for n, b := range v.PreviousBindings {
		if b.Validate() != nil || b.AssignmentSequence > v.PreviousCutoff || n > 0 && v.PreviousBindings[n-1].SkillID.String() >= b.SkillID.String() {
			return invalid()
		}
		directory[b.SkillID] = b
	}
	applied := map[sc.AssignmentChangeID]f.Version{}
	seen := map[sc.AssignmentChangeID]bool{}
	previous := v.PreviousCutoff
	for _, d := range v.Dispositions {
		if d.ChangeID.Validate() != nil || seen[d.ChangeID] || d.Sequence <= previous || d.Sequence > v.AssignmentCutoff {
			return invalid()
		}
		seen[d.ChangeID] = true
		previous = d.Sequence
		switch d.State {
		case "applied":
			if d.Reason != "" {
				return invalid()
			}
			applied[d.ChangeID] = d.Sequence
		case "skipped":
			if d.Reason != "removed" && d.Reason != "disabled" && d.Reason != "deleted" && d.Reason != "superseded" {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	if len(applied) != len(v.Additions) || (len(applied) == 0) != (v.ControlSequence == nil) || v.ControlSequence != nil && *v.ControlSequence < 4 {
		return invalid()
	}
	for n, a := range v.Additions {
		if a.Binding.Validate() != nil || applied[a.ChangeID] != a.Binding.AssignmentSequence || n > 0 && v.Additions[n-1].Binding.SkillID.String() >= a.Binding.SkillID.String() {
			return invalid()
		}
		if old, ok := directory[a.Binding.SkillID]; ok {
			if a.ReplacedAssignmentID == nil || *a.ReplacedAssignmentID != old.AssignmentID || old.AssignmentID == a.Binding.AssignmentID || old.AssignmentSequence >= a.Binding.AssignmentSequence {
				return invalid()
			}
		} else if a.ReplacedAssignmentID != nil {
			return invalid()
		}
		directory[a.Binding.SkillID] = a.Binding
		delete(applied, a.ChangeID)
	}
	if len(applied) != 0 || len(directory) != len(v.Bindings) {
		return invalid()
	}
	for n, b := range v.Bindings {
		if b.Validate() != nil || directory[b.SkillID] != b || n > 0 && v.Bindings[n-1].SkillID.String() >= b.SkillID.String() {
			return invalid()
		}
	}
	return nil
}

func (v SkillRoundInput) Clone() SkillRoundInput {
	v.PreviousBindings = slices.Clone(v.PreviousBindings)
	v.Bindings = slices.Clone(v.Bindings)
	v.Dispositions = slices.Clone(v.Dispositions)
	v.Additions = slices.Clone(v.Additions)
	for n, a := range v.Additions {
		if a.ReplacedAssignmentID != nil {
			p := *a.ReplacedAssignmentID
			v.Additions[n].ReplacedAssignmentID = &p
		}
	}
	if v.ControlSequence != nil {
		n := *v.ControlSequence
		v.ControlSequence = &n
	}
	return v
}

func (v SkillRoundInput) CanonicalBytes() ([]byte, error) {
	if v.Validate() != nil {
		return nil, invalid()
	}
	b, e := json.Marshal(v)
	if e != nil || len(b) > MaxSkillRoundInputBytes {
		return nil, invalid()
	}
	return cursor.CanonicalJSON(b)
}

func DecodeSkillRoundInput(raw []byte) (SkillRoundInput, error) {
	var v SkillRoundInput
	if len(raw) > MaxSkillRoundInputBytes || directTextDecode(raw, &v) != nil {
		return v, invalid()
	}
	b, e := v.CanonicalBytes()
	if e != nil || !bytes.Equal(raw, b) {
		return SkillRoundInput{}, invalid()
	}
	return v, nil
}

// ControlBytes is the typed transcript record; ModelMessage is its bounded
// textual projection. Both describe the same committed addition identities.
func (v SkillRoundInput) ControlBytes() ([]byte, error) {
	if v.Validate() != nil {
		return nil, invalid()
	}
	if v.ControlSequence == nil {
		return nil, nil
	}
	b, e := json.Marshal(struct {
		Kind               string          `json:"kind"`
		SchemaVersion      f.Version       `json:"schema_version"`
		ExecutionID        i.ExecutionID   `json:"execution_id"`
		RoundID            RoundID         `json:"round_id"`
		SourceRoundID      RoundID         `json:"source_round_id"`
		AssignmentCutoff   f.Version       `json:"assignment_cutoff"`
		Payload            []SkillAddition `json:"payload"`
		TranscriptPosition f.Sequence      `json:"transcript_position"`
	}{"skills_added", 1, v.ExecutionID, v.RoundID, v.SourceRoundID, v.AssignmentCutoff, v.Additions, *v.ControlSequence})
	if e != nil {
		return nil, e
	}
	return cursor.CanonicalJSON(b)
}

func (v SkillRoundInput) ModelMessage() (*mc.Message, error) {
	b, e := v.ControlBytes()
	if e != nil || b == nil {
		return nil, e
	}
	m := mc.Message{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: string(b)}}}}
	if m.Validate() != nil {
		return nil, invalid()
	}
	return &m, nil
}
