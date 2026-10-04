//go:build integration

package objects_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func reserveOnly(t *testing.T, f *fixture, service *object.Service, owner oc.ObjectOwner, key string) (oc.UploadAttempt, error) {
	t.Helper()
	p, err := service.PreparePayload(contextFor(t), f.actor, owner, "text/plain", 0, nil, io.NopCloser(strings.NewReader("")))
	if err != nil {
		return oc.UploadAttempt{}, err
	}
	defer service.DiscardPrepared(p)
	var attempt oc.UploadAttempt
	command := command(t, key)
	accessPlan1 := ownerPlan(t, service, f.actor, owner, oc.ReserveAccess, oc.AccessRequestDetails{Command: &command, Prepared: p})
	result := plannedTx(f.store, service, contextFor(t), cause(t), accessPlan1, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		var err error
		attempt, err = service.ReserveUploadInTx(ctx, tx, f.actor, owner, command, p, plan, locked)
		return err
	})
	if result.State() != foundation.Committed {
		return oc.UploadAttempt{}, result.Fault()
	}
	return attempt, nil
}
func TestObjectGlobalAdmissionSerializesDifferentOwners(t *testing.T) {
	f := newFixture(t, true)
	for n := range 62 {
		if _, err := reserveOnly(t, f, f.service, f.owner, fmt.Sprintf("fill-%d", n)); err != nil {
			t.Fatal(err)
		}
	}
	services := make([]*object.Service, 3)
	owners := make([]oc.ObjectOwner, 3)
	prepared := make([]oc.PreparedPayload, 3)
	for n := range services {
		services[n], _ = f.on(t, f.store, f.remote.Endpoint(), nil)
		owners[n], _ = oc.NewObjectOwner(oc.Artifact, id[struct{}](t).String(), f.project.String())
		f.sql(t, `INSERT INTO object_fixture.owners(id,kind,project_id,user_id,existence,cause) VALUES($1,'artifact',$2,$3,'prospective',$4)`, owners[n].Details().ID, f.project.String(), f.actor.Details().UserID, id[struct{}](t).String())
		var err error
		prepared[n], err = services[n].PreparePayload(contextFor(t), f.actor, owners[n], "text/plain", 0, nil, io.NopCloser(strings.NewReader("")))
		if err != nil {
			t.Fatal(err)
		}
		defer services[n].DiscardPrepared(prepared[n])
	}
	// Hold the exact production global admission key. Each contender has a
	// different command, owner and object, so only this common key may block it.
	key, _ := foundation.SystemConfigLock("object-attempt-admission")
	held := make(chan struct{})
	release := make(chan struct{})
	holder := make(chan foundation.CommitResult, 1)
	ctx := contextFor(t)
	holderCause := cause(t)
	go func() {
		holder <- f.store.WithinTx(ctx, holderCause, func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
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
		t.Fatal("global lock unavailable")
	}
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	done := make(chan foundation.CommitResult, 3)
	for n := range 3 {
		cmd, c := command(t, fmt.Sprintf("contender-%d", n)), cause(t)
		go func() {
			accessPlan2 := ownerPlan(t, services[n], f.actor, owners[n], oc.ReserveAccess, oc.AccessRequestDetails{Command: &cmd, Prepared: prepared[n]})
			done <- plannedTx(f.store, services[n], ctx, c, accessPlan2, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
				_, err := services[n].ReserveUploadInTx(ctx, tx, f.actor, owners[n], cmd, prepared[n], plan, locked)
				return err
			})
		}()
	}
	hash := uint64(key.AdvisoryKey())
	waitCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiters int64
		err := f.store.QueryRow(waitCtx, `SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a USING(pid) WHERE a.datname=$1 AND locktype='advisory' AND NOT granted AND classid::bigint=$2 AND objid::bigint=$3 AND objsubid=1`, f.db.Name, int64(hash>>32), int64(hash&0xffffffff)).Scan(&waiters)
		if err != nil {
			t.Fatal("admission wait observation", err)
		}
		if waiters == 3 {
			break
		}
		select {
		case <-tick.C:
		case <-waitCtx.Done():
			t.Fatal("different owners did not share the DB admission lock")
		}
	}
	close(release)
	released = true
	if result := <-holder; result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	committed, busy := 0, 0
	for range 3 {
		select {
		case result := <-done:
			if result.State() == foundation.Committed {
				committed++
			} else {
				requireCode(t, result.Fault(), foundation.ResourceBusy)
				busy++
			}
		case <-ctx.Done():
			t.Fatal("admission contender did not join")
		}
	}
	var count int64
	if err := f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.upload_attempts WHERE phase NOT IN ('published','cleaned')`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 64 || committed != 2 || busy != 1 {
		t.Fatalf("limit breached count=%d committed=%d busy=%d", count, committed, busy)
	}
}
func TestObjectPerCommandAttemptsRemainBoundedAcrossUnknownVerification(t *testing.T) {
	f := newFixture(t, true)
	proxy := newStorageProxy(t, f)
	service, _ := f.on(t, f.store, proxy.server.URL, nil)
	proxy.mode.Store(proxyRejectCandidateRead)
	for range 2 {
		_, err := service.PutObject(contextFor(t), f.actor, f.owner, command(t, "attempt-bound"), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
		requireCode(t, err, foundation.DependencyUnavailable)
	}
	var objects, attempts, keys int64
	if err := f.store.QueryRow(contextFor(t), `SELECT count(DISTINCT object_id),count(*),count(DISTINCT candidate_key) FROM agenteam_object.upload_attempts`).Scan(&objects, &attempts, &keys); err != nil {
		t.Fatal(err)
	}
	if objects != 1 || attempts != 2 || keys != 2 || proxy.puts.Load() != 2 {
		t.Fatal("unknown attempt did not retain stable identity and distinct keys")
	}
	_, err := service.PutObject(contextFor(t), f.actor, f.owner, command(t, "attempt-bound"), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
	requireCode(t, err, foundation.ResourceBusy)
	if proxy.puts.Load() != 2 {
		t.Fatal("third unresolved attempt reached storage")
	}
	proxy.mode.Store(proxyPass)
	if err = service.Recover(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	result, err := service.PutObject(contextFor(t), f.actor, f.owner, command(t, "attempt-bound"), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
	if err != nil || result.Meta.State != oc.Available || proxy.puts.Load() != 2 || proxy.markers.Load() != 1 || proxy.deletes.Load() != 0 {
		t.Fatal("recovery republished abandoned key or repeated payload", err)
	}
}
