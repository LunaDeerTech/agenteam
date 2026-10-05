//go:build integration

package account_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/minio/minio-go/v7"
)

type avatarCommitStore struct {
	*postgres.Store
	proxy          *commitProxy
	stage          string
	enabled, fired atomic.Bool
}

func (w *avatarCommitStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	return w.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := fn(ctx, tx); e != nil {
			return e
		}
		if !w.enabled.Load() || w.fired.Load() {
			return nil
		}
		x, e := w.InTx(tx)
		if e != nil {
			return e
		}
		predicate := map[string]string{"plan": "phase='preparing' AND new_object_id IS NULL", "reserve": "phase='preparing' AND new_object_id IS NOT NULL", "publish": "phase='published'", "apply": "phase='applied'"}[w.stage]
		var found bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.avatar_changes WHERE `+predicate+`)`).Scan(&found); e != nil {
			return e
		}
		if found && w.fired.CompareAndSwap(false, true) {
			w.proxy.armed.Store(true)
		}
		return nil
	})
}
func newAvatarUnknown(t *testing.T, stage string, commit bool) (*avatarFixture, *avatarCommitStore, *commitProxy) {
	t.Helper()
	db, _, _ := database(t)
	p := newCommitProxy(t, net.JoinHostPort("127.0.0.1", db.Fixture.Port), commit)
	u, e := url.Parse(db.Fixture.URL(db.Name))
	if e != nil {
		t.Fatal(e)
	}
	u.Host = p.listener.Addr().String()
	raw := openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	w := &avatarCommitStore{Store: raw, proxy: p, stage: stage}
	keys, _, _ := keys(t)
	authority, e := account.NewAuthority(w, keys)
	if e != nil {
		t.Fatal(e)
	}
	f := assembleAvatarFixture(t, assembleB02(t, db, raw, w, authority, nil))
	return f, w, p
}
func avatarPayloadCount(t *testing.T, f *avatarFixture) int {
	t.Helper()
	n := 0
	identityMarkers := 0
	for item := range f.s3.ListObjects(ctxFor(t), f.bucket, minio.ListObjectsOptions{Recursive: true}) {
		if item.Err != nil {
			t.Fatal("owned bucket listing", item.Err)
		}
		// Object.Initialize owns exactly this technical identity marker. All
		// other stored keys count, including unexpected/late payloads.
		if item.Key == "control/store-identity" {
			identityMarkers++
		} else {
			// A potentially late writer is fenced by a permanent empty marker.
			// Only an exact completed cleanup fact, deleted object and a full
			// empty GET justify excluding it from the payload count.
			var marker bool
			e := f.store.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_object.cleanup_operations c JOIN agenteam_object.upload_attempts a ON a.id=c.attempt_id JOIN agenteam_object.objects o ON o.id=a.object_id WHERE a.candidate_key=$1 AND c.mode='zero_marker' AND c.phase='completed' AND a.phase='cleaned' AND o.state='deleted' AND o.cleaning AND a.cleanup_gate)`, item.Key).Scan(&marker)
			if e != nil {
				t.Fatal("exact marker fact", e)
			}
			if marker {
				body, e := f.s3.GetObject(ctxFor(t), f.bucket, item.Key, minio.GetObjectOptions{})
				if e != nil {
					t.Fatal("marker GET", e)
				}
				raw, readErr := io.ReadAll(io.LimitReader(body, 1))
				closeErr := body.Close()
				if readErr != nil || closeErr != nil || len(raw) != 0 || item.Size != 0 {
					t.Fatal("retained marker contains payload", readErr, closeErr, len(raw), item.Size)
				}
			} else {
				n++
			}
		}
	}
	if identityMarkers != 1 {
		t.Fatal("owned storage identity marker missing", identityMarkers)
	}
	return n
}
func TestAccountAvatarUnknownKeepsMappingAndNoRepeatedPut(t *testing.T) {
	for _, stage := range []string{"plan", "reserve", "publish", "apply"} {
		for _, commit := range []bool{true, false} {
			name := "late_commit"
			if !commit {
				name = "rollback"
			}
			t.Run(stage+"/"+name, func(t *testing.T) {
				f, w, p := newAvatarUnknown(t, stage, commit)
				if n := avatarPayloadCount(t, f); n != 0 {
					t.Fatal("fixture bucket not empty", n)
				}
				raw := avatarPNG(t, 100)
				invoke := func(ctx context.Context) (c.ProfileView, error) {
					return f.profiles.PutAvatar(ctx, c.AvatarUpload{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "held-avatar-command", ExpectedVersion: 1}, MediaType: "image/png", ByteSize: int64(len(raw)), Body: io.NopCloser(bytes.NewReader(raw))})
				}
				w.enabled.Store(true)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() { _, e := invoke(ctx); done <- e }()
				await(t, p.reached)
				select {
				case e := <-done:
					var fault *foundation.Fault
					if !errors.As(e, &fault) || fault.Code != foundation.CommitUnknown || fault.CommitState != foundation.Unknown {
						t.Fatal("original COMMIT state", e, safeFailure(e))
					}
				case <-time.After(4 * time.Second):
					t.Fatal("avatar unknown did not return")
				}
				if !w.fired.Load() {
					t.Fatal("real COMMIT did not reach proxy")
				}
				if (stage == "plan" || stage == "reserve") && avatarPayloadCount(t, f) != 0 {
					t.Fatal("PUT preceded committed reservation mapping")
				}
				before := avatarPayloadCount(t, f)
				check, stop := context.WithTimeout(context.Background(), 150*time.Millisecond)
				_, e := f.profiles.RecoverAvatars(check)
				stop()
				if e == nil || !errors.Is(e, context.DeadlineExceeded) {
					t.Fatal("recovery did not wait original command writer", e, safeFailure(e))
				}
				if n := avatarPayloadCount(t, f); n != before {
					t.Fatal("unresolved writer caused I/O", n, before)
				}
				close(p.release)
				await(t, p.completed)
				for range 2 {
					if _, e = f.profiles.RecoverAvatars(ctxFor(t)); e != nil {
						t.Fatal("joined recovery", e, safeFailure(e))
					}
				}
				var pending, mappings, current, committed int
				e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.avatar_changes WHERE phase IN ('preparing','published','cancelled','cleanup_pending')),(SELECT count(*) FROM agenteam_account.avatar_changes WHERE (new_object_id IS NULL)<>(upload_id IS NULL) OR (upload_id IS NULL)<>(attempt_id IS NULL)),(SELECT count(*) FROM agenteam_account.users WHERE avatar_object_id IS NOT NULL),(SELECT count(*) FROM agenteam_account.commands WHERE command_name='avatar-update' AND phase='committed')`).Scan(&pending, &mappings, &current, &committed)
				want := 0
				if stage == "apply" && commit {
					want = 1
				}
				if e != nil || pending != 0 || mappings != 0 || current != want || committed != want {
					t.Fatal("partial recovery facts", pending, mappings, current, committed, e)
				}
				if want == 1 {
					before = avatarPayloadCount(t, f)
					view, e := invoke(ctxFor(t))
					if e != nil || view.Avatar == nil || avatarPayloadCount(t, f) != before {
						t.Fatal("historical commit repeated upload", e)
					}
				} else if stage != "plan" || commit {
					if _, e = invoke(ctxFor(t)); !hasCode(e, foundation.InvalidState) {
						t.Fatal("cancelled original command revived", e, safeFailure(e))
					}
					if n := avatarPayloadCount(t, f); n != 0 {
						t.Fatal("failed candidate payload remains", n)
					}
				}
			})
		}
	}
}

// avatarBoundaryStore observes a real committed domain row. It either pauses
// that exact caller after COMMIT or cancels the caller then returns the original
// CommitResult unchanged; it never fabricates a transaction outcome.
type avatarBoundaryStore struct {
	*postgres.Store
	key, stage string
	fired      bool
	cancel     context.CancelFunc
}

func (w *avatarBoundaryStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	r := w.Store.WithinTx(ctx, cause, fn)
	if r.State() != foundation.Committed || w.fired || w.key == "" {
		return r
	}
	predicate := map[string]string{"plan": "a.phase='preparing' AND a.new_object_id IS NULL", "reserve": "a.phase='preparing' AND a.new_object_id IS NOT NULL", "publish": "a.phase='published'"}[w.stage]
	var found bool
	e := w.Store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.avatar_changes a JOIN agenteam_account.commands c ON c.id=a.command_id WHERE c.command_key=$1 AND `+predicate+`)`, w.key).Scan(&found)
	if e == nil && found {
		w.fired = true
		if w.cancel != nil {
			w.cancel()
		} else {
			avatarChildBoundary()
		}
	}
	return r
}
func avatarChildBoundary() {
	fmt.Println("AVATAR_BOUNDARY")
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
}

type avatarCrashInput struct {
	Database, Bucket, Spool, Process, User, Session, Stage string
	Version                                                foundation.Version
	Count                                                  int
}

func TestAccountAvatarCrashHelper(t *testing.T) {
	path := os.Getenv("AGENTEAM_AVATAR_CRASH_INPUT")
	if path == "" {
		t.Skip("owned Avatar subprocess helper")
	}
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatal("invalid owned avatar descriptor")
	}
	b, e := os.ReadFile(path)
	var input avatarCrashInput
	if e != nil || json.Unmarshal(b, &input) != nil || input.Count < 0 || input.Count > 100 {
		t.Fatal("invalid avatar child input")
	}
	pg, e := pgfixture.Load()
	if e != nil {
		t.Fatal(e)
	}
	cfg, e := pg.Config(input.Database, nil)
	if e != nil {
		t.Fatal(e)
	}
	raw := openStore(t, cfg)
	w := &avatarBoundaryStore{Store: raw, stage: input.Stage}
	k, _, _ := keys(t)
	authority, e := account.NewAuthority(w, k)
	if e != nil {
		t.Fatal(e)
	}
	pid, e := foundation.ParseID[c.Process](input.Process)
	if e != nil {
		t.Fatal(e)
	}
	uid, e := foundation.ParseID[identity.User](input.User)
	if e != nil {
		t.Fatal(e)
	}
	sid, e := foundation.ParseID[identity.Session](input.Session)
	if e != nil {
		t.Fatal(e)
	}
	actor, e := identity.NewHuman(uid, sid)
	if e != nil {
		t.Fatal(e)
	}
	f := reopenAvatarFixture(t, nil, raw, w, authority, input.Bucket, input.Spool, pid, actor)
	body := avatarPNG(t, 121)
	invoke := func(ctx context.Context, key string) error {
		_, e := f.profiles.PutAvatar(ctx, c.AvatarUpload{ProfileMutation: c.ProfileMutation{Actor: actor, Key: foundation.IdempotencyKey(key), ExpectedVersion: input.Version}, MediaType: "image/png", ByteSize: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body))})
		return e
	}
	if input.Count > 0 {
		for i := range input.Count {
			ctx, cancel := context.WithCancel(ctxFor(t))
			w.key = fmt.Sprintf("protected-avatar-%03d", i)
			w.cancel = cancel
			w.fired = false
			e := invoke(ctx, w.key)
			cancel()
			if !w.fired || !errors.Is(e, context.Canceled) {
				t.Fatal("real planned cancellation not reached", i, e, safeFailure(e))
			}
		}
		avatarChildBoundary()
		return
	}
	w.key = "crash-avatar-" + input.Stage
	if e = invoke(ctxFor(t), w.key); e != nil {
		t.Fatal("avatar child returned before held boundary", e, safeFailure(e))
	}
}

