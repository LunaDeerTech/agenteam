package projectvariable

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// SecretReferenceService maintains this producer's complete Agent reference
// index. Agent owns the canonical write and its private same-Tx witness. Calls
// are synchronous; the caller retains its transaction, cancellation and join.
type SecretReferenceService struct{ data func() *secretReferenceState }
type secretReferenceState struct {
	store     Store
	directory *SecretDirectory
	owner     c.SecretReferenceOwnerAuthority
	issuer    c.SecretPlanIssuer
}

func NewSecretReferences(store Store, directory *SecretDirectory, owner c.SecretReferenceOwnerAuthority) (*SecretReferenceService, error) {
	if nilPort(store) || directory.state() == nil || !sameStore(store, directory.state().store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	state := &secretReferenceState{store, directory, owner, c.NewSecretPlanIssuer()}
	return &SecretReferenceService{func() *secretReferenceState { return state }}, nil
}
func (s *SecretReferenceService) state() *secretReferenceState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}
func (s *SecretReferenceService) validate(ctx context.Context, r c.SecretReferenceChange) error {
	if s.state() == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || r.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return canceled(err)
	}
	return nil
}

func referenceDirectoryRequest(r c.SecretReferenceChange, ids []c.VariableID) c.SecretDirectoryRequest {
	return c.SecretDirectoryRequest{Actor: r.Actor, ProjectID: r.ProjectID, Command: r.Command, IDs: slices.Clone(ids)}
}
func secretReferenceBaseLocks(r c.SecretReferenceChange, owner c.SecretReferenceOwnerPlan) ([]f.LockRequest, error) {
	locks := secretDirectoryBaseLocks(referenceDirectoryRequest(r, r.After))
	agent, err := f.AgentLock(r.AgentID.String())
	if err != nil {
		return nil, portError(err)
	}
	locks = append(locks, f.LockRequest{Key: agent, Mode: f.Exclusive})
	locks = append(locks, owner.RequiredLocks()...)
	return oc.NormalizeLocks(locks)
}

type secretReferenceIndexEntry struct {
	ID      c.VariableID
	Version f.Version
}
type secretReferenceSnapshot struct {
	Index         []secretReferenceIndexEntry
	Before, After f.Digest
}

// The indexed owner query is complete, including an empty set. The extra row
// detects invalid oversized state instead of truncating it to the request.
func readSecretReferenceIndex(ctx context.Context, x postgres.SQLExecutor, r c.SecretReferenceChange) ([]secretReferenceIndexEntry, error) {
	// QueryRow owns the real Rows close. Aggregate only the bounded indexed
	// prefix, retaining the 257th entry solely to reject oversized state.
	var ids []string
	var versions []int64
	err := x.QueryRow(ctx, `SELECT COALESCE(array_agg(variable_id::text ORDER BY variable_id),'{}'::text[]),COALESCE(array_agg(owner_version ORDER BY variable_id),'{}'::bigint[]) FROM (SELECT variable_id,owner_version FROM agenteam_projectvariable.secret_references WHERE project_id=$1 AND owner_kind='agent' AND owner_id=$2 ORDER BY variable_id LIMIT 257) AS owned`, r.ProjectID.String(), r.AgentID.String()).Scan(&ids, &versions)
	if err != nil {
		return nil, unavailable(err)
	}
	if len(ids) != len(versions) || len(ids) > c.MaxSecretReferences {
		return nil, internal(nil)
	}
	index := make([]secretReferenceIndexEntry, len(ids))
	for n, raw := range ids {
		if err := ctx.Err(); err != nil {
			return nil, canceled(err)
		}
		id, err := f.ParseID[i.ProjectVariable](raw)
		if err != nil || versions[n] < 1 || n > 0 && ids[n-1] >= raw {
			return nil, internal(nil)
		}
		index[n] = secretReferenceIndexEntry{id, f.Version(versions[n])}
	}
	return index, nil
}
func requireSecretReferenceBefore(r c.SecretReferenceChange, index []secretReferenceIndexEntry) error {
	if len(index) != len(r.Before) {
		return fault(f.ResourceBusy)
	}
	for n, entry := range index {
		if r.ExpectedOwnerVersion == nil || entry.ID != r.Before[n] || entry.Version != *r.ExpectedOwnerVersion {
			return fault(f.ResourceBusy)
		}
	}
	return nil
}

// Before and After each retain their original <=256-item contract; their
// disjoint union may have 512 IDs. No oversized public Directory request or
// lossy diff is substituted for these two complete sets.
func secretReferenceRead(ctx context.Context, x postgres.SQLExecutor, r c.SecretReferenceChange, owner c.SecretReferenceOwnerPlan) (secretReferenceSnapshot, []*secretVariableRow, []f.LockRequest, error) {
	var snapshot secretReferenceSnapshot
	index, err := readSecretReferenceIndex(ctx, x, r)
	if err != nil {
		return snapshot, nil, nil, err
	}
	snapshot.Index = index
	locks, err := secretReferenceBaseLocks(r, owner)
	if err != nil {
		return snapshot, nil, nil, portError(err)
	}
	var after []*secretVariableRow
	for n, ids := range [][]c.VariableID{r.Before, r.After} {
		request := referenceDirectoryRequest(r, ids)
		rows, err := secretDirectoryRows(ctx, x, request)
		if err != nil {
			return snapshot, nil, nil, err
		}
		mapping, err := secretDirectoryMapping(request, rows)
		if err != nil {
			return snapshot, nil, nil, err
		}
		if n == 0 {
			snapshot.Before = mapping
		} else {
			snapshot.After, after = mapping, rows
		}
		variableLocks, err := secretDirectoryLocks(request, rows)
		if err != nil {
			return snapshot, nil, nil, err
		}
		locks = append(locks, variableLocks...)
	}
	locks, err = oc.NormalizeLocks(locks)
	return snapshot, after, locks, portError(err)
}
func secretReferenceMapping(snapshot secretReferenceSnapshot) (f.Digest, error) {
	raw, err := canonical(struct {
		Format   string
		Snapshot secretReferenceSnapshot
	}{"secret-reference-mapping-v1", snapshot})
	if err != nil {
		return "", err
	}
	return digest(raw), nil
}
func requireSecretReferenceTargets(rows []*secretVariableRow) error {
	for _, row := range rows {
		if row == nil || row.Deleted != nil {
			return fault(f.NotFound)
		}
	}
	return nil
}

