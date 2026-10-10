package secret

import (
	"context"
	"crypto/subtle"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Service) projectVariableAuthority() (sc.ProjectVariableWriteAuthority, error) {
	if s == nil || s.data == nil {
		return nil, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	state := s.state()
	if state == nil || lookupNil(state.store) || lookupNil(state.auth.ProjectVariables) {
		return nil, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	return state.auth.ProjectVariables, nil
}

// InTx paths never acquire additional locks or open an auxiliary transaction.
// Pure CheckPlan cannot replace the subsequent current-authority CheckInTx.
func (s *Service) projectVariableInTx(ctx context.Context, tx f.Tx, request sc.ProjectVariableWriteRequest, plan sc.ProjectVariableWritePlan, stage sc.ProjectVariableWriteStage) (postgres.SQLExecutor, error) {
	if ctx == nil || request.Validate() != nil || !stage.Valid() {
		return nil, invalid()
	}
	authority, err := s.projectVariableAuthority()
	if err != nil {
		return nil, err
	}
	if err = authority.CheckPlan(request, plan); err != nil {
		return nil, lookupError(err)
	}
	state := s.state()
	e, err := state.store.InTx(tx)
	if err != nil {
		return nil, lookupError(err)
	}
	if lookupNil(e) {
		return nil, unavailable(nil)
	}
	locks, err := plan.RequiredLocks()
	if err != nil {
		return nil, invalid()
	}
	if err = state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, lookupError(err)
	}
	if err = authority.CheckInTx(ctx, tx, request, plan, stage); err != nil {
		return nil, lookupError(err)
	}
	if err = ctx.Err(); err != nil {
		return nil, unavailable(err)
	}
	return e, nil
}

type projectVariableStoredReceipt struct {
	observation sc.ProjectVariableWriteObservation
	payload     payloadID
}

// This query is reached only after the caller's ReceiptRead stage and actual
// full lock check. Absence is not a commit decision or a permission to create.
func loadProjectVariableReceipt(ctx context.Context, e postgres.SQLExecutor, request sc.ProjectVariableWriteRequest, plan sc.ProjectVariableWritePlan) (projectVariableStoredReceipt, error) {
	r := request.Fields()
	digest, err := cursor.Digest([]byte(r.Identity.Canonical()))
	if err != nil {
		return projectVariableStoredReceipt{}, invalid()
	}
	var receipt, variable, user, command, credential, effect, payload string
	var expected pgtype.Int8
	var version int64
	var deleted bool
	err = e.QueryRow(ctx, `SELECT id::text,variable_id::text,user_id::text,command_kind,external_expected_version,credential_id::text,effect,result_version,deleted,digest_payload_id::text FROM agenteam_secret.project_variable_receipts WHERE project_id=$1 AND command_digest=$2`, r.ProjectID.String(), string(digest)).Scan(&receipt, &variable, &user, &command, &expected, &credential, &effect, &version, &deleted, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		basis, basisErr := plan.Basis()
		if basisErr != nil || basis.Receipt.Observed() {
			return projectVariableStoredReceipt{}, unavailable(nil)
		}
		return projectVariableStoredReceipt{observation: sc.ProjectVariableWriteNotObserved()}, nil
	}
	if err != nil {
		return projectVariableStoredReceipt{}, unavailable(err)
	}
	if command != r.Identity.Command() || variable != r.VariableID.String() || user != r.Actor.Details().UserID || expected.Valid != (r.ExpectedVersion != nil) || expected.Valid && expected.Int64 != int64(*r.ExpectedVersion) {
		return projectVariableStoredReceipt{}, failure(KeyReused, f.IdempotencyKeyReused, nil)
	}
	receiptID, err := f.ParseID[sc.ProjectVariableReceipt](receipt)
	if err != nil {
		return projectVariableStoredReceipt{}, unavailable(err)
	}
	variableID, err := f.ParseID[i.ProjectVariable](variable)
	if err != nil {
		return projectVariableStoredReceipt{}, unavailable(err)
	}
	userID, err := f.ParseID[i.User](user)
	if err != nil {
		return projectVariableStoredReceipt{}, unavailable(err)
	}
	credentialID, err := f.ParseID[sc.Credential](credential)
	if err != nil {
		return projectVariableStoredReceipt{}, unavailable(err)
	}
	payloadID, err := f.ParseID[payloadMarker](payload)
	if err != nil {
		return projectVariableStoredReceipt{}, unavailable(err)
	}
	scope, err := i.InProject(r.ProjectID)
	if err != nil {
		return projectVariableStoredReceipt{}, invalid()
	}
	ref, err := sc.NewCredentialRef(credentialID, scope)
	if err != nil {
		return projectVariableStoredReceipt{}, unavailable(err)
	}
	basis, err := plan.Basis()
	if err != nil {
		return projectVariableStoredReceipt{}, invalid()
	}
	if !ref.Equal(basis.Ref) {
		return projectVariableStoredReceipt{}, failure(PreparationRequired, f.ResourceBusy, nil)
	}
	fields := sc.ProjectVariableWriteResultFields{ReceiptID: receiptID, ProjectID: r.ProjectID, VariableID: variableID, UserID: userID, Identity: r.Identity, Kind: r.Kind, ExpectedVersion: r.ExpectedVersion, Ref: ref, Effect: sc.ProjectVariableEffect(effect), Version: f.Version(version), Deleted: deleted}
	observation, err := sc.NewProjectVariableWriteObservation(fields)
	if err != nil {
		return projectVariableStoredReceipt{}, unavailable(err)
	}
	if basis.Receipt.Observed() {
		before, _ := basis.Receipt.Result()
		if before.ReceiptID != receiptID || before.Effect != fields.Effect || before.Version != fields.Version || before.Deleted != fields.Deleted {
			return projectVariableStoredReceipt{}, unavailable(nil)
		}
	}
	if err = checkAuditPayload(ctx, e, payloadID, scope, projectVariableReceiptOwner, receiptID.String()); err != nil {
		return projectVariableStoredReceipt{}, unavailable(err)
	}
	return projectVariableStoredReceipt{observation, payloadID}, nil
}

func (s *Service) LookupProjectVariableWriteInTx(ctx context.Context, tx f.Tx, request sc.ProjectVariableWriteRequest, plan sc.ProjectVariableWritePlan) (sc.ProjectVariableWriteObservation, error) {
	e, err := s.projectVariableInTx(ctx, tx, request, plan, sc.ProjectVariableReceiptRead)
	if err != nil {
		return sc.ProjectVariableWriteObservation{}, err
	}
	receipt, err := loadProjectVariableReceipt(ctx, e, request, plan)
	if err != nil {
		return sc.ProjectVariableWriteObservation{}, err
	}
	if err = ctx.Err(); err != nil {
		return sc.ProjectVariableWriteObservation{}, unavailable(err)
	}
	return receipt.observation, nil
}

func (s *Service) MatchProjectVariableIntentInTx(ctx context.Context, tx f.Tx, raw sc.PreparedProjectVariableWrite) (sc.ProjectVariableWriteObservation, error) {
	prepared, err := s.lockProjectVariablePrepared(raw)
	if err != nil {
		return sc.ProjectVariableWriteObservation{}, err
	}
	defer prepared.mu.Unlock()
	meta, err := prepared.preparation.Fields()
	if err != nil {
		return sc.ProjectVariableWriteObservation{}, invalid()
	}
	e, err := s.projectVariableInTx(ctx, tx, meta.Request, prepared.plan, sc.ProjectVariableReceiptRead)
	if err != nil {
		return sc.ProjectVariableWriteObservation{}, err
	}
	stored, err := loadProjectVariableReceipt(ctx, e, meta.Request, prepared.plan)
	if err != nil {
		return sc.ProjectVariableWriteObservation{}, err
	}
	if !stored.observation.Observed() {
		return stored.observation, nil
	}
	result, _ := stored.observation.Result()
	envelope, err := loadPayload(ctx, e, stored.payload)
	if err != nil {
		return sc.ProjectVariableWriteObservation{}, err
	}
	if envelope.ownerKind != projectVariableReceiptOwner || envelope.ownerID != result.ReceiptID.String() || !envelope.scope.Equal(result.Ref.Details().Scope) {
		return sc.ProjectVariableWriteObservation{}, unavailable(nil)
	}
	old, err := openEnvelope(s.state().keys, envelope)
	if err != nil {
		return sc.ProjectVariableWriteObservation{}, err
	}
	defer clear(old)
	candidate, err := openEnvelope(s.state().keys, prepared.receipt)
	if err != nil {
		return sc.ProjectVariableWriteObservation{}, err
	}
	defer clear(candidate)
	if len(old) != 32 || len(candidate) != 32 || subtle.ConstantTimeCompare(old, candidate) != 1 {
		return sc.ProjectVariableWriteObservation{}, failure(KeyReused, f.IdempotencyKeyReused, nil)
	}
	if err = ctx.Err(); err != nil {
		return sc.ProjectVariableWriteObservation{}, unavailable(err)
	}
	return stored.observation, nil
}
