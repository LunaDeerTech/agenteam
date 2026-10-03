package foundation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

var errCause = errors.New("INVALID_TRANSACTION_CAUSE")

// CommandIdentity contains stable scalar identities only. The owning command
// still validates its registered namespace and ordered owner interpretation.
type CommandIdentity struct{ data func() commandIdentity }
type commandIdentity struct {
	Namespace string         `json:"namespace"`
	OwnerIDs  []string       `json:"owner_ids"`
	Command   string         `json:"command"`
	Key       IdempotencyKey `json:"key"`
}

func NewCommandIdentity(namespace string, owners []string, command string, key IdempotencyKey) (CommandIdentity, error) {
	if !stableName(namespace) || !stableName(command) || key.Validate() != nil {
		return CommandIdentity{}, errCause
	}
	for _, id := range owners {
		if !validScalarID(id) {
			return CommandIdentity{}, errCause
		}
	}
	data := commandIdentity{namespace, append([]string{}, owners...), command, key}
	return CommandIdentity{data: func() commandIdentity { return data }}, nil
}
func (c CommandIdentity) Validate() error {
	if c.data == nil {
		return errCause
	}
	return nil
}
func (c CommandIdentity) Namespace() string {
	if c.data == nil {
		return ""
	}
	return c.data().Namespace
}
func (c CommandIdentity) OwnerIDs() []string {
	if c.data == nil {
		return nil
	}
	return append([]string{}, c.data().OwnerIDs...)
}
func (c CommandIdentity) Command() string {
	if c.data == nil {
		return ""
	}
	return c.data().Command
}
func (c CommandIdentity) Key() IdempotencyKey {
	if c.data == nil {
		return ""
	}
	return c.data().Key
}

// Canonical is an explicit lookup/lock projection, never a general log field.
func (c CommandIdentity) Canonical() string {
	if c.data == nil {
		return ""
	}
	b, _ := json.Marshal(c.data())
	return string(b)
}
func (c CommandIdentity) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "command_identity") }
func (c CommandIdentity) MarshalJSON() ([]byte, error) { return []byte(`"command_identity"`), nil }
func (c CommandIdentity) LogValue() slog.Value         { return slog.StringValue("command_identity") }

type CauseKind string

const (
	CommandsCause CauseKind = "commands"
	JobCause      CauseKind = "job"
	DeliveryCause CauseKind = "delivery"
	RecoveryCause CauseKind = "recovery"
)

// CauseDetails is an explicit lookup projection. Do not log it or include it in
// transport DTOs; command keys and checkpoint references belong to their owner.
type CauseDetails struct {
	Kind                                CauseKind
	Primary                             CommandIdentity
	Related                             []CommandIdentity
	JobType, JobID, JobAttemptID        string
	EventID, HandlerName                string
	Owner, RecoveryRunID, CheckpointRef string
}
type TransactionCause struct{ data func() CauseDetails }

func NewCommandsCause(primary CommandIdentity, related ...CommandIdentity) (TransactionCause, error) {
	if primary.Validate() != nil {
		return TransactionCause{}, errCause
	}
	for _, identity := range related {
		if identity.Validate() != nil {
			return TransactionCause{}, errCause
		}
	}
	return newCause(CauseDetails{Kind: CommandsCause, Primary: primary, Related: append([]CommandIdentity(nil), related...)}), nil
}
func NewJobCause(jobType, jobID, attemptID string) (TransactionCause, error) {
	if !stableName(jobType) || !validScalarID(jobID) || !validScalarID(attemptID) {
		return TransactionCause{}, errCause
	}
	return newCause(CauseDetails{Kind: JobCause, JobType: jobType, JobID: jobID, JobAttemptID: attemptID}), nil
}
func NewDeliveryCause(eventID, handlerName string) (TransactionCause, error) {
	if !validScalarID(eventID) || !stableName(handlerName) {
		return TransactionCause{}, errCause
	}
	return newCause(CauseDetails{Kind: DeliveryCause, EventID: eventID, HandlerName: handlerName}), nil
}
func NewRecoveryCause(owner, runID, checkpointRef string) (TransactionCause, error) {
	if !stableName(owner) || !validScalarID(runID) || checkpointRef != "" && IdempotencyKey(checkpointRef).Validate() != nil {
		return TransactionCause{}, errCause
	}
	return newCause(CauseDetails{Kind: RecoveryCause, Owner: owner, RecoveryRunID: runID, CheckpointRef: checkpointRef}), nil
}
func newCause(data CauseDetails) TransactionCause {
	return TransactionCause{data: func() CauseDetails { return data }}
}
func (c TransactionCause) Validate() error {
	if c.data == nil {
		return errCause
	}
	return nil
}
func (c TransactionCause) Details() CauseDetails {
	if c.data == nil {
		return CauseDetails{}
	}
	data := c.data()
	data.Related = append([]CommandIdentity(nil), data.Related...)
	return data
}
func (c TransactionCause) Kind() CauseKind {
	if c.data == nil {
		return ""
	}
	return c.data().Kind
}
func (c TransactionCause) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "transaction_cause") }
func (c TransactionCause) MarshalJSON() ([]byte, error) { return []byte(`"transaction_cause"`), nil }
func (c TransactionCause) LogValue() slog.Value         { return slog.StringValue("transaction_cause") }

func validScalarID(id string) bool { _, err := ParseID[struct{}](id); return err == nil }
func stableName(name string) bool {
	if len(name) == 0 || len(name) > 128 || strings.TrimSpace(name) != name {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || i > 0 && (c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.')) {
			return false
		}
	}
	return true
}
