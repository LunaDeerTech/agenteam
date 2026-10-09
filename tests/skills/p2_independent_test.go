//go:build integration

package skill_test

// Independent P2 supplement. Real Skill/Object/Account/Project/Audit/SQL, with
// the original owned fixture. Human/Creation/lifecycle are explicit upstream
// seeds and Object Runtime is nil. No cleanup/root/foreign-process claim.
import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type independentP2Facts struct {
	skill, revision, object, upload, attempt, digest string
	size                                             int64
	skills, revisions, attempts, canonical, audits   int
	work, running, readers, activeReaders            int
}

func independentP2Snapshot(t *testing.T, x *skillObjectFixture) independentP2Facts {
	t.Helper()
	var out independentP2Facts
	err := x.pg.store.QueryRow(testContext(t), `SELECT i.skill_id::text,i.revision_id::text,i.object_id::text,i.upload_id::text,i.current_attempt_id::text,encode(o.sha256,'hex'),o.byte_size,
 (SELECT count(*) FROM agenteam_skill.skills s WHERE s.project_id=i.project_id AND s.protected AND s.serving AND s.current_revision=1),
 (SELECT count(*) FROM agenteam_skill.revisions r WHERE r.project_id=i.project_id AND r.skill_id=i.skill_id AND r.object_id=i.object_id),
 (SELECT count(*) FROM agenteam_skill.object_attempts a WHERE a.project_id=i.project_id),
 (SELECT count(*) FROM agenteam_object.object_references r WHERE r.object_id=i.object_id AND r.owner_kind='skill_revision' AND r.owner_id=i.revision_id AND r.kind='canonical'),
 (SELECT count(*) FROM agenteam_audit.audit_records a WHERE a.project_id=i.project_id AND a.producer='object' AND a.resource_id=i.object_id),
 (SELECT count(*) FROM agenteam_skill.work w WHERE w.project_id=i.project_id),
 (SELECT count(*) FROM agenteam_skill.work w WHERE w.project_id=i.project_id AND w.phase<>'joined'),
 (SELECT count(*) FROM agenteam_object.object_leases l WHERE l.object_id=i.object_id AND l.owner_kind='reader'),
 (SELECT count(*) FROM agenteam_object.object_leases l WHERE l.object_id=i.object_id AND l.owner_kind='reader' AND l.state='active')
 FROM agenteam_skill.initializations i JOIN agenteam_object.objects o ON o.id=i.object_id
 JOIN agenteam_object.uploads u ON u.id=i.upload_id
 WHERE i.project_id=$1 AND i.phase='published' AND o.state='available' AND u.state='committed' AND u.disposition='attached'`, x.seed.request.ProjectID.String()).Scan(
		&out.skill, &out.revision, &out.object, &out.upload, &out.attempt, &out.digest, &out.size,
		&out.skills, &out.revisions, &out.attempts, &out.canonical, &out.audits, &out.work, &out.running, &out.readers, &out.activeReaders)
	if err != nil || out.skills != 1 || out.revisions != 1 || out.attempts != 1 || out.canonical != 1 || out.audits != 1 || out.size <= 0 || "sha256:"+out.digest != string(skill.AddSkillsPackageSHA256) {
		t.Fatal("independent original Skill/Object/canonical/Audit mapping", err)
	}
	return out
}

func independentP2Unchanged(t *testing.T, x *skillObjectFixture, before independentP2Facts) {
	t.Helper()
	if independentP2Snapshot(t, x) != before || x.facts.calls.Load() != 1 || len(x.seed.objects.steps) != 0 {
		t.Fatal("read/confirmation replaced original publication or made extra Object work")
	}
}

