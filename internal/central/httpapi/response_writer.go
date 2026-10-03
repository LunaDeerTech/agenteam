package httpapi

import (
	"bufio"
	"net"
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// Like net/http.ResponseWriter, a tracked writer belongs to one handler and is
// not for concurrent writes. Independent requests never share response state.
type responseWriter struct {
	w         http.ResponseWriter
	status    int
	bytes     int64
	committed bool
	aborted   bool
	hijacked  net.Conn
	code      foundation.Code
}

func (w *responseWriter) Header() http.Header            { return w.w.Header() }
func (w *responseWriter) Unwrap() http.ResponseWriter    { return w.w }
func (w *responseWriter) responseState() *responseWriter { return w }

func stateOf(w http.ResponseWriter) *responseWriter {
	if state, ok := w.(interface{ responseState() *responseWriter }); ok {
		return state.responseState()
	}
	if wrapper, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
		return stateOf(wrapper.Unwrap())
	}
	return nil
}

func (w *responseWriter) WriteHeader(status int) {
	if w.committed {
		return
	}
	w.w.WriteHeader(status)
	// 101 ends HTTP header processing; other informational responses do not.
	if status >= 200 || status == http.StatusSwitchingProtocols {
		w.committed = true
		w.status = status
	}
}

func (w *responseWriter) Write(p []byte) (int, error) {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.w.Write(p)
	w.bytes += int64(n)
	return n, err
}

func (w *responseWriter) abort() {
	w.aborted = true
	// net/http no longer owns a successfully hijacked connection.
	if w.hijacked != nil {
		_ = w.hijacked.Close()
	}
}

func (w *responseWriter) finalStatus() int {
	if w.status == 0 && !w.aborted && w.hijacked == nil {
		return http.StatusOK
	}
	return w.status
}

type flushWriter struct{ *responseWriter }

func (w flushWriter) Flush() { _ = w.FlushError() }
func (w flushWriter) FlushError() error {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.w).Flush()
}

type hijackWriter struct{ *responseWriter }

func (w hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.w).Hijack()
	if err == nil {
		w.committed = true
		w.hijacked = conn
	}
	return conn, rw, err
}

type flushHijackWriter struct{ *responseWriter }

func (w flushHijackWriter) Flush()            { _ = w.FlushError() }
func (w flushHijackWriter) FlushError() error { return (flushWriter{w.responseWriter}).FlushError() }
func (w flushHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return (hijackWriter{w.responseWriter}).Hijack()
}

// Optional interfaces are exposed only when the underlying writer supports them,
// including through ResponseController's documented Unwrap chain.
func trackResponse(w http.ResponseWriter) (http.ResponseWriter, *responseWriter) {
	if state := stateOf(w); state != nil {
		return w, state
	}
	state := &responseWriter{w: w}
	flush, hijack := writerCapabilities(w)
	switch {
	case flush && hijack:
		return flushHijackWriter{state}, state
	case flush:
		return flushWriter{state}, state
	case hijack:
		return hijackWriter{state}, state
	default:
		return state, state
	}
}

func writerCapabilities(w http.ResponseWriter) (flush, hijack bool) {
	for {
		_, f := w.(http.Flusher)
		_, fe := w.(interface{ FlushError() error })
		_, h := w.(http.Hijacker)
		flush = flush || f || fe
		hijack = hijack || h
		wrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return flush, hijack
		}
		w = wrapper.Unwrap()
	}
}
