package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func loadCancellation(ctx context.Context, x postgres.SQLExecutor, r c.CancelRequest, user string, digest f.Digest) (*c.CancelReceipt, error) {
	var actualUser, actualDigest, receiptDigest string
	var raw []byte
	err := x.QueryRow(ctx, `SELECT user_id,request_digest,receipt,receipt_digest FROM agenteam_execution.cancel_commands WHERE project_id=$1 AND task_id=$2 AND execution_id=$3 AND idempotency_key=$4`, r.ProjectID.String(), r.TaskID, r.ExecutionID.String(), r.Meta.IdempotencyKey.String()).Scan(&actualUser, &actualDigest, &raw, &receiptDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if actualUser != user || actualDigest != digest.String() {
		return nil, fault(f.IdempotencyKeyReused)
	}
	if len(raw) > 16384 || c.TriggerInputDigest(raw).String() != receiptDigest {
		return nil, unavailable(nil)
	}
	var receipt c.CancelReceipt
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&receipt) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, unavailable(nil)
	}
	e := receipt.Execution
	if e.ID != r.ExecutionID || e.ProjectID != r.ProjectID || e.TaskID != r.TaskID || e.CancelRequestedAt == nil || e.Status.Terminal() || e.Version != r.ExpectedVersion+1 || receipt.Task.Validate() != nil || receipt.Task.ID != r.TaskID || receipt.Task.Version != r.ExpectedTaskVersion+1 {
		return nil, unavailable(nil)
	}
	return &receipt, nil
}
