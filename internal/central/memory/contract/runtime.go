package contract

import f "github.com/LunaDeerTech/agenteam/internal/central/foundation"

type RetainInput struct {
	Content       string `json:"content"`
	SourceContext string `json:"source_context,omitempty"`
	Intent        string `json:"intent,omitempty"`
}

func (v RetainInput) Validate() error {
	if !validText(v.Content, MaxContentBytes) || v.SourceContext != "" && !validText(v.SourceContext, 4096) || v.Intent != "" && !validText(v.Intent, 1024) {
		return invalid()
	}
	return nil
}

type Filters struct {
	Types []Type   `json:"types,omitempty"`
	Tags  []string `json:"tags,omitempty"`
}

func (v Filters) Validate() error {
	if len(v.Types) > 4 || len(v.Tags) > MaxTags {
		return invalid()
	}
	types := map[Type]bool{}
	tags := map[string]bool{}
	for _, t := range v.Types {
		if !t.Valid() || types[t] {
			return invalid()
		}
		types[t] = true
	}
	for _, t := range v.Tags {
		if !validText(t, 64) || tags[t] {
			return invalid()
		}
		tags[t] = true
	}
	return nil
}

type RecallInput struct {
	Query   string  `json:"query"`
	Filters Filters `json:"filters,omitempty"`
	Limit   int     `json:"limit,omitempty"`
}

func (v RecallInput) Validate() error {
	if !validText(v.Query, MaxQueryBytes) || v.Filters.Validate() != nil || v.Limit < 0 || v.Limit > MaxRecall {
		return invalid()
	}
	return nil
}

type ReflectInput struct {
	Query string `json:"query"`
}

func (v ReflectInput) Validate() error {
	if !validText(v.Query, MaxQueryBytes) {
		return invalid()
	}
	return nil
}

type RecallResult struct {
	Memories []Revision `json:"memories"`
	Mode     string     `json:"mode"`
	Degraded bool       `json:"degraded"`
}
type ReflectResult struct {
	Answer     string   `json:"answer"`
	MemoryRefs []Target `json:"memory_refs"`
}
type RetainResult struct {
	Added        []Target `json:"added"`
	Updated      []Target `json:"updated"`
	DeletedCount int      `json:"deleted_count"`
	NoopCount    int      `json:"noop_count"`
	Indexing     string   `json:"indexing,omitempty"`
}

type ActionKind string

const (
	Add    ActionKind = "ADD"
	Update ActionKind = "UPDATE"
	Delete ActionKind = "DELETE"
	Noop   ActionKind = "NOOP"
)

// Action is only a model proposal. It carries no authority to apply a change.
// Empty target fields are encoded as empty strings by the structured schema.
type Action struct {
	Kind             ActionKind `json:"action"`
	TargetID         string     `json:"target_memory_id"`
	ExpectedRevision string     `json:"expected_revision"`
	Content          Content    `json:"content"`
	Reason           string     `json:"reason"`
}

func (v Action) Validate() error {
	if !validText(v.Reason, 2048) {
		return invalid()
	}
	switch v.Kind {
	case Add:
		if v.TargetID != "" || v.ExpectedRevision != "" || v.Content.Validate() != nil {
			return invalid()
		}
	case Update, Delete:
		if !uuid(v.TargetID) {
			return invalid()
		}
		var version f.Version
		if version.UnmarshalJSON([]byte(`"`+v.ExpectedRevision+`"`)) != nil {
			return invalid()
		}
		if v.Kind == Update && v.Content.Validate() != nil {
			return invalid()
		}
		if v.Kind == Delete && !emptyContent(v.Content) {
			return invalid()
		}
	case Noop:
		if v.TargetID != "" || v.ExpectedRevision != "" || !emptyContent(v.Content) {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}
func emptyContent(v Content) bool {
	return v.Type == "" && v.Text == "" && v.Importance == "" && len(v.Tags) == 0
}

type Extraction struct {
	Candidates []Content `json:"candidates"`
}

func (v Extraction) Validate() error {
	if v.Candidates == nil || len(v.Candidates) > MaxCandidates {
		return invalid()
	}
	for _, x := range v.Candidates {
		if x.Validate() != nil {
			return invalid()
		}
	}
	return nil
}

type Proposal struct {
	Actions []Action `json:"actions"`
}

func (v Proposal) Validate() error {
	if v.Actions == nil || len(v.Actions) > MaxActions {
		return invalid()
	}
	seen := map[string]bool{}
	for _, a := range v.Actions {
		if a.Validate() != nil {
			return invalid()
		}
		if a.TargetID != "" {
			if seen[a.TargetID] {
				return invalid()
			}
			seen[a.TargetID] = true
		}
	}
	return nil
}
