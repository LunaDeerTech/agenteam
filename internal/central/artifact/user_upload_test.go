package artifact

import (
	"context"
	"io"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type rejectedUserUploadBody struct{ reads, closes int }

func (b *rejectedUserUploadBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *rejectedUserUploadBody) Close() error             { b.closes++; return nil }

func TestUserUploadEntryRejectsExecutionProvenanceAndClosesBody(t *testing.T) {
	project := newTestID[i.Project](t)
	actor, _ := i.NewHuman(newTestID[i.User](t), newTestID[i.Session](t))
	v, err := ac.NewInvocation(ac.InvocationDetails{Actor: actor, ProjectID: project, ExecutionID: newTestID[i.Execution](t).String(), AttemptID: newTestID[ac.CallAttempt](t)})
	if err != nil {
		t.Fatal(err)
	}
	meta := f.CommandMeta{RequestID: newTestID[f.Request](t), IdempotencyKey: "human-file"}
	input := ac.UserUploadIntent{Display: ac.Display{Name: "file.bin"}, MediaType: "application/octet-stream", ByteSize: 1}
	s := &Service{} // Validation must reject before touching any service port.
	if _, err = s.BeginUserUpload(context.Background(), v, meta, input); err == nil {
		t.Fatal("begin accepted execution provenance")
	}
	if _, err = s.LookupUserUpload(context.Background(), v, meta, input); err == nil {
		t.Fatal("lookup accepted execution provenance")
	}
	if _, err = s.CompleteUserUpload(context.Background(), v, meta, input); err == nil {
		t.Fatal("complete accepted execution provenance")
	}
	if _, err = s.CancelUserUpload(context.Background(), v, meta, input); err == nil {
		t.Fatal("cancel accepted execution provenance")
	}
	body := &rejectedUserUploadBody{}
	if _, err = s.WriteUserUpload(context.Background(), v, meta, input.MediaType, 1, body); err == nil || body.reads != 0 || body.closes != 1 {
		t.Fatal("rejected upload body escaped ownership", err, body.reads, body.closes)
	}
}
