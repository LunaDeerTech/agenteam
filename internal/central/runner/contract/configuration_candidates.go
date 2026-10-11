package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"slices"
)

// ConfigurationCandidate contains only Project-visible Runner metadata. An
// offline candidate remains a valid durable configuration reference.
type ConfigurationCandidate struct {
	ID          RunnerID  `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     f.Version `json:"version"`
	Status      Status    `json:"status"`
}

func (v ConfigurationCandidate) Validate() error {
	if v.ID.Validate() != nil || !textValid(v.Name, 1, 128) || !textValid(v.Description, 0, 4096) || v.Version.Validate() != nil || v.Status != Offline && v.Status != Online && v.Status != Incompatible {
		return invalid()
	}
	return nil
}

type ConfigurationCandidates interface {
	ConfigurationCandidateLocks([]RunnerID) ([]f.LockRequest, error)
	RequireConfigurationCandidatesInTx(context.Context, f.Tx, i.Actor, i.ProjectID, []RunnerID) ([]ConfigurationCandidate, error)
}

// ConfigurationCandidateLockRequests is pure: historical Agent receipts need
// no live candidate provider merely to acquire their original command gate.
func ConfigurationCandidateLockRequests(ids []RunnerID) ([]f.LockRequest, error) {
	if ids == nil || len(ids) > 128 {
		return nil, invalid()
	}
	global, _ := f.SystemConfigLock("runner-management")
	out := []f.LockRequest{{Key: global, Mode: f.Shared}}
	seen := map[RunnerID]bool{}
	for _, id := range ids {
		if id.Validate() != nil {
			return nil, invalid()
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		key, err := f.SystemConfigLock("runner-control-" + id.String())
		if err != nil {
			return nil, invalid()
		}
		out = append(out, f.LockRequest{Key: key, Mode: f.Shared})
	}
	slices.SortFunc(out, func(a, b f.LockRequest) int { return f.CompareLockKeys(a.Key, b.Key) })
	return out, nil
}
