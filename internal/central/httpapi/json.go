package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"mime"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

const (
	DefaultMaxJSONBytes int64 = 1 << 20
	MaxJSONDepth              = 64
)

var (
	errJSONInput    = errors.New("invalid JSON input")
	errDTO          = errors.New("invalid JSON DTO declaration")
	unmarshalerType = reflect.TypeFor[json.Unmarshaler]()
	rawMessageType  = reflect.TypeFor[json.RawMessage]()
)

// DecodeJSON accepts an object into a nonnil pointer to an explicitly tagged DTO
// struct. Nested structs likewise require explicit JSON names; dynamic maps and
// interface fields must be deliberately declared by that DTO. Integers in dynamic
// fields remain json.Number. A failed decode leaves dst unchanged.
// maxBytes == 0 selects 1 MiB. Upload and streaming routes must use other readers.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	v := reflect.ValueOf(dst)
	if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct || maxBytes < 0 {
		return foundation.NewFault(foundation.InternalError, foundation.Unknown).WithCause(errDTO)
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(r.Header.Values("Content-Type")) > 1 {
		return inputFault(foundation.UnsupportedMediaType)
	}
	for key, value := range params {
		if key != "charset" || !strings.EqualFold(value, "utf-8") {
			return inputFault(foundation.UnsupportedMediaType)
		}
	}
	if encoding := r.Header.Get("Content-Encoding"); encoding != "" || len(r.Header.Values("Content-Encoding")) > 1 {
		return inputFault(foundation.UnsupportedMediaType)
	}
	if maxBytes == 0 {
		maxBytes = DefaultMaxJSONBytes
	}
	if r.Body == nil {
		return inputFault(foundation.InvalidArgument)
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return inputFault(foundation.PayloadTooLarge)
		}
		return inputFault(foundation.InvalidArgument).WithCause(err)
	}
	if !utf8.Valid(body) {
		return inputFault(foundation.InvalidArgument)
	}
	scanner := json.NewDecoder(bytes.NewReader(body))
	scanner.UseNumber()
	node, err := scanJSON(scanner, 0)
	if err != nil {
		return inputFault(foundation.InvalidArgument)
	}
	if _, ok := node.(map[string]any); !ok {
		return inputFault(foundation.InvalidArgument)
	}
	if _, err := scanner.Token(); err != io.EOF {
		return inputFault(foundation.InvalidArgument)
	}
	if err := validateDTO(node, v.Elem().Type()); err != nil {
		if errors.Is(err, errDTO) {
			return foundation.NewFault(foundation.InternalError, foundation.Unknown).WithCause(err)
		}
		return inputFault(foundation.InvalidArgument)
	}
	// Decode directly from the original bytes, never a map re-encoding or float64
	// intermediary. Named scalar unmarshallers enforce their own wire contracts.
	next := reflect.New(v.Elem().Type())
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(next.Interface()); err != nil {
		return inputFault(foundation.InvalidArgument)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return inputFault(foundation.InvalidArgument)
	}
	v.Elem().Set(next.Elem())
	return nil
}

func inputFault(code foundation.Code) *foundation.Fault {
	return foundation.NewFault(code, foundation.NotStarted)
}

// scanJSON validates every object, including dynamic fields and custom-decoded
// values. Token decodes key escapes before duplicate comparison.
func scanJSON(decoder *json.Decoder, depth int) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, errJSONInput
	}
	delim, container := token.(json.Delim)
	if !container {
		return token, nil
	}
	if depth >= MaxJSONDepth {
		return nil, errJSONInput
	}
	switch delim {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, errJSONInput
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errJSONInput
			}
			if _, exists := object[key]; exists {
				return nil, errJSONInput
			}
			value, err := scanJSON(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return nil, errJSONInput
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.More() {
			value, err := scanJSON(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return nil, errJSONInput
		}
		return array, nil
	default:
		return nil, errJSONInput
	}
}

