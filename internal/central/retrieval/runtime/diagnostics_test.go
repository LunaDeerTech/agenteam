package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
)

type diagnosticObserver func(context.Context, rc.Diagnostics)

func (o diagnosticObserver) ObserveRetrieval(ctx context.Context, d rc.Diagnostics) { o(ctx, d) }

type diagnosticProvider struct{ *provider }

func (*diagnosticProvider) DiagnosticBackend(context.Context, rc.Request) string { return "pg_search" }

func TestDiagnosticsObserveActualModeAndCandidates(t *testing.T) {
	a, b := candidate("018f0000-0000-7000-8000-000000000011"), candidate("018f0000-0000-7000-8000-000000000012")
	for _, scenario := range []struct {
		name              string
		lexicalError      error
		changed           bool
		mode              rc.Mode
		errorCode, reason string
	}{
		{"hybrid", nil, false, rc.Hybrid, "", ""},
		{"lexical_unavailable", fault(f.DependencyUnavailable), false, rc.DenseOnly, "", "lexical_unavailable"},
		{"scope_denied", fault(f.Forbidden), false, "", "FORBIDDEN", ""},
		{"generation_changed", nil, true, "", "VERSION_CONFLICT", ""},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			p := &diagnosticProvider{&provider{lexical: []rc.Candidate{a}, dense: []rc.Candidate{a, b}, lexErr: scenario.lexicalError, changed: scenario.changed}}
			var observed []rc.Diagnostics
			r, err := New(p, embedder{[]float64{1, 0}}, Timeouts{time.Second, time.Second, time.Second}, diagnosticObserver(func(_ context.Context, d rc.Diagnostics) { observed = append(observed, d) }))
			if err != nil {
				t.Fatal(err)
			}
			q := request(t)
			q.Query = "private query: do not log this original text"
			out, err := r.Search(context.Background(), q)
			if (err != nil) != (scenario.errorCode != "") || len(observed) != 1 {
				t.Fatal("missing actual retrieval observation")
			}
			d := observed[0]
			digest := sha256.Sum256([]byte(q.Query))
			if _, err := f.ParseID[struct{}](d.RequestID); err != nil {
				t.Fatal("pipeline request identity missing")
			}
			if d.Mode != scenario.mode || d.ErrorCode != scenario.errorCode || d.DegradationReason != scenario.reason || d.Backend != "pg_search" || d.QuerySHA256 != hex.EncodeToString(digest[:]) || d.QueryBytes != len(q.Query) || d.ProjectID != q.Scope.ProjectID.String() || d.Domain != rc.Knowledge || d.RerankUsed || d.Total <= 0 || d.Lexical <= 0 || d.Embedding <= 0 || d.Dense <= 0 {
				t.Fatal("diagnostics did not reflect the actual execution")
			}
			if d.LexicalCandidates != 1 || d.DenseCandidates != 2 || d.ResultCount != len(out.Hits) {
				t.Fatal("diagnostics lost actual callback/result counts")
			}
			if scenario.errorCode == "" && d.FusionCandidates != 2 {
				t.Fatal("rank merge count was guessed from one leg")
			}
		})
	}
}

func TestDiagnosticsWaitForOriginalJoinAndCannotReplaceResult(t *testing.T) {
	p := &blockingProvider{provider: &provider{}, entered: make(chan struct{}), release: make(chan struct{}), returned: make(chan struct{})}
	observed := make(chan rc.Diagnostics, 1)
	r, err := New(p, embedder{[]float64{1, 0}}, Timeouts{time.Second, time.Second, time.Second}, diagnosticObserver(func(_ context.Context, d rc.Diagnostics) { observed <- d }))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := request(t)
	done := make(chan error, 1)
	go func() { _, err := r.Search(ctx, q); done <- err }()
	<-p.entered
	cancel()
	select {
	case <-observed:
		t.Fatal("diagnostics claimed return while original callback was live")
	default:
	}
	close(p.release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("original cancellation lost")
	}
	if d := <-observed; d.ErrorCode != "CANCELED" || d.Mode != "" {
		t.Fatal("cancellation was mislabeled as a retrieval mode")
	}
	a := candidate("018f0000-0000-7000-8000-000000000011")
	r, err = New(&provider{lexical: []rc.Candidate{a}}, embedder{[]float64{1, 0}}, Timeouts{time.Second, time.Second, time.Second}, diagnosticObserver(func(context.Context, rc.Diagnostics) { panic("private observer failure") }))
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Search(context.Background(), q)
	if err != nil || out.Mode != rc.Hybrid || len(out.Hits) != 1 {
		t.Fatal("observer replaced the original business result")
	}
}
