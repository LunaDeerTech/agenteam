package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

const MaxRoundInputBytes = 4 << 20

type ControlInput struct{}
type ControlInputID = f.ID[ControlInput]

// These explicit persistence projections are facts, never invocation grants.
// The original Model Round and its tool_pending checkpoint remain immutable.
type SkillChangeDisposition struct {
	ChangeID sc.AssignmentChangeID `json:"change_id"`
	Sequence f.Version             `json:"assignment_sequence"`
	State    string                `json:"state"`
	Reason   string                `json:"reason"`
}
type SkillAddition struct {
	ChangeID             sc.AssignmentChangeID `json:"change_id"`
	Binding              sc.SkillBinding       `json:"binding"`
	ReplacedAssignmentID *sc.AssignmentID      `json:"replaced_assignment_id"`
}
type SkillsAddedControl struct {
	ID                 ControlInputID  `json:"id"`
	ExecutionID        i.ExecutionID   `json:"execution_id"`
	RoundID            RoundID         `json:"round_id"`
	Kind               string          `json:"kind"`
	SourceID           RoundID         `json:"source_id"`
	SchemaVersion      f.Version       `json:"schema_version"`
	Payload            []SkillAddition `json:"payload"`
	TranscriptPosition f.Sequence      `json:"transcript_position"`
}

type RoundInputFields struct {
	SchemaVersion          f.Version                    `json:"schema_version"`
	RoundID                RoundID                      `json:"round_id"`
	ExecutionID            i.ExecutionID                `json:"execution_id"`
	SnapshotID             SnapshotID                   `json:"snapshot_id"`
	SnapshotDigest         f.Digest                     `json:"snapshot_digest"`
	PredecessorRoundID     RoundID                      `json:"predecessor_round_id"`
	Source                 ToolResultConsumptionReceipt `json:"source_consumption"`
	Initial                sc.InitialSkillBindings      `json:"initial_bindings"`
	SkillInputBindingID    InputBindingID               `json:"skill_input_binding_id"`
	Bindings               []sc.SkillBinding            `json:"bindings"`
	AppliedChangeIDs       []sc.AssignmentChangeID      `json:"applied_change_ids"`
	Dispositions           []SkillChangeDisposition     `json:"dispositions"`
	AssignmentCutoff       f.Version                    `json:"assignment_cutoff"`
	TranscriptThrough      f.Sequence                   `json:"transcript_through"`
	TranscriptPrefixDigest f.Digest                     `json:"transcript_prefix_digest"`
	CompactionRef          *string                      `json:"compaction_ref"`
	Control                *SkillsAddedControl          `json:"control"`
	CheckpointVersion      f.Version                    `json:"checkpoint_version"`
	CreatedAt              f.Instant                    `json:"created_at"`
}

