package usage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Store interface {
	postgres.SQLExecutor
	InTx(f.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	AcquireAll(context.Context, f.Tx, []f.LockRequest) error
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}

func fault(c f.Code) *f.Fault { return f.NewFault(c, f.NotStarted) }
func portError(e error) error {
	if e == nil {
		return nil
	}
	var ff *f.Fault
	if errors.As(e, &ff) {
		return e
	}
	return fault(f.DependencyUnavailable).WithCause(e)
}
func dbError(e error) error {
	var p *pgconn.PgError
	if errors.As(e, &p) && p.Code == "22003" {
		return fault(f.InvalidState).WithCause(e)
	}
	return portError(e)
}
func corrupt(e error) error { return fault(f.InternalError).WithCause(e) }
func hash(b []byte) f.Digest {
	h := sha256.Sum256(b)
	return f.Digest("sha256:" + hex.EncodeToString(h[:]))
}
func encode(v any) ([]byte, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, corrupt(e)
	}
	return b, nil
}
func strict(b []byte, v any, limit int) error {
	if len(b) == 0 || len(b) > limit {
		return corrupt(nil)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return corrupt(e)
	}
	if d.Decode(new(any)) != io.EOF {
		return corrupt(nil)
	}
	return nil
}
func dbNow(ctx context.Context, x postgres.SQLExecutor) (f.Instant, error) {
	var t time.Time
	if e := x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&t); e != nil {
		return f.Instant{}, dbError(e)
	}
	v, e := f.NewInstant(t)
	return v, portError(e)
}
func readCause(name string) (f.TransactionCause, error) {
	v, e := f.NewID[struct{}]()
	if e != nil {
		return f.TransactionCause{}, portError(e)
	}
	return f.NewRecoveryCause("model.usage."+name, v.String(), "")
}

// Unknown retains the original transaction identity. It is never dispatch unknown.
type UnknownError struct{ data func() f.CommitResult }

func (e *UnknownError) Error() string { return string(f.CommitUnknown) }
func (e *UnknownError) Unwrap() error { return f.NewFault(f.CommitUnknown, f.Unknown) }
func (e *UnknownError) Cause() f.TransactionCause {
	if e == nil || e.data == nil {
		return f.TransactionCause{}
	}
	return e.data().Cause()
}
func (e *UnknownError) AttemptID() f.ID[f.TransactionAttempt] {
	if e == nil || e.data == nil {
		return f.ID[f.TransactionAttempt]{}
	}
	return e.data().AttemptID()
}
func (*UnknownError) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("COMMIT_UNKNOWN")) }
func (*UnknownError) LogValue() slog.Value       { return slog.StringValue("COMMIT_UNKNOWN") }
func (*UnknownError) MarshalJSON() ([]byte, error) {
	return []byte(`{"code":"COMMIT_UNKNOWN","commit_state":"unknown"}`), nil
}
func commitError(r f.CommitResult) error {
	switch r.State() {
	case f.Committed:
		return nil
	case f.NotCommitted:
		return r.Fault()
	default:
		return &UnknownError{func() f.CommitResult { return r }}
	}
}

type consumerDTO struct {
	Format int         `json:"format_version"`
	Value  mc.Consumer `json:"value"`
}
type inputDTO struct {
	Format int              `json:"format_version"`
	Value  mc.InputIdentity `json:"value"`
}
type initiatorDTO struct {
	Format int             `json:"format_version"`
	Value  id.ActorDetails `json:"value"`
}
type identityDTO struct {
	Format int              `json:"format_version"`
	Value  mc.ModelIdentity `json:"value"`
}
type receiptDTO struct {
	Format int                  `json:"format_version"`
	Value  uc.InvocationReceipt `json:"value"`
}
type headerDTO struct {
	Format    int
	Identity  uc.InvocationIdentity
	Initiator id.ActorDetails
	Model     mc.ModelIdentity
	StartedAt f.Instant
}

