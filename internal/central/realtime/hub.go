// Package realtime implements bounded, process-local browser notifications.
// The original domain readers remain the only source of authorized state.
package realtime

import (
	"context"
	"math"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/realtime/contract"
)

const maxQueueBytes = c.MaxSnapshotBytes + (64 << 10)

// HubOptions may lower the hard limits for a deployment or controlled test.
// Zero values select the defaults. There are no unbounded queues.
type HubOptions struct {
	MaxConnections         int
	MaxQueuedNotifications int
	MaxQueuedBytes         int
}

type Hub struct {
	mu          sync.Mutex
	options     HubOptions
	epoch       c.EpochID
	sequence    f.Progress
	connections map[*connection]struct{}
	stopped     bool
	done        chan struct{}
	joined      bool
}

// NewHub is pure apart from creating its random epoch. It starts no goroutine
// and accepts no Authority, so producers cannot acquire read capabilities.
func NewHub(options HubOptions) (*Hub, error) {
	if options.MaxConnections == 0 {
		options.MaxConnections = 64
	}
	if options.MaxQueuedNotifications == 0 {
		options.MaxQueuedNotifications = c.MaxQueuedNotifications
	}
	if options.MaxQueuedBytes == 0 {
		options.MaxQueuedBytes = maxQueueBytes
	}
	if options.MaxConnections < 1 || options.MaxConnections > 256 || options.MaxQueuedNotifications < 1 || options.MaxQueuedNotifications > c.MaxQueuedNotifications || options.MaxQueuedBytes < 1 || options.MaxQueuedBytes > maxQueueBytes {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	epoch, err := f.NewID[c.StreamEpoch]()
	if err != nil {
		return nil, err
	}
	return &Hub{options: options, epoch: epoch, connections: make(map[*connection]struct{}), done: make(chan struct{})}, nil
}

func (h *Hub) admit(ctx context.Context) (*connection, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped || len(h.connections) >= h.options.MaxConnections {
		return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	ctx, cancel := context.WithCancel(ctx)
	conn := &connection{hub: h, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), stop: make(chan struct{}), subscriptions: make(map[c.SubscriptionID]*subscription)}
	h.connections[conn] = struct{}{}
	return conn, nil
}

func (h *Hub) watermarkLocked() c.Watermark {
	return c.Watermark{Epoch: h.epoch, Sequence: h.sequence}
}

func (h *Hub) InvalidateMeeting(project i.ProjectID, meeting string) {
	topic := c.Topic{Kind: c.MeetingTimeline, ProjectID: project, MeetingID: meeting}
	if topic.Validate() != nil {
		return
	}
	h.invalidate(func(t c.Topic) bool { return t.ProjectID == project && t.MeetingID == meeting })
}

func (h *Hub) InvalidateExecution(project i.ProjectID, execution i.ExecutionID) {
	if project.Validate() != nil || execution.Validate() != nil {
		return
	}
	h.invalidate(func(t c.Topic) bool {
		return t.Kind == c.MeetingExecution && t.ProjectID == project && t.ExecutionID == execution
	})
}

// Publication holds only the bounded queue lock. It does no authorization,
// database read, network write, callback, or wait on a consumer. The number of
// connections, subscriptions and bytes appended per publication is capped.
func (h *Hub) invalidate(matches func(c.Topic) bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return
	}
	if h.sequence == f.Progress(math.MaxInt64) {
		// A transport cursor cannot wrap and masquerade as an older update.
		for conn := range h.connections {
			conn.stopLocked()
		}
		return
	}
	h.sequence++
	watermark := h.watermarkLocked()
	for conn := range h.connections {
		if conn.stopped {
			continue
		}
		for _, sub := range conn.subscriptions {
			if matches(sub.topic) {
				frame, err := encodeFrame(c.ServerMessage{Type: c.Invalidated, SubscriptionID: sub.id, Topic: &sub.topic, Watermark: &watermark}, sub)
				if err != nil || !conn.enqueueLocked(frame) {
					conn.stopLocked()
					break
				}
			}
		}
	}
}

var _ c.Publisher = (*Hub)(nil)
