package work

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func taskContextFixture(t *testing.T, purpose string) (ec.PreparationRequest, ec.CapturedTriggerInput) {
	t.Helper()
	execution, request := taskTriggerFixtureRequest(t)
	w := taskTriggerFixtureInput(t)
	// Controlled codec material; real Task capture/commit is independently
	// exercised by the PostgreSQL context fixture, not minted by these values.
	w.Task.AssigneeAgentID = &request.AgentID
	w.Task.State = c.TaskStateInProgress
	request.Purpose = purpose
	w.Purpose = purpose
	if purpose == "task/review" {
		w.Task.State = c.TaskStateInReview
	}
	w.Sprint.State = c.Current
	at := w.Sprint.CreatedAt
	w.Sprint.StartedAt = &at
	w.Sprint.StartedBy = &c.ActorHistory{Kind: i.Human, UserID: pureID[i.User](t, 1).String()}
	digest, err := request.Digest()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(capturedTaskWire{1, execution, digest, w})
	if err != nil {
		t.Fatal(err)
	}
	input, err := ec.NewCapturedTriggerInput(ec.TriggerInputRef{ProviderType: "task", SchemaVersion: 1, InputID: pureID[ec.TriggerInput](t, 93), Digest: ec.TriggerInputDigest(raw)}, raw)
	if err != nil {
		t.Fatal(err)
	}
	return ec.PreparationRequest{ExecutionID: execution, Launch: request}, input
}
func TestTaskContextBuildUsesOnlyFixedTypedSource(t *testing.T) {
	for _, purpose := range []string{"task/work", "task/review"} {
		r, source := taskContextFixture(t, purpose)
		built, err := (TaskContextBuilder{}).BuildTriggerContext(context.Background(), r, source)
		if err != nil || !built.Matches(r, source) {
			t.Fatal("Build lost original source", err)
		}
		typed, err := DecodeTaskContext(built)
		if err != nil || typed.Purpose() != purpose || typed.Task().ProjectID != r.Launch.ProjectID || typed.Task().ID.String() != r.Launch.Trigger.TaskID || typed.Task().Version != 1 || len(typed.RecentTaskEvents()) != 1 {
			t.Fatal("typed source not preserved", err)
		}
		want := TaskWorkPromptRevision
		if purpose == "task/review" {
			want = TaskReviewPromptRevision
		}
		if built.Instructions().Revision() != want || !strings.Contains(built.Instructions().Content(), "version conflict") || !strings.Contains(built.Instructions().Content(), "actually supplied") {
			t.Fatal("missing scoped task instructions")
		}
		// This provider has no Store or mutable current Task dependency. Repeated
		// builds preserve original exact bytes and fixed component revision.
		again, err := (TaskContextBuilder{}).BuildTriggerContext(context.Background(), r, source)
		if err != nil || again.Instructions().Digest() != built.Instructions().Digest() || !bytes.Equal(again.Source().Data(), source.Data()) {
			t.Fatal("pure Build drifted", err)
		}
		body := built.Source().Data()
		body[0] = '!'
		task := typed.Task()
		task.Title = "later title"
		restored, err := DecodeTaskContext(built)
		if err != nil || restored.Task().Title == task.Title || !bytes.Equal(built.Source().Data(), source.Data()) {
			t.Fatal("returned source aliased", err)
		}
		safe, _ := json.Marshal(built)
		if string(safe) != `"trigger_context"` || strings.Contains(fmt.Sprintf("%+v", built), "private") {
			t.Fatal("implicit Task material escaped")
		}
	}
}
func TestTaskContextBuildRejectsReboundInputAndUnknownComponent(t *testing.T) {
	r, source := taskContextFixture(t, "task/work")
	for _, mode := range []string{"execution", "purpose", "agent", "malformed", "canceled"} {
		request := r.Clone()
		input := source
		ctx, cancel := context.WithCancel(context.Background())
		switch mode {
		case "execution":
			request.ExecutionID = pureID[i.Execution](t, 98)
		case "purpose":
			request.Launch.Purpose = "task/review"
		case "agent":
			request.Launch.AgentID = pureID[i.Agent](t, 98)
		case "malformed":
			raw := []byte(`{"partial":true}`)
			ref := source.Ref()
			ref.Digest = ec.TriggerInputDigest(raw)
			var err error
			input, err = ec.NewCapturedTriggerInput(ref, raw)
			if err != nil {
				t.Fatal(err)
			}
		case "canceled":
			cancel()
		}
		out, err := (TaskContextBuilder{}).BuildTriggerContext(ctx, request, input)
		cancel()
		if err == nil || out.Validate() == nil {
			t.Fatal("rebound Task source accepted", mode)
		}
		if mode == "canceled" && !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation lost")
		}
	}
	component, err := ec.NewPromptComponent("unknown/task-prompt/v2", "different task instructions")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := ec.NewTriggerContext(r, source, component)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeTaskContext(changed); err == nil {
		t.Fatal("unknown scene interpreted as current version")
	}
	if out, err := (TaskContextBuilder{}).BuildTriggerContext(nil, r, source); err == nil || out.Validate() == nil {
		t.Fatal("nil context accepted")
	}
}
