package outbox

import (
	"context"
	"errors"
	"reflect"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type Store interface {
	postgres.SQLExecutor
	WithinTx(context.Context, foundation.TransactionCause, func(context.Context, foundation.Tx) error) foundation.CommitResult
	InTx(foundation.Tx) (postgres.SQLExecutor, error)
	AcquireAll(context.Context, foundation.Tx, []foundation.LockRequest) error
	RequireHeldLocks(context.Context, foundation.Tx, []foundation.LockRequest) error
}
type Authorizations struct {
	Producers map[event.StableName]oc.ProducerAuthority
	Projects  oc.ProjectAuthority
	Processes oc.ProcessAuthority
	Sessions  identity.SessionAuthority
	System    identity.SystemAuthority
	Audit     ac.Appender
	Cursors   cursor.Keyring
}
type serviceState struct {
	store         Store
	catalog       *event.Catalog
	auth          Authorizations
	issuer        oc.PlanIssuer
	mu            sync.RWMutex
	registerMu    sync.Mutex
	handlers      map[event.StableName]oc.HandlerDefinition
	runtime       *Runtime
	claimsStopped bool
}
type Service struct{ data func() *serviceState }

func New(store Store, catalog *event.Catalog, auth Authorizations) (*Service, error) {
	if nilPort(store) || !catalog.Valid() {
		return nil, invalid()
	}
	copyAuth := Authorizations{Projects: auth.Projects, Processes: auth.Processes, Sessions: auth.Sessions, System: auth.System, Audit: auth.Audit, Cursors: auth.Cursors, Producers: make(map[event.StableName]oc.ProducerAuthority)}
	for name, provider := range auth.Producers {
		if name.Validate() != nil || nilPort(provider) {
			return nil, invalid()
		}
		copyAuth.Producers[name] = provider
	}
	if err := catalog.Seal(); err != nil {
		return nil, err
	}
	d := &serviceState{store: store, catalog: catalog, auth: copyAuth, issuer: oc.NewPlanIssuer(), handlers: make(map[event.StableName]oc.HandlerDefinition)}
	return &Service{func() *serviceState { return d }}, nil
}
func (s *Service) state() *serviceState { return s.data() }
func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func invalid() error { return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted) }
func failure(code foundation.Code, cause error) error {
	return foundation.NewFault(code, foundation.NotStarted).WithCause(cause)
}
func unavailable(cause error) error { return failure(foundation.DependencyUnavailable, cause) }
func portError(err error) error {
	var f *foundation.Fault
	if errors.As(err, &f) && f != nil {
		return f
	}
	return unavailable(err)
}
func commitError(result foundation.CommitResult) error {
	switch result.State() {
	case foundation.Committed:
		return nil
	case foundation.NotCommitted:
		if f := result.Fault(); f != nil {
			return f
		}
		return foundation.NewFault(foundation.InternalError, foundation.NotCommitted)
	default:
		return foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
	}
}
func registryLock() foundation.LockKey {
	k, _ := foundation.SystemConfigLock("outbox-registration")
	return k
}
func eventLock(id event.EventID) foundation.LockKey {
	k, _ := foundation.RecordLock(foundation.OutboxRecordLock, "event:"+id.String())
	return k
}
func handlerLock(id event.StableName) foundation.LockKey {
	k, _ := foundation.RecordLock(foundation.OutboxRecordLock, "handler:"+oc.DigestBytes([]byte(id)).String())
	return k
}
func deliveryLock(id oc.DeliveryID) foundation.LockKey {
	k, _ := foundation.RecordLock(foundation.OutboxRecordLock, "delivery:"+id.String())
	return k
}
func recoveryCause(owner string) foundation.TransactionCause {
	id, _ := foundation.NewID[foundation.TransactionAttempt]()
	c, _ := foundation.NewRecoveryCause(owner, id.String(), "")
	return c
}
func executor(s *Service, tx foundation.Tx) (postgres.SQLExecutor, error) {
	e, err := s.state().store.InTx(tx)
	if err != nil {
		return nil, unavailable(err)
	}
	return e, nil
}

