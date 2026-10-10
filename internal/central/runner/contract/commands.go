package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type CommandName string

const (
	Create          CommandName = "runner.create"
	Update          CommandName = "runner.update"
	IssueEnrollment CommandName = "runner.enrollment.issue"
	Revoke          CommandName = "runner.revoke"
)

func (c CommandName) Valid() bool {
	return c == Create || c == Update || c == IssueEnrollment || c == Revoke
}

type CreateRequest struct {
	RunnerID    RunnerID `json:"runner_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	RootPath    string   `json:"root_path"`
}

func (r CreateRequest) Validate() error {
	if r.RunnerID.Validate() != nil || !textValid(r.Name, 1, 128) || !textValid(r.Description, 0, 4096) || !tagsValid(r.Tags) || !rootValid(r.RootPath) {
		return invalid()
	}
	return nil
}
func (r CreateRequest) MarshalJSON() ([]byte, error) {
	type w CreateRequest
	return checked(w(r), r.Validate())
}
func (r *CreateRequest) UnmarshalJSON(raw []byte) error {
	type w CreateRequest
	v, e := decode[w](raw, MaxRequestBytes, []string{"runner_id", "name", "description", "tags", "root_path"}, nil, nil)
	if e != nil {
		return e
	}
	n := CreateRequest(v)
	if e = n.Validate(); e == nil {
		*r = n
	}
	return e
}

type UpdateRequest struct {
	ExpectedVersion f.Version `json:"expected_version"`
	Name            *string   `json:"name,omitempty"`
	Description     *string   `json:"description,omitempty"`
	Tags            *[]string `json:"tags,omitempty"`
}

func (r UpdateRequest) Validate() error {
	if r.ExpectedVersion.Validate() != nil || r.Name == nil && r.Description == nil && r.Tags == nil || r.Name != nil && !textValid(*r.Name, 1, 128) || r.Description != nil && !textValid(*r.Description, 0, 4096) || r.Tags != nil && !tagsValid(*r.Tags) {
		return invalid()
	}
	return nil
}
func (r UpdateRequest) MarshalJSON() ([]byte, error) {
	type w UpdateRequest
	return checked(w(r), r.Validate())
}
func (r *UpdateRequest) UnmarshalJSON(raw []byte) error {
	type w UpdateRequest
	v, e := decode[w](raw, MaxRequestBytes, []string{"expected_version"}, []string{"name", "description", "tags"}, nil)
	if e != nil {
		return e
	}
	n := UpdateRequest(v)
	if e = n.Validate(); e == nil {
		*r = n
	}
	return e
}

type CredentialRequest struct {
	ExpectedVersion f.Version `json:"expected_version"`
}

func (r CredentialRequest) Validate() error {
	if r.ExpectedVersion.Validate() != nil {
		return invalid()
	}
	return nil
}
func (r CredentialRequest) MarshalJSON() ([]byte, error) {
	type w CredentialRequest
	return checked(w(r), r.Validate())
}
func (r *CredentialRequest) UnmarshalJSON(raw []byte) error {
	type w CredentialRequest
	v, e := decode[w](raw, MaxRequestBytes, []string{"expected_version"}, nil, nil)
	if e != nil {
		return e
	}
	n := CredentialRequest(v)
	if e = n.Validate(); e == nil {
		*r = n
	}
	return e
}

// Intent owns the original typed request bytes; later caller mutations cannot
// change either execution or the digest used by a disconnected lookup.
type Intent struct{ data func() intentData }
type intentData struct {
	command  CommandName
	target   RunnerID
	raw      string
	expected f.Version
}

func NewCreate(r CreateRequest) (Intent, error) {
	b, e := json.Marshal(r)
	if e != nil {
		return Intent{}, e
	}
	return DecodeIntent(Create, r.RunnerID, b)
}
func NewUpdate(target RunnerID, r UpdateRequest) (Intent, error) {
	b, e := json.Marshal(r)
	if e != nil {
		return Intent{}, e
	}
	return DecodeIntent(Update, target, b)
}
func NewEnrollment(target RunnerID, r CredentialRequest) (Intent, error) {
	b, e := json.Marshal(r)
	if e != nil {
		return Intent{}, e
	}
	return DecodeIntent(IssueEnrollment, target, b)
}
func NewRevoke(target RunnerID, r CredentialRequest) (Intent, error) {
	b, e := json.Marshal(r)
	if e != nil {
		return Intent{}, e
	}
	return DecodeIntent(Revoke, target, b)
}
func DecodeIntent(command CommandName, target RunnerID, raw []byte) (Intent, error) {
	if !command.Valid() || target.Validate() != nil || len(raw) > MaxRequestBytes {
		return Intent{}, invalid()
	}
	var typed any
	expected := f.Version(0)
	switch command {
	case Create:
		var v CreateRequest
		if json.Unmarshal(raw, &v) != nil || v.RunnerID != target {
			return Intent{}, invalid()
		}
		typed = v
	case Update:
		var v UpdateRequest
		if json.Unmarshal(raw, &v) != nil {
			return Intent{}, invalid()
		}
		typed = v
		expected = v.ExpectedVersion
	case IssueEnrollment, Revoke:
		var v CredentialRequest
		if json.Unmarshal(raw, &v) != nil {
			return Intent{}, invalid()
		}
		typed = v
		expected = v.ExpectedVersion
	}
	b, e := json.Marshal(typed)
	if e != nil {
		return Intent{}, invalid()
	}
	b, e = cursor.CanonicalJSON(b)
	if e != nil {
		return Intent{}, invalid()
	}
	d := intentData{command, target, string(b), expected}
	return Intent{data: func() intentData { return d }}, nil
}
func (v Intent) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}
func (v Intent) Command() CommandName {
	if v.data == nil {
		return ""
	}
	return v.data().command
}
func (v Intent) Target() RunnerID {
	if v.data == nil {
		return RunnerID{}
	}
	return v.data().target
}
func (v Intent) ExpectedVersion() f.Version {
	if v.data == nil {
		return 0
	}
	return v.data().expected
}
func (v Intent) RequestJSON() ([]byte, error) {
	if v.Validate() != nil {
		return nil, invalid()
	}
	return []byte(v.data().raw), nil
}
func (v Intent) Identity(actor id.Actor, key f.IdempotencyKey) (f.CommandIdentity, error) {
	if v.Validate() != nil || actor.Validate() != nil || actor.Details().Kind != id.Human || key.Validate() != nil {
		return f.CommandIdentity{}, invalid()
	}
	return f.NewCommandIdentity("runner-management", []string{actor.Details().UserID, v.Target().String()}, string(v.Command()), key)
}
func (v Intent) Digest(actor id.Actor) (f.Digest, error) {
	if v.Validate() != nil || actor.Validate() != nil || actor.Details().Kind != id.Human {
		return "", invalid()
	}
	raw, e := json.Marshal(struct {
		Format  string          `json:"format"`
		User    string          `json:"user_id"`
		Runner  RunnerID        `json:"runner_id"`
		Command CommandName     `json:"command"`
		Request json.RawMessage `json:"request"`
	}{"runner-management-v1", actor.Details().UserID, v.Target(), v.Command(), json.RawMessage(v.data().raw)})
	if e != nil {
		return "", invalid()
	}
	raw, e = cursor.CanonicalJSON(raw)
	if e != nil {
		return "", invalid()
	}
	hash := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(hash[:])), nil
}
func (Intent) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_intent") }
func (Intent) MarshalJSON() ([]byte, error) { return []byte(`"runner_intent"`), nil }
func (Intent) LogValue() slog.Value         { return slog.StringValue("runner_intent") }

type Receipt struct {
	CommandID CommandID   `json:"command_id"`
	Command   CommandName `json:"command"`
	Runner    Snapshot    `json:"runner"`
	Changed   bool        `json:"changed"`
}

func (r Receipt) Validate() error {
	if r.CommandID.Validate() != nil || !r.Command.Valid() || r.Runner.Validate() != nil || r.Command != Update && !r.Changed {
		return invalid()
	}
	return nil
}
func (r Receipt) Clone() Receipt               { r.Runner = r.Runner.Clone(); return r }
func (r Receipt) MarshalJSON() ([]byte, error) { type w Receipt; return checked(w(r), r.Validate()) }
func (r *Receipt) UnmarshalJSON(raw []byte) error {
	type w Receipt
	v, e := decode[w](raw, MaxRecordBytes, []string{"command_id", "command", "runner", "changed"}, nil, nil)
	if e != nil {
		return e
	}
	n := Receipt(v)
	if e = n.Validate(); e == nil {
		*r = n
	}
	return e
}
func (Receipt) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "runner_receipt") }
func (Receipt) LogValue() slog.Value       { return slog.StringValue("runner_receipt") }

// A material value exists only for the first known commit. It is never put in
// commands.receipt, replay or Lookup, nor in implicit JSON/log output.
type EnrollmentMaterial struct {
	Token     p.EnrollmentToken
	ExpiresAt f.Instant
}

func (EnrollmentMaterial) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "runner_enrollment_material")
}
func (EnrollmentMaterial) MarshalJSON() ([]byte, error) {
	return []byte(`"runner_enrollment_material"`), nil
}
func (EnrollmentMaterial) LogValue() slog.Value {
	return slog.StringValue("runner_enrollment_material")
}

type Mutation struct {
	Receipt  Receipt
	Material *EnrollmentMaterial
}

func (Mutation) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_mutation") }
func (Mutation) MarshalJSON() ([]byte, error) { return []byte(`"runner_mutation"`), nil }
func (Mutation) LogValue() slog.Value         { return slog.StringValue("runner_mutation") }

type Lookup struct{ Receipt *Receipt }
type Commands interface {
	Execute(context.Context, id.Actor, f.IdempotencyKey, Intent) (Mutation, error)
	Lookup(context.Context, id.Actor, f.IdempotencyKey, Intent) (Lookup, error)
}
