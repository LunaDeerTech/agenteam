package projectvariable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Controlled SQL and Execution facts isolate the provider's ordering, binding
// and original call lifetime. These are not real preparing proofs or PG tests.
type environmentTestStore struct {
	*secretAuthorityStore
	ordinary, secret secretControlRow
	secretRows       map[string]secretControlRow
	ids              []string
	index            []secretReferenceIndexEntry
	head             []any
	refs, leases     []string
	writes, acquires int
	afterRead        func()
}

func (s *environmentTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, e := s.secretAuthorityStore.InTx(tx); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *environmentTestStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.acquires++
	return s.secretAuthorityStore.AcquireAll(ctx, tx, locks)
}
func (s *environmentTestStore) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	s.queries++
	if s.afterRead != nil {
		s.afterRead()
	}
	switch {
	case strings.Contains(q, "AS directory"):
		return secretControlRow{values: []any{slices.Clone(s.ids)}}
	case strings.Contains(q, "AS owned"):
		ids, versions := []string{}, []int64{}
		for _, entry := range s.index {
			ids = append(ids, entry.ID.String())
			versions = append(versions, int64(entry.Version))
		}
		return secretControlRow{values: []any{ids, versions}}
	case strings.Contains(q, "type='variable'"):
		return s.ordinary
	case strings.Contains(q, "type='secret'"):
		if s.secretRows != nil {
			return s.secretRows[args[1].(string)]
		}
		return s.secret
	case strings.Contains(q, "FROM agenteam_projectvariable.execution_environments"):
		if s.head == nil {
			return secretControlRow{err: pgx.ErrNoRows}
		}
		return secretControlRow{values: s.head}
	case strings.Contains(q, "FROM agenteam_projectvariable.execution_secret_references"):
		return secretControlRow{values: []any{s.refs, s.leases}}
	default:
		return secretControlRow{err: errors.New("unexpected environment SQL")}
	}
}
func (s *environmentTestStore) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	s.writes++
	switch {
	case strings.Contains(q, "INSERT INTO agenteam_projectvariable.execution_environments"):
		s.head = slices.Clone(args[1:])
	case strings.Contains(q, "INSERT INTO agenteam_projectvariable.execution_secret_references"):
		s.refs = append(s.refs, args[1].(string))
		s.leases = append(s.leases, args[2].(string))
	default:
		return pgconn.CommandTag{}, errors.New("unexpected environment write")
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type environmentTestOwner struct {
	store                      *environmentTestStore
	r                          c.EnvironmentCaptureRequest
	d                          c.EnvironmentDiscoveryFacts
	version                    f.Version
	allowed                    []i.ProjectVariableID
	denyDiscovery, denyFinal   bool
	discoveryCalls, finalCalls int
	entered, release           chan struct{}
}

func (o *environmentTestOwner) RequireEnvironmentDiscoveryInTx(_ context.Context, tx f.Tx, r c.EnvironmentCaptureRequest) (c.EnvironmentDiscoveryFacts, error) {
	o.discoveryCalls++
	if o.entered != nil {
		close(o.entered)
		<-o.release
	}
	if o.denyDiscovery || tx != o.store.tx || r != o.r {
		return c.EnvironmentDiscoveryFacts{}, fault(f.Forbidden)
	}
	return o.d.Clone(), nil
}
func (o *environmentTestOwner) RequireEnvironmentCaptureInTx(_ context.Context, tx f.Tx, r c.EnvironmentCaptureRequest) (c.EnvironmentCaptureFacts, error) {
	o.finalCalls++
	if o.denyFinal || tx != o.store.tx || r != o.r {
		return c.EnvironmentCaptureFacts{}, fault(f.Forbidden)
	}
	return c.EnvironmentCaptureFacts{EnvironmentDiscoveryFacts: o.d.Clone(), AgentVersion: o.version, AllowedSecretVariableIDs: slices.Clone(o.allowed)}, nil
}

type environmentTestLeasePlan struct{ locks []f.LockRequest }

func (p environmentTestLeasePlan) RequiredLocks() []f.LockRequest { return slices.Clone(p.locks) }

type environmentTestLeases struct {
	owner             *EnvironmentAuthority
	planned, applied  int
	planCtx, finalCtx context.Context
	request           sc.ProjectVariableLeaseRequest
	tx                f.Tx
}

func (l *environmentTestLeases) DiscoverProjectVariableLease(ctx context.Context, r sc.ProjectVariableLeaseRequest) (sc.ProjectVariableLeasePlan, error) {
	if err := l.owner.CheckProjectVariableLeasePlan(ctx, r); err != nil {
		return nil, err
	}
	l.planned++
	l.planCtx, l.request = ctx, r
	return environmentTestLeasePlan{r.RequiredLocks()}, nil
}
func (l *environmentTestLeases) AcquireProjectVariableLeaseInTx(ctx context.Context, tx f.Tx, r sc.ProjectVariableLeaseRequest, _ sc.ProjectVariableLeasePlan) (sc.CredentialLease, error) {
	if err := l.owner.CheckProjectVariableLeaseInTx(ctx, tx, r); err != nil {
		return sc.CredentialLease{}, err
	}
	l.applied++
	l.finalCtx, l.tx = ctx, tx
	id, _ := f.ParseID[sc.Lease](r.VariableID.String())
	return sc.CredentialLease{LeaseID: id, CredentialRef: r.Ref}, nil
}
func newEnvironmentTest(t *testing.T) (*ExecutionEnvironmentProvider, *environmentTestStore, *environmentTestOwner, *environmentTestLeases) {
	t.Helper()
	at, _ := f.ParseInstant("2026-10-09T10:00:00Z")
	r := c.EnvironmentCaptureRequest{ProjectID: testID[i.Project](2), AgentID: testID[i.Agent](3), ExecutionID: testID[i.Execution](4)}
	ordinary, secret, credential := testID[i.ProjectVariable](5), testID[i.ProjectVariable](6), testID[sc.Credential](7).String()
	cv := int64(2)
	s := &environmentTestStore{secretAuthorityStore: &secretAuthorityStore{}, ids: []string{ordinary.String()}, index: []secretReferenceIndexEntry{{ID: secret, Version: 3}}}
	s.ordinary = secretControlRow{values: []any{ordinary.String(), r.ProjectID.String(), c.VariableType, "ORDINARY", "ordinary description", "ordinary-private-canary", int64(1), at.Time(), at.Time(), (*time.Time)(nil)}}
	s.secret = secretControlRow{values: []any{secret.String(), r.ProjectID.String(), c.SecretVariableType, "SECRET", "metadata description", int64(2), at.Time(), at.Time(), (*time.Time)(nil), &credential, &cv}}
	o := &environmentTestOwner{store: s, r: r, version: 3, allowed: []i.ProjectVariableID{secret}, d: c.EnvironmentDiscoveryFacts{Project: pc.ProjectRef{ID: r.ProjectID, OwnerUserID: testID[i.User](1), Name: "Project", NormalizedName: "project", Lifecycle: pc.Active, Version: 1, CreatedAt: at, UpdatedAt: at}, AttemptBinding: digest([]byte("actual-attempt-binding-control")), ScopeConstraints: []json.RawMessage{}}}
	base, e := NewAuthority(s)
	if e != nil {
		t.Fatal(e)
	}
	authority, e := NewEnvironmentAuthority(base, o)
	if e != nil {
		t.Fatal(e)
	}
	l := &environmentTestLeases{owner: authority}
	p, e := NewExecutionEnvironment(authority, l)
	if e != nil {
		t.Fatal(e)
	}
	return p, s, o, l
}
func environmentTestFinal(p *ExecutionEnvironmentProvider, s *environmentTestStore, r c.EnvironmentCaptureRequest, plan c.EnvironmentCapturePlan) (c.EnvironmentCapture, error) {
	s.tx, s.locks = f.NewTx(), plan.RequiredLocks()
	defer func() { s.tx, s.locks = f.Tx{}, nil }()
	return p.ResolveExecutionEnvironmentInTx(context.Background(), s.tx, r, plan)
}
func TestExecutionEnvironmentCapturesValuesAndStableSecretReferences(t *testing.T) {
	p, s, o, l := newEnvironmentTest(t)
	plan, err := p.DiscoverExecutionEnvironment(context.Background(), o.r)
	if err != nil {
		t.Fatal(err)
	}
	if l.planned != 1 || l.owner.CheckProjectVariableLeasePlan(l.planCtx, l.request) == nil {
		t.Fatal("planning witness survived original call")
	}
	v, err := environmentTestFinal(p, s, o.r, plan)
	if err != nil {
		t.Fatal(err)
	}
	d := v.Fields()
	if v.Validate() != nil || len(d.Variables) != 1 || len(d.Secrets) != 1 || d.Variables[0].Fields().Value != "ordinary-private-canary" || d.Secrets[0].Variable.Fields().Name != "SECRET" || l.applied != 1 || s.writes != 2 || s.acquires != 1 {
		t.Fatal("capture lost metadata or opened/acquired a final transaction")
	}
	if l.owner.CheckProjectVariableLeaseInTx(l.finalCtx, l.tx, l.request) == nil {
		t.Fatal("final witness survived original call")
	}
	for _, v := range []any{v, plan, l.request} {
		raw, e := json.Marshal(v)
		if e != nil || strings.Contains(string(raw), "ordinary-private-canary") || strings.Contains(fmt.Sprintf("%+v", v), "ordinary-private-canary") {
			t.Fatal("default formatting leaked capture")
		}
	}
	d.Variables[0] = c.Variable{}
	d.Secrets[0] = c.ExecutionSecretVariable{}
	if v.Clone().Validate() != nil {
		t.Fatal("mutable result")
	}
	before := s.writes
	if _, err = environmentTestFinal(p, s, o.r, plan); err != nil || s.writes != before {
		t.Fatal("same capture replay rewrote references", err)
	}
}
func TestExecutionEnvironmentRejectsUnprovenAndChangedFacts(t *testing.T) {
	for _, which := range []string{"discovery", "final", "reference-version", "allowed", "ordinary", "credential", "attempt", "scope", "held", "tx", "issuer"} {
		t.Run(which, func(t *testing.T) {
			p, s, o, l := newEnvironmentTest(t)
			if which == "discovery" {
				o.denyDiscovery = true
				if plan, err := p.DiscoverExecutionEnvironment(context.Background(), o.r); err == nil || plan != nil || s.queries != 0 {
					t.Fatal("unproven discovery")
				}
				return
			}
			plan, err := p.DiscoverExecutionEnvironment(context.Background(), o.r)
			if err != nil {
				t.Fatal(err)
			}
			switch which {
			case "final":
				o.denyFinal = true
			case "reference-version":
				s.index[0].Version++
			case "allowed":
				o.allowed = []i.ProjectVariableID{}
			case "ordinary":
				s.ordinary.values[5] = "changed"
			case "credential":
				v := int64(3)
				s.secret.values[10] = &v
			case "attempt":
				o.d.AttemptBinding = digest([]byte("changed"))
			case "scope":
				o.d.ScopeConstraints = []json.RawMessage{json.RawMessage(`{}`)}
			case "issuer":
				p, _ = NewExecutionEnvironment(l.owner, l)
			}
			s.tx, s.locks = f.NewTx(), plan.RequiredLocks()
			tx := s.tx
			if which == "held" {
				s.locks = nil
			}
			if which == "tx" {
				tx = f.NewTx()
			}
			out, err := p.ResolveExecutionEnvironmentInTx(context.Background(), tx, o.r, plan)
			if err == nil || out.Validate() == nil || s.writes != 0 || l.applied != 0 {
				t.Fatal("changed or unproven capture wrote references")
			}
		})
	}
}
func TestExecutionEnvironmentEmptyStillRequiresRealOwner(t *testing.T) {
	p, s, o, l := newEnvironmentTest(t)
	s.ids = []string{}
	s.index = nil
	o.allowed = []i.ProjectVariableID{}
	plan, err := p.DiscoverExecutionEnvironment(context.Background(), o.r)
	if err != nil {
		t.Fatal(err)
	}
	out, err := environmentTestFinal(p, s, o.r, plan)
	if err != nil || out.Validate() != nil || len(out.Fields().Secrets) != 0 || o.finalCalls != 1 || l.applied != 0 || s.writes != 1 {
		t.Fatal("empty set bypassed owner/head", err)
	}
	o.denyFinal = true
	if _, err = environmentTestFinal(p, s, o.r, plan); err == nil {
		t.Fatal("empty set bypass")
	}
	if _, err = NewExecutionEnvironment(l.owner, (*environmentTestLeases)(nil)); err == nil {
		t.Fatal("nil lease dependency")
	}
	p, s, o, _ = newEnvironmentTest(t)
	s.ids = make([]string, c.MaxVariables+1)
	if plan, err = p.DiscoverExecutionEnvironment(context.Background(), o.r); err == nil || plan != nil {
		t.Fatal("oversize directory")
	}
	// The full legal 256-ID capability must fit the real unique lock union,
	// despite every dedicated lease plan repeating the same four scope locks.
	p, s, o, l = newEnvironmentTest(t)
	s.ids, s.index, o.allowed = []string{}, nil, []i.ProjectVariableID{}
	s.secretRows = make(map[string]secretControlRow)
	for n := 0; n < c.MaxSecretReferences; n++ {
		id := testID[i.ProjectVariable](1000 + n)
		credential := testID[sc.Credential](2000 + n).String()
		row := secretControlRow{values: slices.Clone(s.secret.values)}
		row.values[0], row.values[3], row.values[9] = id.String(), fmt.Sprintf("SECRET_%03d", n), &credential
		s.secretRows[id.String()] = row
		s.index = append(s.index, secretReferenceIndexEntry{ID: id, Version: o.version})
		o.allowed = append(o.allowed, id)
	}
	plan, err = p.DiscoverExecutionEnvironment(context.Background(), o.r)
	if err != nil || len(plan.RequiredLocks()) != 260 || l.planned != 256 {
		t.Fatal("legal full capability exceeded unique lock budget", err)
	}
	out, err = environmentTestFinal(p, s, o.r, plan)
	if err != nil || out.Validate() != nil || len(out.Fields().Secrets) != 256 || l.applied != 256 {
		t.Fatal("full legal capture lost an entry", err)
	}
}
func TestExecutionEnvironmentOriginalCallJoinsAndUnknownKeepsNoPlan(t *testing.T) {
	p, s, o, _ := newEnvironmentTest(t)
	s.unknown = true
	plan, err := p.DiscoverExecutionEnvironment(context.Background(), o.r)
	var problem *f.Fault
	if plan != nil || !errors.As(err, &problem) || problem.Code != f.CommitUnknown || problem.CauseID == "" {
		t.Fatal("unknown published a plan")
	}
	s.unknown = false
	o.entered, o.release = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		p, e := p.DiscoverExecutionEnvironment(ctx, o.r)
		if p != nil {
			done <- errors.New("cancelled call published plan")
			return
		}
		done <- e
	}()
	<-o.entered
	cancel()
	select {
	case <-done:
		t.Fatal("returned before original owner call")
	default:
	}
	close(o.release)
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("lost original cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("original call not joined")
	}
	o.entered, o.release = nil, nil
	plan, err = p.DiscoverExecutionEnvironment(context.Background(), o.r)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	s.tx, s.locks = f.NewTx(), plan.RequiredLocks()
	s.afterRead = cancel
	value, err := p.ResolveExecutionEnvironmentInTx(ctx, s.tx, o.r, plan)
	if !errors.Is(err, context.Canceled) || value.Validate() == nil || s.writes != 0 {
		t.Fatal("cancelled read published/wrote")
	}
}
