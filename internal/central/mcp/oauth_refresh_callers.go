package mcp

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func oauthInvocationCall(actor id.Actor, project id.ProjectID, execution, operation, tool, request string) (outbound.CallContext, error) {
	scope, err := id.InProject(project)
	if err != nil {
		return outbound.CallContext{}, err
	}
	key, err := ac.NewAppendKey(ac.AccessProducer, request, 0)
	if err != nil {
		return outbound.CallContext{}, err
	}
	return outbound.NewCallContext(actor, scope, key, ac.Associations{ExecutionID: execution, OperationID: operation, ToolID: tool, RequestID: request})
}

func (s *Service) runtimeOAuthAccess(ctx context.Context, w *runtimeSecretUse) (sc.SecretMaterial, error) {
	ctx = context.WithValue(ctx, runtimeSecretKey{}, w)
	if _, err := s.runtimeSecretUse(ctx, w.request); err != nil {
		return sc.SecretMaterial{}, err
	}
	v := w.invocation
	call, err := oauthInvocationCall(v.Call.Actor, v.Call.ProjectID, v.Call.ExecutionID.String(), v.OperationID.String(), v.Call.Spec.ToolID.String(), v.RequestID)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	source := &oauthCredentialSource{service: s, credential: w.snapshot.credential, binding: w.snapshot.binding, locks: w.locks, call: call,
		caller: oauthRefreshCaller{execution: v.Call.ExecutionID.String(), operation: v.OperationID.String(), attempt: v.AttemptID.String(), request: v.RequestID, runtime: w.snapshot.binding.RuntimeBindingID}}
	source.read = func(ctx context.Context) (sc.SecretMaterial, error) {
		return s.options.SecretReader.ReadMCPCredentialForUsage(ctx, w.request)
	}
	source.require = func(ctx context.Context, tx f.Tx) error {
		if original, err := s.runtimeSecretUse(ctx, w.request); err != nil || original != w {
			return fail(f.Forbidden)
		}
		if err := w.backend.authority.RequireMCPInvocationInTx(ctx, tx, v, w.invocationPlan); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		current, err := loadRuntimeSnapshot(ctx, x, v)
		if err != nil {
			return err
		}
		if current != w.snapshot {
			return fail(f.ConfirmationStale)
		}
		return nil
	}
	return s.readOAuthCredential(ctx, source)
}

func (s *Service) resourceOAuthAccess(ctx context.Context, w *resourceSecretUse) (sc.SecretMaterial, error) {
	ctx = context.WithValue(ctx, resourceSecretKey{}, w)
	if _, err := s.resourceSecretUse(ctx, w.request); err != nil {
		return sc.SecretMaterial{}, err
	}
	v := w.invocation
	call, err := oauthInvocationCall(v.Call.Actor, v.Call.ProjectID, v.Call.ExecutionID.String(), v.OperationID.String(), v.Call.Spec.ToolID.String(), v.RequestID)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	source := &oauthCredentialSource{service: s, credential: w.snapshot.credential, binding: w.snapshot.binding, locks: w.locks, call: call,
		caller: oauthRefreshCaller{execution: v.Call.ExecutionID.String(), operation: v.OperationID.String(), attempt: v.AttemptID.String(), request: v.RequestID, runtime: w.snapshot.binding.RuntimeBindingID}}
	source.read = func(ctx context.Context) (sc.SecretMaterial, error) {
		return s.options.SecretReader.ReadMCPCredentialForUsage(ctx, w.request)
	}
	source.require = func(ctx context.Context, tx f.Tx) error {
		if original, err := s.resourceSecretUse(ctx, w.request); err != nil || original != w {
			return fail(f.Forbidden)
		}
		if err := w.backend.authority.RequireMCPResourceInvocationInTx(ctx, tx, v, w.plan); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		current, err := loadConnectionSnapshot(ctx, x, v.Call.ExecutionID, w.snapshot.binding)
		if err != nil {
			return err
		}
		if current != w.snapshot {
			return fail(f.ConfirmationStale)
		}
		return nil
	}
	return s.readOAuthCredential(ctx, source)
}

func (s *Service) startupOAuthAccess(ctx context.Context, w *startupUse, call outbound.CallContext) (sc.SecretMaterial, error) {
	if w == nil || w.service != s {
		return sc.SecretMaterial{}, fail(f.Forbidden)
	}
	b := RuntimeConnectionBinding{ProjectID: w.connection.ProjectID, ConfigID: w.config.ID.String(), ConnectionID: w.connection.ID.String(), ConfigRevision: w.config.RuntimeRevision, RuntimeBindingID: w.connection.RuntimeBindingID}
	source := &oauthCredentialSource{service: s, credential: w.connection.CredentialID, binding: b, connectionVersion: w.connection.Version, locks: w.locks, call: call, caller: oauthRefreshCaller{startup: w.request}}
	source.read = func(ctx context.Context) (sc.SecretMaterial, error) { return s.readStartupCredential(ctx, w) }
	source.require = func(ctx context.Context, tx f.Tx) error {
		original, fact, err := s.requireStartupInTx(ctx, tx, true)
		if err != nil {
			return err
		}
		if original != w || !fact.started || fact.retired {
			return fail(f.Forbidden)
		}
		return s.requireStartupProject(ctx, tx, w)
	}
	return s.readOAuthCredential(ctx, source)
}
