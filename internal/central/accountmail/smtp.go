package accountmail

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

// smtpWire is below TLS. The account permit encloses the actual first socket
// Write, not a successful write into a protocol buffer or TLS staging buffer.
type smtpWire struct {
	*outbound.Conn
	permit c.SendPermit
	armed  bool
	ctx    context.Context
}

func (w *smtpWire) Write(b []byte) (int, error) {
	if !w.armed {
		return w.Conn.Write(b)
	}
	w.armed = false
	return w.permit.RunSMTPFirstWrite(func(ctx context.Context) (int, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return 0, invalid()
		}
		if e := w.Conn.SetWriteDeadline(deadline); e != nil {
			return 0, e
		}
		done := make(chan struct{})
		stop := context.AfterFunc(ctx, func() { _ = w.Conn.Close(); close(done) })
		n, e := w.Conn.Write(b)
		if !stop() {
			<-done
		}
		// Preserve the operation's shorter absolute deadline and idle cap.
		_ = w.Conn.SetWriteDeadline(smtpDeadline(w.ctx, 5*time.Second))
		return n, e
	})
}
func smtpDeadline(ctx context.Context, cap time.Duration) time.Time {
	d := time.Now().Add(cap)
	if parent, ok := ctx.Deadline(); ok && parent.Before(d) {
		d = parent
	}
	return d
}

type smtpProtocol struct {
	conn   net.Conn
	reader *bufio.Reader
	ctx    context.Context
}

func newSMTPProtocol(ctx context.Context, conn net.Conn) *smtpProtocol {
	return &smtpProtocol{conn, bufio.NewReaderSize(conn, 4096), ctx}
}
func (p *smtpProtocol) reply() (int, []string, error) {
	var lines []string
	code := 0
	total := 0
	for n := 0; n < 100; n++ {
		if e := p.conn.SetReadDeadline(smtpDeadline(p.ctx, 5*time.Second)); e != nil {
			return 0, nil, e
		}
		line, e := p.reader.ReadSlice('\n')
		total += len(line)
		if e != nil && !errors.Is(e, bufio.ErrBufferFull) {
			return 0, nil, e
		}
		if e != nil || len(line) > 4096 || total > 32768 || len(line) < 5 || line[len(line)-2] != '\r' {
			return 0, nil, fail(foundation.InvalidState, e)
		}
		v, e := strconv.Atoi(string(line[:3]))
		if e != nil || v < 200 || v > 599 || line[3] != ' ' && line[3] != '-' {
			return 0, nil, invalid()
		}
		if code != 0 && code != v {
			return 0, nil, invalid()
		}
		code = v
		// The reply is used only for protocol negotiation. It never crosses an
		// error/log boundary or enters Audit; fixed numeric codes suffice there.
		lines = append(lines, string(line[4:len(line)-2]))
		if line[3] == ' ' {
			return code, lines, nil
		}
	}
	return 0, nil, fail(foundation.InvalidState, nil)
}
func (p *smtpProtocol) write(b []byte) error {
	if e := p.conn.SetWriteDeadline(smtpDeadline(p.ctx, 5*time.Second)); e != nil {
		return e
	}
	n, e := p.conn.Write(b)
	if e == nil && n != len(b) {
		e = io.ErrShortWrite
	}
	return e
}
func (p *smtpProtocol) command(s string) (int, []string, error) {
	if len(s) > 4096 || strings.ContainsAny(s, "\r\n") {
		return 0, nil, invalid()
	}
	if e := p.write([]byte(s + "\r\n")); e != nil {
		return 0, nil, e
	}
	return p.reply()
}
func smtpStatus(code, want int) c.DeliveryOutcome {
	if code == want {
		return c.DeliveryOutcome{Result: c.DeliverySent, Reason: c.ReasonSent}
	}
	return c.DeliveryOutcome{Result: c.DeliveryFailed, Reason: c.ReasonSMTP, Retryable: code >= 400 && code < 500}
}

type smtpReplyError struct{ code int }

