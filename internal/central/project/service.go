package project

import (
	"context"
	"errors"
	"reflect"
	"sync"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type ActivityAuthority interface {
	TouchActivityInTx(context.Context, foundation.Tx, identity.Actor) error
}
type Dependencies struct {
	Authority         *Authority
	Activity          ActivityAuthority
	Audit             audit.Appender
	Events            oc.Appender
	ProjectEvents     c.ProjectEvents
	Initializer       c.ProjectSkillInitializer
	Processes         oc.ProcessAuthority
	Cursors           cursor.Keyring
	LifecycleRegistry *LifecycleRegistry
}
type Config struct{ MaxInitializing int }

func DefaultConfig() Config { return Config{MaxInitializing: 16} }

// Service implements Project commands and lifecycle acceptance, not the later
// participant runtime or HTTP adapters.
// An absent initializer is allowed at construction for reading existing data;
// a new Create explicitly fails before reserving a target or a name.
type Service struct{ data func() *serviceState }
type serviceState struct {
	store            Store
	deps             Dependencies
	initRegistration identity.ServiceRegistration
	mu               sync.Mutex
	stopped          bool
	calls            map[*call]struct{}
	changed          chan struct{}
	slots            chan struct{}
	joined           map[string]bool
}
type call struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func sameStore(a, b Store) bool {
	return reflect.TypeOf(a) == reflect.TypeOf(b) && reflect.TypeOf(a).Comparable() && a == b
}
func New(store Store, d Dependencies, cfg Config) (*Service, error) {
	if nilPort(store) || d.Authority.state() == nil || !sameStore(d.Authority.state().store, store) || nilPort(d.Activity) || nilPort(d.Audit) || nilPort(d.Events) || nilPort(d.Processes) {
		return nil, fault(foundation.DependencyUnbound)
	}
	if cfg.MaxInitializing < 1 || cfg.MaxInitializing > 64 || d.Cursors.Validate() != nil || d.Processes.CurrentProcess().Validate() != nil {
		return nil, invalid()
	}
	if reflect.ValueOf(d.ProjectEvents).IsZero() {
		return nil, invalid()
	}
	if d.LifecycleRegistry != nil {
		manifest, err := d.LifecycleRegistry.Manifest()
		if err != nil {
			return nil, err
		}
		if _, err = d.LifecycleRegistry.Resolve(manifest); err != nil {
			return nil, err
		}
	}
	registration, e := identity.RegisterService(identity.ProjectInitialization)
	if e != nil {
		return nil, e
	}
	st := &serviceState{store: store, deps: d, initRegistration: registration, calls: map[*call]struct{}{}, changed: make(chan struct{}), slots: make(chan struct{}, cfg.MaxInitializing), joined: map[string]bool{}}
	return &Service{data: func() *serviceState { return st }}, nil
}
func (s *Service) state() *serviceState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}
func (s *Service) begin(ctx context.Context) (context.Context, func(), error) {
	st := s.state()
	if st == nil {
		return nil, nil, fault(foundation.DependencyUnbound)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.stopped {
		return nil, nil, fault(foundation.ShuttingDown)
	}
	run, cancel := context.WithCancel(ctx)
	c := &call{run, cancel}
	st.calls[c] = struct{}{}
	var once sync.Once
	done := func() {
		once.Do(func() {
			cancel()
			st.mu.Lock()
			delete(st.calls, c)
			close(st.changed)
			st.changed = make(chan struct{})
			st.mu.Unlock()
		})
	}
	return run, done, nil
}
func (s *Service) Stop() {
	st := s.state()
	if st == nil {
		return
	}
	st.mu.Lock()
	st.stopped = true
	for c := range st.calls {
		c.cancel()
	}
	st.mu.Unlock()
}
func (s *Service) Drain(ctx context.Context) error {
	st := s.state()
	if st == nil {
		return nil
	}
	for {
		st.mu.Lock()
		if len(st.calls) == 0 {
			st.mu.Unlock()
			return nil
		}
		changed := st.changed
		st.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Force requests cancellation; only Drain proves that every borrowed resource
// and external initializer call actually returned. It never fabricates a join.
func (s *Service) Force() { s.Stop() }
func (s *Service) slot() bool {
	select {
	case s.state().slots <- struct{}{}:
		return true
	default:
		return false
	}
}
func (s *Service) releaseSlot() { <-s.state().slots }
func (s *Service) ResolveProjectPath(ctx context.Context, a identity.Actor, user, name string) (c.ProjectRef, error) {
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return c.ProjectRef{}, e
	}
	defer done()
	return s.state().deps.Authority.ResolveProjectPath(ctx, a, user, name)
}
func (s *Service) GetProject(ctx context.Context, actor identity.Actor, id c.ProjectID) (c.ProjectRef, error) {
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return c.ProjectRef{}, e
	}
	defer done()
	if e = human(actor); e != nil {
		return c.ProjectRef{}, e
	}
	if id.Validate() != nil {
		return c.ProjectRef{}, invalid()
	}
	cause, e := readCause("get")
	if e != nil {
		return c.ProjectRef{}, e
	}
	var ref c.ProjectRef
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared), projectLock(id, foundation.Shared)}); e != nil {
			return unavailable(e)
		}
		access, e := s.state().deps.Authority.RequireOwnerInTx(ctx, tx, actor, id, identity.Read)
		if e != nil {
			return e
		}
		ref = access.Project()
		return nil
	})
	if e = commitError(result); e != nil {
		return c.ProjectRef{}, e
	}
	return ref, nil
}
func (s *Service) ListOwnedProjects(ctx context.Context, actor identity.Actor, request c.ListOwnedProjectsRequest, page foundation.PageRequest) (foundation.Page[c.ProjectListItem], error) {
	empty := foundation.Page[c.ProjectListItem]{}
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return empty, e
	}
	defer done()
	if e = human(actor); e != nil {
		return empty, e
	}
	if e = c.ValidateProjectPage(page); e != nil {
		return empty, e
	}
	filter, e := request.NormalizedFilter()
	if e != nil {
		return empty, e
	}
	owner, e := parseID[identity.User](actor.Details().UserID)
	if e != nil {
		return empty, e
	}
	query, e := c.OwnedProjectsQueryDigest(owner, request)
	if e != nil {
		return empty, e
	}
	binding := cursor.Binding{Scope: identity.SystemScope(), QueryDigest: query, Order: "created_at-desc,id-desc"}
	var afterTime any
	var afterID any
	if page.Cursor != "" {
		position, e := s.state().deps.Cursors.Verify(page.Cursor, binding)
		if e != nil {
			return empty, e
		}
		if len(position.Scalars) != 2 || position.OrderGeneration != nil || position.Scalars[0].Kind() != "instant" || position.Scalars[1].Kind() != "uuid" {
			return empty, fault(foundation.CursorInvalid)
		}
		t, e := foundation.ParseInstant(position.Scalars[0].Value())
		if e != nil {
			return empty, fault(foundation.CursorInvalid)
		}
		id, e := foundation.ParseID[identity.Project](position.Scalars[1].Value())
		if e != nil {
			return empty, fault(foundation.CursorInvalid)
		}
		afterTime, afterID = t.Time(), id.String()
	}
	names := make([]string, len(filter))
	for i, v := range filter {
		names[i] = string(v)
	}
	cause, e := readCause("list")
	if e != nil {
		return empty, e
	}
	output := empty
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared)}); e != nil {
			return unavailable(e)
		}
		if e := s.state().deps.Authority.state().sessions.RequireCurrentSession(ctx, tx, actor); e != nil {
			return portError(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		rows, e := x.Query(ctx, `SELECT `+projectColumns+` FROM agenteam_project.projects WHERE owner_user_id=$1 AND initialized_at IS NOT NULL AND lifecycle=ANY($2::text[]) AND ($3::timestamptz IS NULL OR (created_at,id)<($3::timestamptz,$4::uuid)) ORDER BY created_at DESC,id DESC LIMIT $5`, owner.String(), names, afterTime, afterID, page.Limit+1)
		if e != nil {
			return unavailable(e)
		}
		defer rows.Close()
		refs := []*projectRecord{}
		for rows.Next() {
			r, e := scanProject(rows)
			if e != nil {
				return e
			}
			refs = append(refs, r)
		}
		if e = rows.Err(); e != nil {
			return unavailable(e)
		}
		more := len(refs) > page.Limit
		if more {
			refs = refs[:page.Limit]
		}
		output.Items = make([]c.ProjectListItem, 0, len(refs))
		for _, r := range refs {
			item := c.ProjectListItem{ID: r.ref.ID, Name: r.ref.Name, Lifecycle: r.ref.Lifecycle, Version: r.ref.Version}
			if r.ref.Lifecycle != c.Deleting {
				description := r.ref.Description
				item.Description = &description
			}
			if r.ref.Lifecycle == c.Archiving || r.ref.Lifecycle == c.Deleting {
				item.OperationID = r.operation
			}
			if item.Validate() != nil {
				return unavailable(nil)
			}
			output.Items = append(output.Items, item)
		}
		if more {
			last := refs[len(refs)-1]
			t, _ := cursor.Instant(last.ref.CreatedAt)
			id, _ := cursor.UUID(last.ref.ID.String())
			output.NextCursor, e = s.state().deps.Cursors.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{t, id}})
			if e != nil {
				return e
			}
		}
		return nil
	})
	if e = commitError(result); e != nil {
		return empty, e
	}
	return output, nil
}

