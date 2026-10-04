package contract

import (
	"bytes"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestOutboxRequeueTypedMetadataAndActorBoundary(t *testing.T) {
	f := OutboxRequeueFields{DeliveryID: contentID, EventID: contentOtherID, HandlerID: "fixture.handler", FromState: "dead_letter", RedriveCycle: 9223372036854775807, Reason: OperatorRetry}
	m, err := OutboxRequeueMetadata(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(m.JSON(), []byte(`"redrive_cycle":"9223372036854775807"`)) {
		t.Fatal("cycle lost canonical precision")
	}
	if _, err = DecodeMetadata(OutboxDeliveryRequeue, m.JSON()); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{`"handler_id":"fixture.handler"`, `"handler_id":"private text"`}, {`"from_state":"dead_letter"`, `"from_state":"succeeded"`}, {`"redrive_cycle":"9223372036854775807"`, `"redrive_cycle":1`}, {`"redrive_cycle":"9223372036854775807"`, `"redrive_cycle":"01"`}, {`"reason_code":"operator_retry"`, `"reason_code":"private-canary"`}, {`"reason_code":"operator_retry"`, `"reason_code":"operator_retry","name":"private-canary"`}} {
		if _, err = DecodeMetadata(OutboxDeliveryRequeue, bytes.Replace(m.JSON(), []byte(pair[0]), []byte(pair[1]), 1)); err == nil {
			t.Fatal("open metadata boundary")
		}
	}
	user, _ := foundation.ParseID[identity.User](contentID)
	session, _ := foundation.ParseID[identity.Session](contentOtherID)
	human, _ := identity.NewHuman(user, session)
	resource, _ := NewResource(OutboxDeliveryResource, contentID)
	entry := EntryFields{Scope: identity.SystemScope(), Actor: human, Action: OutboxDeliveryRequeue, Outcome: Success, Resource: resource, Metadata: m}
	if _, err = NewEntry(entry); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []Outcome{Denied, Failed, Unknown} {
		entry.Outcome = outcome
		if _, err = NewEntry(entry); err == nil {
			t.Fatal("non-success requeue")
		}
	}
	entry.Outcome = Success
	role, _ := identity.RegisterService(identity.OutboxDelivery)
	entry.Actor, _ = role.Actor(contentID, identity.SystemScope())
	if _, err = NewEntry(entry); err == nil {
		t.Fatal("delivery role obtained Human audit privilege")
	}
	entry.Actor = human
	entry.Resource, _ = NewResource(OutboxDeliveryResource, contentOtherID)
	if _, err = NewEntry(entry); err == nil {
		t.Fatal("metadata delivery mismatch")
	}
	if ProducerFor(OutboxDeliveryRequeue) != OutboxProducer {
		t.Fatal("producer registry drift")
	}
}
