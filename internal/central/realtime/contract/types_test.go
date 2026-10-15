package contract

import (
	"bytes"
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func topicFixture(t *testing.T) Topic {
	t.Helper()
	p, err := f.NewID[i.Project]()
	if err != nil {
		t.Fatal(err)
	}
	m, err := f.NewID[struct{}]()
	if err != nil {
		t.Fatal(err)
	}
	return Topic{Kind: MeetingTimeline, ProjectID: p, MeetingID: m.String()}
}

func TestRealtimeTopicsRequireExactMeetingOrContributionTuple(t *testing.T) {
	v := topicFixture(t)
	if v.Validate() != nil {
		t.Fatal("timeline rejected")
	}
	c, _ := f.NewID[struct{}]()
	e, _ := f.NewID[i.Execution]()
	v.Kind, v.ContributionID, v.Generation, v.Attempt, v.ExecutionID = MeetingExecution, c.String(), 2, 3, e
	if v.Validate() != nil {
		t.Fatal("complete execution target rejected")
	}
	for _, mutate := range []func(*Topic){
		func(v *Topic) { v.Kind = "execution" },
		func(v *Topic) { v.MeetingID = "" },
		func(v *Topic) { v.ContributionID = "" },
		func(v *Topic) { v.Generation = 0 },
		func(v *Topic) { v.Attempt = 0 },
		func(v *Topic) { v.ExecutionID = i.ExecutionID{} },
		func(v *Topic) { v.Kind = MeetingTimeline },
	} {
		bad := v
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("partial or cross-kind topic accepted")
		}
	}
}

func TestRealtimeClientRejectsDuplicateUnknownOrReplacementFields(t *testing.T) {
	topic := topicFixture(t)
	sub, _ := f.NewID[Subscription]()
	valid, err := json.Marshal(ClientMessage{Type: Subscribe, SubscriptionID: sub, Topic: &topic})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeClientMessage(valid); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{
		append(append([]byte{}, valid[:len(valid)-1]...), []byte(`,"type":"subscribe"}`)...),
		append(append([]byte{}, valid[:len(valid)-1]...), []byte(`,"actor":{"kind":"human"}}`)...),
		append(append([]byte{}, valid...), []byte(`{}`)...),
		[]byte(`null`),
		bytes.Replace(valid, []byte(`"type"`), []byte(`"Type"`), 1),
		bytes.Replace(valid, []byte(`"subscription_id"`), []byte(`"Subscription_ID"`), 1),
		bytes.Replace(valid, []byte(`"kind"`), []byte(`"Kind"`), 1),
		append(append([]byte{}, valid[:len(valid)-1]...), []byte(`,"Type":"subscribe"}`)...),
		bytes.Replace(valid, []byte(`"kind":`), []byte(`"generation":0,"kind":`), 1),
	} {
		if _, err := DecodeClientMessage(raw); err == nil {
			t.Fatal("ambiguous or caller-authorized message accepted")
		}
	}
	if (ClientMessage{Type: ReadSnapshot, SubscriptionID: sub, Topic: &topic}).Validate() == nil {
		t.Fatal("snapshot silently replaced admitted topic")
	}
	for _, kind := range []ClientType{Unsubscribe, ReadSnapshot} {
		raw, _ := json.Marshal(ClientMessage{Type: kind, SubscriptionID: sub})
		if _, err := DecodeClientMessage(raw); err != nil {
			t.Fatal("canonical admitted-topic command rejected", err)
		}
		raw = append(raw[:len(raw)-1], []byte(`,"topic":null}`)...)
		if _, err := DecodeClientMessage(raw); err == nil {
			t.Fatal("null topic field bypassed closed command shape")
		}
	}
}
