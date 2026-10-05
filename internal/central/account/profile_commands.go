package account

import (
	"context"
	"encoding/json"
	"math"
	"strings"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func profileIdentity(r c.ProfileMutation, name string) (foundation.CommandIdentity, error) {
	return foundation.NewCommandIdentity("account.profile", []string{r.Actor.Details().UserID}, name, r.Key)
}
func (p *ProfileService) profileMAC(r c.ProfileMutation, name string, fields ...any) (commandMAC, error) {
	// Optional fields are explicit primitives: never hash safe opaque projections.
	b, e := json.Marshal(append([]any{name, r.Actor.Details().UserID, r.ExpectedVersion.String()}, fields...))
	if e != nil {
		return nil, invalid()
	}
	return func(kid string) ([]byte, error) { return p.state().core.state().keys.mac(kid, "command-v1", b) }, nil
}
func profileLocks(r c.ProfileMutation, key foundation.CommandIdentity) []foundation.LockRequest {
	return []foundation.LockRequest{commandLock(key), configLock("account-directory", foundation.Exclusive), userLock(r.Actor.Details().UserID, foundation.Exclusive)}
}
func (p *ProfileService) insertProfileCommand(ctx context.Context, x postgres.SQLExecutor, r c.ProfileMutation, key foundation.CommandIdentity, id c.CommandID, mac commandMAC) error {
	st := p.state().core.state()
	value, e := mac(st.keys.current())
	if e != nil {
		return e
	}
	defer clear(value)
	_, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,session_id,origin_process_id,expected_version,resource_id,phase) VALUES($1,'account.profile',$2,$3,$4,$5,$6,$7,'human',$2,$8,$9,$10,$2,'planned')`, id.String(), r.Actor.Details().UserID, string(r.Key), key.Command(), string(digest([]byte(key.Canonical()))), st.keys.current(), value, r.Actor.Details().SessionID, st.process.String(), int64(r.ExpectedVersion))
	return accountConflict(e)
}
func (p *ProfileService) UpdateProfile(ctx context.Context, r c.ProfileChange) (c.ProfileView, error) {
	if e := r.Validate(); e != nil {
		return c.ProfileView{}, e
	}
	var username, display any
	if r.Username != nil {
		value := strings.ToLower(*r.Username)
		r.Username = &value
		username = value
	}
	if r.DisplayName != nil {
		value := *r.DisplayName
		r.DisplayName = &value
		display = value
	}
	mac, e := p.profileMAC(r.ProfileMutation, "profile-update", username, display)
	if e != nil {
		return c.ProfileView{}, e
	}
	return p.mutateProfile(ctx, r.ProfileMutation, mac, func(current c.User) (string, string, c.Theme, []ac.AccountChangedField, error) {
		patch, e := normalizeProfilePatch(current, r)
		var fields []ac.AccountChangedField
		if r.Username != nil {
			fields = append(fields, ac.UsernameChanged)
		}
		if r.DisplayName != nil {
			fields = append(fields, ac.DisplayNameChanged)
		}
		return patch.username, patch.displayName, current.Theme, fields, e
	})
}
func (p *ProfileService) SetTheme(ctx context.Context, r c.ThemeChange) (c.ProfileView, error) {
	if e := r.Validate(); e != nil {
		return c.ProfileView{}, e
	}
	mac, e := p.profileMAC(r.ProfileMutation, "theme", string(r.Theme))
	if e != nil {
		return c.ProfileView{}, e
	}
	return p.mutateProfile(ctx, r.ProfileMutation, mac, func(current c.User) (string, string, c.Theme, []ac.AccountChangedField, error) {
		return current.Username, current.DisplayName, r.Theme, []ac.AccountChangedField{ac.ThemeChanged}, nil
	})
}
func (p *ProfileService) mutateProfile(ctx context.Context, r c.ProfileMutation, mac commandMAC, change func(c.User) (string, string, c.Theme, []ac.AccountChangedField, error)) (c.ProfileView, error) {
	core := p.state().core
	st := core.state()
	op, e := core.begin(ctx, false)
	if e != nil {
		return c.ProfileView{}, e
	}
	defer core.finish(op)
	ctx = op.ctx
	key, e := profileIdentity(r, "profile-update")
	if e != nil {
		return c.ProfileView{}, e
	}
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return c.ProfileView{}, e
	}
	id, e := foundation.NewID[c.Command]()
	if e != nil {
		return c.ProfileView{}, unavailable(e)
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, profileLocks(r, key)); e != nil {
			return unavailable(e)
		}
		session, e := st.deps.Authority.current(ctx, tx, r.Actor)
		if e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		old, e := loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e == nil {
			if e = core.checkCommand(old, key, mac); e != nil {
				return e
			}
			if old.phase == "committed" {
				return nil
			}
			return fault(foundation.InvalidState, nil)
		}
		if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		if session.user.user.Version != r.ExpectedVersion {
			return fault(foundation.VersionConflict, nil)
		}
		if r.ExpectedVersion == foundation.Version(math.MaxInt64) {
			return fault(foundation.InvalidState, nil)
		}
		username, display, theme, fields, e := change(session.user.user)
		if e != nil {
			return e
		}
		// The held account-directory EX serializes this check with account
		// username writers. Detect a known conflict before a unique violation
		// poisons the Tx; the constraint remains the integrity backstop.
		if username != session.user.user.Username {
			var exists bool
			if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.users WHERE username=$1 AND id<>$2)`, username, r.Actor.Details().UserID).Scan(&exists); e != nil {
				return unavailable(e)
			}
			if exists {
				return field("/username", "ALREADY_EXISTS")
			}
		}
		if e = p.insertProfileCommand(ctx, x, r, key, id, mac); e != nil {
			return e
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.users SET username=$2,display_name=$3,theme=$4,version=version+1 WHERE id=$1`, r.Actor.Details().UserID, username, display, string(theme)); e != nil {
			return accountConflict(e)
		}
		if e = completeCommand(ctx, x, id.String(), int64(r.ExpectedVersion)+1); e != nil {
			return e
		}
		return core.mutationAudit(ctx, tx, r.Actor, ac.AccountProfileUpdate, ac.UserResource, r.Actor.Details().UserID, id.String(), ac.AccountMetadataFields{UserID: r.Actor.Details().UserID, Version: r.ExpectedVersion + 1, ChangedFields: fields, Phase: ac.AccountUpdated})
	})
	if result.State() == foundation.Unknown {
		cmd, err := core.lookupMutation(ctx, key, mac, r.Actor, c.BrowserIdentity{}, false)
		if err != nil || cmd.phase != "committed" {
			return c.ProfileView{}, confirmationError(result, err)
		}
	} else if e = resultError(result); e != nil {
		return c.ProfileView{}, e
	}
	return p.currentProfile(ctx, r.Actor)
}
