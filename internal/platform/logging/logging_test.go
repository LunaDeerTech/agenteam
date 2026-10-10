package logging

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/platform/lifecycle"
)

func TestStructuredLogsAreSafeAndConcurrent(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Runner, slog.LevelInfo, &output)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() { logger.Transition(Unconnected) })
	}
	wg.Wait()
	logger.InvalidConfig("credential-SENTINEL", "credential-SENTINEL")
	logger.Failed(Phase("credential-SENTINEL"), lifecycle.FailureCode("credential-SENTINEL"))
	logger.ServerErrorLog().Print("credential-SENTINEL request/body/stack")
	if strings.Contains(output.String(), "SENTINEL") {
		t.Fatal("unsafe log projection")
	}
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	runID := ""
	count := 0
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		count++
		if record["service"] != "runner" || record["ready"] != false || record["connected"] != false || record["authenticated"] != false || record["event"] == nil {
			t.Fatalf("log contract: %v", record)
		}
		stamp := record["time"].(string)
		if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil || !strings.HasSuffix(stamp, "Z") {
			t.Fatal("time not UTC")
		}
		id := record["run_id"].(string)
		if len(id) != 32 {
			t.Fatal("invalid process run ID")
		}
		if runID != "" && runID != id {
			t.Fatal("run ID changed")
		}
		runID = id
	}
	if count != 35 {
		t.Fatal("log records lost")
	}
}
func TestLogLevelAndEntropyFailure(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Central, slog.LevelError, &output)
	if err != nil {
		t.Fatal(err)
	}
	logger.Transition(Starting)
	logger.Listening(netip.MustParseAddrPort("127.0.0.1:8080"))
	if output.Len() != 0 {
		t.Fatal("log level ignored")
	}
	logger.ShutdownComplete(true, lifecycle.ForcedShutdown)
	if !strings.Contains(output.String(), `"outcome":"forced"`) {
		t.Fatal("forced stop not logged")
	}
	if _, err := newLogger(Central, slog.LevelInfo, &output, strings.NewReader("")); err == nil {
		t.Fatal("entropy fallback")
	}
}

func TestRunnerConnectionProjectionAndCentralCompatibility(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Runner, slog.LevelInfo, &output)
	if err != nil {
		t.Fatal(err)
	}
	logger.Transition(Starting)
	states := []RunnerConnectionState{RunnerConnecting, RunnerConnected, RunnerDisconnected, RunnerIncompatible, RunnerConnectionState("PRIVATE_CREDENTIAL_CANARY")}
	for _, state := range states {
		logger.RunnerConnection(state)
		logger.Transition(Stopping)
	}
	for _, field := range []string{"IDENTITY_FILE", "CENTRAL_URL", "ID", "ROOT_PATH", "CA_FILE"} {
		logger.InvalidConfig("AGENTEAM_RUNNER_"+field, "invalid")
	}
	if strings.Contains(output.String(), "CANARY") {
		t.Fatal("unknown connection state leaked")
	}
	dec := json.NewDecoder(bytes.NewReader(output.Bytes()))
	index := 0
	for dec.More() {
		var record map[string]any
		if err := dec.Decode(&record); err != nil {
			t.Fatal(err)
		}
		expected := index == 3 || index == 4
		if record["connected"] != expected || record["authenticated"] != expected || record["ready"] != false {
			t.Fatal("connection projection changed current state", index, record)
		}
		// A duplicate member would make strict clients ambiguous even if Go's map
		// decoder chose its last value. The emitted JSON must contain one of each.
		index++
	}
	if strings.Count(output.String(), `"connected":`) != index || strings.Count(output.String(), `"authenticated":`) != index {
		t.Fatal("duplicate current connection fields")
	}
	output.Reset()
	central, _ := New(Central, slog.LevelInfo, &output)
	central.RunnerConnection(RunnerConnected)
	if output.Len() != 0 {
		t.Fatal("Runner event admitted on Central logger")
	}
	central.Transition(Starting)
	var record map[string]any
	if json.Unmarshal(output.Bytes(), &record) != nil || record["service"] != "central" {
		t.Fatal("Central logger changed")
	}
	if _, found := record["connected"]; found {
		t.Fatal("Runner state added to Central")
	}
}