func (e smtpReplyError) Error() string { return "SMTP_NEGOTIATION_REJECTED" }
func smtpError(e error, uncertain bool) (c.DeliveryOutcome, error) {
	if e == nil {
		e = invalid()
	}
	var reply smtpReplyError
	if errors.As(e, &reply) {
		return smtpStatus(reply.code, 250), fail(foundation.DependencyUnavailable, e)
	}
	var timeout net.Error
	if !uncertain && errors.As(e, &timeout) && timeout.Timeout() {
		return c.DeliveryOutcome{Result: c.DeliveryFailed, Reason: c.ReasonTimeout, Retryable: true}, fail(foundation.DependencyUnavailable, e)
	}
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var cert x509.CertificateInvalidError
	if errors.As(e, &unknown) || errors.As(e, &hostname) || errors.As(e, &cert) {
		return c.DeliveryOutcome{Result: c.DeliveryFailed, Reason: c.ReasonConfiguration}, fail(foundation.DependencyUnavailable, e)
	}
	var network *outbound.NetworkError
	if errors.As(e, &network) {
		switch network.Decision().Reason {
		case ac.AddressForbidden, ac.PrivateNotAllowed, ac.PortDenied, ac.HTTPDenied, ac.BindingInvalid:
			return c.DeliveryOutcome{Result: c.DeliveryFailed, Reason: c.ReasonPolicy}, fail(foundation.DependencyUnavailable, e)
		}
	}
	return classify(e, uncertain), fail(foundation.DependencyUnavailable, e)
}
func (w *Worker) sendSMTP(parent context.Context, f c.DeliveryMaterialFields) (out c.DeliveryOutcome, err error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	attempt := f.Attempt
	id := attempt.Details().AttemptID.String()
	registration, e := identity.RegisterService(identity.OutboundService)
	if e != nil {
		return smtpError(e, false)
	}
	actor, e := registration.Actor(id, identity.SystemScope())
	if e != nil {
		return smtpError(e, false)
	}
	key, e := ac.NewAppendKey(ac.AccessProducer, id, 0)
	if e != nil {
		return smtpError(e, false)
	}
	call, e := outbound.NewCallContext(actor, identity.SystemScope(), key, ac.Associations{})
	if e != nil {
		return smtpError(e, false)
	}
	profile, e := outbound.NewSMTPProfile(call, outbound.Limits{Connect: 5 * time.Second, TLS: 10 * time.Second, Overall: 30 * time.Second, ReadIdle: 5 * time.Second})
	if e != nil {
		return smtpError(e, false)
	}
	raw, e := w.data().Outbound.DialTarget(ctx, f.Host, uint16(f.Port), profile)
	if e != nil {
		return smtpError(e, false)
	}
	defer raw.Close()
	wire := &smtpWire{Conn: raw, ctx: ctx}
	defer func() { wire.permit.Close() }()
	var conn net.Conn = wire
	upgrade := func() error {
		cfg, e := w.data().Trust.SMTPClientTLSConfig(f.Host)
		if e != nil {
			return e
		}
		tlsConn := tls.Client(wire, cfg)
		handshake, stop := context.WithTimeout(ctx, 10*time.Second)
		defer stop()
		if e = tlsConn.SetDeadline(smtpDeadline(handshake, 10*time.Second)); e != nil {
			return e
		}
		if e = tlsConn.HandshakeContext(handshake); e != nil {
			return e
		}
		conn = tlsConn
		return conn.SetDeadline(smtpDeadline(ctx, 5*time.Second))
	}
	if f.TLSMode == "tls" {
		if e = upgrade(); e != nil {
			return smtpError(e, false)
		}
	} else if f.TLSMode != "none" && f.TLSMode != "starttls" {
		return smtpError(invalid(), false)
	}
	p := newSMTPProtocol(ctx, conn)
	code, _, e := p.reply()
	if e != nil {
		return smtpError(e, false)
	}
	if code != 220 {
		return smtpStatus(code, 220), fail(foundation.DependencyUnavailable, nil)
	}
	ehlo := func() (map[string]bool, error) {
		code, lines, e := p.command("EHLO agenteam.invalid")
		if e != nil {
			return nil, e
		}
		if code != 250 {
			return nil, smtpReplyError{code}
		}
		caps := map[string]bool{}
		// The first EHLO line is the server greeting, not an extension.
		for _, line := range lines[1:] {
			words := strings.Fields(strings.ToUpper(line))
			if len(words) == 0 {
				continue
			}
			caps[words[0]] = true
			if words[0] == "AUTH" {
				for _, mechanism := range words[1:] {
					caps["AUTH:"+mechanism] = true
				}
			}
		}
		return caps, nil
	}
	caps, e := ehlo()
	if e != nil {
		return smtpError(e, false)
	}
	if f.TLSMode == "starttls" {
		if !caps["STARTTLS"] {
			return smtpError(invalid(), false)
		}
		code, _, e = p.command("STARTTLS")
		if e != nil {
			return smtpError(e, false)
		}
		if code != 220 {
			return smtpStatus(code, 220), fail(foundation.InvalidState, nil)
		}
		if p.reader.Buffered() != 0 {
			return smtpError(invalid(), false)
		}
		if e = upgrade(); e != nil {
			return smtpError(e, false)
		}
		p = newSMTPProtocol(ctx, conn)
		caps, e = ehlo()
		if e != nil {
			return smtpError(e, false)
		}
	}
	begin := func(phase c.DeliveryPhase) error {
		if e := raw.BeginSend(ctx); e != nil {
			return e
		}
		permit, e := w.data().Port.BeginDelivery(ctx, attempt, phase)
		if e != nil {
			return e
		}
		wire.permit = permit
		wire.armed = true
		return nil
	}
	if f.Username != "" {
		if !caps["AUTH"] || !caps["AUTH:PLAIN"] {
			return smtpError(invalid(), false)
		}
		var command []byte
		e = f.Password.Use(func(password []byte) error {
			b := make([]byte, 0, len(f.Username)+len(password)+2)
			b = append(b, 0)
			b = append(b, f.Username...)
			b = append(b, 0)
			b = append(b, password...)
			defer clear(b)
			command = []byte("AUTH PLAIN " + base64.StdEncoding.EncodeToString(b))
			return nil
		})
		if e != nil {
			return smtpError(e, false)
		}
		defer clear(command)
		if e = begin(c.SMTPAuth); e != nil {
			return smtpError(e, false)
		}
		code, _, e = p.command(string(command))
		clear(command)
		if e != nil {
			return smtpError(e, false)
		}
		if code != 235 {
			return c.DeliveryOutcome{Result: c.DeliveryFailed, Reason: c.ReasonSMTP}, fail(foundation.Forbidden, nil)
		}
		if e = raw.EndSend(); e != nil {
			return smtpError(e, false)
		}
	}
	message, e := w.message(f)
	if e != nil {
		return smtpError(e, false)
	}
	defer clear(message)
	if e = begin(c.SMTPMail); e != nil {
		return smtpError(e, false)
	}
	for _, cmd := range []string{"MAIL FROM:<" + f.SenderEmail + ">", "RCPT TO:<" + f.Recipient + ">"} {
		code, _, e = p.command(cmd)
		if e != nil {
			return smtpError(e, false)
		}
		if code != 250 && !(strings.HasPrefix(cmd, "RCPT") && code == 251) {
			return smtpStatus(code, 250), fail(foundation.DependencyUnavailable, nil)
		}
	}
	if e = w.data().Port.CheckpointDelivery(ctx, attempt, c.Data); e != nil {
		return smtpError(e, false)
	}
	code, _, e = p.command("DATA")
	if e != nil {
		return smtpError(e, false)
	}
	if code != 354 {
		return smtpStatus(code, 354), fail(foundation.DependencyUnavailable, nil)
	}
	// Once DATA has begun, an interrupted stream or absent final acceptance is
	// unknown. No retry occurs on this connection or with this attempt handle.
	if e = p.write(message); e != nil {
		return smtpError(e, true)
	}
	if e = w.data().Port.CheckpointDelivery(ctx, attempt, c.AwaitingAcceptance); e != nil {
		return smtpError(e, true)
	}
	if e = p.write([]byte(".\r\n")); e != nil {
		return smtpError(e, true)
	}
	code, _, e = p.reply()
	if e != nil {
		return smtpError(e, true)
	}
	if code != 250 {
		return smtpStatus(code, 250), fail(foundation.DependencyUnavailable, nil)
	}
	_ = raw.EndSend() // No QUIT or any other protocol write after EndSend.
	return c.DeliveryOutcome{Result: c.DeliverySent, Reason: c.ReasonSent}, nil
}
func (w *Worker) message(f c.DeliveryMaterialFields) ([]byte, error) {
	for _, s := range []string{f.Recipient, f.SenderEmail, f.SenderName} {
		if strings.ContainsAny(s, "\r\n\x00") {
			return nil, invalid()
		}
	}
	for _, s := range []string{f.Recipient, f.SenderEmail} {
		a, e := mail.ParseAddress(s)
		if e != nil || a.Address != s || a.Name != "" {
			return nil, invalid()
		}
	}
	from := (&mail.Address{Name: f.SenderName, Address: f.SenderEmail}).String()
	origin, e := url.Parse(w.data().PublicOrigin)
	if e != nil {
		return nil, invalid()
	}
	header := "From: " + from + "\r\nTo: <" + f.Recipient + ">\r\nMessage-ID: <" + f.Attempt.Details().JobID.String() + "@" + origin.Hostname() + ">\r\nSubject: Agenteam account\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n"
	body := "Agenteam SMTP test.\r\n"
	if f.Attempt.Details().Kind != c.TestDelivery {
		link, e := w.link(f)
		if e != nil {
			return nil, e
		}
		defer link.Destroy()
		e = link.Use(func(b []byte) error { body = string(b) + "\r\n"; return nil })
		if e != nil {
			return nil, e
		}
	}
	message := []byte(header + strings.ReplaceAll(body, "\r\n.", "\r\n.."))
	if len(message) > 64<<10 {
		clear(message)
		return nil, invalid()
	}
	return message, nil
}