func validateDTO(node any, t reflect.Type) error {
	if t.Kind() == reflect.Pointer {
		if node == nil {
			return nil
		}
		return validateDTO(node, t.Elem())
	}
	if t == rawMessageType || t.Kind() == reflect.Interface {
		return nil
	}
	if node == nil {
		// Optional scalar values require a pointer. Let named scalar decoders
		// reject null themselves as well; do not silently turn null into zero.
		if t.Kind() == reflect.Map || t.Kind() == reflect.Slice {
			return nil
		}
		return errJSONInput
	}
	switch value := node.(type) {
	case map[string]any:
		switch t.Kind() {
		case reflect.Struct:
			fields, err := dtoFields(t)
			if err != nil {
				return err
			}
			for key, child := range value {
				field, ok := fields[key]
				if !ok {
					return errJSONInput
				}
				if err := validateDTO(child, field); err != nil {
					return err
				}
			}
		case reflect.Map:
			if t.Key().Kind() != reflect.String {
				return errDTO
			}
			for _, child := range value {
				if err := validateDTO(child, t.Elem()); err != nil {
					return err
				}
			}
		default:
			return errJSONInput
		}
	case []any:
		if t.Kind() != reflect.Slice && t.Kind() != reflect.Array {
			return errJSONInput
		}
		if t.Kind() == reflect.Array && len(value) != t.Len() {
			return errJSONInput
		}
		for _, child := range value {
			if err := validateDTO(child, t.Elem()); err != nil {
				return err
			}
		}
	default:
		if t.Kind() == reflect.Struct && !reflect.PointerTo(t).Implements(unmarshalerType) {
			return errJSONInput
		}
		if number, ok := value.(json.Number); ok && (t.Kind() == reflect.Float32 || t.Kind() == reflect.Float64) {
			// A declared float may represent fractional quantities, but must not
			// silently round an integral value (including exponent notation).
			parsed, err := strconv.ParseFloat(string(number), t.Bits())
			if err != nil {
				return errJSONInput
			}
			if !exactIntegralFloat(string(number), parsed) {
				return errJSONInput
			}
		}
	}
	return nil
}

// Compare integral decimal values without expanding untrusted exponents into
// enormous big.Int denominators. Finite nonzero floats need at most 309 digits.
func exactIntegralFloat(literal string, parsed float64) bool {
	mantissa, exponentText, hasExponent := strings.Cut(strings.ToLower(literal), "e")
	negative := strings.HasPrefix(mantissa, "-")
	mantissa = strings.TrimPrefix(mantissa, "-")
	whole, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(whole+fraction, "0")
	if digits == "" {
		return true
	}
	if parsed == 0 {
		return false
	} // A nonzero quantity must not underflow silently.
	exponent := int64(0)
	if hasExponent {
		var err error
		exponent, err = strconv.ParseInt(exponentText, 10, 32)
		if err != nil {
			return false
		}
	}
	significant := strings.TrimRight(digits, "0")
	scale := exponent - int64(len(fraction)) + int64(len(digits)-len(significant))
	if scale < 0 {
		return true
	} // Explicitly declared fractional floating quantity.
	if int64(len(significant))+scale > 309 {
		return false
	}
	decimal := significant + strings.Repeat("0", int(scale))
	if negative {
		decimal = "-" + decimal
	}
	exact, ok := new(big.Int).SetString(decimal, 10)
	if !ok {
		return false
	}
	actual, accuracy := new(big.Float).SetFloat64(parsed).Int(nil)
	return accuracy == big.Exact && actual.Cmp(exact) == 0
}

func dtoFields(t reflect.Type) (map[string]reflect.Type, error) {
	fields := make(map[string]reflect.Type)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			return nil, errDTO
		}
		if _, duplicate := fields[name]; duplicate {
			return nil, errDTO
		}
		fields[name] = field.Type
	}
	return fields, nil
}
