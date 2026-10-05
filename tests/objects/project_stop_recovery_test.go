//go:build integration

package objects_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestObjectProjectStopRecoveryRequiresExactProcessAndJoin(t *testing.T) {
	f := newFixture(t, false)
	a := newObjectStopAuthority(t, f, f.store)
	proxy := newStorageProxy(t, f)
	process := id[oc.Process](t)
	f.service = newObjectStopService(t, f, a, stopServiceOptions{endpoint: proxy.server.URL, process: process})
	stored := f.put(t, "old-native-reader", strings.Repeat("body", oc.StreamBufferSize))
	proxy.mode.Store(proxyHoldReadBody)
	reader, err := f.service.ReadObject(contextFor(t), f.actor, f.owner, stored.Meta.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	assertStopBodyHeld(t, proxy)
	assertStopReaderLifetime(t, f, stored.Meta.ID, process, true)
	s := newObjectStopService(t, f, a, stopServiceOptions{})
	actor, cause := activateObjectStop(t, f, oc.ProjectStopDelete)
	for range 7 {
		report, err := s.RequestProjectStop(contextFor(t), actor, cause)
		if err != nil || report.Details().State != oc.ProjectStopPending {
			t.Fatal("missing local handle guessed external process death", err)
		}
	}
	var active int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='reader' AND state='active'`).Scan(&active); err != nil || active != 1 {
		t.Fatal("external live reader was released", err)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	assertStopReaderLifetime(t, f, stored.Meta.ID, process, false)
	stopUntilSettled(t, s, actor, cause)
	// Exact live flock, actual child kill, historical boot and remote grant
	// independence are additionally exercised against the real shared guard.
	t.Run("real_process_guard", testObjectStopExactGuardDeath)
}

func TestObjectProjectStopNativeFactsAndFairBatches(t *testing.T) {
	f := newFixture(t, false)
	a := newObjectStopAuthority(t, f, f.store)
	proxy := newStorageProxy(t, f)
	process := id[oc.Process](t)
	f.service = newObjectStopService(t, f, a, stopServiceOptions{endpoint: proxy.server.URL, process: process})
	stored := f.put(t, "legacy-first", strings.Repeat("body", oc.StreamBufferSize))
	proxy.mode.Store(proxyHoldReadBody)
	reader, err := f.service.ReadObject(contextFor(t), f.actor, f.owner, stored.Meta.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	assertStopBodyHeld(t, proxy)
	assertStopReaderLifetime(t, f, stored.Meta.ID, process, true)
	// This real opened native lease represents a pre-registry deployment.
	f.sql(t, `DELETE FROM agenteam_object.project_work WHERE object_id=$1`, stored.Meta.ID.String())
	var legacyActive bool
	if err = f.store.QueryRow(contextFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE object_id=$1 AND process_id=$2 AND owner_kind='reader' AND state='active') AND NOT EXISTS(SELECT 1 FROM agenteam_object.project_work WHERE object_id=$1)`, stored.Meta.ID.String(), process.String()).Scan(&legacyActive); err != nil || !legacyActive {
		t.Fatal("legacy native lease was not active without registry evidence", err)
	}
	s := newObjectStopService(t, f, a, stopServiceOptions{})
	f.service = s
	for i := 0; i < 55; i++ {
		f.put(t, foundationKeyForStop(i), "later")
	}
	actor, cause := activateObjectStop(t, f, oc.ProjectStopDelete)
	var cursorMoved bool
	for range 12 {
		report, err := s.RequestProjectStop(contextFor(t), actor, cause)
		if err != nil || report.Details().State != oc.ProjectStopPending {
			t.Fatal("legacy native lease was ignored", err)
		}
		var lane int
		var after *string
		if err = f.store.QueryRow(contextFor(t), `SELECT scan_kind,scan_after::text FROM agenteam_object.project_stops WHERE operation_id=$1`, cause.Details().OperationID.String()).Scan(&lane, &after); err != nil {
			t.Fatal(err)
		}
		cursorMoved = cursorMoved || lane != 0 || after != nil
	}
	if !cursorMoved {
		t.Fatal("low unresolved native record starved later pages")
	}
	var unrevoked int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.project_work WHERE project_id=$1 AND revoked_by IS NULL`, f.project.String()).Scan(&unrevoked); err != nil || unrevoked != 0 {
		t.Fatal("later independent work never advanced", err, unrevoked)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	stopUntilSettled(t, s, actor, cause)
}
func foundationKeyForStop(i int) string { return "fair-" + strconv.Itoa(i) }

func TestObjectProjectStopRuntimeRecoversOnlyTechnicalRows(t *testing.T) {
	for _, kind := range []string{"work", "stopping"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, false)
			if kind == "work" {
				f.sql(t, `INSERT INTO agenteam_object.project_work(id,project_id,process_id,kind,resource_id,admission_version) VALUES($1,$2,$3,'preparation',$1,0)`, id[struct{}](t).String(), f.project.String(), id[oc.Process](t).String())
			} else {
				f.sql(t, `INSERT INTO agenteam_object.project_stops(project_id,operation_id,action,project_version) VALUES($1,$2,'archive',2)`, f.project.String(), id[oc.ProjectStopOperation](t).String())
			}
			runtime, _, _ := runtimeFixture(t, f)
			requireCode(t, runtime.Initialize(contextFor(t)), foundation.DependencyUnbound)
		})
	}
}

func TestObjectProjectStopCleanupClaimOverwriteKeepsOldWorker(t *testing.T) {
	f := newFixture(t, true)
	a := newObjectStopAuthority(t, f, f.store)
	planner := &recoveryItemPlanner{base: f.authority}
	s := newObjectStopService(t, f, a, stopServiceOptions{planner: planner})
	f.service = s
	f.put(t, "overwrite-cleanup", "body")
	entered, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	first := true
	var once sync.Once
	defer once.Do(func() { close(release) })
	planner.set(func(d oc.AccessRequestDetails) error {
		if d.Operation != oc.CheckpointCleanupAccess {
			return nil
		}
		mu.Lock()
		block := first
		first = false
		mu.Unlock()
		if block {
			close(entered)
			<-release
		}
		return nil
	})
	done := make(chan error, 1)
	go func() { _, err := s.CancelUpload(contextFor(t), f.actor, f.owner, "overwrite-cleanup"); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first actual cleanup claim never entered checkpoint")
	}
	var oldWorker, resource string
	var fence int64
	if err := f.store.QueryRow(contextFor(t), `SELECT worker_id::text,id::text,fence FROM agenteam_object.cleanup_operations`).Scan(&oldWorker, &resource, &fence); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelUpload(contextFor(t), f.actor, f.owner, "overwrite-cleanup"); err != nil {
		t.Fatal("independent replacement claim", err)
	}
	var current string
	if err := f.store.QueryRow(contextFor(t), `SELECT worker_id::text FROM agenteam_object.cleanup_operations`).Scan(&current); err != nil || current == oldWorker {
		t.Fatal("native worker was not actually overwritten", err)
	}
	var preserved bool
	if err := f.store.QueryRow(contextFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_object.project_work WHERE id=$1 AND resource_id=$2 AND cleanup_claim_fence=$3 AND joined_at IS NULL)`, oldWorker, resource, fence).Scan(&preserved); err != nil || !preserved {
		t.Fatal("new claim erased old worker evidence", err)
	}
	actor, cause := activateObjectStop(t, f, oc.ProjectStopArchive)
	for _, field := range []string{"resource_id", "cleanup_claim_fence", "process_id"} {
		t.Run(field, func(t *testing.T) {
			var original string
			if err := f.store.QueryRow(contextFor(t), `SELECT `+field+`::text FROM agenteam_object.project_work WHERE id=$1`, oldWorker).Scan(&original); err != nil {
				t.Fatal(err)
			}
			if field == "cleanup_claim_fence" {
				f.sql(t, `UPDATE agenteam_object.project_work SET cleanup_claim_fence=cleanup_claim_fence+20 WHERE id=$1`, oldWorker)
			} else {
				f.sql(t, `UPDATE agenteam_object.project_work SET `+field+`=$2 WHERE id=$1`, oldWorker, id[struct{}](t).String())
			}
			report, err := s.RequestProjectStop(contextFor(t), actor, cause)
			if err == nil || report.Details().State == oc.ProjectStopped {
				t.Fatal("same worker accepted changed immutable identity")
			}
			f.sql(t, `UPDATE agenteam_object.project_work SET `+field+`=$2 WHERE id=$1`, oldWorker, original)
		})
	}
	report, err := s.RequestProjectStop(contextFor(t), actor, cause)
	if err != nil || report.Details().State != oc.ProjectStopPending {
		t.Fatal("completed replacement claim hid old live work", err)
	}
	once.Do(func() { close(release) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("original cleanup did not return")
	}
	planner.set(nil)
	stopUntilSettled(t, s, actor, cause)
}

