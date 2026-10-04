package object

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type Store interface {
	postgres.SQLExecutor
	InTx(foundation.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, foundation.TransactionCause, func(context.Context, foundation.Tx) error) foundation.CommitResult
	AcquireAll(context.Context, foundation.Tx, []foundation.LockRequest) error
}
type Authorizations struct {
	Planner   oc.AccessPlanner
	Resources oc.ResourceAuthority
	Read      oc.ObjectReadAuthority
	Gate      oc.ProjectGate
	Cleanup   oc.CleanupAuthority
	Leases    oc.LeaseAuthority
	Processes oc.ProcessAuthority
}
type operation struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	once    sync.Once
	service *Service
}
type operationKey struct{}
type preparation struct {
	payload   oc.PreparedPayload
	actor     string
	owner     oc.ObjectOwner
	objectID  oc.ObjectID
	uploadID  oc.UploadID
	receiptID oc.ReceiptID
	operation *operation
	release   func()
	stop      func() bool
}
type writer struct {
	operation *operation
	attempt   oc.AttemptID
	done      chan struct{}
}
type cleanupRequest struct{ cancel context.CancelFunc }
type serviceState struct {
	accessIssuer                 oc.AccessIssuer
	accessMu                     sync.Mutex
	accessTransactions           map[foundation.Tx]bool
	store                        Store
	backend                      *Backend
	transferBackend              *Backend
	runtime                      *Runtime
	runtimeReady                 bool
	spool                        *Spool
	audit                        ac.Appender
	auth                         Authorizations
	registration                 identity.ServiceRegistration
	process                      oc.ProcessID
	mu                           sync.Mutex
	initialized, stopped, forced bool
	drained                      bool
	operations                   map[*operation]bool
	prepared                     map[oc.PayloadID]*preparation
	writers                      map[oc.AttemptID]*writer
	closedAttempts               map[oc.AttemptID]bool
	closedLeases                 map[oc.LeaseID]oc.ObjectID
	changed                      chan struct{}
	forceContext                 context.Context
	cleanupRequests              map[*cleanupRequest]bool
	workerStop                   chan struct{}
	workerOnce                   sync.Once
	workerDone                   chan struct{}
	maintenance                  MaintenanceStatus
}
type Service struct{ data func() *serviceState }

var (
	_ oc.Objects = (*Service)(nil)
	_ oc.Uploads = (*Service)(nil)
	_ oc.Leases  = (*Service)(nil)
	_ oc.Cleaner = (*Service)(nil)
)

