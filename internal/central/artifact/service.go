package artifact

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type serviceState struct {
	store    Store
	owners   *OwnerProvider
	objects  oc.Objects
	uploads  oc.Uploads
	cleaner  oc.Cleaner
	reads    oc.SourceReads
	resolver oc.SourceResolver
	audit    au.Appender
	cursors  cursor.Keyring
	mu       sync.Mutex
	leases   map[foundation.Digest]oc.SourceLease
}
type Service struct{ data func() *serviceState }

func New(store Store, owners *OwnerProvider, objects oc.Objects, uploads oc.Uploads, cleaner oc.Cleaner, reads oc.SourceReads, resolver oc.SourceResolver, auditing au.Appender, cursors cursor.Keyring) (*Service, error) {
	if nilPort(store) || owners == nil || owners.data == nil || nilPort(objects) || nilPort(uploads) || nilPort(cleaner) || nilPort(auditing) || cursors.Validate() != nil {
		return nil, invalid()
	}
	// Source ports can be explicitly unbound in the composition root; source
	// creation rejects that state. Inline/upload never invent source providers.
	r := &serviceState{store: store, owners: owners, objects: objects, uploads: uploads, cleaner: cleaner, reads: reads, resolver: resolver, audit: auditing, cursors: cursors, leases: map[foundation.Digest]oc.SourceLease{}}
	return &Service{func() *serviceState { return r }}, nil
}
func (s *Service) state() *serviceState      { return s.data() }
func (Service) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "artifact_service") }
func (Service) MarshalJSON() ([]byte, error) { return []byte(`"artifact_service"`), nil }
func (*Service) UnmarshalJSON([]byte) error  { return invalid() }
func (Service) LogValue() slog.Value         { return slog.StringValue("artifact_service") }
func rejected(err error) foundation.CommitResult {
	f, ok := portError(err).(*foundation.Fault)
	if !ok {
		f = foundation.NewFault(foundation.DependencyUnavailable, foundation.NotCommitted)
	}
	return foundation.NotCommittedResult(f)
}

// within collects every domain and object lock before the Tx. No callback may
// add locks, call external I/O or retry the transaction after a mapping change.
func (s *Service) within(ctx context.Context, subject ac.AccessSubject, intent identity.AccessIntent, cause foundation.TransactionCause, plans []oc.AccessLockPlan, extra []foundation.LockRequest, fn func(context.Context, foundation.Tx, oc.LockedAccess) error) foundation.CommitResult {
	r := s.state()
	if err := r.owners.bound(); err != nil {
		return rejected(err)
	}
	if subject.Validate() != nil || subject.Maintenance {
		return rejected(invalid())
	}
	deps, err := r.owners.state().authority.Discover(ctx, subject)
	if err != nil {
		return rejected(err)
	}
	if deps.Validate() != nil {
		return rejected(unavailable(nil))
	}
	extra = append(append([]foundation.LockRequest{}, extra...), deps.Locks()...)
	return r.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		var locked oc.LockedAccess
		var err error
		if len(plans) > 0 {
			locked, err = r.objects.AcquireAccessPlansInTx(ctx, tx, plans, extra)
		} else {
			err = r.store.AcquireAll(ctx, tx, extra)
		}
		if err != nil {
			return portError(err)
		}
		current, err := r.owners.state().authority.DiscoverInTx(ctx, tx, subject)
		if err != nil {
			return portError(err)
		}
		if !current.Equal(deps) {
			return foundation.NewFault(foundation.ResourceBusy, foundation.NotCommitted)
		}
		for _, plan := range plans {
			if err = r.objects.ValidateAccessPlanInTx(ctx, tx, plan.Details().Request, plan, locked); err != nil {
				return err
			}
		}
		if err = r.owners.authorize(ctx, tx, subject, intent); err != nil {
			return err
		}
		return fn(ctx, tx, locked)
	})
}
func createLocks(c foundation.CommandIdentity, artifact string) []foundation.LockRequest {
	key, _ := foundation.CommandLock(c)
	locks := []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}
	if artifact != "" {
		locks = append(locks, artifactLock(artifact))
	}
	return locks
}
func makeOwnerRequest(actor identity.Actor, owner oc.ObjectOwner, op oc.AccessOperation, d oc.AccessRequestDetails) (oc.AccessRequest, error) {
	d.Actor = actor
	d.Owner = owner
	d.Operation = op
	d.Intent = identity.Mutate
	if op == oc.LookupAccess {
		d.Intent = identity.Read
	}
	if op == oc.CancelAccess || op == oc.ReleaseAccess {
		d.Intent = identity.Converge
	}
	return oc.NewOwnerAccess(d)
}
func (s *Service) ownerPlan(ctx context.Context, actor identity.Actor, owner oc.ObjectOwner, op oc.AccessOperation, d oc.AccessRequestDetails) (oc.AccessLockPlan, error) {
	req, err := makeOwnerRequest(actor, owner, op, d)
	if err != nil {
		return oc.AccessLockPlan{}, err
	}
	return s.state().objects.DiscoverAccess(ctx, req)
}
func (s *Service) sourcePlan(ctx context.Context, actor identity.Actor, source oc.ResolvedSource, op oc.AccessOperation) (oc.AccessLockPlan, error) {
	req, err := oc.NewSourceAccess(oc.AccessRequestDetails{Operation: op, Actor: actor, Source: source})
	if err != nil {
		return oc.AccessLockPlan{}, err
	}
	return s.state().objects.DiscoverAccess(ctx, req)
}

// Lookup is deliberately target-only. In particular, no Resolve, source plan,
// source authorization or stream is consulted for a completed command.
func (s *Service) lookup(ctx context.Context, req createRequest) (commandRow, *ac.Metadata, error) {
	r := s.state()
	before, found, err := loadCommand(ctx, r.store, req.identity)
	if err != nil {
		return commandRow{}, nil, err
	}
	var artifact string
	if found {
		artifact = before.ref.ArtifactID.String()
	}
	cause, _ := foundation.NewCommandsCause(req.identity)
	var current commandRow
	var out *ac.Metadata
	result := s.within(ctx, invocationSubject(req.invocation), identity.Read, cause, nil, createLocks(req.identity, artifact), func(ctx context.Context, tx foundation.Tx, _ oc.LockedAccess) error {
		e, err := r.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		var ok bool
		current, ok, err = loadCommand(ctx, e, req.identity)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if !found || current.ref.ArtifactID != before.ref.ArtifactID {
			return failure(foundation.ResourceBusy, nil)
		}
		if _, err = r.owners.AuthorizeOwnerInTx(ctx, tx, req.invocation.Details().Actor, artifactOwner(current.ref), identity.Read); err != nil {
			return err
		}
		if err = matchesCommand(current, req); err != nil {
			return err
		}
		if current.state == "deleted" {
			return foundation.NewFault(foundation.ResourceDeleted, foundation.Committed)
		}
		if current.state == "completed" {
			row, exists, err := loadArtifact(ctx, e, current.ref.ArtifactID.String())
			if err != nil {
				return err
			}
			if !exists {
				return foundation.NewFault(foundation.ResourceDeleted, foundation.Committed)
			}
			if row.meta.Details().Reference != current.ref || row.meta.Details().Object.ID != current.object {
				return unavailable(nil)
			}
			v := row.meta
			out = &v
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return commandRow{}, nil, err
	}
	return current, out, nil
}
