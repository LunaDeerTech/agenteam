// Package contract defines the Meeting sources of Human Inbox. It is a read
// projection; source actions continue to enter Meeting's own APIs.
package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"time"
)

type ItemIdentity struct{}
type ItemID = f.ID[ItemIdentity]
type Item struct {
	ID               ItemID        `json:"id"`
	ProjectID        i.ProjectID   `json:"project_id"`
	SourceType       string        `json:"source_type"`
	SourceID         string        `json:"source_id"`
	Context          SourceContext `json:"source_context"`
	ActionType       string        `json:"action_type"`
	Status           string        `json:"status"`
	Blocking         bool          `json:"blocking"`
	Priority         int           `json:"priority"`
	Dismissible      bool          `json:"dismissible"`
	Title            string        `json:"title"`
	Summary          string        `json:"summary"`
	SourceVersion    f.Version     `json:"source_version"`
	SourceOccurredAt time.Time     `json:"source_occurred_at"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
	ResolvedAt       *time.Time    `json:"resolved_at"`
}
type ListRequest struct {
	ProjectID i.ProjectID
	Status    string
	Limit     int
	Before    *ItemID
}
type Page struct {
	Items      []Item  `json:"items"`
	NextBefore *ItemID `json:"next_before,omitempty"`
}

// Source is a current read projection supplied by the source owner. It is not
// authority and does not mirror the Meeting business status machine.
type Source struct {
	ProjectID      i.ProjectID
	ID             string
	Context        SourceContext
	Version        f.Version
	Relevant       bool
	Open           bool
	Title, Summary string
	OccurredAt     time.Time
}

// SourceReadAuthority accepts only the Inbox's synchronous private projection
// or current Owner rebuild witness under the caller's complete lock union.
type SourceReadAuthority interface {
	RequireSourceReadInTx(context.Context, f.Tx, any, i.ProjectID, string, bool) error
}
type SourceProvider interface {
	ReadSourceInTx(context.Context, f.Tx, i.ProjectID, string) (Source, error)
	ScanSourcesInTx(context.Context, f.Tx, i.ProjectID) ([]Source, error)
}
