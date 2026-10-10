package secret

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"slices"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// The digest is never a contract value. Only D04 consumes it synchronously for
// kind3 encryption or constant-time receipt comparison, then clears the bytes.
func projectVariableIntentDigest(intent sc.ProjectVariableIntent) ([]byte, error) {
	if intent.Validate() != nil {
		return nil, invalid()
	}
	m := intent.Fields()
	r := m.Request.Fields()
	h := sha256.New()
	_, _ = h.Write([]byte("agenteam.secret.project-variable.intent.v1\x00"))
	projectVariableDigestField(h, []byte(r.Actor.Details().UserID))
	projectVariableDigestField(h, []byte(r.ProjectID.String()))
	projectVariableDigestField(h, []byte(r.Identity.Canonical()))
	projectVariableDigestField(h, []byte(r.VariableID.String()))
	if r.ExpectedVersion == nil {
		_, _ = h.Write([]byte{0})
	} else {
		_, _ = h.Write([]byte{1})
		var encoded [8]byte
		binary.BigEndian.PutUint64(encoded[:], uint64(*r.ExpectedVersion))
		_, _ = h.Write(encoded[:])
	}
	for _, text := range []*string{m.Name, m.Description} {
		if text == nil {
			_, _ = h.Write([]byte{0})
		} else {
			_, _ = h.Write([]byte{1})
			projectVariableDigestField(h, []byte(*text))
		}
	}
	if !m.ValuePresent {
		_, _ = h.Write([]byte{0})
	} else {
		_, _ = h.Write([]byte{1})
		if err := intent.UseValue(func(value []byte) error { projectVariableDigestField(h, value); return nil }); err != nil {
			return nil, invalid()
		}
	}
	return h.Sum(nil), nil
}
func projectVariableDigestField(h hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = h.Write(length[:])
	_, _ = h.Write(value)
}

type projectVariablePreparedState struct {
	mu             sync.Mutex
	destroyed      bool
	issuer         *serviceState
	plan           sc.ProjectVariableWritePlan
	preparation    sc.ProjectVariablePreparation
	locks          []f.LockRequest
	value, receipt envelope
	version, epoch f.Version
}
type preparedProjectVariableWrite struct {
	data func() *projectVariablePreparedState
}

var _ sc.PreparedProjectVariableWrite = (*preparedProjectVariableWrite)(nil)

// The returned state is locked. The consumer must unlock it after its operation;
// Destroy therefore retires all aliases and waits for an already-entered use.
// No method of an unknown interface value is called during validation.
func (s *Service) lockProjectVariablePrepared(raw sc.PreparedProjectVariableWrite) (*projectVariablePreparedState, error) {
	p, ok := raw.(*preparedProjectVariableWrite)
	if !ok || p == nil || p.data == nil || s == nil || s.data == nil {
		return nil, invalid()
	}
	state := p.data()
	if state == nil {
		return nil, invalid()
	}
	state.mu.Lock()
	if state.destroyed || state.issuer == nil || state.issuer != s.state() {
		state.mu.Unlock()
		return nil, invalid()
	}
	return state, nil
}
func (p *preparedProjectVariableWrite) Preparation() (sc.ProjectVariablePreparation, error) {
	if p == nil || p.data == nil {
		return sc.ProjectVariablePreparation{}, invalid()
	}
	state := p.data()
	if state == nil {
		return sc.ProjectVariablePreparation{}, invalid()
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.destroyed {
		return sc.ProjectVariablePreparation{}, invalid()
	}
	if _, err := state.preparation.Fields(); err != nil {
		return sc.ProjectVariablePreparation{}, invalid()
	}
	return state.preparation, nil
}
func (p *preparedProjectVariableWrite) RequiredLocks() ([]f.LockRequest, error) {
	if p == nil || p.data == nil {
		return nil, invalid()
	}
	state := p.data()
	if state == nil {
		return nil, invalid()
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.destroyed || len(state.locks) == 0 {
		return nil, invalid()
	}
	return slices.Clone(state.locks), nil
}
func (p *preparedProjectVariableWrite) Destroy() {
	if p == nil || p.data == nil {
		return
	}
	state := p.data()
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.destroyed {
		return
	}
	state.destroyed = true
	clearProjectVariableEnvelope(&state.value)
	clearProjectVariableEnvelope(&state.receipt)
	state.plan = sc.ProjectVariableWritePlan{}
	state.preparation = sc.ProjectVariablePreparation{}
	clear(state.locks)
	state.locks = nil
	state.issuer = nil
}
func clearProjectVariableEnvelope(p *envelope) {
	clear(p.dataNonce)
	clear(p.ciphertext)
	clear(p.wrapNonce)
	clear(p.wrappedDEK)
	*p = envelope{}
}
func (*preparedProjectVariableWrite) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "prepared_project_variable_secret_write")
}
func (*preparedProjectVariableWrite) LogValue() slog.Value {
	return slog.StringValue("prepared_project_variable_secret_write")
}
func (*preparedProjectVariableWrite) MarshalJSON() ([]byte, error) {
	return []byte(`"prepared_project_variable_secret_write"`), nil
}
