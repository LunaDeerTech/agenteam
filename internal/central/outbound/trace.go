package outbound

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// httptrace callbacks observe the actual borrowed connection before the send
// gate. They may cancel that socket, but cannot write/read around policy or
// change transport deadlines. Callbacks follow the standard synchronous trace
// contract and must return promptly (including when their context is cancelled).
type observedConn struct{ data func() *pooledConn }

func observedConnection(pc *pooledConn) net.Conn {
	return observedConn{data: func() *pooledConn { return pc }}
}
func (c observedConn) Read([]byte) (int, error)  { return 0, errors.ErrUnsupported }
func (c observedConn) Write([]byte) (int, error) { return 0, errors.ErrUnsupported }
func (c observedConn) Close() error              { c.data().close(); return nil }

// CloseWrite supports cancelling only the observed socket's send half. It
// exposes no raw connection and cannot replace, write or widen that socket.
func (c observedConn) CloseWrite() error {
	if tcp, ok := c.data().raw.(*net.TCPConn); ok {
		return tcp.CloseWrite()
	}
	return errors.ErrUnsupported
}
func (c observedConn) LocalAddr() net.Addr              { return c.data().raw.LocalAddr() }
func (c observedConn) RemoteAddr() net.Addr             { return c.data().raw.RemoteAddr() }
func (c observedConn) SetDeadline(time.Time) error      { return errors.ErrUnsupported }
func (c observedConn) SetReadDeadline(time.Time) error  { return errors.ErrUnsupported }
func (c observedConn) SetWriteDeadline(time.Time) error { return errors.ErrUnsupported }
func (c observedConn) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "outbound_connection_observation")
}