func (v RoundInputFields) Clone() RoundInputFields {
	v.Initial = v.Initial.Clone()
	v.Bindings = slices.Clone(v.Bindings)
	v.AppliedChangeIDs = slices.Clone(v.AppliedChangeIDs)
	v.Dispositions = slices.Clone(v.Dispositions)
	if v.Control != nil {
		c := *v.Control
		c.Payload = slices.Clone(c.Payload)
		for n := range c.Payload {
			if p := c.Payload[n].ReplacedAssignmentID; p != nil {
				q := *p
				c.Payload[n].ReplacedAssignmentID = &q
			}
		}
		v.Control = &c
	}
	if v.CompactionRef != nil {
		q := *v.CompactionRef
		v.CompactionRef = &q
	}
	return v
}
func (v RoundInputFields) Validate() error {
	if v.SchemaVersion != 1 || v.RoundID.Validate() != nil || v.ExecutionID.Validate() != nil || v.SnapshotID.Validate() != nil || v.SnapshotDigest.Validate() != nil || v.PredecessorRoundID.Validate() != nil || v.RoundID == v.PredecessorRoundID || v.Source.Validate() != nil || v.Source.ExecutionID != v.ExecutionID || v.Source.RoundID != v.PredecessorRoundID || v.Initial.Validate() != nil || v.Initial.Request.ExecutionID != v.ExecutionID || v.SkillInputBindingID.Validate() != nil || v.AssignmentCutoff < v.Initial.AssignmentSequence || v.CompactionRef != nil || v.TranscriptPrefixDigest.Validate() != nil || v.CheckpointVersion <= v.Source.Version || v.CreatedAt.Validate() != nil || v.CreatedAt.Time().Before(v.Source.CreatedAt.Time()) || v.Bindings == nil || v.AppliedChangeIDs == nil || v.Dispositions == nil {
		return invalid()
	}
	directory := make(map[sc.SkillID]sc.SkillBinding, len(v.Initial.Bindings))
	for _, b := range v.Initial.Bindings {
		directory[b.SkillID] = b
	}
	applied := make(map[sc.AssignmentChangeID]f.Version)
	ids := make([]sc.AssignmentChangeID, 0)
	var previous f.Version
	for _, d := range v.Dispositions {
		if d.ChangeID.Validate() != nil || d.Sequence <= 1 || d.Sequence > v.AssignmentCutoff || d.Sequence <= previous {
			return invalid()
		}
		previous = d.Sequence
		switch d.State {
		case "covered":
			if d.Sequence > v.Initial.AssignmentSequence || d.Reason != "" {
				return invalid()
			}
		case "skipped":
			if d.Sequence <= v.Initial.AssignmentSequence || (d.Reason != "removed" && d.Reason != "disabled" && d.Reason != "deleted" && d.Reason != "superseded") {
				return invalid()
			}
		case "applied":
			if d.Sequence <= v.Initial.AssignmentSequence || d.Reason != "" {
				return invalid()
			}
			applied[d.ChangeID] = d.Sequence
			ids = append(ids, d.ChangeID)
		default:
			return invalid()
		}
	}
	if !slices.Equal(ids, v.AppliedChangeIDs) {
		return invalid()
	}
	if len(applied) == 0 {
		if v.Control != nil || v.TranscriptThrough != 3 {
			return invalid()
		}
	} else {
		c := v.Control
		if c == nil || c.ID.Validate() != nil || c.ExecutionID != v.ExecutionID || c.RoundID != v.RoundID || c.SourceID != v.RoundID || c.Kind != "skills_added" || c.SchemaVersion != 1 || c.TranscriptPosition != 4 || v.TranscriptThrough != 4 || len(c.Payload) != len(applied) {
			return invalid()
		}
		for n, a := range c.Payload {
			if a.Binding.Validate() != nil || applied[a.ChangeID] != a.Binding.AssignmentSequence || n > 0 && c.Payload[n-1].Binding.SkillID.String() >= a.Binding.SkillID.String() {
				return invalid()
			}
			if old, ok := directory[a.Binding.SkillID]; ok {
				if a.ReplacedAssignmentID == nil || *a.ReplacedAssignmentID != old.AssignmentID || old.AssignmentSequence >= a.Binding.AssignmentSequence || old.AssignmentID == a.Binding.AssignmentID {
					return invalid()
				}
			} else if a.ReplacedAssignmentID != nil {
				return invalid()
			}
			directory[a.Binding.SkillID] = a.Binding
			delete(applied, a.ChangeID)
		}
		if len(applied) != 0 {
			return invalid()
		}
	}
	if len(directory) != len(v.Bindings) {
		return invalid()
	}
	for n, b := range v.Bindings {
		if b.Validate() != nil || b.AssignmentSequence > v.AssignmentCutoff || directory[b.SkillID] != b || n > 0 && v.Bindings[n-1].SkillID.String() >= b.SkillID.String() {
			return invalid()
		}
	}
	return nil
}

// RoundInput is immutable canonical input. Constructing or decoding it does
// not prove a commit; only the original writer or durable Lookup does that.
type RoundInput struct {
	data func() (RoundInputFields, []byte, f.Digest)
}

func NewRoundInput(v RoundInputFields) (RoundInput, error) {
	if v.Validate() != nil {
		return RoundInput{}, invalid()
	}
	v = v.Clone()
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > MaxRoundInputBytes {
		return RoundInput{}, invalid()
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return RoundInput{}, invalid()
	}
	digest := TriggerInputDigest(raw)
	return RoundInput{func() (RoundInputFields, []byte, f.Digest) { return v.Clone(), bytes.Clone(raw), digest }}, nil
}
func (v RoundInput) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}
func (v RoundInput) Fields() RoundInputFields {
	if v.data == nil {
		return RoundInputFields{}
	}
	f, _, _ := v.data()
	return f
}
func (v RoundInput) CanonicalBytes() []byte {
	if v.data == nil {
		return nil
	}
	_, b, _ := v.data()
	return b
}
func (v RoundInput) Digest() f.Digest {
	if v.data == nil {
		return ""
	}
	_, _, d := v.data()
	return d
}
func DecodeRoundInput(raw []byte) (RoundInput, error) {
	var fields RoundInputFields
	if len(raw) > MaxRoundInputBytes || directTextDecode(raw, &fields) != nil {
		return RoundInput{}, invalid()
	}
	v, err := NewRoundInput(fields)
	if err != nil || !bytes.Equal(raw, v.CanonicalBytes()) {
		return RoundInput{}, invalid()
	}
	return v, nil
}
func (RoundInput) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "execution_round_input") }
func (RoundInput) LogValue() slog.Value         { return slog.StringValue("execution_round_input") }
func (RoundInput) MarshalJSON() ([]byte, error) { return []byte(`"execution_round_input"`), nil }
func (*RoundInput) UnmarshalJSON([]byte) error  { return invalid() }

type RoundInputPlan interface{ RequiredLocks() []f.LockRequest }
