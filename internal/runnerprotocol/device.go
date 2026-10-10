package runnerprotocol

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"path"
	"strings"
	"unicode/utf8"
)

const MaxDeviceBodyBytes = 32 << 10

// Device HTTP values have explicit wire encoders. Default JSON/fmt/slog cannot
// accidentally serialize enrollment material or a live nonce.
type deviceSafe struct{}

func (deviceSafe) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_device_message") }
func (deviceSafe) MarshalJSON() ([]byte, error) { return []byte(`"runner_device_message"`), nil }
func (deviceSafe) LogValue() slog.Value         { return slog.StringValue("runner_device_message") }

// EnrollmentToken and Nonce intentionally have the same canonical 32-byte
// random shape. Their different types do not prove issuer/purpose; the Central
// transaction must check the correct hashed token/nonce table and generation.
type EnrollmentToken struct {
	deviceSafe
	value Nonce
}

func NewEnrollmentToken() (EnrollmentToken, error) {
	v, err := NewNonce()
	return EnrollmentToken{value: v}, err
}
func ParseEnrollmentToken(wire string) (EnrollmentToken, error) {
	v, err := ParseNonce(wire)
	return EnrollmentToken{value: v}, err
}
func (v EnrollmentToken) Valid() bool                        { return v.value.Valid() }
func (v EnrollmentToken) Wire() (string, error)              { return v.value.Wire() }
func (v EnrollmentToken) Digest() ([sha256.Size]byte, error) { return v.value.Digest() }

type ChallengeRequest struct {
	deviceSafe
	runner ID
}

func NewChallengeRequest(runner ID) (ChallengeRequest, error) {
	if !runner.Valid() {
		return ChallengeRequest{}, ErrInvalid
	}
	return ChallengeRequest{runner: runner}, nil
}
func (v ChallengeRequest) RunnerID() ID { return v.runner }
func EncodeChallengeRequest(v ChallengeRequest) ([]byte, error) {
	if !v.runner.Valid() {
		return nil, ErrInvalid
	}
	return json.Marshal(map[string]string{"runner_id": string(v.runner)})
}
func DecodeChallengeRequest(raw []byte) (ChallengeRequest, error) {
	m, err := deviceStrings(raw, "runner_id")
	if err != nil {
		return ChallengeRequest{}, err
	}
	return NewChallengeRequest(ID(m["runner_id"]))
}
func (v *ChallengeRequest) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodeChallengeRequest(raw)
	if err == nil {
		*v = decoded
	}
	return err
}

type ChallengeResponse struct {
	deviceSafe
	nonce   Nonce
	expires Instant
}

func NewChallengeResponse(nonce Nonce, expires Instant) (ChallengeResponse, error) {
	if !nonce.Valid() || !expires.Valid() {
		return ChallengeResponse{}, ErrInvalid
	}
	return ChallengeResponse{nonce: nonce, expires: expires}, nil
}
func (v ChallengeResponse) Nonce() Nonce       { return v.nonce }
func (v ChallengeResponse) ExpiresAt() Instant { return v.expires }
func EncodeChallengeResponse(v ChallengeResponse) ([]byte, error) {
	if _, err := NewChallengeResponse(v.nonce, v.expires); err != nil {
		return nil, err
	}
	nonce, _ := v.nonce.Wire()
	return json.Marshal(map[string]string{"nonce": nonce, "expires_at": string(v.expires)})
}
func DecodeChallengeResponse(raw []byte) (ChallengeResponse, error) {
	m, err := deviceStrings(raw, "nonce expires_at")
	if err != nil {
		return ChallengeResponse{}, err
	}
	nonce, err := ParseNonce(m["nonce"])
	if err != nil {
		return ChallengeResponse{}, err
	}
	return NewChallengeResponse(nonce, Instant(m["expires_at"]))
}
func (v *ChallengeResponse) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodeChallengeResponse(raw)
	if err == nil {
		*v = decoded
	}
	return err
}

type EnrollmentRequest struct {
	deviceSafe
	runner         ID
	token          EnrollmentToken
	public         [32]byte
	root, os, arch string
}

