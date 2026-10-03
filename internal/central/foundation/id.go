// Package foundation defines Central's domain-independent scalar contracts.
package foundation

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"
)

var errID = errors.New("invalid UUIDv7")

// ID is a nonzero RFC 9562 UUIDv7. K distinguishes identities at compile time.
// The zero value is invalid; optional identities must use a pointer.
type ID[K any] struct{ value [16]byte }

func NewID[K any]() (ID[K], error) {
	return newID[K](time.Now(), rand.Reader)
}

func newID[K any](now time.Time, entropy io.Reader) (ID[K], error) {
	var id ID[K]
	ms := now.UnixMilli()
	if ms < 0 || ms > 1<<48-1 {
		return id, errID
	}
	if _, err := io.ReadFull(entropy, id.value[6:]); err != nil {
		return ID[K]{}, errors.New("UUID entropy unavailable")
	}
	for i := 5; i >= 0; i-- {
		id.value[i] = byte(ms)
		ms >>= 8
	}
	id.value[6] = id.value[6]&0x0f | 0x70
	id.value[8] = id.value[8]&0x3f | 0x80
	return id, nil
}

func ParseID[K any](s string) (ID[K], error) {
	var id ID[K]
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return id, errID
	}
	var compact [32]byte
	j := 0
	for i := range len(s) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !lowerHex(s[i]) {
			return id, errID
		}
		compact[j] = s[i]
		j++
	}
	if _, err := hex.Decode(id.value[:], compact[:]); err != nil {
		return ID[K]{}, errID
	}
	if err := id.Validate(); err != nil {
		return ID[K]{}, err
	}
	return id, nil
}

func (id ID[K]) Validate() error {
	if id.value[6]>>4 != 7 || id.value[8]>>6 != 2 {
		return errID
	}
	return nil
}

func (id ID[K]) String() string {
	var s [36]byte
	hex.Encode(s[:8], id.value[:4])
	s[8] = '-'
	hex.Encode(s[9:13], id.value[4:6])
	s[13] = '-'
	hex.Encode(s[14:18], id.value[6:8])
	s[18] = '-'
	hex.Encode(s[19:23], id.value[8:10])
	s[23] = '-'
	hex.Encode(s[24:], id.value[10:])
	return string(s[:])
}

func (id ID[K]) MarshalText() ([]byte, error) {
	if err := id.Validate(); err != nil {
		return nil, err
	}
	return []byte(id.String()), nil
}

func (id *ID[K]) UnmarshalText(b []byte) error {
	value, err := ParseID[K](string(b))
	if err == nil {
		*id = value
	}
	return err
}

func (id ID[K]) MarshalJSON() ([]byte, error) {
	b, err := id.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(b))
}

func (id *ID[K]) UnmarshalJSON(b []byte) error {
	s, err := scalarString(b)
	if err != nil {
		return errID
	}
	return id.UnmarshalText([]byte(s))
}

func lowerHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' }

// Request marks a transport attempt, never a business idempotency identity.
type Request struct{}

type CommandMeta struct {
	RequestID       ID[Request]    `json:"request_id"`
	IdempotencyKey  IdempotencyKey `json:"idempotency_key"`
	ExpectedVersion *Version       `json:"expected_version,omitempty"`
}

func (m CommandMeta) Validate() error {
	if err := m.RequestID.Validate(); err != nil {
		return err
	}
	if err := m.IdempotencyKey.Validate(); err != nil {
		return err
	}
	if m.ExpectedVersion != nil {
		return m.ExpectedVersion.Validate()
	}
	return nil
}
