package scheduler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type relaunchRuntime struct {
	project           i.ProjectID
	task              wc.TaskID
	latestDispatch    DispatchID
	latestExecution   i.ExecutionID
	cooldownExecution *i.ExecutionID
	remaining         int64
	version           f.Version
	updatedAt         f.Instant
}

const relaunchRuntimeSQL = `SELECT latest_dispatch_id::text,latest_execution_id::text,cooldown_execution_id::text,cooldown_purpose,relaunch_skip_remaining,version,updated_at FROM agenteam_scheduler.task_runtimes WHERE project_id=$1 AND task_id=$2`
const relaunchVisitSQL = `SELECT request,request_digest,outcome,remaining,created_at FROM agenteam_scheduler.relaunch_visits WHERE project_id=$1 AND task_id=$2 AND id=$3`
const relaunchHistorySQL = `SELECT ` + dispatchColumns + ` FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND task_id=$2 AND status='launched' AND convert_from(launch_request,'UTF8')::jsonb->>'purpose'='task/work' ORDER BY id COLLATE "C"`

type relaunchIntent struct {
	Request           wc.TaskRelaunchRequest `json:"request"`
	Policy            ec.Policy              `json:"policy"`
	RetryPolicyDigest f.Digest               `json:"retry_policy_digest"`
}
type relaunchReceipt struct {
	raw       []byte
	intent    relaunchIntent
	outcome   string
	remaining int64
}

