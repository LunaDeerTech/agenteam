package realtime

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// StopAdmission rejects HTTP admission and subscription changes, cancels the
// original reads and wakes each writer to close its owned socket. It does not
// claim completion of a reader or callback that has not actually returned.
func (h *Hub) StopAdmission() {
	h.mu.Lock()
	h.stopped = true
	cancels := make([]context.CancelFunc, 0, len(h.connections))
	for conn := range h.connections {
		conn.stopLocked()
		cancels = append(cancels, conn.cancel)
	}
	h.joinLocked()
	h.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (h *Hub) joinLocked() {
	if h.stopped && len(h.connections) == 0 && !h.joined {
		h.joined = true
		close(h.done)
	}
}

// release is called only after the handler's reader and writer have returned.
// It also accounts for admitted authentication/upgrade calls before a socket
// exists, so shutdown cannot bypass their original completion.
func (h *Hub) release(conn *connection) {
	conn.stopConnection()
	h.mu.Lock()
	delete(h.connections, conn)
	h.joinLocked()
	h.mu.Unlock()
}

func (h *Hub) Stop(ctx context.Context) error {
	if ctx == nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	h.StopAdmission()
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hub) Joined() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.joined
}

func (h *Hub) Done() <-chan struct{} { return h.done }