// commitFailure preserves the opaque original physical attempt and cause. A
// timeout or a failed lookup never rewrites Unknown as NotCommitted.
type commitFailure struct{ result foundation.CommitResult }

func (e commitFailure) Error() string { return string(foundation.CommitUnknown) }
func UnknownAttempt(err error) (foundation.CommitResult, bool) {
	var e commitFailure
	if errors.As(err, &e) {
		return e.result, true
	}
	return foundation.CommitResult{}, false
}
func commitError(result foundation.CommitResult) error {
	switch result.State() {
	case foundation.Committed:
		return nil
	case foundation.NotCommitted:
		return result.Fault()
	default:
		f := foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
		f.RetryHint = "lookup"
		if result.AttemptID().Validate() == nil {
			f.CauseID = result.AttemptID().String()
		}
		return f.WithCause(commitFailure{result})
	}
}

// This is only valid after the original writer's complete lock union has
// serialized and its canonical checkpoint proves that attempt did not commit.
// Keep the physical Unknown as private provenance rather than manufacturing a
// different transaction attempt or erasing its cause.
func notCommittedAfterUnknown(original foundation.CommitResult) error {
	f := foundation.NewFault(foundation.DependencyUnavailable, foundation.NotCommitted)
	f.RetryHint = "retry_same_key"
	if original.AttemptID().Validate() == nil {
		f.CauseID = original.AttemptID().String()
	}
	return f.WithCause(commitFailure{original})
}
