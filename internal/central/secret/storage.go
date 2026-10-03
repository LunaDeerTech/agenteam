package secret

import (
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

const payloadColumns = `payload_id::text,scope,coalesce(project_id::text,''),owner_kind,owner_id::text,format,algorithm,data_nonce,ciphertext,wrap_nonce,wrapped_dek,master_version,wrap_revision`

type scanner interface{ Scan(...any) error }

func scanPayload(row scanner) (envelope, error) {
	var p envelope
	var id, scope, project string
	var kind int16
	var version, revision int64
	if err := row.Scan(&id, &scope, &project, &kind, &p.ownerID, &p.format, &p.algorithm, &p.dataNonce, &p.ciphertext, &p.wrapNonce, &p.wrappedDEK, &version, &revision); err != nil {
		return envelope{}, err
	}
	parsed, err := foundation.ParseID[payloadMarker](id)
	if err != nil {
		return envelope{}, unavailable(err)
	}
	p.id = parsed
	p.scope, err = decodeScope(scope, project)
	if err != nil {
		return envelope{}, err
	}
	p.ownerKind = ownerKind(kind)
	p.masterVersion = foundation.Version(version)
	p.wrapRevision = foundation.Version(revision)
	if _, err := wrapAAD(p); err != nil || p.wrapRevision.Validate() != nil || len(p.wrappedDEK) != 48 || !validMasterNonce(p.wrapNonce) {
		return envelope{}, failure(DecryptFailed, foundation.DependencyUnavailable, nil)
	}
	return p, nil
}
func decodeScope(scope, project string) (identity.Scope, error) {
	if scope == string(identity.System) && project == "" {
		return identity.SystemScope(), nil
	}
	if scope != string(identity.ProjectScope) {
		return identity.Scope{}, unavailable(nil)
	}
	id, err := foundation.ParseID[identity.Project](project)
	if err != nil {
		return identity.Scope{}, unavailable(err)
	}
	result, err := identity.InProject(id)
	if err != nil {
		return identity.Scope{}, unavailable(err)
	}
	return result, nil
}
func insertPayload(ctx context.Context, e postgres.SQLExecutor, p envelope) error {
	_, err := e.Exec(ctx, `INSERT INTO agenteam_secret.secret_payloads(payload_id,scope,project_id,owner_kind,owner_id,format,algorithm,data_nonce,ciphertext,wrap_nonce,wrapped_dek,master_version,wrap_revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, p.id.String(), string(p.scope.Details().Kind), null(p.scope.Details().ProjectID), int16(p.ownerKind), p.ownerID, p.format, p.algorithm, p.dataNonce, p.ciphertext, p.wrapNonce, p.wrappedDEK, int64(p.masterVersion), int64(p.wrapRevision))
	if err != nil {
		return unavailable(err)
	}
	return nil
}
func loadPayload(ctx context.Context, e postgres.SQLExecutor, id payloadID) (envelope, error) {
	p, err := scanPayload(e.QueryRow(ctx, `SELECT `+payloadColumns+` FROM agenteam_secret.secret_payloads WHERE payload_id=$1`, id.String()))
	if err != nil {
		return envelope{}, unavailable(err)
	}
	return p, nil
}
func loadMetadata(ctx context.Context, e postgres.SQLExecutor, ref sc.CredentialRef) (sc.Metadata, payloadID, error) {
	var purpose string
	var version int64
	var raw string
	d := ref.Details()
	err := e.QueryRow(ctx, `SELECT purpose,version,current_payload_id::text FROM agenteam_secret.secrets WHERE id=$1 AND scope=$2 AND scope_key=$3`, d.ID.String(), string(d.Scope.Details().Kind), scopeKey(d.Scope)).Scan(&purpose, &version, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return sc.Metadata{}, payloadID{}, failure(NotFound, foundation.NotFound, nil)
	}
	if err != nil {
		return sc.Metadata{}, payloadID{}, unavailable(err)
	}
	p, err := foundation.ParseID[payloadMarker](raw)
	v := foundation.Version(version)
	if err != nil || !sc.Purpose(purpose).Valid() || v.Validate() != nil {
		return sc.Metadata{}, payloadID{}, unavailable(err)
	}
	return sc.Metadata{CredentialRef: ref, Purpose: sc.Purpose(purpose), Version: v}, p, nil
}
func (s *Service) control(ctx context.Context, e postgres.SQLExecutor) (foundation.Version, foundation.Version, error) {
	var version, epoch int64
	if err := e.QueryRow(ctx, `SELECT current_write_version,write_epoch FROM agenteam_secret.secret_control WHERE singleton`).Scan(&version, &epoch); err != nil {
		return 0, 0, unavailable(err)
	}
	v, g := foundation.Version(version), foundation.Version(epoch)
	if v.Validate() != nil || g.Validate() != nil {
		return 0, 0, unavailable(nil)
	}
	return v, g, nil
}
