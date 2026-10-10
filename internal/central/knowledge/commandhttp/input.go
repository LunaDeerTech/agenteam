package commandhttp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

const requestLimit = 16 << 10

// Raw fields preserve absence/null and the original bytes after the shared
// decoder checks duplicate decoded keys at every depth. Surrogates must be
// checked before encoding/json can replace them with U+FFFD.
type inputWire struct {
	Expected       json.RawMessage `json:"expected_version"`
	Title          json.RawMessage `json:"title"`
	ExpectedParent json.RawMessage `json:"expected_parent_id"`
	TargetParent   json.RawMessage `json:"target_parent_id"`
	Token          json.RawMessage `json:"confirmation_token"`
	Command        json.RawMessage `json:"command"`
	Document       json.RawMessage `json:"document_id"`
	Request        json.RawMessage `json:"request"`
}

func (v *inputWire) UnmarshalJSON(raw []byte) error {
	if !pairedEscapes(raw) {
		return invalidInput()
	}
	type plain inputWire
	var next plain
	if err := json.Unmarshal(raw, &next); err != nil {
		return invalidInput()
	}
	*v = inputWire(next)
	return nil
}
func pairedEscapes(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		n, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
func exactObject(raw []byte, fields ...string) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil || len(obj) != len(fields) {
		return nil, invalidInput()
	}
	for _, field := range fields {
		if _, ok := obj[field]; !ok {
			return nil, invalidInput()
		}
	}
	return obj, nil
}
func textField(raw json.RawMessage, dst *string) bool {
	b := bytes.TrimSpace(raw)
	return len(b) > 0 && b[0] == '"' && json.Unmarshal(b, dst) == nil
}
func headers(r *http.Request, name string) []string {
	var values []string
	for k, v := range r.Header {
		if strings.EqualFold(k, name) {
			values = append(values, v...)
		}
	}
	return values
}

type intent struct {
	name   kc.CommandName
	target kc.DocumentID
	meta   f.CommandMeta
	title  string
	move   kc.MoveRequest
	token  kc.ConfirmationToken
}

func (v intent) update() kc.UpdateRequest { title := v.title; return kc.UpdateRequest{Title: &title} }
func (v intent) digest(a id.Actor, p id.ProjectID) (f.Digest, error) {
	switch v.name {
	case kc.Update:
		return kc.UpdateDigest(a, v.meta, p, v.target, v.update(), nil)
	case kc.Move:
		return kc.MoveDigest(a, v.meta, p, v.target, v.move)
	case kc.DeleteSubtree:
		return kc.DeleteDigest(a, v.meta, p, v.target, v.token)
	}
	return "", invalidInput()
}
func decodeIntent(w http.ResponseWriter, r *http.Request, route route) (intent, error) {
	var out intent
	for k := range r.Header {
		if strings.EqualFold(k, "Content-Encoding") {
			return out, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
		}
	}
	keys := headers(r, "Idempotency-Key")
	if route.action == "delete-preview" {
		if len(keys) != 0 {
			return out, invalidInput()
		}
	} else {
		if len(keys) != 1 || f.IdempotencyKey(keys[0]).Validate() != nil {
			return out, invalidInput()
		}
		out.meta = f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: f.IdempotencyKey(keys[0])}
	}
	var wire inputWire
	if err := httpapi.DecodeJSON(w, r, &wire, requestLimit); err != nil {
		return out, err
	}
	// Re-encoding only this raw-field envelope reconstructs its presence set;
	// individual request bytes remain untouched for their strict decoders.
	fields := map[string]json.RawMessage{}
	for k, v := range map[string]json.RawMessage{"expected_version": wire.Expected, "title": wire.Title, "expected_parent_id": wire.ExpectedParent, "target_parent_id": wire.TargetParent, "confirmation_token": wire.Token, "command": wire.Command, "document_id": wire.Document, "request": wire.Request} {
		if len(v) > 0 {
			fields[k] = v
		}
	}
	raw, _ := json.Marshal(fields)
	if route.action == "lookup" {
		if _, err := exactObject(raw, "command", "document_id", "request"); err != nil {
			return out, err
		}
		var command, target string
		if !textField(wire.Command, &command) || !textField(wire.Document, &target) {
			return out, invalidInput()
		}
		out.name = kc.CommandName(command)
		var err error
		out.target, err = f.ParseID[kc.Document](target)
		if err != nil {
			return out, invalidInput()
		}
		raw = wire.Request
	} else {
		var err error
		out.target, err = f.ParseID[kc.Document](route.target)
		if err != nil {
			return out, invalidInput()
		}
		switch route.action {
		case "rename":
			out.name = kc.Update
		case "move":
			out.name = kc.Move
		case "delete-subtree":
			out.name = kc.DeleteSubtree
		case "delete-preview":
			_, err = exactObject(raw)
			return out, err
		default:
			return out, invalidInput()
		}
	}
	switch out.name {
	case kc.Update:
		obj, err := exactObject(raw, "expected_version", "title")
		if err != nil {
			return out, err
		}
		var expected f.Version
		if json.Unmarshal(obj["expected_version"], &expected) != nil || !textField(obj["title"], &out.title) || kc.ValidateTitle(out.title) != nil {
			return out, invalidInput()
		}
		out.meta.ExpectedVersion = &expected
	case kc.Move:
		if _, err := exactObject(raw, "expected_parent_id", "target_parent_id"); err != nil {
			return out, err
		}
		if json.Unmarshal(raw, &out.move) != nil {
			return out, invalidInput()
		}
	case kc.DeleteSubtree:
		obj, err := exactObject(raw, "confirmation_token")
		if err != nil {
			return out, err
		}
		var token string
		if !textField(obj["confirmation_token"], &token) {
			return out, invalidInput()
		}
		out.token, err = kc.ParseConfirmationToken(token)
		if err != nil {
			return out, invalidInput()
		}
	default:
		return out, invalidInput()
	}
	return out, nil
}
