package execution

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestTaskApprovalWaitingProjectionIsCompleteAndCurrent(t *testing.T) {
	started, _ := f.NewInstant(time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
	at := started.Time().Add(time.Second)
	reference, reason := newTestID[struct{}](t).String(), "approval"
	for _, mode := range []string{"valid", "missing-reason", "missing-reference", "missing-time", "old-time", "wrong-reason", "invalid-reference", "terminal-residue"} {
		t.Run(mode, func(t *testing.T) {
			out := c.TaskExecution{Status: c.Waiting, StartedAt: &started}
			rr, ref, when := reason, reference, at
			rp, fp, wp := &rr, &ref, &when
			switch mode {
			case "missing-reason":
				rp = nil
			case "missing-reference":
				fp = nil
			case "missing-time":
				wp = nil
			case "old-time":
				when = started.Time().Add(-time.Microsecond)
			case "wrong-reason":
				rr = "other"
			case "invalid-reference":
				ref = "wrong"
			case "terminal-residue":
				out.Status = c.Cancelled
			}
			err := projectTaskWaiting(&out, rp, fp, wp)
			if (err == nil) != (mode == "valid") {
				t.Fatal("waiting projection admitted incomplete/stale tuple", err)
			}
			if err == nil && (out.WaitingReference == nil || *out.WaitingReference != reference || out.WaitingAt.Time() != at) {
				t.Fatal("lost canonical wait")
			}
		})
	}
	out := c.TaskExecution{Status: c.Cancelled}
	if err := projectTaskWaiting(&out, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestBeforeStartCancellationLifecycleHasNoInventedIdentity(t *testing.T) {
	v := c.DirectTextLifecycle{ExecutionID: newTestID[i.Execution](t), AgentID: newTestID[i.Agent](t), Status: c.Cancelled, Reason: "cancelled_before_start"}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"start_id":null`) || !strings.Contains(string(raw), `"transcript_through":"0"`) {
		t.Fatal("invented before-start identity", string(raw))
	}
	var decoded c.DirectTextLifecycle
	if err = json.Unmarshal(raw, &decoded); err != nil || decoded != v {
		t.Fatal("before-start lifecycle lost closed shape", err)
	}
	for _, changed := range []string{strings.Replace(string(raw), `"status":"cancelled"`, `"status":"running"`, 1), strings.Replace(string(raw), `"transcript_through":"0"`, `"transcript_through":"1"`, 1), strings.Replace(string(raw), `"start_id":null`, `"start_id":"`+newTestID[c.DirectTextStart](t).String()+`"`, 1)} {
		if json.Unmarshal([]byte(changed), &decoded) == nil {
			t.Fatal("accepted forged partial before-start lifecycle")
		}
	}
}

func TestStartedLifecycleRetainsOriginalRequiredIdentity(t *testing.T) {
	v := c.DirectTextLifecycle{ExecutionID: newTestID[i.Execution](t), AgentID: newTestID[i.Agent](t), StartID: newTestID[c.DirectTextStart](t), SnapshotID: newTestID[c.Snapshot](t), RoundID: newTestID[c.Round](t), CallID: newTestID[mc.Call](t), Status: c.Running, Reason: "started", TranscriptThrough: 1}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var decoded c.DirectTextLifecycle
	if err = json.Unmarshal(raw, &decoded); err != nil || decoded != v {
		t.Fatal("normal lifecycle shape changed", err)
	}
	changed := strings.Replace(string(raw), `"start_id":"`+v.StartID.String()+`"`, `"start_id":null`, 1)
	if json.Unmarshal([]byte(changed), &decoded) == nil {
		t.Fatal("started event lost required start identity")
	}
}
