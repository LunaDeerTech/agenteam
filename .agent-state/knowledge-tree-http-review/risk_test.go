package commandhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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

// The adapter executes unchanged. Only private boundary/service ports are
// controlled; this cannot stand in for current Owner/Session/PG authorization.
type independentLookupCapture struct {
	*testService
	actor id.Actor
}

func (s *independentLookupCapture) LookupCommand(ctx context.Context, a id.Actor, r kc.LookupRequest) (kc.CommandLookup, error) {
	s.actor = a
	return s.testService.LookupCommand(ctx, a, r)
}

func TestIndependentTreeCommandBindingAndProjection(t *testing.T) {
	var baseline f.Digest
	for _, scenario := range []string{"original", "same_user_new_session", "other_user", "other_title", "other_expected"} {
		t.Run(scenario, func(t *testing.T) {
			h, s, b := fixture()
			capture := &independentLookupCapture{testService: s}
			h.service = capture
			title, expected := "original", "1"
			switch scenario {
			case "same_user_new_session":
				b.actor = must(id.NewHuman(testID[id.User](1), testID[id.Session](91)))
			case "other_user":
				b.actor = must(id.NewHuman(testID[id.User](92), testID[id.Session](93)))
			case "other_title":
				title = "changed"
			case "other_expected":
				expected = "2"
			}
			body := fmt.Sprintf(`{"command":"update","document_id":%q,"request":{"expected_version":%q,"title":%q}}`, s.doc.ID.String(), expected, title)
			requireSuccess(t, h, request("lookup", body))
			if capture.actor.Details() != b.actor.Details() || s.calls != 1 || s.gotLookup.ProjectID != s.doc.ProjectID {
				t.Fatal("original authenticated actor/project was replaced or mutation retried")
			}
			if scenario == "original" {
				baseline = s.gotLookup.SemanticDigest
			} else if (s.gotLookup.SemanticDigest == baseline) != (scenario == "same_user_new_session") {
				t.Fatal("stable user or original semantic binding changed")
			}
		})
	}
	for _, scenario := range []string{"active_creator", "foreign_creator", "zero_hidden_object", "committed_then_close_error"} {
		t.Run(scenario, func(t *testing.T) {
			h, s, _ := fixture()
			project := s.doc.ProjectID
			if scenario == "foreign_creator" {
				project = testID[id.Project](99)
			}
			s.doc.CreatedBy = must(kc.NewCreatorRef(kc.CreatorDetails{Kind: id.AgentRun, ProjectID: project, AgentID: testID[id.Agent](98), ExecutionID: testID[id.Execution](97)}))
			if scenario == "zero_hidden_object" {
				s.doc.ObjectID = oc.ObjectID{}
			}
			r := request("rename", `{"expected_version":"1","title":"original"}`)
			body := &heldBody{Reader: strings.NewReader(`{"expected_version":"1","title":"original"}`)}
			if scenario == "committed_then_close_error" {
				body.close = func() error { return errors.New("PRIVATE-CLOSE-CANARY") }
			}
			r.Body = body
			w := writer()
			aborted := serve(h, r, w)
			if s.calls != 1 || body.closes.Load() != 1 {
				t.Fatal("missing original service/body completion")
			}
			if scenario == "active_creator" {
				if aborted || !strings.Contains(w.Body.String(), `"kind":"agent_run"`) || strings.Contains(w.Body.String(), "object_id") || !w.cleared() {
					t.Fatal("safe active creator projection failed")
				}
			} else if !aborted || w.Body.Len() != 0 || w.cleared() {
				t.Fatal("post-success invalid/private result published or made reusable")
			}
		})
	}
}

func TestIndependentTreeCommandActualCallbackTail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), requestBudget)
	defer cancel()
	entered, release, bodyClosed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var held atomic.Bool
	private := errors.New("PRIVATE-CALLBACK-CANARY")
	w := writer()
	w.write = func(at time.Time) error {
		if !at.IsZero() && !at.After(time.Now()) && held.CompareAndSwap(false, true) {
			close(entered)
			<-release
			return private
		}
		return nil
	}
	body := &heldBody{Reader: strings.NewReader(""), close: func() error { close(bodyClosed); return nil }}
	native := &nativeWriter{ResponseWriter: w}
	if !native.prepare() {
		t.Fatal("native writer")
	}
	owner := requestIO{controller: http.NewResponseController(native), ctx: ctx, body: body}
	if owner.start() != nil {
		t.Fatal("start")
	}
	cancel()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("callback not entered")
	}
	done := make(chan error, 1)
	go func() { done <- owner.finish(true) }()
	select {
	case <-bodyClosed:
	case <-time.After(time.Second):
		t.Fatal("original body not closed")
	}
	// Confirm actual finish is in its channel receive, not merely scheduled late
	// or held inside a second SetDeadline call. No wall-clock sleep is proof.
	observed := false
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		buf := make([]byte, 1<<16)
		n := runtime.Stack(buf, true)
		for _, stack := range strings.Split(string(buf[:n]), "\n\n") {
			if strings.Contains(stack, "[chan receive]:") && strings.Contains(stack, "commandhttp.(*requestIO).finish") {
				observed = true
			}
		}
		if observed {
			break
		}
		runtime.Gosched()
	}
	if !observed {
		t.Fatal("original callback join not observed")
	}
	select {
	case <-done:
		t.Fatal("finish returned before callback")
	default:
	}
	if w.cleared() {
		t.Fatal("deadline cleared before callback")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		if !errors.Is(err, private) || !errors.Is(err, context.DeadlineExceeded) || body.closes.Load() != 1 || w.cleared() {
			t.Fatal("callback error or actual retirement was lost")
		}
	case <-time.After(time.Second):
		t.Fatal("actual finish did not return")
	}
}
