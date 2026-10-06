package model

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type ModelReplacementRequirement string
type ModelDeletionBlocker string

type ModelReferenceCount struct {
	OwnerKind string     `json:"owner_kind"`
	Role      string     `json:"role"`
	Count     f.Progress `json:"count"`
}

// ModelDeletionImpact is a bounded observation of registered reference edges.
// It grants no deletion capability; DeleteModel must rediscover current facts.
type ModelDeletionImpact struct {
	ModelID                mc.ModelID                  `json:"model_id"`
	Version                f.Version                   `json:"version"`
	ReferenceCount         f.Progress                  `json:"reference_count"`
	ReferenceGroups        []ModelReferenceCount       `json:"reference_groups"`
	ReplacementRequirement ModelReplacementRequirement `json:"replacement_requirement"`
	DeleteBlocker          *ModelDeletionBlocker       `json:"delete_blocker"`
}

const managementReadBudget = 3 * time.Second
const managementReferenceLimit = 10000

// The order is also the wire order. A fixed array bounds aggregation memory and
// closes the accepted kind/role matrix without loading foreign owner identities.
var managementReferenceKinds = [...]struct {
	kind, role string
	required   bool
	unbound    bool
}{
	{"agent", "agent_model", true, true},
	{"agent", "approval_model", true, true},
	{"platform_selector", "embedding", true, false},
	{"platform_selector", "image", false, false},
	{"platform_selector", "memory", true, false},
	{"platform_selector", "reranker", false, false},
	{"project_summary", "meeting_summary", true, true},
}

func managementReadError(err error) error {
	var ff *f.Fault
	if errors.As(err, &ff) {
		return err
	}
	return unavailable(err)
}

func (s *Service) GetModelDeletionImpact(ctx context.Context, actor id.Actor, model mc.ModelID) (ModelDeletionImpact, error) {
	var zero ModelDeletionImpact
	if ctx == nil || model.Validate() != nil {
		return zero, fault(f.InvalidArgument)
	}
	state := s.state()
	if state == nil || nilPort(state.store) || state.authority.state() == nil {
		return zero, fault(f.DependencyUnbound)
	}
	authority := state.authority.state()
	if nilPort(authority.store) || !sameStore(state.store, authority.store) || nilPort(authority.auth.Sessions) || nilPort(authority.auth.System) {
		return zero, fault(f.DependencyUnbound)
	}
	if err := human(actor); err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, managementReadBudget)
	defer cancel()
	cause, err := readCause("management-impact")
	if err != nil {
		return zero, err
	}
	locks := []f.LockRequest{
		userLock(actor.Details().UserID),
		aggregateLock(f.ModelConfigAggregate, model.String(), f.Shared),
		systemLock("model-platform-selection", f.Shared),
		// Ordinary reference writers hold Shared. Exclusive freezes their union
		// with the canonical selector; it does not grant a business mutation.
		systemLock("model-references", f.Exclusive),
	}
	var out ModelDeletionImpact
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := state.store.AcquireAll(ctx, tx, locks); err != nil {
			return managementReadError(err)
		}
		x, err := state.store.InTx(tx)
		if err != nil {
			return managementReadError(err)
		}
		if nilPort(x) {
			return unavailable(nil)
		}
		if err = state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return managementReadError(err)
		}
		if err = state.authority.currentScope(ctx, tx, actor, id.SystemScope(), id.Read); err != nil {
			return err
		}
		var version f.Version
		err = x.QueryRow(ctx, `SELECT m.version FROM agenteam_model.models m JOIN agenteam_model.providers p ON p.id=m.provider_id WHERE p.scope='system' AND m.id=$1`, model.String()).Scan(&version)
		if errors.Is(err, pgx.ErrNoRows) {
			return fault(f.NotFound)
		}
		if err != nil {
			return managementReadError(err)
		}
		if version.Validate() != nil {
			return unavailable(nil)
		}
		if err = managementSelectionConsistent(ctx, x); err != nil {
			return err
		}
		out, err = managementReferenceImpact(ctx, x, model, version)
		return err
	})
	if err = commitError(result); err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, f.NewFault(f.DependencyUnavailable, f.Committed).WithCause(err)
	}
	return out, nil
}

