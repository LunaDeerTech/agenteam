package identity

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"
)

// The file is deliberately a flat, closed schema. Reject replacement-decoded
// strings before the normal decoder, including escaped duplicate field names.
func decodeFile(raw []byte) (Identity, error) {
	if len(raw) == 0 || len(raw) > MaxFileBytes || !utf8.Valid(raw) {
		return Identity{}, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if t, e := d.Token(); e != nil || t != json.Delim('{') {
		return Identity{}, ErrInvalid
	}
	fields := map[string]json.RawMessage{}
	allowed := map[string]bool{"version": true, "state": true, "central_url": true, "runner_id": true, "root_path": true, "private_seed": true}
	for d.More() {
		token, e := d.Token()
		if e != nil {
			return Identity{}, ErrInvalid
		}
		key, ok := token.(string)
		if !ok || !allowed[key] || fields[key] != nil {
			return Identity{}, ErrInvalid
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Identity{}, ErrInvalid
		}
		fields[key] = value
	}
	if t, e := d.Token(); e != nil || t != json.Delim('}') {
		return Identity{}, ErrInvalid
	}
	if _, e := d.Token(); e != io.EOF || len(fields) != len(allowed) {
		return Identity{}, ErrInvalid
	}
	for key, value := range fields {
		if key == "version" {
			continue
		}
		if len(value) < 2 || value[0] != '"' {
			return Identity{}, ErrInvalid
		}
		// Validate UTF-16 pairs before JSON can replace unpaired code units.
		if !validStringEscapes(value) {
			return Identity{}, ErrInvalid
		}
	}
	var w fileWire
	if json.Unmarshal(raw, &w) != nil || w.Version != 1 || len(w.PrivateSeed) != 43 {
		return Identity{}, ErrInvalid
	}
	secret, e := base64.RawURLEncoding.DecodeString(w.PrivateSeed)
	if e != nil || len(secret) != 32 || base64.RawURLEncoding.EncodeToString(secret) != w.PrivateSeed {
		clear(secret)
		return Identity{}, ErrInvalid
	}
	defer clear(secret)
	v := Identity{config: Configuration{w.CentralURL, w.RunnerID, w.RootPath}, state: w.State, valid: true}
	copy(v.seed[:], secret)
	if v.Validate() != nil {
		clear(v.seed[:])
		return Identity{}, ErrInvalid
	}
	return v, nil
}

func validStringEscapes(raw []byte) bool {
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw)-1 {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		n, e := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if e != nil {
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
			next, e := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if e != nil || next < 0xdc00 || next > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
