package execution

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// ContextBuilder consumes the original complete captured input. It has no
// Store or live configuration providers and performs no runtime authorization.
type ContextBuilder struct{ trigger c.TriggerContextBuilder }

func NewContextBuilder(trigger c.TriggerContextBuilder) (*ContextBuilder, error) {
	if nilPort(trigger) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ContextBuilder{trigger: trigger}, nil
}

func (b *ContextBuilder) BuildContext(ctx context.Context, input c.PreparationInput) (c.ExecutionContext, error) {
	var zero c.ExecutionContext
	if ctx == nil || input.Validate() != nil {
		return zero, invalid()
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if b == nil || nilPort(b.trigger) {
		return zero, fault(f.DependencyUnbound)
	}
	fields := input.Fields()
	trigger, err := b.trigger.BuildTriggerContext(ctx, fields.Request, fields.Trigger)
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	if err != nil {
		return zero, portError(err)
	}
	if !trigger.Matches(fields.Request, fields.Trigger) {
		return zero, fault(f.InvalidArgument)
	}
	out, err := c.NewExecutionContext(input, trigger)
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	if err != nil {
		return zero, portError(err)
	}
	return out, nil
}
