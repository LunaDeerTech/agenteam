// Package identity owns the Runner's private local Ed25519 identity. Its default
// formatting never serializes credentials; only the locked file owner may persist them.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

const MaxFileBytes = 8192

type State string

const (
	Pending State = "pending"
	Active  State = "active"
)

var (
	ErrInvalid     = errors.New("invalid runner identity")
	ErrUnsafeFile  = errors.New("unsafe runner identity file")
	ErrLocked      = errors.New("runner identity is already in use")
	ErrClosed      = errors.New("runner identity owner is closed")
	ErrUnavailable = errors.New("runner identity storage unavailable")
	ErrUnsupported = errors.New("runner identity platform is unsupported")
)

type Configuration struct {
	CentralURL string
	RunnerID   p.ID
	RootPath   string
}

func (c Configuration) Validate() error {
	u, e := url.Parse(c.CentralURL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.String() != c.CentralURL || len(c.CentralURL) > 2048 || strings.ToLower(u.Host) != u.Host || strings.ContainsAny(u.Host, "%\\\x00") || !c.RunnerID.Valid() {
		return ErrInvalid
	}
	if len(c.RootPath) == 0 || len(c.RootPath) > 4096 || !utf8.ValidString(c.RootPath) || !strings.HasPrefix(c.RootPath, "/") || path.Clean(c.RootPath) != c.RootPath || strings.ContainsAny(c.RootPath, "\\\x00") {
		return ErrInvalid
	}
	return nil
}
func (c Configuration) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "runner_identity_configuration")
}
func (c Configuration) MarshalJSON() ([]byte, error) {
	return []byte(`"runner_identity_configuration"`), nil
}
func (c Configuration) LogValue() slog.Value {
	return slog.StringValue("runner_identity_configuration")
}

type Identity struct {
	config Configuration
	state  State
	seed   [ed25519.SeedSize]byte
	valid  bool
}

func NewPending(c Configuration) (Identity, error) { return newPending(c, rand.Reader) }
func newPending(c Configuration, entropy io.Reader) (Identity, error) {
	if c.Validate() != nil || entropy == nil {
		return Identity{}, ErrInvalid
	}
	v := Identity{config: c, state: Pending, valid: true}
	if _, e := io.ReadFull(entropy, v.seed[:]); e != nil {
		clear(v.seed[:])
		return Identity{}, ErrUnavailable
	}
	return v, nil
}
func (v Identity) Validate() error {
	if !v.valid || v.config.Validate() != nil || (v.state != Pending && v.state != Active) {
		return ErrInvalid
	}
	return nil
}
func (v Identity) Configuration() Configuration { return v.config }
func (v Identity) State() State                 { return v.state }
func (v Identity) PublicKey() ([ed25519.PublicKeySize]byte, error) {
	var public [ed25519.PublicKeySize]byte
	if v.Validate() != nil {
		return public, ErrInvalid
	}
	private := ed25519.NewKeyFromSeed(v.seed[:])
	defer clear(private)
	copy(public[:], private[ed25519.SeedSize:])
	return public, nil
}
func (v Identity) AsActive() (Identity, error) {
	if v.Validate() != nil {
		return Identity{}, ErrInvalid
	}
	v.state = Active
	return v, nil
}
func (v Identity) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_identity") }
func (v Identity) MarshalJSON() ([]byte, error) { return []byte(`"runner_identity"`), nil }
func (v Identity) LogValue() slog.Value         { return slog.StringValue("runner_identity") }

type fileWire struct {
	Version     int    `json:"version"`
	State       State  `json:"state"`
	CentralURL  string `json:"central_url"`
	RunnerID    p.ID   `json:"runner_id"`
	RootPath    string `json:"root_path"`
	PrivateSeed string `json:"private_seed"`
}

func encodeFile(v Identity) ([]byte, error) {
	if v.Validate() != nil {
		return nil, ErrInvalid
	}
	raw, e := json.Marshal(fileWire{1, v.state, v.config.CentralURL, v.config.RunnerID, v.config.RootPath, base64.RawURLEncoding.EncodeToString(v.seed[:])})
	if e != nil || len(raw) > MaxFileBytes {
		clear(raw)
		return nil, ErrInvalid
	}
	return raw, nil
}
