package knowledgehttp

import (
	"context"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// Real DTO validators/handler/Account Problem, with the author's explicitly
// private domain and authentication doubles. This does not certify permissions.
func TestIndependentKnowledgeHTTPProjection(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		h, _, ports := testHandler()
		d := testDocument()
		project := d.ProjectID
		if foreign {
			project = testID[id.Project](90)
		}
		creator, err := kc.NewCreatorRef(kc.CreatorDetails{Kind: id.AgentRun, ProjectID: project,
			AgentID: testID[id.Agent](91), ExecutionID: testID[id.Execution](92)})
		if err != nil {
			t.Fatal(err)
		}
		d.CreatedBy = creator
		ports.head = kc.DocumentHead{Active: &d}
		w := newTestWriter()
		r := httptest.NewRequest("GET", testPath("/knowledge/documents/"+d.ID.String()), nil)
		if serveTest(h, r, w) {
			t.Fatal("projection unexpectedly aborted")
		}
		if foreign {
			if w.Code != 503 || strings.Contains(w.Body.String(), "active") || strings.Contains(w.Body.String(), project.String()) {
				t.Fatal("foreign AgentRun creator projection escaped")
			}
			continue
		}
		if w.Code != 200 || strings.Contains(w.Body.String(), d.ObjectID.String()) {
			t.Fatal("known projection failed or leaked Object identity")
		}
		body := wireObject(t, w.Body.Bytes())["active"].(map[string]any)
		c := body["created_by"].(map[string]any)
		if len(body) != 12 || len(c) != 4 || c["kind"] != "agent_run" || c["user_id"] != nil || c["project_id"] != project.String() {
			t.Fatal("AgentRun safe fields changed")
		}
	}
	// A valid first row must never become a published prefix when the last
	// row has an invalid hidden ObjectID. Exercise actual HTTP publication.
	h, _, ports := testHandler()
	first, last := testDocument(), testDocument()
	first.Title, last.Title = "first-private-title", "last-private-title"
	last.ID, last.ObjectID = testID[kc.Document](93), oc.ObjectID{}
	ports.page = f.Page[kc.DocumentRef]{Items: []kc.DocumentRef{first, last}}
	w := newTestWriter()
	if serveTest(h, httptest.NewRequest("GET", testPath("/knowledge/documents"), nil), w) || w.Code != 503 || strings.Contains(w.Body.String(), first.Title) || strings.Contains(w.Body.String(), "items") {
		t.Fatal("bad final row published a validated prefix")
	}
}

func TestIndependentKnowledgeHTTPHeldBodyAndCallback(t *testing.T) {
	h, _, ports := testHandler()
	ctx, cancel := context.WithCancel(context.Background())
	bodyEntered, callbackEntered, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	bodyRelease, callbackRelease := make(chan struct{}), make(chan struct{})
	var bodyOnce, callbackOnce sync.Once
	releaseBody := func() { bodyOnce.Do(func() { close(bodyRelease) }) }
	releaseCallback := func() { callbackOnce.Do(func() { close(callbackRelease) }) }
	t.Cleanup(func() { cancel(); releaseBody(); releaseCallback(); joined(t, done) })
	var armed, bodyReturned, callbackReturned, earlyClear atomic.Bool
	w := newTestWriter()
	w.setRead = func(at time.Time) error {
		if !at.IsZero() && ctx.Err() != nil && armed.CompareAndSwap(false, true) {
			close(callbackEntered)
			<-callbackRelease
			callbackReturned.Store(true)
		}
		if at.IsZero() && (!bodyReturned.Load() || !callbackReturned.Load()) {
			earlyClear.Store(true)
		}
		return nil
	}
	// Keep the synchronous service call parked until the actual AfterFunc
	// entered. This excludes the handler's own abort SetReadDeadline call.
	ports.before = func(context.Context) { cancel(); <-callbackEntered }
	body := &testBody{Reader: strings.NewReader(""), close: func() error {
		close(bodyEntered)
		<-bodyRelease
		bodyReturned.Store(true)
		return nil
	}}
	r := httptest.NewRequest("GET", testPath("/knowledge/documents"), nil).WithContext(ctx)
	r.Body = body
	var aborted bool
	go func() { defer close(done); aborted = serveTest(h, r, w) }()
	joined(t, bodyEntered)
	select {
	case <-done:
		t.Fatal("handler returned with its original Body.Close held")
	default:
	}
	releaseBody()
	// Observe the original finish blocked on its callback channel after Close
	// has returned. A scheduling delay or an immediate select is not proof.
	waiting := false
	deadline := time.Now().Add(3 * time.Second)
	buffer := make([]byte, 1<<20)
	for !waiting && time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatal("handler returned with its actual cancellation callback held")
		default:
		}
		n := runtime.Stack(buffer, true)
		if n == len(buffer) {
			t.Fatal("owned callback wait observation truncated")
		}
		for _, stack := range strings.Split(string(buffer[:n]), "\n\n") {
			if strings.Contains(stack, "[chan receive]") && strings.Contains(stack, "(*requestIO).finish(") && !strings.Contains(stack, "(*testBody).Close(") {
				waiting = true
			}
		}
		runtime.Gosched()
	}
	if !waiting {
		t.Fatal("original finish callback wait was not observed")
	}
	releaseCallback()
	joined(t, done)
	if !aborted || earlyClear.Load() || !bodyReturned.Load() || !callbackReturned.Load() || body.closes.Load() != 1 || w.Body.Len() != 0 {
		t.Fatal("cancellation tail was abandoned or published late")
	}
	w.cleared(t)
}
