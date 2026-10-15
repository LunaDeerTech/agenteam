package builtin

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

type SkillInstallLookupService interface {
	LookupInstallExecution(context.Context, id.Actor, f.CommandMeta, id.ProjectID, skill.InstallRequest) (skill.InstallReceipt, error)
}

type SkillInstallLookupOperationAuthority interface {
	RequireCurrentSkillInstallLookup(context.Context, SkillInstallCall, f.ID[f.Request]) error
}

// Lookup borrows the original immutable call material. It cannot reconstruct
// a package, call Install, allocate an identity, or infer rollback from absence.
func (a *SkillInstallAdapter) Lookup(ctx context.Context, call SkillInstallCall, requestID f.ID[f.Request]) (SkillInstallResult, error) {
	if err := skillInstallContext(ctx); err != nil {
		return SkillInstallResult{}, err
	}
	if a == nil || a.data == nil {
		return SkillInstallResult{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if call.Validate() != nil || requestID.Validate() != nil {
		return SkillInstallResult{}, skillInstallInvalid()
	}
	s := a.data()
	service, ok := s.service.(SkillInstallLookupService)
	authority, bound := s.operation.(SkillInstallLookupOperationAuthority)
	if !ok || !bound || skillInstallNil(service) || skillInstallNil(authority) {
		return SkillInstallResult{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if err := authority.RequireCurrentSkillInstallLookup(ctx, call, requestID); err != nil {
		return SkillInstallResult{}, skillInstallPortError(err, f.NotStarted)
	}
	if err := ctx.Err(); err != nil {
		return SkillInstallResult{}, err
	}
	d := call.data()
	meta := f.CommandMeta{RequestID: requestID, IdempotencyKey: d.details.Key}
	receipt, err := service.LookupInstallExecution(ctx, d.actor, meta, d.details.ProjectID, d.request)
	if err != nil {
		return SkillInstallResult{}, skillInstallPortError(err, f.Unknown)
	}
	output, err := ProjectSkillInstallReceipt(context.WithoutCancel(ctx), call, receipt)
	if err != nil {
		return SkillInstallResult{}, err
	}
	return SkillInstallResult{receipt: receipt, output: output}, nil
}
