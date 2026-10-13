package projectvariable

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// The real capture provider supplies the historical PV header and refs. The
// Execution proof and Secret persistence ports are controlled; these tests do
// not claim the E consumer's real return evidence or PostgreSQL rollback.
type executionVariableStore struct {
	*environmentTestStore
	request ec.ProjectExecutionRetirementRequest
	order   []string
	deletes int
}

func (s *executionVariableStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.secretAuthorityStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *executionVariableStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if len(args) != 1 || args[0] != s.request.Binding.ExecutionID.String() || !strings.Contains(q, "FROM agenteam_projectvariable.execution_") {
		return secretControlRow{err: errors.New("nonhistorical or foreign execution read")}
	}
	return s.environmentTestStore.QueryRow(ctx, q, args...)
}
func (s *executionVariableStore) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.HasPrefix(q, "DELETE FROM agenteam_projectvariable.execution_secret_references"):
		if len(s.refs) != 1 || !reflect.DeepEqual(args, []any{s.request.Binding.ExecutionID.String(), s.refs[0], s.leases[0]}) {
			return pgconn.CommandTag{}, errors.New("nonexact PV ref delete")
		}
		s.order = append(s.order, "refs")
		s.refs, s.leases = nil, nil
	case strings.HasPrefix(q, "DELETE FROM agenteam_projectvariable.execution_environments"):
		if s.head == nil || len(s.refs) != 0 || !reflect.DeepEqual(args, append([]any{s.request.Binding.ExecutionID.String()}, s.head...)) || !strings.Contains(q, "capture_digest=$6") {
			return pgconn.CommandTag{}, errors.New("nonexact PV header delete")
		}
		s.order = append(s.order, "environment")
		s.head = nil
	default:
		return pgconn.CommandTag{}, errors.New("unrelated PV write")
	}
	s.deletes++
	return pgconn.NewCommandTag("DELETE 1"), nil
}

type executionVariableOwner struct {
	store                       *executionVariableStore
	request                     ec.ProjectExecutionRetirementRequest
	applied, reject             bool
	preparedCalls, appliedCalls int
}

func (o *executionVariableOwner) check(tx f.Tx, r ec.ProjectExecutionRetirementRequest, applied bool) error {
	if o.reject || o.applied != applied || tx != o.store.tx || r.Binding != o.request.Binding || r.Cause != o.request.Cause || !r.Actor.Equal(o.request.Actor) {
		return fault(f.Forbidden)
	}
	return nil
}
func (o *executionVariableOwner) RequireProjectExecutionRetirementPreparedInTx(_ context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest) error {
	o.preparedCalls++
	return o.check(tx, r, false)
}
func (o *executionVariableOwner) RequireProjectExecutionRetirementAppliedInTx(_ context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest) error {
	o.appliedCalls++
	return o.check(tx, r, true)
}
func (*executionVariableOwner) ProjectExecutionRetirementProofInTx(context.Context, f.Tx, ec.ProjectExecutionRetirementRequest) (ec.ProjectExecutionRetirementCommit, error) {
	return ec.ProjectExecutionRetirementCommit{}, fault(f.Forbidden)
}

type executionVariableLeasePlan struct {
	locks   []f.LockRequest
	binding f.Digest
}

func (p executionVariableLeasePlan) RequiredLocks() []f.LockRequest { return slices.Clone(p.locks) }
func (p executionVariableLeasePlan) ProjectExecutionLeaseRetirementDigest() f.Digest {
	return p.binding
}

type executionVariableLeases struct {
	authority    *ProjectExecutionRetirementAuthority
	store        *executionVariableStore
	request      sc.ProjectExecutionLeaseRetirementRequest
	contexts     [3]context.Context
	transactions [3]f.Tx
	digest       f.Digest
	retired      bool
	after        func(context.Context, f.Tx, sc.ProjectExecutionLeaseRetirementRequest, projectExecutionRetirementStage)
}