func NewEnrollmentRequest(runner ID, token EnrollmentToken, public [32]byte, root, os, arch string) (EnrollmentRequest, error) {
	if !runner.Valid() || !token.Valid() || !validRoot(root) || os != "linux" && os != "darwin" || arch != "amd64" && arch != "arm64" {
		return EnrollmentRequest{}, ErrInvalid
	}
	return EnrollmentRequest{runner: runner, token: token, public: public, root: root, os: os, arch: arch}, nil
}
func validRoot(root string) bool {
	return len(root) > 0 && len(root) <= 4096 && utf8.ValidString(root) && strings.HasPrefix(root, "/") && path.Clean(root) == root && !strings.ContainsAny(root, "\\\x00")
}
func (v EnrollmentRequest) RunnerID() ID           { return v.runner }
func (v EnrollmentRequest) Token() EnrollmentToken { return v.token }
func (v EnrollmentRequest) PublicKey() [32]byte    { return v.public }
func (v EnrollmentRequest) RootPath() string       { return v.root }
func (v EnrollmentRequest) OS() string             { return v.os }
func (v EnrollmentRequest) Arch() string           { return v.arch }
func EncodeEnrollmentRequest(v EnrollmentRequest) ([]byte, error) {
	if _, err := NewEnrollmentRequest(v.runner, v.token, v.public, v.root, v.os, v.arch); err != nil {
		return nil, err
	}
	token, _ := v.token.Wire()
	return json.Marshal(map[string]string{"runner_id": string(v.runner), "token": token, "public_key": base64.RawURLEncoding.EncodeToString(v.public[:]), "root_path": v.root, "os": v.os, "arch": v.arch})
}
func DecodeEnrollmentRequest(raw []byte) (EnrollmentRequest, error) {
	m, err := deviceStrings(raw, "runner_id token public_key root_path os arch")
	if err != nil {
		return EnrollmentRequest{}, err
	}
	token, err := ParseEnrollmentToken(m["token"])
	if err != nil {
		return EnrollmentRequest{}, err
	}
	if len(m["public_key"]) != 43 {
		return EnrollmentRequest{}, ErrInvalid
	}
	bytes, err := base64.RawURLEncoding.DecodeString(m["public_key"])
	defer clear(bytes)
	if err != nil || len(bytes) != 32 || base64.RawURLEncoding.EncodeToString(bytes) != m["public_key"] {
		return EnrollmentRequest{}, ErrInvalid
	}
	var public [32]byte
	copy(public[:], bytes)
	return NewEnrollmentRequest(ID(m["runner_id"]), token, public, m["root_path"], m["os"], m["arch"])
}
func (v *EnrollmentRequest) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodeEnrollmentRequest(raw)
	if err == nil {
		*v = decoded
	}
	return err
}

type EnrollmentResponse struct {
	deviceSafe
	runner              ID
	version, generation Decimal
	enrolled            Instant
	fingerprint         string
}

func PublicKeyFingerprint(public [32]byte) string {
	digest := sha256.Sum256(public[:])
	return "sha256:" + hex.EncodeToString(digest[:])
}
func NewEnrollmentResponse(runner ID, version, generation Decimal, enrolled Instant, fingerprint string) (EnrollmentResponse, error) {
	if !runner.Valid() || !decimal(version, math.MaxInt64, true) || version == "1" || !decimal(generation, math.MaxInt64, true) || !enrolled.Valid() || !digest(fingerprint) {
		return EnrollmentResponse{}, ErrInvalid
	}
	return EnrollmentResponse{runner: runner, version: version, generation: generation, enrolled: enrolled, fingerprint: fingerprint}, nil
}
func (v EnrollmentResponse) RunnerID() ID                  { return v.runner }
func (v EnrollmentResponse) Version() Decimal              { return v.version }
func (v EnrollmentResponse) CredentialGeneration() Decimal { return v.generation }
func (v EnrollmentResponse) EnrolledAt() Instant           { return v.enrolled }
func (v EnrollmentResponse) PublicKeyFingerprint() string  { return v.fingerprint }
func EncodeEnrollmentResponse(v EnrollmentResponse) ([]byte, error) {
	if _, err := NewEnrollmentResponse(v.runner, v.version, v.generation, v.enrolled, v.fingerprint); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"runner_id": string(v.runner), "version": string(v.version), "credential_generation": string(v.generation), "enrolled_at": string(v.enrolled), "public_key_fingerprint": v.fingerprint})
}
func DecodeEnrollmentResponse(raw []byte) (EnrollmentResponse, error) {
	m, err := deviceStrings(raw, "runner_id version credential_generation enrolled_at public_key_fingerprint")
	if err != nil {
		return EnrollmentResponse{}, err
	}
	return NewEnrollmentResponse(ID(m["runner_id"]), Decimal(m["version"]), Decimal(m["credential_generation"]), Instant(m["enrolled_at"]), m["public_key_fingerprint"])
}
func (v *EnrollmentResponse) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodeEnrollmentResponse(raw)
	if err == nil {
		*v = decoded
	}
	return err
}

func deviceStrings(raw []byte, names string) (map[string]string, error) {
	if len(raw) > MaxDeviceBodyBytes {
		return nil, ErrTooLarge
	}
	tree, err := parseJSON(raw)
	if err != nil {
		return nil, err
	}
	m, err := fields(tree, words(names), nil)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(m))
	for key, value := range m {
		s, ok := value.(string)
		if !ok {
			return nil, ErrInvalid
		}
		out[key] = s
	}
	return out, nil
}
