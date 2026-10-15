package contract

import (
	"bytes"
	"encoding/json"
	"io"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	MeetingDeleteAccepted  Action       = "meeting.delete.accepted"
	MeetingDeleteCompleted Action       = "meeting.delete.completed"
	MeetingProducer        Producer     = "meeting"
	MeetingResource        ResourceKind = "meeting"
)

func MeetingAction(action Action) bool {
	return action == MeetingDeleteAccepted || action == MeetingDeleteCompleted
}

// Version is the immutable version at which deletion was accepted. Counts
// describe only Meeting's actual deleted rows, never external domain records.
type MeetingDeletionMetadataFields struct {
	MeetingID   string    `json:"meeting_id"`
	DeletionID  string    `json:"deletion_id"`
	InitiatorID string    `json:"initiator_id"`
	Version     f.Version `json:"version"`
	DeletedRows int64     `json:"deleted_rows,string"`
}

func MeetingDeletionMetadata(action Action, fields MeetingDeletionMetadataFields) (Metadata, error) {
	if !MeetingAction(action) || !validID(fields.MeetingID) || !validID(fields.DeletionID) || !validID(fields.InitiatorID) || fields.Version.Validate() != nil || fields.DeletedRows < 0 || action == MeetingDeleteAccepted && fields.DeletedRows != 0 || action == MeetingDeleteCompleted && fields.DeletedRows == 0 {
		return Metadata{}, invalid("metadata")
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return Metadata{}, invalid("metadata")
	}
	data := metadataData{action: action, raw: string(raw)}
	return Metadata{data: func() metadataData { return data }}, nil
}

func (m Metadata) MeetingDeletionFields() (MeetingDeletionMetadataFields, error) {
	var fields MeetingDeletionMetadataFields
	if m.data == nil || !MeetingAction(m.data().action) || json.Unmarshal([]byte(m.data().raw), &fields) != nil {
		return fields, invalid("metadata")
	}
	return fields, nil
}

func decodeMeetingDeletionMetadata(action Action, raw []byte) (Metadata, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return Metadata{}, invalid("metadata")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		name, ok := token.(string)
		if err != nil || !ok || seen[name] || name != "meeting_id" && name != "deletion_id" && name != "initiator_id" && name != "version" && name != "deleted_rows" {
			return Metadata{}, invalid("metadata")
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Metadata{}, invalid("metadata")
		}
	}
	if len(seen) != 5 {
		return Metadata{}, invalid("metadata")
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return Metadata{}, invalid("metadata")
	}
	if _, err = d.Token(); err != io.EOF {
		return Metadata{}, invalid("metadata")
	}
	var fields MeetingDeletionMetadataFields
	if json.Unmarshal(raw, &fields) != nil {
		return Metadata{}, invalid("metadata")
	}
	// Re-encoding rejects alternate integer spellings as well as an unbounded
	// JSON number, preserving one exact count representation for the SQL row.
	var wire map[string]json.RawMessage
	if json.Unmarshal(raw, &wire) != nil {
		return Metadata{}, invalid("metadata")
	}
	count, _ := json.Marshal(fields.DeletedRows)
	var text string
	if json.Unmarshal(wire["deleted_rows"], &text) != nil || text != string(count) {
		return Metadata{}, invalid("metadata")
	}
	return MeetingDeletionMetadata(action, fields)
}

func validateMeetingDeletionEntry(fields EntryFields) error {
	m, err := fields.Metadata.MeetingDeletionFields()
	if err != nil {
		return err
	}
	a, r := fields.Actor.Details(), fields.Resource.Details()
	if fields.Scope.Details().Kind != i.ProjectScope || fields.Outcome != Success || r.Kind != MeetingResource || r.ID != m.MeetingID || fields.Associations != (Associations{OperationID: m.DeletionID}) {
		return invalid("entry")
	}
	if fields.Action == MeetingDeleteAccepted {
		if a.Kind != i.Human || a.UserID != m.InitiatorID {
			return invalid("entry")
		}
	} else if a.Kind != i.Service || a.ServiceName != i.MeetingRuntime || a.CauseRef != m.DeletionID || a.ProjectID != fields.Scope.Details().ProjectID {
		return invalid("entry")
	}
	return nil
}
