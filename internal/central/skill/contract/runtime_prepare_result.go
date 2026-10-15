package contract

import (
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// PrepareUnknownResult is a returned Runner result, not a missing response or
// a Commit Unknown receipt. It deliberately contains no usable package path.
// The owning adapter must still join the original handler and Transfer lease
// before publishing this result through the existing Tool Attempt.
type PrepareUnknownResult struct {
	SkillID         SkillID    `json:"skill_id"`
	RevisionID      RevisionID `json:"revision_id"`
	Revision        f.Revision `json:"revision"`
	MountID         id.MountID `json:"mount_id"`
	Outcome         string     `json:"outcome"`
	MayHavePrepared bool       `json:"may_have_prepared"`
	Error           struct {
		Code f.Code `json:"code"`
	} `json:"error"`
}

func (r PrepareUnknownResult) Validate() error {
	if r.SkillID.Validate() != nil || r.RevisionID.Validate() != nil || r.Revision.Validate() != nil || r.MountID.Validate() != nil || r.Outcome != "unknown" || !r.MayHavePrepared || r.Error.Code != f.DependencyUnavailable {
		return invalid()
	}
	return nil
}

func EncodePrepareUnknownResult(r PrepareUnknownResult) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(r)
}

func DecodePrepareUnknownResult(raw []byte) (PrepareUnknownResult, error) {
	var r PrepareUnknownResult
	if strictObject(raw, 8192, []string{"skill_id", "revision_id", "revision", "mount_id", "outcome", "may_have_prepared", "error"}) != nil || json.Unmarshal(raw, &r) != nil || r.Validate() != nil {
		return PrepareUnknownResult{}, invalid()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || strictObject(fields["error"], 128, []string{"code"}) != nil {
		return PrepareUnknownResult{}, invalid()
	}
	return r, nil
}
