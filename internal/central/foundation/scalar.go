package foundation

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
)

var errScalar = errors.New("invalid scalar encoding")

func scalarString(b []byte) (string, error) {
	b = bytes.TrimSpace(b)
	if len(b) < 2 || b[0] != '"' {
		return "", errScalar
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return "", errScalar
	}
	return s, nil
}

func parseInteger(s string, minimum int64) (int64, error) {
	if len(s) == 0 || len(s) > 19 || len(s) > 1 && s[0] == '0' {
		return 0, errScalar
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0, errScalar
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < minimum {
		return 0, errScalar
	}
	return n, nil
}

func integerText(n, minimum int64) ([]byte, error) {
	if n < minimum {
		return nil, errScalar
	}
	return strconv.AppendInt(nil, n, 10), nil
}

func integerJSON(n, minimum int64) ([]byte, error) {
	b, err := integerText(n, minimum)
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(b))
}

// Digest is a lowercase sha256:<64 hex digits> value, not an authorization token.
type Digest string

func ParseDigest(s string) (Digest, error) {
	d := Digest(s)
	if err := d.Validate(); err != nil {
		return "", err
	}
	return d, nil
}

func (d Digest) Validate() error {
	if len(d) != 71 || d[:7] != "sha256:" {
		return errScalar
	}
	for i := 7; i < len(d); i++ {
		if !lowerHex(d[i]) {
			return errScalar
		}
	}
	return nil
}

func (d Digest) String() string { return string(d) }
func (d Digest) MarshalText() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return []byte(d), nil
}
func (d *Digest) UnmarshalText(b []byte) error {
	v, err := ParseDigest(string(b))
	if err == nil {
		*d = v
	}
	return err
}
func (d Digest) MarshalJSON() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(string(d))
}
func (d *Digest) UnmarshalJSON(b []byte) error {
	s, err := scalarString(b)
	if err != nil {
		return err
	}
	return d.UnmarshalText([]byte(s))
}

// IdempotencyKey identifies a caller's business intent, independently of Request.
type IdempotencyKey string

func ParseIdempotencyKey(s string) (IdempotencyKey, error) {
	k := IdempotencyKey(s)
	if err := k.Validate(); err != nil {
		return "", err
	}
	return k, nil
}
func (k IdempotencyKey) Validate() error {
	if len(k) < 1 || len(k) > 128 {
		return errScalar
	}
	for i := range len(k) {
		c := k[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == ':' || c == '/' || c == '-') {
			return errScalar
		}
	}
	return nil
}
func (k IdempotencyKey) String() string { return string(k) }
func (k IdempotencyKey) MarshalText() ([]byte, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}
	return []byte(k), nil
}
func (k *IdempotencyKey) UnmarshalText(b []byte) error {
	v, err := ParseIdempotencyKey(string(b))
	if err == nil {
		*k = v
	}
	return err
}
func (k IdempotencyKey) MarshalJSON() ([]byte, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(string(k))
}
func (k *IdempotencyKey) UnmarshalJSON(b []byte) error {
	s, err := scalarString(b)
	if err != nil {
		return err
	}
	return k.UnmarshalText([]byte(s))
}
