package artifacthttp

import (
	"encoding/json"
	"net/http"
	"strings"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type userUploadInput struct {
	Name        json.RawMessage `json:"name"`
	Description json.RawMessage `json:"description"`
	MediaType   json.RawMessage `json:"media_type"`
	ByteSize    json.RawMessage `json:"byte_size"`
	SHA256      json.RawMessage `json:"sha256"`
}

func (v *userUploadInput) UnmarshalJSON(raw []byte) error {
	if !pairedCommandEscapes(raw) {
		return invalidInput()
	}
	type plain userUploadInput
	var out plain
	if json.Unmarshal(raw, &out) != nil {
		return invalidInput()
	}
	*v = userUploadInput(out)
	return nil
}

func userUploadHeaders(r *http.Request) (f.CommandMeta, error) {
	if r.URL == nil || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return f.CommandMeta{}, invalidInput()
	}
	for key := range r.Header {
		if strings.EqualFold(key, "Content-Encoding") {
			return f.CommandMeta{}, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
		}
		if strings.EqualFold(key, "Range") || strings.EqualFold(key, "If-Range") {
			return f.CommandMeta{}, invalidInput()
		}
	}
	keys := commandHeaders(r, "Idempotency-Key")
	if len(keys) != 1 || len(commandHeaders(r, "Content-Type")) != 1 {
		return f.CommandMeta{}, invalidInput()
	}
	meta := f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: f.IdempotencyKey(keys[0])}
	if meta.Validate() != nil {
		return f.CommandMeta{}, invalidInput()
	}
	return meta, nil
}

func decodeUserUploadIntent(w http.ResponseWriter, r *http.Request) (ac.UserUploadIntent, error) {
	var out ac.UserUploadIntent
	var wire userUploadInput
	if err := httpapi.DecodeJSON(w, r, &wire, maxUserUploadCommandBytes); err != nil {
		return out, err
	}
	if !commandString(wire.Name, &out.Display.Name) || !commandString(wire.MediaType, &out.MediaType) || len(wire.ByteSize) == 0 || json.Unmarshal(wire.ByteSize, &out.ByteSize) != nil {
		return out, invalidInput()
	}
	if len(wire.Description) != 0 && !commandString(wire.Description, &out.Display.Description) {
		return out, invalidInput()
	}
	if len(wire.SHA256) != 0 {
		var text string
		if !commandString(wire.SHA256, &text) {
			return out, invalidInput()
		}
		digest, err := f.ParseDigest(text)
		if err != nil {
			return out, invalidInput()
		}
		out.SHA256 = &digest
	}
	var err error
	out.MediaType, err = oc.NormalizeMediaType(out.MediaType)
	if err != nil || out.Validate() != nil {
		return out, invalidInput()
	}
	return out, nil
}

func userUploadPayloadInput(w http.ResponseWriter, r *http.Request) (string, error) {
	if r.Body == nil || r.ContentLength < 0 || len(r.TransferEncoding) != 0 {
		return "", invalidInput()
	}
	if r.ContentLength > oc.MaxObjectSize {
		return "", f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	media, err := oc.NormalizeMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != r.Header.Get("Content-Type") {
		return "", invalidInput()
	}
	r.Body = http.MaxBytesReader(w, r.Body, oc.MaxObjectSize)
	return media, nil
}
