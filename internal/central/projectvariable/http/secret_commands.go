package projectvariablehttp

import (
	"encoding/json"
	"net/http"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type secretIntent struct {
	name   c.SecretCommandName
	target c.VariableID
	meta   f.CommandMeta
	create *c.SecretVariableCreate
	update *c.SecretVariableUpdate
	lookup bool
}

func (in *secretIntent) destroy() {
	if in.create != nil {
		in.create.Destroy()
	}
	if in.update != nil {
		in.update.Destroy()
	}
}

func secretMaterial(raw []byte) (sc.SecretMaterial, error) {
	value, err := secretStringBytes(raw)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	defer clear(value)
	if err = c.ValidateSecretValue(value); err != nil {
		return sc.SecretMaterial{}, inputError(err)
	}
	material, err := sc.NewSecretMaterial(value)
	if err != nil {
		return sc.SecretMaterial{}, invalidInput()
	}
	return material, nil
}

// The caller installs in.destroy before decoding, so every constructed request
// already has an owner even on a subsequent error, expiry or panic.
func decodeSecretIntent(r *http.Request, route route, raw []byte, in *secretIntent) error {
	in.lookup = route.lookup()
	var allowed []string
	if in.lookup {
		allowed = []string{"command", "target_id", "expected_version"}
	} else {
		switch {
		case route.kind == variables && r.Method == http.MethodPost:
			in.name, allowed = c.SecretCreateCommand, []string{"request"}
		case route.kind == variable && r.Method == http.MethodPatch:
			in.name, allowed = c.SecretUpdateCommand, []string{"expected_version", "request"}
		case route.kind == variable && r.Method == http.MethodDelete:
			in.name, allowed = c.SecretDeleteCommand, []string{"expected_version"}
		default:
			return invalidInput()
		}
	}
	fields, err := secretObject(raw, allowed...)
	if err != nil {
		return err
	}
	if in.lookup && json.Unmarshal(fields["command"], &in.name) != nil {
		return invalidInput()
	}
	if in.name == c.SecretCreateCommand {
		if _, present := fields["expected_version"]; present {
			return invalidInput()
		}
	} else {
		var expected f.Version
		if json.Unmarshal(fields["expected_version"], &expected) != nil {
			return invalidInput()
		}
		in.meta.ExpectedVersion = &expected
	}
	if err = c.ValidateSecretCommandMeta(in.name, in.meta); err != nil {
		return err
	}
	if in.lookup {
		if json.Unmarshal(fields["target_id"], &in.target) != nil {
			return invalidInput()
		}
		return nil
	}
	if in.name != c.SecretCreateCommand {
		in.target, err = f.ParseID[id.ProjectVariable](route.target)
		if err != nil {
			return invalidInput()
		}
	}
	if in.name == c.SecretDeleteCommand {
		return nil
	}
	request, err := secretObject(fields["request"], "variable_id", "name", "description", "value")
	if err != nil {
		return err
	}
	if in.name == c.SecretCreateCommand {
		if len(request) != 4 {
			return invalidInput()
		}
		var fields c.SecretVariableCreateFields
		if json.Unmarshal(request["variable_id"], &fields.ID) != nil || json.Unmarshal(request["name"], &fields.Name) != nil || json.Unmarshal(request["description"], &fields.Description) != nil {
			return invalidInput()
		}
		material, err := secretMaterial(request["value"])
		if err != nil {
			return err
		}
		defer material.Destroy()
		fields.Value = material
		value, err := c.NewSecretVariableCreate(fields)
		in.create = &value
		if err != nil {
			return inputError(err)
		}
		in.target = fields.ID
		return nil
	}
	if _, present := request["variable_id"]; present || len(request) == 0 {
		return invalidInput()
	}
	var fields c.SecretVariableUpdateFields
	if raw, present := request["name"]; present {
		var name string
		if json.Unmarshal(raw, &name) != nil {
			return invalidInput()
		}
		fields.Name = &name
	}
	if raw, present := request["description"]; present {
		var description string
		if json.Unmarshal(raw, &description) != nil {
			return invalidInput()
		}
		fields.Description = &description
	}
	if raw, present := request["value"]; present {
		material, err := secretMaterial(raw)
		if err != nil {
			return err
		}
		defer material.Destroy()
		fields.Value = &material
	}
	value, err := c.NewSecretVariableUpdate(fields)
	in.update = &value
	if err != nil {
		return inputError(err)
	}
	return nil
}

func validSecretReceipt(in secretIntent, project c.ProjectID, out c.SecretVariableMutation) bool {
	if out.Validate() != nil {
		return false
	}
	r := out.Fields()
	if r.Command != in.name {
		return false
	}
	if in.name == c.SecretDeleteCommand {
		return r.Deleted != nil && r.Deleted.ID == in.target && r.Deleted.ProjectID == project && validVersion(r.Deleted.Version, in.meta.ExpectedVersion, true)
	}
	v := r.Variable.Fields()
	if v.ID != in.target || v.ProjectID != project || !validVersion(v.Version, in.meta.ExpectedVersion, r.Changed) {
		return false
	}
	if in.lookup {
		// Identity-only history has no caller-supplied metadata to compare.
		return true
	}
	if in.create != nil {
		q := in.create.Fields()
		return v.Name == q.Name && v.Description == q.Description
	}
	if in.update == nil {
		return false
	}
	q := in.update.Fields()
	return (!q.ValuePresent || r.Changed) && (q.Name == nil || v.Name == *q.Name) && (q.Description == nil || v.Description == *q.Description)
}

func (h *secretHandler) command(r *http.Request, actor id.Actor, project c.ProjectID, route route) ([]byte, error) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, invalidInput()
	}
	key, err := commandKey(r)
	if err != nil {
		return nil, err
	}
	raw, err := readSecretBody(r)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	in := secretIntent{meta: f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: key}}
	defer in.destroy()
	if err = decodeSecretIntent(r, route, raw, &in); err != nil {
		return nil, err
	}
	clear(raw) // all material now belongs to the typed request
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if in.lookup {
		q, err := c.NewSecretVariableCommandLookupRequest(c.SecretVariableCommandLookupFields{ProjectID: project, Command: in.name, TargetID: in.target, IdempotencyKey: key, ExpectedVersion: in.meta.ExpectedVersion})
		if err != nil {
			return nil, err
		}
		out, err := h.secrets.LookupSecretVariableCommand(r.Context(), actor, q)
		if err != nil {
			return nil, err
		}
		if out.Validate() != nil {
			return nil, badProjection()
		}
		if receipt := out.Receipt(); receipt != nil && !validSecretReceipt(in, project, *receipt) {
			return nil, badProjection()
		}
		return encodeValue(r.Context(), out, bodyLimit)
	}
	var out c.SecretVariableMutation
	switch in.name {
	case c.SecretCreateCommand:
		out, err = h.secrets.CreateSecretVariable(r.Context(), actor, in.meta, project, *in.create)
	case c.SecretUpdateCommand:
		out, err = h.secrets.UpdateSecretVariable(r.Context(), actor, in.meta, project, in.target, *in.update)
	case c.SecretDeleteCommand:
		out, err = h.secrets.DeleteSecretVariable(r.Context(), actor, in.meta, project, in.target)
	default:
		return nil, invalidInput()
	}
	if err != nil {
		return nil, err
	}
	if !validSecretReceipt(in, project, out) {
		panic(http.ErrAbortHandler)
	}
	encoded, err := encodeValue(r.Context(), out, bodyLimit)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	return encoded, nil
}