func stableActor(a id.Actor) id.ActorDetails {
	d := a.Details()
	if d.Kind == id.Human {
		d.SessionID = ""
	}
	return d
}
func scalarID(s string) bool { v, e := f.ParseID[struct{}](s); return e == nil && v.String() == s }
func validInitiator(a id.ActorDetails) bool {
	if a.SessionID != "" {
		return false
	}
	switch a.Kind {
	case id.Human:
		return scalarID(a.UserID) && a.ProjectID == "" && a.AgentID == "" && a.ExecutionID == "" && a.ServiceName == "" && a.CauseRef == ""
	case id.AgentRun:
		return a.UserID == "" && scalarID(a.ProjectID) && scalarID(a.AgentID) && scalarID(a.ExecutionID) && a.ServiceName == "" && a.CauseRef == ""
	case id.Service:
		if a.UserID != "" || a.AgentID != "" || a.ExecutionID != "" || a.ProjectID != "" && !scalarID(a.ProjectID) || !id.ValidCauseRef(a.CauseRef) {
			return false
		}
		switch a.ServiceName {
		case id.SecretService, id.SecretMaintenance, id.OutboundService, id.ObjectService, id.ObjectMaintenance, id.ProjectLifecycle, id.ProjectInitialization, id.OutboxDelivery, id.AccountBootstrap, id.AccountAuth, id.AccountMaintenance, id.AccountMail, id.ModelRuntime:
			return true
		}
	}
	return false
}
func headerFor(v uc.InvocationFact) headerDTO {
	return headerDTO{1, v.Identity.Clone(), stableActor(v.Initiator), v.Value.Identity, v.Value.StartedAt}
}
func digestHeader(h headerDTO) f.Digest { b, _ := encode(h); return hash(b) }
func historyValue(v uc.Invocation) uc.Invocation {
	v = v.Clone()
	v.LiveModelID = nil
	v.LiveProviderID = nil
	return v
}
func digestFact(h headerDTO, a uc.InvocationAction, seq f.Sequence, v uc.Invocation) f.Digest {
	b, _ := encode(struct {
		Header   headerDTO
		Action   uc.InvocationAction
		Sequence f.Sequence
		Value    uc.Invocation
	}{h, a, seq, historyValue(v)})
	return hash(b)
}
func digestFinal(h headerDTO, v uc.Invocation) f.Digest {
	b, _ := encode(struct {
		Header headerDTO
		Value  uc.Invocation
	}{h, historyValue(v)})
	return hash(b)
}

type invocationRecord struct {
	header       headerDTO
	value        uc.Invocation
	sequence     f.Sequence
	headerDigest f.Digest
	finalDigest  *f.Digest
	updated      f.Instant
}
type scanner interface{ Scan(...any) error }

const invocationColumns = `id::text,call_id::text,attempt_index,project_id::text,snapshot_id::text,process_id::text,fence,consumer_data,input_data,initiator_data,identity_data,header_digest,consumer_kind,purpose,agent_id::text,execution_id::text,meeting_id::text,operation_id::text,historical_provider_id::text,historical_model_id::text,live_provider_id::text,live_model_id::text,dispatch,started_at,dispatched_at,last_sequence,updated_at,final_status,terminal_version,finished_at,error_data,final_digest,input_tokens,output_tokens,total_tokens,cached_input_tokens,cache_write_tokens,reasoning_tokens,usage_source,provider_request_id`

