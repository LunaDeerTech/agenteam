package account

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// ProfileService shares the account authority and actual operation registry.
// Objects must have the same owning-domain AvatarAuthority installed by the root.
type ProfileService struct{ data func() *profileState }
type profileState struct {
	core    *Service
	objects *object.Service
	mu      sync.Mutex
	local   map[string]*avatarLocal
	cursor  string
	runtime *Runtime
}
type avatarLocal struct {
	op       *operation
	identity foundation.CommandIdentity
}

func NewProfileService(core *Service, objects *object.Service) (*ProfileService, error) {
	if core == nil || core.data == nil || objects == nil {
		return nil, invalid()
	}
	st := &profileState{core: core, objects: objects, local: map[string]*avatarLocal{}}
	return &ProfileService{func() *profileState { return st }}, nil
}
func (p *ProfileService) state() *profileState { return p.data() }
func (p *ProfileService) GetProfile(ctx context.Context, actor identity.Actor) (c.ProfileView, error) {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return c.ProfileView{}, invalid()
	}
	op, e := p.state().core.begin(ctx, false)
	if e != nil {
		return c.ProfileView{}, e
	}
	defer p.state().core.finish(op)
	return p.currentProfile(op.ctx, actor)
}
func currentAvatar(ctx context.Context, x postgres.SQLExecutor, user string) (oc.ObjectID, error) {
	var raw string
	if e := x.QueryRow(ctx, `SELECT coalesce(avatar_object_id::text,'') FROM agenteam_account.users WHERE id=$1`, user).Scan(&raw); e != nil {
		return oc.ObjectID{}, unavailable(e)
	}
	if raw == "" {
		return oc.ObjectID{}, nil
	}
	return parseID[oc.StoredObject](raw)
}
func (p *ProfileService) currentProfile(ctx context.Context, actor identity.Actor) (c.ProfileView, error) {
	core := p.state().core
	st := core.state()
	cause, e := recoveryCause("profile-read")
	if e != nil {
		return c.ProfileView{}, e
	}
	var user userRecord
	var avatar oc.ObjectID
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared)}); e != nil {
			return unavailable(e)
		}
		session, e := st.deps.Authority.current(ctx, tx, actor)
		if e != nil {
			return e
		}
		user = session.user
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		avatar, e = currentAvatar(ctx, x, actor.Details().UserID)
		return e
	})
	if e = resultError(r); e != nil {
		return c.ProfileView{}, e
	}
	var meta *c.AvatarMetadata
	if avatar.Validate() == nil {
		owner, _ := oc.NewObjectOwner(oc.Avatar, actor.Details().UserID, "")
		m, e := p.state().objects.StatObject(ctx, actor, owner, avatar)
		if e != nil {
			return c.ProfileView{}, portError(e)
		}
		meta = &c.AvatarMetadata{MediaType: m.MediaType, ByteSize: m.ByteSize, SHA256: m.SHA256}
	}
	return projectProfile(user, meta), nil
}
func (p ProfileService) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "account_profile_service")
}
func (p ProfileService) MarshalJSON() ([]byte, error) {
	return []byte(`"account_profile_service"`), nil
}
func (*ProfileService) UnmarshalJSON([]byte) error { return invalid() }
func (p ProfileService) LogValue() slog.Value      { return slog.StringValue("account_profile_service") }

var _ c.ProfilePort = (*ProfileService)(nil)
