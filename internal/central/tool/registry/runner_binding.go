package registry

import (
	"bytes"
	"context"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// RunnerSource proves the fixed metadata and local contract binding only.
// Operation ownership, live Runner capability and dispatch remain separate,
// mandatory authorities. There is no default source or network work here.
type RunnerSource interface {
	DescribeInTx(context.Context, f.Tx) (tc.RunnerRegistration, error)
	CheckBindingInTx(context.Context, f.Tx, tc.RunnerRegistration) error
}

type readFileMetadataSource struct{ store Store }

func NewReadFileMetadataSource(store Store) (RunnerSource, error) {
	if nilPort(store) {
		return nil, fail(f.DependencyUnbound)
	}
	return &readFileMetadataSource{store: store}, nil
}

func (s *readFileMetadataSource) current(ctx context.Context, tx f.Tx) error {
	if err := captureContext(ctx); err != nil {
		return err
	}
	if _, err := s.store.InTx(tx); err != nil {
		return portError(err)
	}
	return portError(s.store.RequireHeldLocks(ctx, tx, []f.LockRequest{RegistryLock(f.Shared)}))
}
func (s *readFileMetadataSource) DescribeInTx(ctx context.Context, tx f.Tx) (tc.RunnerRegistration, error) {
	if err := s.current(ctx, tx); err != nil {
		return tc.RunnerRegistration{}, err
	}
	return tc.ReadFileRegistration(), nil
}
func (s *readFileMetadataSource) CheckBindingInTx(ctx context.Context, tx f.Tx, v tc.RunnerRegistration) error {
	if err := s.current(ctx, tx); err != nil {
		return err
	}
	if !v.Active {
		return fail(f.InvalidArgument)
	}
	return v.Validate(ctx)
}

func (r *Registry) runnerSource(ctx context.Context, tx f.Tx, key string) (tc.RunnerRegistration, []byte, error) {
	s := r.data()
	if s == nil || key != tc.RunnerReadFileKey || nilPort(s.runners[key]) {
		return tc.RunnerRegistration{}, nil, fail(f.DependencyUnbound)
	}
	source := s.runners[key]
	v, err := source.DescribeInTx(ctx, tx)
	if err != nil {
		return tc.RunnerRegistration{}, nil, portError(err)
	}
	if err = v.Validate(ctx); err != nil {
		return tc.RunnerRegistration{}, nil, portError(err)
	}
	if v.Active {
		if err = source.CheckBindingInTx(ctx, tx, v); err != nil {
			return tc.RunnerRegistration{}, nil, portError(err)
		}
	}
	raw, err := tc.CanonicalDefinition(ctx, v.Definition)
	return v, raw, portError(err)
}

func (r *Registry) currentRunner(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, v currentTool, raw []byte) (currentTool, error) {
	if v.Binding != (tc.BuiltinBinding{}) {
		return currentTool{}, fail(f.InvalidState)
	}
	var kind string
	var major int64
	err := x.QueryRow(ctx, `SELECT binding_kind,operation_name,operation_revision,protocol_major FROM agenteam_tool.registrations WHERE tool_id=$1`, v.Ref.ToolID.String()).Scan(&kind, &v.RunnerBinding.OperationName, &v.RunnerBinding.OperationRevision, &major)
	if err != nil {
		return currentTool{}, portError(err)
	}
	if kind != "runner" || major != 1 {
		return currentTool{}, fail(f.InvalidState)
	}
	v.RunnerBinding.ProtocolMajor = 1
	v.Definition, err = decodeDefinition(ctx, raw, v.Key)
	if err != nil {
		return currentTool{}, err
	}
	source, canonical, err := r.runnerSource(ctx, tx, v.Key)
	if err != nil {
		return currentTool{}, err
	}
	if !source.Active {
		return currentTool{}, fail(f.NotFound)
	}
	if !bytes.Equal(canonical, v.Definition) || source.Binding != v.RunnerBinding || source.ScopeResolverID != v.Scope || source.RiskClassifierID != v.Risk || source.Class != v.Class {
		return currentTool{}, fail(f.InvalidState)
	}
	return v, captureContext(ctx)
}

// ReconcileRunnerInTx is an explicit trusted-source metadata write. The caller
// owns Registry EX, the transaction and any uncertain-commit confirmation.
func (r *Registry) ReconcileRunnerInTx(ctx context.Context, tx f.Tx, key string) (tc.SpecRef, error) {
	if err := captureContext(ctx); err != nil {
		return tc.SpecRef{}, err
	}
	if key != tc.RunnerReadFileKey {
		return tc.SpecRef{}, fail(f.InvalidArgument)
	}
	x, err := r.executor(ctx, tx, []f.LockRequest{RegistryLock(f.Exclusive)})
	if err != nil {
		return tc.SpecRef{}, err
	}
	v, definition, err := r.runnerSource(ctx, tx, key)
	if err != nil {
		return tc.SpecRef{}, err
	}
	old, err := loadIdentity(ctx, x, key)
	if err != nil {
		return tc.SpecRef{}, err
	}
	if !v.Active {
		if old == nil {
			return tc.SpecRef{}, fail(f.NotFound)
		}
		if _, err = x.Exec(ctx, `DELETE FROM agenteam_tool.registrations WHERE tool_id=$1`, old.ID.String()); err != nil {
			return tc.SpecRef{}, portError(err)
		}
		return tc.SpecRef{ToolID: old.ID, SpecRevision: old.Revision}, captureContext(ctx)
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
		tool, revision = old.ID, old.Revision
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
	_, err = x.Exec(ctx, `INSERT INTO agenteam_tool.registrations(tool_id,spec_revision,handler_id,contract_revision,scope_resolver_id,risk_classifier_id,class,binding_kind,operation_name,operation_revision,protocol_major) VALUES($1,$2,NULL,NULL,$3,$4,$5,'runner',$6,$7,$8) ON CONFLICT(tool_id) DO UPDATE SET spec_revision=EXCLUDED.spec_revision,handler_id=NULL,contract_revision=NULL,scope_resolver_id=EXCLUDED.scope_resolver_id,risk_classifier_id=EXCLUDED.risk_classifier_id,class=EXCLUDED.class,binding_kind=EXCLUDED.binding_kind,operation_name=EXCLUDED.operation_name,operation_revision=EXCLUDED.operation_revision,protocol_major=EXCLUDED.protocol_major`, tool.String(), int64(revision), string(v.ScopeResolverID), string(v.RiskClassifierID), string(v.Class), v.Binding.OperationName, v.Binding.OperationRevision, int(v.Binding.ProtocolMajor))
	if err != nil {
		return tc.SpecRef{}, portError(err)
	}
	return tc.SpecRef{ToolID: tool, SpecRevision: revision}, captureContext(ctx)
}

func (r *Registry) RequireCurrentRunnerInTx(ctx context.Context, tx f.Tx, spec tc.SpecRef, binding tc.RunnerBinding, scope tc.ScopeResolverID, risk tc.RiskClassifierID) error {
	if err := captureContext(ctx); err != nil {
		return err
	}
	if spec.Validate() != nil || binding.Validate() != nil || scope != "runner.read-file" || risk != "runner.read-file" {
		return fail(f.InvalidArgument)
	}
	x, err := r.executor(ctx, tx, []f.LockRequest{RegistryLock(f.Shared), toolLock(spec.ToolID, f.Shared)})
	if err != nil {
		return err
	}
	v, err := r.currentTool(ctx, tx, x, spec.ToolID)
	if err != nil {
		return err
	}
	if v.Ref != spec || v.RunnerBinding != binding || v.Scope != scope || v.Risk != risk {
		return fail(f.InvalidState)
	}
	return captureContext(ctx)
}
