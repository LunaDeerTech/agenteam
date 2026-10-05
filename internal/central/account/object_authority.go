package account

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// AvatarAuthority exposes only the owning account domain's object decisions.
// It is separate from Authority's Secret lease interface and shares its facts.
type AvatarAuthority struct{ data func() *Service }

func NewAvatarAuthority(core *Service) (*AvatarAuthority, error) {
	if core == nil || core.data == nil {
		return nil, invalid()
	}
	return &AvatarAuthority{func() *Service { return core }}, nil
}
func (a *AvatarAuthority) core() *Service { return a.data() }
func (a *AvatarAuthority) AuthorizeOwner(ctx context.Context, actor identity.Actor, owner oc.ObjectOwner, intent identity.AccessIntent) (oc.OwnerAuthorization, error) {
	cause, e := recoveryCause("avatar-owner")
	if e != nil {
		return oc.OwnerAuthorization{}, e
	}
	var grant oc.OwnerAuthorization
	r := a.core().state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := a.core().state().store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(owner.Details().ID, foundation.Shared)}); e != nil {
			return unavailable(e)
		}
		grant, e = a.AuthorizeOwnerInTx(ctx, tx, actor, owner, intent)
		return e
	})
	return grant, resultError(r)
}
func (a *AvatarAuthority) AuthorizeOwnerInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, intent identity.AccessIntent) (oc.OwnerAuthorization, error) {
	if !tx.Valid() || owner.Validate() != nil || owner.Details().Kind != oc.Avatar {
		return oc.OwnerAuthorization{}, fault(foundation.DependencyUnbound, nil)
	}
	if actor.Validate() != nil || actor.Details().Kind != identity.Human || actor.Details().UserID != owner.Details().ID {
		return oc.OwnerAuthorization{}, fault(foundation.Forbidden, nil)
	}
	current, e := a.core().state().deps.Authority.current(ctx, tx, actor)
	if e != nil {
		return oc.OwnerAuthorization{}, e
	}
	return oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: intent, Existence: oc.ExistingOwner, Version: current.user.user.Version})
}
func (a *AvatarAuthority) AuthorizeObjectReadInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, id oc.ObjectID) (oc.OwnerAuthorization, error) {
	grant, e := a.AuthorizeOwnerInTx(ctx, tx, actor, owner, identity.Read)
	if e != nil {
		return oc.OwnerAuthorization{}, e
	}
	x, e := a.core().state().store.InTx(tx)
	if e != nil {
		return oc.OwnerAuthorization{}, unavailable(e)
	}
	current, e := currentAvatar(ctx, x, owner.Details().ID)
	if e != nil {
		return oc.OwnerAuthorization{}, e
	}
	if current != id {
		return oc.OwnerAuthorization{}, fault(foundation.NotFound, nil)
	}
	d := grant.Details()
	d.ReadObjectID = id
	return oc.NewOwnerAuthorization(d)
}
func (a *AvatarAuthority) CheckInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, intent identity.AccessIntent) error {
	if actor.Details().Kind == identity.Service && actor.Details().ServiceName == identity.ObjectMaintenance && intent == identity.Converge {
		id, e := parseID[oc.CleanupOperation](actor.Details().CauseRef)
		if e != nil {
			return fault(foundation.Forbidden, e)
		}
		x, e := a.core().state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		change, e := loadAvatarChange(ctx, x, id.String())
		if e != nil {
			return e
		}
		object, e := parseID[oc.StoredObject](change.object)
		if e != nil {
			return e
		}
		cause, e := avatarCause(id.String(), owner.Details().ID, oc.CancelledUpload)
		if e != nil {
			return e
		}
		return a.CheckCleanupInTx(ctx, tx, cause, object)
	}
	_, e := a.AuthorizeOwnerInTx(ctx, tx, actor, owner, intent)
	return e
}
func (a *AvatarAuthority) AuthorizeLeaseInTx(context.Context, foundation.Tx, identity.Actor, oc.ObjectID, oc.LeaseOwner, oc.LeaseAction) error {
	return fault(foundation.DependencyUnbound, nil)
}
func (a *AvatarAuthority) CheckProjectCleanupInTx(context.Context, foundation.Tx, identity.Actor, oc.ProjectCleanupCause) error {
	return fault(foundation.DependencyUnbound, nil)
}
func (a *AvatarAuthority) CheckCleanupInTx(ctx context.Context, tx foundation.Tx, cause oc.ObjectCleanupCause, id oc.ObjectID) error {
	if cause.Validate() != nil || id.Validate() != nil || cause.Details().Owner.Details().Kind != oc.Avatar {
		return invalid()
	}
	d := cause.Details()
	st := a.core().state()
	if e := st.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{userLock(d.Owner.Details().ID, foundation.Exclusive)}); e != nil {
		return unavailable(e)
	}
	x, e := st.store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	change, e := loadAvatarChange(ctx, x, d.OperationID.String())
	if e != nil {
		return e
	}
	if change.user != d.Owner.Details().ID {
		return fault(foundation.Forbidden, nil)
	}
	cmd, e := loadCommand(ctx, x, change.command, false)
	if e != nil {
		return e
	}
	if e = st.store.RequireHeldLocks(ctx, tx, commandLocks(cmd)); e != nil {
		return unavailable(e)
	}
	current, e := currentAvatar(ctx, x, change.user)
	if e != nil {
		return e
	}
	if current == id {
		return fault(foundation.Forbidden, nil)
	}
	switch d.Reason {
	case oc.CancelledUpload:
		if change.object != id.String() || change.cleanup != d.OperationID.String() || (change.phase != "cancelled" && change.phase != "cleanup_pending" && change.phase != "completed") || cmd.phase != "cancelled" {
			return fault(foundation.Forbidden, nil)
		}
	case oc.ReplacedObject:
		if change.previous != id.String() || change.phase != "applied" || cmd.phase != "committed" {
			return fault(foundation.Forbidden, nil)
		}
		var exact bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.avatar_cleanup WHERE id=$1 AND change_id=$1 AND user_id=$2 AND object_id=$3)`, change.id, change.user, id.String()).Scan(&exact); e != nil {
			return unavailable(e)
		}
		if !exact {
			return fault(foundation.Forbidden, nil)
		}
	default:
		return fault(foundation.Forbidden, nil)
	}
	return nil
}

type avatarAccessFacts struct {
	owner          oc.ObjectOwner
	change, source avatarChange
	commands       []commandRecord
	locks          []foundation.LockRequest
}

func (a *AvatarAuthority) objectFacts(ctx context.Context, x postgres.SQLExecutor, r oc.AccessRequest) (avatarAccessFacts, error) {
	d := r.Details()
	var v avatarAccessFacts
	var e error
	switch d.Kind {
	case oc.OwnerAccess, oc.ObjectReadAccess:
		v.owner = d.Owner
	case oc.ObjectCleanupAccess, oc.CleanupReleaseAccess:
		v.owner = d.Cleanup.Details().Owner
	case oc.MaintenanceAccess:
		if d.InstanceID.String() != a.core().state().process.String() {
			return v, fault(foundation.Forbidden, nil)
		}
	default:
		return v, fault(foundation.DependencyUnbound, nil)
	}
	object := d.ObjectID
	if d.Attempt.Validate() == nil {
		object = d.Attempt.Details().ObjectID
	}
	if d.Receipt.Validate() == nil {
		object = d.Receipt.Details().ObjectID
	}
	if object.Validate() == nil {
		v.source, e = loadAvatarObject(ctx, x, object)
		if e != nil {
			return v, e
		}
		mapped := avatarOwner(v.source.user)
		if v.owner.Validate() == nil && !v.owner.Equal(mapped) {
			return v, fault(foundation.Forbidden, nil)
		}
		v.owner = mapped
	}
	if v.owner.Validate() != nil || v.owner.Details().Kind != oc.Avatar {
		return v, fault(foundation.DependencyUnbound, nil)
	}
	var changeID string
	if d.Kind == oc.CleanupReleaseAccess || d.Kind == oc.ObjectCleanupAccess {
		changeID = d.Cleanup.Details().OperationID.String()
	}
	if d.Kind == oc.OwnerAccess {
		if d.Operation == oc.ReserveAccess {
			changeID = string(d.Command.IdempotencyKey)
		}
		if d.Operation == oc.LookupAccess || d.Operation == oc.CancelAccess {
			changeID = string(d.Key)
		}
	}
	if changeID != "" {
		v.change, e = loadAvatarChange(ctx, x, changeID)
		if e != nil {
			return v, e
		}
		if v.change.user != v.owner.Details().ID {
			return v, fault(foundation.Forbidden, nil)
		}
	} else {
		v.change = v.source
	}
	seen := map[string]bool{}
	for _, change := range []avatarChange{v.change, v.source} {
		if change.command == "" || seen[change.command] {
			continue
		}
		seen[change.command] = true
		cmd, e := loadCommand(ctx, x, change.command, false)
		if e != nil {
			return v, e
		}
		v.commands = append(v.commands, cmd)
		v.locks = append(v.locks, commandLocks(cmd)...)
	}
	mode := foundation.Exclusive
	if d.Operation == oc.PrepareAccess || d.Operation == oc.StatAccess || d.Operation == oc.ReadAccess || d.Operation == oc.LookupAccess {
		mode = foundation.Shared
	}
	v.locks = append(v.locks, userLock(v.owner.Details().ID, mode))
	return v, nil
}
func avatarDependencies(v avatarAccessFacts) (oc.AccessDependencies, error) {
	commands := []string{}
	for _, cmd := range v.commands {
		commands = append(commands, cmd.identity.Canonical())
	}
	b, e := json.Marshal([]any{v.owner.Details(), v.change.id, v.change.command, v.change.user, v.source.id, v.source.command, v.source.user, v.source.object, v.source.upload, commands})
	if e != nil {
		return oc.AccessDependencies{}, invalid()
	}
	return oc.NewAccessDependencies(digest(b), v.locks)
}
func (a *AvatarAuthority) Discover(ctx context.Context, r oc.AccessRequest) (oc.AccessDependencies, error) {
	if r.Validate() != nil {
		return oc.AccessDependencies{}, invalid()
	}
	v, e := a.objectFacts(ctx, a.core().state().store, r)
	if e != nil {
		return oc.AccessDependencies{}, e
	}
	return avatarDependencies(v)
}
func (a *AvatarAuthority) ValidateInTx(ctx context.Context, tx foundation.Tx, r oc.AccessRequest, deps oc.AccessDependencies) error {
	if r.Validate() != nil || deps.Validate() != nil {
		return invalid()
	}
	st := a.core().state()
	x, e := st.store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	v, e := a.objectFacts(ctx, x, r)
	if e != nil {
		return e
	}
	current, e := avatarDependencies(v)
	if e != nil {
		return e
	}
	if !deps.Equal(current) {
		return fault(foundation.ResourceBusy, nil)
	}
	if e = st.store.RequireHeldLocks(ctx, tx, deps.Locks()); e != nil {
		return unavailable(e)
	}
	d := r.Details()
	switch d.Kind {
	case oc.ObjectReadAccess:
		if d.Operation != oc.ReadAccess && d.Operation != oc.StatAccess {
			return fault(foundation.DependencyUnbound, nil)
		}
		_, e = a.AuthorizeObjectReadInTx(ctx, tx, d.Actor, v.owner, d.ObjectID)
		return e
	case oc.CleanupReleaseAccess, oc.ObjectCleanupAccess:
		return a.CheckCleanupInTx(ctx, tx, d.Cleanup, d.ObjectID)
	case oc.MaintenanceAccess:
		return nil // Object's exact instance and actual domain mapping were checked above.
	case oc.OwnerAccess:
		if d.Actor.Details().Kind == identity.Service {
			if d.Operation != oc.CancelAccess || d.Actor.Details().ServiceName != identity.ObjectMaintenance || d.Actor.Details().CauseRef != v.change.cleanup {
				return fault(foundation.Forbidden, nil)
			}
			id, e := parseID[oc.StoredObject](v.change.object)
			if e != nil {
				return e
			}
			cause, e := avatarCause(v.change.cleanup, v.change.user, oc.CancelledUpload)
			if e != nil {
				return e
			}
			return a.CheckCleanupInTx(ctx, tx, cause, id)
		}
		_, e = a.AuthorizeOwnerInTx(ctx, tx, d.Actor, v.owner, d.Intent)
		return e
	}
	return fault(foundation.DependencyUnbound, nil)
}
func (a AvatarAuthority) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "avatar_authority") }
func (a AvatarAuthority) MarshalJSON() ([]byte, error) { return []byte(`"avatar_authority"`), nil }
func (*AvatarAuthority) UnmarshalJSON([]byte) error    { return invalid() }
func (a AvatarAuthority) LogValue() slog.Value         { return slog.StringValue("avatar_authority") }

var _ oc.AccessPlanner = (*AvatarAuthority)(nil)
var _ oc.ResourceAuthority = (*AvatarAuthority)(nil)
var _ oc.CleanupAuthority = (*AvatarAuthority)(nil)
