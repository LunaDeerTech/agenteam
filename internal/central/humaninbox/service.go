package humaninbox

import (
	"context"
	"encoding/json"
	"errors"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/humaninbox/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"reflect"
	"time"
)

type Store interface {
	postgres.SQLExecutor
	InTx(f.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	AcquireAll(context.Context, f.Tx, []f.LockRequest) error
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}
type Authority struct {
	store    Store
	projects pc.ProjectAuthority
	issuer   oc.PlanIssuer
}

func NewAuthority(store Store, projects pc.ProjectAuthority) (*Authority, error) {
	if nilPort(store) || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &Authority{store, projects, oc.NewPlanIssuer()}, nil
}

type Service struct {
	authority      *Authority
	sources        c.SourceProvider
	events         mc.Events
	decisions      c.DecisionSourceProvider
	decisionEvents mc.DecisionEvents
}

func NewService(a *Authority, sources c.SourceProvider, events mc.Events) (*Service, error) {
	if a == nil || nilPort(sources) || !events.Valid() {
		return nil, fault(f.DependencyUnbound)
	}
	return &Service{authority: a, sources: sources, events: events}, nil
}
func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func fault(code f.Code) error { return f.NewFault(code, f.NotStarted) }
func portError(err error) error {
	if err == nil {
		return nil
	}
	var v *f.Fault
	if errors.As(err, &v) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(err)
}
func commitError(r f.CommitResult) error {
	if r.State() == f.Committed {
		return nil
	}
	if r.State() == f.NotCommitted && r.Fault() != nil {
		return r.Fault()
	}
	if r.State() == f.Unknown {
		e := f.NewFault(f.CommitUnknown, f.Unknown)
		e.RetryHint = "reload"
		return e
	}
	return fault(f.InternalError)
}
func projectLock(p i.ProjectID, mode f.LockMode) f.LockRequest {
	k, _ := f.ProjectLock(p.String())
	return f.LockRequest{Key: k, Mode: mode}
}
func meetingLock(id string) f.LockRequest {
	k, _ := f.AggregateLock(f.MeetingAggregate, id)
	return f.LockRequest{Key: k, Mode: f.Shared}
}
func ownerLocks(a i.Actor, p i.ProjectID, mode f.LockMode) []f.LockRequest {
	k, _ := f.UserLock(a.Details().UserID)
	return []f.LockRequest{{Key: k, Mode: f.Shared}, projectLock(p, mode)}
}
func readCause() (f.TransactionCause, error) {
	id, e := f.NewID[f.Request]()
	if e != nil {
		return f.TransactionCause{}, portError(e)
	}
	return f.NewRecoveryCause("human-inbox", id.String(), "")
}
func (a *Authority) owner(ctx context.Context, tx f.Tx, actor i.Actor, p i.ProjectID) error {
	if actor.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	if actor.Details().Kind != i.Human {
		return fault(f.Forbidden)
	}
	g, err := a.projects.RequireOwnerInTx(ctx, tx, actor, p, i.Read)
	if err != nil {
		return portError(err)
	}
	if !g.Matches(actor, p) {
		return fault(f.Forbidden)
	}
	return nil
}
func (s *Service) List(ctx context.Context, a i.Actor, r c.ListRequest) (c.Page, error) {
	if s == nil || ctx == nil || r.ProjectID.Validate() != nil || r.Limit < 0 || r.Limit > 100 || r.Before != nil && r.Before.Validate() != nil {
		return c.Page{}, fault(f.InvalidArgument)
	}
	if a.Validate() != nil {
		return c.Page{}, fault(f.Unauthenticated)
	}
	if a.Details().Kind != i.Human {
		return c.Page{}, fault(f.Forbidden)
	}
	if r.Status == "" {
		r.Status = "open"
	}
	if r.Status != "open" && r.Status != "resolved" {
		return c.Page{}, fault(f.InvalidArgument)
	}
	if r.Limit == 0 {
		r.Limit = 50
	}
	locks, err := oc.NormalizeLocks(ownerLocks(a, r.ProjectID, f.Shared))
	if err != nil {
		return c.Page{}, err
	}
	cause, err := readCause()
	if err != nil {
		return c.Page{}, err
	}
	out := c.Page{Items: []c.Item{}}
	result := s.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.authority.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.authority.owner(ctx, tx, a, r.ProjectID); err != nil {
			return err
		}
		x, err := s.authority.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		var before any
		var at any
		var blocking any
		var priority any
		if r.Before != nil {
			var t time.Time
			var b bool
			var priorityValue int
			err = x.QueryRow(ctx, `SELECT blocking,priority,updated_at FROM agenteam_humaninbox.items WHERE id=$1 AND project_id=$2 AND status=$3`, r.Before.String(), r.ProjectID.String(), r.Status).Scan(&b, &priorityValue, &t)
			if errors.Is(err, pgx.ErrNoRows) {
				return fault(f.InvalidArgument)
			}
			if err != nil {
				return portError(err)
			}
			before = r.Before.String()
			at = t
			blocking, priority = b, priorityValue
		}
		rows, err := x.Query(ctx, `SELECT id,source_type,source_id,action_type,source_context,status,blocking,priority,dismissible,title_snapshot,summary_snapshot,source_version,source_occurred_at,created_at,updated_at,resolved_at FROM agenteam_humaninbox.items WHERE project_id=$1 AND status=$2 AND ($3::text IS NULL OR (blocking,priority,updated_at,id)<($4::boolean,$5::integer,$6::timestamptz,$3::text)) ORDER BY blocking DESC,priority DESC,updated_at DESC,id DESC LIMIT $7`, r.ProjectID.String(), r.Status, before, blocking, priority, at, r.Limit+1)
		if err != nil {
			return portError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var v c.Item
			var id string
			var version int64
			v.ProjectID = r.ProjectID
			var sourceContext json.RawMessage
			if err = rows.Scan(&id, &v.SourceType, &v.SourceID, &v.ActionType, &sourceContext, &v.Status, &v.Blocking, &v.Priority, &v.Dismissible, &v.Title, &v.Summary, &version, &v.SourceOccurredAt, &v.CreatedAt, &v.UpdatedAt, &v.ResolvedAt); err != nil {
				return portError(err)
			}
			v.ID, err = f.ParseID[c.ItemIdentity](id)
			if err != nil {
				return fault(f.InternalError)
			}
			v.SourceVersion = f.Version(version)
			v.Context, err = decodeSourceContext(v.SourceType, v.ActionType, sourceContext)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, v)
		}
		return portError(rows.Err())
	})
	if err = commitError(result); err != nil {
		return c.Page{}, err
	}
	if len(out.Items) > r.Limit {
		out.Items = out.Items[:r.Limit]
		id := out.Items[len(out.Items)-1].ID
		out.NextBefore = &id
	}
	return out, nil
}

