package execution

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// These use original typed capture candidates and controlled SQL. Real
// terminal transaction binding, Model HTTP and restart are App/PG concerns.
type runtimeSnapshotSQL struct {
	postgres.SQLExecutor
	args          []any
	reads, writes int
}

func (s *runtimeSnapshotSQL) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if !bytes.HasPrefix([]byte(q), []byte("INSERT INTO agenteam_execution.runtime_snapshots")) {
		return pgconn.CommandTag{}, invalid()
	}
	s.args = append([]any(nil), args...)
	s.writes++
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (s *runtimeSnapshotSQL) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	s.reads++
	if len(s.args) != 9 || !reflect.DeepEqual(args, s.args[:7]) {
		return launchScan{}
	}
	return launchScan{values: []any{s.args[7], s.args[8]}}
}
func runtimeCandidate(t *testing.T, text string) *directTextCall {
	t.Helper()
	facts, _ := runtimeModelCandidate(t)
	r := facts.Round.Fields()
	at, _ := f.NewInstant(r.CreatedAt.Time().Add(time.Second))
	response := mc.ModelResponse{CallID: r.CallID, InvocationID: newTestID[mc.Invocation](t), Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: text}}, {Text: &mc.TextPart{Text: "\n末尾"}}}}, FinishReason: "stop", Usage: mc.Usage{Source: mc.UnknownUsage}, ProviderRequestID: "not-public"}
	raw, err := directTextJSON(response)
	if err != nil {
		t.Fatal(err)
	}
	return &directTextCall{owner: &directTextState{}, request: facts.Snapshot.Fields().Context.Input().Fields().Request, snapshot: facts.Snapshot, round: facts.Round, terminal: &directTextTerminal{status: ec.Succeeded, reason: "completed", version: 4, through: 2, at: at, response: &response, responseBytes: raw}}
}
func TestRuntimeFinalTextProducer(t *testing.T) {
	for _, status := range []ec.Status{ec.Succeeded, ec.Failed, ec.Cancelled} {
		t.Run(string(status), func(t *testing.T) {
			run := runtimeCandidate(t, "<script>literal</script>\t")
			run.terminal.status = status
			if status != ec.Succeeded {
				run.terminal.response = nil
				run.terminal.responseBytes = nil
				run.terminal.through = 1
				run.terminal.reason = "model_failed"
				if status == ec.Cancelled {
					run.terminal.reason = "cancelled"
				}
			}
			x := &runtimeSnapshotSQL{}
			if err := saveDirectTextRuntime(context.Background(), x, run); err != nil {
				t.Fatal(err)
			}
			if x.writes != 1 {
				t.Fatal("missing actual projection INSERT")
			}
			raw := x.args[7].([]byte)
			if bytes.Contains(raw, []byte("not-public")) || bytes.Contains(raw, []byte("captured system")) || bytes.Contains(raw, []byte("invocation_id")) {
				t.Fatal("private Model facts projected")
			}
			var snapshot ec.RuntimeSnapshot
			if err := decodeDirectTextJSON(raw, &snapshot); err != nil {
				t.Fatal(err)
			}
			if snapshot.Generation != run.snapshot.Fields().ID || snapshot.IncludedUpdateProgress != 1 || snapshot.ExecutionVersion != 4 {
				t.Fatal("original generation/version lost")
			}
			if status == ec.Succeeded {
				if len(snapshot.Items) != 1 || snapshot.Items[0].Data.Content != "<script>literal</script>\t\n末尾" {
					t.Fatal("text lost or rewritten")
				}
			} else if snapshot.Items == nil || len(snapshot.Items) != 0 {
				t.Fatal("failed output is not a recorded empty snapshot")
			}
			if err := confirmDirectTextRuntime(context.Background(), x, run); err != nil {
				t.Fatal(err)
			}
			if x.reads != 1 || x.writes != 1 {
				t.Fatal("confirmation wrote")
			}
			x.args[7] = []byte(`{}`)
			if err := confirmDirectTextRuntime(context.Background(), x, run); err == nil {
				t.Fatal("partial projection confirmed")
			}
		})
	}
	// These deliberately invalid candidates exercise the producer's own bounds
	// before any INSERT. They are not completed Model responses or PG facts.
	for _, kind := range []string{"invalid-utf8", "parts-total", "encoded-total"} {
		t.Run(kind, func(t *testing.T) {
			run := runtimeCandidate(t, "original")
			switch kind {
			case "invalid-utf8":
				run.terminal.response.Message.Parts[0].Text.Text = string([]byte{0xff})
			case "parts-total":
				run.terminal.response.Message.Parts[0].Text.Text = strings.Repeat("a", ec.MaxRuntimeTextBytes/2)
				run.terminal.response.Message.Parts[1].Text.Text = strings.Repeat("b", ec.MaxRuntimeTextBytes/2+1)
				if run.terminal.response.Validate() != nil {
					t.Fatal("each individual TextPart must be valid for the aggregate-bound control")
				}
			case "encoded-total":
				// JSON's HTML escaping expands each valid '<' to six bytes.
				run.terminal.response.Message.Parts[0].Text.Text = strings.Repeat("<", ec.MaxRuntimeViewBytes/6+1)
				snapshot, err := finalRuntimeSnapshot(run.round, run.terminal)
				if err != nil || len(snapshot.Items[0].Data.Content) > ec.MaxRuntimeTextBytes {
					t.Fatal("encoded-bound control must first accept the valid UTF-8 text total", err)
				}
				if raw, err := runtimeSnapshotBytes(snapshot); err == nil || raw != nil {
					t.Fatal("encoded full snapshot exceeded its bound without rejection")
				}
			}
			x := &runtimeSnapshotSQL{}
			if err := saveDirectTextRuntime(context.Background(), x, run); err == nil || x.writes != 0 {
				t.Fatal("invalid or oversized text reached projection INSERT", err, x.writes)
			}
		})
	}
}

