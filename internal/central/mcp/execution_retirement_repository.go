package mcp

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Retirement reads only the retained execution rows and immutable shadows.
// Removing a live Config/Connection cannot erase the credential release facts.
type executionRuntimeLease struct {
	RuntimeBindingID string
	ConnectionID     string
	CredentialID     string
	LeaseID          sc.LeaseID
	Retired          bool
}

func loadExecutionRuntimeLeases(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, execution id.ExecutionID) ([]executionRuntimeLease, error) {
	if ctx == nil || project.Validate() != nil || execution.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	rows, err := x.Query(ctx, `SELECT l.runtime_binding_id,l.connection_id,COALESCE(r.credential_id,''),COALESCE(l.credential_lease_id,''),l.retired FROM agenteam_mcp.runtime_leases l JOIN agenteam_mcp.runtime_bindings r ON r.id=l.runtime_binding_id AND r.project_id=l.project_id AND r.connection_id=l.connection_id WHERE l.execution_id=$1 AND l.project_id=$2 ORDER BY l.runtime_binding_id COLLATE "C" LIMIT 193`, execution.String(), project.String())
	if err != nil {
		return nil, safeError(err)
	}
	defer rows.Close()
	out := []executionRuntimeLease{}
	leases := map[sc.LeaseID]string{}
	credentials := map[string]sc.LeaseID{}
	for rows.Next() {
		var row executionRuntimeLease
		var lease string
		if err = rows.Scan(&row.RuntimeBindingID, &row.ConnectionID, &row.CredentialID, &lease, &row.Retired); err != nil {
			return nil, safeError(err)
		}
		if len(out) >= 192 {
			return nil, fail(f.PayloadTooLarge)
		}
		for _, value := range []string{row.RuntimeBindingID, row.ConnectionID} {
			if _, err = f.ParseID[struct{}](value); err != nil {
				return nil, fail(f.InvalidState)
			}
		}
		if row.CredentialID == "" {
			if lease != "" {
				return nil, fail(f.InvalidState)
			}
		} else {
			if _, err = f.ParseID[sc.Credential](row.CredentialID); err != nil {
				return nil, fail(f.InvalidState)
			}
			row.LeaseID, err = f.ParseID[sc.Lease](lease)
			if err != nil {
				return nil, fail(f.InvalidState)
			}
			if prior, ok := leases[row.LeaseID]; ok && prior != row.CredentialID {
				return nil, fail(f.InvalidState)
			}
			if prior, ok := credentials[row.CredentialID]; ok && prior != row.LeaseID {
				return nil, fail(f.InvalidState)
			}
			leases[row.LeaseID], credentials[row.CredentialID] = row.CredentialID, row.LeaseID
		}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(err)
	}
	return out, nil
}

func sameRetirementLeaseSet(a, b []executionRuntimeLease) bool {
	if len(a) != len(b) {
		return false
	}
	for n := range a {
		left, right := a[n], b[n]
		left.Retired, right.Retired = false, false
		if left != right {
			return false
		}
	}
	return true
}