type sourceReadKey struct{}
type sourceReadWitness struct {
	owner        *Authority
	tx           f.Tx
	project      i.ProjectID
	source       string
	scan, active bool
}

func (a *Authority) withSource(ctx context.Context, tx f.Tx, p i.ProjectID, id string, scan bool, fn func(context.Context) error) error {
	w := &sourceReadWitness{a, tx, p, id, scan, true}
	defer func() { w.active = false }()
	return fn(context.WithValue(ctx, sourceReadKey{}, w))
}
func (a *Authority) RequireSourceReadInTx(ctx context.Context, tx f.Tx, store any, p i.ProjectID, id string, scan bool) error {
	if a == nil || ctx == nil || store == nil || !reflect.TypeOf(store).Comparable() || a.store != store {
		return fault(f.DependencyUnbound)
	}
	w, _ := ctx.Value(sourceReadKey{}).(*sourceReadWitness)
	if w == nil || !w.active || w.owner != a || w.tx != tx || w.project != p || w.source != id || w.scan != scan {
		return fault(f.Forbidden)
	}
	locks := []f.LockRequest{projectLock(p, f.Shared)}
	if scan {
		locks = []f.LockRequest{projectLock(p, f.Exclusive)}
	} else {
		locks = append(locks, meetingLock(id))
	}
	if err := a.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	_, err := a.store.InTx(tx)
	return portError(err)
}
