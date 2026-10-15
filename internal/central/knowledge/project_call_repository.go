package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func canonicalProjectIdentity(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > 8192 {
		return nil, unavailable(nil)
	}
	var v projectCallIdentity
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&v) != nil || decoder.Decode(new(any)) != io.EOF || v.Format != 1 || v.Method == "" || len(v.Method) > 64 {
		return nil, unavailable(nil)
	}
	for _, value := range []string{v.User, v.Document, v.Agent, v.Execution, v.Operation, v.Tool, v.ToolCall, v.Attempt, v.IndexJob, v.IndexBuild} {
		if value != "" {
			if _, err := f.ParseID[struct{}](value); err != nil {
				return nil, unavailable(err)
			}
		}
	}
	if (v.IndexBuild != "") != (v.Method == "index-build") || v.IndexBuild != "" && (v.User != "" || v.Agent != "" || v.Document != "" || v.Command != "" || v.Key != "" || v.Digest != "" || v.IndexJob != "") {
		return nil, unavailable(nil)
	}
	if (v.IndexJob != "") != (v.Method == "index-job") || v.IndexJob != "" && (v.User != "" || v.Agent != "" || v.Document == "" || v.Command != "") {
		return nil, unavailable(nil)
	}
	if v.Agent != "" && (v.User != "" || v.Execution == "" || v.Operation == "" || v.Tool == "" || v.ToolCall == "" || v.Attempt == "") || v.Agent == "" && (v.Execution != "" || v.Operation != "" || v.Tool != "" || v.ToolCall != "" || v.Attempt != "") {
		return nil, unavailable(nil)
	}
	if v.Command != "" && kc.CommandName(v.Command).Validate() != nil {
		return nil, unavailable(nil)
	}
	if v.Key != "" && f.IdempotencyKey(v.Key).Validate() != nil {
		return nil, unavailable(nil)
	}
	if v.Digest != "" && v.Digest.Validate() != nil {
		return nil, unavailable(nil)
	}
	out, err := json.Marshal(v)
	if err != nil || len(out) > 4096 {
		return nil, unavailable(err)
	}
	return out, nil
}
func (r projectCallRecord) valid() bool {
	if r.ID.Validate() != nil || r.Project.Validate() != nil || r.Process.Validate() != nil || r.Instance.Validate() != nil {
		return false
	}
	if r.Source != "" {
		p, err := f.ParseID[id.Project](r.Source)
		if err != nil || p == r.Project {
			return false
		}
	}
	switch r.Lane {
	case projectCallRead, projectCallCommand, projectCallSource, projectCallRecovery:
	default:
		return false
	}
	raw, err := canonicalProjectIdentity(r.Identity)
	if err != nil || projectCallDigest(raw) != r.Digest {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, r.Registered)
	if err != nil {
		return false
	}
	if r.Phase == "running" {
		return r.Closed == ""
	}
	end, err := time.Parse(time.RFC3339Nano, r.Closed)
	return r.Phase == "business_closed" && err == nil && !end.Before(at)
}
func (r projectCallRecord) sameOwner(v projectCallRecord) bool {
	a, e := canonicalProjectIdentity(r.Identity)
	b, e2 := canonicalProjectIdentity(v.Identity)
	return e == nil && e2 == nil && bytes.Equal(a, b) && r.ID == v.ID && r.Project == v.Project && r.Source == v.Source && r.Process == v.Process && r.Instance == v.Instance && r.Lane == v.Lane && r.Digest == v.Digest && (r.Registered == "" || r.Registered == v.Registered)
}
func (r projectCallRecord) equal(v projectCallRecord) bool {
	return r.sameOwner(v) && r.Registered == v.Registered && r.Phase == v.Phase && r.Closed == v.Closed
}
func equalProjectCalls(a, b []projectCallRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for n := range a {
		if !a[n].equal(b[n]) {
			return false
		}
	}
	return true
}

const projectCallProjection = `jsonb_build_object('id',id,'project_id',project_id,'source_project_id',COALESCE(source_project_id::text,''),'process_id',process_id,'instance_id',instance_id,'lane',lane,'identity',identity,'identity_digest',identity_digest,'phase',phase,'registered_at',registered_at,'closed_at',COALESCE(to_jsonb(closed_at),'""'::jsonb))`

