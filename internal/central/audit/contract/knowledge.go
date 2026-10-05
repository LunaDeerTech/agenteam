package contract

import (
	"bytes"
	"encoding/json"
	"io"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	KnowledgeDeleteSubtree    Action       = "knowledge.delete_subtree"
	KnowledgeDocumentResource ResourceKind = "knowledge_document"
	KnowledgeProducer         Producer     = "knowledge"
)

func KnowledgeAction(a Action) bool { return a == KnowledgeDeleteSubtree }

// KnowledgeMetadataFields is the minimal dangerous-delete summary. It carries
// no document text, titles, object identities, confirmation token or node list.
type KnowledgeMetadataFields struct {
	ProjectID    string     `json:"project_id"`
	RootID       string     `json:"root_id"`
	InitiatorID  string     `json:"initiator_id"`
	ScopeDigest  f.Digest   `json:"scope_digest"`
	DeletedCount f.Progress `json:"deleted_count"`
}

func KnowledgeMetadata(action Action, v KnowledgeMetadataFields) (Metadata, error) {
	if !KnowledgeAction(action) || !validID(v.ProjectID) || !validID(v.RootID) || !validID(v.InitiatorID) || v.ScopeDigest.Validate() != nil || v.DeletedCount.Validate() != nil || v.DeletedCount == 0 {
		return Metadata{}, invalid("metadata")
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return Metadata{}, invalid("metadata")
	}
	d := metadataData{action: action, raw: string(raw)}
	return Metadata{data: func() metadataData { return d }}, nil
}
func (m Metadata) KnowledgeFields() (KnowledgeMetadataFields, error) {
	var v KnowledgeMetadataFields
	if m.data == nil || !KnowledgeAction(m.data().action) || json.Unmarshal([]byte(m.data().raw), &v) != nil {
		return v, invalid("metadata")
	}
	return v, nil
}
func decodeKnowledgeMetadata(action Action, raw []byte) (Metadata, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return Metadata{}, invalid("metadata")
	}
	seen := map[string]bool{}
	for d.More() {
		key, e := d.Token()
		name, ok := key.(string)
		if e != nil || !ok || seen[name] {
			return Metadata{}, invalid("metadata")
		}
		switch name {
		case "project_id", "root_id", "initiator_id", "scope_digest", "deleted_count":
		default:
			return Metadata{}, invalid("metadata")
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Metadata{}, invalid("metadata")
		}
	}
	if last, e := d.Token(); e != nil || last != json.Delim('}') {
		return Metadata{}, invalid("metadata")
	}
	if d.Decode(new(any)) != io.EOF || len(seen) != 5 {
		return Metadata{}, invalid("metadata")
	}
	var v KnowledgeMetadataFields
	if json.Unmarshal(raw, &v) != nil {
		return Metadata{}, invalid("metadata")
	}
	return KnowledgeMetadata(action, v)
}
func validateKnowledgeEntry(v EntryFields) error {
	m, e := v.Metadata.KnowledgeFields()
	if e != nil {
		return e
	}
	a, s, r := v.Actor.Details(), v.Scope.Details(), v.Resource.Details()
	if a.Kind != id.Human || v.Outcome != Success || s.Kind != id.ProjectScope || s.ProjectID != m.ProjectID || a.UserID != m.InitiatorID || r.Kind != KnowledgeDocumentResource || r.ID != m.RootID {
		return invalid("entry")
	}
	// This action has no execution/tool/approval/operation association. Transport
	// correlation is not part of its domain deletion facts either.
	if v.Associations != (Associations{}) {
		return invalid("association")
	}
	return nil
}
