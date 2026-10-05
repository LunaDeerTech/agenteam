package contract_test

import (
	"context"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// Compile the external consumer against the real typed Tx/Entry/AppendKey.
// This checks only the new interface boundary, not production authorization.
type projectFactConsumer func(context.Context, foundation.Tx, audit.Entry, audit.AppendKey) error

func (check projectFactConsumer) CheckProjectAuditInTx(ctx context.Context, tx foundation.Tx, entry audit.Entry, key audit.AppendKey) error {
	return check(ctx, tx, entry, key)
}

var _ audit.ProjectFactAuthority = projectFactConsumer(nil)