type avatarChild struct {
	process c.ProcessID
	cmd     *exec.Cmd
	done    <-chan struct{}
}

func startAvatarChild(t *testing.T, f *avatarFixture, stage string, count int, version foundation.Version) *avatarChild {
	t.Helper()
	pid := id[c.Process](t)
	input := avatarCrashInput{Database: f.db.Name, Bucket: f.bucket, Spool: filepath.Join(t.TempDir(), "spool"), Process: pid.String(), User: f.actor.Details().UserID, Session: f.actor.Details().SessionID, Stage: stage, Version: version, Count: count}
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "avatar-child.json")
	b, e := json.Marshal(input)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAccountAvatarCrashHelper$")
	cmd.Env = append(os.Environ(), "AGENTEAM_AVATAR_CRASH_INPUT="+path, "TMPDIR="+dir)
	stdin, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = stdin.Close() })
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("owned avatar child did not join")
		}
	})
	line := make(chan string, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		if scan.Scan() {
			line <- scan.Text()
		} else {
			line <- ""
		}
	}()
	select {
	case got := <-line:
		if got != "AVATAR_BOUNDARY" {
			t.Fatalf("owned child missed boundary: %q", got)
		}
	case <-ctx.Done():
		t.Fatal("owned avatar boundary deadline")
	}
	if f.deaths == nil {
		t.Fatal("missing exact child registry")
	}
	f.deaths.mu.Lock()
	f.deaths.entries[pid.String()] = &avatarDeathEntry{done: done, spool: input.Spool}
	f.deaths.mu.Unlock()
	return &avatarChild{pid, cmd, done}
}
func (p *avatarChild) kill(t *testing.T, f *avatarFixture) {
	t.Helper()
	if e := p.cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-p.done:
	case <-ctxFor(t).Done():
		t.Fatal("actual avatar child kill not joined")
	}
	f.deaths.mu.Lock()
	entry := f.deaths.entries[p.process.String()]
	f.deaths.mu.Unlock()
	if entry == nil {
		t.Fatal("unknown child cannot prove death")
	}
	proof := reopenAvatarFixture(t, f.db, f.store, f.store, f.authority, f.bucket, entry.spool, id[c.Process](t), f.actor)
	old, e := foundation.ParseID[oc.Process](p.process.String())
	if e != nil {
		t.Fatal(e)
	}
	if e = proof.guard.ConfirmStopped(ctxFor(t), old); e != nil {
		t.Fatal("real host/deployment/spool/claim death proof", e, safeFailure(e))
	}
	f.deaths.mu.Lock()
	entry.proof = proof.guard
	f.deaths.mu.Unlock()
}