// CheckStorage is usable before a dispatcher exists. It validates the durable
// mechanism, including the sequence assumptions required by registration.
func (s *Service) CheckStorage(ctx context.Context) error {
	var format int
	if err := s.state().store.QueryRow(ctx, `SELECT format FROM agenteam_outbox.control WHERE singleton`).Scan(&format); err != nil || format != 1 {
		return unavailable(err)
	}
	var increment, min, max, cache int64
	var cycle bool
	if err := s.state().store.QueryRow(ctx, `SELECT seqincrement,seqmin,seqmax,seqcache,seqcycle FROM pg_sequence WHERE seqrelid='agenteam_outbox.outbox_sequence'::regclass`).Scan(&increment, &min, &max, &cache, &cycle); err != nil || increment != 1 || min != 1 || max != 9223372036854775807 || cache != 1 || cycle {
		return unavailable(err)
	}
	// Resolve every durable relation even when the queue is empty. A missing
	// marker/attempt table must not look healthy merely because handlers exist.
	var tables [6]bool
	if err := s.state().store.QueryRow(ctx, `SELECT
        EXISTS(SELECT id,format FROM agenteam_outbox.events LIMIT 1),
        EXISTS(SELECT id,format FROM agenteam_outbox.deliveries LIMIT 1),
        EXISTS(SELECT id,format FROM agenteam_outbox.attempts LIMIT 1),
        EXISTS(SELECT event_id,format FROM agenteam_outbox.processed LIMIT 1),
        EXISTS(SELECT command_hash,format FROM agenteam_outbox.requeue_commands LIMIT 1),
        EXISTS(SELECT project_id,format FROM agenteam_outbox.project_lifecycle LIMIT 1)`).Scan(&tables[0], &tables[1], &tables[2], &tables[3], &tables[4], &tables[5]); err != nil {
		return unavailable(err)
	}
	// Version nine is the transactional representation of a genuinely stopped
	// but never claimed Project delivery. Do not start on the older CHECK.
	var stopSchema bool
	if err := s.state().store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_meta.migration_journal WHERE version=9 AND filename='00009_outbox_unattempted_stop.sql' AND mode='tx' AND state='applied') AND EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='agenteam_outbox.deliveries'::regclass AND conname='outbox_deliveries_attempt_check' AND convalidated AND position('project_stopped' in pg_get_constraintdef(oid))>0)`).Scan(&stopSchema); err != nil || !stopSchema {
		return unavailable(err)
	}
	var handlers, fanout int
	if err := s.state().store.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_outbox.handlers),coalesce((SELECT max(n) FROM (SELECT count(*) n FROM agenteam_outbox.subscriptions GROUP BY event_type) s),0)`).Scan(&handlers, &fanout); err != nil || handlers > 128 || fanout > 128 {
		return unavailable(err)
	}
	return nil
}

func actorLocks(a identity.Actor) ([]foundation.LockRequest, error) {
	if a.Validate() != nil {
		return nil, invalid()
	}
	d := a.Details()
	var out []foundation.LockRequest
	add := func(k foundation.LockKey, e error) error {
		if e != nil {
			return invalid()
		}
		out = append(out, foundation.LockRequest{Key: k, Mode: foundation.Shared})
		return nil
	}
	switch d.Kind {
	case identity.Human:
		if err := add(foundation.UserLock(d.UserID)); err != nil {
			return nil, err
		}
	case identity.AgentRun:
		if err := add(foundation.ProjectLock(d.ProjectID)); err != nil {
			return nil, err
		}
		if err := add(foundation.AgentLock(d.AgentID)); err != nil {
			return nil, err
		}
		if err := add(foundation.AggregateLock(foundation.ExecutionAggregate, d.ExecutionID)); err != nil {
			return nil, err
		}
	case identity.Service:
	default:
		return nil, invalid()
	}
	return out, nil
}
