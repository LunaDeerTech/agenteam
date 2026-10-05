//go:build integration

package account_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func avatarStoredAttempt(t *testing.T, f *avatarFixture, object oc.ObjectID) (string, oc.UploadAttempt) {
	t.Helper()
	var change, u, a string
	if e := f.store.QueryRow(ctxFor(t), `SELECT id::text,upload_id::text,attempt_id::text FROM agenteam_account.avatar_changes WHERE new_object_id=$1`, object.String()).Scan(&change, &u, &a); e != nil {
		t.Fatal(e)
	}
	upload, e := foundation.ParseID[oc.Upload](u)
	if e != nil {
		t.Fatal(e)
	}
	attempt, e := foundation.ParseID[oc.Attempt](a)
	if e != nil {
		t.Fatal(e)
	}
	handle, e := oc.NewUploadAttempt(oc.AttemptDetails{ID: attempt, UploadID: upload, ObjectID: object})
	if e != nil {
		t.Fatal(e)
	}
	return change, handle
}
func avatarWithinPlan(t *testing.T, f *avatarFixture, request oc.AccessRequest, fn func(context.Context, foundation.Tx, oc.AccessLockPlan, oc.LockedAccess) error) error {
	t.Helper()
	plan, e := f.objects.DiscoverAccess(ctxFor(t), request)
	if e != nil {
		return e
	}
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		locked, e := f.objects.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
		if e != nil {
			return e
		}
		return fn(ctx, tx, plan, locked)
	})
	if result.State() == foundation.Committed {
		return nil
	}
	if result.Fault() != nil {
		return result.Fault()
	}
	return foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
}

