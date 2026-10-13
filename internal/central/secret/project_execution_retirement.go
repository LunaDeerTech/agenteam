package secret

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// ProjectExecutionLeaseRetirementService only retires dedicated environment
// retention rows. It never reads credential material or deletes credentials.
type ProjectExecutionLeaseRetirementService struct {
	service *Service
	owner   sc.ProjectExecutionLeaseRetirementAuthority
}

func NewProjectExecutionLeaseRetirement(service *Service, owner sc.ProjectExecutionLeaseRetirementAuthority) (*ProjectExecutionLeaseRetirementService, error) {
	if service == nil || service.data == nil || service.state() == nil || nilPort(owner) {
		return nil, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	return &ProjectExecutionLeaseRetirementService{service, owner}, nil
}

type projectExecutionLeaseRow struct {
	Lease             string     `json:"lease_id"`
	Execution         string     `json:"execution_id"`
	Project           string     `json:"project_id"`
	Agent             string     `json:"agent_id"`
	Variable          string     `json:"variable_id"`
	VariableVersion   int64      `json:"variable_version"`
	Credential        string     `json:"credential_id"`
	CredentialVersion int64      `json:"credential_version"`
	AgentVersion      int64      `json:"agent_version"`
	Attempt           string     `json:"attempt_binding"`
	Released          bool       `json:"released"`
	ReleasedAt        *time.Time `json:"released_at"`
}

func sameProjectExecutionLease(a, b projectExecutionLeaseRow) bool {
	at, bt := a.ReleasedAt, b.ReleasedAt
	a.ReleasedAt, b.ReleasedAt = nil, nil
	return a == b && (at == nil && bt == nil || at != nil && bt != nil && at.Equal(*bt))
}
func (r projectExecutionLeaseRow) args() []any {
	return []any{r.Lease, r.Execution, r.Project, r.Agent, r.Variable, r.VariableVersion, r.Credential, r.CredentialVersion, r.AgentVersion, r.Attempt}
}

type projectExecutionLeasePlan struct {
	issuer  *ProjectExecutionLeaseRetirementService
	request sc.ProjectExecutionLeaseRetirementRequest
	rows    []projectExecutionLeaseRow
	locks   []f.LockRequest
	binding f.Digest
}

func (p *projectExecutionLeasePlan) ProjectExecutionLeaseRetirementDigest() f.Digest {
	if p == nil {
		return ""
	}
	return p.binding
}
func (p *projectExecutionLeasePlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (*projectExecutionLeasePlan) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_execution_lease_plan"))
}
func (*projectExecutionLeasePlan) LogValue() slog.Value {
	return slog.StringValue("project_execution_lease_plan")
}
func (*projectExecutionLeasePlan) MarshalJSON() ([]byte, error) {
	return []byte(`"project_execution_lease_plan"`), nil
}

func (s *ProjectExecutionLeaseRetirementService) authorize(ctx context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest, locks []f.LockRequest, stage uint8) (postgres.SQLExecutor, error) {
	if err := environmentLeaseContext(ctx); err != nil {
		return nil, err
	}
	if r.Validate() != nil {
		return nil, invalid()
	}
	if s == nil || s.service == nil || s.service.data == nil || s.service.state() == nil || nilPort(s.owner) {
		return nil, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	store := s.service.state().store
	x, err := store.InTx(tx)
	if err != nil || nilPort(x) {
		return nil, unavailable(err)
	}
	if err = store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, environmentLeaseError(err)
	}
	switch stage {
	case 0:
		err = s.owner.CheckProjectExecutionLeaseDiscoveryInTx(ctx, tx, r)
	case 1:
		err = s.owner.CheckProjectExecutionLeaseRetirementInTx(ctx, tx, r)
	case 2:
		err = s.owner.CheckProjectExecutionLeasesRetiredInTx(ctx, tx, r)
	default:
		return nil, invalid()
	}
	if canceled := environmentLeaseContext(ctx); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return nil, environmentLeaseError(err)
	}
	return x, nil
}

