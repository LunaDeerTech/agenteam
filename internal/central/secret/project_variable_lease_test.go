package secret

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SQL and Environment authorization below are controlled ports. This checks
// the real dedicated lease adapter, not real preparing authority or PG commit.
type environmentLeaseTestStore struct {
	*projectAuditUnitStore
	r          sc.ProjectVariableLeaseRequest
	version    int64
	purpose    sc.Purpose
	lease      string
	writes     int
	afterWrite func()
}

func (s *environmentLeaseTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.projectAuditUnitStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *environmentLeaseTestStore) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	s.queries++
	row := func(v ...any) postgres.Row { return projectAuditUnitRow{values: v} }
	switch {
	case strings.Contains(q, "FROM agenteam_secret.secrets"):
		if args[0] != s.r.Ref.Details().ID.String() || args[1] != s.r.ProjectID.String() {
			return projectAuditUnitRow{err: errors.New("wrong credential scope")}
		}
		return row(string(s.purpose), s.version, "01900000-0000-7000-8000-000000000099")
	case strings.Contains(q, "FROM agenteam_secret.secret_payloads"):
		if !strings.HasPrefix(q, "SELECT scope,") {
			return projectAuditUnitRow{err: errors.New("material read forbidden")}
		}
		return row("project", s.r.ProjectID.String(), int16(valueOwner), s.r.Ref.Details().ID.String())
	case strings.Contains(q, "FROM agenteam_secret.project_variable_execution_leases"):
		if args[0] != s.r.ExecutionID.String() || args[1] != s.r.Ref.Details().ID.String() {
			return projectAuditUnitRow{err: errors.New("wrong lease owner")}
		}
		if s.lease == "" {
			return projectAuditUnitRow{err: pgx.ErrNoRows}
		}
		return row(s.lease, s.r.ProjectID.String(), s.r.AgentID.String(), s.r.VariableID.String(), int64(s.r.VariableVersion), int64(s.r.CredentialVersion), int64(s.r.AgentVersion), string(s.r.AttemptBinding), false)
	default:
		return projectAuditUnitRow{err: errors.New("unexpected lease query")}
	}
}
func (s *environmentLeaseTestStore) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(q, "INSERT INTO agenteam_secret.project_variable_execution_leases") || len(args) != 10 {
		return pgconn.CommandTag{}, errors.New("unexpected lease write")
	}
	s.writes++
	s.lease = args[0].(string)
	if s.afterWrite != nil {
		s.afterWrite()
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type environmentLeaseTestOwner struct {
	store                 *environmentLeaseTestStore
	r                     sc.ProjectVariableLeaseRequest
	reject                error
	planCalls, finalCalls int
	entered, release      chan struct{}
}

func (o *environmentLeaseTestOwner) CheckProjectVariableLeasePlan(_ context.Context, r sc.ProjectVariableLeaseRequest) error {
	o.planCalls++
	if o.reject != nil {
		return o.reject
	}
	if !sameEnvironmentLease(r, o.r) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}
func (o *environmentLeaseTestOwner) CheckProjectVariableLeaseInTx(_ context.Context, tx f.Tx, r sc.ProjectVariableLeaseRequest) error {
	o.finalCalls++
	if o.entered != nil {
		close(o.entered)
		<-o.release
	}
	if o.reject != nil {
		return o.reject
	}
	if tx != o.store.tx || o.store.checks == 0 || !sameEnvironmentLease(r, o.r) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}
func environmentLeaseFixture(t *testing.T) (*ProjectVariableLeaseService, *environmentLeaseTestStore, *environmentLeaseTestOwner, sc.ProjectVariableLeaseRequest) {
	t.Helper()
	r := sc.ProjectVariableLeaseRequest{ProjectID: projectAuditID[i.Project](t), AgentID: projectAuditID[i.Agent](t), ExecutionID: projectAuditID[i.Execution](t), VariableID: projectAuditID[i.ProjectVariable](t), VariableVersion: 2, CredentialVersion: 3, AgentVersion: 4, AttemptBinding: f.Digest("sha256:" + strings.Repeat("a", 64))}
	scope, _ := i.InProject(r.ProjectID)
	r.Ref, _ = sc.NewCredentialRef(projectAuditID[sc.Credential](t), scope)
	s := &environmentLeaseTestStore{projectAuditUnitStore: &projectAuditUnitStore{tx: f.NewTx(), locks: r.RequiredLocks()}, r: r, version: 3, purpose: sc.ProjectVariable}
	owner := &environmentLeaseTestOwner{store: s, r: r}
	state := &serviceState{store: s, initialized: true}
	service := &Service{func() *serviceState { return state }}
	adapter, err := NewProjectVariableLeases(service, owner)
	if err != nil {
		t.Fatal(err)
	}
	return adapter, s, owner, r
}
func TestProjectVariableEnvironmentLeaseUsesDedicatedCurrentMetadata(t *testing.T) {
	a, s, o, r := environmentLeaseFixture(t)
	plan, err := a.DiscoverProjectVariableLease(context.Background(), r)
	if err != nil || s.queries != 0 {
		t.Fatal("planning did SQL or failed", err)
	}
	lease, err := a.AcquireProjectVariableLeaseInTx(context.Background(), s.tx, r, plan)
	if err != nil || lease.LeaseID.Validate() != nil || !lease.CredentialRef.Equal(r.Ref) || s.writes != 1 || o.finalCalls != 1 {
		t.Fatal("dedicated acquisition", err)
	}
	secondPlan, err := a.DiscoverProjectVariableLease(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.AcquireProjectVariableLeaseInTx(context.Background(), s.tx, r, secondPlan)
	if err != nil || second.LeaseID != lease.LeaseID || s.writes != 1 {
		t.Fatal("same original capture created another lease", err)
	}
	if sc.ProjectVariable.Valid() {
		t.Fatal("environment broadened legacy Purpose")
	}
	if _, err = loadLease(context.Background(), s, lease.LeaseID); err == nil {
		t.Fatal("dedicated lease reached legacy reader")
	}
	for _, value := range []any{plan, r} {
		raw, err := json.Marshal(value)
		if err != nil || strings.Contains(string(raw), r.Ref.Details().ID.String()) || strings.Contains(fmt.Sprintf("%#v", value), r.Ref.Details().ID.String()) {
			t.Fatal("private binding escaped")
		}
	}
}
func TestProjectVariableEnvironmentLeaseRejectsUnprovenAndJoinsOriginalCall(t *testing.T) {
	for _, name := range []string{"owner", "tx", "held", "issuer", "request", "version", "purpose", "cancel"} {
		t.Run(name, func(t *testing.T) {
			a, s, o, r := environmentLeaseFixture(t)
			plan, err := a.DiscoverProjectVariableLease(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			tx := s.tx
			switch name {
			case "owner":
				o.reject = f.NewFault(f.Forbidden, f.NotStarted)
			case "tx":
				tx = f.NewTx()
			case "held":
				s.locks = nil
			case "issuer":
				a, _ = NewProjectVariableLeases(a.state().service, o)
			case "request":
				r.AgentVersion++
			case "version":
				s.version++
			case "purpose":
				s.purpose = sc.Model
			case "cancel":
				run, cancel := context.WithCancel(ctx)
				cancel()
				ctx = run
			}
			lease, err := a.AcquireProjectVariableLeaseInTx(ctx, tx, r, plan)
			if err == nil || lease.LeaseID.Validate() == nil || s.writes != 0 {
				t.Fatal("invalid acquisition wrote/published")
			}
		})
	}
	a, s, o, r := environmentLeaseFixture(t)
	plan, err := a.DiscoverProjectVariableLease(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	o.entered, o.release = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		lease, e := a.AcquireProjectVariableLeaseInTx(ctx, s.tx, r, plan)
		if lease.LeaseID.Validate() == nil {
			done <- errors.New("cancelled lease published")
			return
		}
		done <- e
	}()
	<-o.entered
	cancel()
	select {
	case <-done:
		t.Fatal("returned before original callback")
	default:
	}
	close(o.release)
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) || s.writes != 0 {
			t.Fatal("lost cancellation/continued writes", err)
		}
	case <-time.After(time.Second):
		t.Fatal("callback not joined")
	}
	o.entered, o.release = nil, nil
	o.reject = fmt.Errorf("private-material-canary: %w", context.Canceled)
	if _, err = a.DiscoverProjectVariableLease(context.Background(), r); err != context.Canceled || strings.Contains(fmt.Sprintf("%+v", err), "canary") {
		t.Fatal("wrapped cancellation disclosed private text")
	}
	o.reject = nil
	ctx, cancel = context.WithCancel(context.Background())
	s.afterWrite = cancel
	lease, err := a.AcquireProjectVariableLeaseInTx(ctx, s.tx, r, plan)
	if !errors.Is(err, context.Canceled) || lease.LeaseID.Validate() == nil || s.writes != 1 {
		t.Fatal("write callback return not joined before cancellation result")
	}
}