func TestAccountAvatarRecoveryExactChildDeathBeforeAndAfterPublish(t *testing.T) {
	for _, stage := range []string{"plan", "reserve", "publish"} {
		t.Run(stage, func(t *testing.T) {
			original := newAvatarFixture(t)
			first := original.putAvatar(t, "kept-current", 1, 80)
			current := original.avatarID(t)
			f := reopenAvatarFixture(t, original.db, original.store, original.store, original.authority, original.bucket, filepath.Join(t.TempDir(), "spool"), id[c.Process](t), original.actor, &avatarDeaths{entries: map[string]*avatarDeathEntry{}})
			child := startAvatarChild(t, f, stage, 0, first.User.Version)
			before := avatarPayloadCount(t, f)
			status, e := f.profiles.RecoverAvatars(ctxFor(t))
			if e != nil || status.Pending == 0 {
				t.Fatal("live exact child not protected", status, e, safeFailure(e))
			}
			if f.avatarID(t) != current || avatarPayloadCount(t, f) != before {
				t.Fatal("live child/current avatar changed")
			}
			child.kill(t, f)
			for range 2 {
				if _, e = f.profiles.RecoverAvatars(ctxFor(t)); e != nil {
					t.Fatal("exact child recovery", e, safeFailure(e))
				}
			}
			var phase, command string
			var live int
			e = f.store.QueryRow(ctxFor(t), `SELECT a.phase,c.phase,(SELECT count(*) FROM agenteam_object.objects WHERE state<>'deleted') FROM agenteam_account.avatar_changes a JOIN agenteam_account.commands c ON c.id=a.command_id WHERE c.command_key=$1`, "crash-avatar-"+stage).Scan(&phase, &command, &live)
			if e != nil || phase != "completed" || command != "cancelled" || live != 1 || f.avatarID(t) != current || avatarPayloadCount(t, f) != 1 {
				t.Fatal("dead upload failed to converge without changing current", phase, command, live, e)
			}
			reader, e := f.profiles.ReadAvatar(ctxFor(t), f.actor, c.AvatarRange{Kind: c.AvatarRangeAll})
			if e != nil {
				t.Fatal(e)
			}
			b, e := io.ReadAll(reader)
			closeErr := reader.Close()
			if e != nil || closeErr != nil || len(b) == 0 {
				t.Fatal("current image lost after unrelated death", e, closeErr)
			}
		})
	}
}

