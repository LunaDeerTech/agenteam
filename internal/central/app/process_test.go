package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

// This helper exists only in a Go test executable. It runs the actual app and
// lifecycle with a controlled blocking handler, and has no production CLI flag.
func TestAppProcessFixture(t *testing.T) {
	mode := os.Getenv("AGENTEAM_APP_PROCESS_FIXTURE")
	if mode == "" {
		return
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	cfg, err := config.Load(os.LookupEnv, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	logger, err := logging.New(logging.Central, slog.LevelInfo, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	go func() {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err == nil && strings.TrimSpace(line) == "release" {
			close(release)
		}
	}()
	deps := dependencies{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(os.Stdout, `{"event":"handler_entered"}`)
		<-release // Deliberately ignores cancellation to test bounded process exit.
		if r.Context().Err() != nil {
			_, _ = fmt.Fprintln(os.Stdout, `{"event":"early_cancel"}`)
			w.WriteHeader(500)
			return
		}
		_ = httpapi.WriteJSON(w, r, 200, struct {
			Finished bool `json:"finished"`
		}{true})
	})}
	if mode == "startup" {
		deps.listen = func(ctx context.Context, network, address string) (net.Listener, error) {
			listener, err := (&net.ListenConfig{}).Listen(ctx, network, address)
			if err != nil {
				return nil, err
			}
			_ = json.NewEncoder(os.Stdout).Encode(struct {
				Event   string `json:"event"`
				Address string `json:"listen_address"`
			}{"startup_blocked", listener.Addr().String()})
			<-ctx.Done()
			return listener, nil
		}
	}
	err = run(context.Background(), cfg, logger, signals, unitDependencies(deps))
	signal.Stop(signals)
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

type fixtureProcess struct {
	cmd            *exec.Cmd
	stdout, stderr *eventLog
	stdin          io.WriteCloser
	done           chan struct{}
	err            error
}

func launchFixture(t *testing.T, mode, timeout string) *fixtureProcess {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := &fixtureProcess{cmd: exec.Command(executable, "-test.run=^TestAppProcessFixture$", "-test.timeout=15s"), stdout: newEventLog(), stderr: newEventLog(), done: make(chan struct{})}
	p.cmd.Dir = t.TempDir()
	p.cmd.Env = []string{`AGENTEAM_CENTRAL_SECRET_KEYRING={"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`, `AGENTEAM_CENTRAL_CURSOR_KEYRING={"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`, "AGENTEAM_APP_PROCESS_FIXTURE=" + mode, "AGENTEAM_CENTRAL_HTTP_ADDR=127.0.0.1:0", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=" + timeout, "AGENTEAM_CENTRAL_DATABASE_URL=postgresql://unit:unit@127.0.0.1:1/unit", "AGENTEAM_CENTRAL_DATABASE_TLS_MODE=disable"}
	p.cmd.Stdout = p.stdout
	p.cmd.Stderr = p.stderr
	p.stdin, err = p.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		_ = p.stdin.Close()
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			await(t, p.done)
		}
	})
	return p
}
func (p *fixtureProcess) wait(t *testing.T, want int) {
	t.Helper()
	select {
	case <-p.done:
	case <-time.After(4 * time.Second):
		t.Fatalf("fixture failed bounded exit: %s", p.stderr.String())
	}
	code := 0
	if p.err != nil {
		e, ok := p.err.(*exec.ExitError)
		if !ok {
			t.Fatal(p.err)
		}
		code = e.ExitCode()
	}
	if code != want {
		t.Fatalf("fixture exit=%d want=%d stdout=%s stderr=%s", code, want, p.stdout.String(), p.stderr.String())
	}
}

func TestRealProcessDrainDeadlineAndSecondSignal(t *testing.T) {
	for _, mode := range []string{"graceful", "deadline", "second_signal"} {
		t.Run(mode, func(t *testing.T) {
			timeout := "5s"
			if mode == "deadline" {
				timeout = "100ms"
			}
			p := launchFixture(t, mode, timeout)
			listening := p.stderr.wait(t, func(e map[string]any) bool { return e["event"] == "listening" })
			address := listening["listen_address"].(string)
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			response := asyncGet(client, "http://"+address+"/hold")
			p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "handler_entered" })
			if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			p.stderr.wait(t, func(e map[string]any) bool { return e["phase"] == "stopping" })
			if mode == "graceful" {
				select {
				case <-p.done:
					t.Fatal("first signal skipped active request drain")
				default:
				}
				_, _ = io.WriteString(p.stdin, "release\n")
				result := receiveResponse(t, response)
				if result.err != nil || result.status != 200 {
					t.Fatalf("real-process drain failed: %+v", result)
				}
				p.wait(t, 0)
				if strings.Contains(p.stdout.String(), "early_cancel") || !strings.Contains(p.stderr.String(), `"outcome":"drained"`) {
					t.Fatal("first signal cancelled serving or misreported outcome")
				}
			} else {
				if mode == "second_signal" {
					if err := p.cmd.Process.Signal(syscall.SIGINT); err != nil {
						t.Fatal(err)
					}
				}
				p.wait(t, 1)
				if result := receiveResponse(t, response); result.err == nil {
					t.Fatal("noncooperative handler retained a successful connection")
				}
				code := "SHUTDOWN_TIMEOUT"
				if mode == "second_signal" {
					code = "FORCED_SHUTDOWN"
				}
				if !strings.Contains(p.stderr.String(), `"outcome":"forced"`) || !strings.Contains(p.stderr.String(), `"code":"`+code+`"`) {
					t.Fatal("forced process exit not distinguished")
				}
			}
		})
	}
}

func TestRealProcessSignalDuringStartup(t *testing.T) {
	p := launchFixture(t, "startup", "1s")
	event := p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "startup_blocked" })
	address := event["listen_address"].(string)
	if err := p.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	p.wait(t, 0)
	if strings.Contains(p.stderr.String(), `"event":"listening"`) || strings.Contains(p.stderr.String(), `"phase":"diagnostic_serving"`) {
		t.Fatal("interrupted startup claimed to serve")
	}
	if conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("startup-acquired listener leaked")
	}
}
