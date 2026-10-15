package texthttp

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

const requestLimit = 1 << 20

// The minimal Lookup envelope adds 84 bytes to the unchanged original JSON:
// two six-byte command names and a canonical 36-byte document ID have the same
// size. Every admitted write therefore remains representable for recovery.
const lookupRequestLimit = requestLimit + 84

const uploadBytesLimit = 1 << 20
const uploadRequestLimit = 1414488
const uploadLookupLimit = uploadRequestLimit + 84

// Raw fields retain missing/null and exact text bytes. DecodeJSON validates
// unknown and duplicate decoded keys before this additional surrogate check.
type inputWire struct {
	Parent   json.RawMessage `json:"parent_document_id"`
	Title    json.RawMessage `json:"title"`
	Expected json.RawMessage `json:"expected_version"`
	Media    json.RawMessage `json:"media_type"`
	Text     json.RawMessage `json:"text"`
	Bytes    json.RawMessage `json:"bytes_base64"`
	Command  json.RawMessage `json:"command"`
	Document json.RawMessage `json:"document_id"`
	Request  json.RawMessage `json:"request"`
}

func (v *inputWire) UnmarshalJSON(raw []byte) error {
	if !pairedEscapes(raw) {
		return invalidInput()
	}
	type plain inputWire
	var next plain
	if json.Unmarshal(raw, &next) != nil {
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
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil || out == nil || len(out) != len(fields) {
		return nil, invalidInput()
	}
	for _, name := range fields {
		if _, ok := out[name]; !ok {
			return nil, invalidInput()
		}
	}
	return out, nil
}

func textField(raw json.RawMessage, out *string) bool {
	b := bytes.TrimSpace(raw)
	return len(b) > 0 && b[0] == '"' && json.Unmarshal(b, out) == nil
}

func headers(r *http.Request, name string) []string {
	var out []string
	for key, values := range r.Header {
		if strings.EqualFold(key, name) {
			out = append(out, values...)
		}
	}
	return out
}

type intent struct {
	name         kc.CommandName
	target       kc.DocumentID
	meta         f.CommandMeta
	parent       *kc.DocumentID
	title, media string
	source       kc.SourceInput
}

func (v intent) create(project id.ProjectID) kc.CreateRequest {
	return kc.CreateRequest{ProjectID: project, DocumentID: v.target, ParentDocumentID: v.parent, Title: v.title}
}

func (v intent) digest(actor id.Actor, project id.ProjectID) (f.Digest, error) {
	switch v.name {
	case kc.Create:
		return kc.CreateDigest(actor, v.meta, v.create(project), v.source)
	case kc.Update:
		return kc.UpdateDigest(actor, v.meta, project, v.target, kc.UpdateRequest{ReplaceSource: true}, &v.source)
	}
	return "", invalidInput()
}

func decodeIntent(w http.ResponseWriter, r *http.Request, route route) (intent, error) {
	var out intent
	for key := range r.Header {
		if strings.EqualFold(key, "Content-Encoding") {
			return out, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
		}
	}
	keys := headers(r, "Idempotency-Key")
	if len(keys) != 1 || f.IdempotencyKey(keys[0]).Validate() != nil {
		return out, invalidInput()
	}
	out.meta = f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: f.IdempotencyKey(keys[0])}
	var wire inputWire
	limit := int64(requestLimit)
	if route.action == "lookup" {
		limit = lookupRequestLimit
	}
	if route.file {
		limit = uploadRequestLimit
		if route.action == "lookup" {
			limit = uploadLookupLimit
		}
	}
	if err := httpapi.DecodeJSON(w, r, &wire, limit); err != nil {
		return out, err
	}
	fields := map[string]json.RawMessage{}
	for key, value := range map[string]json.RawMessage{"parent_document_id": wire.Parent, "title": wire.Title, "expected_version": wire.Expected, "media_type": wire.Media, "text": wire.Text, "bytes_base64": wire.Bytes, "command": wire.Command, "document_id": wire.Document, "request": wire.Request} {
		if len(value) > 0 {
			fields[key] = value
		}
	}
	raw, _ := json.Marshal(fields)
	var target string
	if route.action == "lookup" {
		if _, err := exactObject(raw, "command", "document_id", "request"); err != nil {
			return out, err
		}
		var command string
		if !textField(wire.Command, &command) || !textField(wire.Document, &target) {
			return out, invalidInput()
		}
		out.name = kc.CommandName(command)
		raw = wire.Request
		originalLimit := requestLimit
		if route.file {
			originalLimit = uploadRequestLimit
		}
		if len(raw) > originalLimit {
			return out, f.NewFault(f.PayloadTooLarge, f.NotStarted)
		}
	} else {
		target = route.target
		switch route.action {
		case "create-text", "create-file":
			out.name = kc.Create
		case "replace-text", "replace-file":
			out.name = kc.Update
		default:
			return out, invalidInput()
		}
	}
	var err error
	out.target, err = f.ParseID[kc.Document](target)
	if err != nil {
		return out, invalidInput()
	}
	var obj map[string]json.RawMessage
	payloadField := "text"
	if route.file {
		payloadField = "bytes_base64"
	}
	switch out.name {
	case kc.Create:
		obj, err = exactObject(raw, "parent_document_id", "title", "media_type", payloadField)
		if err != nil {
			return out, err
		}
		parent := bytes.TrimSpace(obj["parent_document_id"])
		if !bytes.Equal(parent, []byte("null")) {
			var value string
			if !textField(parent, &value) {
				return out, invalidInput()
			}
			parsed, e := f.ParseID[kc.Document](value)
			if e != nil || parsed == out.target {
				return out, invalidInput()
			}
			out.parent = &parsed
		}
		if !textField(obj["title"], &out.title) || kc.ValidateTitle(out.title) != nil {
			return out, invalidInput()
		}
	case kc.Update:
		obj, err = exactObject(raw, "expected_version", "media_type", payloadField)
		if err != nil {
			return out, err
		}
		var expected f.Version
		if json.Unmarshal(obj["expected_version"], &expected) != nil || expected.Validate() != nil {
			return out, invalidInput()
		}
		out.meta.ExpectedVersion = &expected
	default:
		return out, invalidInput()
	}
	var text string
	if !textField(obj["media_type"], &out.media) || !textField(obj[payloadField], &text) {
		return out, invalidInput()
	}
	if route.file {
		if len(text) > base64.StdEncoding.EncodedLen(uploadBytesLimit) {
			return out, f.NewFault(f.PayloadTooLarge, f.NotStarted)
		}
		raw, e := base64.StdEncoding.Strict().DecodeString(text)
		if e != nil || base64.StdEncoding.EncodeToString(raw) != text {
			return out, invalidInput()
		}
		if len(raw) > uploadBytesLimit {
			return out, f.NewFault(f.PayloadTooLarge, f.NotStarted)
		}
		sum := sha256.Sum256(raw)
		out.source, err = kc.NewUploadSource(out.media, f.Progress(len(raw)), f.Digest("sha256:"+hex.EncodeToString(sum[:])), io.NopCloser(bytes.NewReader(raw)))
	} else {
		out.source, err = kc.NewTextSource(out.media, text)
	}
	if err != nil {
		return intent{}, invalidInput()
	}
	return out, nil
}
