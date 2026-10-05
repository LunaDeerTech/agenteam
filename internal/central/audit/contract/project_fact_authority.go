package contract

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// ProjectFactAuthority checks facts owned by one registered Project producer.
// Project gate/current actor authorization precedes this check. The provider
// verifies its own typed Entry/AppendKey against real facts in the caller's
// active Tx; service name plus an arbitrary cause is never sufficient.
//
// A provider must not acquire omitted locks, open or commit another Tx, inspect
// Project's private tables, or authorize a different producer. Existing
// transient audit causes may use package-private exact-Tx witnesses together
// with real lease/phase facts; this interface exposes no witness minting port.
type ProjectFactAuthority interface {
	CheckProjectAuditInTx(context.Context, foundation.Tx, Entry, AppendKey) error
}
