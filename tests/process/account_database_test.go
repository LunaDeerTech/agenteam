//go:build integration

package process_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func TestAccountProcessBootstrapRecoveryLogAndBoundCapabilities(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	env := databaseEnvironment(t, db, "AGENTEAM_CENTRAL_PUBLIC_ORIGIN=http://localhost:8080")
	var recoveryPath string
	for _, entry := range env {
		if strings.HasPrefix(entry, "AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG=") {
			recoveryPath = strings.TrimPrefix(entry, "AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG=")
		}
	}
	if recoveryPath == "" {
		t.Fatal("fixture omitted its private recovery path")
	}
	p := launch(t, "agenteam", nil, env)
	address := p.event(t, "event", "listening")["listen_address"].(string)
	waitDiagnosticState(t, address, "available", time.Second)
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	r, _ := http.NewRequest("GET", "http://"+address+"/api/v1/auth/bootstrap", nil)
	r.Host = "localhost:8080"
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	var body map[string]any
	if err != nil || response.StatusCode != 200 || json.Unmarshal(raw, &body) != nil || body["delivery_channel"] != "backend_log" || body["csrf_token"] == "" {
		t.Fatal("real binary did not expose its initialized account browser boundary")
	}
	conn := db.Connect(t)
	var users int
	var logState string
	if err := conn.QueryRow(databaseContext(t), `SELECT (SELECT count(*) FROM agenteam_account.users),log_state FROM agenteam_account.bootstrap WHERE singleton`).Scan(&users, &logState); err != nil || users != 1 || logState != "written" {
		t.Fatal("real bootstrap did not commit its singleton and restricted log", err)
	}
	if err := p.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	p.wait(t, 0)
	first, err := os.ReadFile(recoveryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(first)
	lines := bufio.NewScanner(bytes.NewReader(first))
	count := 0
	for lines.Scan() {
		var record struct {
			Purpose  string `json:"purpose"`
			Password string `json:"initial_password"`
		}
		if json.Unmarshal(lines.Bytes(), &record) != nil || record.Purpose != "bootstrap" || record.Password == "" {
			t.Fatal("invalid dedicated bootstrap record")
		}
		if strings.Contains(p.stderr.String()+p.stdout.String(), record.Password) {
			t.Fatal("real process leaked the bootstrap password to ordinary output")
		}
		count++
	}
	if lines.Err() != nil || count != 1 {
		t.Fatal("bootstrap was printed more than once")
	}
	// Reuse exact key/log/object inputs, as a deployment restart would.
	restarted := launch(t, "agenteam", nil, env)
	restarted.event(t, "event", "listening")
	if err := restarted.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	restarted.wait(t, 0)
	second, err := os.ReadFile(recoveryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(second)
	if !bytes.Equal(first, second) {
		t.Fatal("restart reprinted or changed bootstrap recovery material")
	}
	if err := conn.QueryRow(databaseContext(t), `SELECT count(*) FROM agenteam_account.users`).Scan(&users); err != nil || users != 1 {
		t.Fatal("restart created another bootstrap user", err)
	}
	assertDatabaseLogsSafe(t, p, db)
	assertDatabaseLogsSafe(t, restarted, db)
	noCentralBackends(t, db)
}

func TestAccountProcessUnsafeRecoveryLogFailsBeforeListening(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	env := databaseEnvironment(t, db)
	var path string
	for _, entry := range env {
		if strings.HasPrefix(entry, "AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG=") {
			path = strings.TrimPrefix(entry, "AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG=")
		}
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	p := launch(t, "agenteam", nil, env)
	p.wait(t, 1)
	if strings.Contains(p.stderr.String(), `"event":"listening"`) || strings.Contains(p.stderr.String()+p.stdout.String(), path) {
		t.Fatal("unsafe recovery log either served traffic or leaked its private path")
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != 0 || info.Mode().Perm() != 0644 {
		t.Fatal("failed sink opening changed the rejected file", err)
	}
	noCentralBackends(t, db)
}
