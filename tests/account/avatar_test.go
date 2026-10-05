//go:build integration

package account_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image/jpeg"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// The completed Publish transaction is real. This test-only boundary holds
// its caller before account apply, without changing its CommitResult.
type avatarPublishBarrier struct {
	*postgres.Store
	armed, fired atomic.Bool
	held         chan struct{}
	release      chan struct{}
}

func (w *avatarPublishBarrier) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	r := w.Store.WithinTx(ctx, cause, fn)
	if r.State() != foundation.Committed || !w.armed.Load() || w.fired.Load() {
		return r
	}
	var published bool
	e := w.Store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.avatar_changes a JOIN agenteam_account.commands c ON c.id=a.command_id WHERE c.command_key='held-published-avatar' AND a.phase='published')`).Scan(&published)
	if e == nil && published && w.fired.CompareAndSwap(false, true) {
		close(w.held)
		select {
		case <-w.release:
		case <-ctx.Done():
		}
	}
	return r
}

func TestAccountAvatarPublishedCandidateRechecksCurrentSessionAndVersion(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		name := "version_changed"
		if revoke {
			name = "session_revoked"
		}
		t.Run(name, func(t *testing.T) {
			db, _, _ := database(t)
			raw := openStore(t, db.Config(t, nil))
			w := &avatarPublishBarrier{Store: raw, held: make(chan struct{}), release: make(chan struct{})}
			k, _, _ := keys(t)
			authority, e := account.NewAuthority(w, k)
			if e != nil {
				t.Fatal(e)
			}
			f := assembleAvatarFixture(t, assembleB02(t, db, raw, w, authority, nil))
			first := f.putAvatar(t, "current-before-boundary", 1, 63)
			current := f.avatarID(t)
			var once sync.Once
			release := func() { once.Do(func() { close(w.release) }) }
			defer release()
			w.armed.Store(true)
			body := avatarPNG(t, 64)
			done := make(chan error, 1)
			go func() {
				_, e := f.profiles.PutAvatar(ctxFor(t), c.AvatarUpload{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "held-published-avatar", ExpectedVersion: first.User.Version}, MediaType: "image/png", ByteSize: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body))})
				done <- e
			}()
			select {
			case <-w.held:
			case <-time.After(3 * time.Second):
				t.Fatal("real Publish did not reach account-apply boundary")
			}
			var nextID string
			if e = raw.QueryRow(ctxFor(t), `SELECT new_object_id::text FROM agenteam_account.avatar_changes a JOIN agenteam_account.commands c ON c.id=a.command_id WHERE c.command_key='held-published-avatar' AND a.phase='published'`).Scan(&nextID); e != nil {
				t.Fatal(e)
			}
			candidate, e := foundation.ParseID[oc.StoredObject](nextID)
			if e != nil {
				t.Fatal(e)
			}
			owner, _ := oc.NewObjectOwner(oc.Avatar, f.actor.Details().UserID, "")
			if _, e = f.objects.StatObject(ctxFor(t), f.actor, owner, candidate); e == nil {
				t.Fatal("published non-current candidate readable")
			}
			want := foundation.VersionConflict
			if revoke {
				want = foundation.SessionRevoked
				e = f.service.Logout(ctxFor(t), account.LogoutRequest{Actor: f.actor, Key: "revoke-before-avatar-apply"})
			} else {
				_, e = f.profiles.SetTheme(ctxFor(t), c.ThemeChange{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "advance-before-avatar-apply", ExpectedVersion: first.User.Version}, Theme: c.DarkTheme})
			}
			if e != nil {
				t.Fatal("real competing account mutation", e, safeFailure(e))
			}
			release()
			select {
			case e = <-done:
				requireAvatarFault(t, e, want)
			case <-time.After(3 * time.Second):
				t.Fatal("published caller did not join")
			}
			for range 2 {
				if _, e = f.profiles.RecoverAvatars(ctxFor(t)); e != nil {
					t.Fatal("failed apply recovery", e, safeFailure(e))
				}
			}
			var objectState, changePhase string
			var refs, audits int
			if e = raw.QueryRow(ctxFor(t), `SELECT o.state,a.phase,(SELECT count(*) FROM agenteam_object.object_references WHERE object_id=o.id),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.avatar.update') FROM agenteam_object.objects o JOIN agenteam_account.avatar_changes a ON a.new_object_id=o.id WHERE o.id=$1`, nextID).Scan(&objectState, &changePhase, &refs, &audits); e != nil || objectState != "deleted" || changePhase != "completed" || refs != 0 || audits != 1 || f.avatarID(t) != current {
				t.Fatal("rejected apply retained/published candidate", objectState, changePhase, refs, audits, e)
			}
		})
	}
}

