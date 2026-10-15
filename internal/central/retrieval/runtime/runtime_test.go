package runtime

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
)

func parseID[T any](t *testing.T, value string) f.ID[T] {
	t.Helper()
	v, err := f.ParseID[T](value)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func request(t *testing.T) rc.Request {
	return rc.Request{Query: "query", Scope: rc.Scope{Kind: rc.Knowledge, ProjectID: parseID[id.Project](t, "018f0000-0000-7000-8000-000000000001")}, Serving: rc.ServingIdentity{GenerationID: "018f0000-0000-7000-8000-000000000002", SnapshotID: parseID[mc.Snapshot](t, "018f0000-0000-7000-8000-000000000003"), Dimension: 2}, Limits: rc.Limits{LexicalK: 20, DenseK: 20, FusedK: 20, ResultK: 5, RRFConstant: 60, MaxResultBytes: 4096}}
}
func candidate(id string) rc.Candidate {
	return rc.Candidate{Source: rc.SourceKey{ID: id, Revision: 1}, RawText: "原文", Locator: []byte(`{"ordinal":1}`)}
}

func TestFusionUsesRanksAndPreservesSourceIdentity(t *testing.T) {
	a := candidate("018f0000-0000-7000-8000-000000000011")
	b := candidate("018f0000-0000-7000-8000-000000000012")
	c := candidate("018f0000-0000-7000-8000-000000000013")
	veryLarge := 1e200
	c.NativeScore = &veryLarge
	hits, err := fuse([]rc.Candidate{a, b, c}, []rc.Candidate{b, a}, request(t).Limits)
	if err != nil || len(hits) != 3 || hits[0].Source != a.Source || hits[1].Source != b.Source || hits[2].Source != c.Source {
		t.Fatalf("rank fusion/order failed: %v", err)
	}
	want := 1.0/61 + 1.0/62
	if math.Abs(hits[0].FusedScore-want) > 1e-15 || hits[0].LexicalRank != 1 || hits[0].DenseRank != 2 {
		t.Fatal("fusion did not give equal weight to each rank")
	}
	changed := a.Clone()
	changed.RawText = "other version under the same identity"
	if _, err = fuse([]rc.Candidate{a}, []rc.Candidate{changed}, request(t).Limits); err == nil {
		t.Fatal("inconsistent source generations were merged")
	}
	if _, err = fuse([]rc.Candidate{a, a}, nil, request(t).Limits); err == nil {
		t.Fatal("duplicate leg rank accepted")
	}
}

type provider struct {
	lexical, dense []rc.Candidate
	lexErr, denErr error
	checks         int
	changed        bool
}

func (p *provider) Lexical(context.Context, rc.Request) ([]rc.Candidate, error) {
	return p.lexical, p.lexErr
}
func (p *provider) Dense(context.Context, rc.Request, []float64) ([]rc.Candidate, error) {
	return p.dense, p.denErr
}
func (p *provider) ValidateServing(context.Context, rc.Request) error {
	p.checks++
	if p.changed && p.checks == 2 {
		return fault(f.VersionConflict)
	}
	return nil
}

type embedder struct{ vector []float64 }

func (e embedder) EmbedQuery(context.Context, rc.Request) ([]float64, error) {
	return e.vector, nil
}

func TestDegradationDoesNotBypassScopeOrGenerationFailure(t *testing.T) {
	a := candidate("018f0000-0000-7000-8000-000000000011")
	for _, tc := range []struct {
		name      string
		lexErr    error
		changed   bool
		vector    []float64
		wantError bool
		wantMode  rc.Mode
	}{
		{"hybrid", nil, false, []float64{1, 0}, false, rc.Hybrid},
		{"transient", fault(f.DependencyUnavailable), false, []float64{1, 0}, false, rc.DenseOnly},
		{"scope_denied", fault(f.Forbidden), false, []float64{1, 0}, true, ""},
		{"serving_changed", nil, true, []float64{1, 0}, true, ""},
		{"space_mismatch", nil, false, []float64{1}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &provider{lexical: []rc.Candidate{a}, dense: []rc.Candidate{a}, lexErr: tc.lexErr, changed: tc.changed}
			r, err := New(p, embedder{tc.vector}, Timeouts{time.Second, time.Second, time.Second})
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.Search(context.Background(), request(t))
			if (err != nil) != tc.wantError || tc.wantError && len(out.Hits) != 0 || !tc.wantError && (out.Mode != tc.wantMode || len(out.Hits) != 1) {
				t.Fatalf("wrong failure/mode: %v %s", err, out.Mode)
			}
		})
	}
}

type blockingProvider struct {
	*provider
	entered  chan struct{}
	release  chan struct{}
	returned chan struct{}
}

func (p *blockingProvider) Lexical(ctx context.Context, _ rc.Request) ([]rc.Candidate, error) {
	close(p.entered)
	<-ctx.Done()
	<-p.release
	close(p.returned)
	return nil, ctx.Err()
}

func TestCancellationWaitsForActualProviderReturn(t *testing.T) {
	p := &blockingProvider{provider: &provider{}, entered: make(chan struct{}), release: make(chan struct{}), returned: make(chan struct{})}
	r, _ := New(p, embedder{[]float64{1, 0}}, Timeouts{time.Second, time.Second, time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := r.Search(ctx, request(t)); done <- err }()
	<-p.entered
	cancel()
	select {
	case <-done:
		t.Fatal("cancel was mistaken for provider return")
	default:
	}
	close(p.release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	select {
	case <-p.returned:
	default:
		t.Fatal("original provider did not return")
	}
}
