package cursor

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

var errEncoding = errors.New("INVALID_CANONICAL_ENCODING")

// CanonicalJSON implements canonical-v1 for already typed projections. It
// sorts object keys, retains array order, and rejects duplicate keys, floats,
// imprecise/out-of-range integers and trailing values. Schema owners expand
// defaults, normalize scalars and sort sets before calling it.
func CanonicalJSON(raw []byte) ([]byte, error) {
	if !utf8.Valid(raw) {
		return nil, errEncoding
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := value(d, 0)
	if err != nil {
		return nil, errEncoding
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errEncoding
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, errEncoding
	}
	return b, nil
}
func value(d *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, errEncoding
	}
	token, err := d.Token()
	if err != nil {
		return nil, errEncoding
	}
	switch t := token.(type) {
	case json.Delim:
		switch t {
		case '{':
			m := map[string]any{}
			for d.More() {
				key, err := d.Token()
				s, ok := key.(string)
				if err != nil || !ok {
					return nil, errEncoding
				}
				if _, exists := m[s]; exists {
					return nil, errEncoding
				}
				v, err := value(d, depth+1)
				if err != nil {
					return nil, err
				}
				m[s] = v
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return nil, errEncoding
			}
			return m, nil
		case '[':
			a := []any{}
			for d.More() {
				v, err := value(d, depth+1)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return nil, errEncoding
			}
			return a, nil
		}
		return nil, errEncoding
	case json.Number:
		n, err := strconv.ParseInt(string(t), 10, 64)
		if err != nil || strconv.FormatInt(n, 10) != string(t) {
			return nil, errEncoding
		}
		return json.Number(strconv.FormatInt(n, 10)), nil
	case string, bool, nil:
		return t, nil
	}
	return nil, errEncoding
}
func strict(raw []byte, out any) ([]byte, error) {
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		return nil, errEncoding
	}
	var shape any
	d := json.NewDecoder(bytes.NewReader(canonical))
	d.UseNumber()
	if d.Decode(&shape) != nil || !exactShape(shape, reflect.TypeOf(out).Elem()) {
		return nil, errEncoding
	}
	d = json.NewDecoder(bytes.NewReader(canonical))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return nil, errEncoding
	}
	return canonical, nil
}
func exactShape(v any, t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Pointer:
		return v != nil && exactShape(v, t.Elem())
	case reflect.Struct:
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name != "-" {
				fields[name] = f.Type
			}
		}
		for name, x := range m {
			ft, ok := fields[name]
			if !ok || !exactShape(x, ft) {
				return false
			}
		}
		return true
	case reflect.Slice:
		a, ok := v.([]any)
		if !ok {
			return false
		}
		for _, x := range a {
			if !exactShape(x, t.Elem()) {
				return false
			}
		}
		return true
	case reflect.String:
		_, ok := v.(string)
		return ok
	case reflect.Int, reflect.Int64:
		_, ok := v.(json.Number)
		return ok
	default:
		return false
	}
}