func TestAccountAvatarPutReplaceDeleteAndCurrentRead(t *testing.T) {
	f := newAvatarFixture(t)
	view, e := f.profiles.GetProfile(ctxFor(t), f.actor)
	if e != nil {
		t.Fatal(e)
	}
	first := f.putAvatar(t, "avatar-a", view.User.Version, 31)
	if first.Avatar == nil || first.Avatar.MediaType != "image/jpeg" {
		t.Fatal("avatar metadata", first)
	}
	aid := f.avatarID(t)
	reader, e := f.profiles.ReadAvatar(ctxFor(t), f.actor, c.AvatarRange{Kind: c.AvatarRangeAll})
	if e != nil {
		t.Fatal("read", e)
	}
	body, e := io.ReadAll(reader)
	if e != nil {
		t.Fatal(e)
	}
	if e = reader.Close(); e != nil {
		t.Fatal(e)
	}
	decoded, e := jpeg.Decode(bytes.NewReader(body))
	if e != nil || decoded.Bounds().Dx() != 47 || decoded.Bounds().Dy() != 31 {
		t.Fatal("jpeg", e)
	}
	ranged, e := f.profiles.ReadAvatar(ctxFor(t), f.actor, c.AvatarRange{Kind: c.AvatarRangeSuffix, Length: 9})
	if e != nil {
		t.Fatal(e)
	}
	tail, e := io.ReadAll(ranged)
	if e != nil {
		t.Fatal(e)
	}
	if e = ranged.Close(); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(tail, body[len(body)-9:]) {
		t.Fatal("range changed payload")
	}
	second := f.putAvatar(t, "avatar-b", first.User.Version, 87)
	bid := f.avatarID(t)
	if bid == aid {
		t.Fatal("object reused")
	}
	owner, _ := oc.NewObjectOwner(oc.Avatar, f.actor.Details().UserID, "")
	if _, e = f.objects.ReadObject(ctxFor(t), f.actor, owner, aid, nil); e == nil {
		t.Fatal("old avatar readable")
	}
	// Historical committed command returns the current projection without
	// switching User.avatar back, reattaching or putting any old payload.
	replay := f.putAvatar(t, "avatar-a", view.User.Version, 31)
	if replay.User.Version != second.User.Version || f.avatarID(t) != bid {
		t.Fatal("historical avatar resurrected")
	}
	if _, e = f.profiles.RecoverAvatars(ctxFor(t)); e != nil {
		t.Fatalf("recover replacement %v [%s]", e, safeFailure(e))
	}
	var state string
	if e = f.store.QueryRow(ctxFor(t), `SELECT state FROM agenteam_object.objects WHERE id=$1`, aid.String()).Scan(&state); e != nil || state != "deleted" {
		t.Fatal("old payload not deleted", state, e)
	}
	deleted, e := f.profiles.DeleteAvatar(ctxFor(t), c.ProfileMutation{Actor: f.actor, Key: "avatar-delete", ExpectedVersion: second.User.Version})
	if e != nil || deleted.Avatar != nil {
		t.Fatalf("delete %v [%s]", e, safeFailure(e))
	}
	if _, e = f.profiles.ReadAvatar(ctxFor(t), f.actor, c.AvatarRange{Kind: c.AvatarRangeAll}); e == nil {
		t.Fatal("read after delete")
	}
	if _, e = f.profiles.RecoverAvatars(ctxFor(t)); e != nil {
		t.Fatal("recover deletion", e)
	}
	var mapped, audit int
	e = f.store.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM agenteam_account.avatar_changes WHERE upload_id IS NOT NULL AND attempt_id IS NOT NULL AND new_object_id IS NOT NULL),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.avatar.update')`).Scan(&mapped, &audit)
	if e != nil || mapped != 2 || audit != 3 {
		t.Fatal("mapping/audit", mapped, audit, e)
	}
}

