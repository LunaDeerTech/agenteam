//go:build integration

package objects_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/minio/minio-go/v7"
)

// Only the trusted planner is interrupted. Payloads, checkpoints, transactions,
// cleanup and reader/writer I/O use the real library and isolated services.
type recoveryItemPlanner struct {
	base oc.AccessPlanner
	mu   sync.Mutex
	hook func(oc.AccessRequestDetails) error
}

func (p *recoveryItemPlanner) set(hook func(oc.AccessRequestDetails) error) {
	p.mu.Lock()
	p.hook = hook
	p.mu.Unlock()
}
func (p *recoveryItemPlanner) Discover(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	p.mu.Lock()
	hook := p.hook
	p.mu.Unlock()
	if hook != nil {
		if err := hook(request.Details()); err != nil {
			return oc.AccessDependencies{}, err
		}
	}
	return p.base.Discover(ctx, request)
}
func (p *recoveryItemPlanner) ValidateInTx(ctx context.Context, tx foundation.Tx, request oc.AccessRequest, dependencies oc.AccessDependencies) error {
	return p.base.ValidateInTx(ctx, tx, request, dependencies)
}

func TestObjectRecoveryItemFailureKeepsIndependentCleanupMoving(t *testing.T) {
	for _, stage := range []string{"cleanup", "closed_reader", "cancelled_round"} {
		t.Run(stage, func(t *testing.T) {
			f := newFixture(t, true)
			planner := &recoveryItemPlanner{base: f.authority}
			service := plannedService(t, f, f.store, planner)
			var objects []oc.ObjectID
			var readers []*oc.ObjectReader
			var keys []string
			for _, name := range []string{"busy-first", "independent-second"} {
				body := strings.Repeat("x", 128<<10)
				stored, err := service.PutObject(contextFor(t), f.actor, f.owner, command(t, name), "text/plain", int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
				if err != nil {
					t.Fatal(err)
				}
				objects = append(objects, stored.Meta.ID)
				var key string
				if err = f.store.QueryRow(contextFor(t), `SELECT candidate_key FROM agenteam_object.upload_attempts WHERE object_id=$1`, stored.Meta.ID.String()).Scan(&key); err != nil {
					t.Fatal(err)
				}
				keys = append(keys, key)
				reader, err := service.OpenUploadSource(contextFor(t), f.actor, f.owner, stored.Receipt)
				if err != nil {
					t.Fatal(err)
				}
				readers = append(readers, reader)
				defer reader.Close()
				cancelled, err := service.CancelUpload(contextFor(t), f.actor, f.owner, foundation.IdempotencyKey(name))
				if err != nil || cancelled.Cleanup != oc.CleanupPending {
					t.Fatal("actual pending cleanup not established", err)
				}
			}
			if stage == "closed_reader" {
				planner.set(func(d oc.AccessRequestDetails) error {
					if d.Operation == oc.ReleaseReaderAccess {
						return fault(foundation.ResourceBusy)
					}
					return nil
				})
			}
			for _, reader := range readers {
				err := reader.Close()
				if stage == "closed_reader" {
					requireCode(t, err, foundation.ResourceBusy)
				} else if err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(contextFor(t))
			defer cancel()
			var blockedCalls, nextCalls int
			planner.set(func(d oc.AccessRequestDetails) error {
				if d.Kind != oc.MaintenanceAccess {
					return nil
				}
				if d.ObjectID == objects[1] {
					nextCalls++
				}
				if d.ObjectID == objects[0] {
					blockedCalls++
					if stage == "cancelled_round" {
						cancel()
					}
					return fault(foundation.ResourceBusy)
				}
				return nil
			})
			for range 2 {
				requireCode(t, service.Recover(ctx), foundation.ResourceBusy)
				if stage == "cancelled_round" {
					break
				}
			}
			var blocked, independent string
			if err := f.store.QueryRow(contextFor(t), `SELECT (SELECT state FROM agenteam_object.objects WHERE id=$1),(SELECT state FROM agenteam_object.objects WHERE id=$2)`, objects[0].String(), objects[1].String()).Scan(&blocked, &independent); err != nil {
				t.Fatal(err)
			}
			if blockedCalls == 0 || blocked != string(oc.Available) {
				t.Fatal("busy object/checkpoint not retained", blockedCalls, blocked)
			}
			if stage == "cancelled_round" {
				if nextCalls != 0 || independent != string(oc.Available) {
					t.Fatal("cancelled round admitted another item", nextCalls, independent)
				}
			} else {
				if blockedCalls < 2 || nextCalls == 0 || independent != string(oc.Deleted) {
					t.Fatal("independent cleanup was starved", blockedCalls, nextCalls, independent)
				}
				_, err := f.s3.StatObject(contextFor(t), f.bucket, keys[1], minio.StatObjectOptions{})
				if minio.ToErrorResponse(err).Code != "NoSuchKey" {
					t.Fatal("independent payload still exists", err)
				}
			}
			if _, err := f.s3.StatObject(contextFor(t), f.bucket, keys[0], minio.StatObjectOptions{}); err != nil {
				t.Fatal("busy payload was removed", err)
			}
			planner.set(nil)
			if err := service.Recover(contextFor(t)); err != nil {
				t.Fatal("retained checkpoint did not resume", err)
			}
		})
	}
}

func TestObjectRecoveryWriterCheckpointsAndVerificationAreIndependent(t *testing.T) {
	for _, stage := range []oc.AccessOperation{oc.JoinAttemptAccess, oc.RecoverAttemptAccess} {
		t.Run(string(stage), func(t *testing.T) {
			f := newFixture(t, true)
			planner := &recoveryItemPlanner{base: f.authority}
			service := plannedService(t, f, f.store, planner)
			planner.set(func(d oc.AccessRequestDetails) error {
				if d.Operation == oc.FinishWriterAccess {
					return fault(foundation.ResourceBusy)
				}
				return nil
			})
			var attempts []oc.UploadAttempt
			for _, name := range []string{"writer-first", "writer-second"} {
				prepared, err := service.PreparePayload(contextFor(t), f.actor, f.owner, "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
				if err != nil {
					t.Fatal(err)
				}
				cmd := command(t, name)
				plan := ownerPlan(t, service, f.actor, f.owner, oc.ReserveAccess, oc.AccessRequestDetails{Command: &cmd, Prepared: prepared})
				var attempt oc.UploadAttempt
				result := plannedTx(f.store, service, contextFor(t), cause(t), plan, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
					var err error
					attempt, err = service.ReserveUploadInTx(ctx, tx, f.actor, f.owner, cmd, prepared, plan, locked)
					return err
				})
				if result.State() != foundation.Committed {
					t.Fatal(result.Fault())
				}
				_, err = service.UploadPrepared(contextFor(t), f.actor, f.owner, prepared, attempt)
				requireCode(t, err, foundation.ResourceBusy)
				if err = service.DiscardPrepared(prepared); err != nil {
					t.Fatal(err)
				}
				attempts = append(attempts, attempt)
			}
			var hits int
			planner.set(func(d oc.AccessRequestDetails) error {
				if d.Operation == stage && d.ObjectID == attempts[0].Details().ObjectID {
					hits++
					return fault(foundation.ResourceBusy)
				}
				return nil
			})
			for range 2 {
				requireCode(t, service.Recover(contextFor(t)), foundation.ResourceBusy)
			}
			var first, second string
			var firstClosed bool
			if err := f.store.QueryRow(contextFor(t), `SELECT (SELECT phase FROM agenteam_object.upload_attempts WHERE id=$1),(SELECT phase FROM agenteam_object.upload_attempts WHERE id=$2),(SELECT io_closed FROM agenteam_object.upload_attempts WHERE id=$1)`, attempts[0].Details().ID.String(), attempts[1].Details().ID.String()).Scan(&first, &second, &firstClosed); err != nil {
				t.Fatal(err)
			}
			expected := "sending"
			if stage == oc.RecoverAttemptAccess {
				expected = "unknown"
			}
			if hits != 2 || first != expected || firstClosed != (stage == oc.RecoverAttemptAccess) || second != "verified" {
				t.Fatal("writer checkpoint or independent verification lost", hits, first, firstClosed, second)
			}
			planner.set(nil)
			if err := service.Recover(contextFor(t)); err != nil {
				t.Fatal("failed writer checkpoint was forgotten", err)
			}
			if err := f.store.QueryRow(contextFor(t), `SELECT phase FROM agenteam_object.upload_attempts WHERE id=$1`, attempts[0].Details().ID.String()).Scan(&first); err != nil || first != "verified" {
				t.Fatal("writer retry did not converge", err, first)
			}
		})
	}
}

type recoveryLeaseInput struct {
	crashInput
	Objects []string
}

func TestObjectRecoveryLeaseChild(t *testing.T) {
	path := os.Getenv("AGENTEAM_OBJECT_RECOVERY_LEASE_INPUT")
	if path == "" {
		t.Skip("owned subprocess only")
	}
	stat, err := os.Stat(path)
	if err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm() != 0600 {
		t.Fatal("invalid child input")
	}
	raw, err := os.ReadFile(path)
	var input recoveryLeaseInput
	if err != nil || json.Unmarshal(raw, &input) != nil || len(input.Objects) != 2 {
		t.Fatal("invalid child descriptor")
	}
	pg, err := pgfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pg.Config(input.Database, nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(contextFor(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := objectfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	user, e1 := foundation.ParseID[identity.User](input.User)
	session, e2 := foundation.ParseID[identity.Session](input.Session)
	actor, e3 := identity.NewHuman(user, session)
	owner, e4 := oc.NewObjectOwner(oc.Artifact, input.Owner, input.Project)
	process, e5 := foundation.ParseID[oc.Process](input.Process)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil {
		t.Fatal("invalid child identity")
	}
	f := &fixture{remote: remote, bucket: input.Bucket}
	service, _ := f.reopen(t, store, input.Endpoint, nil, process, input.Spool)
	for _, rawID := range input.Objects {
		id, err := foundation.ParseID[oc.StoredObject](rawID)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := service.ReadObject(contextFor(t), actor, owner, id, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		if _, err = io.ReadFull(reader, make([]byte, 1)); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println("RECOVERY_READERS_READY")
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
}

func recoveryServiceWithDeathProof(t *testing.T, f *fixture, planner oc.AccessPlanner, proof oc.ProcessAuthority) *object.Service {
	t.Helper()
	backend, err := object.NewBackend(f.config)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := object.OpenSpool(filepath.Join(t.TempDir(), "spool"), id[oc.Process](t))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	auditing, err := audit.New(f.store, keys, audit.Authorizations{Projects: auditAuthority{f.authority}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := object.New(f.store, backend, spool, auditing, object.Authorizations{Planner: planner, Resources: f.authority, Read: f.authority, Gate: f.authority, Cleanup: f.authority, Leases: f.authority, Processes: proof})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		service.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil {
			_ = service.Force(ctx)
			t.Error(err)
		}
	})
	if err = service.Initialize(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	return service
}

func TestObjectRecoveryStoppedProcessContinuesPastOneBusyObject(t *testing.T) {
	f := newFixture(t, false)
	first := f.put(t, "stopped-first", strings.Repeat("a", 128<<10))
	second := f.put(t, "stopped-second", strings.Repeat("b", 128<<10))
	process := id[oc.Process](t)
	input := recoveryLeaseInput{crashInput: crashInput{Database: f.db.Name, Bucket: f.bucket, Endpoint: f.remote.Endpoint(), Spool: filepath.Join(t.TempDir(), "child-spool"), User: f.actor.Details().UserID, Session: f.actor.Details().SessionID, Project: f.project.String(), Owner: f.owner.Details().ID, Process: process.String()}, Objects: []string{first.Meta.ID.String(), second.Meta.ID.String()}}
	raw, _ := json.Marshal(input)
	path := filepath.Join(t.TempDir(), "child.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestObjectRecoveryLeaseChild$")
	cmd.Env = append(os.Environ(), "AGENTEAM_OBJECT_RECOVERY_LEASE_INPUT="+path)
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
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("owned child did not stop")
		}
	})
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "RECOVERY_READERS_READY\n" {
			t.Fatal("child did not establish actual readers")
		}
	case <-ctx.Done():
		t.Fatal("child readiness timed out")
	}
	var active int64
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE process_id=$1 AND owner_kind='reader' AND state='active'`, process.String()).Scan(&active); err != nil || active != 2 {
		t.Fatal("two actual reader leases not established", err, active)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("owned SIGKILL did not join")
	}
	proof := killedProcessProof{id: process, done: done, err: &waitErr}
	if err = proof.ConfirmStopped(contextFor(t), process); err != nil {
		t.Fatal("actual death not confirmed", err)
	}
	planner := &recoveryItemPlanner{base: f.authority}
	var hits int
	planner.set(func(d oc.AccessRequestDetails) error {
		if d.Operation == oc.ReleaseProcessAccess && d.ObjectID == first.Meta.ID {
			hits++
			return fault(foundation.ResourceBusy)
		}
		return nil
	})
	service := recoveryServiceWithDeathProof(t, f, planner, proof)
	for range 2 {
		requireCode(t, service.Recover(contextFor(t)), foundation.ResourceBusy)
	}
	var firstState, secondState string
	if err = f.store.QueryRow(contextFor(t), `SELECT (SELECT state FROM agenteam_object.object_leases WHERE process_id=$1 AND object_id=$2 AND owner_kind='reader'),(SELECT state FROM agenteam_object.object_leases WHERE process_id=$1 AND object_id=$3 AND owner_kind='reader')`, process.String(), first.Meta.ID.String(), second.Meta.ID.String()).Scan(&firstState, &secondState); err != nil {
		t.Fatal(err)
	}
	if hits != 2 || firstState != "active" || secondState != "released" {
		t.Fatal("one object blocked independent release for a dead process", hits, firstState, secondState)
	}
	planner.set(nil)
	if err = service.Recover(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE process_id=$1 AND state='active'`, process.String()).Scan(&active); err != nil || active != 0 {
		t.Fatal("retained exact-process checkpoint did not resume", err, active)
	}
}
