package realtime

import (
	"bytes"
	"context"
	"encoding/json"

	c "github.com/LunaDeerTech/agenteam/internal/central/realtime/contract"
)

type subscription struct {
	id    c.SubscriptionID
	topic c.Topic
}

type queuedFrame struct {
	raw []byte
	sub *subscription
}

type connection struct {
	hub           *Hub
	ctx           context.Context
	cancel        context.CancelFunc
	wake          chan struct{}
	stop          chan struct{}
	stopped       bool // all fields below are guarded by hub.mu
	subscriptions map[c.SubscriptionID]*subscription
	queue         []queuedFrame
	queuedBytes   int
}

func encodeFrame(message c.ServerMessage, sub *subscription) (queuedFrame, error) {
	if err := message.Validate(); err != nil {
		return queuedFrame{}, err
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(message); err != nil {
		return queuedFrame{}, err
	}
	return queuedFrame{raw: buf.Bytes(), sub: sub}, nil
}

func (conn *connection) stopLocked() {
	if !conn.stopped {
		conn.stopped = true
		conn.queue = nil
		conn.queuedBytes = 0
		close(conn.stop)
	}
}

func (conn *connection) stopConnection() {
	conn.hub.mu.Lock()
	conn.stopLocked()
	conn.hub.mu.Unlock()
	conn.cancel()
}

func (conn *connection) enqueueLocked(frame queuedFrame) bool {
	if conn.stopped {
		return false
	}
	if len(conn.queue) >= conn.hub.options.MaxQueuedNotifications || len(frame.raw) > conn.hub.options.MaxQueuedBytes-conn.queuedBytes {
		// Closing the socket forces a fresh subscription and canonical read.
		// Never silently omit a notification or discard an ack to fit a queue.
		conn.stopLocked()
		return false
	}
	conn.queue = append(conn.queue, frame)
	conn.queuedBytes += len(frame.raw)
	select {
	case conn.wake <- struct{}{}:
	default:
	}
	return true
}

func (conn *connection) subscribe(message c.ClientMessage) bool {
	h := conn.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped || conn.stopped || conn.subscriptions[message.SubscriptionID] != nil || len(conn.subscriptions) >= c.MaxSubscriptions {
		return false
	}
	sub := &subscription{id: message.SubscriptionID, topic: *message.Topic}
	watermark := h.watermarkLocked()
	frame, err := encodeFrame(c.ServerMessage{Type: c.Subscribed, SubscriptionID: sub.id, Topic: &sub.topic, Watermark: &watermark}, sub)
	if err != nil {
		return false
	}
	// Registration and its first ack share the exact publication lock.
	conn.subscriptions[sub.id] = sub
	return conn.enqueueLocked(frame)
}

func (conn *connection) unsubscribe(id c.SubscriptionID) bool {
	h := conn.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped || conn.stopped || conn.subscriptions[id] == nil {
		return false
	}
	delete(conn.subscriptions, id)
	frame, err := encodeFrame(c.ServerMessage{Type: c.Unsubscribed, SubscriptionID: id}, nil)
	return err == nil && conn.enqueueLocked(frame)
}

func (conn *connection) snapshotStart(id c.SubscriptionID) (*subscription, c.Watermark, bool) {
	conn.hub.mu.Lock()
	defer conn.hub.mu.Unlock()
	sub := conn.subscriptions[id]
	return sub, conn.hub.watermarkLocked(), !conn.stopped && !conn.hub.stopped && sub != nil
}

func (conn *connection) enqueue(frame queuedFrame) bool {
	conn.hub.mu.Lock()
	defer conn.hub.mu.Unlock()
	return conn.enqueueLocked(frame)
}

func (conn *connection) takeBatch() ([]queuedFrame, bool) {
	conn.hub.mu.Lock()
	defer conn.hub.mu.Unlock()
	if conn.stopped {
		return nil, false
	}
	count := min(8, len(conn.queue))
	batch := append([]queuedFrame(nil), conn.queue[:count]...)
	for _, frame := range batch {
		conn.queuedBytes -= len(frame.raw)
	}
	copy(conn.queue, conn.queue[count:])
	clear(conn.queue[len(conn.queue)-count:])
	conn.queue = conn.queue[:len(conn.queue)-count]
	if len(conn.queue) > 0 {
		select {
		case conn.wake <- struct{}{}:
		default:
		}
	}
	return batch, true
}

func (conn *connection) current(sub *subscription) bool {
	conn.hub.mu.Lock()
	defer conn.hub.mu.Unlock()
	return !conn.stopped && (sub == nil || conn.subscriptions[sub.id] == sub)
}

func (conn *connection) topics() []c.Topic {
	conn.hub.mu.Lock()
	defer conn.hub.mu.Unlock()
	topics := make([]c.Topic, 0, len(conn.subscriptions))
	for _, sub := range conn.subscriptions {
		topics = append(topics, sub.topic)
	}
	return topics
}
