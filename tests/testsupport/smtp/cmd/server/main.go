package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

type scenario struct {
	mu      sync.Mutex
	config  f.Scenario
	state   f.State
	release chan struct{}
	once    sync.Once
	tls     *tls.Config
}
type server struct {
	mu    sync.Mutex
	cases map[string]*scenario
	tls   *tls.Config
	nonce string
}

func main() {
	cert := flag.String("cert", "", "owned certificate")
	key := flag.String("key", "", "owned private key")
	nonce := flag.String("nonce", "", "owned nonce")
	flag.Parse()
	pair, e := tls.LoadX509KeyPair(*cert, *key)
	if e != nil || len(*nonce) != 32 {
		os.Exit(2)
	}
	s := &server{cases: map[string]*scenario{}, tls: &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}, nonce: *nonce}
	h := &http.Server{Addr: ":9000", Handler: s, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, MaxHeaderBytes: 8192}
	if h.ListenAndServe() != nil {
		os.Exit(1)
	}
}
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Fixture-Nonce") != s.nonce {
		w.WriteHeader(403)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/ready" {
		w.WriteHeader(200)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/create" {
		var cfg f.Scenario
		d := json.NewDecoder(io.LimitReader(r.Body, 16384))
		d.DisallowUnknownFields()
		if d.Decode(&cfg) != nil || cfg.Mode != "none" && cfg.Mode != "starttls" && cfg.Mode != "tls" {
			w.WriteHeader(400)
			return
		}
		if cfg.AuthCode == 0 {
			cfg.AuthCode = 235
		}
		if cfg.MailCode == 0 {
			cfg.MailCode = 250
		}
		if cfg.RCPTCode == 0 {
			cfg.RCPTCode = 250
		}
		if cfg.DataCode == 0 {
			cfg.DataCode = 250
		}
		if cfg.GreetingBytes < 0 || cfg.GreetingBytes > 65536 || cfg.GreetingLines < 0 || cfg.GreetingLines > 200 {
			w.WriteHeader(400)
			return
		}
		l, e := net.Listen("tcp", ":0")
		if e != nil {
			w.WriteHeader(500)
			return
		}
		var raw [16]byte
		if _, e = rand.Read(raw[:]); e != nil {
			l.Close()
			w.WriteHeader(500)
			return
		}
		id := hex.EncodeToString(raw[:])
		c := &scenario{config: cfg, tls: s.tls, release: make(chan struct{})}
		s.mu.Lock()
		s.cases[id] = c
		s.mu.Unlock()
		go func() {
			for {
				conn, e := l.Accept()
				if e != nil {
					return
				}
				go c.serve(conn)
			}
		}()
		_ = json.NewEncoder(w).Encode(f.Endpoint{ID: id, Port: l.Addr().(*net.TCPAddr).Port})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) != 2 {
		w.WriteHeader(404)
		return
	}
	s.mu.Lock()
	c := s.cases[parts[1]]
	s.mu.Unlock()
	if c == nil {
		w.WriteHeader(404)
		return
	}
	if r.Method == "POST" && parts[0] == "release" {
		c.once.Do(func() { close(c.release) })
		w.WriteHeader(200)
		return
	}
	if r.Method == "GET" && parts[0] == "state" {
		c.mu.Lock()
		state := c.state
		c.mu.Unlock()
		_ = json.NewEncoder(w).Encode(state)
		return
	}
	w.WriteHeader(404)
}
func (c *scenario) phase(name string) bool {
	c.mu.Lock()
	c.state.Phase = name
	c.mu.Unlock()
	if c.config.PauseStage == name {
		select {
		case <-c.release:
		case <-time.After(35 * time.Second):
			return false
		}
	}
	return c.config.CloseStage != name
}
func (c *scenario) serve(raw net.Conn) {
	c.mu.Lock()
	c.state.Connections++
	c.mu.Unlock()
	defer func() { raw.Close(); c.mu.Lock(); c.state.Closed++; c.mu.Unlock() }()
	_ = raw.SetDeadline(time.Now().Add(40 * time.Second))
	conn := raw
	upgrade := func() bool {
		t := tls.Server(raw, c.tls)
		if t.Handshake() != nil {
			return false
		}
		conn = t
		c.mu.Lock()
		c.state.TLS++
		c.mu.Unlock()
		return true
	}
	if c.config.Mode == "tls" && !upgrade() {
		return
	}
	if !c.phase("greeting") {
		return
	}
	if c.config.GreetingLines > 0 {
		for range c.config.GreetingLines {
			if _, e := io.WriteString(conn, "220-"+strings.Repeat("x", max(1, c.config.GreetingBytes))+"\r\n"); e != nil {
				return
			}
		}
	}
	if c.config.GreetingBytes > 0 && c.config.GreetingLines == 0 {
		if _, e := io.WriteString(conn, "220 "+strings.Repeat("x", c.config.GreetingBytes)+"\r\n"); e != nil {
			return
		}
	} else if _, e := io.WriteString(conn, "220 owned SMTP fixture\r\n"); e != nil {
		return
	}
	b := bufio.NewReaderSize(conn, 8192)
	write := func(code int) bool { _, e := io.WriteString(conn, strconv.Itoa(code)+" fixture\r\n"); return e == nil }
	for {
		line, e := b.ReadString('\n')
		if e != nil || len(line) > 8192 || !strings.HasSuffix(line, "\r\n") {
			return
		}
		command := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		command = strings.TrimSpace(command)
		switch command {
		case "EHLO":
			if !c.phase("ehlo") {
				return
			}
			response := "250-owned fixture\r\n"
			if c.config.Mode == "starttls" && !c.config.NoSTARTTLS {
				response += "250-STARTTLS\r\n"
			}
			if !c.config.NoAuth {
				response += "250-AUTH PLAIN\r\n"
			}
			response += "250 SIZE 65536\r\n"
			if _, e = io.WriteString(conn, response); e != nil {
				return
			}
		case "STARTTLS":
			if c.config.Mode != "starttls" || c.config.NoSTARTTLS {
				write(500)
				return
			}
			if !c.phase("starttls") || !write(220) || !upgrade() {
				return
			}
			b = bufio.NewReaderSize(conn, 8192)
		case "AUTH":
			c.mu.Lock()
			c.state.Auth++
			c.mu.Unlock()
			if !c.phase("auth") || !write(c.config.AuthCode) {
				return
			}
		case "MAIL":
			c.mu.Lock()
			c.state.Mail++
			c.mu.Unlock()
			if !c.phase("mail") || !write(c.config.MailCode) {
				return
			}
		case "RCPT":
			c.mu.Lock()
			c.state.RCPT++
			c.mu.Unlock()
			if !c.phase("rcpt") || !write(c.config.RCPTCode) {
				return
			}
		case "DATA":
			c.mu.Lock()
			c.state.Data++
			c.mu.Unlock()
			if !c.phase("data") || !write(354) {
				return
			}
			var message []byte
			for {
				line, e = b.ReadString('\n')
				if e != nil {
					return
				}
				if line == ".\r\n" {
					break
				}
				if strings.HasPrefix(line, "..") {
					line = line[1:]
				}
				message = append(message, line...)
				if len(message) > 65536 {
					return
				}
			}
			sum := sha256.Sum256(message)
			mid := ""
			for _, line := range strings.Split(string(message), "\r\n") {
				if strings.HasPrefix(line, "Message-ID: ") {
					mid = strings.TrimPrefix(line, "Message-ID: ")
					break
				}
			}
			c.mu.Lock()
			c.state.Messages++
			c.state.MessageBytes = len(message)
			c.state.MessageSHA = hex.EncodeToString(sum[:])
			c.state.MessageID = mid
			c.mu.Unlock()
			clear(message)
			if !c.phase("accepted") || !write(c.config.DataCode) {
				return
			}
		default:
			_, _ = fmt.Fprint(conn, "500 unsupported\r\n")
			return
		}
	}
}