func scanInvocation(row scanner) (*invocationRecord, error) {
	var r invocationRecord
	var key, call, project, snapshot, process, pid, mid string
	var agent, execution, meeting, operation, liveP, liveM *string
	var consumer, input, initiator, identity, rawError []byte
	var kind mc.ConsumerKind
	var purpose mc.Purpose
	var started, updated time.Time
	var dispatched, finished *time.Time
	var status *uc.TerminalStatus
	var version *f.Version
	v := &r.value
	e := row.Scan(&key, &call, &v.AttemptIndex, &project, &snapshot, &process, &v.Fence, &consumer, &input, &initiator, &identity, &r.headerDigest, &kind, &purpose, &agent, &execution, &meeting, &operation, &pid, &mid, &liveP, &liveM, &v.Dispatch, &started, &dispatched, &r.sequence, &updated, &status, &version, &finished, &rawError, &r.finalDigest, &v.Usage.InputTokens, &v.Usage.OutputTokens, &v.Usage.TotalTokens, &v.Usage.CachedInputTokens, &v.Usage.CacheWriteTokens, &v.Usage.ReasoningTokens, &v.Usage.Source, &v.ProviderRequestID)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, dbError(e)
	}
	var c consumerDTO
	var i inputDTO
	var a initiatorDTO
	var m identityDTO
	for _, p := range []struct {
		b []byte
		v any
	}{{consumer, &c}, {input, &i}, {initiator, &a}, {identity, &m}} {
		if e = strict(p.b, p.v, 16<<10); e != nil {
			return nil, e
		}
	}
	if c.Format != 1 || i.Format != 1 || a.Format != 1 || m.Format != 1 {
		return nil, corrupt(nil)
	}
	if !scalarID(key) || !scalarID(call) || !scalarID(project) || !scalarID(snapshot) || !scalarID(process) {
		return nil, corrupt(nil)
	}
	v.ID, _ = f.ParseID[mc.Invocation](key)
	v.CallID, _ = f.ParseID[mc.Call](call)
	v.SnapshotID, _ = f.ParseID[mc.Snapshot](snapshot)
	_ = v.ProcessID.UnmarshalText([]byte(process))
	v.Consumer = c.Value
	v.Identity = m.Value
	v.StartedAt, _ = f.NewInstant(started)
	r.updated, _ = f.NewInstant(updated)
	if dispatched != nil {
		t, _ := f.NewInstant(*dispatched)
		v.DispatchedAt = &t
	}
	if liveP != nil {
		p, e := f.ParseID[mc.Provider](*liveP)
		if e != nil {
			return nil, corrupt(e)
		}
		v.LiveProviderID = &p
	}
	if liveM != nil {
		m, e := f.ParseID[mc.Model](*liveM)
		if e != nil {
			return nil, corrupt(e)
		}
		v.LiveModelID = &m
	}
	if status != nil {
		if version == nil || finished == nil || r.finalDigest == nil {
			return nil, corrupt(nil)
		}
		t, _ := f.NewInstant(*finished)
		v.Final = &uc.Final{Status: *status, Version: *version, FinishedAt: t}
		if len(rawError) > 0 {
			var me mc.ModelError
			if e = strict(rawError, &me, 4<<10); e != nil {
				return nil, e
			}
			v.Final.Error = &me
		}
	} else if version != nil || finished != nil || r.finalDigest != nil || len(rawError) > 0 {
		return nil, corrupt(nil)
	}
	r.header = headerDTO{1, uc.InvocationIdentity{Attempt: mc.AttemptIdentity{CallID: v.CallID, InvocationID: v.ID, AttemptIndex: v.AttemptIndex, ProcessID: v.ProcessID, Fence: v.Fence}, Consumer: v.Consumer.Clone(), SnapshotID: v.SnapshotID, Input: i.Value}, a.Value, v.Identity, v.StartedAt}
	if v.Validate() != nil || r.header.Identity.Validate() != nil || !validInitiator(a.Value) || r.sequence.Validate() != nil || r.headerDigest != digestHeader(r.header) || updated.Before(started) || v.Consumer.ProjectID.String() != project || kind != v.Consumer.Kind || purpose != v.Consumer.Purpose || nullableID(v.Consumer.AgentID) != textPtr(agent) || nullableID(v.Consumer.ExecutionID) != textPtr(execution) || v.Consumer.MeetingID != textPtr(meeting) || v.Consumer.OperationID != textPtr(operation) || v.Identity.ProviderID.String() != pid || v.Identity.ModelID.String() != mid {
		return nil, corrupt(nil)
	}
	if r.finalDigest != nil && *r.finalDigest != digestFinal(r.header, *v) {
		return nil, corrupt(nil)
	}
	if e = validLedgerValue(*v); e != nil {
		return nil, corrupt(e)
	}
	return &r, nil
}
func nullableID[T interface{ String() string }](v *T) string {
	if v == nil {
		return ""
	}
	return (*v).String()
}
func textPtr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func loadInvocation(ctx context.Context, x postgres.SQLExecutor, invocation mc.InvocationID) (*invocationRecord, error) {
	return scanInvocation(x.QueryRow(ctx, `SELECT `+invocationColumns+` FROM agenteam_model.invocations WHERE id=$1`, invocation.String()))
}
func sameHeader(a, b headerDTO) bool    { return reflect.DeepEqual(a, b) }
func sameValue(a, b uc.Invocation) bool { return reflect.DeepEqual(historyValue(a), historyValue(b)) }
func loadReceipt(ctx context.Context, x postgres.SQLExecutor, r uc.InvocationRequest, h headerDTO) (*uc.InvocationReceipt, error) {
	var action uc.InvocationAction
	var digest f.Digest
	var raw []byte
	e := x.QueryRow(ctx, `SELECT action,fact_digest,receipt_data FROM agenteam_model.invocation_observations WHERE invocation_id=$1 AND project_id=$2 AND sequence=$3`, r.Identity.Attempt.InvocationID.String(), r.Identity.Consumer.ProjectID.String(), int64(r.Sequence)).Scan(&action, &digest, &raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, dbError(e)
	}
	var dto receiptDTO
	if e = strict(raw, &dto, 16<<10); e != nil {
		return nil, e
	}
	v := dto.Value
	if dto.Format != 1 || v.Validate() != nil || v.Action != action || v.Sequence != r.Sequence || v.Digest != digest || digest != digestFact(h, action, v.Sequence, v.Value) || v.Value.ID != h.Identity.Attempt.InvocationID || !v.Value.Consumer.Equal(h.Identity.Consumer) || v.Value.LiveProviderID != nil || v.Value.LiveModelID != nil {
		return nil, corrupt(nil)
	}
	return &v, nil
}
