// Package contract owns stable credential references and the three D01 lease
// ports. It has no database driver or identity implementation dependency.
package contract

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

const MaxValueBytes = 65536

var errMaterial = errors.New("SECRET_MATERIAL_UNAVAILABLE")

type materialState struct {
	mu        sync.Mutex
	bytes     []byte
	destroyed bool
}
type SecretMaterial struct{ data func() *materialState }

func NewSecretMaterial(value []byte) (SecretMaterial, error) {
	if len(value) < 1 || len(value) > MaxValueBytes {
		return SecretMaterial{}, errMaterial
	}
	state := &materialState{bytes: append([]byte(nil), value...)}
	return SecretMaterial{data: func() *materialState { return state }}, nil
}

// Use lends a private copy only for this synchronous backend operation. Callers
// must not retain it or convert it into DTO/log fields. The copy is zeroed even
// if the callback panics; Go runtime/consumer-created copies cannot be erased.
func (m SecretMaterial) Use(fn func([]byte) error) error {
	if m.data == nil || fn == nil {
		return errMaterial
	}
	s := m.data()
	s.mu.Lock()
	if s.destroyed {
		s.mu.Unlock()
		return errMaterial
	}
	copy := append([]byte(nil), s.bytes...)
	s.mu.Unlock()
	defer clear(copy)
	return fn(copy)
}
func (m SecretMaterial) Destroy() {
	if m.data == nil {
		return
	}
	s := m.data()
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.bytes)
	s.bytes = nil
	s.destroyed = true
}
func (m SecretMaterial) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "secret_material") }
func (m SecretMaterial) MarshalJSON() ([]byte, error) { return []byte(`"secret_material"`), nil }
func (m *SecretMaterial) UnmarshalJSON([]byte) error  { return errMaterial }
func (m SecretMaterial) LogValue() slog.Value         { return slog.StringValue("secret_material") }
