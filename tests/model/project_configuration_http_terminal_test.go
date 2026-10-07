//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestModelProjectConfigurationHTTPAuthorityAndTerminal(t *testing.T) {
	projectConfigurationHTTPTop(t)
	v := newProjectConfigurationHTTP(t)
	provider := v.projectProvider(t, nil)
	m := v.projectModel(t, provider)
	base := projectConfigurationHTTPPath(v.project.ID, "")
	paths := []string{base + "model-providers", base + "model-providers/" + provider.ID.String(), base + "models", base + "models/" + m.ID.String(), base + "available-chat-models"}
	for _, path := range paths {
		for _, browser := range []systemHTTPBrowser{v.adminBrowser, v.otherBrowser} {
			v.request(t, browser, "GET", path).problem(t, 404, f.NotFound)
		}
		v.request(t, systemHTTPBrowser{}, "GET", path).problem(t, 401, f.Unauthenticated)
	}
	before := v.modelCounts(t)
	for _, path := range paths {
		t.Run("same-tx/"+strings.TrimPrefix(path, base), func(t *testing.T) {
			user, _ := f.UserLock(v.owner.Details().UserID)
			projectKey, _ := f.ProjectLock(v.project.ID.String())
			references, _ := f.SystemConfigLock("model-references")
			required := []f.LockRequest{{Key: user, Mode: f.Shared}, {Key: projectKey, Mode: f.Shared}, {Key: references, Mode: f.Shared}}
			if strings.HasPrefix(path, base+"models/") {
				key, _ := f.AggregateLock(f.ModelConfigAggregate, m.ID.String())
				required = append(required, f.LockRequest{Key: key, Mode: f.Shared})
			}
			if strings.HasPrefix(path, base+"model-providers/") {
				key, _ := f.AggregateLock(f.ProviderAggregate, provider.ID.String())
				required = append(required, f.LockRequest{Key: key, Mode: f.Shared})
			}
			var calls atomic.Int32
			v.tracked.hooks(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
				if projectConfigurationHTTPCause(c) {
					calls.Add(1)
					return v.raw.RequireHeldLocks(ctx, tx, required)
				}
				return nil
			}, nil)
			defer v.tracked.hooks(nil, nil)
			v.request(t, v.ownerBrowser, "GET", path).want(t, 200)
			if calls.Load() != 1 {
				t.Fatal("query count/Tx ownership", calls.Load())
			}
		})
	}
	v.unchanged(t, before)
	t.Run("formal-Logout-blocks-on-current-read", func(t *testing.T) {
		browser := v.login(t, v.ownerBrowser.email)
		user, _ := f.UserLock(browser.actor.Details().UserID)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		ctx, cancel := context.WithCancel(testContext(t))
		var hit atomic.Bool
		v.tracked.hooks(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
			if projectConfigurationHTTPCause(c) && hit.CompareAndSwap(false, true) {
				if e := v.raw.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Shared}}); e != nil {
					return e
				}
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}, nil)
		flight := v.flight(ctx, browser, base+"models")
		defer func() { unblock(); cancel(); <-flight.done; v.tracked.hooks(nil, nil) }()
		waitSignal(t, entered)
		logoutCtx, logoutCancel := context.WithCancel(testContext(t))
		done := make(chan error, 1)
		joined := false
		key := f.IdempotencyKey(newID[struct{}](t).String())
		defer func() {
			unblock()
			logoutCancel()
			if !joined {
				<-done
			}
		}()
		go func() { done <- v.core.Logout(logoutCtx, account.LogoutRequest{Actor: browser.actor, Key: key}) }()
		managementWaitBlocked(t, &systemHTTPFixture{fixture: v.fixture}, user)
		if flight.writer.writeCount() != 0 {
			t.Fatal("published before transaction end")
		}
		unblock()
		<-flight.done
		e := <-done
		joined = true
		if e != nil || flight.aborted {
			t.Fatal("read/Logout actual terminal", e)
		}
		flight.result().want(t, 200)
		v.tracked.hooks(nil, nil)
		v.request(t, browser, "GET", base+"models").problem(t, 401, f.SessionRevoked)
	})
	t.Run("Project-SH-blocks-writer", func(t *testing.T) {
		key, _ := f.ProjectLock(v.project.ID.String())
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		ctx, cancel := context.WithCancel(testContext(t))
		var hit atomic.Bool
		v.tracked.hooks(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
			if projectConfigurationHTTPCause(c) && hit.CompareAndSwap(false, true) {
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}, nil)
		flight := v.flight(ctx, v.ownerBrowser, base+"models")
		defer func() { unblock(); cancel(); <-flight.done; v.tracked.hooks(nil, nil) }()
		waitSignal(t, entered)
		writerCtx, writerCancel := context.WithCancel(testContext(t))
		done := make(chan f.CommitResult, 1)
		joined := false
		cause := recoveryCause(t)
		defer func() {
			unblock()
			writerCancel()
			if !joined {
				<-done
			}
		}()
		go func() {
			done <- v.raw.WithinTx(writerCtx, cause, func(ctx context.Context, tx f.Tx) error {
				return v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}})
			})
		}()
		managementWaitBlocked(t, &systemHTTPFixture{fixture: v.fixture}, key)
		unblock()
		<-flight.done
		result := <-done
		joined = true
		if flight.aborted || result.State() != f.Committed {
			t.Fatal("Project writer/reader did not actually join")
		}
	})
	t.Run("cancelled-callback-tail", func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		ctx, cancel := context.WithCancel(testContext(t))
		var hit atomic.Bool
		v.tracked.hooks(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
			if projectConfigurationHTTPCause(c) && hit.CompareAndSwap(false, true) {
				close(entered)
				<-release
				return ctx.Err()
			}
			return nil
		}, nil)
		flight := v.flight(ctx, v.ownerBrowser, base+"models")
		defer func() { unblock(); cancel(); <-flight.done; v.tracked.hooks(nil, nil) }()
		waitSignal(t, entered)
		cancel()
		select {
		case <-flight.done:
			t.Fatal("cancel mistaken for actual callback join")
		case <-time.After(20 * time.Millisecond):
		}
		if flight.writer.writeCount() != 0 {
			t.Fatal("cancelled candidate published")
		}
		unblock()
		<-flight.done
		if !flight.aborted || flight.writer.writeCount() != 0 {
			t.Fatal("late cancellation published")
		}
	})
	for _, state := range []pc.Lifecycle{pc.Archiving, pc.Archived, pc.Deleting} {
		t.Run("lifecycle/"+string(state), func(t *testing.T) {
			other := *v.projectConfigurationFixture
			other.project = v.createProject(t, v.owner)
			other.scope, _ = id.InProject(other.project.ID)
			other.gate(t, state)
			path := projectConfigurationHTTPPath(other.project.ID, "models")
			if state == pc.Deleting {
				v.request(t, v.ownerBrowser, "GET", path).problem(t, 409, f.ProjectNotActive)
			} else {
				v.request(t, v.ownerBrowser, "GET", path).want(t, 200)
			}
		})
	}
	t.Run("pending", func(t *testing.T) {
		v.skills.setMode("pending")
		defer v.skills.setMode("")
		target := newID[id.Project](t)
		r, e := v.projectService.CreateProject(testContext(t), v.owner, f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: f.IdempotencyKey(target.String())}, pc.CreateProjectRequest{ProjectID: target, Name: "pending-" + target.String()[24:]})
		if e != nil || r.State != pc.CreationPending {
			t.Fatal("pending initialization fixture", e)
		}
		v.request(t, v.ownerBrowser, "GET", projectConfigurationHTTPPath(target, "models")).problem(t, 409, f.ProjectNotActive)
	})
	t.Run("real-COMMIT-ACK-drop", func(t *testing.T) { projectConfigurationHTTPUnknown(t, v, paths[3], 200) })
}
func projectConfigurationHTTPUnknown(t *testing.T, v *projectUsageHTTPFixture, path string, laterStatus int) {
	for _, method := range []string{"GET", "HEAD"} {
		t.Run(method, func(t *testing.T) {
			upstream := net.JoinHostPort("127.0.0.1", v.db.Fixture.Port)
			var proxy *projectModelPGTrace
			if laterStatus == 503 {
				proxy = newProjectConfigurationHTTPPGTrace(t, upstream)
			} else {
				proxy = newProjectModelPGTrace(t, upstream)
			}
			u, e := url.Parse(v.db.Fixture.URL(v.db.Name))
			if e != nil {
				t.Fatal(e)
			}
			u.Host = proxy.listener.Addr().String()
			raw := openStore(t, v.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
			tracked := &projectUsageHTTPStore{Store: raw}
			ak, _, _ := testKeys(t)
			sessions, e := account.NewAuthority(tracked, ak)
			if e != nil {
				t.Fatal(e)
			}
			projects, e := project.NewAuthority(tracked, project.AuthorityDependencies{Sessions: sessions, Routes: sessions})
			if e != nil {
				t.Fatal(e)
			}
			authority, e := model.NewAuthority(tracked, model.Authorizations{Sessions: sessions, System: sessions, Projects: projects})
			if e != nil {
				t.Fatal(e)
			}
			core, e := model.New(tracked, authority, v.deps)
			if e != nil {
				t.Fatal(e)
			}
			old := v.handler
			installProjectConfigurationHTTP(t, v, core)
			defer func() { v.handler = old; tracked.hooks(nil, nil) }()
			var hit atomic.Bool
			observed := make(chan f.CommitResult, 1)
			tracked.hooks(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
				if projectConfigurationHTTPCause(c) && hit.CompareAndSwap(false, true) {
					user, _ := f.UserLock(v.owner.Details().UserID)
					projectKey, _ := f.ProjectLock(v.project.ID.String())
					if e := raw.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Shared}, {Key: projectKey, Mode: f.Shared}}); e != nil {
						return e
					}
					proxy.dropCommitACK.Store(true)
				}
				return nil
			}, func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
				if projectConfigurationHTTPCause(c) {
					observed <- r
				}
				return r
			})
			r := v.request(t, v.ownerBrowser, method, path).want(t, 503)
			var original f.CommitResult
			select {
			case original = <-observed:
			case <-time.After(time.Second):
				t.Fatal("actual read result missing")
			}
			waitSignal(t, proxy.ackDropped)
			if !hit.Load() || original.State() != f.Unknown || original.AttemptID().Validate() != nil || !projectConfigurationHTTPCause(original.Cause()) {
				t.Fatal("original read attempt/cause lost")
			}
			if bytes.Contains(r.body, []byte(`"input"`)) || bytes.Contains(r.body, []byte(original.AttemptID().String())) {
				t.Fatal("Unknown leaked candidate/private attempt")
			}
			if method == "HEAD" {
				if len(r.body) != 0 {
					t.Fatal("Unknown HEAD body")
				}
			} else {
				r.problem(t, 503, f.CommitUnknown)
				var p httpapi.Problem
				if json.Unmarshal(r.body, &p) != nil || p.CommitState != f.Unknown || p.RequestID.String() != r.headers.Get("X-Request-ID") {
					t.Fatal("Unknown response binding")
				}
				projectConfigurationHTTPExport(t, "read-unknown", method, path, r)
			}
			if !strings.Contains(strings.Join(proxy.snapshot(), " "), "commit-idle-ack-dropped") {
				t.Fatal("no real backend COMMIT/idle ACK drop")
			}
			if laterStatus == 503 {
				// Bind the complete large-row transfer and terminal frames to the
				// same unique dropped connection before the later independent GET.
				events := proxy.snapshot()
				connection, dropIndex := "", -1
				for i, event := range events {
					n, kind, ok := strings.Cut(event, ":")
					if !ok || kind != "commit-idle-ack-dropped" {
						continue
					}
					id, err := strconv.ParseUint(n, 10, 32)
					if err != nil || id == 0 || dropIndex >= 0 {
						t.Fatal("actual ACK drop connection is not unique")
					}
					connection, dropIndex = n, i
				}
				if dropIndex < 0 {
					t.Fatal("actual ACK drop connection missing")
				}
				stage := 0
				sequence := []string{"send-COMMIT", "tag-COMMIT", "ready-I-before-drop", "commit-idle-ack-dropped"}
				for _, event := range events[:dropIndex+1] {
					n, kind, ok := strings.Cut(event, ":")
					if !ok || n != connection {
						continue
					}
					if stage == 0 {
						length, ok := strings.CutPrefix(kind, "forwarded-large-D:")
						if !ok {
							continue
						}
						size, err := strconv.ParseUint(length, 10, 32)
						if err != nil || size <= 1<<20 || size > 16<<20 {
							t.Fatal("large DataRow observation outside private fixture bound")
						}
						stage++
					} else if stage <= len(sequence) && kind == sequence[stage-1] {
						stage++
					}
				}
				if stage != len(sequence)+1 {
					t.Fatal("same-connection large DataRow/COMMIT/idle/ACKdrop sequence missing")
				}
			}
			tracked.hooks(nil, nil)
			later := v.request(t, v.ownerBrowser, "GET", path).want(t, laterStatus)
			if laterStatus == 503 {
				later.problem(t, 503, f.DependencyUnavailable)
			}
			t.Log("actual COMMIT idle ACK dropped after callback; later GET is a new read, not original confirmation")
		})
	}
}

