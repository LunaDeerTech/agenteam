//go:build integration

package account_test

import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"testing"
)

func TestAccountCurrentSessionRequiresRealHeldUserAndCurrentRole(t *testing.T) {
	_, s, a := database(t)
	uid, sid := id[identity.User](t), id[identity.Session](t)
	actor, _ := identity.NewHuman(uid, sid)
	_, e := s.Exec(ctxFor(t), `INSERT INTO agenteam_account.users(id,email,username,display_name,role,password_phc,password_version,version,auth_sequence,initial_password_suggestion,theme) VALUES($1,'user@example.com','valid-user','Current user','user','fixture-phc',1,1,1,false,'system');`, uid.String())
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Exec(ctxFor(t), `INSERT INTO agenteam_account.sessions(id,user_id,token_verifier,csrf_kid,idle_seconds,absolute_expires_at) VALUES($1,$2,decode(repeat('01',32),'hex'),'a',3600,clock_timestamp()+interval '1 day')`, sid.String(), uid.String())
	if e != nil {
		t.Fatal(e)
	}
	if e = a.RequireCurrentSession(ctxFor(t), foundation.Tx{}, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = a.AuthorizeSystem(ctxFor(t), foundation.Tx{}, actor, identity.Mutate); !hasCode(e, foundation.Forbidden) {
		t.Fatal(e)
	}
	r := s.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		_ = a.RequireCurrentSession(ctx, tx, actor)
		return nil
	})
	if r.State() != foundation.NotCommitted {
		t.Fatal("ignored missing lock committed")
	}
	key, _ := foundation.UserLock(uid.String())
	r = s.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if e := s.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}); e != nil {
			return e
		}
		return a.RequireCurrentSession(ctx, tx, actor)
	})
	if r.State() != foundation.Committed {
		t.Fatal(r.Fault())
	}
	if _, e = s.Exec(ctxFor(t), `UPDATE agenteam_account.users SET role='admin',version=version+1 WHERE id=$1`, uid.String()); e != nil {
		t.Fatal(e)
	}
	if _, e = a.AuthorizeSystem(ctxFor(t), foundation.Tx{}, actor, identity.Mutate); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Exec(ctxFor(t), `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='logout' WHERE id=$1`, sid.String()); e != nil {
		t.Fatal(e)
	}
	if e = a.RequireCurrentSession(ctxFor(t), foundation.Tx{}, actor); !hasCode(e, foundation.SessionRevoked) {
		t.Fatal(e)
	}
}
func hasCode(err error, want foundation.Code) bool {
	var f *foundation.Fault
	return errors.As(err, &f) && f != nil && f.Code == want
}
