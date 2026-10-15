package mcp

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type contentTailStore struct {
	Store
	tx           f.Tx
	fact         contentFact
	objectExists bool
	denyLocks    bool
	queries      int
}

func (s *contentTailStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, fail(f.InvalidArgument)
	}
	return s, nil
}
func (s *contentTailStore) RequireHeldLocks(_ context.Context, tx f.Tx, _ []f.LockRequest) error {
	if tx != s.tx || s.denyLocks {
		return fail(f.InvalidState)
	}
	return nil
}
func (s *contentTailStore) QueryRow(context.Context, string, ...any) postgres.Row {
	s.queries++
	return contentTailRow{s.fact, s.objectExists}
}

type contentTailRow struct {
	fact         contentFact
	objectExists bool
}

func (r contentTailRow) Scan(dest ...any) error {
	if len(dest) == 1 {
		*dest[0].(*bool) = r.objectExists
		return nil
	}
	values := []string{r.fact.id, r.fact.project, r.fact.connection, r.fact.runtime, r.fact.execution, r.fact.operation, r.fact.attempt, r.fact.request, r.fact.uri, r.fact.state}
	if len(dest) != len(values)+1 {
		return fail(f.InvalidState)
	}
	for i, value := range values {
		*dest[i].(*string) = value
	}
	*dest[len(values)].(*f.Version) = r.fact.version
	return nil
}

type contentTailResolver struct {
	object oc.ObjectID
	owner  oc.ObjectOwner
	tx     f.Tx
	calls  int
}