func TestAccountAvatarOriginalBytesAndDeleteVersionCompetition(t *testing.T) {
	f := newAvatarFixture(t)
	raw := avatarPNG(t, 71)
	request := func(key string, version foundation.Version, body []byte) c.AvatarUpload {
		return c.AvatarUpload{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: foundation.IdempotencyKey(key), ExpectedVersion: version}, MediaType: "image/png", ByteSize: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body))}
	}
	first, e := f.profiles.PutAvatar(ctxFor(t), request("original-bytes", 1, raw))
	if e != nil {
		t.Fatal(e)
	}
	// Add a valid ancillary text chunk before IEND: decoded pixels and the
	// normalized JPEG are unchanged, while the original bytes are different.
	payload := []byte("fixture\x00different public metadata")
	chunk := make([]byte, 12+len(payload))
	binary.BigEndian.PutUint32(chunk, uint32(len(payload)))
	copy(chunk[4:8], "tEXt")
	copy(chunk[8:], payload)
	binary.BigEndian.PutUint32(chunk[8+len(payload):], crc32.ChecksumIEEE(chunk[4:8+len(payload)]))
	changed := append(append(append([]byte(nil), raw[:len(raw)-12]...), chunk...), raw[len(raw)-12:]...)
	if _, e = f.profiles.PutAvatar(ctxFor(t), request("original-bytes", 1, changed)); !hasCode(e, foundation.IdempotencyKeyReused) {
		t.Fatal("normalized-image equivalence replaced original-byte binding", e, safeFailure(e))
	}
	if _, e = f.profiles.PutAvatar(ctxFor(t), request("original-bytes", 1, raw)); e != nil {
		t.Fatal("original replay", e)
	}
	ctx := ctxFor(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	next := avatarPNG(t, 72)
	go func() {
		<-start
		_, e := f.profiles.PutAvatar(ctx, request("racing-replace", first.User.Version, next))
		results <- e
	}()
	go func() {
		<-start
		_, e := f.profiles.DeleteAvatar(ctx, c.ProfileMutation{Actor: f.actor, Key: "racing-delete", ExpectedVersion: first.User.Version})
		results <- e
	}()
	close(start)
	success, conflict := 0, 0
	for range 2 {
		e := <-results
		if e == nil {
			success++
		} else if hasCode(e, foundation.VersionConflict) {
			conflict++
		} else {
			t.Fatal("unexpected competing mutation", e, safeFailure(e))
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("version competition did not serialize", success, conflict)
	}
	for range 2 {
		if _, e = f.profiles.RecoverAvatars(ctxFor(t)); e != nil {
			t.Fatal("losing candidate cleanup", e, safeFailure(e))
		}
	}
	var pending, audits int
	var version int64
	if e = f.store.QueryRow(ctxFor(t), `SELECT version,(SELECT count(*) FROM agenteam_account.avatar_changes WHERE phase IN ('preparing','published','cancelled','cleanup_pending')),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.avatar.update') FROM agenteam_account.users WHERE id=$1`, f.actor.Details().UserID).Scan(&version, &pending, &audits); e != nil || version != 3 || pending != 0 || audits != 2 {
		t.Fatal("competing facts leaked", version, pending, audits, e)
	}
}

func TestAccountAvatarApplyAuditRollbackKeepsCurrentAndRecoversCandidate(t *testing.T) {
	f := newAvatarFixture(t)
	first := f.putAvatar(t, "kept-before-audit-failure", 1, 19)
	old := f.avatarID(t)
	sql := f.db.Connect(t)
	if _, e := sql.Exec(ctxFor(t), `CREATE FUNCTION public.avatar_reject_audit() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.action='account.avatar.update' THEN RAISE EXCEPTION 'owned avatar audit rejection'; END IF; RETURN NEW; END$$`); e != nil {
		t.Fatal(e)
	}
	if _, e := sql.Exec(ctxFor(t), `CREATE TRIGGER avatar_reject_audit BEFORE INSERT ON agenteam_audit.audit_records FOR EACH ROW EXECUTE FUNCTION public.avatar_reject_audit()`); e != nil {
		t.Fatal(e)
	}
	raw := avatarPNG(t, 20)
	_, e := f.profiles.PutAvatar(ctxFor(t), c.AvatarUpload{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "audit-rolled-back", ExpectedVersion: first.User.Version}, MediaType: "image/png", ByteSize: int64(len(raw)), Body: io.NopCloser(bytes.NewReader(raw))})
	var fault *foundation.Fault
	if !errors.As(e, &fault) || fault.CommitState != foundation.NotCommitted {
		t.Fatal("failed audit did not roll back final mutation", e, safeFailure(e))
	}
	var phase, newID string
	var cleaning bool
	var refs, audits int
	if e = f.store.QueryRow(ctxFor(t), `SELECT a.phase,a.new_object_id::text,(SELECT cleaning FROM agenteam_object.objects WHERE id=$1),(SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.avatar.update') FROM agenteam_account.avatar_changes a JOIN agenteam_account.commands c ON c.id=a.command_id WHERE c.command_key='audit-rolled-back'`, old.String()).Scan(&phase, &newID, &cleaning, &refs, &audits); e != nil || phase != "published" || cleaning || refs != 1 || audits != 1 || f.avatarID(t) != old {
		t.Fatal("audit failure changed current/gate/receipt", phase, cleaning, refs, audits, e)
	}
	candidate, e := foundation.ParseID[oc.StoredObject](newID)
	if e != nil {
		t.Fatal(e)
	}
	owner, _ := oc.NewObjectOwner(oc.Avatar, f.actor.Details().UserID, "")
	if reader, e := f.objects.ReadObject(ctxFor(t), f.actor, owner, candidate, nil); e == nil {
		_ = reader.Close()
		t.Fatal("unapplied published avatar exposed")
	}
	if _, e = sql.Exec(ctxFor(t), `DROP TRIGGER avatar_reject_audit ON agenteam_audit.audit_records`); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		if _, e = f.profiles.RecoverAvatars(ctxFor(t)); e != nil {
			t.Fatal("audit rollback recovery", e, safeFailure(e))
		}
	}
	var state string
	if e = f.store.QueryRow(ctxFor(t), `SELECT state FROM agenteam_object.objects WHERE id=$1`, candidate.String()).Scan(&state); e != nil || state != "deleted" || f.avatarID(t) != old {
		t.Fatal("failed candidate not cleared safely", state, e)
	}
}
