package projectvariable

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// SecretDirectory is a synchronous, store-bound metadata authority. It has no
// material reader, lease, worker or independently owned transaction lifetime.
// The consumer owns and joins its call and the final caller transaction.
type SecretDirectory struct{ data func() *secretDirectoryState }
type secretDirectoryState struct {
	store    Store
	projects pc.ProjectAuthority
	issuer   c.SecretPlanIssuer
}

func NewSecretDirectory(store Store, projects pc.ProjectAuthority) (*SecretDirectory, error) {
	if nilPort(store) || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	if !reflect.TypeOf(store).Comparable() {
		return nil, fault(f.InvalidArgument)
	}
	st := &secretDirectoryState{store, projects, c.NewSecretPlanIssuer()}
	return &SecretDirectory{func() *secretDirectoryState { return st }}, nil
}
func (d *SecretDirectory) state() *secretDirectoryState {
	if d == nil || d.data == nil {
		return nil
	}
	return d.data()
}

func secretDirectoryBaseLocks(r c.SecretDirectoryRequest) []f.LockRequest {
	return []f.LockRequest{commandLock(r.Command), userLock(r.Actor.Details().UserID, f.Exclusive), projectLock(r.ProjectID, f.Shared)}
}
func secretDirectoryLocks(r c.SecretDirectoryRequest, rows []*secretVariableRow) ([]f.LockRequest, error) {
	locks := secretDirectoryBaseLocks(r)
	for _, row := range rows {
		if row == nil || row.Deleted != nil {
			continue
		}
		key, err := f.AggregateLock(f.CredentialRefAggregate, row.Ref.Details().ID.String())
		if err != nil {
			return nil, internal(err)
		}
		locks = append(locks, f.LockRequest{Key: key, Mode: f.Shared})
	}
	return oc.NormalizeLocks(locks)
}
func sameSecretDirectoryLocks(a, b []f.LockRequest) bool {
	return slices.EqualFunc(a, b, func(a, b f.LockRequest) bool {
		return f.CompareLockKeys(a.Key, b.Key) == 0 && a.Mode == b.Mode
	})
}

func (st *secretDirectoryState) current(ctx context.Context, tx f.Tx, r c.SecretDirectoryRequest, locks []f.LockRequest, intent i.AccessIntent) (postgres.SQLExecutor, error) {
	x, err := st.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, internal(nil)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, portError(err)
	}
	access, err := st.projects.RequireOwnerInTx(ctx, tx, r.Actor, r.ProjectID, intent)
	if err != nil {
		return nil, portError(err)
	}
	if access.Project().ID != r.ProjectID {
		return nil, internal(nil)
	}
	return x, nil
}

// Project SH protects all entries, including absent IDs and tombstones: every
// ordinary/Secret variable writer and delete gate takes the same Project EX.
// Only this Project and type=secret are read. Foreign, ordinary and missing IDs
// therefore have the same public status without probing another Project.
func secretDirectoryRows(ctx context.Context, x postgres.SQLExecutor, r c.SecretDirectoryRequest) ([]*secretVariableRow, error) {
	rows := make([]*secretVariableRow, len(r.IDs))
	for n, id := range r.IDs {
		if err := ctx.Err(); err != nil {
			return nil, canceled(err)
		}
		row, err := loadSecretVariable(ctx, x, r.ProjectID, id, true)
		if err != nil {
			var problem *f.Fault
			if errors.As(err, &problem) && problem.Code == f.NotFound {
				continue
			}
			return nil, err
		}
		if row == nil || row.Variable.Fields().ProjectID != r.ProjectID || row.Variable.Fields().ID != id {
			return nil, internal(nil)
		}
		rows[n] = row
	}
	return rows, nil
}

