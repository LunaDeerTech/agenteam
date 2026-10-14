package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type artifactServiceControl struct {
	ArtifactService
	create func(context.Context, ac.Invocation, f.CommandMeta, ac.Display, string, string) (ac.Metadata, error)
	lookup func(context.Context, ac.Invocation, f.CommandMeta, ac.Display, string, string) (ac.CreationLookupResult, error)
	read   func(context.Context, ac.Invocation, ac.ArtifactRef, ac.PreviewRequest) (ac.ReadResult, error)
}

func (s *artifactServiceControl) CreateFromContent(ctx context.Context, v ac.Invocation, m f.CommandMeta, d ac.Display, media, content string) (ac.Metadata, error) {
	return s.create(ctx, v, m, d, media, content)
}
func (s *artifactServiceControl) LookupCreation(ctx context.Context, v ac.Invocation, m f.CommandMeta, d ac.Display, media, content string) (ac.CreationLookupResult, error) {
	return s.lookup(ctx, v, m, d, media, content)
}
func (s *artifactServiceControl) ReadArtifact(ctx context.Context, v ac.Invocation, r ac.ArtifactRef, p ac.PreviewRequest) (ac.ReadResult, error) {
	return s.read(ctx, v, r, p)
}

func artifactAdapterFixture(t *testing.T) (ac.Invocation, f.CommandMeta, ac.Metadata) {
	t.Helper()
	project, execution := installTestID[i.Project](t), installTestID[i.Execution](t)
	actor, _ := i.NewAgentRun(project, installTestID[i.Agent](t), execution)
	v, err := ac.NewInvocation(ac.InvocationDetails{Actor: actor, ProjectID: project, ExecutionID: execution.String(), OperationID: installTestID[struct{}](t).String(), ToolID: installTestID[struct{}](t).String(), ToolCallID: installTestID[struct{}](t).String(), AttemptID: installTestID[ac.CallAttempt](t)})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ArtifactCreateKey(v.Details().OperationID)
	command := f.CommandMeta{RequestID: installTestID[f.Request](t), IdempotencyKey: key}
	scope, _ := i.InProject(project)
	now, _ := f.NewInstant(time.Now().UTC())
	sha, _ := f.ParseDigest("sha256:" + strings.Repeat("a", 64))
	meta, err := ac.NewMetadata(ac.MetadataDetails{Reference: ac.ArtifactRef{ProjectID: project, ArtifactID: installTestID[ac.ArtifactEntity](t), FileID: installTestID[ac.File](t)}, Kind: ac.Generated, Name: "result.md", Object: oc.ObjectMeta{ID: installTestID[oc.StoredObject](t), Scope: scope, MediaType: "text/markdown", ByteSize: 5, SHA256: sha, State: oc.Available, Version: 1, CreatedAt: now}, ExecutionID: execution.String(), OperationID: v.Details().OperationID, CreatedByAgentID: actor.Details().AgentID, CreatedAt: now, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	return v, command, meta
}

func TestArtifactArgumentsClosedAndBounded(t *testing.T) {
	for _, definition := range ArtifactDefinitions() {
		if _, err := tc.CanonicalDefinition(context.Background(), definition); err != nil {
			t.Fatal(err)
		}
	}
	valid := `{"name":"result.md","media_type":"text/markdown","content":"hello"}`
	if err := ValidateArtifactArguments(context.Background(), ArtifactCreateName, []byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`null`, `{}`, `{"name":"a","name":"b","media_type":"text/plain","content":"x"}`, `{"name":"a","media_type":"text/plain","content":"x","operation_id":"forged"}`, `{"name":"a","media_type":"text/plain","content":"\ud800"}`, `{"name":"a","media_type":"text/plain","content":null}`, `{"name":"a/b","media_type":"text/plain","content":"x"}`, valid + ` {}`} {
		if ValidateArtifactArguments(context.Background(), ArtifactCreateName, []byte(raw)) == nil {
			t.Fatal("invalid creation accepted", raw)
		}
	}
	for _, raw := range []string{`{"limit":0}`, `{"limit":201}`, `{"cursor":null}`, `{"Execution_id":"ignored"}`, `{"limit":2,"limit":3}`} {
		if ValidateArtifactArguments(context.Background(), ArtifactListName, []byte(raw)) == nil {
			t.Fatal("invalid list accepted", raw)
		}
	}
}

func TestArtifactAdapterPublishesOriginalProvenanceAndProjectsBusinessReference(t *testing.T) {
	v, command, meta := artifactAdapterFixture(t)
	calls := 0
	service := &artifactServiceControl{create: func(_ context.Context, got ac.Invocation, m f.CommandMeta, d ac.Display, media, content string) (ac.Metadata, error) {
		calls++
		if got.Details().OperationID != v.Details().OperationID || m.IdempotencyKey != command.IdempotencyKey || d.Name != "result.md" || media != "text/markdown" || content != "hello" {
			t.Fatal("original domain call changed")
		}
		return meta, nil
	}}
	adapter, _ := NewArtifactAdapter(service)
	raw := json.RawMessage(`{"name":"result.md","media_type":"text/markdown","content":"hello"}`)
	out, err := adapter.Execute(context.Background(), v, command, ArtifactCreateName, raw)
	if err != nil || !json.Valid(out) || !strings.Contains(string(out), v.Details().ExecutionID) || strings.Contains(string(out), meta.Details().Object.ID.String()) || strings.Contains(string(out), "hello") || calls != 1 {
		t.Fatal("invalid safe publication projection", err, calls)
	}
	bad := command
	bad.IdempotencyKey = "new-key"
	if _, err = adapter.Execute(context.Background(), v, bad, ArtifactCreateName, raw); err == nil || calls != 1 {
		t.Fatal("changed key invoked service")
	}
	if adapter.RequireBindingInTx(context.Background(), f.NewTx(), service) == nil {
		t.Fatal("test service claimed production binding")
	}
	unknown := f.NewFault(f.CommitUnknown, f.Unknown)
	service.create = func(context.Context, ac.Invocation, f.CommandMeta, ac.Display, string, string) (ac.Metadata, error) {
		calls++
		return ac.Metadata{}, unknown
	}
	if _, err = adapter.Execute(context.Background(), v, command, ArtifactCreateName, raw); err != unknown || calls != 2 {
		t.Fatal("unknown result retried or changed", err, calls)
	}
}

func TestArtifactAdapterLookupNeverRestartsPendingCreation(t *testing.T) {
	v, command, meta := artifactAdapterFixture(t)
	for _, state := range []ac.CreationState{ac.CreationNotObserved, ac.CreationCommitted, ac.CreationPending, ac.CreationCancelled, ac.CreationCancelRequested} {
		service := &artifactServiceControl{create: func(context.Context, ac.Invocation, f.CommandMeta, ac.Display, string, string) (ac.Metadata, error) {
			t.Fatal("lookup dispatched creation")
			return ac.Metadata{}, nil
		}, lookup: func(context.Context, ac.Invocation, f.CommandMeta, ac.Display, string, string) (ac.CreationLookupResult, error) {
			r := ac.CreationLookupResult{State: state}
			if state == ac.CreationCommitted {
				r.Artifact = &meta
			}
			return r, nil
		}}
		adapter, _ := NewArtifactAdapter(service)
		out, found, err := adapter.LookupCreate(context.Background(), v, command, []byte(`{"name":"result.md","media_type":"text/markdown","content":"hello"}`))
		if state == ac.CreationCommitted {
			if err != nil || !found || len(out) == 0 {
				t.Fatal("committed lookup lost receipt", err)
			}
		} else if state == ac.CreationNotObserved {
			if err != nil || found || out != nil {
				t.Fatal("absent lookup changed", err)
			}
		} else if err == nil || found || out != nil {
			t.Fatal("unsettled lookup could redispatch")
		}
	}
}

func TestArtifactAdapterJoinsCancellationAndRejectsCrossProjectRead(t *testing.T) {
	v, command, meta := artifactAdapterFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	service := &artifactServiceControl{create: func(ctx context.Context, _ ac.Invocation, _ f.CommandMeta, _ ac.Display, _, _ string) (ac.Metadata, error) {
		close(entered)
		<-ctx.Done()
		<-release
		return ac.Metadata{}, ctx.Err()
	}}
	adapter, _ := NewArtifactAdapter(service)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := adapter.Execute(ctx, v, command, ArtifactCreateName, []byte(`{"name":"result.md","media_type":"text/markdown","content":"hello"}`))
		done <- err
	}()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("adapter returned before domain call joined")
	default:
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("cancel outcome changed", err)
	}
	ref := meta.Details().Reference
	ref.ProjectID = installTestID[i.Project](t)
	raw, _ := json.Marshal(map[string]any{"artifact_ref": ref})
	service.read = func(context.Context, ac.Invocation, ac.ArtifactRef, ac.PreviewRequest) (ac.ReadResult, error) {
		t.Fatal("cross project reached domain")
		return ac.ReadResult{}, nil
	}
	if _, err := adapter.Execute(context.Background(), v, command, ArtifactReadName, raw); err == nil {
		t.Fatal("cross project read accepted")
	}
}
