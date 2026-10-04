package artifact

import (
	"errors"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func newTestID[T any](t *testing.T) foundation.ID[T] {
	t.Helper()
	id, e := foundation.NewID[T]()
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func TestCommandSemanticBindsExplicitSourceReceiptAndDisplay(t *testing.T) {
	project := newTestID[identity.Project](t)
	user := newTestID[identity.User](t)
	actor, _ := identity.NewHuman(user, newTestID[identity.Session](t))
	invocation, _ := ac.NewInvocation(ac.InvocationDetails{Actor: actor, ProjectID: project, AttemptID: newTestID[ac.CallAttempt](t)})
	meta := foundation.CommandMeta{RequestID: newTestID[foundation.Request](t), IdempotencyKey: "same-key"}
	display := ac.Display{Name: "source.txt", Description: "descriptive"}
	reference := oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: project, DocumentID: newTestID[struct{}](t).String(), Revision: 1}
	ref, _ := oc.NewBusinessFileRef(reference)
	base, e := makeRequest(invocation, meta, display, "source", "", 0, "", ref, oc.UploadReceipt{})
	if e != nil {
		t.Fatal(e)
	}
	again, _ := identity.NewHuman(user, newTestID[identity.Session](t))
	retry, _ := ac.NewInvocation(ac.InvocationDetails{Actor: again, ProjectID: project, AttemptID: newTestID[ac.CallAttempt](t)})
	meta.RequestID = newTestID[foundation.Request](t)
	same, e := makeRequest(retry, meta, display, "source", "", 0, "", ref, oc.UploadReceipt{})
	if e != nil || same.digest != base.digest {
		t.Fatal("Session/attempt/trace changed business identity", e)
	}
	variants := []createRequest{}
	for _, field := range []string{"project", "document", "revision"} {
		d := reference
		switch field {
		case "project":
			d.ProjectID = newTestID[identity.Project](t)
		case "document":
			d.DocumentID = newTestID[struct{}](t).String()
		case "revision":
			d.Revision = 9007199254740993
		}
		r, e := oc.NewBusinessFileRef(d)
		if e != nil {
			t.Fatal(e)
		}
		q, e := makeRequest(invocation, meta, display, "source", "", 0, "", r, oc.UploadReceipt{})
		if e != nil {
			t.Fatal(e)
		}
		variants = append(variants, q)
	}
	for _, d := range []ac.Display{{Name: "changed.txt", Description: display.Description}, {Name: display.Name, Description: "changed"}} {
		q, e := makeRequest(invocation, meta, d, "source", "", 0, "", ref, oc.UploadReceipt{})
		if e != nil {
			t.Fatal(e)
		}
		variants = append(variants, q)
	}
	version := foundation.Version(2)
	changed := meta
	changed.ExpectedVersion = &version
	q, e := makeRequest(invocation, changed, display, "source", "", 0, "", ref, oc.UploadReceipt{})
	if e != nil {
		t.Fatal(e)
	}
	variants = append(variants, q)
	for _, q := range variants {
		if q.digest == base.digest || q.digest == digestBytes(nil) {
			t.Fatal("source semantics collapsed")
		}
	}
	owner, _ := oc.NewObjectOwner(oc.Artifact, newTestID[struct{}](t).String(), project.String())
	receipt := oc.ReceiptDetails{ID: newTestID[oc.Receipt](t), UploadID: newTestID[oc.Upload](t), ObjectID: newTestID[oc.StoredObject](t), Owner: owner, CreationCause: newTestID[struct{}](t).String()}
	upload, _ := oc.NewUploadReceipt(receipt)
	original, e := makeRequest(invocation, meta, display, "upload", "", 0, "", oc.BusinessFileRef{}, upload)
	if e != nil {
		t.Fatal(e)
	}
	for _, field := range []string{"receipt", "upload", "object", "owner", "cause"} {
		d := receipt
		switch field {
		case "receipt":
			d.ID = newTestID[oc.Receipt](t)
		case "upload":
			d.UploadID = newTestID[oc.Upload](t)
		case "object":
			d.ObjectID = newTestID[oc.StoredObject](t)
		case "owner":
			d.Owner, _ = oc.NewObjectOwner(oc.Artifact, newTestID[struct{}](t).String(), project.String())
		case "cause":
			d.CreationCause = newTestID[struct{}](t).String()
		}
		r, e := oc.NewUploadReceipt(d)
		if e != nil {
			t.Fatal(e)
		}
		q, e := makeRequest(invocation, meta, display, "upload", "", 0, "", oc.BusinessFileRef{}, r)
		if e != nil || q.digest == original.digest {
			t.Fatal("receipt fields collapsed", field, e)
		}
	}
	inline, e := makeRequest(invocation, meta, display, "content", "text/plain", 3, digestBytes([]byte("one")), oc.BusinessFileRef{}, oc.UploadReceipt{})
	if e != nil {
		t.Fatal(e)
	}
	other, e := makeRequest(invocation, meta, display, "content", "text/plain", 3, digestBytes([]byte("two")), oc.BusinessFileRef{}, oc.UploadReceipt{})
	if e != nil || other.digest == inline.digest {
		t.Fatal("equal-length bytes collapsed", e)
	}
	if d, e := jsonDigest(struct{ SHA foundation.Digest }{SHA: ""}); e == nil || d != "" {
		t.Fatal("marshal error became a usable digest")
	}
	err := matchesCommand(commandRow{originalActor: stableActor(actor), digest: base.digest}, variants[0])
	var f *foundation.Fault
	if !errors.As(err, &f) || f.Code != foundation.IdempotencyKeyReused {
		t.Fatal("different semantic accepted", err)
	}
}
