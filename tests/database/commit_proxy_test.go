//go:build integration

package database_test

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// commitProxy forwards the real PostgreSQL protocol on a test-only plaintext
// loopback connection. It withholds actual commit response frames; no driver
// Commit result is replaced or faked.
type commitProxy struct {
	listener    net.Listener
	upstream    string
	after       bool
	reached     chan struct{}
	release     chan struct{}
	quit        chan struct{}
	once        sync.Once
	wg          sync.WaitGroup
	mu          sync.Mutex
	connections map[net.Conn]bool
}

func newCommitProxy(t *testing.T, upstream string, after bool) *commitProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &commitProxy{listener: listener, upstream: upstream, after: after, reached: make(chan struct{}, 1), release: make(chan struct{}), quit: make(chan struct{}), connections: make(map[net.Conn]bool)}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			p.connections[client] = true
			p.mu.Unlock()
			p.wg.Add(1)
			go p.serve(client)
		}
	}()
	t.Cleanup(p.Close)
	return p
}
func (p *commitProxy) Close() {
	p.once.Do(func() {
		close(p.quit)
		_ = p.listener.Close()
		p.mu.Lock()
		for conn := range p.connections {
			_ = conn.Close()
		}
		p.mu.Unlock()
		p.wg.Wait()
	})
}
func (p *commitProxy) hold() {
	p.reached <- struct{}{}
	select {
	case <-p.release:
	case <-p.quit:
	}
}
func (p *commitProxy) serve(client net.Conn) {
	defer p.wg.Done()
	defer func() { _ = client.Close(); p.mu.Lock(); delete(p.connections, client); p.mu.Unlock() }()
	server, err := net.Dial("tcp", p.upstream)
	if err != nil {
		return
	}
	defer server.Close()
	var header [4]byte
	if _, err := io.ReadFull(client, header[:]); err != nil {
		return
	}
	size := binary.BigEndian.Uint32(header[:])
	if size < 8 || size > 1<<20 {
		return
	}
	startup := make([]byte, int(size))
	copy(startup, header[:])
	if _, err := io.ReadFull(client, startup[4:]); err != nil {
		return
	}
	if _, err := server.Write(startup); err != nil {
		return
	}
	var committing atomic.Bool
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, err := readPGFrame(client)
			if err != nil {
				return
			}
			if frame[0] == 'Q' && strings.EqualFold(strings.Trim(string(frame[5:]), "\x00; \t\r\n"), "COMMIT") {
				committing.Store(true)
				if !p.after {
					p.hold()
					return
				}
			}
			if _, err := server.Write(frame); err != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, err := readPGFrame(server)
			if err != nil {
				return
			}
			if p.after && committing.Load() && frame[0] == 'C' && string(frame[5:]) == "COMMIT\x00" {
				ready, err := readPGFrame(server)
				if err != nil || ready[0] != 'Z' || len(ready) != 6 || ready[5] != 'I' {
					return
				}
				p.hold()
				return
			}
			if _, err := client.Write(frame); err != nil {
				return
			}
		}
	}()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
}
func readPGFrame(reader io.Reader) ([]byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size < 4 || size > 1<<20 {
		return nil, errors.New("fixture protocol frame rejected")
	}
	frame := make([]byte, int(size)+1)
	copy(frame, header[:])
	_, err := io.ReadFull(reader, frame[5:])
	return frame, err
}

func TestCommitUnknownUsesObservedWireEvidence(t *testing.T) {
	for _, after := range []bool{true, false} {
		name := "before_server_commit"
		if after {
			name = "after_server_commit"
		}
		t.Run(name, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			migrate(t, db.Config(t, nil))
			admin := db.Connect(t)
			if _, err := admin.Exec(testContext(t), "CREATE TABLE commit_evidence(id integer PRIMARY KEY)"); err != nil {
				t.Fatal("fixture table failed")
			}
			proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", db.Fixture.Port), after)
			u, _ := url.Parse(db.Fixture.URL(db.Name))
			u.Host = proxy.listener.Addr().String()
			store := openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
			originalCause := cause(t)
			results := make(chan foundation.CommitResult, 1)
			var calls atomic.Int32
			go func() {
				results <- store.WithinTx(testContext(t), originalCause, func(ctx context.Context, tx foundation.Tx) error {
					calls.Add(1)
					_, err := executor(t, store, tx).Exec(ctx, "INSERT INTO commit_evidence VALUES(1)")
					return err
				})
			}()
			receive(t, proxy.reached)
			var exists bool
			if err := admin.QueryRow(testContext(t), "SELECT EXISTS(SELECT 1 FROM commit_evidence)").Scan(&exists); err != nil || exists != after {
				t.Fatal("real server commit did not match intercepted wire phase")
			}
			close(proxy.release)
			result := receive(t, results)
			if result.State() != foundation.Unknown {
				var safe *postgres.Error
				if errors.As(result.Fault(), &safe) {
					t.Fatalf("commit state=%s adapter_code=%s raw_type=%T safe_to_retry=%v", result.State(), safe.Code(), safe.Unwrap(), pgconn.SafeToRetry(safe.Unwrap()))
				}
				t.Fatal("unknown result missing")
			}
			if result.AttemptID().Validate() != nil || result.Cause().Details().RecoveryRunID != originalCause.Details().RecoveryRunID || calls.Load() != 1 {
				t.Fatal("unknown evidence lost or callback retried")
			}
			if err := admin.QueryRow(testContext(t), "SELECT EXISTS(SELECT 1 FROM commit_evidence)").Scan(&exists); err != nil || exists != after {
				t.Fatal("commit fact changed after disconnect")
			}
		})
	}
}
