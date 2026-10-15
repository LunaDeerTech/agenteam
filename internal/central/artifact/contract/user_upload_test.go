package contract

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestUserUploadIntentPreservesObjectBoundsAndCanonicalMedia(t *testing.T) {
	base := UserUploadIntent{Display: Display{Name: "file.bin"}, MediaType: "application/octet-stream"}
	for _, size := range []f.Progress{0, f.Progress(oc.MaxObjectSize)} {
		v := base
		v.ByteSize = size
		if v.Validate() != nil {
			t.Fatal("existing Object size boundary rejected", size)
		}
	}
	for _, change := range []func(*UserUploadIntent){
		func(v *UserUploadIntent) { v.ByteSize = -1 },
		func(v *UserUploadIntent) { v.ByteSize = f.Progress(oc.MaxObjectSize + 1) },
		func(v *UserUploadIntent) { v.MediaType = "APPLICATION/OCTET-STREAM" },
		func(v *UserUploadIntent) { v.Display.Name = "../file.bin" },
		func(v *UserUploadIntent) { d := f.Digest("invalid"); v.SHA256 = &d },
	} {
		v := base
		change(&v)
		if v.Validate() == nil {
			t.Fatal("invalid upload intent accepted")
		}
	}
	digest := testMetadata(t).Details().Object.SHA256
	base.SHA256 = &digest
	if base.Validate() != nil {
		t.Fatal("optional valid expected digest rejected")
	}
}

func TestUserUploadObservationOnlyPublishesHumanUserUpload(t *testing.T) {
	d := testMetadata(t).Details()
	d.Kind = UserUpload
	m, err := NewMetadata(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []UserUploadState{UserUploadNotObserved, UserUploadPending, UserUploadUploaded, UserUploadCancelRequested, UserUploadCancelled} {
		if (UserUploadResult{State: state}).Validate() != nil {
			t.Fatal("known observation rejected", state)
		}
		if (UserUploadResult{State: state, Artifact: &m}).Validate() == nil {
			t.Fatal("incomplete upload manufactured a canonical reference", state)
		}
	}
	if (UserUploadResult{State: UserUploadCommitted, Artifact: &m}).Validate() != nil {
		t.Fatal("published user file rejected")
	}
	for _, out := range []UserUploadResult{{}, {State: "unknown"}, {State: UserUploadCommitted}} {
		if out.Validate() == nil {
			t.Fatal("unknown state or missing committed artifact accepted")
		}
	}
	for _, change := range []func(*MetadataDetails){
		func(d *MetadataDetails) { d.Kind = Generated },
		func(d *MetadataDetails) { d.ExecutionID = testID[i.Execution](t).String() },
		func(d *MetadataDetails) {
			d.CreatedByUserID = ""
			d.CreatedByAgentID = testID[i.Agent](t).String()
			d.ExecutionID = testID[i.Execution](t).String()
		},
	} {
		changed := d
		change(&changed)
		other, err := NewMetadata(changed)
		if err != nil {
			t.Fatal("invalid fixture", err)
		}
		if (UserUploadResult{State: UserUploadCommitted, Artifact: &other}).Validate() == nil {
			t.Fatal("generated/execution provenance passed as Human file upload")
		}
	}
}
