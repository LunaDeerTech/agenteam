package runnerprotocol

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
)

const MaxAuthorizationBytes = 2048

// Nonce owns one canonical 32-byte challenge. Wire is deliberately explicit;
// default formatting and serialization are safe for diagnostic use.
type Nonce struct {
	bytes [32]byte
	valid bool
}

func NewNonce() (Nonce, error) {
	n := Nonce{valid: true}
	if _, err := rand.Read(n.bytes[:]); err != nil {
		return Nonce{}, ErrInvalid
	}
	return n, nil
}
func ParseNonce(wire string) (Nonce, error) {
	if len(wire) != 43 {
		return Nonce{}, ErrInvalid
	}
	bytes, err := base64.RawURLEncoding.DecodeString(wire)
	if err != nil || len(bytes) != 32 || base64.RawURLEncoding.EncodeToString(bytes) != wire {
		clear(bytes)
		return Nonce{}, ErrInvalid
	}
	defer clear(bytes)
	n := Nonce{valid: true}
	copy(n.bytes[:], bytes)
	return n, nil
}
func (n Nonce) Valid() bool { return n.valid }
func (n Nonce) Wire() (string, error) {
	if !n.valid {
		return "", ErrInvalid
	}
	return base64.RawURLEncoding.EncodeToString(n.bytes[:]), nil
}
func (n Nonce) Digest() ([sha256.Size]byte, error) {
	if !n.valid {
		return [sha256.Size]byte{}, ErrInvalid
	}
	return sha256.Sum256(n.bytes[:]), nil
}
func (n Nonce) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_nonce") }
func (n Nonce) MarshalJSON() ([]byte, error) { return []byte(`"runner_nonce"`), nil }
func (n Nonce) LogValue() slog.Value         { return slog.StringValue("runner_nonce") }

// UnixSeconds is signed canonical decimal: no plus, padding, exponent or -0.
type UnixSeconds string

func (s UnixSeconds) Seconds() (int64, error) {
	if len(s) == 0 || len(s) > 20 {
		return 0, ErrInvalid
	}
	n, err := strconv.ParseInt(string(s), 10, 64)
	if err != nil || strconv.FormatInt(n, 10) != string(s) {
		return 0, ErrInvalid
	}
	return n, nil
}
func NewUnixSeconds(seconds int64) UnixSeconds { return UnixSeconds(strconv.FormatInt(seconds, 10)) }

// Authentication contains no key. Its validated tuple is immutable; checking
// its signature never proves a current credential, nonce or DB clock window.
type Authentication struct {
	runner    ID
	nonce     Nonce
	timestamp UnixSeconds
	signature [ed25519.SignatureSize]byte
	valid     bool
}

func NewAuthentication(runner ID, nonce Nonce, timestamp UnixSeconds, signature []byte) (Authentication, error) {
	if _, err := SigningBytes(runner, nonce, timestamp); err != nil || len(signature) != ed25519.SignatureSize {
		return Authentication{}, ErrInvalid
	}
	a := Authentication{runner: runner, nonce: nonce, timestamp: timestamp, valid: true}
	copy(a.signature[:], signature)
	return a, nil
}
func (a Authentication) RunnerID() ID                           { return a.runner }
func (a Authentication) Nonce() Nonce                           { return a.nonce }
func (a Authentication) Timestamp() UnixSeconds                 { return a.timestamp }
func (a Authentication) Signature() [ed25519.SignatureSize]byte { return a.signature }
func (a Authentication) Valid() bool                            { return a.valid }
func (a Authentication) Verify(public [ed25519.PublicKeySize]byte) bool {
	if !a.valid {
		return false
	}
	message, err := SigningBytes(a.runner, a.nonce, a.timestamp)
	return err == nil && ed25519.Verify(public[:], message, a.signature[:])
}
func (a Authentication) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "runner_authentication")
}
func (a Authentication) MarshalJSON() ([]byte, error) { return []byte(`"runner_authentication"`), nil }
func (a Authentication) LogValue() slog.Value         { return slog.StringValue("runner_authentication") }

// SigningBytes is the single v1 signing purpose. It signs exact canonical wire
// characters, with a final LF, without JSON quoting, trimming or normalization.
func SigningBytes(runner ID, nonce Nonce, timestamp UnixSeconds) ([]byte, error) {
	if !runner.Valid() {
		return nil, ErrInvalid
	}
	n, err := nonce.Wire()
	if err != nil {
		return nil, ErrInvalid
	}
	if _, err = timestamp.Seconds(); err != nil {
		return nil, ErrInvalid
	}
	return []byte("agenteam-runner-control-v1\n" + string(runner) + "\n" + n + "\n" + string(timestamp) + "\n"), nil
}

type authenticationWire struct {
	RunnerID  ID          `json:"runner_id"`
	Nonce     string      `json:"nonce"`
	Timestamp UnixSeconds `json:"timestamp"`
	Signature string      `json:"signature"`
}

func EncodeAuthorization(a Authentication) (string, error) {
	if !a.valid {
		return "", ErrInvalid
	}
	nonce, err := a.nonce.Wire()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(authenticationWire{a.runner, nonce, a.timestamp, base64.RawURLEncoding.EncodeToString(a.signature[:])})
	if err != nil {
		return "", ErrInvalid
	}
	defer clear(raw)
	header := "Runner " + base64.RawURLEncoding.EncodeToString(raw)
	if len(header) > MaxAuthorizationBytes {
		return "", ErrTooLarge
	}
	return header, nil
}

func DecodeAuthorization(header string) (Authentication, error) {
	if len(header) > MaxAuthorizationBytes {
		return Authentication{}, ErrTooLarge
	}
	if !strings.HasPrefix(header, "Runner ") {
		return Authentication{}, ErrInvalid
	}
	encoded := header[len("Runner "):]
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != encoded {
		clear(raw)
		return Authentication{}, ErrInvalid
	}
	defer clear(raw)
	value, err := parseJSON(raw)
	if err != nil {
		return Authentication{}, err
	}
	if _, err = fields(value, words("runner_id nonce timestamp signature"), nil); err != nil {
		return Authentication{}, err
	}
	var wire authenticationWire
	if json.Unmarshal(raw, &wire) != nil || len(wire.Signature) != 86 {
		return Authentication{}, ErrInvalid
	}
	nonce, err := ParseNonce(wire.Nonce)
	if err != nil {
		return Authentication{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(wire.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || base64.RawURLEncoding.EncodeToString(signature) != wire.Signature {
		clear(signature)
		return Authentication{}, ErrInvalid
	}
	defer clear(signature)
	return NewAuthentication(wire.RunnerID, nonce, wire.Timestamp, signature)
}
