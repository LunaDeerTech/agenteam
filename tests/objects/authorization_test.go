//go:build integration

package objects_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestObjectReplayCurrentAuthorityBeforeGateAndVersion(t *testing.T) {
	f := newFixture(t, false)
	expected := foundation.Version(1)
	cmd := command(t, "versioned")
	cmd.ExpectedVersion = &expected
	body := "same bytes"
	media := `text/plain; name="private-business-name"`
	first, err := f.service.PutObject(contextFor(t), f.actor, f.owner, cmd, media, int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
	if err != nil {
		t.Fatal(err)
	}
	session := id[identity.Session](t)
	f.sql(t, `INSERT INTO object_fixture.sessions(id,user_id) VALUES($1,$2)`, session.String(), f.actor.Details().UserID)
	user, _ := foundation.ParseID[identity.User](f.actor.Details().UserID)
	newSession, _ := identity.NewHuman(user, session)
	f.sql(t, `UPDATE object_fixture.owners SET version=2`)
	f.sql(t, `UPDATE object_fixture.projects SET state='archived'`)
	same := digest([]byte(body))
	replay, err := f.service.PutObject(contextFor(t), newSession, f.owner, cmd, media, int64(len(body)), &same, io.NopCloser(strings.NewReader(body)))
	if err != nil || replay.Meta.ID != first.Meta.ID {
		t.Fatal("successful replay was incorrectly gated/versioned/session-bound", err)
	}
	_, err = f.service.PutObject(contextFor(t), newSession, f.owner, command(t, "new-archived"), media, int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
	requireCode(t, err, foundation.ProjectNotActive)
	reader, err := f.service.ReadObject(contextFor(t), newSession, f.owner, first.Meta.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(got) != body {
		t.Fatal("archived read failed", err)
	}
	var auditCount int64
	var named bool
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*),coalesce(bool_or(metadata::text LIKE '%private-business-name%'),false) FROM agenteam_audit.audit_records`).Scan(&auditCount, &named); err != nil || auditCount != 1 || named {
		t.Fatal("audit replay or MIME projection", err)
	}
	f.sql(t, `UPDATE object_fixture.projects SET owner_id=$1`, id[identity.User](t).String())
	_, err = f.service.LookupPut(contextFor(t), newSession, f.owner, cmd.IdempotencyKey)
	requireCode(t, err, foundation.Forbidden)
	_, err = f.service.PutObject(contextFor(t), newSession, f.owner, cmd, media, int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
	requireCode(t, err, foundation.Forbidden)
}
func TestObjectConsumeAndCancelLinearizeWithCompleteLockPlan(t *testing.T) {
	f := newFixture(t, true)
	result := f.put(t, "consume-race", "body")
	f.sql(t, `UPDATE object_fixture.owners SET existence='existing'`)
	// Bare IDs never consume a prospective reservation, even after the business
	// entity becomes real. The matching receipt is still required.
	accessPlan1 := ownerPlan(t, f.service, f.actor, f.owner, oc.AttachAccess, oc.AccessRequestDetails{ObjectID: result.Meta.ID})
	r := plannedTx(f.store, f.service, contextFor(t), cause(t), accessPlan1, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		_, err := f.service.AttachObjectInTx(ctx, tx, f.actor, f.owner, result.Meta.ID, plan, locked)
		return err
	})
	requireCode(t, r.Fault(), foundation.Forbidden)
	ctx := contextFor(t)
	held, release := make(chan struct{}), make(chan struct{})
	consumed := make(chan foundation.CommitResult, 1)
	var consumePID int32
	c := cause(t)
	go func() {
		accessPlan2 := ownerPlan(t, f.service, f.actor, f.owner, oc.ConsumeAccess, oc.AccessRequestDetails{Receipt: result.Receipt})
		consumed <- plannedTx(f.store, f.service, ctx, c, accessPlan2, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
			_, err := f.service.ConsumeUploadInTx(ctx, tx, f.actor, f.owner, result.Receipt, plan, locked)
			if err != nil {
				return err
			}
			e, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			if err = e.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&consumePID); err != nil {
				return err
			}
			close(held)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-held:
	case <-ctx.Done():
		t.Fatal("consume did not reach transaction barrier")
	}
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	// The complete plan holds both locks. Command precedes Object globally,
	// so Cancel must wait at that exact command while Consume retains Object EX.
	objectKey, _ := foundation.AggregateLock(foundation.ObjectAggregate, result.Meta.ID.String())
	identity, err := foundation.NewCommandIdentity("object", []string{f.project.String(), f.owner.Details().ID}, "put."+string(f.owner.Details().Kind), "consume-race")
	if err != nil {
		t.Fatal(err)
	}
	commandKey, _ := foundation.CommandLock(identity)
	for _, key := range []foundation.LockKey{commandKey, objectKey} {
		hash := uint64(key.AdvisoryKey())
		var held bool
		err := f.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a USING(pid) WHERE a.datname=$1 AND l.pid=$2 AND locktype='advisory' AND granted AND mode='ExclusiveLock' AND classid::bigint=$3 AND objid::bigint=$4 AND objsubid=1)`, f.db.Name, consumePID, int64(hash>>32), int64(hash&0xffffffff)).Scan(&held)
		if err != nil || !held {
			t.Fatal("consume backend did not hold exact command and object exclusively", err)
		}
	}
	cancelled := make(chan error, 1)
	go func() { _, err := f.service.CancelUpload(ctx, f.actor, f.owner, "consume-race"); cancelled <- err }()
	hash := uint64(commandKey.AdvisoryKey())
	waitCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting bool
		err := f.store.QueryRow(waitCtx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a USING(pid) WHERE a.datname=$1 AND locktype='advisory' AND NOT granted AND classid::bigint=$2 AND objid::bigint=$3 AND objsubid=1 AND $4=ANY(pg_blocking_pids(l.pid)))`, f.db.Name, int64(hash>>32), int64(hash&0xffffffff), consumePID).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-tick.C:
		case <-waitCtx.Done():
			t.Fatal("cancel did not wait on consume backend's exact command lock")
		}
	}
	close(release)
	released = true
	if r := <-consumed; r.State() != foundation.Committed {
		t.Fatal(r.Fault())
	}
	requireCode(t, <-cancelled, foundation.InvalidState)
	accessPlan3 := ownerPlan(t, f.service, f.actor, f.owner, oc.ConsumeAccess, oc.AccessRequestDetails{Receipt: result.Receipt})
	r = plannedTx(f.store, f.service, ctx, cause(t), accessPlan3, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		_, err := f.service.ConsumeUploadInTx(ctx, tx, f.actor, f.owner, result.Receipt, plan, locked)
		return err
	})
	if r.State() != foundation.Committed {
		t.Fatal("same receipt replay not idempotent", r.Fault())
	}
	refs, err := f.service.InspectReferences(ctx, result.Meta.ID)
	if err != nil || len(refs.References) != 1 || refs.References[0].Kind != oc.CanonicalReference {
		t.Fatal("consume/cancel left inconsistent reference", err)
	}
	// Conversely, a committed cancellation prevents a later business Tx from
	// binding; the original storage command remains committed in its fault.
	other := newFixture(t, true)
	upload := other.put(t, "cancel-first", "body")
	if _, err = other.service.CancelUpload(contextFor(t), other.actor, other.owner, "cancel-first"); err != nil {
		t.Fatal(err)
	}
	other.sql(t, `UPDATE object_fixture.owners SET existence='existing'`)
	accessPlan4 := ownerPlan(t, other.service, other.actor, other.owner, oc.ConsumeAccess, oc.AccessRequestDetails{Receipt: upload.Receipt})
	r = plannedTx(other.store, other.service, contextFor(t), cause(t), accessPlan4, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		_, err := other.service.ConsumeUploadInTx(ctx, tx, other.actor, other.owner, upload.Receipt, plan, locked)
		return err
	})
	if r.State() == foundation.Committed {
		t.Fatal("revoked receipt consumed")
	}
	_, err = other.service.PutObject(contextFor(t), other.actor, other.owner, command(t, "cancel-first"), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
	requireCode(t, err, foundation.ResourceDeleted)
	var fault *foundation.Fault
	if !errors.As(err, &fault) || fault.CommitState != foundation.Committed {
		t.Fatal("revoked original success lost committed fact")
	}
}
func TestObjectReadCommitUnknownDoesNotOpenStorageAndRetainsReleaseCheckpoint(t *testing.T) {
	f := newFixture(t, false)
	stored := f.put(t, "read-unknown", strings.Repeat("r", 130000))
	proxy := newStorageProxy(t, f)
	store, p := proxyStore(t, f, true)
	service, _ := f.on(t, store, proxy.server.URL, nil)
	f.sql(t, `CREATE FUNCTION object_fixture.reject_release() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='released' THEN RAISE EXCEPTION 'owned release barrier'; END IF; RETURN NEW; END $$`)
	f.sql(t, `CREATE TRIGGER reject_release BEFORE UPDATE ON agenteam_object.object_leases FOR EACH ROW EXECUTE FUNCTION object_fixture.reject_release()`)
	p.armed.Store(1)
	done := make(chan error, 1)
	ctx := contextFor(t)
	go func() {
		reader, err := service.ReadObject(ctx, f.actor, f.owner, stored.Meta.ID, nil)
		if reader != nil {
			_ = reader.Close()
		}
		done <- err
	}()
	select {
	case <-p.reached:
	case <-time.After(3 * time.Second):
		t.Fatal("reader lease commit not intercepted")
	}
	if proxy.gets.Load() != 0 {
		t.Fatal("storage opened before confirmed read lease")
	}
	close(p.release)
	select {
	case err := <-done:
		requireCode(t, err, foundation.CommitUnknown)
	case <-time.After(3 * time.Second):
		t.Fatal("unknown read did not return")
	}
	var active int64
	if err := f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='reader' AND state='active'`).Scan(&active); err != nil || active != 1 {
		t.Fatal("unresolved internal release was forgotten", err)
	}
	if proxy.gets.Load() != 0 {
		t.Fatal("unknown lease opened speculative stream")
	}
	f.sql(t, `DROP TRIGGER reject_release ON agenteam_object.object_leases`)
	if err := service.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='reader' AND state='active'`).Scan(&active); err != nil || active != 0 {
		t.Fatal("exact reader lease checkpoint not retried", err)
	}
}

func TestObjectAuditFailureCannotPublishAndDoesNotRepeatVerifiedPUT(t *testing.T) {
	f := newFixture(t, false)
	proxy := newStorageProxy(t, f)
	service, _ := f.on(t, f.store, proxy.server.URL, nil)
	f.sql(t, `CREATE FUNCTION object_fixture.reject_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'owned audit barrier'; END $$`)
	f.sql(t, `CREATE TRIGGER reject_audit BEFORE INSERT ON agenteam_audit.audit_records FOR EACH ROW EXECUTE FUNCTION object_fixture.reject_audit()`)
	_, err := service.PutObject(contextFor(t), f.actor, f.owner, command(t, "audit-failure"), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
	if err == nil {
		t.Fatal("publication returned without audit")
	}
	var objects, canonical, events int64
	if err = f.store.QueryRow(contextFor(t), `SELECT (SELECT count(*) FROM agenteam_object.objects WHERE state='available'),(SELECT count(*) FROM agenteam_object.object_references WHERE kind='canonical'),(SELECT count(*) FROM agenteam_audit.audit_records)`).Scan(&objects, &canonical, &events); err != nil || objects != 0 || canonical != 0 || events != 0 {
		t.Fatal("partial publication escaped rollback", err)
	}
	f.sql(t, `DROP TRIGGER reject_audit ON agenteam_audit.audit_records`)
	result, err := service.PutObject(contextFor(t), f.actor, f.owner, command(t, "audit-failure"), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
	if err != nil || result.Meta.State != oc.Available || proxy.puts.Load() != 1 {
		t.Fatal("publication recovery repeated verified payload", err)
	}
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE action='object.upload.complete'`).Scan(&events); err != nil || events != 1 {
		t.Fatal("original success audit lost or duplicated", err)
	}
}
func TestObjectAvatarIsExactUserAndSeparateFromProject(t *testing.T) {
	f := newFixture(t, false)
	projectObject := f.put(t, "project-bytes", "project")
	avatar, _ := oc.NewObjectOwner(oc.Avatar, f.actor.Details().UserID, "")
	f.sql(t, `INSERT INTO object_fixture.owners(id,kind,user_id,existence) VALUES($1,'avatar',$1,'existing')`, f.actor.Details().UserID)
	result, err := f.service.PutObject(contextFor(t), f.actor, avatar, command(t, "avatar"), "image/png", 6, nil, io.NopCloser(strings.NewReader("avatar")))
	if err != nil || !result.Meta.Scope.Equal(identity.SystemScope()) {
		t.Fatal("exact user avatar failed", err)
	}
	_, err = f.service.StatObject(contextFor(t), f.actor, avatar, projectObject.Meta.ID)
	requireCode(t, err, foundation.Forbidden)
	_, err = f.service.StatObject(contextFor(t), f.actor, f.owner, result.Meta.ID)
	requireCode(t, err, foundation.Forbidden)
	otherUser, otherSession := id[identity.User](t), id[identity.Session](t)
	other, _ := identity.NewHuman(otherUser, otherSession)
	f.sql(t, `INSERT INTO object_fixture.sessions(id,user_id) VALUES($1,$2)`, otherSession.String(), otherUser.String())
	_, err = f.service.StatObject(contextFor(t), other, avatar, result.Meta.ID)
	requireCode(t, err, foundation.Forbidden)
	var leaked bool
	if err = f.store.QueryRow(contextFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_audit.audit_records WHERE resource_id=$1 AND (scope<>'system' OR project_id IS NOT NULL))`, result.Meta.ID.String()).Scan(&leaked); err != nil || leaked {
		t.Fatal("avatar audit crossed project partition", err)
	}
}

func TestObjectMaintenanceCancelsAvatarOnlyWithExactPersistentCause(t *testing.T) {
	f := newFixture(t, false)
	avatar, _ := oc.NewObjectOwner(oc.Avatar, f.actor.Details().UserID, "")
	f.sql(t, `INSERT INTO object_fixture.owners(id,kind,user_id,existence) VALUES($1,'avatar',$1,'existing')`, f.actor.Details().UserID)
	attempt, err := reserveOnly(t, f, f.service, avatar, "avatar-pending")
	if err != nil {
		t.Fatal(err)
	}
	operation := id[oc.CleanupOperation](t)
	f.sql(t, `INSERT INTO object_fixture.cleanup(operation_id,object_id,owner_id) VALUES($1,$2,$3)`, operation.String(), attempt.Details().ObjectID.String(), avatar.Details().ID)
	registration, err := identity.RegisterService(identity.ObjectMaintenance)
	if err != nil {
		t.Fatal(err)
	}
	maintenance, err := registration.Actor(operation.String(), avatar.Scope())
	if err != nil {
		t.Fatal(err)
	}
	forged, _ := registration.Actor(id[oc.CleanupOperation](t).String(), avatar.Scope())
	_, err = f.service.CancelUpload(contextFor(t), forged, avatar, "avatar-pending")
	requireCode(t, err, foundation.Forbidden)
	otherRegistration, _ := identity.RegisterService(identity.ObjectService)
	otherService, _ := otherRegistration.Actor(operation.String(), avatar.Scope())
	_, err = f.service.CancelUpload(contextFor(t), otherService, avatar, "avatar-pending")
	requireCode(t, err, foundation.Forbidden)
	anotherAvatar, _ := oc.NewObjectOwner(oc.Avatar, id[identity.User](t).String(), "")
	_, err = f.service.CancelUpload(contextFor(t), maintenance, anotherAvatar, "avatar-pending")
	requireCode(t, err, foundation.Forbidden)
	_, err = f.service.CancelUpload(contextFor(t), maintenance, avatar, "missing-command")
	requireCode(t, err, foundation.Forbidden)
	_, err = f.service.StatObject(contextFor(t), maintenance, avatar, attempt.Details().ObjectID)
	if err == nil {
		t.Fatal("maintenance became avatar read permission")
	}
	result, err := f.service.CancelUpload(contextFor(t), maintenance, avatar, "avatar-pending")
	if err != nil || result.Cleanup != oc.CleanupCompleted || result.State != oc.UploadRevoked {
		t.Fatal("exact avatar maintenance cause rejected", err)
	}
}
