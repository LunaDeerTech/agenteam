package projectvariable

import (
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// A completed record contains no request value, plaintext semantic digest,
// Session, prepared handle or ciphertext. The D04 observation below is a safe
// identity projection only; it is never a substitute for the real D04 lookup.
type secretCommandRecord struct {
	ID                                            c.OperationID
	Project                                       c.ProjectID
	User                                          i.UserID
	Command                                       c.SecretCommandName
	Key                                           f.IdempotencyKey
	Target                                        c.VariableID
	Expected                                      *f.Version
	NamePresent, DescriptionPresent, ValuePresent bool
	Observation                                   sc.ProjectVariableWriteObservation
	Receipt                                       c.SecretVariableMutation
	CommittedAt                                   f.Instant
}

func secretRecordSafe(w io.Writer)                     { _, _ = io.WriteString(w, "secret_variable_record") }
func (secretCommandRecord) Format(w fmt.State, _ rune) { secretRecordSafe(w) }
func (secretCommandRecord) LogValue() slog.Value       { return slog.StringValue("secret_variable_record") }
func (secretCommandRecord) MarshalJSON() ([]byte, error) {
	return []byte(`"secret_variable_record"`), nil
}

func secretMutationKind(command c.SecretCommandName) (sc.MutationKind, error) {
	switch command {
	case c.SecretCreateCommand:
		return sc.Create, nil
	case c.SecretUpdateCommand:
		return sc.Update, nil
	case c.SecretDeleteCommand:
		return sc.Delete, nil
	default:
		return "", fault(f.InvalidArgument)
	}
}
func sameSecretExpected(a, b *f.Version) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// Explicit field comparison is necessary: opaque types intentionally redact
// JSON and formatting, so their safe representations cannot prove equality.
func sameSecretObservation(a, b sc.ProjectVariableWriteObservation) bool {
	if a.Validate() != nil || b.Validate() != nil || a.Observed() != b.Observed() {
		return false
	}
	if !a.Observed() {
		return true
	}
	x, xe := a.Result()
	y, ye := b.Result()
	return xe == nil && ye == nil && x.ReceiptID == y.ReceiptID && x.ProjectID == y.ProjectID &&
		x.VariableID == y.VariableID && x.UserID == y.UserID && x.Identity.Canonical() == y.Identity.Canonical() &&
		x.Kind == y.Kind && sameSecretExpected(x.ExpectedVersion, y.ExpectedVersion) && x.Ref.Equal(y.Ref) &&
		x.Effect == y.Effect && x.Version == y.Version && x.Deleted == y.Deleted
}

func validateSecretRecord(r *secretCommandRecord) error {
	if r == nil || r.ID.Validate() != nil || r.Project.Validate() != nil || r.User.Validate() != nil ||
		r.Command.Validate() != nil || r.Key.Validate() != nil || r.Target.Validate() != nil ||
		r.Receipt.Validate() != nil || r.CommittedAt.Validate() != nil || r.CommittedAt.Time().IsZero() ||
		r.Observation.Validate() != nil || !r.Observation.Observed() {
		return internal(nil)
	}
	kind, err := secretMutationKind(r.Command)
	if err != nil || (kind == sc.Create) != (r.Expected == nil) || r.Expected != nil && r.Expected.Validate() != nil {
		return internal(nil)
	}
	identity, err := c.SecretVariableCommandIdentity(r.Project, r.Command, r.Key)
	if err != nil {
		return internal(nil)
	}
	observation, err := r.Observation.Result()
	if err != nil || observation.ProjectID != r.Project || observation.VariableID != r.Target ||
		observation.UserID != r.User || observation.Identity.Canonical() != identity.Canonical() ||
		observation.Kind != kind || !sameSecretExpected(observation.ExpectedVersion, r.Expected) {
		return internal(nil)
	}
	result := r.Receipt.Fields()
	if result.Command != r.Command {
		return internal(nil)
	}
	var version f.Version
	var changedAt f.Instant
	if kind == sc.Delete {
		if result.Deleted == nil || result.Deleted.ProjectID != r.Project || result.Deleted.ID != r.Target {
			return internal(nil)
		}
		version, changedAt = result.Deleted.Version, result.Deleted.DeletedAt
	} else {
		v := result.Variable.Fields()
		if v.ProjectID != r.Project || v.ID != r.Target {
			return internal(nil)
		}
		version, changedAt = v.Version, v.UpdatedAt
	}
	if changedAt.Time().After(r.CommittedAt.Time()) || result.Changed && !changedAt.Time().Equal(r.CommittedAt.Time()) {
		return internal(nil)
	}
	switch kind {
	case sc.Create:
		if !r.NamePresent || !r.DescriptionPresent || !r.ValuePresent || !result.Changed || version != 1 || observation.Effect != sc.ProjectVariableCreated {
			return internal(nil)
		}
	case sc.Update:
		if !r.NamePresent && !r.DescriptionPresent && !r.ValuePresent || r.ValuePresent && !result.Changed ||
			r.ValuePresent && observation.Effect != sc.ProjectVariableReplaced || !r.ValuePresent && observation.Effect != sc.ProjectVariableUnchanged {
			return internal(nil)
		}
		if result.Changed {
			next, err := nextVersion(*r.Expected)
			if err != nil || version != next {
				return internal(nil)
			}
		} else if version != *r.Expected {
			return internal(nil)
		}
	case sc.Delete:
		next, err := nextVersion(*r.Expected)
		if err != nil || r.NamePresent || r.DescriptionPresent || r.ValuePresent || !result.Changed || version != next || observation.Effect != sc.ProjectVariableDeleted {
			return internal(nil)
		}
	}
	return nil
}

func secretRecordMatchesRequest(r *secretCommandRecord, request sc.ProjectVariableWriteRequest) bool {
	if r == nil || request.Validate() != nil {
		return false
	}
	v := request.Fields()
	return r.Project == v.ProjectID && r.Target == v.VariableID && r.User.String() == v.Actor.Details().UserID &&
		string(r.Command) == v.Identity.Command() && r.Key == v.Identity.Key() && sameSecretExpected(r.Expected, v.ExpectedVersion)
}

func secretLookupWriteRequest(actor i.Actor, query c.SecretVariableCommandLookupRequest) (sc.ProjectVariableWriteRequest, error) {
	if query.Validate() != nil || actor.Validate() != nil || actor.Details().Kind != i.Human {
		return sc.ProjectVariableWriteRequest{}, fault(f.InvalidArgument)
	}
	q := query.Fields()
	identity, err := c.SecretVariableCommandIdentity(q.ProjectID, q.Command, q.IdempotencyKey)
	if err != nil {
		return sc.ProjectVariableWriteRequest{}, portError(err)
	}
	kind, err := secretMutationKind(q.Command)
	if err != nil {
		return sc.ProjectVariableWriteRequest{}, err
	}
	request, err := sc.NewProjectVariableWriteRequest(sc.ProjectVariableWriteFields{
		Actor: actor, ProjectID: q.ProjectID, VariableID: q.TargetID, Identity: identity, Kind: kind, ExpectedVersion: q.ExpectedVersion,
	})
	return request, portError(err)
}
