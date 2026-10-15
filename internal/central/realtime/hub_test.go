package realtime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/realtime/contract"
)

func topicFixture(t *testing.T) c.Topic {
	t.Helper()
	project, err := f.NewID[i.Project]()
	if err != nil {
		t.Fatal(err)
	}
	meeting, err := f.NewID[struct{}]()
	if err != nil {
		t.Fatal(err)
	}
	return c.Topic{Kind: c.MeetingTimeline, ProjectID: project, MeetingID: meeting.String()}
}

func queuedMessage(t *testing.T, frame queuedFrame) c.ServerMessage {
	t.Helper()
	var message c.ServerMessage
	if json.Unmarshal(frame.raw, &message) != nil || message.Validate() != nil {
		t.Fatal("invalid queued frame")
	}
	return message
}

func TestHubRegistrationAckCannotBeOvertakenByPublication(t *testing.T) {
	hub, err := NewHub(HubOptions{})
	if err != nil {
		t.Fatal(err)
	}
	topic := topicFixture(t)
	conn, err := hub.admit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer hub.release(conn)
	id, _ := f.NewID[c.Subscription]()
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		conn.subscribe(c.ClientMessage{Type: c.Subscribe, SubscriptionID: id, Topic: &topic})
	}()
	go func() {
		defer workers.Done()
		<-start
		hub.InvalidateMeeting(topic.ProjectID, topic.MeetingID)
	}()
	close(start)
	workers.Wait()
	hub.InvalidateMeeting(topic.ProjectID, topic.MeetingID)
	batch, ok := conn.takeBatch()
	if !ok || len(batch) < 2 {
		t.Fatal("subscription or later publication disappeared")
	}
	ack := queuedMessage(t, batch[0])
	if ack.Type != c.Subscribed {
		t.Fatal("publication preceded registration ack")
	}
	for _, frame := range batch[1:] {
		message := queuedMessage(t, frame)
		if message.Type != c.Invalidated || message.Watermark.Sequence <= ack.Watermark.Sequence {
			t.Fatal("ack baseline overtook a queued publication")
		}
	}
}

func TestHubOverflowClosesInsteadOfOmittingMessages(t *testing.T) {
	for _, options := range []HubOptions{{MaxQueuedNotifications: 2}, {MaxQueuedBytes: 1024}} {
		hub, err := NewHub(options)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := hub.admit(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		topic := topicFixture(t)
		id, _ := f.NewID[c.Subscription]()
		if !conn.subscribe(c.ClientMessage{Type: c.Subscribe, SubscriptionID: id, Topic: &topic}) {
			t.Fatal("initial ack did not fit bounded queue")
		}
		for range 8 {
			hub.InvalidateMeeting(topic.ProjectID, topic.MeetingID)
		}
		select {
		case <-conn.stop:
		default:
			t.Fatal("overflow silently retained the connection")
		}
		if _, ok := conn.takeBatch(); ok {
			t.Fatal("overflow allowed another outgoing batch")
		}
		hub.release(conn)
	}
}

func TestHubExecutionInvalidationUsesAdmittedExactTuple(t *testing.T) {
	hub, _ := NewHub(HubOptions{})
	conn, _ := hub.admit(context.Background())
	defer hub.release(conn)
	topic := topicFixture(t)
	contribution, _ := f.NewID[struct{}]()
	execution, _ := f.NewID[i.Execution]()
	topic.Kind, topic.ContributionID, topic.Generation, topic.Attempt, topic.ExecutionID = c.MeetingExecution, contribution.String(), 2, 3, execution
	id, _ := f.NewID[c.Subscription]()
	if !conn.subscribe(c.ClientMessage{Type: c.Subscribe, SubscriptionID: id, Topic: &topic}) {
		t.Fatal("valid exact tuple rejected")
	}
	conn.takeBatch()
	foreign, _ := f.NewID[i.Project]()
	hub.InvalidateExecution(foreign, execution)
	if batch, _ := conn.takeBatch(); len(batch) != 0 {
		t.Fatal("foreign project notification crossed subscription")
	}
	hub.InvalidateExecution(topic.ProjectID, execution)
	batch, _ := conn.takeBatch()
	if len(batch) != 1 || *queuedMessage(t, batch[0]).Topic != topic {
		t.Fatal("execution invalidation invented a generic execution subscription")
	}
}

func TestHubBoundsConnectionAndSubscriptionAdmission(t *testing.T) {
	hub, _ := NewHub(HubOptions{MaxConnections: 1})
	conn, _ := hub.admit(context.Background())
	defer hub.release(conn)
	if _, err := hub.admit(context.Background()); err == nil {
		t.Fatal("connection limit ignored")
	}
	topic := topicFixture(t)
	for range c.MaxSubscriptions {
		id, _ := f.NewID[c.Subscription]()
		if !conn.subscribe(c.ClientMessage{Type: c.Subscribe, SubscriptionID: id, Topic: &topic}) {
			t.Fatal("allowed subscription unexpectedly rejected")
		}
	}
	id, _ := f.NewID[c.Subscription]()
	if conn.subscribe(c.ClientMessage{Type: c.Subscribe, SubscriptionID: id, Topic: &topic}) {
		t.Fatal("subscription limit ignored")
	}
	hub.StopAdmission()
	if _, err := hub.admit(context.Background()); err == nil || hub.Joined() {
		t.Fatal("StopAdmission admitted work or pretended the reader returned")
	}
}