func runtimeReadCandidate(t *testing.T, v *taskReadFixture, mode string) {
	t.Helper()
	base := append([]any(nil), v.store.row...)
	var reason, status *string
	var version *int64
	var finished *time.Time
	projection := []any{[]byte(nil), (*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil), (*int64)(nil), (*time.Time)(nil)}
	if mode != "pending" {
		at := base[7].(time.Time).Add(time.Second)
		done := at.Add(time.Second)
		snap := newTestID[ec.Snapshot](t)
		round := newTestID[ec.Round](t)
		snapText, roundText := snap.String(), round.String()
		state, why := "succeeded", "completed"
		ver := int64(4)
		base[3], base[4], base[6], base[8], base[9] = state, ver, &snapText, &at, &done
		reason, status, version, finished = &why, &state, &ver, &done
		if mode != "not_recorded" {
			started, _ := f.NewInstant(at)
			completed, _ := f.NewInstant(done)
			s := &ec.RuntimeSnapshot{Schema: ec.DurableFinalTextV1, Generation: snap, IncludedUpdateProgress: 1, ExecutionVersion: 4, Items: []ec.RuntimeTextItem{{ID: round, ExecutionID: v.id, Seq: 1, Type: "text", Status: "completed", Revision: 1, StartedAt: started, UpdatedAt: completed, CompletedAt: completed, SourceReference: ec.RuntimeRoundReference{Kind: "execution_round", ID: round}, Data: ec.RuntimeTextData{Content: ""}}}}
			raw, err := runtimeSnapshotBytes(s)
			if err != nil {
				t.Fatal(err)
			}
			digest, p := string(ec.TriggerInputDigest(raw)), v.p.String()
			projection = []any{raw, &digest, &p, &snapText, &roundText, &state, &ver, &done}
		}
	}
	v.store.runtimeRow = append(append(base, reason, status, version, finished), projection...)
}
func TestRuntimeFinalTextReadAvailability(t *testing.T) {
	for _, mode := range []string{"pending", "not_recorded", "available"} {
		t.Run(mode, func(t *testing.T) {
			v := newTaskReadFixture(t)
			runtimeReadCandidate(t, v, mode)
			out, err := v.owner.TaskReader().GetRuntimeView(context.Background(), v.actor, v.p, v.task, v.id)
			if err != nil || string(out.Availability) != mode || out.Execution.ID != v.id || out.ValidateProjection() != nil {
				t.Fatal("wrong availability", out.Availability, err)
			}
			if mode == "available" && (out.Snapshot == nil || len(out.Snapshot.Items) != 1 || out.Snapshot.Items[0].Data.Content != "") {
				t.Fatal("empty successful text lost")
			}
			if mode != "available" && out.Snapshot != nil {
				t.Fatal("missing projection fabricated")
			}
		})
	}
}
func TestRuntimeFinalTextUnknownReturn(t *testing.T) {
	v := newTaskReadFixture(t)
	runtimeReadCandidate(t, v, "available")
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var original f.CommitResult
	v.store.setAfter(func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "execution.task-read" {
			original = f.UnknownResult(newTestID[f.TransactionAttempt](t), cause)
			close(entered)
			<-release
			return original
		}
		return result
	})
	go func() {
		out, err := v.owner.TaskReader().GetRuntimeView(context.Background(), v.actor, v.p, v.task, v.id)
		if out.Snapshot != nil || out.Execution.ID.Validate() == nil {
			done <- invalid()
			return
		}
		done <- err
	}()
	associatedTestReceive(t, entered)
	v.owner.Stop()
	if v.owner.Joined() {
		t.Fatal("held original Store counted as returned")
	}
	select {
	case <-done:
		t.Fatal("returned before Store")
	default:
	}
	close(release)
	err := associatedTestReceive(t, done)
	raw, ok := UnknownAttempt(err)
	if !ok || raw.AttemptID() != original.AttemptID() || !reflect.DeepEqual(raw.Cause().Details(), original.Cause().Details()) {
		t.Fatal("original Unknown lost", err)
	}
	v.store.setAfter(nil)
}
