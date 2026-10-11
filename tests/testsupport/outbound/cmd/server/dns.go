package main

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// This isolated fixture answers one owned synthetic name. It is never a
// recursive resolver, never reads host DNS configuration and has no upstream.
type fixtureDNS struct {
	address netip.Addr
	udp     *net.UDPConn
	tcp     net.Listener
	mu      sync.Mutex
	closing bool
	conns   map[net.Conn]struct{}
	wg      sync.WaitGroup
	once    sync.Once
	done    chan struct{}
	errors  chan error
	a, aaaa atomic.Uint64
}

func fixtureDNSAddress() (netip.Addr, error) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return netip.Addr{}, err
	}
	var selected netip.Addr
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err != nil {
			continue
		}
		ip := prefix.Addr().Unmap()
		if !ip.Is4() || !ip.IsPrivate() || ip.IsLoopback() {
			continue
		}
		if selected.IsValid() && selected != ip {
			return netip.Addr{}, errors.New("fixture DNS requires one private address")
		}
		selected = ip
	}
	if !selected.IsValid() {
		return netip.Addr{}, errors.New("fixture DNS private address missing")
	}
	return selected, nil
}

// Port zero and loopback are used by the local socket test only. The container
// entry point supplies its sole private address and port 53 explicitly.
func newFixtureDNS(address netip.Addr, port uint16) (*fixtureDNS, error) {
	if !address.Is4() || address.IsUnspecified() || address.IsMulticast() {
		return nil, errors.New("invalid fixture DNS address")
	}
	tcp, err := net.Listen("tcp4", net.JoinHostPort(address.String(), strconv.Itoa(int(port))))
	if err != nil {
		return nil, err
	}
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IP(address.AsSlice()), Port: tcp.Addr().(*net.TCPAddr).Port})
	if err != nil {
		tcp.Close()
		return nil, err
	}
	d := &fixtureDNS{address: address, udp: udp, tcp: tcp, conns: make(map[net.Conn]struct{}), done: make(chan struct{}), errors: make(chan error, 2)}
	d.wg.Add(2)
	go d.serveUDP()
	go d.serveTCP()
	return d, nil
}

func (d *fixtureDNS) failed(err error) {
	if errors.Is(err, net.ErrClosed) {
		return
	}
	select {
	case d.errors <- err:
	default:
	}
}

func (d *fixtureDNS) serveUDP() {
	defer d.wg.Done()
	buf := make([]byte, 513)
	for {
		n, peer, err := d.udp.ReadFromUDP(buf)
		if err != nil {
			d.failed(err)
			return
		}
		response := d.response(buf[:n])
		if len(response) == 0 {
			continue
		}
		_ = d.udp.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if _, err = d.udp.WriteToUDP(response, peer); err != nil {
			d.failed(err)
			return
		}
	}
}

func (d *fixtureDNS) serveTCP() {
	defer d.wg.Done()
	for {
		conn, err := d.tcp.Accept()
		if err != nil {
			d.failed(err)
			return
		}
		d.mu.Lock()
		if d.closing || len(d.conns) >= 32 {
			d.mu.Unlock()
			conn.Close()
			continue
		}
		d.conns[conn] = struct{}{}
		d.wg.Add(1)
		d.mu.Unlock()
		go d.serveConnection(conn)
	}
}

func (d *fixtureDNS) serveConnection(conn net.Conn) {
	defer d.wg.Done()
	defer func() {
		conn.Close()
		d.mu.Lock()
		delete(d.conns, conn)
		d.mu.Unlock()
	}()
	if conn.SetDeadline(time.Now().Add(2*time.Second)) != nil {
		return
	}
	var size [2]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return
	}
	n := int(binary.BigEndian.Uint16(size[:]))
	if n < 12 || n > 512 {
		return
	}
	query := make([]byte, n)
	if _, err := io.ReadFull(conn, query); err != nil {
		return
	}
	response := d.response(query)
	binary.BigEndian.PutUint16(size[:], uint16(len(response)))
	packet := append(size[:], response...)
	for len(packet) > 0 {
		n, err := conn.Write(packet)
		if err != nil || n == 0 {
			return
		}
		packet = packet[n:]
	}
}

func (d *fixtureDNS) response(query []byte) []byte {
	if len(query) < 12 {
		return nil
	}
	response := make([]byte, 12, 64)
	copy(response, query[:2])
	flags := binary.BigEndian.Uint16(query[2:])
	// Preserve the question's recursion-desired bit but never offer recursion.
	binary.BigEndian.PutUint16(response[2:], 0x8001|(flags&0x0100))
	if len(query) > 512 || flags & ^uint16(0x0100) != 0 || binary.BigEndian.Uint16(query[4:]) != 1 || binary.BigEndian.Uint16(query[6:]) != 0 || binary.BigEndian.Uint16(query[8:]) != 0 || binary.BigEndian.Uint16(query[10:]) != 0 {
		return response
	}
	pos := 12
	var labels []string
	for {
		if pos >= len(query) {
			return response
		}
		n := int(query[pos])
		pos++
		if n == 0 {
			break
		}
		if n > 63 || pos+n > len(query) || len(labels) >= 4 {
			return response
		}
		label := query[pos : pos+n]
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return response
			}
		}
		labels = append(labels, strings.ToLower(string(label)))
		pos += n
	}
	if pos+4 != len(query) {
		return response
	}
	kind, class := binary.BigEndian.Uint16(query[pos:]), binary.BigEndian.Uint16(query[pos+2:])
	response = append(response, query[12:]...)
	binary.BigEndian.PutUint16(response[4:], 1)
	binary.BigEndian.PutUint16(response[2:], 0x8005|(flags&0x0100))
	if strings.Join(labels, ".") != "fixture.test" || class != 1 || kind != 1 && kind != 28 {
		return response
	}
	binary.BigEndian.PutUint16(response[2:], 0x8400|(flags&0x0100))
	if kind == 28 {
		d.aaaa.Add(1)
		return response // The IPv6 family is explicitly complete and empty.
	}
	d.a.Add(1)
	binary.BigEndian.PutUint16(response[6:], 1)
	response = append(response, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 0, 0, 4)
	ip := d.address.As4()
	return append(response, ip[:]...)
}

func (d *fixtureDNS) Counts() (uint64, uint64) { return d.a.Load(), d.aaaa.Load() }

func (d *fixtureDNS) Close() error {
	d.once.Do(func() {
		d.mu.Lock()
		d.closing = true
		d.tcp.Close()
		d.udp.Close()
		for conn := range d.conns {
			conn.Close()
		}
		d.mu.Unlock()
		d.wg.Wait()
		close(d.done)
	})
	return nil
}

func (d *fixtureDNS) Joined() bool {
	select {
	case <-d.done:
		return true
	default:
		return false
	}
}