// Private copy of the accepted projectModelPGTrace constructor/serve, reused
// only for this card's oversized read. Original ownership and COMMIT+idle ACK
// drop stay intact; complete writes and server DataRow streaming are explicit.
func newProjectConfigurationHTTPPGTrace(t *testing.T, upstream string) *projectModelPGTrace {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &projectModelPGTrace{listener: listener, upstream: upstream, connections: map[net.Conn]bool{}, ackDropped: make(chan struct{})}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			p.next++
			n := p.next
			p.connections[client] = true
			p.mu.Unlock()
			p.wg.Add(1)
			go p.serveProjectConfigurationHTTP(client, n)
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		p.mu.Lock()
		for conn := range p.connections {
			_ = conn.Close()
		}
		p.mu.Unlock()
		p.wg.Wait()
		t.Logf("safe PG frame events after actual join: %v", p.snapshot())
	})
	return p
}

func (p *projectModelPGTrace) serveProjectConfigurationHTTP(client net.Conn, n int) {
	defer p.wg.Done()
	defer func() { _ = client.Close(); p.mu.Lock(); delete(p.connections, client); p.mu.Unlock() }()
	server, err := net.Dial("tcp", p.upstream)
	if err != nil {
		return
	}
	p.mu.Lock()
	p.connections[server] = true
	p.mu.Unlock()
	defer func() { _ = server.Close(); p.mu.Lock(); delete(p.connections, server); p.mu.Unlock() }()
	var header [4]byte
	if _, err = io.ReadFull(client, header[:]); err != nil {
		return
	}
	size := binary.BigEndian.Uint32(header[:])
	if size < 8 || size > 1<<20 {
		return
	}
	startup := make([]byte, int(size))
	copy(startup, header[:])
	if _, err = io.ReadFull(client, startup[4:]); err != nil {
		return
	}
	if binary.BigEndian.Uint32(startup[4:8]) == 80877102 {
		p.record(n, "cancel")
	}
	if err = projectConfigurationHTTPPGWrite(server, startup); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	var dropACK atomic.Bool
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, err := readModelPGFrame(client)
			if err != nil {
				return
			}
			if frame[0] == 'Q' {
				q := strings.Trim(string(frame[5:]), "\x00; \t\r\n")
				if strings.EqualFold(q, "COMMIT") || strings.EqualFold(q, "ROLLBACK") {
					p.record(n, "send-"+strings.ToUpper(q))
				}
				if strings.EqualFold(q, "COMMIT") && p.dropCommitACK.CompareAndSwap(true, false) {
					dropACK.Store(true)
				}
			}
			if err = projectConfigurationHTTPPGWrite(server, frame); err != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		completion := false
		for {
			frame, err := p.readProjectConfigurationHTTPPGFrame(server, client, n)
			if err != nil {
				p.record(n, "backend-closed")
				return
			}
			if frame == nil {
				continue // only a completely forwarded large DataRow
			}
			switch frame[0] {
			case 'C':
				if string(frame[5:]) == "COMMIT\x00" || string(frame[5:]) == "ROLLBACK\x00" {
					p.record(n, "tag-"+strings.TrimRight(string(frame[5:]), "\x00"))
					completion = true
				}
				if dropACK.Load() && string(frame[5:]) == "COMMIT\x00" {
					ready, err := readModelPGFrame(server)
					if err != nil || ready[0] != 'Z' || len(ready) != 6 || ready[5] != 'I' {
						p.record(n, "commit-idle-unconfirmed")
						return
					}
					// The committed case drops only a proven COMMIT + idle ACK.
					// A later driver CancelRequest cannot race this transaction's
					// commit. Pending/rollback use the unchanged held-writer proxy.
					p.record(n, "ready-I-before-drop")
					p.record(n, "commit-idle-ack-dropped")
					_ = client.Close()
					close(p.ackDropped)
					return
				}
			case 'E':
				for fields := frame[5:]; len(fields) > 1; {
					i := bytes.IndexByte(fields[1:], 0)
					if i < 0 {
						break
					}
					if fields[0] == 'C' {
						p.record(n, "sqlstate-"+string(fields[1:1+i]))
					}
					fields = fields[i+2:]
				}
			case 'Z':
				if completion {
					p.record(n, "ready-"+string(frame[5:]))
					completion = false
				}
			case 'K':
				if len(frame) >= 9 {
					p.record(n, fmt.Sprintf("backend-%d", binary.BigEndian.Uint32(frame[5:9])))
				}
			}
			if err = projectConfigurationHTTPPGWrite(client, frame); err != nil {
				return
			}
		}
	}()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
}

