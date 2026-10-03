//go:build integration

package security_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// This is the real library in an isolated test process. Parent-controlled
// stdin/stdout barriers bracket a real database transaction, never a fake
// Commit result or a production testing switch.
func TestSecretCrashChild(t *testing.T) {
	mode := os.Getenv("AGENTEAM_SECRET_CRASH_CHILD")
	if mode == "" {
		t.Skip("owned subprocess only")
	}
	fixture, err := pgfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := fixture.Config(os.Getenv("AGENTEAM_SECRET_CRASH_DATABASE"), nil)
	if err != nil {
		t.Fatal(err)
	}
	store := openAuditStore(t, cfg)
	auditing, err := audit.New(store, auditKeys(t), audit.Authorizations{})
	if err != nil {
		t.Fatal(err)
	}
	service, err := secret.New(store, masterKeys(t, 2, 1, 2), auditing, secret.Authorizations{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := auditContext(t)
	if err = service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	batch, err := service.PrepareRewrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "nonce" {
		fmt.Println("PREPARED 0")
		_, _ = bufio.NewReader(os.Stdin).ReadByte()
		return
	}
	var pid int32
	result := store.WithinTx(ctx, txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		if _, err := service.ApplyPreparedRewrapInTx(ctx, tx, batch); err != nil {
			return err
		}
		e, _ := store.InTx(tx)
		if err := e.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		if mode == "before" {
			fmt.Printf("UNCOMMITTED %d\n", pid)
			_, _ = bufio.NewReader(os.Stdin).ReadByte()
		}
		return nil
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	fmt.Printf("COMMITTED %d\n", pid)
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
}
func TestSecretKilledWorkerResumesDurableCheckpointAndBurnsRange(t *testing.T) {
	for _, mode := range []string{"nonce", "before", "after"} {
		t.Run(mode, func(t *testing.T) {
			f := newSecretFixture(t)
			finishSecretRotation(t, f.secret)
			for range 52 {
				f.create(t, []byte("restart value"))
			}
			next := f.reopen(t, masterKeys(t, 2, 1, 2))
			_ = next
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSecretCrashChild$")
			cmd.Env = append(os.Environ(), "AGENTEAM_SECRET_CRASH_CHILD="+mode, "AGENTEAM_SECRET_CRASH_DATABASE="+f.db.Name)
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
			waitDone := make(chan struct{})
			var waitErr error
			go func() { waitErr = cmd.Wait(); close(waitDone) }()
			t.Cleanup(func() {
				_ = cmd.Process.Kill()
				select {
				case <-waitDone:
				case <-ctx.Done():
				}
			})
			lines := make(chan string, 1)
			go func() {
				reader := bufio.NewScanner(stdout)
				if reader.Scan() {
					lines <- reader.Text()
				} else {
					lines <- ""
				}
			}()
			var line string
			select {
			case line = <-lines:
			case <-ctx.Done():
				t.Fatal("child barrier timeout")
			}
			fields := strings.Fields(line)
			if len(fields) != 2 {
				_ = cmd.Process.Kill()
				t.Fatalf("unexpected child barrier %q", line)
			}
			expected := "COMMITTED"
			if mode == "nonce" {
				expected = "PREPARED"
			}
			if mode == "before" {
				expected = "UNCOMMITTED"
			}
			if fields[0] != expected {
				t.Fatalf("barrier=%q", line)
			}
			pid, err := strconv.Atoi(fields[1])
			if err != nil {
				t.Fatal(err)
			}
			var processed, old int
			var high int64
			if err = f.store.QueryRow(ctx, `SELECT processed FROM agenteam_secret.secret_rotation_runs WHERE target_version=2`).Scan(&processed); err != nil {
				t.Fatal(err)
			}
			if err = f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_secret.secret_payloads WHERE master_version=1`).Scan(&old); err != nil {
				t.Fatal(err)
			}
			if mode == "after" {
				if processed != 100 || old != 4 {
					t.Fatalf("real checkpoint %d old=%d", processed, old)
				}
			} else if processed != 0 || old != 104 {
				t.Fatalf("uncommitted state escaped %d old=%d", processed, old)
			}
			if err = f.store.QueryRow(ctx, `SELECT nonce_high_water FROM agenteam_secret.secret_master_registry WHERE version=2`).Scan(&high); err != nil || high != 2048 {
				t.Fatalf("child range %d %v", high, err)
			}
			if err = cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-waitDone:
				err = waitErr
				if err == nil {
					t.Fatal("child was not killed")
				}
			case <-ctx.Done():
				t.Fatal("killed child did not exit")
			}
			if pid != 0 {
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for {
					var alive bool
					if err = f.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=$1 AND pid=$2)`, f.db.Name, pid).Scan(&alive); err != nil {
						t.Fatal(err)
					}
					if !alive {
						break
					}
					select {
					case <-ticker.C:
					case <-ctx.Done():
						t.Fatal("owned child backend outlived process")
					}
				}
			}
			restarted := f.reopen(t, masterKeys(t, 2, 1, 2))
			finishSecretRotation(t, restarted)
			if err = f.store.QueryRow(ctx, `SELECT processed FROM agenteam_secret.secret_rotation_runs WHERE target_version=2`).Scan(&processed); err != nil || processed != 104 {
				t.Fatalf("recovered checkpoint %d %v", processed, err)
			}
			if mode != "after" {
				var min int64
				if err = f.store.QueryRow(ctx, `SELECT min(('x'||encode(substring(wrap_nonce from 5),'hex'))::bit(64)::bigint) FROM agenteam_secret.secret_payloads WHERE master_version=2`).Scan(&min); err != nil || min <= high {
					t.Fatalf("crashed range reused min=%d high=%d %v", min, high, err)
				}
			}
			f.reopen(t, masterKeys(t, 2, 2))
		})
	}
}
