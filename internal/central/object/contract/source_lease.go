package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// SourceIssuer is private to one trusted SourceReads adapter. A caller may
// construct its own issuer, but cannot retrieve or impersonate an existing one.
type SourceIssuer struct{ data func() foundation.Tx }

func NewSourceIssuer() SourceIssuer {
	id := foundation.NewTx()
	return SourceIssuer{func() foundation.Tx { return id }}
}

type SourceLease struct{ data func() sourceLeaseData }
type sourceLeaseData struct {
	issuer SourceIssuer
	id     LeaseID
}

// NewSourceLease is only the adapter's data constructor. It is not permission:
// every operation must also find this ID in the issuing adapter's registry,
// bound to its actual origin Tx, Process, Actor and complete resolved source.
func NewSourceLease(issuer SourceIssuer, id LeaseID) (SourceLease, error) {
	if issuer.data == nil || id.Validate() != nil {
		return SourceLease{}, bad()
	}
	d := sourceLeaseData{issuer, id}
	return SourceLease{func() sourceLeaseData { return d }}, nil
}
func (s SourceLease) Validate() error {
	if s.data == nil {
		return bad()
	}
	return nil
}
func (s SourceLease) ID() LeaseID {
	if s.data == nil {
		return LeaseID{}
	}
	return s.data().id
}
func (s SourceLease) IssuedBy(issuer SourceIssuer) bool {
	return s.data != nil && issuer.data != nil && s.data().issuer.data() != (foundation.Tx{}) && s.data().issuer.data() == issuer.data()
}
func (s SourceIssuer) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "source_issuer") }
func (s SourceIssuer) MarshalJSON() ([]byte, error) { return []byte(`"source_issuer"`), nil }
func (*SourceIssuer) UnmarshalJSON([]byte) error    { return bad() }
func (s SourceIssuer) LogValue() slog.Value         { return slog.StringValue("source_issuer") }
func (s SourceLease) Format(w fmt.State, _ rune)    { _, _ = io.WriteString(w, "source_lease") }
func (s SourceLease) MarshalJSON() ([]byte, error)  { return []byte(`"source_lease"`), nil }
func (*SourceLease) UnmarshalJSON([]byte) error     { return bad() }
func (s SourceLease) LogValue() slog.Value          { return slog.StringValue("source_lease") }

// SourceReads composes durable source protection with the caller's command Tx.
// Acquire does no external I/O. Open must verify exact committed lease facts in
// another confirmed short Tx before its single GET; a handle is not a commit
// receipt. Cancel is called outside the origin Tx and only releases protection
// after actual I/O has closed and joined. Unknown outcomes retain checkpoints.
type SourceReads interface {
	AcquireSourceInTx(context.Context, foundation.Tx, identity.Actor, ResolvedSource, AccessLockPlan, LockedAccess) (SourceLease, error)
	OpenLeasedSource(context.Context, identity.Actor, SourceLease) (*ObjectReader, error)
	CancelSourceLease(context.Context, SourceLease) error
}
