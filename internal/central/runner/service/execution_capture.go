package service

import (
	"context"
	"errors"
	"reflect"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/jackc/pgx/v5"
)

// ExecutionMetadataDirectory supplies safe metadata to execution preparation.
// It does not reuse Human configuration or administrator read authority.
type ExecutionMetadataDirectory struct {
	store     Store
	authority c.ExecutionMetadataAuthority
}

func NewExecutionMetadataDirectory(store Store, authority c.ExecutionMetadataAuthority) (*ExecutionMetadataDirectory, error) {
	if nilPort(store) || nilPort(authority) || !reflect.TypeOf(store).Comparable() {
		return nil, fault(f.DependencyUnbound)
	}
	return &ExecutionMetadataDirectory{store: store, authority: authority}, nil
}

func (d *ExecutionMetadataDirectory) ExecutionMetadataLocks(ids []c.RunnerID) ([]f.LockRequest, error) {
	if d == nil || nilPort(d.store) || nilPort(d.authority) {
		return nil, fault(f.DependencyUnbound)
	}
	return c.ConfigurationCandidateLockRequests(ids)
}

// The projection deliberately avoids both private columns and the connection
// join: offline or incompatible Runners retain valid durable metadata.
const executionMetadataQuery = `SELECT id::text,name,description,version FROM agenteam_runner.runners WHERE id=$1`

func scanExecutionMetadata(row postgres.Row) (c.ExecutionMetadata, error) {
	if nilPort(row) {
		return c.ExecutionMetadata{}, unavailable(nil)
	}
	var out c.ExecutionMetadata
	var raw string
	var version int64
	if err := row.Scan(&raw, &out.Name, &out.Description, &version); err != nil {
		return c.ExecutionMetadata{}, err
	}
	var err error
	out.ID, err = f.ParseID[c.Runner](raw)
	if err != nil {
		return c.ExecutionMetadata{}, unavailable(nil)
	}
	out.Version = f.Version(version)
	if out.Validate() != nil {
		return c.ExecutionMetadata{}, unavailable(nil)
	}
	return out, nil
}

func (d *ExecutionMetadataDirectory) ReadExecutionMetadataInTx(ctx context.Context, tx f.Tx, request c.ExecutionMetadataRequest) ([]c.ExecutionMetadata, error) {
	if ctx == nil || !tx.Valid() || request.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request.RunnerIDs = slices.Clone(request.RunnerIDs)
	held, err := d.ExecutionMetadataLocks(request.RunnerIDs)
	if err != nil {
		return nil, err
	}
	x, err := d.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, unavailable(nil)
	}
	if err = d.store.RequireHeldLocks(ctx, tx, held); err != nil {
		return nil, portError(err)
	}
	authorized := request
	authorized.RunnerIDs = slices.Clone(request.RunnerIDs)
	if err = d.authority.RequireExecutionMetadataInTx(ctx, tx, authorized); err != nil {
		return nil, portError(err)
	}
	out := make([]c.ExecutionMetadata, 0, len(request.RunnerIDs))
	for _, id := range request.RunnerIDs {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		metadata, err := scanExecutionMetadata(x.QueryRow(ctx, executionMetadataQuery, id.String()))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fault(f.NotFound)
		}
		if err != nil {
			return nil, portError(err)
		}
		if metadata.ID != id {
			return nil, unavailable(nil)
		}
		out = append(out, metadata)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

var _ c.ExecutionMetadataProvider = (*ExecutionMetadataDirectory)(nil)