// One bounded statement freezes the complete historical row set, including
// credential versions. Current credential/variable configuration is irrelevant.
func readProjectExecutionLeases(ctx context.Context, x postgres.SQLExecutor, execution string) ([]projectExecutionLeaseRow, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(lease) ORDER BY lease.variable_id),'[]'::jsonb) FROM (SELECT lease_id::text,execution_id,project_id,agent_id,variable_id::text,variable_version,credential_id::text,credential_version,agent_version,attempt_binding,released,released_at FROM agenteam_secret.project_variable_execution_leases WHERE execution_id=$1 ORDER BY variable_id LIMIT 257) lease`, execution).Scan(&raw)
	if canceled := environmentLeaseContext(ctx); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return nil, unavailable(err)
	}
	var rows []projectExecutionLeaseRow
	if err = json.Unmarshal(raw, &rows); err != nil || rows == nil || len(rows) > 256 {
		return nil, unavailable(err)
	}
	for n := range rows {
		if rows[n].ReleasedAt != nil {
			at := rows[n].ReleasedAt.UTC()
			rows[n].ReleasedAt = &at
		}
	}
	return rows, nil
}
func validateProjectExecutionLeases(r sc.ProjectExecutionLeaseRetirementRequest, rows []projectExecutionLeaseRow) error {
	if len(rows) != len(r.References) {
		return failure(Conflict, f.VersionConflict, nil)
	}
	credentials := make(map[string]bool, len(rows))
	for n, row := range rows {
		ref := r.References[n]
		credential, err := f.ParseID[sc.Credential](row.Credential)
		if err != nil || credential.String() != row.Credential || credentials[row.Credential] || row.Execution != r.ExecutionID.String() || row.Project != r.ProjectID.String() || row.Agent != r.AgentID.String() || row.Lease != ref.LeaseID.String() || row.Variable != ref.VariableID.String() || row.VariableVersion < 1 || row.CredentialVersion < 1 || row.AgentVersion != int64(r.AgentVersion) || row.Attempt != string(r.AttemptBinding) {
			return failure(Conflict, f.VersionConflict, nil)
		}
		if row.Released != (row.ReleasedAt != nil) || row.ReleasedAt != nil && (row.ReleasedAt.IsZero() || row.ReleasedAt.Nanosecond()%1000 != 0) {
			return unavailable(nil)
		}
		credentials[row.Credential] = true
	}
	return nil
}
func (s *ProjectExecutionLeaseRetirementService) DiscoverProjectExecutionLeasesInTx(ctx context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest) (sc.ProjectExecutionLeaseRetirementPlan, error) {
	x, err := s.authorize(ctx, tx, r, r.RequiredLocks(), 0)
	if err != nil {
		return nil, err
	}
	rows, err := readProjectExecutionLeases(ctx, x, r.ExecutionID.String())
	if err != nil {
		return nil, err
	}
	if err = validateProjectExecutionLeases(r, rows); err != nil {
		return nil, err
	}
	locks := r.RequiredLocks()
	for _, row := range rows {
		key, _ := f.AggregateLock(f.CredentialRefAggregate, row.Credential)
		locks = append(locks, f.LockRequest{Key: key, Mode: f.Exclusive})
	}
	locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return nil, invalid()
	}
	binding, err := projectExecutionLeaseDigest(r, rows, locks)
	if err != nil {
		return nil, err
	}
	return &projectExecutionLeasePlan{issuer: s, request: r.Clone(), rows: rows, locks: locks, binding: binding}, nil
}

func (s *ProjectExecutionLeaseRetirementService) plan(r sc.ProjectExecutionLeaseRetirementRequest, p sc.ProjectExecutionLeaseRetirementPlan) (*projectExecutionLeasePlan, error) {
	v, ok := p.(*projectExecutionLeasePlan)
	if !ok || v == nil || v.issuer != s || !v.request.Equal(r) {
		return nil, invalid()
	}
	return v, nil
}

func (s *ProjectExecutionLeaseRetirementService) RetireProjectExecutionLeasesInTx(ctx context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest, plan sc.ProjectExecutionLeaseRetirementPlan) error {
	p, err := s.plan(r, plan)
	if err != nil {
		return err
	}
	x, err := s.authorize(ctx, tx, r, p.locks, 1)
	if err != nil {
		return err
	}
	rows, err := readProjectExecutionLeases(ctx, x, r.ExecutionID.String())
	if err != nil {
		return err
	}
	if !slices.EqualFunc(rows, p.rows, sameProjectExecutionLease) {
		return failure(Conflict, f.VersionConflict, nil)
	}
	if len(rows) > 0 {
		if err = s.service.writable(); err != nil {
			return err
		}
	}
	// The private writing scope proves the original consumers returned. Preserve
	// the existing monotonic release guard before deleting any Secret lease.
	for _, row := range rows {
		if row.Released {
			continue
		}
		if err = environmentLeaseContext(ctx); err != nil {
			return err
		}
		tag, writeErr := x.Exec(ctx, `UPDATE agenteam_secret.project_variable_execution_leases SET released=true,released_at=clock_timestamp() WHERE lease_id=$1 AND execution_id=$2 AND project_id=$3 AND agent_id=$4 AND variable_id=$5 AND variable_version=$6 AND credential_id=$7 AND credential_version=$8 AND agent_version=$9 AND attempt_binding=$10 AND NOT released AND released_at IS NULL`, row.args()...)
		if canceled := environmentLeaseContext(ctx); canceled != nil {
			return canceled
		}
		if writeErr != nil || tag.RowsAffected() != 1 {
			return unavailable(writeErr)
		}
	}
	released, err := readProjectExecutionLeases(ctx, x, r.ExecutionID.String())
	if err != nil {
		return err
	}
	if err = validateProjectExecutionLeases(r, released); err != nil {
		return err
	}
	for n, row := range released {
		before := rows[n]
		if !row.Released {
			return unavailable(nil)
		}
		if !before.Released {
			before.Released, before.ReleasedAt = true, row.ReleasedAt
		}
		if !sameProjectExecutionLease(before, row) {
			return failure(Conflict, f.VersionConflict, nil)
		}
	}
	for _, row := range released {
		if err = environmentLeaseContext(ctx); err != nil {
			return err
		}
		args := append(row.args(), *row.ReleasedAt)
		tag, writeErr := x.Exec(ctx, `DELETE FROM agenteam_secret.project_variable_execution_leases WHERE lease_id=$1 AND execution_id=$2 AND project_id=$3 AND agent_id=$4 AND variable_id=$5 AND variable_version=$6 AND credential_id=$7 AND credential_version=$8 AND agent_version=$9 AND attempt_binding=$10 AND released AND released_at=$11`, args...)
		if canceled := environmentLeaseContext(ctx); canceled != nil {
			return canceled
		}
		if writeErr != nil || tag.RowsAffected() != 1 {
			return unavailable(writeErr)
		}
	}
	return nil
}
func (s *ProjectExecutionLeaseRetirementService) RequireProjectExecutionLeasesRetiredInTx(ctx context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest, plan sc.ProjectExecutionLeaseRetirementPlan) error {
	p, err := s.plan(r, plan)
	if err != nil {
		return err
	}
	x, err := s.authorize(ctx, tx, r, p.locks, 2)
	if err != nil {
		return err
	}
	rows, err := readProjectExecutionLeases(ctx, x, r.ExecutionID.String())
	if err != nil {
		return err
	}
	if len(rows) != 0 {
		return failure(Conflict, f.ResourceBusy, nil)
	}
	return nil
}

var _ sc.ProjectExecutionLeaseRetirement = (*ProjectExecutionLeaseRetirementService)(nil)

// Identity for read-only NotApplied comparison, never an authorization token.
func projectExecutionLeaseDigest(r sc.ProjectExecutionLeaseRetirementRequest, rows []projectExecutionLeaseRow, locks []f.LockRequest) (f.Digest, error) {
	// Optional absent-capture scalars intentionally use primitive wire types;
	// Version/Digest MarshalJSON correctly reject their zero domain values.
	type request struct {
		Project, Agent, Execution string
		Present                   bool
		AgentVersion              int64
		Attempt, Capture          string
		Ordinary, Secrets         int
		References                []sc.ProjectExecutionLeaseReference
	}
	r0 := r
	facts := request{r0.ProjectID.String(), r0.AgentID.String(), r0.ExecutionID.String(), r0.EnvironmentPresent, int64(r0.AgentVersion), string(r0.AttemptBinding), string(r0.CaptureDigest), r0.OrdinaryCount, r0.SecretCount, r0.References}
	type lock struct {
		Key  string
		Mode f.LockMode
	}
	keys := make([]lock, len(locks))
	for n, l := range locks {
		keys[n] = lock{l.Key.Canonical(), l.Mode}
	}
	raw, err := json.Marshal(struct {
		Domain  string
		Request request
		Rows    []projectExecutionLeaseRow
		Locks   []lock
	}{"project_execution_leases.v1", facts, rows, keys})
	if err != nil {
		return "", unavailable(err)
	}
	h := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(h[:])), nil
}
