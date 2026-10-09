package secret

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// Keep the legacy loader's closed Purpose.Valid check intact. Dedicated writes
// cannot convert a Model/Account credential into ProjectVariable ownership.
func loadProjectVariableMetadata(ctx context.Context, e postgres.SQLExecutor, ref sc.CredentialRef) (sc.Metadata, payloadID, error) {
	if ref.Validate() != nil || ref.Details().Scope.Details().Kind != i.ProjectScope {
		return sc.Metadata{}, payloadID{}, invalid()
	}
	var purpose, raw string
	var version int64
	d := ref.Details()
	err := e.QueryRow(ctx, `SELECT purpose,version,current_payload_id::text FROM agenteam_secret.secrets WHERE id=$1 AND scope='project' AND project_id=$2`, d.ID.String(), d.Scope.Details().ProjectID).Scan(&purpose, &version, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return sc.Metadata{}, payloadID{}, failure(NotFound, f.NotFound, nil)
	}
	if err != nil {
		return sc.Metadata{}, payloadID{}, unavailable(err)
	}
	if purpose != string(sc.ProjectVariable) {
		return sc.Metadata{}, payloadID{}, failure(AuthorizationRejected, f.Forbidden, nil)
	}
	payload, err := f.ParseID[payloadMarker](raw)
	v := f.Version(version)
	if err != nil || v.Validate() != nil {
		return sc.Metadata{}, payloadID{}, unavailable(err)
	}
	if err = checkAuditPayload(ctx, e, payload, d.Scope, valueOwner, d.ID.String()); err != nil {
		return sc.Metadata{}, payloadID{}, unavailable(err)
	}
	return sc.Metadata{CredentialRef: ref, Purpose: sc.ProjectVariable, Version: v}, payload, nil
}
