package runtime

import (
	"context"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type backendTaskLookup struct {
	builtin.TaskToolService
	calls int
	key   f.IdempotencyKey
}

func (s *backendTaskLookup) LookupTaskToolTransfer(_ context.Context, _ i.Actor, meta f.CommandMeta, _ wc.ProjectID, _ wc.TaskID, _ wc.TaskTransfer) (wc.TaskTransitionLookup, error) {
	s.calls++
	s.key = meta.IdempotencyKey
	return wc.TaskTransitionLookup{Status: wc.LookupNotObserved}, nil
}

type backendArtifactLookup struct {
	builtin.ArtifactService
	calls      int
	invocation ac.Invocation
	meta       f.CommandMeta
}

func (s *backendArtifactLookup) LookupCreation(_ context.Context, v ac.Invocation, meta f.CommandMeta, _ ac.Display, _, _ string) (ac.CreationLookupResult, error) {
	s.calls++
	s.invocation = v
	s.meta = meta
	return ac.CreationLookupResult{State: ac.CreationNotObserved}, nil
}
func backendInvocation(t *testing.T, b BuiltinBackend) BuiltinInvocation {
	t.Helper()
	h := readFileHandoffFixture(t)
	call := h.binding
	call.RunnerBinding = tc.RunnerBinding{}
	call.Binding = b.Registration().Binding
	v := BuiltinInvocation{Call: call, OperationID: h.record.ID, AttemptID: h.attempt, RequestID: h.request}
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	return v
}
func TestBuiltinBackendsFixedRegistrationAndReadLookup(t *testing.T) {
	ta, _ := builtin.NewTaskAdapter(&backendTaskLookup{})
	aa, _ := builtin.NewArtifactAdapter(&backendArtifactLookup{})
	tasks, err := NewTaskBackends(ta)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := NewArtifactBackends(aa)
	if err != nil {
		t.Fatal(err)
	}
	backends := append(tasks, artifacts...)
	if len(backends) != 5 {
		t.Fatal("wrong builtin set")
	}
	for _, b := range backends {
		reg := b.Registration()
		if err := reg.Validate(context.Background()); err != nil || reg.Class != tc.CoreTool || !reg.Active || reg.Binding.ContractRevision != 1 {
			t.Fatal("invalid fixed registration", err)
		}
		reg.Definition.InputSchema[0] = '!'
		if b.Registration().Validate(context.Background()) != nil {
			t.Fatal("caller mutated stored registration")
		}
		if b.Registration().Definition.Annotations.ReadOnly {
			raw, found, err := b.Lookup(context.Background(), backendInvocation(t, b), []byte(`{}`))
			if err != nil || found || raw != nil {
				t.Fatal("read Lookup repeated domain I/O", err)
			}
		}
		if err := b.RequireBindingInTx(context.Background(), f.NewTx(), struct{}{}); err == nil {
			t.Fatal("test adapter published as a production binding")
		}
	}
}
func TestBuiltinBackendsMutationLookupRetainsOriginalIdentity(t *testing.T) {
	ts := &backendTaskLookup{}
	ta, _ := builtin.NewTaskAdapter(ts)
	tasks, _ := NewTaskBackends(ta)
	b := tasks[1]
	v := backendInvocation(t, b)
	task := newOperationID[wc.Task](t)
	raw := []byte(`{"task_id":"` + task.String() + `","expected_version":"2","target_state":"cancelled"}`)
	_, found, err := b.Lookup(context.Background(), v, raw)
	key, _ := builtin.TaskTransferKey(v.OperationID)
	if err != nil || found || ts.calls != 1 || ts.key != key {
		t.Fatal("Task Lookup lost original identity or resent mutation", err)
	}
	as := &backendArtifactLookup{}
	aa, _ := builtin.NewArtifactAdapter(as)
	artifacts, _ := NewArtifactBackends(aa)
	b = artifacts[0]
	v = backendInvocation(t, b)
	_, found, err = b.Lookup(context.Background(), v, []byte(`{"name":"result.md","media_type":"text/markdown","content":"actual result"}`))
	d := as.invocation.Details()
	artifactKey, _ := builtin.ArtifactCreateKey(v.OperationID.String())
	if err != nil || found || as.calls != 1 || d.ToolCallID != v.Call.Input.PayloadID || d.OperationID != v.OperationID.String() || d.AttemptID.String() != v.AttemptID.String() || d.ExecutionID != v.Call.ExecutionID.String() || as.meta.IdempotencyKey != artifactKey || as.meta.RequestID.String() != string(v.RequestID) {
		t.Fatal("Artifact Lookup lost original physical identity", err)
	}
	v.Call.Binding = tasks[0].Registration().Binding
	if _, _, err = b.Lookup(context.Background(), v, []byte(`{}`)); err == nil || as.calls != 1 {
		t.Fatal("foreign handler reached domain")
	}
}
