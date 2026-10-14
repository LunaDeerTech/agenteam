package execution

import (
	"context"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func projectTaskWaiting(out *c.TaskExecution, reason, reference *string, at *time.Time) error {
	if out.Status != c.Waiting {
		if reason != nil || reference != nil || at != nil {
			return unavailable(nil)
		}
		return nil
	}
	if reason == nil || *reason != "approval" || reference == nil || at == nil || out.StartedAt == nil {
		return unavailable(nil)
	}
	if _, err := f.ParseID[struct{}](*reference); err != nil {
		return unavailable(nil)
	}
	instant, err := f.NewInstant(*at)
	if err != nil || instant.Time().Before(out.StartedAt.Time()) {
		return unavailable(nil)
	}
	out.WaitingReason, out.WaitingReference, out.WaitingAt = reason, reference, &instant
	return nil
}

func loadWaitingReference(ctx context.Context, x postgres.SQLExecutor, id i.ExecutionID) (*string, error) {
	var ref *string
	if err := x.QueryRow(ctx, `SELECT waiting_reference::text FROM agenteam_execution.executions WHERE id=$1`, id.String()).Scan(&ref); err != nil {
		return nil, unavailable(err)
	}
	return ref, nil
}

func loadTaskWaiting(ctx context.Context, x postgres.SQLExecutor, out *c.TaskExecution) error {
	if out.Status != c.Waiting {
		return nil
	}
	var reason, reference *string
	var at *time.Time
	if err := x.QueryRow(ctx, `SELECT waiting_reason,waiting_reference::text,waiting_at FROM agenteam_execution.executions WHERE id=$1`, out.ID.String()).Scan(&reason, &reference, &at); err != nil {
		return unavailable(err)
	}
	return projectTaskWaiting(out, reason, reference, at)
}
