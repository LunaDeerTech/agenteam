package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// InitialRoundBindingsRequest identifies the immutable initial capture which
// the Execution owner loaded from its original preparation input. It is not
// permission to start an Execution, invoke Model, or access Skill content.
type InitialRoundBindingsRequest struct {
	Initial               InitialSkillBindings
	CaptureAttemptBinding f.Digest
}

func (r InitialRoundBindingsRequest) Validate() error {
	if r.Initial.Validate() != nil || r.CaptureAttemptBinding.Validate() != nil {
		return invalid()
	}
	return nil
}

func (r InitialRoundBindingsRequest) Clone() InitialRoundBindingsRequest {
	r.Initial = r.Initial.Clone()
	return r
}

// A plan only freezes Skill-owned facts and the complete lock set. The final
// read must use the same provider and original request in the caller's live Tx.
type InitialRoundBindingsPlan interface{ RequiredLocks() []f.LockRequest }

// InitialRoundBindingsReader is a trusted, read-only fact port. Discovery
// reads the real capture head even for an empty directory; the final read
// checks the stored canonical capture, current initialized assignments and
// fixed revision/Object protection under the complete preheld lock union.
// Neither method writes a binding, begins a Model call, or supplies Execution
// authority. The caller must enforce current Project/Execution authority and
// commit the returned binding with RoundInput before any Model dispatch.
// Assignment changes are rejected by this initial-only implementation; it
// does not substitute an empty directory or implement dynamic Skill ingress.
type InitialRoundBindingsReader interface {
	DiscoverInitialRoundBindings(context.Context, InitialRoundBindingsRequest) (InitialRoundBindingsPlan, error)
	ReadInitialRoundBindingsInTx(context.Context, f.Tx, InitialRoundBindingsRequest, InitialRoundBindingsPlan) (InitialSkillBindings, error)
}

func (InitialRoundBindingsRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_initial_round_bindings_request")
}
func (InitialRoundBindingsRequest) LogValue() slog.Value {
	return slog.StringValue("skill_initial_round_bindings_request")
}
func (InitialRoundBindingsRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_initial_round_bindings_request"`), nil
}
func (*InitialRoundBindingsRequest) UnmarshalJSON([]byte) error { return invalid() }