func independentP2Confirm(t *testing.T, x *skillObjectFixture, service *skill.Service, plan pc.InitializationConfirmationPlan, locks []f.LockRequest, wantError bool) error {
	t.Helper()
	var receipt pc.InitializationReceipt
	var callbackErr error
	result := x.pg.store.WithinTx(testContext(t), testCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := x.pg.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		receipt, callbackErr = service.ConfirmInitializedInTx(ctx, tx, x.seed.actor, x.seed.request, plan)
		return callbackErr
	})
	if wantError {
		if callbackErr == nil || result.State() != f.NotCommitted || receipt != (pc.InitializationReceipt{}) {
			t.Fatal("original plan denial did not roll back with an empty receipt")
		}
	} else if callbackErr != nil || result.State() != f.Committed || receipt != plan.ProposedReceipt() || !receipt.Matches(x.seed.request) {
		t.Fatal("same Store/live Tx/full locks did not confirm original receipt", callbackErr)
	}
	return callbackErr
}

// Only the original D05 ObjectReader's return boundary is delayed. The actual
// implementation receives the caller's unchanged ctx/actor/owner/id/range;
// reads, bytes, errors, metadata and Close all come from that same reader.
type independentP2Objects struct {
	skill.ObjectPorts
	held  *independentP2Held
	calls atomic.Int64
}

func (o *independentP2Objects) ReadObject(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, object oc.ObjectID, r *oc.ByteRange) (*oc.ObjectReader, error) {
	o.calls.Add(1)
	body, err := o.ObjectPorts.ReadObject(ctx, actor, owner, object, r)
	if err != nil || body == nil {
		return body, err
	}
	o.held.original = body
	wrapped, err := oc.NewObjectReader(body.Meta(), body.Range(), o.held)
	if err != nil {
		_ = body.Close()
	}
	return wrapped, err
}

type independentP2Held struct {
	original                                               *oc.ObjectReader
	readReached, closeReached                              chan struct{}
	releaseRead, releaseClose                              chan struct{}
	readOnce, closeOnce, releaseReadOnce, releaseCloseOnce sync.Once
	readCalls, closeCalls                                  atomic.Int64
	readN                                                  int
	readErr, closeErr                                      error
}

func (h *independentP2Held) Read(p []byte) (int, error) {
	h.readCalls.Add(1)
	n, err := h.original.Read(p)
	h.readN, h.readErr = n, err
	h.readOnce.Do(func() { close(h.readReached) })
	<-h.releaseRead
	return n, err
}
func (h *independentP2Held) Close() error {
	h.closeCalls.Add(1)
	err := h.original.Close()
	h.closeErr = err
	h.closeOnce.Do(func() { close(h.closeReached) })
	<-h.releaseClose
	return err
}
func (h *independentP2Held) releaseReadCall() { h.releaseReadOnce.Do(func() { close(h.releaseRead) }) }
func (h *independentP2Held) releaseCloseCall() {
	h.releaseCloseOnce.Do(func() { close(h.releaseClose) })
}
func independentP2Wait(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("original call boundary unavailable: " + label)
	}
}

