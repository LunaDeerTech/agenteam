package cursor

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const MaxTokenBytes = 8 << 10
const AuditOrder = "created_at:desc,id:desc"

type Scalar struct{ kind, value string }

func Instant(value foundation.Instant) (Scalar, error) {
	if value.Validate() != nil {
		return Scalar{}, invalidCursor()
	}
	return Scalar{"instant", value.String()}, nil
}
func UUID(value string) (Scalar, error) {
	if _, err := foundation.ParseID[struct{}](value); err != nil {
		return Scalar{}, invalidCursor()
	}
	return Scalar{"uuid", value}, nil
}
func Integer(value int64) Scalar { return Scalar{"integer", strconv.FormatInt(value, 10)} }
func (s Scalar) Kind() string    { return s.kind }
func (s Scalar) Value() string   { return s.value }

type Binding struct {
	Scope       identity.Scope
	QueryDigest foundation.Digest
	Order       string
}
type Position struct {
	Scalars         []Scalar
	OrderGeneration *int64
}
type header struct {
	Version int    `json:"signature_version"`
	Kid     string `json:"kid"`
}
type scopeWire struct {
	Kind      string `json:"kind"`
	ProjectID string `json:"project_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
}
type scalarWire struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}
type payload struct {
	Format      int          `json:"format"`
	Scope       scopeWire    `json:"scope"`
	QueryDigest string       `json:"query_digest"`
	Order       string       `json:"order"`
	Position    []scalarWire `json:"position"`
	Generation  *string      `json:"order_generation,omitempty"`
}

func invalidCursor() error {
	return foundation.NewFault(foundation.CursorInvalid, foundation.NotStarted)
}
func Digest(raw []byte) (foundation.Digest, error) {
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return foundation.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}
func wireBinding(b Binding) (scopeWire, error) {
	if b.Scope.Validate() != nil || b.QueryDigest.Validate() != nil || b.Order == "" || len(b.Order) > 128 {
		return scopeWire{}, invalidCursor()
	}
	s := b.Scope.Details()
	return scopeWire{string(s.Kind), s.ProjectID, s.AgentID}, nil
}
func scalarValid(s Scalar) bool {
	switch s.kind {
	case "instant":
		v, e := foundation.ParseInstant(s.value)
		return e == nil && v.String() == s.value
	case "uuid":
		_, e := foundation.ParseID[struct{}](s.value)
		return e == nil
	case "integer":
		v, e := strconv.ParseInt(s.value, 10, 64)
		return e == nil && strconv.FormatInt(v, 10) == s.value
	}
	return false
}
func (k Keyring) Sign(b Binding, p Position) (string, error) {
	scope, err := wireBinding(b)
	if err != nil || k.Validate() != nil || len(p.Scalars) == 0 || len(p.Scalars) > 8 {
		return "", invalidCursor()
	}
	w := payload{Format: 1, Scope: scope, QueryDigest: string(b.QueryDigest), Order: b.Order, Position: []scalarWire{}}
	for _, s := range p.Scalars {
		if !scalarValid(s) {
			return "", invalidCursor()
		}
		w.Position = append(w.Position, scalarWire{s.kind, s.value})
	}
	if p.OrderGeneration != nil {
		if *p.OrderGeneration < 1 {
			return "", invalidCursor()
		}
		generation := strconv.FormatInt(*p.OrderGeneration, 10)
		w.Generation = &generation
	}
	h, _ := json.Marshal(header{1, k.data().current})
	h, _ = CanonicalJSON(h)
	body, _ := json.Marshal(w)
	body, _ = CanonicalJSON(body)
	hp := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(body)
	key := k.data().values[k.data().current]
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte("agenteam.cursor.v1\x00"))
	mac.Write([]byte(hp))
	token := hp + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(token) > MaxTokenBytes {
		return "", invalidCursor()
	}
	return token, nil
}
func (k Keyring) Verify(token string, b Binding) (Position, error) {
	scope, err := wireBinding(b)
	if err != nil || k.Validate() != nil || len(token) == 0 || len(token) > MaxTokenBytes {
		return Position{}, invalidCursor()
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Position{}, invalidCursor()
	}
	decoded := make([][]byte, 3)
	for i, p := range parts {
		v, e := base64.RawURLEncoding.Strict().DecodeString(p)
		if e != nil || base64.RawURLEncoding.EncodeToString(v) != p {
			return Position{}, invalidCursor()
		}
		decoded[i] = v
	}
	var h header
	canonical, e := strict(decoded[0], &h)
	if e != nil || !bytes.Equal(canonical, decoded[0]) || h.Version != 1 || !validKid(h.Kid) {
		return Position{}, invalidCursor()
	}
	key, ok := k.data().values[h.Kid]
	if !ok {
		return Position{}, invalidCursor()
	}
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte("agenteam.cursor.v1\x00"))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(mac.Sum(nil), decoded[2]) {
		return Position{}, invalidCursor()
	}
	var p payload
	canonical, e = strict(decoded[1], &p)
	if e != nil || !bytes.Equal(canonical, decoded[1]) || p.Format != 1 || p.Scope != scope || p.QueryDigest != string(b.QueryDigest) || p.Order != b.Order || len(p.Position) == 0 || len(p.Position) > 8 {
		return Position{}, invalidCursor()
	}
	out := Position{Scalars: []Scalar{}}
	for _, s := range p.Position {
		v := Scalar{s.Type, s.Value}
		if !scalarValid(v) {
			return Position{}, invalidCursor()
		}
		out.Scalars = append(out.Scalars, v)
	}
	if p.Generation != nil {
		parsed, err := foundation.ParseVersion(*p.Generation)
		if err != nil {
			return Position{}, invalidCursor()
		}
		g := int64(parsed)
		out.OrderGeneration = &g
	}
	return out, nil
}
