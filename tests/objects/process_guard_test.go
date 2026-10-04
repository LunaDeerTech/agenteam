//go:build integration

package objects_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type guardChildInput struct{ Database, Bucket, Spool, Process string }

func TestObjectProcessGuardChild(t *testing.T) {
	path := os.Getenv("AGENTEAM_OBJECT_GUARD_CHILD")
	if path == "" {
		t.Skip("owned subprocess only")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatal("invalid owned child input")
	}
	raw, err := os.ReadFile(path)
	var input guardChildInput
	if err != nil || json.Unmarshal(raw, &input) != nil {
		t.Fatal("invalid child input")
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
	defer store.ForceClose(context.Background())
	remote, err := objectfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"ENDPOINT": remote.Endpoint(), "BUCKET": input.Bucket, "ACCESS_KEY": remote.AccessKey, "SECRET_KEY": remote.SecretKey, "TLS_MODE": "verify-full", "CA_FILE": remote.CAFile}
	storage, err := object.LoadStorageConfig(func(name string) (string, bool) {
		v, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	process, err := foundation.ParseID[oc.Process](input.Process)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{store: store, config: storage, authority: &authority{store}}
	runtime, service, _ := runtimeAt(t, f, input.Spool, process, nil)
	if err = runtime.Initialize(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	fmt.Println("CLAIM_BOUND")
	reader := bufio.NewReader(os.Stdin)
	if _, err = reader.ReadByte(); err != nil {
		t.Fatal("parent barrier closed")
	}
	// Close the directory spool lock through its real lifecycle, deliberately
	// retain the independently held ProcessGuard flock for the live-FD probe.
	service.StopAdmission()
	if err = service.Drain(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	fmt.Println("SPOOL_DRAINED_CLAIM_HELD")
	_, _ = reader.ReadByte()
}

func TestObjectProcessGuardExactFlockKillAndHistoricalBoot(t *testing.T) {
	f := newTransferFixture(t)
	old := id[oc.Process](t)
	spool := filepath.Join(t.TempDir(), "spool")
	input := guardChildInput{f.db.Name, f.bucket, spool, old.String()}
	raw, _ := json.Marshal(input)
	path := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(contextFor(t), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestObjectProcessGuardChild$")
	cmd.Env = append(os.Environ(), "AGENTEAM_OBJECT_GUARD_CHILD="+path)
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
		case <-time.After(time.Second):
			t.Error("owned guard child failed to join")
		}
	})
	lines := make(chan string, 8)
	go func() {
		defer close(lines)
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			lines <- s.Text()
		}
	}()
	waitLine := func(want string) {
		t.Helper()
		select {
		case got := <-lines:
			if got != want {
				t.Fatalf("guard barrier %q", got)
			}
		case <-ctx.Done():
			t.Fatal("guard barrier timed out")
		}
	}
	waitLine("CLAIM_BOUND")
	_, err = object.OpenSpool(spool, id[oc.Process](t))
	requireCode(t, err, foundation.ResourceBusy)
	if _, err = stdin.Write([]byte{'d'}); err != nil {
		t.Fatal(err)
	}
	waitLine("SPOOL_DRAINED_CLAIM_HELD")
	runtime, _, guard := runtimeAt(t, f.fixture, spool, id[oc.Process](t), nil)
	if err = runtime.Initialize(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	requireCode(t, guard.ConfirmStopped(contextFor(t), old), foundation.ResourceBusy)
	// A genuinely external grant is not a local process lease and remains live
	// after this exact OS death proof. No age/heartbeat/TTL inference is used.
	grant, err := f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "external-survives"), f.spec(t, []byte("pending remote body")))
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("kill did not join")
	}
	var exit *exec.ExitError
	if !errors.As(waitErr, &exit) {
		t.Fatal("no actual killed exit")
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("not actual SIGKILL")
	}
	if err = guard.ConfirmStopped(contextFor(t), old); err != nil {
		t.Fatal("same-host actual kill proof", err)
	}
	claimPath := filepath.Join(spool+".processes", old.String()+".claim")
	claimRaw, err := os.ReadFile(claimPath)
	if err != nil {
		t.Fatal(err)
	}
	// Keep exact canonical field ordering and inode. This is a persisted
	// historical-boot branch probe; it does not claim this host was rebooted.
	var claim struct {
		Format        int    `json:"format"`
		Process       string `json:"process"`
		Deployment    string `json:"deployment"`
		SpoolIdentity string `json:"spool_identity"`
		SpoolDevice   uint64 `json:"spool_device"`
		SpoolInode    uint64 `json:"spool_inode"`
		Host          string `json:"host"`
		Boot          string `json:"boot"`
		Nonce         string `json:"nonce"`
	}
	if json.Unmarshal(claimRaw, &claim) != nil {
		t.Fatal("invalid actual claim")
	}
	oldBoot, oldHost := claim.Boot, claim.Host
	claim.Boot = id[struct{}](t).String()
	writeClaim := func() {
		t.Helper()
		data, _ := json.Marshal(claim)
		if err := os.WriteFile(claimPath, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeClaim()
	f.sql(t, `UPDATE agenteam_object.process_claims SET boot_id=$2 WHERE process_id=$1`, old.String(), claim.Boot)
	if err = guard.ConfirmStopped(contextFor(t), old); err != nil {
		t.Fatal("trusted same-host historical boot", err)
	}
	claim.Host = strings.Repeat("a", 64)
	writeClaim()
	f.sql(t, `UPDATE agenteam_object.process_claims SET host_identity=decode($2,'hex') WHERE process_id=$1`, old.String(), claim.Host)
	requireCode(t, guard.ConfirmStopped(contextFor(t), old), foundation.DependencyUnavailable)
	claim.Boot, claim.Host = oldBoot, oldHost
	writeClaim()
	f.sql(t, `UPDATE agenteam_object.process_claims SET boot_id=$2,host_identity=decode($3,'hex') WHERE process_id=$1`, old.String(), oldBoot, oldHost)
	claim.Nonce = strings.Repeat("b", 64)
	writeClaim()
	requireCode(t, guard.ConfirmStopped(contextFor(t), old), foundation.DependencyUnavailable)
	if err = os.Remove(claimPath); err != nil {
		t.Fatal(err)
	}
	requireCode(t, guard.ConfirmStopped(contextFor(t), old), foundation.DependencyUnavailable)
	requireCode(t, guard.ConfirmStopped(contextFor(t), id[oc.Process](t)), foundation.DependencyUnavailable)
	var active bool
	if err = f.store.QueryRow(contextFor(t), `SELECT l.state='active' FROM agenteam_object.object_transfers t JOIN agenteam_object.object_leases l ON l.id=t.lease_id WHERE t.id=$1`, grant.Status.ID.String()).Scan(&active); err != nil || !active {
		t.Fatal("local death released external lease", err)
	}
}
