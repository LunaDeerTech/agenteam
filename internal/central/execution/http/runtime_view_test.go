package executionhttp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func testRuntimeView(mode ec.RuntimeAvailability) ec.TaskRuntimeView {
	e := testExecution()
	v := ec.TaskRuntimeView{Execution: e, Availability: mode}
	if mode == ec.RuntimePending {
		return v
	}
	at, _ := f.NewInstant(e.CreatedAt.Time().Add(time.Second))
	done, _ := f.NewInstant(at.Time().Add(time.Second))
	snapshot := testID[ec.Snapshot](7)
	round := testID[ec.Round](8)
	reason := "completed"
	e.Status, e.Version, e.StartedAt, e.CompletedAt, e.SnapshotID, e.Reason = ec.Succeeded, 4, &at, &done, &snapshot, &reason
	v.Execution = e
	if mode == ec.RuntimeAvailable {
		v.Snapshot = &ec.RuntimeSnapshot{Schema: ec.DurableFinalTextV1, Generation: snapshot, IncludedUpdateProgress: 1, ExecutionVersion: 4, Items: []ec.RuntimeTextItem{{ID: round, ExecutionID: e.ID, Seq: 1, Type: "text", Status: "completed", Revision: 1, StartedAt: at, UpdatedAt: done, CompletedAt: done, SourceReference: ec.RuntimeRoundReference{Kind: "execution_round", ID: round}, Data: ec.RuntimeTextData{Content: "<b>literal</b>\n文字"}}}}
	}
	return v
}
func TestTaskRuntimeViewHTTP(t *testing.T) {
	for _, mode := range []ec.RuntimeAvailability{ec.RuntimePending, ec.RuntimeNotRecorded, ec.RuntimeAvailable} {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(string(mode)+"-"+method, func(t *testing.T) {
				h, p, b := testHandler()
				p.runtime = testRuntimeView(mode)
				path := testPath() + "/" + p.get.ID.String() + "/runtime-view"
				if !HandlesPath(path) {
					t.Fatal("default App dispatch would miss route")
				}
				w := &testWriter{httptest.NewRecorder()}
				if runHTTP(h, httptest.NewRequest(method, path, nil), w) || w.Code != 200 || p.calls != 1 || b.auths != 1 {
					t.Fatal("runtime route", w.Code, b.problem)
				}
				if method == "HEAD" {
					if w.Body.Len() != 0 {
						t.Fatal("HEAD body")
					}
					return
				}
				var got ec.TaskRuntimeView
				if json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Availability != mode || got.ValidateProjection() != nil {
					t.Fatal("closed runtime wire")
				}
				if mode == ec.RuntimeAvailable && got.Snapshot.Items[0].Data.Content != p.runtime.Snapshot.Items[0].Data.Content {
					t.Fatal("text modified")
				}
			})
		}
	}
}
func TestTaskRuntimeViewHTTPRejectsProjection(t *testing.T) {
	for _, mutate := range []func(*ec.TaskRuntimeView){
		func(v *ec.TaskRuntimeView) { v.Execution.TaskID = testID[struct{}](9).String() },
		func(v *ec.TaskRuntimeView) { v.Snapshot.Generation = testID[ec.Snapshot](9) },
		func(v *ec.TaskRuntimeView) { v.Snapshot.ExecutionVersion++ },
		func(v *ec.TaskRuntimeView) { v.Snapshot.IncludedUpdateProgress++ },
		func(v *ec.TaskRuntimeView) { v.Snapshot.Items = nil },
		func(v *ec.TaskRuntimeView) { v.Snapshot.Items[0].SourceReference.ID = testID[ec.Round](9) },
	} {
		h, p, b := testHandler()
		p.runtime = testRuntimeView(ec.RuntimeAvailable)
		mutate(&p.runtime)
		w := &testWriter{httptest.NewRecorder()}
		if runHTTP(h, httptest.NewRequest("GET", testPath()+"/"+p.get.ID.String()+"/runtime-view", nil), w) || b.problem == nil {
			t.Fatal("bad projection exposed")
		}
	}
}
func TestTaskRuntimeViewHTTPOriginalReturn(t *testing.T) {
	h, p, _ := testHandler()
	p.runtime = testRuntimeView(ec.RuntimeAvailable)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan bool, 1)
	p.before = func(ctx context.Context) { close(entered); <-release }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("GET", testPath()+"/"+p.get.ID.String()+"/runtime-view", nil).WithContext(ctx)
	go func() { done <- runHTTP(h, r, &testWriter{httptest.NewRecorder()}) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("original read not entered")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("cancel bypassed original read return")
	default:
	}
	close(release)
	select {
	case aborted := <-done:
		if !aborted {
			t.Fatal("expired response published")
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP tail missing")
	}
}