func (r *contentTailResolver) Discover(_ context.Context, request oc.AccessRequest) (oc.ObjectOwner, error) {
	r.calls++
	if request.Details().ObjectID != r.object {
		return oc.ObjectOwner{}, fail(f.Forbidden)
	}
	return r.owner, nil
}
func (r *contentTailResolver) ResolveInTx(ctx context.Context, tx f.Tx, request oc.AccessRequest) (oc.ObjectOwner, error) {
	if tx != r.tx {
		return oc.ObjectOwner{}, fail(f.InvalidArgument)
	}
	return r.Discover(ctx, request)
}
func contentTailID[T any](t *testing.T, raw string) f.ID[T] {
	t.Helper()
	v, err := f.ParseID[T]("01960aa5-0286-7000-8000-0000000000" + raw)
	if err != nil {
		t.Fatal("tail fixture identity")
	}
	return v
}
func contentTailFixture(t *testing.T) (*ContentAuthority, *contentUse, *contentTailStore, *contentTailResolver, context.Context, oc.AccessRequest) {
	t.Helper()
	p := contentTailID[id.Project](t, "01")
	agent := contentTailID[id.Agent](t, "02")
	execution := contentTailID[id.Execution](t, "03")
	actor, err := id.NewAgentRun(p, agent, execution)
	if err != nil {
		t.Fatal("tail fixture actor")
	}
	store := &contentTailStore{tx: f.NewTx(), objectExists: true}
	owner, err := oc.NewObjectOwner(oc.MCPContent, contentTailID[struct{}](t, "04").String(), p.String())
	if err != nil {
		t.Fatal("tail fixture owner")
	}
	resolver := &contentTailResolver{object: contentTailID[oc.StoredObject](t, "05"), owner: owner, tx: store.tx}
	a := &ContentAuthority{direct: &Service{store: store}, maintenance: resolver}
	w := &contentUse{owner: a, materialization: owner.Details().ID, call: tc.ToolCallBinding{Actor: actor, ProjectID: p, AgentID: agent, ExecutionID: execution}, operation: contentTailID[tc.Operation](t, "06"), attempt: contentTailID[tc.Attempt](t, "07"), request: contentTailID[struct{}](t, "08").String(), connection: RuntimeConnectionBinding{ProjectID: p, ConnectionID: contentTailID[struct{}](t, "09").String(), RuntimeBindingID: contentTailID[struct{}](t, "10").String()}, require: func(context.Context, f.Tx) error { return fail(f.Forbidden) }}
	lock, _ := f.ProjectLock(p.String())
	w.locks = []f.LockRequest{{Key: lock, Mode: f.Shared}}
	w.active.Store(true)
	store.fact = contentFact{id: w.materialization, project: p.String(), execution: execution.String(), operation: w.operation.String(), attempt: w.attempt.String(), request: w.request, connection: w.connection.ConnectionID, runtime: w.connection.RuntimeBindingID, state: "pending", version: 1}
	request, err := oc.NewMaintenanceAccess(oc.AccessRequestDetails{Operation: oc.FinishWriterAccess, ObjectID: resolver.object, InstanceID: contentTailID[oc.Process](t, "11"), AttemptID: contentTailID[oc.Attempt](t, "12")})
	if err != nil {
		t.Fatal("tail fixture request")
	}
	return a, w, store, resolver, context.WithValue(context.Background(), contentUseKey{}, w), request
}
func TestMCPContentMaintenanceRequiresOriginalLiveTailAndExactObject(t *testing.T) {
	a, w, s, r, ctx, request := contentTailFixture(t)
	for _, bad := range []context.Context{context.Background(), context.WithValue(ctx, contentUseKey{}, &contentUse{owner: a}), context.WithValue(ctx, contentUseKey{}, &contentUse{owner: &ContentAuthority{}})} {
		if _, err := a.Discover(bad, request); err == nil {
			t.Fatal("foreign or bare callback issued maintenance plan")
		}
	}
	if r.calls != 0 || s.queries != 0 {
		t.Fatal("missing proof reached Object mapping")
	}
	plan, err := a.Discover(ctx, request)
	if err != nil {
		t.Fatal("original tail discover")
	}
	if err = a.ValidateInTx(ctx, s.tx, request, plan); err != nil {
		t.Fatal("original tail same-transaction verification")
	}
	changed := request.Details()
	changed.ObjectID = contentTailID[oc.StoredObject](t, "13")
	other, _ := oc.NewMaintenanceAccess(changed)
	if _, err = a.Discover(ctx, other); err == nil {
		t.Fatal("foreign Object borrowed tail")
	}
	changed = request.Details()
	changed.AttemptID = contentTailID[oc.Attempt](t, "14")
	other, _ = oc.NewMaintenanceAccess(changed)
	if err = a.ValidateInTx(ctx, s.tx, other, plan); err == nil {
		t.Fatal("substituted attempt consumed original plan")
	}
	s.fact.request = contentTailID[struct{}](t, "15").String()
	if err = a.ValidateInTx(ctx, s.tx, request, plan); err == nil {
		t.Fatal("changed materialization tuple consumed original plan")
	}
	s.fact.request = w.request
	s.denyLocks = true
	if err = a.ValidateInTx(ctx, s.tx, request, plan); err == nil {
		t.Fatal("maintenance acquired missing locks")
	}
	s.denyLocks = false
	w.active.Store(false)
	if _, err = a.Discover(ctx, request); err == nil {
		t.Fatal("returned callback retained maintenance authority")
	}
}
func TestMCPContentMaintenanceConvergesAfterCancellationWithoutNewAdmission(t *testing.T) {
	a, w, s, r, original, request := contentTailFixture(t)
	ctx, cancel := context.WithCancel(original)
	cancel()
	if _, err := a.Discover(ctx, request); err == nil {
		t.Fatal("cancelled discovery unexpectedly ran")
	}
	// Object's owned cleanup retains values under its own bounded context.
	tail, stop := context.WithCancel(context.WithoutCancel(ctx))
	defer stop()
	plan, err := a.Discover(tail, request)
	if err != nil {
		t.Fatal("original writer tail blocked by cancellation")
	}
	if err = a.ValidateInTx(tail, s.tx, request, plan); err != nil {
		t.Fatal("original writer tail failed")
	}
	if _, err = a.AuthorizeOwnerInTx(tail, s.tx, w.call.Actor, r.owner, id.Mutate); err == nil {
		t.Fatal("tail granted a new mutation despite rejected current admission")
	}
	reader, err := oc.NewMaintenanceAccess(oc.AccessRequestDetails{Operation: oc.ReleaseReaderAccess, ObjectID: r.object, InstanceID: request.Details().InstanceID, LeaseID: contentTailID[oc.Lease](t, "16")})
	if err != nil {
		t.Fatal("reader fixture")
	}
	s.fact.state = "ready"
	plan, err = a.Discover(tail, reader)
	if err != nil {
		t.Fatal("original canonical reader tail rejected")
	}
	s.objectExists = false
	if err = a.ValidateInTx(tail, s.tx, reader, plan); err == nil {
		t.Fatal("reader tail ignored canonical Object membership")
	}
	other, _ := oc.NewMaintenanceAccess(oc.AccessRequestDetails{Operation: oc.InspectAccess, ObjectID: r.object, InstanceID: request.Details().InstanceID})
	if _, err = a.Discover(tail, other); err == nil {
		t.Fatal("tail enabled general maintenance")
	}
}
