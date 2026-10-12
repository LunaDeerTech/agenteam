package secret

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// Real original acquisition/release writers produce the fixture's historical
// row. Only SQL persistence and the PV proof port are controlled here; this is
// not a PostgreSQL or original Execution consumer-return proof.
type executionLeaseRetirementStore struct {
	*projectAuditUnitStore
	rows                    []projectExecutionLeaseRow
	reads, updates, deletes int
	afterRelease            func()
}

func (s *executionLeaseRetirementStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.projectAuditUnitStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *executionLeaseRetirementStore) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	if !strings.HasPrefix(q, "SELECT COALESCE(jsonb_agg(to_jsonb(lease)") || len(args) != 1 {
		return projectAuditUnitRow{err: errors.New("unexpected retirement read")}
	}
	for _, row := range s.rows {
		if args[0] != row.Execution {
			return projectAuditUnitRow{err: errors.New("cross-execution read")}
		}
	}
	s.reads++
	rows := s.rows
	if rows == nil {
		rows = []projectExecutionLeaseRow{}
	}
	raw, err := json.Marshal(rows)
	return projectAuditUnitRow{values: []any{raw}, err: err}
}
func (s *executionLeaseRetirementStore) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if len(s.rows) != 1 {
		return pgconn.CommandTag{}, errors.New("unexpected row set for write")
	}
	row := s.rows[0]
	switch {
	case strings.HasPrefix(q, "UPDATE agenteam_secret.project_variable_execution_leases SET released=true"):
		if !reflect.DeepEqual(args, row.args()) || row.Released || !strings.Contains(q, "credential_version=$8") || !strings.Contains(q, "released_at IS NULL") {
			return pgconn.CommandTag{}, errors.New("nonexact release")
		}
		s.updates++
		at := time.Date(2026, 10, 12, 12, 0, 0, 123000, time.UTC)
		s.rows[0].Released, s.rows[0].ReleasedAt = true, &at
		if s.afterRelease != nil {
			s.afterRelease()
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	case strings.HasPrefix(q, "DELETE FROM agenteam_secret.project_variable_execution_leases"):
		if !row.Released || row.ReleasedAt == nil || !reflect.DeepEqual(args, append(row.args(), *row.ReleasedAt)) || !strings.Contains(q, "credential_version=$8") || !strings.Contains(q, "released_at=$11") {
			return pgconn.CommandTag{}, errors.New("nonexact delete")
		}
		s.deletes++
		s.rows = nil
		return pgconn.NewCommandTag("DELETE 1"), nil
	default:
		return pgconn.CommandTag{}, errors.New("unrelated Secret write")
	}
}

type executionLeaseRetirementOwner struct {
	store   *executionLeaseRetirementStore
	request sc.ProjectExecutionLeaseRetirementRequest
	stage   uint8
	reject  bool
}

func (o *executionLeaseRetirementOwner) check(tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest, stage uint8) error {
	if o.reject || stage != o.stage || tx != o.store.tx || !r.Equal(o.request) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}
func (o *executionLeaseRetirementOwner) CheckProjectExecutionLeaseDiscoveryInTx(_ context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest) error {
	return o.check(tx, r, 0)
}
func (o *executionLeaseRetirementOwner) CheckProjectExecutionLeaseRetirementInTx(_ context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest) error {
	return o.check(tx, r, 1)
}
func (o *executionLeaseRetirementOwner) CheckProjectExecutionLeasesRetiredInTx(_ context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest) error {
	return o.check(tx, r, 2)
}

