package app

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type modelManagementProcessAuthority struct{ guard *object.ProcessGuard }

func (a modelManagementProcessAuthority) CurrentManagementProcess() (string, error) {
	v, e := a.guard.CurrentProcess()
	return v.String(), e
}
func (a modelManagementProcessAuthority) ConfirmManagementProcessStopped(ctx context.Context, raw string) error {
	v, e := f.ParseID[objc.Process](raw)
	if e != nil {
		return e
	}
	return a.guard.ConfirmStopped(ctx, v)
}
