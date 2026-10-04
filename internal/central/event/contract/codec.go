package contract

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"reflect"
	"strconv"
	"unicode/utf8"
)

type Codec[T any] interface {
	Encode(T) ([]byte, error)
	Decode([]byte) (T, error)
}

// JSONCodec supplies a strict typed JSON decoder. Required fields and domain
// invariants remain the registered schema validator's responsibility.
type JSONCodec[T any] struct{}

func (JSONCodec[T]) Encode(value T) ([]byte, error) { return json.Marshal(value) }
func (JSONCodec[T]) Decode(raw []byte) (T, error) {
	var value T
	err := decodeStrict(raw, &value)
	return value, err
}

func decodeStrict(raw []byte, out any) error {
	if _, err := canonicalJSON(raw); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return invalid()
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return invalid()
	}
	return nil
}

// The protocol header has exact field names. encoding/json's case-insensitive
// struct matching alone would allow an alias to overwrite an earlier field.
// Required presence is separate from value validation (year 0001 is valid).
func exactFields(raw []byte, required, optional []string) error {
	if _, err := canonicalJSON(raw); err != nil {
		return invalid()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return invalid()
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, name := range required {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return invalid()
		}
		allowed[name] = true
	}
	for _, name := range optional {
		allowed[name] = true
	}
	for name := range fields {
		if !allowed[name] {
			return invalid()
		}
	}
	return nil
}

func canonicalJSON(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > MaxPayloadBytes || !utf8.Valid(raw) {
		return nil, invalid()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := jsonValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, invalid()
	}
	b, err := json.Marshal(v)
	if err != nil || len(b) > MaxPayloadBytes {
		return nil, invalid()
	}
	return b, nil
}

func jsonValue(d *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, invalid()
	}
	token, err := d.Token()
	if err != nil {
		return nil, invalid()
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			object := map[string]any{}
			for d.More() {
				k, err := d.Token()
				name, ok := k.(string)
				if err != nil || !ok {
					return nil, invalid()
				}
				if _, exists := object[name]; exists {
					return nil, invalid()
				}
				item, err := jsonValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				object[name] = item
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return nil, invalid()
			}
			return object, nil
		case '[':
			items := []any{}
			for d.More() {
				item, err := jsonValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return nil, invalid()
			}
			return items, nil
		}
		return nil, invalid()
	case json.Number:
		f, err := strconv.ParseFloat(string(value), 64)
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return nil, invalid()
		}
		return value, nil
	case string, bool, nil:
		return value, nil
	default:
		return nil, invalid()
	}
}

func typedPayload(t reflect.Type, seen map[reflect.Type]bool) bool {
	if t == reflect.TypeFor[json.RawMessage]() || t.Kind() == reflect.Interface {
		return false
	}
	if seen[t] {
		return true
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return typedPayload(t.Elem(), seen)
	case reflect.Map:
		return t.Key().Kind() == reflect.String && typedPayload(t.Elem(), seen)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.IsExported() && f.Tag.Get("json") != "-" && !typedPayload(f.Type, seen) {
				return false
			}
		}
		return true
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}
