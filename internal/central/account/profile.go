package account

import (
	"context"
	"strings"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// CurrentUserRouteInTx reads only the caller's current persisted route. Unlike
// current's convenience wrapper, this port never opens a transaction for a zero
// handle. current checks the same Store's live handle and held User SH/EX before
// reading the Session and its User; neither path acquires locks or touches them.
func (a *Authority) CurrentUserRouteInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor) (c.UserRoute, error) {
	if !tx.Valid() {
		return c.UserRoute{}, invalid()
	}
	session, err := a.current(ctx, tx, actor)
	if err != nil {
		return c.UserRoute{}, err
	}
	return projectUserRoute(session.user.user)
}

var _ c.CurrentUserRoutes = (*Authority)(nil)

func projectUserRoute(user c.User) (c.UserRoute, error) {
	if user.ID.Validate() != nil || user.Version.Validate() != nil || !canonicalProfileRoute(user.Username) {
		return c.UserRoute{}, unavailable(nil)
	}
	return c.UserRoute{UserID: user.ID, Username: user.Username, Version: user.Version}, nil
}

// Persisted routes include bootstrap's admin name. Creation's reserved-name
// rejection is deliberately not used to read or keep an existing route.
func canonicalProfileRoute(name string) bool {
	if len(name) < 3 || len(name) > 32 {
		return false
	}
	for i := range len(name) {
		b := name[i]
		if b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-' && i > 0 && i < len(name)-1 {
			continue
		}
		return false
	}
	return true
}

type profilePatch struct{ username, displayName string }

// normalizeProfilePatch is called with a currently authorized User. It only
// prepares values; command replay, expected-version and uniqueness checks still
// belong to the mutation transaction. It never rewrites the caller's pointers.
func normalizeProfilePatch(current c.User, change c.ProfileChange) (profilePatch, error) {
	if err := change.Validate(); err != nil {
		return profilePatch{}, err
	}
	if !canonicalProfileRoute(current.Username) || ValidateDisplayName(current.DisplayName) != nil {
		return profilePatch{}, unavailable(nil)
	}
	p := profilePatch{username: current.Username, displayName: current.DisplayName}
	if change.Username != nil {
		name := strings.ToLower(*change.Username)
		if name != current.Username {
			var err error
			name, err = NormalizeUsername(*change.Username)
			if err != nil {
				return profilePatch{}, err
			}
		}
		p.username = name
	}
	if change.DisplayName != nil {
		p.displayName = *change.DisplayName
	}
	return p, nil
}

// projectProfile cannot copy a PHC/password version or storage locator from its
// source record. The optional metadata is copied so later assembly cannot mutate
// an already returned view through the caller's pointer.
func projectProfile(user userRecord, avatar *c.AvatarMetadata) c.ProfileView {
	view := c.ProfileView{User: user.user}
	if avatar != nil {
		copy := *avatar
		view.Avatar = &copy
	}
	return view
}
