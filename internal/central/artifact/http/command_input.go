package artifacthttp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// The same full intent fits every entry, including recovery: 1MiB of decoded
// content at six JSON bytes per byte plus bounded display/MIME and framing.
const maxCommandRequestBytes int64 = 7 << 20

type commandInput struct {
	Name        json.RawMessage `json:"name"`
	Description json.RawMessage `json:"description"`
	MediaType   json.RawMessage `json:"media_type"`
	Content     json.RawMessage `json:"content"`
}

func (v *commandInput) UnmarshalJSON(raw []byte) error {
	if !pairedCommandEscapes(raw) {
		return invalidInput()
	}
	type plain commandInput
	var next plain
	if json.Unmarshal(raw, &next) != nil {
		return invalidInput()
	}
	*v = commandInput(next)
	return nil
}

// encoding/json otherwise replaces lone UTF-16 surrogates with U+FFFD. Reject
// them before decoding so an original intent is never silently rewritten.
func pairedCommandEscapes(raw []byte) bool {
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

type contentIntent struct {
	meta           f.CommandMeta
	display        ac.Display
	media, content string
}

func commandHeaders(r *http.Request, name string) []string {
	var out []string
	for key, values := range r.Header {
		if strings.EqualFold(key, name) {
			out = append(out, values...)
		}
	}
	return out
}

func commandString(raw json.RawMessage, out *string) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, out) == nil
}

func decodeContentIntent(w http.ResponseWriter, r *http.Request) (contentIntent, error) {
	var out contentIntent
	if r.URL == nil || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return out, invalidInput()
	}
	for key := range r.Header {
		if strings.EqualFold(key, "Content-Encoding") {
			return out, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
		}
		if strings.EqualFold(key, "Range") || strings.EqualFold(key, "If-Range") {
			return out, invalidInput()
		}
	}
	keys := commandHeaders(r, "Idempotency-Key")
	if len(keys) != 1 || f.IdempotencyKey(keys[0]).Validate() != nil || len(commandHeaders(r, "Content-Type")) != 1 {
		return out, invalidInput()
	}
	out.meta = f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: f.IdempotencyKey(keys[0])}
	if out.meta.Validate() != nil {
		return out, invalidInput()
	}
	var wire commandInput
	if err := httpapi.DecodeJSON(w, r, &wire, maxCommandRequestBytes); err != nil {
		return out, err
	}
	if !commandString(wire.Name, &out.display.Name) || !commandString(wire.MediaType, &out.media) || !commandString(wire.Content, &out.content) {
		return out, invalidInput()
	}
	if len(wire.Description) != 0 && !commandString(wire.Description, &out.display.Description) {
		return out, invalidInput()
	}
	if out.display.Validate() != nil {
		return out, invalidInput()
	}
	if err := ac.ValidateInline(out.content); err != nil {
		return out, err
	}
	var err error
	out.media, err = oc.NormalizeMediaType(out.media)
	if err != nil {
		return out, invalidInput()
	}
	return out, nil
}
