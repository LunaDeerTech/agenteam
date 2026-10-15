package artifacthttp

import (
	"context"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestUserUploadHTTPOnlyCommittedExactHumanMetadata(t *testing.T) {
	v, _ := ac.NewInvocation(ac.InvocationDetails{Actor: testActor(), ProjectID: testReference().ProjectID, AttemptID: testID[ac.CallAttempt](55)})
	in := ac.UserUploadIntent{Display: ac.Display{Name: "file.bin"}, MediaType: "application/octet-stream", ByteSize: 4}
	m := uploadMetadata(t)
	for _, state := range []ac.UserUploadState{ac.UserUploadPending, ac.UserUploadUploaded, ac.UserUploadNotObserved, ac.UserUploadCancelRequested, ac.UserUploadCancelled} {
		raw, err := encodeUserUpload(context.Background(), v, &in, ac.UserUploadResult{State: state})
		if err != nil || !strings.Contains(string(raw), `"artifact":null`) || strings.Contains(string(raw), "artifact_ref") {
			t.Fatal("incomplete upload is canonical", state, err)
		}
	}
	for _, change := range []func(*ac.MetadataDetails){
		func(d *ac.MetadataDetails) { d.Kind = ac.Generated },
		func(d *ac.MetadataDetails) { d.Name = "other.bin" },
		func(d *ac.MetadataDetails) { d.Object.ByteSize++ },
		func(d *ac.MetadataDetails) { d.CreatedByUserID = testID[id.User](77).String() },
		func(d *ac.MetadataDetails) { d.ExecutionID = testID[id.Execution](78).String() },
	} {
		d := m.Details()
		change(&d)
		other, err := ac.NewMetadata(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = projectUserUploadMetadata(v, &in, other); err == nil {
			t.Fatal("inconsistent upload response accepted")
		}
	}
	if _, err := projectUserUploadMetadata(v, &in, m); err != nil {
		t.Fatal("real upload projection rejected", err)
	}
}
