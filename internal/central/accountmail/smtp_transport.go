package accountmail

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

// SMTPResolver is a composition-root DNS dependency, not a request option.
// The default deployment resolver may use hosts and search domains. SMTP is
// platform infrastructure: it does not consume business rules or proxy state.
type SMTPResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

// dialSMTP owns one direct TCP socket for one delivery. As before, DNS and
// TCP connect each have a five-second budget inside the delivery's 30 seconds.
// net.Dialer performs no HTTP/HTTPS/ALL_PROXY environment lookup.
func dialSMTP(ctx context.Context, resolver SMTPResolver, host string, port int) (*directSMTPConn, error) {
	if port < 1 || port > 65535 || host == "" || nilPort(resolver) {
		return nil, invalid()
	}
	var addresses []net.IPAddr
	if ip, err := netip.ParseAddr(host); err == nil && ip.Zone() == "" {
		addresses = []net.IPAddr{{IP: net.IP(ip.Unmap().AsSlice())}}
	} else {
		dns, cancel := context.WithTimeout(ctx, 5*time.Second)
		var err error
		addresses, err = resolver.LookupIPAddr(dns, host)
		cancel()
		if err != nil {
			return nil, err
		}
	}
	if len(addresses) == 0 || len(addresses) > 64 {
		return nil, &net.DNSError{Err: "no usable SMTP addresses", Name: host, IsNotFound: true}
	}
	for _, address := range addresses {
		if _, ok := netip.AddrFromSlice(address.IP); !ok || address.Zone != "" {
			return nil, invalid()
		}
	}
	dial, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var raw net.Conn
	var err error
	for _, address := range addresses {
		var dialer net.Dialer
		raw, err = dialer.DialContext(dial, "tcp", net.JoinHostPort(address.IP.String(), strconv.Itoa(port)))
		if err == nil {
			break
		}
		if dial.Err() != nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	conn := &directSMTPConn{Conn: raw, ctx: ctx, cancelled: make(chan struct{})}
	conn.stop = context.AfterFunc(ctx, func() {
		_ = raw.Close()
		close(conn.cancelled)
	})
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// The work registry cancels ctx on Force. Close joins the actual cancellation
// callback before sendSMTP returns and the registry seals an I/O completion.
type directSMTPConn struct {
	net.Conn
	ctx       context.Context
	stop      func() bool
	cancelled chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func (c *directSMTPConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if err != nil && c.ctx.Err() != nil {
		err = c.ctx.Err()
	}
	return n, err
}

func (c *directSMTPConn) Write(b []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.Conn.Write(b)
	if err != nil && c.ctx.Err() != nil {
		err = c.ctx.Err()
	}
	return n, err
}

func (c *directSMTPConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.Conn.Close()
		if !c.stop() {
			<-c.cancelled
		}
	})
	return c.closeErr
}

func (c *directSMTPConn) boundedDeadline(t time.Time) time.Time {
	if d, ok := c.ctx.Deadline(); ok && (t.IsZero() || d.Before(t)) {
		return d
	}
	return t
}

func (c *directSMTPConn) SetDeadline(t time.Time) error {
	return c.Conn.SetDeadline(c.boundedDeadline(t))
}
func (c *directSMTPConn) SetReadDeadline(t time.Time) error {
	return c.Conn.SetReadDeadline(c.boundedDeadline(t))
}
func (c *directSMTPConn) SetWriteDeadline(t time.Time) error {
	return c.Conn.SetWriteDeadline(c.boundedDeadline(t))
}