func relaunchDigest(raw []byte) f.Digest {
	v := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(v[:]))
}
func encodeRelaunchIntent(request wc.TaskRelaunchRequest, policy ec.Policy, retry LaunchRetryPolicy) ([]byte, error) {
	var identity f.Digest
	if retry != (LaunchRetryPolicy{}) {
		var err error
		identity, err = retry.Identity()
		if err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(relaunchIntent{request, policy.Clone(), identity})
	if err != nil || len(raw) > 262144 {
		return nil, invalid()
	}
	return raw, nil
}
func loadRelaunchReceipt(ctx context.Context, x postgres.SQLExecutor, request wc.TaskRelaunchRequest) (*relaunchReceipt, error) {
	var r relaunchReceipt
	var digest string
	var at time.Time
	err := x.QueryRow(ctx, relaunchVisitSQL, request.ProjectID.String(), request.TaskID.String(), request.DispatchID).Scan(&r.raw, &digest, &r.outcome, &r.remaining, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	if decodeExact(r.raw, &r.intent, 262144) != nil || r.intent.Request.Validate() != nil || r.intent.Request != request || r.intent.Policy.Validate() != nil || r.intent.RetryPolicyDigest != "" && r.intent.RetryPolicyDigest.Validate() != nil || relaunchDigest(r.raw) != f.Digest(digest) || r.remaining < 0 || r.outcome != "cooldown_skipped" && r.outcome != "dispatch_created" || r.outcome == "dispatch_created" && r.remaining != 0 {
		return nil, unavailable(nil)
	}
	if _, err = f.NewInstant(at); err != nil || at.Nanosecond()%1000 != 0 {
		return nil, unavailable(nil)
	}
	return &r, ctx.Err()
}
func insertRelaunchReceipt(ctx context.Context, x postgres.SQLExecutor, call *relaunchCall, v RelaunchVisit) error {
	outcome := "dispatch_created"
	if v.CooldownSkipped {
		outcome = "cooldown_skipped"
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_scheduler.relaunch_visits(id,project_id,task_id,request,request_digest,outcome,remaining,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, call.request.DispatchID, call.request.ProjectID.String(), call.request.TaskID.String(), call.raw, string(relaunchDigest(call.raw)), outcome, v.Remaining, now)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	return ctx.Err()
}
func relaunchReceiptResult(ctx context.Context, x postgres.SQLExecutor, call *relaunchCall, r *relaunchReceipt, exact bool) (RelaunchVisit, error) {
	if r == nil {
		return RelaunchVisit{}, unavailable(nil)
	}
	// Public replay retains the original retry policy. Physical Unknown must
	// additionally match the exact original bytes, including that binding.
	digest, _ := call.launch.Digest()
	candidate := call.launch.Clone()
	candidate.Policy = r.intent.Policy.Clone()
	other, _ := candidate.Digest()
	if digest != other || exact && !bytes.Equal(r.raw, call.raw) {
		return RelaunchVisit{}, fault(f.IdempotencyKeyReused)
	}
	v := RelaunchVisit{CooldownSkipped: r.outcome == "cooldown_skipped", Remaining: r.remaining}
	if !v.CooldownSkipped {
		id, _ := f.ParseID[DispatchIdentity](call.request.DispatchID)
		row, err := loadDispatch(ctx, x, call.request.ProjectID, id)
		if err != nil {
			return RelaunchVisit{}, err
		}
		if row == nil || !validRelaunchOrigin(row) || row.relaunch.Request != call.request || row.digest != digest {
			return RelaunchVisit{}, fault(f.ConfirmationStale)
		}
		var identity f.Digest
		if row.retryPolicy != (LaunchRetryPolicy{}) {
			identity, _ = row.retryPolicy.Identity()
		}
		if identity != r.intent.RetryPolicyDigest {
			return RelaunchVisit{}, fault(f.ConfirmationStale)
		}
		v.Dispatch = snapshot(row)
	}
	return v, ctx.Err()
}
func loadRelaunchRuntime(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, t wc.TaskID) (*relaunchRuntime, error) {
	var d, e string
	var cooldown, purpose *string
	var remaining, version int64
	var at time.Time
	err := x.QueryRow(ctx, relaunchRuntimeSQL, p.String(), t.String()).Scan(&d, &e, &cooldown, &purpose, &remaining, &version, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	r := &relaunchRuntime{project: p, task: t, remaining: remaining, version: f.Version(version)}
	r.latestDispatch, err = f.ParseID[DispatchIdentity](d)
	if err != nil {
		return nil, unavailable(nil)
	}
	r.latestExecution, err = f.ParseID[i.Execution](e)
	if err != nil {
		return nil, unavailable(nil)
	}
	r.updatedAt, err = f.NewInstant(at)
	if err != nil || at.Nanosecond()%1000 != 0 || remaining < 0 || r.version.Validate() != nil {
		return nil, unavailable(nil)
	}
	if cooldown == nil {
		if purpose != nil || remaining != 0 {
			return nil, unavailable(nil)
		}
	} else {
		id, err := f.ParseID[i.Execution](*cooldown)
		if err != nil || id != r.latestExecution || purpose == nil || *purpose != "task/work" {
			return nil, unavailable(nil)
		}
		r.cooldownExecution = &id
	}
	return r, ctx.Err()
}
func writeRelaunchRuntime(ctx context.Context, x postgres.SQLExecutor, r *relaunchRuntime, previous f.Version) error {
	var cooldown, purpose any
	if r.cooldownExecution != nil {
		cooldown = r.cooldownExecution.String()
		purpose = "task/work"
	}
	var tag pgconn.CommandTag
	var err error
	if previous == 0 {
		tag, err = x.Exec(ctx, `INSERT INTO agenteam_scheduler.task_runtimes(project_id,task_id,latest_dispatch_id,latest_execution_id,cooldown_execution_id,cooldown_purpose,relaunch_skip_remaining,version,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,1,$8)`, r.project.String(), r.task.String(), r.latestDispatch.String(), r.latestExecution.String(), cooldown, purpose, r.remaining, r.updatedAt.Time())
	} else {
		tag, err = x.Exec(ctx, `UPDATE agenteam_scheduler.task_runtimes SET latest_dispatch_id=$3,latest_execution_id=$4,cooldown_execution_id=$5,cooldown_purpose=$6,relaunch_skip_remaining=$7,version=$8,updated_at=$9 WHERE project_id=$1 AND task_id=$2 AND version=$10`, r.project.String(), r.task.String(), r.latestDispatch.String(), r.latestExecution.String(), cooldown, purpose, r.remaining, int64(r.version), r.updatedAt.Time(), int64(previous))
	}
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	return ctx.Err()
}
func nextRelaunchRuntime(old *relaunchRuntime, p i.ProjectID, t wc.TaskID, latest *dispatchRecord) (*relaunchRuntime, error) {
	if !validAssociatedDispatch(latest, p) || latest.task != t.String() || latest.launch.Purpose != "task/work" {
		return nil, unavailable(nil)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	r := &relaunchRuntime{project: p, task: t, latestDispatch: latest.id, latestExecution: *latest.execution, version: 1}
	if old != nil {
		if old.version == f.Version(math.MaxInt64) {
			return nil, fault(f.InvalidState)
		}
		*r = *old
		r.version++
		if !now.After(old.updatedAt.Time()) {
			now = old.updatedAt.Time().Add(time.Microsecond)
		}
	}
	var err error
	r.updatedAt, err = f.NewInstant(now)
	return r, err
}

// This write belongs only to a new reliable pending->launched association.
// Historical launched replay never calls it, so it cannot move the head back.
func recordRelaunchAssociation(ctx context.Context, x postgres.SQLExecutor, r *dispatchRecord) error {
	if r.launch.Purpose != "task/work" {
		return nil
	}
	t, err := f.ParseID[wc.Task](r.task)
	if err != nil {
		return unavailable(nil)
	}
	old, err := loadRelaunchRuntime(ctx, x, r.project, t)
	if err != nil {
		return err
	}
	v, err := nextRelaunchRuntime(old, r.project, t, r)
	if err != nil {
		return err
	}
	v.latestDispatch, v.latestExecution, v.cooldownExecution, v.remaining = r.id, *r.execution, nil, 0
	var previous f.Version
	if old != nil {
		previous = old.version
	}
	return writeRelaunchRuntime(ctx, x, v, previous)
}

func loadRelaunchLatest(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, t wc.TaskID, runtime *relaunchRuntime) (*dispatchRecord, error) {
	if runtime != nil {
		r, err := loadDispatch(ctx, x, p, runtime.latestDispatch)
		if err != nil {
			return nil, err
		}
		if !validAssociatedDispatch(r, p) || r.task != t.String() || r.launch.Purpose != "task/work" || *r.execution != runtime.latestExecution {
			return nil, unavailable(nil)
		}
		return r, nil
	}
	rows, err := x.Query(ctx, relaunchHistorySQL, p.String(), t.String())
	if err != nil {
		return nil, portError(err)
	}
	if rows == nil {
		return nil, unavailable(nil)
	}
	return collectRelaunchHistory(ctx, rows, p, t)
}
func collectRelaunchHistory(ctx context.Context, rows dispatchRows, p i.ProjectID, t wc.TaskID) (out *dispatchRecord, err error) {
	defer func() {
		rows.Close()
		if err == nil {
			err = portError(rows.Err())
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			out = nil
		}
	}()
	versions := map[f.Version]bool{}
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		r, e := scanDispatch(rows)
		if e != nil {
			return nil, e
		}
		// All pre-58 issued origins are real todo claims. Their Work versions
		// establish order without assuming clocks or UUID allocation are monotone.
		if !validAssociatedDispatch(r, p) || r.task != t.String() || r.launch.Purpose != "task/work" || r.guard == nil || !r.guard.valid() || r.relaunch != nil || versions[r.guard.ClaimedVersion] {
			return nil, fault(f.DependencyUnbound)
		}
		versions[r.guard.ClaimedVersion] = true
		if out == nil || r.guard.ClaimedVersion > out.guard.ClaimedVersion {
			out = r
		}
	}
	return out, portError(rows.Err())
}
