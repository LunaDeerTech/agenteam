package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type RejectedResultPlan interface {
	RequiredLocks() []f.LockRequest
	Waiting() Waiting
	Binding() tc.ToolCallBinding
}
type RejectedResults interface {
	DiscoverRejectedResult(context.Context, tc.ToolCallBinding, string) (RejectedResultPlan, error)
	WithRejectedResultInTx(context.Context, f.Tx, RejectedResultPlan, func(context.Context, Waiting, mc.ToolResultPart) error) error
}