func managementSelectionConsistent(ctx context.Context, x postgres.SQLExecutor) error {
	selection, err := loadSelection(ctx, x)
	if err != nil {
		var ff *f.Fault
		if errors.As(err, &ff) && ff.Code == f.InvalidState {
			return unavailable(err)
		}
		return err
	}
	if _, err = f.ParseID[struct{}](selection.ID); err != nil {
		return unavailable(err)
	}
	if _, err = selectionView(selection); err != nil {
		return err
	}
	// Query all platform owners, not just the singleton ID: an extra owner is
	// corruption too. Five is the sentinel beyond the four canonical roles.
	var raw []byte
	err = x.QueryRow(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_object('Kind',owner_kind,'Owner',owner_id::text,'Role',role,'Project',coalesce(project_id::text,''),'Model',model_id::text,'Version',owner_version::text,'Effort',reasoning_effort) ORDER BY owner_id,role),'[]'::jsonb) FROM (SELECT owner_kind,owner_id,role,project_id,model_id,owner_version,reasoning_effort FROM agenteam_model.references WHERE owner_kind='platform_selector' ORDER BY owner_id,role LIMIT 5) platform`).Scan(&raw)
	if err != nil {
		return managementReadError(err)
	}
	var actual []referenceRecord
	if err = json.Unmarshal(raw, &actual); err != nil || actual == nil || len(actual) > 4 {
		return unavailable(err)
	}
	if !sameValue(actual, selectionReferences(selection)) {
		return unavailable(nil)
	}
	return nil
}

func managementReferenceImpact(ctx context.Context, x postgres.SQLExecutor, model mc.ModelID, version f.Version) (ModelDeletionImpact, error) {
	var counts [len(managementReferenceKinds)]f.Progress
	var total f.Progress
	// The materialized input is bounded before any aggregate. The eighth
	// count includes unknown combinations so they cannot silently disappear.
	err := x.QueryRow(ctx, `WITH bounded AS MATERIALIZED (SELECT owner_kind,role FROM agenteam_model.references WHERE model_id=$1 LIMIT 10001)
SELECT count(*),
count(*) FILTER (WHERE owner_kind='agent' AND role='agent_model'),
count(*) FILTER (WHERE owner_kind='agent' AND role='approval_model'),
count(*) FILTER (WHERE owner_kind='platform_selector' AND role='embedding'),
count(*) FILTER (WHERE owner_kind='platform_selector' AND role='image'),
count(*) FILTER (WHERE owner_kind='platform_selector' AND role='memory'),
count(*) FILTER (WHERE owner_kind='platform_selector' AND role='reranker'),
count(*) FILTER (WHERE owner_kind='project_summary' AND role='meeting_summary') FROM bounded`, model.String()).Scan(&total, &counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &counts[6])
	if err != nil {
		return ModelDeletionImpact{}, managementReadError(err)
	}
	if total.Validate() != nil {
		return ModelDeletionImpact{}, unavailable(nil)
	}
	if total > managementReferenceLimit {
		return ModelDeletionImpact{}, fault(f.ResourceBusy)
	}
	var sum f.Progress
	for _, count := range counts {
		if count.Validate() != nil || count > total || sum > total-count {
			return ModelDeletionImpact{}, unavailable(nil)
		}
		sum += count
	}
	if sum != total {
		return ModelDeletionImpact{}, unavailable(nil)
	}
	return managementImpact(model, version, counts)
}

func managementImpact(model mc.ModelID, version f.Version, counts [len(managementReferenceKinds)]f.Progress) (ModelDeletionImpact, error) {
	out := ModelDeletionImpact{ModelID: model, Version: version, ReferenceGroups: []ModelReferenceCount{}, ReplacementRequirement: "none"}
	if model.Validate() != nil || version.Validate() != nil {
		return ModelDeletionImpact{}, unavailable(nil)
	}
	for i, count := range counts {
		if count.Validate() != nil || count > managementReferenceLimit || out.ReferenceCount > managementReferenceLimit-count {
			return ModelDeletionImpact{}, unavailable(nil)
		}
		if count == 0 {
			continue
		}
		group := managementReferenceKinds[i]
		out.ReferenceCount += count
		out.ReferenceGroups = append(out.ReferenceGroups, ModelReferenceCount{group.kind, group.role, count})
		if group.required {
			out.ReplacementRequirement = "required"
		} else if out.ReplacementRequirement == "none" {
			out.ReplacementRequirement = "optional"
		}
		if group.unbound && out.DeleteBlocker == nil {
			blocker := ModelDeletionBlocker("reference_adapter_unbound")
			out.DeleteBlocker = &blocker
		}
	}
	return out, nil
}