// The shared trace's 1MiB frame reader is deliberately unchanged. This private
// server-side reader forwards only a large DataRow, bounded to 16MiB, through a
// fixed 32KiB buffer. No row payload is retained or included in trace evidence.
func (p *projectModelPGTrace) readProjectConfigurationHTTPPGFrame(server, client net.Conn, connection int) ([]byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(server, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size < 4 || size > 16<<20 || size > 1<<20 && header[0] != 'D' {
		p.record(connection, fmt.Sprintf("rejected-frame-type-%d-length-%d", header[0], size))
		return nil, fmt.Errorf("invalid bounded fixture frame")
	}
	if size <= 1<<20 {
		frame := make([]byte, int(size)+1)
		copy(frame, header[:])
		_, err := io.ReadFull(server, frame[5:])
		return frame, err
	}
	if err := projectConfigurationHTTPPGWrite(client, header[:]); err != nil {
		return nil, err
	}
	var buffer [32 << 10]byte
	remaining := int(size) - 4
	for remaining > 0 {
		n := min(remaining, len(buffer))
		if _, err := io.ReadFull(server, buffer[:n]); err != nil {
			return nil, err
		}
		if err := projectConfigurationHTTPPGWrite(client, buffer[:n]); err != nil {
			return nil, err
		}
		remaining -= n
	}
	p.record(connection, fmt.Sprintf("forwarded-large-D:%d", size))
	return nil, nil
}

func projectConfigurationHTTPPGWrite(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		n, err := writer.Write(value)
		if n < 0 || n > len(value) {
			return io.ErrShortWrite
		}
		value = value[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