func decodeProjectCall(raw []byte) (projectCallRecord, error) {
	var r projectCallRecord
	if json.Unmarshal(raw, &r) != nil || !r.valid() {
		return r, unavailable(nil)
	}
	return r, nil
}
func loadProjectCall(ctx context.Context, x postgres.SQLExecutor, key f.ID[projectCallID]) (*projectCallRecord, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT `+projectCallProjection+` FROM agenteam_knowledge.project_calls WHERE id=$1`, key.String()).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	r, err := decodeProjectCall(raw)
	if err != nil || r.ID != key {
		return nil, unavailable(err)
	}
	return &r, ctx.Err()
}
func listProjectCalls(ctx context.Context, x postgres.SQLExecutor, p id.ProjectID) ([]projectCallRecord, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(`+projectCallProjection+` ORDER BY id),'[]'::jsonb) FROM agenteam_knowledge.project_calls WHERE project_id=$1 OR source_project_id=$1`, p.String()).Scan(&raw)
	if err != nil {
		return nil, portError(err)
	}
	var out []projectCallRecord
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return nil, unavailable(nil)
	}
	for n, r := range out {
		if !r.valid() || !r.touches(p) || n > 0 && out[n-1].ID.String() >= r.ID.String() {
			return nil, unavailable(nil)
		}
	}
	return out, ctx.Err()
}
func nullableProjectSource(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func closeProjectCall(ctx context.Context, x postgres.SQLExecutor, r projectCallRecord) error {
	tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.project_calls SET phase='business_closed',closed_at=GREATEST(clock_timestamp(),registered_at) WHERE id=$1 AND project_id=$2 AND source_project_id IS NOT DISTINCT FROM $3::uuid AND process_id=$4 AND instance_id=$5 AND lane=$6 AND identity=$7::jsonb AND identity_digest=$8 AND registered_at=$9::timestamptz AND phase='running'`, r.ID.String(), r.Project.String(), nullableProjectSource(r.Source), r.Process.String(), r.Instance.String(), string(r.Lane), string(r.Identity), string(r.Digest), r.Registered)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	return ctx.Err()
}
func projectCallLocks(r projectCallRecord, mode f.LockMode) []f.LockRequest {
	var locks []f.LockRequest
	for _, p := range r.projects() {
		locks = append(locks, knowledgeProjectLock(p, mode))
	}
	out, _ := ob.NormalizeLocks(locks)
	return out
}
func (s *projectCallState) register(ctx context.Context, o *projectCallOwner) (f.CommitResult, bool) {
	r := o.record
	cause, _ := f.NewRecoveryCause("knowledge-call-register", r.ID.String(), r.Project.String())
	valid := false
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locks := projectCallLocks(r, f.Shared)
		if o.actor.Validate() == nil {
			for _, p := range r.projects() {
				more, err := accessLocks(ctx, o.actor, p, false)
				if err != nil {
					return err
				}
				locks = append(locks, more...)
			}
		}
		locks, err := ob.NormalizeLocks(locks)
		if err != nil {
			return portError(err)
		}
		if err = s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err = s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
			return portError(err)
		}
		gate, ok := s.stop.(kc.ProjectCallAdmissionAuthority)
		if !ok || nilPort(gate) {
			return fault(f.DependencyUnbound)
		}
		for _, p := range r.projects() {
			if err = gate.RequireProjectCallAdmissionInTx(ctx, tx, p); err != nil {
				return portError(err)
			}
			if o.actor.Validate() == nil {
				if err := s.service.authorizeReadInTx(ctx, tx, o.actor, p); err != nil {
					return err
				}
			}
		}
		x, err := s.store.InTx(tx)
		if err != nil || nilPort(x) {
			return unavailable(err)
		}
		var raw []byte
		err = x.QueryRow(ctx, `INSERT INTO agenteam_knowledge.project_calls(id,project_id,source_project_id,process_id,instance_id,lane,identity,identity_digest,phase,registered_at) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,'running',clock_timestamp()) RETURNING `+projectCallProjection, r.ID.String(), r.Project.String(), nullableProjectSource(r.Source), r.Process.String(), r.Instance.String(), string(r.Lane), string(r.Identity), string(r.Digest)).Scan(&raw)
		if err != nil {
			return portError(err)
		}
		actual, err := decodeProjectCall(raw)
		if err != nil || !r.sameOwner(actual) {
			return unavailable(err)
		}
		s.mu.Lock()
		o.record = actual
		s.signalLocked()
		s.mu.Unlock()
		valid = true
		return ctx.Err()
	})
	return result, valid
}
func (s *projectCallState) callTx(ctx context.Context, cause f.TransactionCause, r projectCallRecord, stop *projectStopCall, fn func(context.Context, postgres.SQLExecutor) error) f.CommitResult {
	return s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, projectCallLocks(r, f.Exclusive)); err != nil {
			return portError(err)
		}
		if err := s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
			return portError(err)
		}
		if stop != nil {
			if err := s.stop.RequireProjectStopInTx(ctx, tx, stop.actor, stop.cause, stop.scope); err != nil {
				return portError(err)
			}
		}
		x, err := s.store.InTx(tx)
		if err != nil || nilPort(x) {
			return unavailable(err)
		}
		return fn(ctx, x)
	})
}

// An uncertain registration/close is only observed here. Even a successful
// observation needs a later explicit call before another write may occur.
func (s *projectCallState) settle(ctx context.Context, o *projectCallOwner, stop *projectStopCall, readOnly bool) (bool, error) {
	s.mu.Lock()
	if s.owners[o.record.ID] != o {
		s.mu.Unlock()
		return false, nil
	}
	if !o.bodyReturned || o.settling || o.references != 0 {
		s.mu.Unlock()
		return false, fault(f.ResourceBusy)
	}
	if (o.absent || o.closed) && o.unknown == nil {
		s.mu.Unlock()
		return false, nil
	}
	if readOnly && o.unknown == nil {
		s.mu.Unlock()
		return true, nil
	}
	o.settling = true
	r := o.record
	original := o.unknown
	registration := o.unknownRegistration
	s.signalLocked()
	s.mu.Unlock()
	defer func() { s.mu.Lock(); o.settling = false; s.signalLocked(); s.mu.Unlock() }()
	valid, absent, closed := false, false, false
	var observed *projectCallRecord
	cause, _ := f.NewRecoveryCause("knowledge-call-return", r.ID.String(), r.Project.String())
	if original != nil {
		cause = original.Cause()
	}
	result := s.callTx(ctx, cause, r, stop, func(ctx context.Context, x postgres.SQLExecutor) error {
		current, err := loadProjectCall(ctx, x, r.ID)
		if err != nil {
			return err
		}
		observed = current
		if current == nil {
			if original == nil || !registration {
				return fault(f.InvalidState)
			}
			absent = true
		} else {
			if !r.sameOwner(*current) {
				return fault(f.InvalidState)
			}
			closed = current.Phase == "business_closed"
			if original == nil && !closed {
				if err = closeProjectCall(ctx, x, *current); err != nil {
					return err
				}
				closed = true
			}
		}
		valid = true
		return ctx.Err()
	})
	if original != nil {
		if result.State() != f.Committed || !valid || ctx.Err() != nil {
			return true, txError(*original)
		}
		s.mu.Lock()
		o.unknown = nil
		o.registrationKnown = !absent
		o.absent, o.closed = absent, closed
		if observed != nil {
			o.record = *observed
		}
		s.signalLocked()
		s.mu.Unlock()
		return true, nil
	}
	if result.State() == f.Unknown {
		s.mu.Lock()
		saved := result
		o.unknown = &saved
		o.unknownRegistration = false
		s.signalLocked()
		s.mu.Unlock()
		return false, txError(result)
	}
	if err := txError(result); err != nil {
		return false, err
	}
	if !valid || ctx.Err() != nil {
		return false, unavailable(ctx.Err())
	}
	s.mu.Lock()
	o.closed = true
	s.signalLocked()
	s.mu.Unlock()
	return false, nil
}
