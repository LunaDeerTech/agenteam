package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestRetrievalDiagnosticsClosedProjection(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Central, slog.LevelInfo, &output)
	if err != nil {
		t.Fatal(err)
	}
	const key = "018f0000-0000-7000-8000-000000000001"
	logger.Retrieval(RetrievalDiagnostic{RequestID: key, ProjectID: key, Domain: "knowledge", Backend: "pg_search", Mode: "hybrid", QuerySHA256: strings.Repeat("a", 64), QueryBytes: 31, LexicalCandidates: 2, DenseCandidates: 3, FusionCandidates: 4, ResultCount: 2, Total: time.Millisecond})
	var valid map[string]any
	if err := json.Unmarshal(output.Bytes(), &valid); err != nil {
		t.Fatal(err)
	}
	if valid["retrieval_mode"] != "hybrid" || valid["lexical_backend"] != "pg_search" || valid["request_id"] != key || valid["lexical_candidate_count"] != float64(2) || valid["dense_candidate_count"] != float64(3) || valid["error_code"] != "" {
		t.Fatal("actual retrieval projection lost its safe fields")
	}
	output.Reset()
	const canary = "private query/credential/error CANARY"
	logger.Retrieval(RetrievalDiagnostic{RequestID: canary, ProjectID: canary, AgentID: canary, Domain: canary, QuerySHA256: canary, Backend: canary, Mode: canary, ErrorCode: canary, DegradationReason: canary, QueryBytes: -1, LexicalCandidates: 5000, Total: -time.Second})
	if strings.Contains(output.String(), "CANARY") {
		t.Fatal("untrusted diagnostic field escaped its closed projection")
	}
	var invalid map[string]any
	if err := json.Unmarshal(output.Bytes(), &invalid); err != nil {
		t.Fatal(err)
	}
	if invalid["error_code"] != "INTERNAL_ERROR" || invalid["retrieval_mode"] != "" || invalid["query_bytes"] != float64(0) || invalid["lexical_candidate_count"] != float64(0) || invalid["request_id"] != "" {
		t.Fatal("invalid diagnostics were accepted as successful retrieval")
	}
}
