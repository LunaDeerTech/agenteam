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

// Receipt ownership survives replacement and deletion of the current value.
// Never derive this aggregate lock from a current canonical credential row.
func projectVariableReceiptCredential(ctx context.Context, e postgres.SQLExecutor, p envelope) (string, bool, error) {
	if p.ownerKind != projectVariableReceiptOwner || p.id.Validate() != nil || p.scope.Validate() != nil || p.scope.Details().Kind != i.ProjectScope {
		return "", false, invalid()
	}
	if _, err := f.ParseID[sc.ProjectVariableReceipt](p.ownerID); err != nil {
		return "", false, invalid()
	}
	var credential string
	err := e.QueryRow(ctx, `SELECT credential_id::text FROM agenteam_secret.project_variable_receipts WHERE id=$1 AND digest_payload_id=$2 AND project_id=$3`, p.ownerID, p.id.String(), p.scope.Details().ProjectID).Scan(&credential)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_payloads WHERE payload_id=$1)`, p.id.String()).Scan(&exists); err != nil {
			return "", false, unavailable(err)
		}
		if !exists {
			return "", false, nil
		}
		return "", false, unavailable(nil)
	}
	if err != nil {
		return "", false, unavailable(err)
	}
	if _, err = f.ParseID[sc.Credential](credential); err != nil {
		return "", false, unavailable(err)
	}
	return credential, true, nil
}

// The caller already owns Project EX and passed the persisted lifecycle gate.
// A malformed retained payload is an error, not permission to discard its
// receipt and lose reverse ownership. Both checks scan the same bounded head.
func cleanupProjectVariableReceipts(ctx context.Context, e postgres.SQLExecutor, project i.ProjectID) error {
	var malformed bool
	err := e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM (SELECT id,project_id,digest_payload_id FROM agenteam_secret.project_variable_receipts WHERE project_id=$1 ORDER BY id LIMIT 100) r LEFT JOIN agenteam_secret.secret_payloads p ON p.payload_id=r.digest_payload_id WHERE p.payload_id IS NULL OR p.scope<>'project' OR p.project_id IS DISTINCT FROM r.project_id OR p.owner_kind<>3 OR p.owner_id<>r.id)`, project.String()).Scan(&malformed)
	if err != nil {
		return unavailable(err)
	}
	if malformed {
		return unavailable(nil)
	}
	_, err = e.Exec(ctx, `WITH removed AS (DELETE FROM agenteam_secret.project_variable_receipts WHERE id IN (SELECT id FROM agenteam_secret.project_variable_receipts WHERE project_id=$1 ORDER BY id LIMIT 100) RETURNING id,project_id,digest_payload_id) DELETE FROM agenteam_secret.secret_payloads p USING removed r WHERE p.payload_id=r.digest_payload_id AND p.scope='project' AND p.project_id=r.project_id AND p.owner_kind=3 AND p.owner_id=r.id`, project.String())
	if err != nil {
		return unavailable(err)
	}
	return nil
}