func TestAccountAvatarRecoveryPersistentHundredProtectedDoNotStarveTail(t *testing.T) {
	original := newAvatarFixture(t)
	f := reopenAvatarFixture(t, original.db, original.store, original.store, original.authority, original.bucket, filepath.Join(t.TempDir(), "spool"), id[c.Process](t), original.actor, &avatarDeaths{entries: map[string]*avatarDeathEntry{}})
	prefix := startAvatarChild(t, f, "plan", 100, 1)
	tail := startAvatarChild(t, f, "plan", 0, 1)
	tail.kill(t, f)
	var count int
	if e := f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.avatar_changes WHERE phase='preparing' AND new_object_id IS NULL`).Scan(&count); e != nil || count != 101 {
		t.Fatal("actual formal planned rows", count, e)
	}
	status, e := f.profiles.RecoverAvatars(ctxFor(t))
	if e != nil || status.Examined != 100 || status.Pending != 100 {
		t.Fatal("protected prefix first pass", status, e, safeFailure(e))
	}
	var phase string
	if e = f.store.QueryRow(ctxFor(t), `SELECT a.phase FROM agenteam_account.avatar_changes a JOIN agenteam_account.commands c ON c.id=a.command_id WHERE c.command_key='crash-avatar-plan'`).Scan(&phase); e != nil || phase != "preparing" {
		t.Fatal("tail was not behind the full protected batch", phase, e)
	}
	// A new maintenance object has no cursor or local operation map from pass
	// one; only durable pass ordering and exact foreign death can advance tail.
	fresh, e := account.NewProfileService(f.service, f.objects)
	if e != nil {
		t.Fatal(e)
	}
	status, e = fresh.RecoverAvatars(ctxFor(t))
	if e != nil || status.Advanced < 1 || status.Pending < 1 {
		t.Fatal("persistent next pass starved tail", status, e, safeFailure(e))
	}
	if e = f.store.QueryRow(ctxFor(t), `SELECT a.phase FROM agenteam_account.avatar_changes a JOIN agenteam_account.commands c ON c.id=a.command_id WHERE c.command_key='crash-avatar-plan'`).Scan(&phase); e != nil || phase != "completed" {
		t.Fatal("tail did not complete", phase, e)
	}
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.avatar_changes WHERE phase='preparing'`).Scan(&count); e != nil || count != 100 {
		t.Fatal("live prefix was retired", count, e)
	}
	prefix.kill(t, f)
	for range 2 {
		if _, e = fresh.RecoverAvatars(ctxFor(t)); e != nil {
			t.Fatal("joined prefix cleanup", e, safeFailure(e))
		}
	}
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.avatar_changes WHERE phase<>'completed'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("exact-dead prefix remains", count, e)
	}
}
