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
