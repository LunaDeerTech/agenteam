package model

import (
	"context"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

// The original call lock serializes rotation. Usage reserves this exact new
// invocation in the same transaction; the old final attempt remains immutable.
func insertRuntimeRetry(ctx context.Context, x postgres.SQLExecutor, old, next *runtimeRecord) error {
	if old.validate() != nil || next.validate() != nil || old.binding.Format != 2 || old.phase != "retry_wait" || old.retired || old.value.Final == nil || old.version == math.MaxInt64 || old.value.AttemptIndex == math.MaxInt64 || next.version != old.version+1 || next.digest != old.digest || next.phase != "accepted" || next.sequence != 1 || next.value.Dispatch != uc.Reserved || next.value.Final != nil || next.value.AttemptIndex != old.value.AttemptIndex+1 || next.value.ProcessID != old.value.ProcessID || next.value.Fence != old.value.Fence || next.value.StartedAt.Time().Before(old.value.Final.FinishedAt.Time()) {
		return fault(f.InvalidState)
	}
	attempt, err := encoded(runtimeAttemptDTO{1, next.sequence, next.value})
	if err != nil {
		return err
	}
	if len(attempt) > 32<<10 {
		return fault(f.PayloadTooLarge)
	}
	v := next.value
	_, err = x.Exec(ctx, `INSERT INTO agenteam_model.runtime_attempts(id,call_id,project_id,ordinal,process_id,fence,dispatch,sequence,fact_data) VALUES($1,$2,$3,$4,$5,$6,'reserved',1,$7)`, v.ID.String(), v.CallID.String(), v.Consumer.ProjectID.String(), int64(v.AttemptIndex), v.ProcessID.String(), int64(v.Fence), attempt)
	if err != nil {
		return unavailable(err)
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_model.calls SET invocation_id=$1,phase='accepted',finished_at=NULL,version=$2 WHERE id=$3 AND invocation_id=$4 AND request_binding=$5 AND version=$6 AND phase='retry_wait' AND NOT retired`, v.ID.String(), int64(next.version), v.CallID.String(), old.value.ID.String(), old.digest.String(), int64(old.version))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return nil
}
