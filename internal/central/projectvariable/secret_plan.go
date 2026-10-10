package projectvariable

import (
	"fmt"
	"log/slog"
	"slices"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// secretIntent copies material directly between scoped byte callbacks. No
// plaintext string/JSON/digest is created or retained by the D10 Owner.
func secretIntent(request sc.ProjectVariableWriteRequest, create *c.SecretVariableCreate, update *c.SecretVariableUpdate) (sc.ProjectVariableIntent, error) {
	empty := sc.ProjectVariableIntent{}
	if request.Validate() != nil {
		return empty, fault(f.InvalidArgument)
	}
	fields := sc.ProjectVariableIntentFields{Request: request}
	var use func(func([]byte) error) error
	switch request.Fields().Kind {
	case sc.Create:
		if create == nil || update != nil || create.Validate() != nil || create.Fields().ID != request.Fields().VariableID {
			return empty, fault(f.InvalidArgument)
		}
		v := create.Fields()
		fields.Name, fields.Description = &v.Name, &v.Description
		use = create.UseValue
	case sc.Update:
		if create != nil || update == nil || update.Validate() != nil {
			return empty, fault(f.InvalidArgument)
		}
		v := update.Fields()
		fields.Name, fields.Description = v.Name, v.Description
		if v.ValuePresent {
			use = update.UseValue
		}
	case sc.Delete:
		if create != nil || update != nil {
			return empty, fault(f.InvalidArgument)
		}
	default:
		return empty, fault(f.InvalidArgument)
	}
	if use == nil {
		return sc.NewProjectVariableIntent(fields)
	}
	var out sc.ProjectVariableIntent
	err := use(func(value []byte) error {
		material, err := sc.NewSecretMaterial(value)
		if err != nil {
			return err
		}
		defer material.Destroy()
		fields.Value = &material
		out, err = sc.NewProjectVariableIntent(fields)
		return err
	})
	if err != nil {
		return empty, portError(err)
	}
	return out, nil
}

// This is private preparation data, never a durable planned command or a
// permission proof. Audit receives a distinct live-Tx witness after mutation.
type secretMutationPlan struct {
	Request            sc.ProjectVariableWriteRequest
	Before             *secretVariableRow
	After              c.SecretVariable
	Deleted            bool
	Fields             []string
	Operation, History c.OperationID
	Event              event.EventID
	At                 f.Instant
}

func (*secretMutationPlan) Format(w fmt.State, _ rune) { secretRecordSafe(w) }
func (*secretMutationPlan) LogValue() slog.Value       { return slog.StringValue("secret_variable_record") }
func (*secretMutationPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"secret_variable_record"`), nil
}
func planSecretVariable(intent sc.ProjectVariableIntent, before *secretVariableRow, at f.Instant, operation, history c.OperationID) (*secretMutationPlan, bool, error) {
	if intent.Validate() != nil || at.Validate() != nil || at.Time().IsZero() || operation.Validate() != nil || history.Validate() != nil {
		return nil, false, internal(nil)
	}
	m := intent.Fields()
	r := m.Request.Fields()
	p := &secretMutationPlan{Request: m.Request, Operation: operation, History: history, At: at}
	var fields c.SecretVariableFields
	if r.Kind == sc.Create {
		if before != nil || m.Name == nil || m.Description == nil || !m.ValuePresent {
			return nil, false, internal(nil)
		}
		fields = c.SecretVariableFields{ID: r.VariableID, ProjectID: r.ProjectID, Type: c.SecretVariableType, Name: *m.Name, Description: *m.Description, Version: 1, CreatedAt: at, UpdatedAt: at}
		p.Fields = []string{"created"}
	} else {
		if before == nil || before.Deleted != nil || before.Variable.Validate() != nil || before.Ref.Validate() != nil || before.CredentialVersion.Validate() != nil || r.ExpectedVersion == nil {
			return nil, false, internal(nil)
		}
		fields = before.Variable.Fields()
		if fields.ID != r.VariableID || fields.ProjectID != r.ProjectID {
			return nil, false, internal(nil)
		}
		if fields.Version != *r.ExpectedVersion {
			return nil, false, fault(f.VersionConflict)
		}
		copy := *before
		p.Before = &copy
		if r.Kind == sc.Delete {
			p.Deleted = true
			fields.Description = ""
			p.Fields = []string{"deleted"}
		} else {
			if m.Description != nil && *m.Description != fields.Description {
				fields.Description = *m.Description
				p.Fields = append(p.Fields, "description")
			}
			if m.Name != nil && *m.Name != fields.Name {
				fields.Name = *m.Name
				p.Fields = append(p.Fields, "name")
			}
			// Explicit value presence is always a replacement. D10 never compares
			// plaintexts, ciphertexts or a plaintext fingerprint to infer a no-op.
			if m.ValuePresent {
				p.Fields = append(p.Fields, "value")
			}
		}
		if len(p.Fields) == 0 {
			p.After = before.Variable
			return p, false, nil
		}
		version, err := nextVersion(fields.Version)
		if err != nil {
			return nil, false, err
		}
		fields.Version = version
		if at.Time().Before(fields.UpdatedAt.Time()) {
			return nil, false, internal(nil)
		}
		fields.UpdatedAt = at
	}
	var err error
	p.After, err = c.NewSecretVariable(fields)
	if err != nil {
		return nil, false, internal(err)
	}
	return p, true, nil
}
func secretPlanSame(a, b *secretMutationPlan) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Request.Validate() != nil || b.Request.Validate() != nil {
		return false
	}
	x, y := a.Request.Fields(), b.Request.Fields()
	return x.Actor.Equal(y.Actor) && x.ProjectID == y.ProjectID && x.VariableID == y.VariableID && x.Identity.Canonical() == y.Identity.Canonical() && x.Kind == y.Kind && sameSecretExpected(x.ExpectedVersion, y.ExpectedVersion) &&
		a.Operation == b.Operation && a.History == b.History && a.Event == b.Event && a.At == b.At && a.Deleted == b.Deleted && slices.Equal(a.Fields, b.Fields) && sameValue(a.After, b.After) && secretRowSame(a.Before, b.Before)
}
func secretRowSame(a, b *secretVariableRow) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if (a.Deleted == nil) != (b.Deleted == nil) {
		return false
	}
	if a.Deleted != nil && *a.Deleted != *b.Deleted {
		return false
	}
	return sameValue(a.Variable, b.Variable) && a.CredentialVersion == b.CredentialVersion && ((a.Deleted != nil && b.Deleted != nil) || a.Ref.Equal(b.Ref))
}