func New(store Store, backend *Backend, spool *Spool, audit ac.Appender, auth Authorizations) (*Service, error) {
	if nilPort(store) || backend == nil || backend.data == nil || spool == nil || spool.data == nil || nilPort(audit) {
		return nil, invalid()
	}
	r, e := identity.RegisterService(identity.ObjectService)
	if e != nil {
		return nil, invalid()
	}
	state := &serviceState{store: store, backend: backend, spool: spool, audit: audit, auth: auth, registration: r, process: spool.state().process, operations: map[*operation]bool{}, prepared: map[oc.PayloadID]*preparation{}, writers: map[oc.AttemptID]*writer{}, closedAttempts: map[oc.AttemptID]bool{}, closedLeases: map[oc.LeaseID]oc.ObjectID{}, cleanupRequests: map[*cleanupRequest]bool{}, changed: make(chan struct{}), workerStop: make(chan struct{})}
	state.accessIssuer = oc.NewAccessIssuer()
	state.accessTransactions = make(map[foundation.Tx]bool)
	return &Service{func() *serviceState { return state }}, nil
}
func (s *Service) state() *serviceState        { return s.data() }
func (s Service) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "object_service") }
func (s Service) MarshalJSON() ([]byte, error) { return []byte(`"object_service"`), nil }
func (*Service) UnmarshalJSON([]byte) error    { return invalid() }
func (s Service) LogValue() slog.Value         { return slog.StringValue("object_service") }
func (s *Service) begin(ctx context.Context) (*operation, func(), error) {
	return s.admit(ctx, false)
}
func (s *Service) admit(ctx context.Context, initializing bool) (*operation, func(), error) {
	if op, ok := ctx.Value(operationKey{}).(*operation); ok && op.service == s {
		select {
		case <-op.done:
			return nil, nil, failure(foundation.ShuttingDown, nil)
		default:
			return op, func() {}, nil
		}
	}
	r := s.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped || r.forced {
		return nil, nil, failure(foundation.ShuttingDown, nil)
	}
	if (!r.initialized || r.runtime != nil && !r.runtimeReady) && !initializing {
		return nil, nil, unavailable(nil)
	}
	if e := ctx.Err(); e != nil {
		return nil, nil, unavailable(e)
	}
	if len(r.operations) >= 64 {
		return nil, nil, failure(foundation.RateLimited, nil)
	}
	opCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	op := &operation{cancel: cancel, done: make(chan struct{}), service: s}
	op.ctx = context.WithValue(opCtx, operationKey{}, op)
	r.operations[op] = true
	finish := func() {
		op.once.Do(func() {
			cancel()
			r.mu.Lock()
			delete(r.operations, op)
			close(op.done)
			close(r.changed)
			r.changed = make(chan struct{})
			r.mu.Unlock()
		})
	}
	return op, finish, nil
}
func (s *Service) StopAdmission() {
	r := s.state()
	r.mu.Lock()
	r.stopped = true
	r.mu.Unlock()
	r.workerOnce.Do(func() { close(r.workerStop) })
}
func (s *Service) Drain(ctx context.Context) error {
	r := s.state()
	for {
		r.mu.Lock()
		n, ch := len(r.operations)+len(r.cleanupRequests), r.changed
		if r.maintenance.Running {
			n++
		}
		if n == 0 {
			// Fence technical cleanup admission under the same mutex as its
			// registration. Once drain succeeds, a late Cancel keeps its durable
			// checkpoint instead of starting work behind the closed service.
			r.drained = true
			r.mu.Unlock()
			_ = r.backend.Close()
			if r.transferBackend != nil {
				_ = r.transferBackend.Close()
			}
			return r.spool.Close()
		}
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return unavailable(ctx.Err())
		case <-ch:
		}
	}
}
func (s *Service) Force(ctx context.Context) error {
	s.StopAdmission()
	r := s.state()
	r.mu.Lock()
	r.forced = true
	r.forceContext = ctx
	var operations []*operation
	for op := range r.operations {
		operations = append(operations, op)
	}
	var cleanup []*cleanupRequest
	for request := range r.cleanupRequests {
		cleanup = append(cleanup, request)
	}
	r.mu.Unlock()
	for _, request := range cleanup {
		request.cancel()
	}
	for _, op := range operations {
		op.cancel()
	}
	_ = r.backend.Close()
	if r.transferBackend != nil {
		_ = r.transferBackend.Close()
	}
	return s.Drain(ctx)
}
func (s *Service) cleanupContext() (context.Context, context.CancelFunc) {
	r := s.state()
	r.mu.Lock()
	if r.drained {
		r.mu.Unlock()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, func() {}
	}
	parent := r.forceContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	request := &cleanupRequest{cancel}
	r.cleanupRequests[request] = true
	r.mu.Unlock()
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			cancel()
			r.mu.Lock()
			delete(r.cleanupRequests, request)
			close(r.changed)
			r.changed = make(chan struct{})
			r.mu.Unlock()
		})
	}
}
func (s *Service) authorize(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, intent identity.AccessIntent) (oc.OwnerAuthorization, error) {
	if actor.Validate() != nil || owner.Validate() != nil {
		return oc.OwnerAuthorization{}, invalid()
	}
	if nilPort(s.state().auth.Resources) {
		return oc.OwnerAuthorization{}, failure(foundation.DependencyUnbound, nil)
	}
	var grant oc.OwnerAuthorization
	var e error
	if tx.Valid() {
		grant, e = s.state().auth.Resources.AuthorizeOwnerInTx(ctx, tx, actor, owner, intent)
	} else {
		grant, e = s.state().auth.Resources.AuthorizeOwner(ctx, actor, owner, intent)
	}
	if e != nil {
		return oc.OwnerAuthorization{}, portError(e)
	}
	if !grant.Matches(actor, owner, intent) {
		return oc.OwnerAuthorization{}, unavailable(nil)
	}
	if e = ctx.Err(); e != nil {
		return oc.OwnerAuthorization{}, unavailable(e)
	}
	return grant, nil
}
func (s *Service) gate(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, intent identity.AccessIntent) error {
	if nilPort(s.state().auth.Gate) {
		return failure(foundation.DependencyUnbound, nil)
	}
	if e := s.state().auth.Gate.CheckInTx(ctx, tx, actor, owner, intent); e != nil {
		return portError(e)
	}
	return nil
}
func commandIdentity(owner oc.ObjectOwner, key foundation.IdempotencyKey) (foundation.CommandIdentity, error) {
	if owner.Validate() != nil || key.Validate() != nil {
		return foundation.CommandIdentity{}, invalid()
	}
	d := owner.Details()
	owners := []string{d.ID}
	if d.ProjectID != "" {
		owners = append([]string{d.ProjectID}, owners...)
	}
	return foundation.NewCommandIdentity("object", owners, "put."+string(d.Kind), key)
}
func commandHash(c foundation.CommandIdentity) []byte {
	d := sha256.Sum256([]byte(c.Canonical()))
	return d[:]
}
func stableActor(actor identity.Actor) string {
	a := actor.Details()
	a.SessionID = ""
	raw, _ := json.Marshal(struct {
		Kind                                            identity.ActorKind `json:"kind"`
		User, Project, Agent, Execution, Service, Cause string
	}{a.Kind, a.UserID, a.ProjectID, a.AgentID, a.ExecutionID, string(a.ServiceName), a.CauseRef})
	canonical, _ := cursor.CanonicalJSON(raw)
	return string(canonical)
}
func semanticDigest(actor identity.Actor, owner oc.ObjectOwner, meta foundation.CommandMeta, p oc.PreparedPayload) ([]byte, error) {
	d := p.Details()
	return contentSemanticDigest(actor, owner, meta, d.MediaType, d.Length, d.SHA256)
}
func contentSemanticDigest(actor identity.Actor, owner oc.ObjectOwner, meta foundation.CommandMeta, media string, length int64, digest foundation.Digest) ([]byte, error) {
	expected := ""
	if meta.ExpectedVersion != nil {
		expected = meta.ExpectedVersion.String()
	}
	raw, e := json.Marshal(struct {
		Format                           string          `json:"format"`
		Actor                            string          `json:"actor"`
		Owner                            oc.OwnerDetails `json:"owner"`
		Expected, MediaType, Length, SHA string
	}{"canonical-v1", stableActor(actor), owner.Details(), expected, media, foundation.Progress(length).String(), digest.String()})
	if e != nil {
		return nil, invalid()
	}
	canonical, e := cursor.CanonicalJSON(raw)
	if e != nil {
		return nil, invalid()
	}
	sum := sha256.Sum256(canonical)
	return sum[:], nil
}
func recoveryCause() foundation.TransactionCause {
	id, _ := foundation.NewID[oc.CleanupOperation]()
	cause, _ := foundation.NewRecoveryCause("object", id.String(), "")
	return cause
}
func newDigest(b []byte) foundation.Digest {
	return foundation.Digest("sha256:" + hex.EncodeToString(b))
}
func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Service) PreparePayload(ctx context.Context, actor identity.Actor, owner oc.ObjectOwner, media string, length int64, expected *foundation.Digest, source io.ReadCloser) (oc.PreparedPayload, error) {
	if nilPort(source) {
		return oc.PreparedPayload{}, invalid()
	}
	op, release, e := s.begin(ctx)
	if e != nil {
		_ = source.Close()
		return oc.PreparedPayload{}, e
	}
	keep := false
	defer func() {
		if !keep {
			release()
		}
	}()
	if _, e = s.authorize(op.ctx, foundation.Tx{}, actor, owner, identity.Mutate); e != nil {
		_ = source.Close()
		return oc.PreparedPayload{}, e
	}
	checked := s.withinAccess(op.ctx, recoveryCause(), ownerRequest(actor, owner, oc.PrepareAccess, oc.AccessRequestDetails{}), func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		if _, e := s.authorize(ctx, tx, actor, owner, identity.Mutate); e != nil {
			return e
		}
		return nil
	})
	if e = commitError(checked); e != nil {
		_ = source.Close()
		return oc.PreparedPayload{}, e
	}
	p, e := s.state().spool.Prepare(op.ctx, media, length, expected, source)
	if e != nil {
		return oc.PreparedPayload{}, e
	}
	objectID, e := foundation.NewID[oc.StoredObject]()
	if e != nil {
		_ = s.state().spool.Discard(p)
		return oc.PreparedPayload{}, unavailable(e)
	}
	uploadID, e := foundation.NewID[oc.Upload]()
	if e != nil {
		_ = s.state().spool.Discard(p)
		return oc.PreparedPayload{}, unavailable(e)
	}
	receiptID, e := foundation.NewID[oc.Receipt]()
	if e != nil {
		_ = s.state().spool.Discard(p)
		return oc.PreparedPayload{}, unavailable(e)
	}
	entry := &preparation{payload: p, actor: stableActor(actor), owner: owner, objectID: objectID, uploadID: uploadID, receiptID: receiptID, operation: op, release: release}
	r := s.state()
	r.mu.Lock()
	r.prepared[p.Details().ID] = entry
	entry.stop = context.AfterFunc(op.ctx, func() { _ = s.DiscardPrepared(p) })
	r.mu.Unlock()
	keep = true
	return p, nil
}
func (s *Service) DiscardPrepared(p oc.PreparedPayload) error {
	if p.Validate() != nil {
		return invalid()
	}
	r := s.state()
	r.mu.Lock()
	entry := r.prepared[p.Details().ID]
	r.mu.Unlock()
	if entry == nil {
		return nil
	}
	if e := r.spool.Discard(p); e != nil {
		return e
	}
	r.mu.Lock()
	delete(r.prepared, p.Details().ID)
	r.mu.Unlock()
	if entry.stop != nil {
		entry.stop()
	}
	entry.release()
	return nil
}
func (s *Service) prepared(p oc.PreparedPayload, actor identity.Actor, owner oc.ObjectOwner) (*preparation, error) {
	if p.Validate() != nil {
		return nil, invalid()
	}
	r := s.state()
	r.mu.Lock()
	entry := r.prepared[p.Details().ID]
	r.mu.Unlock()
	if entry == nil || entry.actor != stableActor(actor) || !entry.owner.Equal(owner) || entry.payload.Details() != p.Details() {
		return nil, failure(foundation.Forbidden, nil)
	}
	return entry, nil
}
