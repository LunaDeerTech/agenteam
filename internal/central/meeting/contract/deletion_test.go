package contract

import (
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestDeletionReceiptSeparatesIntentFromCompletedPurge(t *testing.T) {
	r := DeleteRequest{ProjectID: fresh[i.Project](t), MeetingID: fresh[Meeting](t), ExpectedVersion: 7, Meta: f.CommandMeta{RequestID: fresh[f.Request](t), IdempotencyKey: "delete-original-meeting"}}
	if r.Validate() != nil {
		t.Fatal("complete Owner intent rejected")
	}
	before := time.Now().UTC().Truncate(time.Microsecond)
	v := DeleteReceipt{DeletionID: fresh[Deletion](t), MeetingID: r.MeetingID, AcceptedVersion: 8, Phase: DeletionCancelling, ErrorCode: f.CommitUnknown, RequestedAt: before}
	if v.Validate() != nil {
		t.Fatal("unsettled original intent was hidden")
	}
	v.Phase = DeletionCompleted
	if v.Validate() == nil {
		t.Fatal("unknown cancellation became a completed purge")
	}
	after := before.Add(time.Second)
	v.DeletedAt, v.ErrorCode = &after, ""
	if v.Validate() != nil {
		t.Fatal("durable completed receipt rejected")
	}
	for _, change := range []func(*DeleteReceipt){
		func(v *DeleteReceipt) { v.Phase = "archive" },
		func(v *DeleteReceipt) { v.Phase = DeletionCancelling },
		func(v *DeleteReceipt) { v.ErrorCode = f.ResourceBusy },
		func(v *DeleteReceipt) { invalid := before.Add(-time.Second); v.DeletedAt = &invalid },
	} {
		bad := v
		change(&bad)
		if bad.Validate() == nil {
			t.Fatal("ambiguous lifecycle receipt accepted")
		}
	}
	wrong := f.Version(9)
	r.Meta.ExpectedVersion = &wrong
	if r.Validate() == nil {
		t.Fatal("two independent confirmation versions accepted")
	}
}