func (l *executionVariableLeases) check(ctx context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest, stage projectExecutionRetirementStage) error {
	checks := []func(context.Context, f.Tx, sc.ProjectExecutionLeaseRetirementRequest) error{l.authority.CheckProjectExecutionLeaseDiscoveryInTx, l.authority.CheckProjectExecutionLeaseRetirementInTx, l.authority.CheckProjectExecutionLeasesRetiredInTx}
	if err := checks[stage](ctx, tx, r); err != nil {
		return err
	}
	for n, check := range checks {
		if n != int(stage) && check(ctx, tx, r) == nil {
			return errors.New("lease witness authorized another stage")
		}
	}
	l.contexts[stage], l.transactions[stage], l.request = ctx, tx, r.Clone()
	if l.after != nil {
		l.after(ctx, tx, r, stage)
	}
	return nil
}
func (l *executionVariableLeases) DiscoverProjectExecutionLeasesInTx(ctx context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest) (sc.ProjectExecutionLeaseRetirementPlan, error) {
	if err := l.check(ctx, tx, r, projectExecutionDiscover); err != nil {
		return nil, err
	}
	locks := r.RequiredLocks()
	if len(r.References) > 0 {
		key, _ := f.AggregateLock(f.CredentialRefAggregate, testID[sc.Credential](7).String())
		locks = append(locks, f.LockRequest{Key: key, Mode: f.Exclusive})
	}
	return executionVariableLeasePlan{locks, l.digest}, nil
}
func (l *executionVariableLeases) RetireProjectExecutionLeasesInTx(ctx context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest, _ sc.ProjectExecutionLeaseRetirementPlan) error {
	if err := l.check(ctx, tx, r, projectExecutionRetire); err != nil {
		return err
	}
	if len(l.store.refs) != 0 || r.EnvironmentPresent && l.store.head == nil {
		return errors.New("wrong cross-owner deletion order")
	}
	l.store.order = append(l.store.order, "leases")
	l.retired = true
	return nil
}
func (l *executionVariableLeases) RequireProjectExecutionLeasesRetiredInTx(ctx context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest, _ sc.ProjectExecutionLeaseRetirementPlan) error {
	if err := l.check(ctx, tx, r, projectExecutionObserve); err != nil {
		return err
	}
	if !l.retired {
		return fault(f.ResourceBusy)
	}
	return nil
}

