package secret

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type projectVariableAuditWitnessKey struct{}

// This call-local proof contains neither prepared state, keyring, sealed bytes
// nor material. Only Apply, after the native writes, supplies it to native Audit.
type projectVariableAuditWitness struct {
	store                        Store
	tx                           f.Tx
	request                      sc.ProjectVariableWriteRequest
	result                       sc.ProjectVariableWriteObservation
	prior                        sc.Metadata
	expected                     f.Version
	receiptPayload, valuePayload payloadID
	locks                        []f.LockRequest
	entry                        ac.Entry
	key                          ac.AppendKey
}

func (projectVariableAuditWitness) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "secret_project_variable_audit_witness")
}

func projectVariableAuditEntry(request sc.ProjectVariableWriteRequest, result sc.ProjectVariableWriteObservation) (ac.Entry, ac.AppendKey, error) {
	if request.Validate() != nil || result.Validate() != nil || !result.Observed() {
		return ac.Entry{}, ac.AppendKey{}, projectAuditDenied()
	}
	r, err := result.Result()
	if err != nil {
		return ac.Entry{}, ac.AppendKey{}, projectAuditDenied()
	}
	action, changed := ac.SecretCreate, []ac.ChangedField{ac.ValueChanged, ac.PurposeChanged}
	switch r.Effect {
	case sc.ProjectVariableCreated:
	case sc.ProjectVariableReplaced:
		action, changed = ac.SecretUpdate, []ac.ChangedField{ac.ValueChanged}
	case sc.ProjectVariableDeleted:
		action, changed = ac.SecretDelete, nil
	default:
		return ac.Entry{}, ac.AppendKey{}, projectAuditDenied()
	}
	metadata, err := ac.SecretMutationMetadata(action, r.Version, changed)
	if err != nil {
		return ac.Entry{}, ac.AppendKey{}, projectAuditDenied()
	}
	resource, err := ac.NewResource(ac.SecretResource, r.Ref.Details().ID.String())
	if err != nil {
		return ac.Entry{}, ac.AppendKey{}, projectAuditDenied()
	}
	entry, err := ac.NewEntry(ac.EntryFields{Scope: r.Ref.Details().Scope, Actor: request.Fields().Actor, Action: action, Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		return ac.Entry{}, ac.AppendKey{}, projectAuditDenied()
	}
	key, err := audit.CommandAppendKey(ac.SecretProducer, request.Fields().Identity, 0)
	return entry, key, err
}

func (a *ProjectAuditAuthority) checkProjectVariableAudit(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey, raw any) error {
	w, ok := raw.(projectVariableAuditWitness)
	store := a.data()
	if !ok || ctx.Value(projectAuditWitnessKey{}) != nil || !sameProjectAuditStore(store, w.store) || w.tx != tx || !sameProjectAuditEntry(w.entry, entry) || w.key.Validate() != nil || w.key.Details() != key.Details() || w.request.Validate() != nil || w.result.Validate() != nil || !w.result.Observed() || w.receiptPayload.Validate() != nil || len(w.locks) == 0 {
		return projectAuditDenied()
	}
	r, err := w.result.Result()
	if err != nil {
		return projectAuditDenied()
	}
	q := w.request.Fields()
	if r.ProjectID != q.ProjectID || r.VariableID != q.VariableID || r.UserID.String() != q.Actor.Details().UserID || r.Identity.Canonical() != q.Identity.Canonical() || r.Kind != q.Kind || (r.ExpectedVersion == nil) != (q.ExpectedVersion == nil) || r.ExpectedVersion != nil && *r.ExpectedVersion != *q.ExpectedVersion {
		return projectAuditDenied()
	}
	if r.Effect == sc.ProjectVariableCreated {
		if w.expected != 0 || w.prior.CredentialRef.Validate() == nil || w.prior.Version != 0 || w.prior.Purpose != "" {
			return projectAuditDenied()
		}
	} else if r.Effect == sc.ProjectVariableReplaced || r.Effect == sc.ProjectVariableDeleted {
		if !w.prior.CredentialRef.Equal(r.Ref) || w.prior.Purpose != sc.ProjectVariable || w.expected.Validate() != nil || w.prior.Version != w.expected || r.Version <= w.expected || r.Version-w.expected != 1 {
			return projectAuditDenied()
		}
	} else {
		return projectAuditDenied()
	}
	expectedEntry, expectedKey, err := projectVariableAuditEntry(w.request, w.result)
	if err != nil || !sameProjectAuditEntry(expectedEntry, entry) || expectedKey.Details() != key.Details() {
		return projectAuditDenied()
	}
	minimum, err := sc.ProjectVariableWriteLocks(w.request, r.Ref)
	if err != nil {
		return projectAuditDenied()
	}
	if err = store.RequireHeldLocks(ctx, tx, minimum); err != nil {
		return unavailable(err)
	}
	if err = store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return unavailable(err)
	}
	x, err := store.InTx(tx)
	if err != nil || lookupNil(x) {
		return unavailable(err)
	}
	var project, variable, user, command, kind, credential, effect, payload string
	var expected pgtype.Int8
	var version int64
	var deleted bool
	err = x.QueryRow(ctx, `SELECT project_id::text,variable_id::text,user_id::text,command_digest,command_kind,external_expected_version,credential_id::text,effect,result_version,deleted,digest_payload_id::text FROM agenteam_secret.project_variable_receipts WHERE id=$1`, r.ReceiptID.String()).Scan(&project, &variable, &user, &command, &kind, &expected, &credential, &effect, &version, &deleted, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return projectAuditDenied()
	}
	if err != nil {
		return unavailable(err)
	}
	if project != r.ProjectID.String() || variable != r.VariableID.String() || user != r.UserID.String() || command != key.Details().CauseRef || kind != r.Identity.Command() || expected.Valid != (r.ExpectedVersion != nil) || expected.Valid && expected.Int64 != int64(*r.ExpectedVersion) || credential != r.Ref.Details().ID.String() || effect != string(r.Effect) || version != int64(r.Version) || deleted != r.Deleted || payload != w.receiptPayload.String() {
		return projectAuditDenied()
	}
	if err = checkAuditPayload(ctx, x, w.receiptPayload, r.Ref.Details().Scope, projectVariableReceiptOwner, r.ReceiptID.String()); err != nil {
		return err
	}
	if r.Deleted {
		var exists bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secrets WHERE id=$1)`, r.Ref.Details().ID.String()).Scan(&exists); err != nil {
			return unavailable(err)
		}
		if exists || w.valuePayload.Validate() == nil {
			return projectAuditDenied()
		}
	} else {
		current, payload, err := loadProjectVariableMetadata(ctx, x, r.Ref)
		if err != nil {
			return err
		}
		if current.Version != r.Version || payload != w.valuePayload {
			return projectAuditDenied()
		}
	}
	if err = ctx.Err(); err != nil {
		return unavailable(err)
	}
	return nil
}

func (s *Service) appendProjectVariableAudit(ctx context.Context, tx f.Tx, p *projectVariablePreparedState, prior sc.Metadata, result sc.ProjectVariableWriteObservation) error {
	meta, err := p.preparation.Fields()
	if err != nil {
		return invalid()
	}
	basis, err := p.plan.Basis()
	if err != nil {
		return invalid()
	}
	entry, key, err := projectVariableAuditEntry(meta.Request, result)
	if err != nil {
		return err
	}
	state := s.state()
	w := projectVariableAuditWitness{store: state.store, tx: tx, request: meta.Request, result: result, prior: prior, expected: basis.CredentialVersion, receiptPayload: p.receipt.id, valuePayload: p.value.id, locks: append([]f.LockRequest(nil), p.locks...), entry: entry, key: key}
	if _, err = state.audit.AppendInTx(context.WithValue(ctx, projectVariableAuditWitnessKey{}, w), tx, entry, key); err != nil {
		return unavailable(err)
	}
	return nil
}