func TestObjectProjectStopRecoveryDoesNotCancelOtherProject(t *testing.T) {
	f := newFixture(t, true)
	a := newObjectStopAuthority(t, f, f.store)
	planner := &recoveryItemPlanner{base: f.authority}
	s := newObjectStopService(t, f, a, stopServiceOptions{planner: planner})
	other := id[identity.Project](t)
	owner, _ := oc.NewObjectOwner(oc.Artifact, id[struct{}](t).String(), other.String())
	f.sql(t, `INSERT INTO object_fixture.projects(id,owner_id) VALUES($1,$2)`, other.String(), f.actor.Details().UserID)
	f.sql(t, `INSERT INTO object_fixture.owners(id,kind,project_id,user_id,existence,cause) VALUES($1,'artifact',$2,$3,'prospective',$4)`, owner.Details().ID, other.String(), f.actor.Details().UserID, id[struct{}](t).String())
	planner.set(func(d oc.AccessRequestDetails) error {
		if d.Operation == oc.FinishWriterAccess {
			return fault(foundation.ResourceBusy)
		}
		return nil
	})
	for i, own := range []oc.ObjectOwner{f.owner, owner} {
		_, err := s.PutObject(contextFor(t), f.actor, own, command(t, foundationKeyForStop(i)), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
		requireCode(t, err, foundation.ResourceBusy)
	}
	var first oc.ObjectID
	var raw string
	if err := f.store.QueryRow(contextFor(t), `SELECT object_id::text FROM agenteam_object.uploads WHERE project_id=$1`, f.project.String()).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	first, _ = foundation.ParseID[oc.StoredObject](raw)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	calls := 0
	planner.set(func(d oc.AccessRequestDetails) error {
		if d.Operation == oc.RecoverAttemptAccess && d.ObjectID == first {
			calls++
			if calls == 2 {
				close(entered)
				<-release
			}
		}
		return nil
	})
	done := make(chan error, 1)
	go func() { done <- s.Recover(contextFor(t)) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first project maintenance barrier not reached")
	}
	actor, cause := activateObjectStop(t, f, oc.ProjectStopArchive)
	report, err := s.RequestProjectStop(contextFor(t), actor, cause)
	if err != nil || report.Details().State != oc.ProjectStopPending {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cross-project recovery did not join")
	}
	planner.set(nil)
	var phase string
	if err = f.store.QueryRow(contextFor(t), `SELECT a.phase FROM agenteam_object.upload_attempts a JOIN agenteam_object.objects o ON o.id=a.object_id WHERE o.project_id=$1`, other.String()).Scan(&phase); err != nil || phase != "verified" {
		t.Fatal("one stop cancelled the parent recovery round", err, phase)
	}
	stopUntilSettled(t, s, actor, cause)
}

func testObjectStopExactGuardDeath(t *testing.T) {
	f := newFixture(t, false)
	old := id[oc.Process](t)
	path := filepath.Join(t.TempDir(), "shared-spool")
	raw, err := json.Marshal(guardChildInput{f.db.Name, f.bucket, path, old.String()})
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "guard.json")
	if err = os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(contextFor(t), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestObjectProcessGuardChild$")
	cmd.Env = append(os.Environ(), "AGENTEAM_OBJECT_GUARD_CHILD="+input)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("owned guard child did not exit")
		}
	})
	lines := make(chan string, 4)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	wait := func(want string) {
		t.Helper()
		select {
		case got := <-lines:
			if got != want {
				t.Fatal("actual guard barrier", got, want)
			}
		case <-ctx.Done():
			t.Fatal("guard barrier timed out")
		}
	}
	wait("CLAIM_BOUND")
	if _, err = stdin.Write([]byte{'d'}); err != nil {
		t.Fatal(err)
	}
	wait("SPOOL_DRAINED_CLAIM_HELD")
	runtime, _, guard := runtimeAt(t, f, path, id[oc.Process](t), nil)
	if err = runtime.Initialize(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	a := newObjectStopAuthority(t, f, f.store)
	s := newObjectStopService(t, f, a, stopServiceOptions{processes: guard})
	work := id[struct{}](t)
	f.sql(t, `INSERT INTO agenteam_object.project_work(id,project_id,process_id,kind,resource_id,admission_version) VALUES($1,$2,$3,'preparation',$1,0)`, work.String(), f.project.String(), old.String())
	actor, cause := activateObjectStop(t, f, oc.ProjectStopArchive)
	report, err := s.RequestProjectStop(contextFor(t), actor, cause)
	if err != nil || report.Details().State != oc.ProjectStopPending {
		t.Fatal("live exact flock inferred dead", err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("actual killed process did not exit")
	}
	stopUntilSettled(t, s, actor, cause)
	var joined bool
	if err = f.store.QueryRow(contextFor(t), `SELECT joined_at IS NOT NULL FROM agenteam_object.project_work WHERE id=$1`, work.String()).Scan(&joined); err != nil || !joined {
		t.Fatal("exact dead process did not converge original work", err)
	}
}
