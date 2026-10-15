package contract

import (
	"bytes"
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestRealtimeSnapshotBindsActualCanonicalContentSeparatelyFromWatermark(t *testing.T) {
	topic := topicFixture(t)
	raw := []byte(`{"version":"2","items":[]}`)
	snapshot, err := NewSnapshot(topic, raw)
	if err != nil || snapshot.Validate() != nil {
		t.Fatal("public snapshot rejected", err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal("public snapshot cannot be sent", err)
	}
	var decoded Snapshot
	if json.Unmarshal(encoded, &decoded) != nil || decoded.Validate() != nil {
		t.Fatal("snapshot wire lost canonical digest or topic")
	}
	raw[0] = '['
	if snapshot.Validate() != nil {
		t.Fatal("snapshot aliases caller input")
	}
	bad := snapshot
	bad.Content = []byte(`{"items":[],"version":"3"}`)
	if bad.Validate() == nil {
		t.Fatal("old digest accepted new content")
	}
	bad = snapshot
	bad.Schema = ExecutionSchema
	if bad.Validate() == nil {
		t.Fatal("wrong domain schema accepted")
	}
	for _, raw := range [][]byte{[]byte(`[]`), []byte(`{"x":1,"x":2}`), bytes.Repeat([]byte(" "), MaxSnapshotBytes+1)} {
		if _, err := NewSnapshot(topic, raw); err == nil {
			t.Fatal("unbounded or ambiguous public snapshot accepted")
		}
	}
	sub, _ := f.NewID[Subscription]()
	epoch, _ := f.NewID[StreamEpoch]()
	watermark := Watermark{Epoch: epoch, Sequence: 0}
	message := ServerMessage{Type: SnapshotRead, SubscriptionID: sub, Topic: &topic, Watermark: &watermark, Snapshot: &snapshot}
	if message.Validate() != nil {
		t.Fatal("initial watermark incorrectly required durable progress")
	}
	changed := topicFixture(t)
	message.Topic = &changed
	if message.Validate() == nil {
		t.Fatal("snapshot escaped admitted topic")
	}
}