func executionLeaseRetirementFixture(t *testing.T, mode string) (*ProjectExecutionLeaseRetirementService, *executionLeaseRetirementStore, *executionLeaseRetirementOwner, sc.ProjectExecutionLeaseRetirementRequest) {
	t.Helper()
	var service *Service
	var row *projectExecutionLeaseRow
	var r sc.ProjectExecutionLeaseRetirementRequest
	if mode == "absent" {
		capture, _, _, source := environmentLeaseFixture(t)
		service = capture.state().service
		r = sc.ProjectExecutionLeaseRetirementRequest{ProjectID: source.ProjectID, AgentID: source.AgentID, ExecutionID: source.ExecutionID}
	} else {
		old, source, _, request, _, _ := leaseRetirementFixture(t)
		if mode == "released" {
			plan, err := old.DiscoverProjectVariableLeaseRetirement(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			source.locks = plan.RequiredLocks()
			if err = old.RetireProjectVariableLeaseInTx(context.Background(), source.tx, request, plan); err != nil {
				t.Fatal(err)
			}
		}
		service = old.service
		lease, err := f.ParseID[sc.Lease](source.lease)
		if err != nil {
			t.Fatal(err)
		}
		r = sc.ProjectExecutionLeaseRetirementRequest{ProjectID: request.ProjectID, AgentID: request.AgentID, ExecutionID: request.ExecutionID, EnvironmentPresent: true, AgentVersion: request.AgentVersion, AttemptBinding: request.AttemptBinding, CaptureDigest: f.Digest("sha256:" + strings.Repeat("b", 64)), OrdinaryCount: 1, SecretCount: 1, References: []sc.ProjectExecutionLeaseReference{{VariableID: request.VariableID, LeaseID: lease}}}
		row = &projectExecutionLeaseRow{Lease: source.lease, Execution: request.ExecutionID.String(), Project: request.ProjectID.String(), Agent: request.AgentID.String(), Variable: request.VariableID.String(), VariableVersion: int64(request.VariableVersion), Credential: request.Ref.Details().ID.String(), CredentialVersion: int64(source.r.CredentialVersion), AgentVersion: int64(request.AgentVersion), Attempt: string(request.AttemptBinding), Released: source.released, ReleasedAt: source.at}
	}
	store := &executionLeaseRetirementStore{projectAuditUnitStore: &projectAuditUnitStore{tx: f.NewTx(), locks: r.RequiredLocks()}}
	if row != nil {
		store.rows = []projectExecutionLeaseRow{*row}
	}
	service.state().store = store
	owner := &executionLeaseRetirementOwner{store: store, request: r.Clone()}
	p, err := NewProjectExecutionLeaseRetirement(service, owner)
	if err != nil {
		t.Fatal(err)
	}
	return p, store, owner, r
}

func TestProjectExecutionLeaseRetirementReleaseAndAppliedConfirmation(t *testing.T) {
	for _, mode := range []string{"captured", "released", "absent"} {
		t.Run(mode, func(t *testing.T) {
			p, s, o, r := executionLeaseRetirementFixture(t, mode)
			plan, err := p.DiscoverProjectExecutionLeasesInTx(context.Background(), s.tx, r)
			if err != nil || plan.ProjectExecutionLeaseRetirementDigest().Validate() != nil || s.updates+s.deletes != 0 {
				t.Fatal("discovery", err)
			}
			before := plan.ProjectExecutionLeaseRetirementDigest()
			s.locks, o.stage = plan.RequiredLocks(), 1
			if err = p.RequireProjectExecutionLeasesRetiredInTx(context.Background(), s.tx, r, plan); err == nil {
				t.Fatal("Prepared authorized Applied observation")
			}
			if err = p.RetireProjectExecutionLeasesInTx(context.Background(), s.tx, r, plan); err != nil {
				t.Fatal("retire", err)
			}
			wantUpdates, wantDeletes := 0, 1
			if mode == "captured" {
				wantUpdates = 1
			}
			if mode == "absent" {
				wantDeletes = 0
			}
			if s.updates != wantUpdates || s.deletes != wantDeletes || len(s.rows) != 0 || plan.ProjectExecutionLeaseRetirementDigest() != before {
				t.Fatal("wrong release/delete or mutable original digest")
			}
			// A new read-only confirmation Tx uses the original Applied proof and
			// original plan; it does not require a fabricated local return receipt.
			s.tx, o.stage = f.NewTx(), 2
			for n := 0; n < 2; n++ {
				if err = p.RequireProjectExecutionLeasesRetiredInTx(context.Background(), s.tx, r, plan); err != nil {
					t.Fatal("Applied confirmation", err)
				}
			}
			if err = p.RetireProjectExecutionLeasesInTx(context.Background(), s.tx, r, plan); err == nil || s.updates != wantUpdates || s.deletes != wantDeletes {
				t.Fatal("Applied confirmation wrote")
			}
		})
	}
}

func TestProjectExecutionLeaseRetirementRejectsUnprovenAndChangedHistory(t *testing.T) {
	for _, mode := range []string{"owner", "phase", "foreign-tx", "held", "issuer", "request", "cancel", "credential-version", "variable-version", "agent-version", "attempt", "row-project", "released-at", "missing", "extra"} {
		t.Run(mode, func(t *testing.T) {
			p, s, o, r := executionLeaseRetirementFixture(t, "captured")
			plan, err := p.DiscoverProjectExecutionLeasesInTx(context.Background(), s.tx, r)
			if err != nil {
				t.Fatal(err)
			}
			s.locks, o.stage = plan.RequiredLocks(), 1
			ctx, tx := context.Background(), s.tx
			switch mode {
			case "owner":
				o.reject = true
			case "phase":
				o.stage = 2
			case "foreign-tx":
				tx = f.NewTx()
			case "held":
				s.locks = nil
			case "issuer":
				p, _ = NewProjectExecutionLeaseRetirement(p.service, o)
			case "request":
				r.CaptureDigest = f.Digest("sha256:" + strings.Repeat("c", 64))
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "credential-version":
				s.rows[0].CredentialVersion++
			case "variable-version":
				s.rows[0].VariableVersion++
			case "agent-version":
				s.rows[0].AgentVersion++
			case "attempt":
				s.rows[0].Attempt = "sha256:" + strings.Repeat("c", 64)
			case "row-project":
				s.rows[0].Project = "another-project"
			case "released-at":
				at := time.Now()
				s.rows[0].ReleasedAt = &at
			case "missing":
				s.rows = nil
			case "extra":
				s.rows = append(s.rows, s.rows[0])
			}
			if err = p.RetireProjectExecutionLeasesInTx(ctx, tx, r, plan); err == nil || s.updates+s.deletes != 0 {
				t.Fatal("unproven or changed history wrote", err)
			}
		})
	}
}

func TestProjectExecutionLeaseRetirementRechecksAfterRelease(t *testing.T) {
	for _, mode := range []string{"version", "timestamp", "extra"} {
		t.Run(mode, func(t *testing.T) {
			p, s, o, r := executionLeaseRetirementFixture(t, "captured")
			plan, err := p.DiscoverProjectExecutionLeasesInTx(context.Background(), s.tx, r)
			if err != nil {
				t.Fatal(err)
			}
			s.locks, o.stage = plan.RequiredLocks(), 1
			s.afterRelease = func() {
				switch mode {
				case "version":
					s.rows[0].CredentialVersion++
				case "timestamp":
					s.rows[0].ReleasedAt = nil
				case "extra":
					s.rows = append(s.rows, s.rows[0])
				}
			}
			if err = p.RetireProjectExecutionLeasesInTx(context.Background(), s.tx, r, plan); err == nil || s.updates != 1 || s.deletes != 0 {
				t.Fatal("unverified release deleted", err)
			}
		})
	}
}

func TestProjectExecutionLeaseRetirementDigestIncludesHistoricalTuple(t *testing.T) {
	p, s, _, r := executionLeaseRetirementFixture(t, "released")
	baseline, err := p.DiscoverProjectExecutionLeasesInTx(context.Background(), s.tx, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*projectExecutionLeaseRow){func(v *projectExecutionLeaseRow) { v.CredentialVersion++ }, func(v *projectExecutionLeaseRow) { v.VariableVersion++ }, func(v *projectExecutionLeaseRow) { at := v.ReleasedAt.Add(time.Microsecond); v.ReleasedAt = &at }} {
		old := s.rows[0]
		change(&s.rows[0])
		changed, e := p.DiscoverProjectExecutionLeasesInTx(context.Background(), s.tx, r)
		if e != nil || changed.ProjectExecutionLeaseRetirementDigest() == baseline.ProjectExecutionLeaseRetirementDigest() {
			t.Fatal("historical change absent from digest", e)
		}
		s.rows[0] = old
	}
	// Same instant represented with another offset is the same frozen fact.
	at := s.rows[0].ReleasedAt.In(time.FixedZone("other", 3600))
	s.rows[0].ReleasedAt = &at
	same, e := p.DiscoverProjectExecutionLeasesInTx(context.Background(), s.tx, r)
	if e != nil || same.ProjectExecutionLeaseRetirementDigest() != baseline.ProjectExecutionLeaseRetirementDigest() {
		t.Fatal("digest depends on SQL session timezone", e)
	}
	// A lease hidden behind an absent PV capture is a mismatch, never success.
	absent := sc.ProjectExecutionLeaseRetirementRequest{ProjectID: r.ProjectID, AgentID: r.AgentID, ExecutionID: r.ExecutionID}
	if validateProjectExecutionLeases(absent, s.rows) == nil {
		t.Fatal("absent environment concealed lease")
	}
}
