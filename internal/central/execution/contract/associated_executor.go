package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type ExecutionAdvancePhase string

const (
	ExecutionAccepted  ExecutionAdvancePhase = "accepted"
	ExecutionPreparing ExecutionAdvancePhase = "preparing"
	ExecutionStarting  ExecutionAdvancePhase = "starting"
	ExecutionRetained  ExecutionAdvancePhase = "retained"
	ExecutionDeferred  ExecutionAdvancePhase = "deferred"
	ExecutionTerminal  ExecutionAdvancePhase = "terminal"
)

// ExecutionAdvance is a local ownership observation, not a launch, Model or
// terminal permit. Status, when present, is a canonical Execution observation;
// Joined only describes this executor's original calls, never Task completion.
// The method's separate error preserves an original Unknown and its physical
// cause. No input, response, credential, idempotency key or digest is projected.
type ExecutionAdvance struct {
	ProjectID   i.ProjectID
	ExecutionID i.ExecutionID
	Phase       ExecutionAdvancePhase
	Status      Status
	Active      bool
	Retained    bool
	Joined      bool
}

func (ExecutionAdvance) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_advance")
}
func (ExecutionAdvance) LogValue() slog.Value { return slog.StringValue("execution_advance") }

// AssociatedExecutor consumes a trusted Scheduler caller's already committed
// association. The caller checks its own authority and canonical association;
// the executor independently verifies the full original Launch tuple before
// any preparation or Model work. This DTO is not a public authorization grant.
//
// Advance does not wait for a database callback or Model response. Its context
// controls admission only; owned work uses the executor's separate Run lifetime.
// Before Run, admission is rejected. A repeated exact tuple observes the same
// owner. A retained call may advance once after the explicit recovery interval,
// through the SAME driver and original ResolveUnknown, never a new Launch,
// preparation claim, Model call or driver. There is no background retry loop.
//
// Fresh capture is limited to created Execution. A preparing Execution without
// a complete immutable input and without this executor's retained owner is
// deferred, not recaptured. Terminal history is read-only. Known failures that
// lack a durable exclusion remain within the explicit ownership capacity so
// repeated visits cannot silently retry them. Recovery is process-local; this
// interface does not implement restart takeover of a running Execution.
type AssociatedExecutor interface {
	Advance(context.Context, i.ProjectID, AssociatedDispatch) (ExecutionAdvance, error)
}
