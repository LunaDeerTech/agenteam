package realtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/realtime/contract"
	"github.com/gorilla/websocket"
)

func TestOverflowClosesSocketButJoinWaitsOriginalWriterCallback(t *testing.T) {
	fixture := newGatewayFixture(t, false, HubOptions{MaxQueuedNotifications: 2}, HTTPOptions{}, nil)
	socket := fixture.dial(t)
	subscribeSocket(t, socket, fixture.domain.topic)
	entered, release := make(chan struct{}), make(chan struct{})
	var enteredOnce, releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	fixture.domain.mu.Lock()
	fixture.domain.check = func(context.Context) {
		enteredOnce.Do(func() { close(entered) })
		<-release // deliberately models a callback still unwinding after cancel
	}
	fixture.domain.mu.Unlock()
	fixture.hub.InvalidateMeeting(fixture.domain.topic.ProjectID, fixture.domain.topic.MeetingID)
	awaitSignal(t, entered, "outgoing current-authority check")
	published := make(chan struct{})
	go func() {
		for range 4 {
			fixture.hub.InvalidateMeeting(fixture.domain.topic.ProjectID, fixture.domain.topic.MeetingID)
		}
		close(published)
	}()
	awaitSignal(t, published, "bounded publisher while writer is blocked")
	requireClosed(t, socket)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := fixture.hub.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) || fixture.hub.Joined() {
		t.Fatal("socket close falsely proved original writer callback joined", err)
	}
	releaseOnce.Do(func() { close(release) })
	awaitSignal(t, fixture.returned, "original blocked callback and writer return")
	awaitSignal(t, fixture.hub.Done(), "Hub joined")
	if !fixture.hub.Joined() {
		t.Fatal("actual reader/writer exit was not observed")
	}
}

func TestStopWaitsOriginalSnapshotReaderAndRejectsFurtherAdmission(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	fixture := newGatewayFixture(t, false, HubOptions{}, HTTPOptions{}, func(fixture *gatewayFixture) {
		fixture.domain.read = func(context.Context) (c.Snapshot, error) {
			close(entered)
			<-release
			return c.NewSnapshot(fixture.domain.topic, []byte(`{"text":"old-read"}`))
		}
	})
	socket := fixture.dial(t)
	sub := subscribeSocket(t, socket, fixture.domain.topic)
	sendClient(t, socket, c.ClientMessage{Type: c.ReadSnapshot, SubscriptionID: sub})
	awaitSignal(t, entered, "original snapshot callback")
	fixture.hub.StopAdmission()
	requireClosed(t, socket)
	if fixture.hub.Joined() {
		t.Fatal("cancel was mistaken for original reader completion")
	}
	if conn, err := fixture.hub.admit(context.Background()); err == nil || conn != nil {
		t.Fatal("stopped Hub admitted another connection")
	}
	once.Do(func() { close(release) })
	awaitSignal(t, fixture.returned, "original reader return")
	awaitSignal(t, fixture.hub.Done(), "Hub joined")
}

func TestStopAccountsForAuthenticationBeforeSocketUpgrade(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	fixture := newGatewayFixture(t, false, HubOptions{}, HTTPOptions{}, func(fixture *gatewayFixture) {
		fixture.accounts.lookupHook = func(context.Context) {
			close(entered)
			<-release
		}
	})
	dialed := make(chan struct{})
	var dialErr error
	go func() {
		defer close(dialed)
		socket, response, err := websocket.DefaultDialer.Dial("ws"+fixture.server.URL[len("http"):]+Path, fixture.headers())
		dialErr = err
		if socket != nil {
			_ = socket.Close()
		}
		if response != nil {
			_ = response.Body.Close()
		}
	}()
	awaitSignal(t, entered, "original Account authentication")
	fixture.hub.StopAdmission()
	if fixture.hub.Joined() {
		t.Fatal("pre-upgrade authentication escaped lifecycle accounting")
	}
	once.Do(func() { close(release) })
	awaitSignal(t, dialed, "rejected original handshake")
	awaitSignal(t, fixture.returned, "authentication handler return")
	awaitSignal(t, fixture.hub.Done(), "all original work joined")
	if dialErr == nil {
		t.Fatal("cancelled authentication upgraded a socket")
	}
}