// This digest binds metadata and its storage mapping, never Secret value,
// ciphertext, value length, or a value fingerprint. It is not a bearer grant.
func secretDirectoryMapping(r c.SecretDirectoryRequest, rows []*secretVariableRow) (f.Digest, error) {
	type entry struct {
		ID                c.VariableID
		Variable          *c.SecretVariableFields
		Deleted           *f.Instant
		CredentialID      string
		CredentialVersion *f.Version
	}
	entries := make([]entry, len(r.IDs))
	for n, id := range r.IDs {
		entries[n].ID = id
		if row := rows[n]; row != nil {
			v := row.Variable.Fields()
			entries[n].Variable, entries[n].Deleted = &v, row.Deleted
			if row.Deleted == nil {
				entries[n].CredentialID = row.Ref.Details().ID.String()
				version := row.CredentialVersion
				entries[n].CredentialVersion = &version
			}
		}
	}
	raw, err := canonical(struct {
		Format  string
		Project c.ProjectID
		Entries []entry
	}{"secret-directory-mapping-v1", r.ProjectID, entries})
	if err != nil {
		return "", err
	}
	return digest(raw), nil
}
func secretDirectoryFacts(r c.SecretDirectoryRequest, rows []*secretVariableRow) (c.SecretDirectoryFacts, error) {
	entries := make([]c.SecretDirectoryEntry, len(r.IDs))
	for n, id := range r.IDs {
		entries[n] = c.SecretDirectoryEntry{ID: id, Status: c.SecretDirectoryNotInScope}
		if row := rows[n]; row != nil {
			if row.Deleted != nil {
				entries[n].Status = c.SecretDirectoryRemoved
			} else {
				variable := row.Variable.Clone()
				entries[n].Status, entries[n].Variable = c.SecretDirectoryValid, &variable
			}
		}
	}
	out, err := c.NewSecretDirectoryFacts(r, entries)
	return out, portError(err)
}

func (d *SecretDirectory) DiscoverSecretVariables(ctx context.Context, request c.SecretDirectoryRequest) (c.SecretDirectoryPlan, error) {
	empty := c.SecretDirectoryPlan{}
	st := d.state()
	if st == nil {
		return empty, fault(f.DependencyUnbound)
	}
	if ctx == nil || request.Validate() != nil {
		return empty, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return empty, canceled(err)
	}
	r := request.Clone()
	var out c.SecretDirectoryPlan
	result := st.store.WithinTx(ctx, commandCause(r.Command), func(ctx context.Context, tx f.Tx) error {
		base := secretDirectoryBaseLocks(r)
		if err := st.store.AcquireAll(ctx, tx, base); err != nil {
			return portError(err)
		}
		x, err := st.current(ctx, tx, r, base, i.Read)
		if err != nil {
			return err
		}
		rows, err := secretDirectoryRows(ctx, x, r)
		if err != nil {
			return err
		}
		mapping, err := secretDirectoryMapping(r, rows)
		if err != nil {
			return err
		}
		locks, err := secretDirectoryLocks(r, rows)
		if err != nil {
			return err
		}
		out, err = c.NewSecretDirectoryPlan(st.issuer, r, mapping, locks)
		return portError(err)
	})
	if err := txError(result); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, canceled(err)
	}
	return out, nil
}

func (d *SecretDirectory) RequireSecretVariablesInTx(ctx context.Context, tx f.Tx, request c.SecretDirectoryRequest, plan c.SecretDirectoryPlan) (c.SecretDirectoryFacts, error) {
	empty := c.SecretDirectoryFacts{}
	st := d.state()
	if st == nil {
		return empty, fault(f.DependencyUnbound)
	}
	if ctx == nil || request.Validate() != nil {
		return empty, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return empty, canceled(err)
	}
	r := request.Clone()
	binding, err := c.SecretDirectoryBinding(r)
	if err != nil {
		return empty, portError(err)
	}
	if plan.Validate() != nil || !plan.Matches(st.issuer, binding, plan.Details().Mapping) {
		return empty, fault(f.Forbidden)
	}
	x, err := st.current(ctx, tx, r, plan.RequiredLocks(), i.Mutate)
	if err != nil {
		return empty, err
	}
	rows, err := secretDirectoryRows(ctx, x, r)
	if err != nil {
		return empty, err
	}
	mapping, err := secretDirectoryMapping(r, rows)
	if err != nil {
		return empty, err
	}
	if !plan.Matches(st.issuer, binding, mapping) {
		return empty, fault(f.ResourceBusy)
	}
	locks, err := secretDirectoryLocks(r, rows)
	if err != nil {
		return empty, err
	}
	if !sameSecretDirectoryLocks(locks, plan.RequiredLocks()) {
		return empty, fault(f.Forbidden)
	}
	out, err := secretDirectoryFacts(r, rows)
	if err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, canceled(err)
	}
	return out, nil
}

func (SecretDirectory) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "secret_directory") }
func (SecretDirectory) LogValue() slog.Value         { return slog.StringValue("secret_directory") }
func (SecretDirectory) MarshalJSON() ([]byte, error) { return []byte(`"secret_directory"`), nil }

var _ c.SecretDirectory = (*SecretDirectory)(nil)
