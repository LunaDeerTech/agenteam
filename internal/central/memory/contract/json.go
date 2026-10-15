package contract

import (
	"bytes"
	"encoding/json"
	"io"
)

func strictJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return invalid()
	}
	if walkJSON(d, first, 0) != nil {
		return invalid()
	}
	if _, e = d.Token(); e != io.EOF {
		return invalid()
	}
	return nil
}
func walkJSON(d *json.Decoder, t json.Token, depth int) error {
	if depth > 16 || t == nil {
		return invalid()
	}
	switch v := t.(type) {
	case json.Delim:
		switch v {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				key, ok := k.(string)
				if e != nil || !ok || seen[key] {
					return invalid()
				}
				seen[key] = true
				t, e = d.Token()
				if e != nil || walkJSON(d, t, depth+1) != nil {
					return invalid()
				}
			}
			t, e := d.Token()
			if e != nil || t != json.Delim('}') {
				return invalid()
			}
		case '[':
			for d.More() {
				t, e := d.Token()
				if e != nil || walkJSON(d, t, depth+1) != nil {
					return invalid()
				}
			}
			t, e := d.Token()
			if e != nil || t != json.Delim(']') {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	return nil
}