func TestAccountAvatarCleanupGateRejectsCurrentAndLateSuccessPaths(t *testing.T) {
	f := newAvatarFixture(t)
	first := f.putAvatar(t, "first-avatar", 1, 34)
	oid := f.avatarID(t)
	change, handle := avatarStoredAttempt(t, f, oid)
	owner, _ := oc.NewObjectOwner(oc.Avatar, f.actor.Details().UserID, "")
	lookup, e := f.objects.LookupPut(ctxFor(t), f.actor, owner, foundation.IdempotencyKey(change))
	if e != nil || lookup.State != oc.UploadCommitted {
		t.Fatal("original receipt", e, safeFailure(e))
	}
	// Avatar is an existing User owner: Publish attaches it immediately and
	// never issues a prospective-owner Consume receipt. That adjacent path is
	// covered with a genuine prospective upload in tests/objects.
	if lookup.Receipt.Validate() == nil {
		t.Fatal("existing Avatar unexpectedly returned a prospective receipt")
	}
	reader, e := f.profiles.ReadAvatar(ctxFor(t), f.actor, c.AvatarRange{Kind: c.AvatarRangeAll})
	if e != nil {
		t.Fatal(e)
	}
	jpeg, e := io.ReadAll(reader)
	if e != nil {
		t.Fatal(e)
	}
	if e = reader.Close(); e != nil {
		t.Fatal(e)
	}
	operation, _ := foundation.ParseID[oc.CleanupOperation](change)
	for _, reason := range []oc.CleanupReason{oc.ReplacedObject, oc.CancelledUpload} {
		forged, _ := oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: operation, Owner: owner, Reason: reason})
		request, _ := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, ObjectID: oid, UploadID: handle.Details().UploadID, Cleanup: forged})
		e = avatarWithinPlan(t, f, request, func(ctx context.Context, tx foundation.Tx, p oc.AccessLockPlan, l oc.LockedAccess) error {
			return f.objects.ReleaseForCleanupInTx(ctx, tx, forged, oid, p, l)
		})
		if e == nil {
			t.Fatal("non-current inequality or invented cause authorized cleanup")
		}
	}
	if f.avatarID(t) != oid {
		t.Fatal("forged cleanup removed current avatar")
	}
	f.putAvatar(t, "replace-avatar", first.User.Version, 35)
	meta := foundation.CommandMeta{RequestID: id[foundation.Request](t), IdempotencyKey: foundation.IdempotencyKey(change)}
	prepared, e := f.objects.PreparePayload(ctxFor(t), f.actor, owner, "image/jpeg", int64(len(jpeg)), nil, io.NopCloser(bytes.NewReader(jpeg)))
	if e != nil {
		t.Fatal(e)
	}
	defer f.objects.DiscardPrepared(prepared)
	for _, op := range []oc.AccessOperation{oc.ReserveAccess, oc.PublishAccess, oc.AttachAccess} {
		t.Run(string(op), func(t *testing.T) {
			d := oc.AccessRequestDetails{Operation: op, Actor: f.actor, Owner: owner, Intent: identity.Mutate}
			switch op {
			case oc.ReserveAccess:
				d.Command = &meta
				d.Prepared = prepared
			case oc.PublishAccess:
				d.Attempt = handle
			case oc.AttachAccess:
				d.ObjectID = oid
			}
			request, e := oc.NewOwnerAccess(d)
			if e != nil {
				t.Fatal(e)
			}
			e = avatarWithinPlan(t, f, request, func(ctx context.Context, tx foundation.Tx, p oc.AccessLockPlan, l oc.LockedAccess) error {
				switch op {
				case oc.ReserveAccess:
					_, e := f.objects.ReserveUploadInTx(ctx, tx, f.actor, owner, meta, prepared, p, l)
					return e
				case oc.PublishAccess:
					_, e := f.objects.PublishVerifiedInTx(ctx, tx, f.actor, owner, handle, p, l)
					return e
				case oc.AttachAccess:
					_, e := f.objects.AttachObjectInTx(ctx, tx, f.actor, owner, oid, p, l)
					return e
				}
				panic("test operation is not exhaustive")
			})
			if !hasCode(e, foundation.ResourceDeleted) {
				t.Fatal("late success shortcut not gated", e, safeFailure(e))
			}
		})
	}
	lookup, e = f.objects.LookupPut(ctxFor(t), f.actor, owner, foundation.IdempotencyKey(change))
	if e != nil || lookup.State != oc.UploadRevoked || lookup.Receipt.Validate() == nil {
		t.Fatal("revoked lookup returned live receipt", lookup.State, e)
	}
	var refs int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$1`, oid.String()).Scan(&refs); e != nil || refs != 0 {
		t.Fatal("old reference revived", refs, e)
	}
}

func TestAccountAvatarReaderKeepsOldPayloadUntilActualClose(t *testing.T) {
	f := newAvatarFixture(t)
	raw := avatarNoisePNG(t)
	first, e := f.profiles.PutAvatar(ctxFor(t), c.AvatarUpload{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "reader-first", ExpectedVersion: 1}, MediaType: "image/png", ByteSize: int64(len(raw)), Body: io.NopCloser(bytes.NewReader(raw))})
	if e != nil || first.Avatar == nil || first.Avatar.ByteSize <= oc.StreamBufferSize {
		t.Fatal("reader fixture must exceed the real integrity tail", first.Avatar, e)
	}
	old := f.avatarID(t)
	r, e := f.profiles.ReadAvatar(ctxFor(t), f.actor, c.AvatarRange{Kind: c.AvatarRangeAll})
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	var active int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND state='active'`, old.String()).Scan(&active); e != nil || active != 1 {
		t.Fatal("reader protection not established", active, e)
	}
	f.putAvatar(t, "reader-second", first.User.Version, 56)
	status, e := f.profiles.RecoverAvatars(ctxFor(t))
	if e != nil || status.Pending == 0 {
		t.Fatal("active reader not pending", status, e, safeFailure(e))
	}
	var state string
	if e = f.store.QueryRow(ctxFor(t), `SELECT state FROM agenteam_object.objects WHERE id=$1`, old.String()).Scan(&state); e != nil || state == "deleted" {
		t.Fatal("active reader payload removed", state, e)
	}
	if e = r.Close(); e != nil {
		t.Fatal("reader actual close", e)
	}
	if _, e = f.profiles.RecoverAvatars(ctxFor(t)); e != nil {
		t.Fatal("post reader cleanup", e, safeFailure(e))
	}
	if e = f.store.QueryRow(ctxFor(t), `SELECT state FROM agenteam_object.objects WHERE id=$1`, old.String()).Scan(&state); e != nil || state != "deleted" {
		t.Fatal("joined reader still blocks payload", state, e)
	}
}
