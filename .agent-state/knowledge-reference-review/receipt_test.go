package review

import (
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// Public identity construction is not a private proof or a receipt returned by
// PublishVerifiedInTx. This test covers only the real contract's safe boundary.
func TestPublicReceiptProjectionBoundary(t *testing.T) {
	const one = "01970000-0000-7000-8000-000000000001"
	const two = "01970000-0000-7000-8000-000000000002"
	r, e := f.ParseID[oc.Receipt](one)
	if e != nil {
		t.Fatal(e)
	}
	u, e := f.ParseID[oc.Upload](two)
	if e != nil {
		t.Fatal(e)
	}
	o, e := f.ParseID[oc.StoredObject](one)
	if e != nil {
		t.Fatal(e)
	}
	owner, e := oc.NewObjectOwner(oc.Knowledge, two, one)
	if e != nil {
		t.Fatal(e)
	}
	d := oc.ReceiptDetails{ID: r, UploadID: u, ObjectID: o, Owner: owner, CreationCause: two}
	valid, e := oc.NewUploadReceipt(d)
	if e != nil || valid.Validate() != nil {
		t.Fatal("valid public identity rejected", e)
	}
	got := valid.Details()
	if got.ID != r || got.UploadID != u || got.ObjectID != o || !got.Owner.Equal(owner) || got.CreationCause != two {
		t.Fatal("projection changed")
	}
	for _, name := range []string{"receipt", "upload", "object", "owner", "cause"} {
		t.Run(name, func(t *testing.T) {
			bad := d
			switch name {
			case "receipt":
				bad.ID = oc.ReceiptID{}
			case "upload":
				bad.UploadID = oc.UploadID{}
			case "object":
				bad.ObjectID = oc.ObjectID{}
			case "owner":
				bad.Owner = oc.ObjectOwner{}
			case "cause":
				bad.CreationCause = ""
			}
			v, e := oc.NewUploadReceipt(bad)
			if e == nil || v.Validate() == nil {
				t.Fatal("invalid projection accepted")
			}
		})
	}
	if (oc.UploadReceipt{}).Validate() == nil {
		t.Fatal("empty public API result became a receipt")
	}
	wire, e := json.Marshal(valid)
	if e != nil || string(wire) != `"upload_receipt"` {
		t.Fatal("opaque wire changed")
	}
	var decoded oc.UploadReceipt
	if json.Unmarshal(wire, &decoded) == nil || decoded.Validate() == nil {
		t.Fatal("opaque wire fabricated authority")
	}
}
