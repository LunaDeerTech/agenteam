package projectvariablehttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

type intent struct {
	name   c.CommandName
	target c.VariableID
	meta   f.CommandMeta
	create *c.VariableCreate
	update *c.VariableUpdate
	lookup bool
}

func commandKey(r *http.Request) (f.IdempotencyKey, error) {
	var values []string
	for name, v := range r.Header {
		if strings.EqualFold(name, "Idempotency-Key") {
			values = append(values, v...)
		}
	}
	if len(values) != 1 || f.IdempotencyKey(values[0]).Validate() != nil {
		return "", invalidInput()
	}
	return f.IdempotencyKey(values[0]), nil
}
func decodeIntent(w http.ResponseWriter, r *http.Request, route route) (intent, error) {
	var out intent
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return out, invalidInput()
	}
	for name := range r.Header {
		if strings.EqualFold(name, "Content-Encoding") {
			return out, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
		}
	}
	key, e := commandKey(r)
	if e != nil {
		return out, e
	}
	var v struct {
		Command  json.RawMessage `json:"command"`
		Target   json.RawMessage `json:"target_id"`
		Expected json.RawMessage `json:"expected_version"`
		Request  json.RawMessage `json:"request"`
	}
	if e = httpapi.DecodeJSON(w, r, &v, bodyLimit); e != nil {
		return out, e
	}
	out.meta = f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: key}
	out.lookup = route.lookup()
	if out.lookup {
		if json.Unmarshal(v.Command, &out.name) != nil {
			return intent{}, invalidInput()
		}
	} else {
		if len(v.Command) != 0 || len(v.Target) != 0 {
			return intent{}, invalidInput()
		}
		switch {
		case route.kind == variables && r.Method == http.MethodPost:
			out.name = c.CreateCommand
		case route.kind == variable && r.Method == http.MethodPatch:
			out.name = c.UpdateCommand
		case route.kind == variable && r.Method == http.MethodDelete:
			out.name = c.DeleteCommand
		default:
			return intent{}, invalidInput()
		}
	}
	if out.name == c.CreateCommand {
		if len(v.Expected) != 0 || len(v.Target) != 0 {
			return intent{}, invalidInput()
		}
	} else {
		var expected f.Version
		if json.Unmarshal(v.Expected, &expected) != nil {
			return intent{}, invalidInput()
		}
		out.meta.ExpectedVersion = &expected
		target := route.target
		if out.lookup {
			if json.Unmarshal(v.Target, &target) != nil {
				return intent{}, invalidInput()
			}
		}
		out.target, e = f.ParseID[id.ProjectVariable](target)
		if e != nil {
			return intent{}, invalidInput()
		}
	}
	if e = c.ValidateCommandMeta(out.name, out.meta); e != nil {
		return intent{}, e
	}
	if out.name == c.DeleteCommand {
		if len(v.Request) != 0 {
			return intent{}, invalidInput()
		}
	} else {
		body := bytes.TrimSpace(v.Request)
		if len(body) == 0 || body[0] != '{' {
			return intent{}, invalidInput()
		}
		if out.name == c.CreateCommand {
			var request c.VariableCreate
			if e = json.Unmarshal(body, &request); e != nil {
				return intent{}, inputError(e)
			}
			out.create = &request
			out.target = request.Fields().ID
		} else {
			var request c.VariableUpdate
			if e = json.Unmarshal(body, &request); e != nil {
				return intent{}, inputError(e)
			}
			out.update = &request
		}
	}
	return out, nil
}
func inputError(e error) error {
	var fault *f.Fault
	if !errors.As(e, &fault) {
		return invalidInput()
	}
	next := *fault
	next.FieldErrors = append([]f.FieldError(nil), fault.FieldErrors...)
	for n := range next.FieldErrors {
		next.FieldErrors[n].Path = "/request" + next.FieldErrors[n].Path
	}
	return &next
}
func (in intent) request() any {
	if in.create != nil {
		return *in.create
	}
	if in.update != nil {
		return *in.update
	}
	return nil
}
func validVersion(v f.Version, expected *f.Version, changed bool) bool {
	if expected == nil {
		return changed && v == 1
	}
	if !changed {
		return v == *expected
	}
	return *expected < math.MaxInt64 && v == *expected+1
}
func validReceipt(in intent, p c.ProjectID, out c.VariableMutation) bool {
	if out.Validate() != nil {
		return false
	}
	r := out.Fields()
	if r.Command != in.name {
		return false
	}
	if in.name == c.DeleteCommand {
		return r.Deleted != nil && r.Deleted.ID == in.target && r.Deleted.ProjectID == p && validVersion(r.Deleted.Version, in.meta.ExpectedVersion, true)
	}
	v := r.Variable.Fields()
	if v.ID != in.target || v.ProjectID != p || !validVersion(v.Version, in.meta.ExpectedVersion, r.Changed) {
		return false
	}
	if in.create != nil {
		q := in.create.Fields()
		return v.Name == q.Name && v.Description == q.Description && v.Value == q.Value
	}
	if in.update == nil {
		return false
	}
	q := in.update.Fields()
	return (q.Name == nil || v.Name == *q.Name) && (q.Description == nil || v.Description == *q.Description) && (q.Value == nil || v.Value == *q.Value)
}
func (h *handler) command(w http.ResponseWriter, r *http.Request, a id.Actor, p c.ProjectID, route route) ([]byte, error) {
	in, e := decodeIntent(w, r, route)
	if e != nil {
		return nil, e
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	digest, e := c.VariableCommandDigest(a, in.meta, p, in.target, in.name, in.request())
	if e != nil {
		return nil, e
	}
	if in.lookup {
		q, e := c.NewVariableCommandLookupRequest(c.VariableCommandLookupFields{ProjectID: p, Command: in.name, IdempotencyKey: in.meta.IdempotencyKey, SemanticDigest: digest})
		if e != nil {
			return nil, e
		}
		out, e := h.variables.LookupVariableCommand(r.Context(), a, q)
		if e != nil {
			return nil, e
		}
		if out.Validate() != nil {
			return nil, badProjection()
		}
		if receipt := out.Receipt(); receipt != nil && !validReceipt(in, p, *receipt) {
			return nil, badProjection()
		}
		return encodeValue(r.Context(), out, bodyLimit)
	}
	var out c.VariableMutation
	switch in.name {
	case c.CreateCommand:
		out, e = h.variables.CreateVariable(r.Context(), a, in.meta, p, *in.create)
	case c.UpdateCommand:
		out, e = h.variables.UpdateVariable(r.Context(), a, in.meta, p, in.target, *in.update)
	case c.DeleteCommand:
		out, e = h.variables.DeleteVariable(r.Context(), a, in.meta, p, in.target)
	default:
		return nil, invalidInput()
	}
	if e != nil {
		return nil, e
	}
	if !validReceipt(in, p, out) {
		panic(http.ErrAbortHandler)
	}
	raw, e := encodeValue(r.Context(), out, bodyLimit)
	if e != nil {
		panic(http.ErrAbortHandler)
	}
	return raw, nil
}
