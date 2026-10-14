package executionhttp

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type controlCommands struct {
	calls, lookups int
	request        ec.CancelRequest
	err            error
	found          bool
	malformed      bool
}

func (p *controlCommands) receipt() ec.CancelReceipt {
	e := testExecution()
	e.Version = 2
	e.CancelRequestedAt = &e.CreatedAt
	if p.malformed {
		e.Status = ec.Cancelled
	}
	return ec.CancelReceipt{Execution: e, Task: ec.TaskCancellationProtection{ID: e.TaskID, Version: 4, State: "blocked", BlockerID: testID[struct{}](8).String()}}
}
func (p *controlCommands) Cancel(_ context.Context, _ i.Actor, r ec.CancelRequest) (ec.CancelResult, error) {
	p.calls++
	p.request = r
	return ec.CancelResult{Receipt: p.receipt()}, p.err
}
func (p *controlCommands) LookupCancel(_ context.Context, _ i.Actor, r ec.CancelRequest) (ec.CancelLookup, error) {
	p.lookups++
	p.request = r
	if !p.found {
		return ec.CancelLookup{}, p.err
	}
	v := p.receipt()
	return ec.CancelLookup{Found: true, Receipt: &v}, p.err
}
func cancelPath() string { return testPath() + "/" + testID[i.Execution](5).String() + "/cancel" }

func TestExecutionCancelHTTPAndOriginalIntentLookup(t *testing.T) {
	for _, lookup := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "lookup"}[lookup], func(t *testing.T) {
			h, reads, _ := testHandler()
			commands := &controlCommands{found: true}
			h.controls = commands
			path := cancelPath()
			if lookup {
				path += "/lookup"
			}
			r := httptest.NewRequest("POST", path, strings.NewReader(`{"expected_version":"1","expected_task_version":"3","reason":"  stop this execution  "}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", "original-cancel")
			w := &testWriter{httptest.NewRecorder()}
			if runHTTP(h, r, w) || w.Code != 200 || reads.calls != 0 {
				t.Fatal("control route failed", w.Code, w.Body.String())
			}
			if commands.request.Meta.IdempotencyKey != "original-cancel" || commands.request.ExpectedVersion != 1 || commands.request.ExpectedTaskVersion != 3 || *commands.request.Reason != "  stop this execution  " {
				t.Fatal("original intent changed")
			}
			if lookup && commands.lookups != 1 || !lookup && commands.calls != 1 {
				t.Fatal("lookup executed mutation")
			}
		})
	}
}
func TestExecutionCancelHTTPStrictBodyAndRefusal(t *testing.T) {
	for _, body := range []string{`{}`, `{"expected_version":1,"expected_task_version":"3"}`, `{"expected_version":"1","expected_task_version":"3","reason":null}`, `{"expected_version":"1","expected_task_version":"3","reason":"  "}`, `{"expected_version":"1","expected_task_version":"3","extra":true}`, `{"expected_version":"1","expected_version":"2","expected_task_version":"3"}`} {
		h, _, _ := testHandler()
		commands := &controlCommands{}
		h.controls = commands
		r := httptest.NewRequest("POST", cancelPath(), strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "original-cancel")
		w := &testWriter{httptest.NewRecorder()}
		runHTTP(h, r, w)
		if w.Code != 400 || commands.calls != 0 || commands.lookups != 0 {
			t.Fatal("invalid command reached domain", w.Code, body)
		}
	}
	h, _, boundary := testHandler()
	commands := &controlCommands{}
	h.controls = commands
	boundary.deny = f.NewFault(f.Forbidden, f.NotStarted)
	r := httptest.NewRequest("POST", cancelPath(), strings.NewReader(`{"expected_version":"1","expected_task_version":"3"}`))
	w := &testWriter{httptest.NewRecorder()}
	runHTTP(h, r, w)
	if commands.calls != 0 || boundary.auths != 0 {
		t.Fatal("denied session reached control service")
	}
}
func TestExecutionCancelHTTPBadMutationReceiptAborts(t *testing.T) {
	h, _, _ := testHandler()
	h.controls = &controlCommands{malformed: true}
	r := httptest.NewRequest("POST", cancelPath(), strings.NewReader(`{"expected_version":"1","expected_task_version":"3"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "original-cancel")
	if !runHTTP(h, r, &testWriter{httptest.NewRecorder()}) {
		t.Fatal("invalid committed receipt became a retryable problem")
	}
}
