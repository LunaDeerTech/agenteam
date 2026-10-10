package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

type identityRecord struct {
	ID         id.ToolID
	Key        string
	Revision   f.Version
	Definition []byte
}

func loadIdentity(ctx context.Context, x postgres.SQLExecutor, key string) (*identityRecord, error) {
	var tool string
	var record identityRecord
	var raw []byte
	err := x.QueryRow(ctx, `SELECT i.tool_id,i.stable_key,i.latest_revision,s.definition FROM agenteam_tool.identities i JOIN agenteam_tool.spec_revisions s ON s.tool_id=i.tool_id AND s.spec_revision=i.latest_revision WHERE i.stable_key=$1`, key).Scan(&tool, &record.Key, &record.Revision, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	record.ID, err = f.ParseID[id.Tool](tool)
	if err != nil || record.Revision.Validate() != nil || record.Key != key {
		return nil, fail(f.InvalidState)
	}
	record.Definition, err = decodeDefinition(ctx, raw, key)
	if err != nil {
		return nil, err
	}
	return &record, nil
}
func decodeDefinition(ctx context.Context, raw []byte, key string) ([]byte, error) {
	if len(raw) > tc.MaxDefinitionBytes {
		return nil, fail(f.InvalidState)
	}
	var definition tc.Definition
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&definition); err != nil || definition.StableKey != key {
		return nil, fail(f.InvalidState)
	}
	b, err := tc.CanonicalDefinition(ctx, definition)
	if err != nil {
		return nil, portError(err)
	}
	// bytea deliberately has no database JSON normalization. Require the complete
	// exact canonical encoding on reads too, rejecting trailing values, replaced
	// Unicode and any data not written by the validated metadata writer.
	if !bytes.Equal(b, raw) {
		return nil, fail(f.InvalidState)
	}
	return b, nil
}

// ReconcileBuiltinInTx accepts only a configured canonical source key. It never
// accepts a caller-supplied definition or generates an ID while resolving a
// reference. The caller owns CommitResult/Unknown recovery and must not replay a
// callback on uncertain commit; lookup with this stable key recovers its facts.
// Registry EX covers identity creation before its ToolID exists. No late locks,
// external calls, autonomous transaction or execution permission are produced.
func (r *Registry) ReconcileBuiltinInTx(ctx context.Context, tx f.Tx, key string) (tc.SpecRef, error) {
	if !tc.ValidBuiltinKey(key) {
		return tc.SpecRef{}, fail(f.InvalidArgument)
	}
	x, err := r.executor(ctx, tx, []f.LockRequest{RegistryLock(f.Exclusive)})
	if err != nil {
		return tc.SpecRef{}, err
	}
	registration, definition, err := r.source(ctx, tx, key)
	if err != nil {
		return tc.SpecRef{}, err
	}
	old, err := loadIdentity(ctx, x, key)
	if err != nil {
		return tc.SpecRef{}, err
	}
	if !registration.Active {
		if old == nil {
			return tc.SpecRef{}, fail(f.NotFound)
		}
		if _, err = x.Exec(ctx, `DELETE FROM agenteam_tool.registrations WHERE tool_id=$1`, old.ID.String()); err != nil {
			return tc.SpecRef{}, portError(err)
		}
		if err = ctx.Err(); err != nil {
			return tc.SpecRef{}, portError(err)
		}
		return tc.SpecRef{ToolID: old.ID, SpecRevision: old.Revision}, nil
	}
	var tool id.ToolID
	revision := f.Version(1)
	newSpec := old == nil
	if old == nil {
		tool, err = f.NewID[id.Tool]()
		if err != nil {
			return tc.SpecRef{}, portError(err)
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_tool.identities(tool_id,stable_key,latest_revision) VALUES($1,$2,1)`, tool.String(), key); err != nil {
			return tc.SpecRef{}, portError(err)
		}
	} else {
		tool = old.ID
		revision = old.Revision
		if !bytes.Equal(old.Definition, definition) {
			if revision == f.Version(math.MaxInt64) {
				return tc.SpecRef{}, fail(f.InvalidState)
			}
			revision++
			newSpec = true
		}
	}
	if newSpec {
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_tool.spec_revisions(tool_id,spec_revision,definition) VALUES($1,$2,$3)`, tool.String(), int64(revision), definition); err != nil {
			return tc.SpecRef{}, portError(err)
		}
		if old != nil {
			if _, err = x.Exec(ctx, `UPDATE agenteam_tool.identities SET latest_revision=$2 WHERE tool_id=$1`, tool.String(), int64(revision)); err != nil {
				return tc.SpecRef{}, portError(err)
			}
		}
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_tool.registrations(tool_id,spec_revision,handler_id,contract_revision,scope_resolver_id,risk_classifier_id,class) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tool_id) DO UPDATE SET spec_revision=EXCLUDED.spec_revision,handler_id=EXCLUDED.handler_id,contract_revision=EXCLUDED.contract_revision,scope_resolver_id=EXCLUDED.scope_resolver_id,risk_classifier_id=EXCLUDED.risk_classifier_id,class=EXCLUDED.class`, tool.String(), int64(revision), registration.Binding.HandlerID, int64(registration.Binding.ContractRevision), string(registration.ScopeResolverID), string(registration.RiskClassifierID), string(registration.Class))
	if err != nil {
		return tc.SpecRef{}, portError(err)
	}
	if err = ctx.Err(); err != nil {
		return tc.SpecRef{}, portError(err)
	}
	return tc.SpecRef{ToolID: tool, SpecRevision: revision}, nil
}

// ReadDefinitionInTx is an explicit historical metadata read for trusted
// consumers already holding Registry SH. It grants no current registration,
// configuration permission or execution. Caller authorization belongs upstream.
func (r *Registry) ReadDefinitionInTx(ctx context.Context, tx f.Tx, ref tc.SpecRef) (tc.Definition, error) {
	if ref.Validate() != nil {
		return tc.Definition{}, fail(f.InvalidArgument)
	}
	x, err := r.executor(ctx, tx, []f.LockRequest{RegistryLock(f.Shared)})
	if err != nil {
		return tc.Definition{}, err
	}
	var raw []byte
	var key string
	err = x.QueryRow(ctx, `SELECT i.stable_key,s.definition FROM agenteam_tool.spec_revisions s JOIN agenteam_tool.identities i ON i.tool_id=s.tool_id WHERE s.tool_id=$1 AND s.spec_revision=$2`, ref.ToolID.String(), int64(ref.SpecRevision)).Scan(&key, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return tc.Definition{}, fail(f.NotFound)
	}
	if err != nil {
		return tc.Definition{}, portError(err)
	}
	b, err := decodeDefinition(ctx, raw, key)
	if err != nil {
		return tc.Definition{}, err
	}
	var d tc.Definition
	if err = json.Unmarshal(b, &d); err != nil {
		return tc.Definition{}, portError(err)
	}
	if err = ctx.Err(); err != nil {
		return tc.Definition{}, portError(err)
	}
	return d, nil
}
