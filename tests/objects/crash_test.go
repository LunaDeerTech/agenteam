//go:build integration

package objects_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/minio/minio-go/v7"
)

// The descriptor contains only task-owned coordinates and random IDs. Credentials
// are loaded exclusively through the inherited, verified fixture descriptors.
type crashInput struct {
	Database, Bucket, Endpoint, Spool, Stage string
	User, Session, Project, Owner, Process   string
}
type crashReady struct {
	ObjectID   string
	BackendPID int32
}

func TestObjectCrashChild(t *testing.T) {
	path := os.Getenv("AGENTEAM_OBJECT_CRASH_INPUT")
	if path == "" {
		t.Skip("owned subprocess only")
	}
	stat, err := os.Stat(path)
	if err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm() != 0600 {
		t.Fatal("invalid child input")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var input crashInput
	if json.Unmarshal(data, &input) != nil {
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
	ctx := contextFor(t)
	store, err := postgres.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = store.ForceClose(ctx)
	}()
	remote, err := objectfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	user, err := foundation.ParseID[identity.User](input.User)
	if err != nil {
		t.Fatal(err)
	}
	session, err := foundation.ParseID[identity.Session](input.Session)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := identity.NewHuman(user, session)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := oc.NewObjectOwner(oc.Artifact, input.Owner, input.Project)
	if err != nil {
		t.Fatal(err)
	}
	process, err := foundation.ParseID[oc.Process](input.Process)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{remote: remote, bucket: input.Bucket}
	service, _ := f.reopen(t, store, input.Endpoint, nil, process, input.Spool)
	body := strings.Repeat("crash-payload-", 20000)
	cmd := command(t, "crash-"+input.Stage)
	var object oc.ObjectID
	if input.Stage == "reserved" || input.Stage == "verified" || input.Stage == "writer" {
		prepared, err := service.PreparePayload(ctx, actor, owner, "text/plain", int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
		if err != nil {
			t.Fatal(err)
		}
		var attempt oc.UploadAttempt
		accessPlan1 := ownerPlan(t, service, actor, owner, oc.ReserveAccess, oc.AccessRequestDetails{Command: &cmd, Prepared: prepared})
		result := plannedTx(store, service, ctx, cause(t), accessPlan1, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
			var err error
			attempt, err = service.ReserveUploadInTx(ctx, tx, actor, owner, cmd, prepared, plan, locked)
			return err
		})
		if result.State() != foundation.Committed {
			t.Fatal(result.Fault())
		}
		object = attempt.Details().ObjectID
		if input.Stage == "verified" {
			if _, err = service.UploadPrepared(ctx, actor, owner, prepared, attempt); err != nil {
				t.Fatal(err)
			}
		}
	} else {
		result, err := service.PutObject(ctx, actor, owner, cmd, "text/plain", int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
		if err != nil {
			t.Fatal(err)
		}
		object = result.Meta.ID
		if input.Stage == "reader" {
			reader, err := service.OpenUploadSource(ctx, actor, owner, result.Receipt)
			if err != nil {
				t.Fatal(err)
			}
			one := make([]byte, 1)
			if _, err = reader.Read(one); err != nil {
				t.Fatal(err)
			}
			// Keep the real reader and its lease alive until SIGKILL.
			defer reader.Close()
		}
	}
	var pid int32
	if err = store.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	line, _ := json.Marshal(crashReady{object.String(), pid})
	fmt.Println(string(line))
	inputReader := bufio.NewReader(os.Stdin)
	if input.Stage == "marker" {
		if _, err = inputReader.ReadByte(); err != nil {
			t.Fatal(err)
		}
		if _, err = service.CancelUpload(ctx, actor, owner, cmd.IdempotencyKey); err == nil {
			t.Fatal("lost marker response did not leave a checkpoint")
		}
		fmt.Println("MARKER_PENDING")
	}
	_, _ = inputReader.ReadByte()
}

type killedProcessProof struct {
	id   oc.ProcessID
	done <-chan struct{}
	err  *error
}

func (p killedProcessProof) ConfirmStopped(_ context.Context, id oc.ProcessID) error {
	if id != p.id {
		return fault(foundation.Forbidden)
	}
	select {
	case <-p.done:
	default:
		return fault(foundation.ResourceBusy)
	}
	var exit *exec.ExitError
	if !errors.As(*p.err, &exit) {
		return fault(foundation.ResourceBusy)
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		return fault(foundation.ResourceBusy)
	}
	return nil
}

func TestObjectSIGKILLRestoresOnlyDurableStorageFacts(t *testing.T) {
	for _, stage := range []string{"reserved", "verified", "reader", "marker", "writer"} {
		t.Run(stage, func(t *testing.T) {
			f := newFixture(t, true)
			proxy := newStorageProxy(t, f)
			if stage == "marker" {
				proxy.mode.Store(proxyLosePutResponse)
			}
			process := id[oc.Process](t)
			spool := filepath.Join(t.TempDir(), "spool")
			input := crashInput{Database: f.db.Name, Bucket: f.bucket, Endpoint: proxy.server.URL, Spool: spool, Stage: stage, User: f.actor.Details().UserID, Session: f.actor.Details().SessionID, Project: f.project.String(), Owner: f.owner.Details().ID, Process: process.String()}
			data, _ := json.Marshal(input)
			inputPath := filepath.Join(t.TempDir(), "child.json")
			if err := os.WriteFile(inputPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestObjectCrashChild$")
			cmd.Env = append(os.Environ(), "AGENTEAM_OBJECT_CRASH_INPUT="+inputPath)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
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
					return
				default:
				}
				timer := time.NewTimer(time.Second)
				defer timer.Stop()
				select {
				case <-done:
				case <-timer.C:
					t.Error("owned child did not join")
				}
			})
			lines := make(chan string, 2)
			go func() {
				defer close(lines)
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					lines <- scanner.Text()
				}
			}()
			line := func() string {
				select {
				case s := <-lines:
					return s
				case <-ctx.Done():
					t.Fatal("child barrier deadline")
					return ""
				}
			}
			var ready crashReady
			if json.Unmarshal([]byte(line()), &ready) != nil {
				t.Fatal("child did not reach real storage barrier")
			}
			object, err := foundation.ParseID[oc.StoredObject](ready.ObjectID)
			if err != nil {
				t.Fatal(err)
			}
			proof := killedProcessProof{process, done, &waitErr}
			if proof.ConfirmStopped(ctx, process) == nil {
				t.Fatal("live process admitted as stopped")
			}
			if stage == "writer" {
				cancelling, _ := f.on(t, f.store, proxy.server.URL, nil)
				r, err := cancelling.CancelUpload(ctx, f.actor, f.owner, "crash-writer")
				if err != nil || r.Cleanup != oc.CleanupPending || proxy.markers.Load() != 1 {
					t.Fatal("live foreign writer lost terminal protection", err)
				}
				refs, err := f.service.InspectReferences(ctx, object)
				if err != nil || len(refs.ActiveLeases) != 1 || refs.ActiveLeases[0].Owner.Details().Kind != oc.WriterOwner {
					t.Fatal("marker was mistaken for writer terminal proof", err)
				}
			}
			if stage == "reader" {
				r, err := f.service.CancelUpload(ctx, f.actor, f.owner, foundation.IdempotencyKey("crash-"+stage))
				if err != nil || r.Cleanup != oc.CleanupPending {
					t.Fatal("live foreign reader was not protected", err)
				}
				if _, err = f.s3.StatObject(ctx, f.bucket, f.physical(t, object), minio.StatObjectOptions{}); err != nil {
					t.Fatal("live reader payload removed")
				}
			}
			if stage == "marker" {
				proxy.mode.Store(proxyLoseMarkerResponse)
				if _, err = stdin.Write([]byte{1}); err != nil {
					t.Fatal(err)
				}
				if line() != "MARKER_PENDING" {
					t.Fatal("missing marker checkpoint barrier")
				}
			}
			if err = cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("SIGKILL child did not join")
			}
			if err = proof.ConfirmStopped(ctx, process); err != nil {
				t.Fatal("actual wait did not prove exact death", err)
			}
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				var alive bool
				if err = f.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=$1 AND pid=$2)`, f.db.Name, ready.BackendPID).Scan(&alive); err != nil {
					t.Fatal(err)
				}
				if !alive {
					break
				}
				select {
				case <-ticker.C:
				case <-ctx.Done():
					t.Fatal("owned child backend survived")
				}
			}
			proxy.mode.Store(proxyPass)
			restarted, _ := f.reopen(t, f.store, proxy.server.URL, proof, id[oc.Process](t), spool)
			if err = restarted.Recover(ctx); err != nil {
				t.Fatal("restart recovery", err)
			}
			if stage == "reserved" || stage == "verified" {
				var state string
				if err = f.store.QueryRow(ctx, `SELECT state FROM agenteam_object.objects WHERE id=$1`, object.String()).Scan(&state); err != nil || state != "pending" {
					t.Fatal("background recovery published business object", err)
				}
				body := strings.Repeat("crash-payload-", 20000)
				r, err := restarted.PutObject(ctx, f.actor, f.owner, command(t, "crash-"+stage), "text/plain", int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
				if err != nil || r.Meta.ID != object || proxy.puts.Load() != 1 {
					t.Fatal("owner recovery changed identity or resent payload", err)
				}
				var attempts int64
				if err = f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.upload_attempts WHERE object_id=$1`, object.String()).Scan(&attempts); err != nil {
					t.Fatal(err)
				}
				want := int64(1)
				if stage == "reserved" {
					want = 2
				}
				if attempts != want {
					t.Fatal("incorrect recovered attempt count", attempts)
				}
			} else {
				r, err := restarted.LookupPut(ctx, f.actor, f.owner, foundation.IdempotencyKey("crash-"+stage))
				if err != nil || r.State != oc.UploadRevoked || r.Cleanup != oc.CleanupCompleted || r.Receipt.Validate() == nil {
					t.Fatal("cleanup checkpoint did not converge", err)
				}
				if stage == "marker" {
					var key string
					if err = f.store.QueryRow(ctx, `SELECT candidate_key FROM agenteam_object.upload_attempts WHERE object_id=$1`, object.String()).Scan(&key); err != nil {
						t.Fatal(err)
					}
					reader, err := f.s3.GetObject(ctx, f.bucket, key, minio.GetObjectOptions{})
					if err != nil {
						t.Fatal(err)
					}
					b, err := io.ReadAll(reader)
					_ = reader.Close()
					if err != nil || len(b) != 0 || digest(b) != digest(nil) || proxy.deletes.Load() != 0 {
						t.Fatal("durable marker disappeared or restored payload", err)
					}
				}
			}
			entries, err := os.ReadDir(spool)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != ".object.lock" {
				t.Fatal("exact killed process spool not reclaimed")
			}
		})
	}
}
