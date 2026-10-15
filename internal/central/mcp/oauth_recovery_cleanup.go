package mcp

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

func oauthRecoveryFlowCredential(v oauthRecoveryFact) (string, f.Version) {
	if v.operation == "begin" {
		return v.resultCredential, v.resultVersion
	}
	return v.credential, v.credentialVersion
}

func sameOAuthCleanupSource(source, deletion oauthRecoveryFact) bool {
	credential, version := oauthRecoveryFlowCredential(source)
	return source.operation != "retire" && deletion.operation == "retire" &&
		deletion.project == source.project && deletion.config == source.config && deletion.revision == source.revision &&
		deletion.connection == source.connection && deletion.version == source.version && deletion.flow == source.flow &&
		deletion.command == source.command && deletion.user == source.user && deletion.session == source.session &&
		deletion.credential == credential && deletion.credentialVersion == version
}

// Selection and allocation are a single canonical transaction. In particular,
// an old retirement is adopted before considering a new Delete identity.
// This function does not access Secret tables or resolve credential material.
func (s *Service) selectOAuthFlowCleanup(ctx context.Context, w *oauthRecoveryUse) (oauthRecoveryFact, bool, error) {
	var selected oauthRecoveryFact
	needed := false
	cause, _ := f.NewRecoveryCause("mcp.oauth.cleanup_select", w.claim, "")
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return err
		}
		source, err := w.require(ctx, tx)
		if err != nil {
			return err
		}
		if !source.retired || !source.released {
			return fail(f.ResourceBusy)
		}
		credential, version := oauthRecoveryFlowCredential(source)
		if credential == "" {
			return nil
		}
		needed = true
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		var existingSource, deletion string
		err = x.QueryRow(ctx, `SELECT source_request_id,delete_request_id FROM agenteam_mcp.oauth_flow_cleanups WHERE flow_id=$1`, source.flow).Scan(&existingSource, &deletion)
		if err == nil {
			if existingSource != source.request {
				return fail(f.Forbidden)
			}
			selected, err = loadOAuthRecoveryFact(ctx, x, deletion)
			if err != nil {
				return err
			}
			if !sameOAuthCleanupSource(source, selected) {
				return fail(f.Forbidden)
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		// Multiple historical Deletes cannot be disambiguated from a row's
		// absence or age. Preserve them and refuse a new mutation.
		rows, err := x.Query(ctx, `SELECT id FROM agenteam_mcp.oauth_requests
 WHERE flow_id=$1 AND operation='retire' ORDER BY id LIMIT 2`, source.flow)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				break
			}
			ids = append(ids, id)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		if len(ids) > 1 {
			return fail(f.InvalidState)
		}
		if len(ids) == 1 {
			selected, err = loadOAuthRecoveryFact(ctx, x, ids[0])
			if err != nil {
				return err
			}
			if !sameOAuthCleanupSource(source, selected) || selected.process != source.process {
				return fail(f.Forbidden)
			}
		} else {
			if w.newDelete == nil {
				request, err := f.NewID[f.Request]()
				if err != nil {
					return safeError(err)
				}
				candidate := source
				candidate.request, candidate.operation, candidate.process = request.String(), "retire", s.owner.process.String()
				candidate.credential, candidate.credentialVersion, candidate.lease = credential, version, ""
				candidate.phase, candidate.writeBinding, candidate.resultCredential, candidate.resultVersion = "claimed", "", "", 0
				candidate.retired, candidate.released = true, true
				write, err := makeOAuthRecoveryDeleteRequest(candidate)
				if err != nil {
					return err
				}
				binding, err := sc.MCPOAuthWriteBinding(write)
				if err != nil {
					return err
				}
				candidate.writeBinding = string(binding)
				w.newDelete = &candidate
			}
			selected = *w.newDelete
			if selected.validate() != nil || !sameOAuthCleanupSource(source, selected) || selected.process != s.owner.process.String() {
				return fail(f.Forbidden)
			}
			_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.oauth_requests
 (id,operation,phase,project_id,config_id,config_revision,connection_id,connection_version,flow_id,
 command_digest,user_id,session_id,credential_id,credential_version,secret_write_binding,
 io_retired,lease_released,retired_at,process_id)
 VALUES($1,'retire','claimed',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,true,true,clock_timestamp(),$14)`,
				selected.request, selected.project, selected.config, int64(selected.revision), selected.connection, int64(selected.version),
				selected.flow, selected.command, selected.user, selected.session, selected.credential, int64(selected.credentialVersion),
				selected.writeBinding, selected.process)
			if err != nil {
				return err
			}
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.oauth_flow_cleanups
 (flow_id,source_request_id,delete_request_id,created_by_claim_id,source_process_id,delete_process_id,
 project_id,config_id,config_revision,connection_id,connection_version,command_digest,user_id,session_id,
 credential_id,credential_version,secret_write_binding)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
			source.flow, source.request, selected.request, w.claim, source.process, selected.process,
			selected.project, selected.config, int64(selected.revision), selected.connection, int64(selected.version),
			selected.command, selected.user, selected.session, selected.credential, int64(selected.credentialVersion), selected.writeBinding)
		return err
	})
	if err := commit(result); err != nil {
		return oauthRecoveryFact{}, false, err
	}
	return selected, needed, nil
}

func (s *Service) cleanupStoppedOAuthFlow(ctx context.Context, recovery *oauthRecoveryUse) error {
	deletion, needed, err := s.selectOAuthFlowCleanup(ctx, recovery)
	if err != nil || !needed {
		return err
	}
	provider, ok := s.options.OAuth.Writes.(sc.MCPOAuthDeleteRecovery)
	if !ok || nilPort(provider) {
		return fail(f.DependencyUnbound)
	}
	owner := &oauthRecoveryDeleteOwner{recovery: recovery, deletion: deletion}
	if deletion.process != s.owner.process.String() {
		process, err := f.ParseID[oc.Process](deletion.process)
		if err != nil {
			return fail(f.InvalidState)
		}
		if err = s.owner.guard.ConfirmStopped(ctx, process); err != nil {
			return safeError(err)
		}
		owner.deleteStopped = true
	}
	request, err := oauthRecoveryDeleteRequest(deletion)
	if err != nil {
		return err
	}
	binding, _ := sc.MCPOAuthWriteBinding(request)
	w := &oauthWriteUse{service: s, request: request, requestID: deletion.request, mapping: binding, locks: recovery.locks, owner: owner}
	w.active.Store(true)
	defer w.active.Store(false)
	ctx = context.WithValue(ctx, oauthWriteKey{}, w)
	plan, err := provider.DiscoverMCPOAuthDeleteRecovery(ctx, request)
	if err != nil {
		return safeError(err)
	}
	var observed sc.MCPOAuthDeleteRecoveryObservation
	cause, _ := f.NewRecoveryCause("mcp.oauth.cleanup_observe", deletion.request, "")
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		var err error
		observed, err = provider.ObserveMCPOAuthDeleteRecoveryInTx(ctx, tx, plan)
		if err != nil || !observed.Found {
			return safeError(err)
		}
		return s.publishOAuthFlowCleanup(ctx, tx, owner, observed.Result)
	})
	if err = commit(result); err != nil {
		return err
	}
	if observed.Found {
		return nil
	}
	if nilPort(observed.Continuation) || deletion.phase == "complete" {
		return fail(f.InvalidState)
	}
	// The only continuation is issued by the actual Secret writer after its
	// original command locks, stopped-owner proof, receipt absence AND exact
	// still-existing credential/version have all been checked. This is the
	// same Delete, never a new key or a retry of remote OAuth I/O.
	prepared, err := provider.PrepareMCPOAuthDeleteRecovery(ctx, observed.Continuation)
	if err != nil {
		return safeError(err)
	}
	cause, _ = f.NewRecoveryCause("mcp.oauth.cleanup_apply", deletion.request, "")
	return commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, prepared.RequiredLocks()); err != nil {
			return err
		}
		receipt, err := provider.ApplyMCPOAuthDeleteRecoveryInTx(ctx, tx, prepared)
		if err != nil {
			return safeError(err)
		}
		if !receipt.Found {
			return fail(f.InvalidState)
		}
		return s.publishOAuthFlowCleanup(ctx, tx, owner, receipt.Result)
	}))
}

func (s *Service) publishOAuthFlowCleanup(ctx context.Context, tx f.Tx, owner *oauthRecoveryDeleteOwner, result sc.MutationResult) error {
	if err := owner.checkOAuthWriteOwnerInTx(ctx, tx); err != nil {
		return err
	}
	d := owner.deletion
	if result.Metadata.CredentialRef.Validate() != nil || result.Metadata.CredentialRef.Details().ID.String() != d.credential ||
		result.Metadata.CredentialRef.Details().Scope.Details().ProjectID != d.project ||
		result.Metadata.Purpose != sc.MCP || result.Metadata.Version != d.credentialVersion+1 || !result.Deleted {
		return fail(f.InvalidState)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return err
	}
	current, err := loadOAuthRecoveryFact(ctx, x, d.request)
	if err != nil {
		return err
	}
	if current.phase == "complete" {
		if current.resultCredential != d.credential || current.resultVersion != result.Metadata.Version {
			return fail(f.InvalidState)
		}
	} else if _, err = x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET phase='complete',result_credential_id=$2,result_credential_version=$3
 WHERE id=$1 AND operation='retire' AND process_id=$4 AND secret_write_binding=$5`, d.request, d.credential,
		int64(result.Metadata.Version), d.process, d.writeBinding); err != nil {
		return err
	}
	// Preserve the callback binding and terminal success, but never leave a
	// ready/exchanging flow claiming that its deleted verifier is usable.
	_, err = x.Exec(ctx, `UPDATE agenteam_mcp.oauth_flows SET phase='failed',consumed_at=COALESCE(consumed_at,clock_timestamp())
 WHERE id=$1 AND credential_id=$2 AND credential_version=$3 AND project_id=$4 AND config_id=$5
 AND config_revision=$6 AND connection_id=$7 AND connection_version=$8 AND phase IN ('ready','exchanging')`,
		d.flow, d.credential, int64(d.credentialVersion), d.project, d.config, int64(d.revision), d.connection, int64(d.version))
	return err
}
