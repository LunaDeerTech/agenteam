//go:build integration

package account_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type crashInput struct{ Database, Process, Stage string }
type bootstrapBarrierStore struct {
	*postgres.Store
	stage string
	fired atomic.Bool
}

func (s *bootstrapBarrierStore) pause() {
	if s.fired.CompareAndSwap(false, true) {
		fmt.Println("ACCOUNT_BOUNDARY")
		_, _ = bufio.NewReader(os.Stdin).ReadByte()
	}
}
func (s *bootstrapBarrierStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	r := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := fn(ctx, tx); e != nil {
			return e
		}
		if s.stage == "logged" && !s.fired.Load() {
			x, e := s.InTx(tx)
			if e != nil {
				return e
			}
			var yes bool
			if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.bootstrap WHERE log_state='written')`).Scan(&yes); e != nil {
				return e
			}
			if yes {
				s.pause()
			}
		}
		return nil
	})
	if r.State() == foundation.Committed && !s.fired.Load() && (s.stage == "created" || s.stage == "claimed") {
		want := "eligible"
		if s.stage == "claimed" {
			want = "attempted"
		}
		var yes bool
		if e := s.Store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.bootstrap WHERE log_state=$1)`, want).Scan(&yes); e == nil && yes {
			s.pause()
		}
	}
	return r
}
func TestAccountCrashHelper(t *testing.T) {
	path := os.Getenv("AGENTEAM_ACCOUNT_CRASH_INPUT")
	if path == "" {
		t.Skip("owned subprocess helper")
	}
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatal("invalid owned helper input")
	}
	b, e := os.ReadFile(path)
	var input crashInput
	if e != nil || json.Unmarshal(b, &input) != nil {
		t.Fatal("invalid helper descriptor")
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
	port := &bootstrapBarrierStore{Store: raw, stage: input.Stage}
	keys, _, _ := keys(t)
	a, e := account.NewAuthority(port, keys)
	if e != nil {
		t.Fatal(e)
	}
	process, e := foundation.ParseID[c.Process](input.Process)
	if e != nil {
		t.Fatal(e)
	}
	f := assembleAccount(t, raw, port, a, liveProcess{process})
	password := f.bootstrap(t)
	if input.Stage == "response" {
		r, _ := loginRequest(t, f, password, "admin@mail.com")
		response, e := f.service.Login(ctxFor(t), r)
		if e != nil {
			t.Fatal(e, safeFailure(e))
		}
		if e = response.UseCookie(func([]byte) error {
			fmt.Println("ACCOUNT_BOUNDARY")
			_, _ = bufio.NewReader(os.Stdin).ReadByte()
			return nil
		}); e != nil {
			t.Fatal(e)
		}
	}
}

// This test adapter proves the exact OS child hosting the old account Service
// has exited. It rejects while that same child is alive; production uses the
// already verified shared ProcessGuard through this formal port.
type childDeath struct {
	current, child c.ProcessID
	exited         <-chan struct{}
}

func (p childDeath) CurrentProcess() c.ProcessID { return p.current }
func (p childDeath) ConfirmStopped(ctx context.Context, id c.ProcessID) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if id != p.child {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	select {
	case <-p.exited:
		return nil
	default:
		return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
	}
}
func TestAccountRecoveryExactChildDeathAndNoBootstrapReprint(t *testing.T) {
	for _, stage := range []string{"created", "claimed", "logged", "response"} {
		t.Run(stage, func(t *testing.T) {
			db, store, authority := database(t)
			process := id[c.Process](t)
			input := crashInput{db.Name, process.String(), stage}
			dir := t.TempDir()
			if e := os.Chmod(dir, 0700); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(dir, "input.json")
			raw, _ := json.Marshal(input)
			if e := os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAccountCrashHelper$")
			cmd.Env = append(os.Environ(), "AGENTEAM_ACCOUNT_CRASH_INPUT="+path, "TMPDIR="+dir)
			stdin, e := cmd.StdinPipe()
			if e != nil {
				t.Fatal(e)
			}
			defer stdin.Close()
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
					t.Error("owned child did not join")
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
				if got != "ACCOUNT_BOUNDARY" {
					t.Fatalf("child missed safe boundary: %q", got)
				}
			case <-ctx.Done():
				t.Fatal("child boundary timeout")
			}
			f := assembleAccount(t, store, store, authority, childDeath{id[c.Process](t), process, done})
			status, e := f.service.Recover(ctxFor(t))
			if e != nil {
				t.Fatal(e, safeFailure(e))
			}
			if status.Pending < 1 {
				t.Fatal("live child was retired")
			}
			if e = cmd.Process.Kill(); e != nil {
				t.Fatal(e)
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("child kill did not join")
			}
			for range 2 {
				if _, e = f.service.Recover(ctxFor(t)); e != nil {
					t.Fatal(e, safeFailure(e))
				}
			}
			var state string
			var active int
			if e = store.QueryRow(ctxFor(t), `SELECT log_state,(SELECT count(*) FROM agenteam_secret.secret_leases WHERE NOT released) FROM agenteam_account.bootstrap WHERE singleton`).Scan(&state, &active); e != nil {
				t.Fatal(e)
			}
			want := "unknown"
			if stage == "created" {
				want = "abandoned"
			}
			if stage == "response" {
				want = "written"
			}
			if state != want || active != 0 {
				t.Fatal("wrong death recovery", state, active)
			}
			out, e := f.service.Bootstrap(ctxFor(t))
			if e != nil || out.Created || out.LogState != want {
				t.Fatal("bootstrap reset/reprinted", out, e)
			}
			info, e := os.Stat(f.log)
			if e != nil || info.Size() != 0 {
				t.Fatal("recovery emitted old bootstrap material")
			}
		})
	}
}