func (s *SecretReferenceService) DiscoverSecretReferences(ctx context.Context, request c.SecretReferenceChange) (c.SecretReferencePlan, error) {
	zero := c.SecretReferencePlan{}
	if err := s.validate(ctx, request); err != nil {
		return zero, err
	}
	r, state := request.Clone(), s.state()
	owner, err := state.owner.Discover(ctx, r.Clone())
	if err != nil {
		return zero, portError(err)
	}
	binding, _ := c.SecretReferenceBinding(r)
	if owner.Validate() != nil || owner.Details().Binding != binding {
		return zero, fault(f.Forbidden)
	}
	base, err := secretReferenceBaseLocks(r, owner)
	if err != nil {
		return zero, portError(err)
	}
	var plan c.SecretReferencePlan
	result := state.store.WithinTx(ctx, commandCause(r.Command), func(ctx context.Context, tx f.Tx) error {
		if err := state.store.AcquireAll(ctx, tx, base); err != nil {
			return portError(err)
		}
		x, err := state.directory.state().current(ctx, tx, referenceDirectoryRequest(r, r.After), base, i.Read)
		if err != nil {
			return err
		}
		snapshot, after, locks, err := secretReferenceRead(ctx, x, r, owner)
		if err != nil {
			return err
		}
		if err = requireSecretReferenceBefore(r, snapshot.Index); err != nil {
			return err
		}
		if err = requireSecretReferenceTargets(after); err != nil {
			return err
		}
		mapping, err := secretReferenceMapping(snapshot)
		if err != nil {
			return err
		}
		plan, err = c.NewSecretReferencePlan(state.issuer, r, mapping, locks, owner)
		return portError(err)
	})
	if err = txError(result); err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, canceled(err)
	}
	return plan, nil
}

func (s *SecretReferenceService) ApplySecretReferencesInTx(ctx context.Context, tx f.Tx, request c.SecretReferenceChange, plan c.SecretReferencePlan) error {
	if err := s.validate(ctx, request); err != nil {
		return err
	}
	r, state := request.Clone(), s.state()
	binding, _ := c.SecretReferenceBinding(r)
	if plan.Validate() != nil || !plan.Matches(state.issuer, binding, plan.Details().Mapping) {
		return fault(f.Forbidden)
	}
	x, err := state.directory.state().current(ctx, tx, referenceDirectoryRequest(r, r.After), plan.RequiredLocks(), i.Mutate)
	if err != nil {
		return err
	}
	// This is required even for an empty or unchanged reference set. Public
	// metadata, the new canonical row alone or an old receipt cannot supply it.
	if err = state.owner.CheckAppliedInTx(ctx, tx, r.Clone(), plan.OwnerPlan()); err != nil {
		return portError(err)
	}
	snapshot, after, locks, err := secretReferenceRead(ctx, x, r, plan.OwnerPlan())
	if err != nil {
		return err
	}
	mapping, err := secretReferenceMapping(snapshot)
	if err != nil {
		return err
	}
	if mapping != plan.Details().Mapping {
		return fault(f.ResourceBusy)
	}
	if !sameSecretDirectoryLocks(locks, plan.RequiredLocks()) {
		return fault(f.Forbidden)
	}
	if err = requireSecretReferenceBefore(r, snapshot.Index); err != nil {
		return err
	}
	if err = requireSecretReferenceTargets(after); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return canceled(err)
	}
	if r.ExpectedOwnerVersion != nil && *r.ExpectedOwnerVersion == r.ResultOwnerVersion && slices.Equal(r.Before, r.After) {
		return nil
	}
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_projectvariable.secret_references WHERE project_id=$1 AND owner_kind='agent' AND owner_id=$2`, r.ProjectID.String(), r.AgentID.String())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != int64(len(snapshot.Index)) {
		return internal(nil)
	}
	for _, id := range r.After {
		if err = ctx.Err(); err != nil {
			return canceled(err)
		}
		tag, err = x.Exec(ctx, `INSERT INTO agenteam_projectvariable.secret_references(project_id,variable_id,owner_kind,owner_id,owner_version) VALUES($1,$2,'agent',$3,$4)`, r.ProjectID.String(), id.String(), r.AgentID.String(), int64(r.ResultOwnerVersion))
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return internal(nil)
		}
	}
	if err = ctx.Err(); err != nil {
		return canceled(err)
	}
	return nil
}

func (SecretReferenceService) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "secret_references")
}
func (SecretReferenceService) LogValue() slog.Value { return slog.StringValue("secret_references") }
func (SecretReferenceService) MarshalJSON() ([]byte, error) {
	return []byte(`"secret_references"`), nil
}

var _ c.SecretReferences = (*SecretReferenceService)(nil)