func TestSkillIndependentP2ConfirmationAndPackage(t *testing.T) {
	x := newSkillObjectFixture(t)
	completed, err := x.service.InitializeProjectSkills(testContext(t), x.seed.actor, x.seed.request)
	if err != nil || completed.State != pc.InitializationCompleted || !completed.Matches(x.seed.request) || completed.AddSkillsID == nil {
		t.Fatal("real D05 publication prerequisite", err)
	}
	baseline := independentP2Snapshot(t, x)
	if baseline.skill != completed.AddSkillsID.String() || baseline.work != 1 || baseline.running != 0 || baseline.readers != 0 || baseline.activeReaders != 0 {
		t.Fatal("publication prerequisite has unexpected persistent work")
	}
	initializeReaderAccountKeys(t, x.pg)
	owner := seedReaderHuman(t, x.pg, x.seed.owner, "p2-independent-owner", "user")
	foreign := seedReaderHuman(t, x.pg, testID[id.User](t), "p2-independent-foreign", "user")
	admin := seedReaderHuman(t, x.pg, testID[id.User](t), "p2-independent-admin", "admin")
	// Canonical Project/Creation completion is a seed, not Project's command.
	seedReaderReadyProject(t, x.pg, x.seed, *completed.AddSkillsID)

	t.Run("same_store_confirmation_and_private_plan", func(t *testing.T) {
		plan, err := x.service.DiscoverConfirmation(testContext(t), x.seed.actor, x.seed.request)
		if err != nil {
			t.Fatal(err)
		}
		independentP2Confirm(t, x, x.service, plan, plan.RequiredLocks(), false)
		weak := plan.RequiredLocks()
		objectLock, err := f.AggregateLock(f.ObjectAggregate, baseline.object)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for i := range weak {
			if weak[i].Key.Canonical() == objectLock.Canonical() {
				weak[i].Mode = f.Shared
				found = true
			}
		}
		if !found {
			t.Fatal("original confirmation plan omitted Object parent")
		}
		independentP2Confirm(t, x, x.service, plan, weak, true)
		fresh := x.newService(t)
		independentP2Confirm(t, x, fresh, plan, plan.RequiredLocks(), true)
		newPlan, err := fresh.DiscoverConfirmation(testContext(t), x.seed.actor, x.seed.request)
		if err != nil {
			t.Fatal(err)
		}
		independentP2Confirm(t, x, fresh, newPlan, newPlan.RequiredLocks(), false)
		independentP2Unchanged(t, x, baseline)
	})
	t.Run("original_plan_rechecks_current_project_gate", func(t *testing.T) {
		plan, err := x.service.DiscoverConfirmation(testContext(t), x.seed.actor, x.seed.request)
		if err != nil {
			t.Fatal(err)
		}
		// Legitimate fixture transition; neither Archive nor Restore command is claimed.
		readerMutation(t, x.pg, x.seed, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=clock_timestamp(),updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, x.seed.request.ProjectID.String())
		denied := independentP2Confirm(t, x, x.service, plan, plan.RequiredLocks(), true)
		var fault *f.Fault
		if !errors.As(denied, &fault) || fault.Code != f.Forbidden {
			t.Fatal("current archived Project gate was not rechecked", denied)
		}
		independentP2Unchanged(t, x, baseline)
		readerMutation(t, x.pg, x.seed, `UPDATE agenteam_project.projects SET lifecycle='active',archived_at=NULL,updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, x.seed.request.ProjectID.String())
		independentP2Confirm(t, x, x.service, plan, plan.RequiredLocks(), false)
		independentP2Unchanged(t, x, baseline)
	})
	t.Run("owner_package_current_session_and_foreign_denials", func(t *testing.T) {
		wrongSession, err := f.ParseID[id.Session](foreign.Details().SessionID)
		if err != nil {
			t.Fatal(err)
		}
		wrong, err := id.NewHuman(x.seed.owner, wrongSession)
		if err != nil {
			t.Fatal(err)
		}
		for _, denial := range []struct {
			actor id.Actor
			code  f.Code
		}{{wrong, f.Unauthenticated}, {foreign, f.NotFound}, {admin, f.NotFound}} {
			body, err := x.service.OpenPackage(testContext(t), denial.actor, x.seed.request.ProjectID, *completed.AddSkillsID, 1)
			if body != nil {
				_ = body.Close()
				t.Fatal("denied reader delivered a body")
			}
			var fault *f.Fault
			if !errors.As(err, &fault) || fault.Code != denial.code {
				t.Fatal("current package denial changed", err)
			}
			independentP2Unchanged(t, x, baseline)
		}
		body, err := x.service.OpenPackage(testContext(t), owner, x.seed.request.ProjectID, *completed.AddSkillsID, 1)
		if err != nil {
			t.Fatal(err)
		}
		defer body.Close()
		archive, err := x.bundle.Package()
		if err != nil {
			t.Fatal(err)
		}
		want, err := archive.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(body)
		if err != nil || !bytes.Equal(got, want) || body.Joined() {
			t.Fatal("actual package bytes or EOF/local join", err)
		}
		if err = body.Close(); err != nil || !body.Joined() {
			t.Fatal("actual positive package Close", err)
		}
		after := baseline
		after.work++
		after.readers++
		independentP2Unchanged(t, x, after)
	})
	t.Run("original_d05_read_close_returns_and_skill_accounting", func(t *testing.T) {
		for _, readFirst := range []bool{true, false} {
			before := independentP2Snapshot(t, x)
			held := &independentP2Held{readReached: make(chan struct{}), closeReached: make(chan struct{}), releaseRead: make(chan struct{}), releaseClose: make(chan struct{})}
			ports := &independentP2Objects{ObjectPorts: x.objects, held: held}
			service, err := skill.New(skill.Dependencies{Authority: x.seed.authority, Objects: ports, Processes: x.guard, ProcessID: x.process, Bundle: x.bundle})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(testContext(t))
			readDone := make(chan struct{})
			var n int
			var readErr error
			var body *sc.PackageReader
			started := false
			t.Cleanup(func() {
				cancel()
				held.releaseReadCall()
				held.releaseCloseCall()
				if started {
					independentP2Wait(t, readDone, "reader cleanup")
				}
				cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
				defer done()
				if err := service.Drain(cleanup); err != nil || !service.Joined() {
					t.Error("independent reader service cleanup", err)
				}
				if body != nil {
					_ = body.Close()
					if !body.Joined() {
						t.Error("independent P1 reader did not actually close")
					}
				}
			})
			body, err = service.OpenPackage(ctx, owner, x.seed.request.ProjectID, *completed.AddSkillsID, 1)
			if err != nil {
				t.Fatal(err)
			}
			// This fixed small bundle is eagerly validated by D05. Its released
			// durable reader lease is intentionally distinct from held Skill work.
			if before.size > oc.StreamBufferSize {
				t.Fatal("small-package boundary prerequisite changed")
			}
			one := make([]byte, 1)
			started = true
			go func() { defer close(readDone); n, readErr = body.Read(one) }()
			independentP2Wait(t, held.readReached, "original D05 Read returned")
			cancel()
			service.Stop()
			independentP2Wait(t, held.closeReached, "original D05 Close returned")
			if held.readN != 1 || held.readErr != nil || held.closeErr != nil {
				t.Fatal("original native reader boundary result", held.readErr, held.closeErr)
			}
			assertHeld := func() {
				t.Helper()
				if body.Joined() || service.Joined() {
					t.Fatal("held actual reader return reported joined")
				}
				want := before
				want.work++
				want.running++
				want.readers++
				independentP2Unchanged(t, x, want)
			}
			assertHeld()
			cancelled, done := context.WithCancel(context.Background())
			done()
			if !errors.Is(service.Drain(cancelled), context.Canceled) {
				t.Fatal("cancelled drain reported success")
			}
			if readFirst {
				held.releaseReadCall()
				independentP2Wait(t, readDone, "actual P1 Read returned")
				assertHeld()
				held.releaseCloseCall()
			} else {
				held.releaseCloseCall()
				if err = body.Close(); err != held.closeErr {
					t.Fatal("original Close return replaced before Read joined", err)
				}
				assertHeld()
				held.releaseReadCall()
				independentP2Wait(t, readDone, "actual P1 Read returned")
			}
			cleanup, finish := context.WithTimeout(context.Background(), 3*time.Second)
			err = service.Drain(cleanup)
			finish()
			if err != nil || !service.Joined() {
				t.Fatal("actual callback/work retirement did not join", err)
			}
			if n != held.readN || readErr != held.readErr || n != 1 || one[0] != 'P' {
				t.Fatal("original ZIP Read bytes/error changed", readErr)
			}
			closeErr := body.Close()
			if readFirst {
				var fault *f.Fault
				if !errors.Is(closeErr, context.Canceled) || !errors.As(closeErr, &fault) || fault.Code != f.DependencyUnavailable {
					t.Fatal("cancelled original accounting error was replaced", closeErr)
				}
			} else if closeErr != held.closeErr {
				t.Fatal("already returned Close error was rewritten", closeErr)
			}
			if !body.Joined() || ports.calls.Load() != 1 || held.readCalls.Load() != 1 || held.closeCalls.Load() != 1 {
				t.Fatal("original reader multiplicity/local join")
			}
			after := before
			after.work++
			after.readers++
			independentP2Unchanged(t, x, after)
		}
	})
}
