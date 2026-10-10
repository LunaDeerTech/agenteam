package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	RunnerCreate          Action       = "runner.create"
	RunnerUpdate          Action       = "runner.update"
	RunnerEnrollmentIssue Action       = "runner.enrollment.issue"
	RunnerEnroll          Action       = "runner.enroll"
	RunnerRevoke          Action       = "runner.revoke"
	RunnerProducer        Producer     = "runner"
	RunnerResource        ResourceKind = "runner"
)

func RunnerAction(a Action) bool {
	return a == RunnerCreate || a == RunnerUpdate || a == RunnerEnrollmentIssue || a == RunnerEnroll || a == RunnerRevoke
}

type RunnerMetadataFields struct {
	RunnerID             string    `json:"runner_id"`
	Version              f.Version `json:"version"`
	CredentialGeneration f.Version `json:"credential_generation"`
	ChangedFields        []string  `json:"changed_fields"`
	PublicKeyFingerprint *f.Digest `json:"public_key_fingerprint,omitempty"`
}

func RunnerMetadata(action Action, v RunnerMetadataFields) (Metadata, error) {
	if !RunnerAction(action) || !validID(v.RunnerID) || v.Version.Validate() != nil || v.CredentialGeneration.Validate() != nil {
		return Metadata{}, invalid("metadata")
	}
	if action == RunnerCreate {
		if v.Version != 1 || v.CredentialGeneration != 1 || !slices.Equal(v.ChangedFields, []string{"created"}) {
			return Metadata{}, invalid("metadata")
		}
	} else {
		if v.Version < 2 {
			return Metadata{}, invalid("metadata")
		}
		if action == RunnerUpdate {
			if len(v.ChangedFields) == 0 || len(v.ChangedFields) > 3 {
				return Metadata{}, invalid("metadata")
			}
			for n, field := range v.ChangedFields {
				if field != "description" && field != "name" && field != "tags" || n > 0 && v.ChangedFields[n-1] >= field {
					return Metadata{}, invalid("metadata")
				}
			}
		} else if !slices.Equal(v.ChangedFields, []string{"credential"}) {
			return Metadata{}, invalid("metadata")
		}
	}
	if action == RunnerEnroll && v.PublicKeyFingerprint == nil || action != RunnerEnroll && action != RunnerRevoke && v.PublicKeyFingerprint != nil || v.PublicKeyFingerprint != nil && v.PublicKeyFingerprint.Validate() != nil {
		return Metadata{}, invalid("metadata")
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return Metadata{}, invalid("metadata")
	}
	d := metadataData{action: action, raw: string(raw)}
	return Metadata{data: func() metadataData { return d }}, nil
}
func (m Metadata) RunnerFields() (RunnerMetadataFields, error) {
	var v RunnerMetadataFields
	if m.data == nil || !RunnerAction(m.data().action) || json.Unmarshal([]byte(m.data().raw), &v) != nil {
		return v, invalid("metadata")
	}
	return v, nil
}
func decodeRunnerMetadata(action Action, raw []byte) (Metadata, error) {
	if len(raw) > 4096 || !utf8.Valid(raw) {
		return Metadata{}, invalid("metadata")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return Metadata{}, invalid("metadata")
	}
	seen := map[string]bool{}
	for d.More() {
		token, e = d.Token()
		name, ok := token.(string)
		if e != nil || !ok || seen[name] {
			return Metadata{}, invalid("metadata")
		}
		switch name {
		case "runner_id", "version", "credential_generation", "changed_fields", "public_key_fingerprint":
		default:
			return Metadata{}, invalid("metadata")
		}
		seen[name] = true
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return Metadata{}, invalid("metadata")
		}
	}
	if token, e = d.Token(); e != nil || token != json.Delim('}') {
		return Metadata{}, invalid("metadata")
	}
	if d.Decode(new(any)) != io.EOF {
		return Metadata{}, invalid("metadata")
	}
	for _, name := range []string{"runner_id", "version", "credential_generation", "changed_fields"} {
		if !seen[name] {
			return Metadata{}, invalid("metadata")
		}
	}
	var v RunnerMetadataFields
	if json.Unmarshal(raw, &v) != nil {
		return Metadata{}, invalid("metadata")
	}
	return RunnerMetadata(action, v)
}
func validateRunnerEntry(v EntryFields) error {
	m, e := v.Metadata.RunnerFields()
	if e != nil {
		return e
	}
	a := v.Actor.Details()
	if v.Scope.Details().Kind != id.System || v.Outcome != Success || v.Resource.Details().Kind != RunnerResource || v.Resource.Details().ID != m.RunnerID {
		return invalid("entry")
	}
	if v.Action == RunnerEnroll {
		if a.Kind != id.Service || a.ServiceName != id.RunnerIdentity || !validID(a.CauseRef) {
			return invalid("actor")
		}
	} else if a.Kind != id.Human {
		return invalid("actor")
	}
	if v.Associations != (Associations{RunnerID: m.RunnerID}) {
		return invalid("association")
	}
	return nil
}

// CheckAppendInTx must bind the original caller-owned transaction and current
// command/identity event and postimage. An Entry alone proves only shape.
type RunnerAuthority interface {
	CheckAppendInTx(context.Context, f.Tx, Entry, AppendKey) error
}