func executionVariableFixture(t *testing.T, absent bool) (*ProjectExecutionRetirementReferences, *executionVariableStore, *executionVariableOwner, *executionVariableLeases, ec.ProjectExecutionRetirementRequest) {
	t.Helper()
	capture, source, original, _ := newEnvironmentTest(t)
	if !absent {
		plan, err := capture.DiscoverExecutionEnvironment(context.Background(), original.r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = environmentTestFinal(capture, source, original.r, plan); err != nil {
			t.Fatal(err)
		}
	}
	op := testID[pc.Operation](30)
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(original.r.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(op.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	r := ec.ProjectExecutionRetirementRequest{Actor: actor, Cause: pc.LifecycleCause{OperationID: op, Action: pc.Delete, ProjectVersion: 9}, Binding: ec.ProjectExecutionRetirementBinding{ProjectID: original.r.ProjectID, AgentID: original.r.AgentID, ExecutionID: original.r.ExecutionID, Version: 7, CanonicalDigest: digest([]byte("complete-original-execution"))}}
	s := &executionVariableStore{environmentTestStore: source, request: r}
	s.tx = f.NewTx()
	o := &executionVariableOwner{store: s, request: r}
	base, err := NewAuthority(s)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewProjectExecutionRetirementAuthority(base, o)
	if err != nil {
		t.Fatal(err)
	}
	l := &executionVariableLeases{authority: authority, store: s, digest: digest([]byte("full-original-secret-lease-snapshot"))}
	s.locks = sc.ProjectExecutionLeaseRetirementRequest{ProjectID: r.Binding.ProjectID, AgentID: r.Binding.AgentID, ExecutionID: r.Binding.ExecutionID}.RequiredLocks()
	p, err := NewProjectExecutionRetirementReferences(authority, l)
	if err != nil {
		t.Fatal(err)
	}
	return p, s, o, l, r
}

func TestProjectExecutionVariablesRetireInOrderAndRequireApplied(t *testing.T) {
	for _, absent := range []bool{false, true} {
		name := "captured"
		if absent {
			name = "absent"
		}
		t.Run(name, func(t *testing.T) {
			p, s, o, l, r := executionVariableFixture(t, absent)
			before := s.writes
			plan, err := p.DiscoverProjectExecutionReferencesInTx(context.Background(), s.tx, r)
			if err != nil || plan.ProjectExecutionRetirementReferenceDigest().Validate() != nil || s.deletes != 0 {
				t.Fatal("readonly original discovery", err)
			}
			binding := plan.ProjectExecutionRetirementReferenceDigest()
			s.locks = plan.RequiredLocks()
			if err = p.RequireProjectExecutionReferencesRetiredInTx(context.Background(), s.tx, r, plan); err == nil {
				t.Fatal("Prepared replaced Applied")
			}
			if err = p.RetireProjectExecutionReferencesInTx(context.Background(), s.tx, r, plan); err != nil {
				t.Fatal("retire", err)
			}
			want := []string{"refs", "leases", "environment"}
			if absent {
				want = []string{"leases"}
			}
			if !slices.Equal(s.order, want) || s.writes != before || s.head != nil || len(s.refs) != 0 || plan.ProjectExecutionRetirementReferenceDigest() != binding {
				t.Fatal("wrong retirement order or mutated snapshot")
			}
			o.applied, s.tx = true, f.NewTx()
			writes := s.deletes
			for n := 0; n < 2; n++ {
				if err = p.RequireProjectExecutionReferencesRetiredInTx(context.Background(), s.tx, r, plan); err != nil {
					t.Fatal("Applied readonly confirmation", err)
				}
			}
			if err = p.RetireProjectExecutionReferencesInTx(context.Background(), s.tx, r, plan); err == nil || s.deletes != writes {
				t.Fatal("Applied authorized writes")
			}
			checks := []func(context.Context, f.Tx, sc.ProjectExecutionLeaseRetirementRequest) error{l.authority.CheckProjectExecutionLeaseDiscoveryInTx, l.authority.CheckProjectExecutionLeaseRetirementInTx, l.authority.CheckProjectExecutionLeasesRetiredInTx}
			for stage, check := range checks {
				if l.contexts[stage] == nil || check(l.contexts[stage], l.transactions[stage], l.request) == nil {
					t.Fatal("private callback scope escaped", stage)
				}
			}
		})
	}
}

func TestProjectExecutionVariablesRejectWrongScopeAndHistory(t *testing.T) {
	for _, mode := range []string{"owner", "phase", "tx", "held", "issuer", "binding", "cause", "cancel", "header", "refs", "missing-header", "missing-ref"} {
		t.Run(mode, func(t *testing.T) {
			p, s, o, _, r := executionVariableFixture(t, false)
			plan, err := p.DiscoverProjectExecutionReferencesInTx(context.Background(), s.tx, r)
			if err != nil {
				t.Fatal(err)
			}
			s.locks = plan.RequiredLocks()
			ctx, tx := context.Background(), s.tx
			switch mode {
			case "owner":
				o.reject = true
			case "phase":
				o.applied = true
			case "tx":
				tx = f.NewTx()
			case "held":
				s.locks = nil
			case "issuer":
				p, _ = NewProjectExecutionRetirementReferences(p.authority, p.leases)
			case "binding":
				r.Binding.CanonicalDigest = digest([]byte("another-original"))
			case "cause":
				r.Cause.ProjectVersion++
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "header":
				s.head[4] = string(digest([]byte("changed-capture")))
			case "refs":
				s.leases[0] = testID[sc.Lease](40).String()
			case "missing-header":
				s.head = nil
			case "missing-ref":
				s.refs, s.leases = nil, nil
			}
			if err = p.RetireProjectExecutionReferencesInTx(ctx, tx, r, plan); err == nil || s.deletes != 0 {
				t.Fatal("unproven or drifted retirement wrote", err)
			}
		})
	}
}

func TestProjectExecutionVariablesPrivateLeaseScopeBindsOriginalPair(t *testing.T) {
	p, s, _, l, r := executionVariableFixture(t, false)
	other, _, _, _, _ := executionVariableFixture(t, false)
	checks := 0
	l.after = func(ctx context.Context, tx f.Tx, request sc.ProjectExecutionLeaseRetirementRequest, stage projectExecutionRetirementStage) {
		if stage != projectExecutionDiscover {
			return
		}
		checks++
		wrong := request.Clone()
		wrong.CaptureDigest = digest([]byte("wrong-capture"))
		if l.authority.CheckProjectExecutionLeaseDiscoveryInTx(context.Background(), tx, request) == nil || l.authority.CheckProjectExecutionLeaseDiscoveryInTx(ctx, f.NewTx(), request) == nil || l.authority.CheckProjectExecutionLeaseDiscoveryInTx(ctx, tx, wrong) == nil || other.authority.CheckProjectExecutionLeaseDiscoveryInTx(ctx, tx, request) == nil {
			t.Fatal("borrowed scope escaped original pair/Tx/facts")
		}
	}
	if _, err := p.DiscoverProjectExecutionReferencesInTx(context.Background(), s.tx, r); err != nil || checks != 1 {
		t.Fatal("real discovery callback not exercised", err)
	}
}

func TestProjectExecutionVariablesDigestIncludesCaptureAndSecretSnapshot(t *testing.T) {
	p, s, _, l, r := executionVariableFixture(t, false)
	plan, err := p.DiscoverProjectExecutionReferencesInTx(context.Background(), s.tx, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"capture", "secret"} {
		oldHead, oldLease := s.head[4], l.digest
		if mode == "capture" {
			s.head[4] = string(digest([]byte("another-capture")))
		} else {
			l.digest = digest([]byte("credential-version-or-released-at-changed"))
		}
		changed, e := p.DiscoverProjectExecutionReferencesInTx(context.Background(), s.tx, r)
		if e != nil || changed.ProjectExecutionRetirementReferenceDigest() == plan.ProjectExecutionRetirementReferenceDigest() {
			t.Fatal("incomplete original digest", mode, e)
		}
		s.head[4], l.digest = oldHead, oldLease
	}
}
