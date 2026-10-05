//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// The observer arms only after the target's real final callback has written
// its committed receipt plus the actual Provider, typed Audit and Outbox event.
// Read/preflight/replay commits cannot be mistaken for this boundary, and no
// CommitResult is replaced by a preset Unknown.
type projectModelCommitStore struct {
	*postgres.Store
	target                         string
	arm                            func()
	fired                          atomic.Bool
	mu                             sync.Mutex
	original                       f.CommitResult
	finalFacts                     bool
	unknownReached, confirmRelease chan struct{}
}

func (w *projectModelCommitStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	result := w.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if cause.Kind() != f.CommandsCause || cause.Details().Primary.Namespace() != "model.project" || cause.Details().Primary.Canonical() != w.target || w.fired.Load() {
			return nil
		}
		x, err := w.Store.InTx(tx)
		if err != nil {
			return err
		}
		var final bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.commands c JOIN agenteam_model.providers p ON p.id=c.resource_id AND p.project_id=c.project_id WHERE c.command_identity=$1 AND c.scope='project' AND c.phase='committed' AND c.safe_receipt IS NOT NULL AND EXISTS(SELECT 1 FROM agenteam_audit.audit_records a WHERE a.producer='model' AND a.action='provider.create' AND a.project_id=c.project_id AND a.resource_id=c.resource_id) AND EXISTS(SELECT 1 FROM agenteam_outbox.events e WHERE e.id=c.event_id AND e.producer='model' AND e.project_id=c.project_id))`, w.target).Scan(&final)
		if err != nil {
			return err
		}
		if final && w.fired.CompareAndSwap(false, true) {
			w.mu.Lock()
			w.finalFacts = true
			w.mu.Unlock()
			w.arm()
		}
		return nil
	})
	if result.State() == f.Unknown && w.fired.Load() {
		w.mu.Lock()
		w.original = result
		w.mu.Unlock()
		close(w.unknownReached)
		select {
		case <-w.confirmRelease:
		case <-ctx.Done():
		}
	}
	return result
}

type projectModelUnknownFixture struct {
	*projectConfigurationFixture
	writer                             *projectModelCommitStore
	held                               *modelCommitProxy
	trace                              *projectModelPGTrace
	releaseWriter, releaseConfirmation func()
}

func newProjectModelUnknownFixture(t *testing.T, committed bool) *projectModelUnknownFixture {
	t.Helper()
	db := newDatabase(t)
	upstream := net.JoinHostPort("127.0.0.1", db.Fixture.Port)
	out := &projectModelUnknownFixture{}
	var address net.Addr
	var arm func()
	if committed {
		out.trace = newProjectModelPGTrace(t, upstream)
		address = out.trace.listener.Addr()
		arm = func() { out.trace.dropCommitACK.Store(true) }
	} else {
		out.held = newModelCommitProxy(t, upstream, false)
		address = out.held.listener.Addr()
		arm = func() { out.held.armed.Store(true) }
	}
	u, err := url.Parse(db.Fixture.URL(db.Name))
	if err != nil {
		t.Fatal(err)
	}
	u.Host = address.String()
	raw := openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	w := &projectModelCommitStore{Store: raw, arm: arm, unknownReached: make(chan struct{}), confirmRelease: make(chan struct{})}
	out.writer = w
	out.projectConfigurationFixture = assembleProjectConfiguration(t, db, raw, w)
	out.admin = out.human(t, "admin")
	out.owner = out.human(t, "user")
	out.regular = out.owner
	out.project = out.createProject(t, out.owner)
	out.scope, _ = id.InProject(out.project.ID)
	var writerOnce, confirmOnce sync.Once
	out.releaseWriter = func() {
		writerOnce.Do(func() {
			if out.held != nil {
				close(out.held.release)
			}
		})
	}
	out.releaseConfirmation = func() { confirmOnce.Do(func() { close(w.confirmRelease) }) }
	t.Cleanup(out.releaseWriter)
	t.Cleanup(out.releaseConfirmation)
	return out
}
func (v *projectModelUnknownFixture) request(t *testing.T) mc.CreateProviderRequest {
	t.Helper()
	r := mc.CreateProviderRequest{CommandMeta: v.projectMeta(t, "exact-unknown-command"), Input: projectProviderInput()}
	command, err := f.NewCommandIdentity("model.project", []string{v.project.ID.String(), v.owner.Details().UserID}, "provider.create", r.Key)
	if err != nil {
		t.Fatal(err)
	}
	v.writer.target = command.Canonical()
	return r
}
func (v *projectModelUnknownFixture) originalUnknown(t *testing.T, err error, receipt mc.CommandReceipt) {
	t.Helper()
	var unknown *model.UnknownCommandError
	if !errors.As(err, &unknown) || receipt != (mc.CommandReceipt{}) {
		t.Fatal("unconfirmed operation returned a receipt or lost Unknown", err)
	}
	v.writer.mu.Lock()
	original, final := v.writer.original, v.writer.finalFacts
	v.writer.mu.Unlock()
	if !final || original.State() != f.Unknown || unknown.AttemptID() != original.AttemptID() || unknown.Cause().Details().Primary.Canonical() != original.Cause().Details().Primary.Canonical() {
		t.Fatal("original final-COMMIT cause/attempt changed")
	}
}
func (v *projectModelUnknownFixture) writerLock(t *testing.T) error {
	t.Helper()
	command, err := f.NewCommandIdentity("model.project", []string{v.project.ID.String(), v.owner.Details().UserID}, "provider.create", "exact-unknown-command")
	if err != nil {
		t.Fatal(err)
	}
	key, err := f.CommandLock(command)
	if err != nil {
		t.Fatal(err)
	}
	// The same exact writer lock is the terminal proof after the PG connection
	// closes, not the proxy's socket-close signal alone.
	result := v.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		return v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}})
	})
	if result.State() != f.Committed {
		return result.Fault()
	}
	return nil
}
func TestModelProjectRealFinalCommitUnknownThreeStates(t *testing.T) {
	for _, mode := range []string{"committed", "committed-archived", "committed-revoked", "rollback", "pending"} {
		t.Run(mode, func(t *testing.T) {
			committed := strings.HasPrefix(mode, "committed")
			v := newProjectModelUnknownFixture(t, committed)
			request := v.request(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			type response struct {
				receipt mc.CommandReceipt
				err     error
			}
			done := make(chan response, 1)
			go func() { r, err := v.service.CreateProvider(ctx, request); done <- response{r, err} }()
			if committed {
				waitSignal(t, v.trace.ackDropped)
			} else {
				waitSignal(t, v.held.reached)
			}
			waitSignal(t, v.writer.unknownReached)
			if mode == "committed-archived" {
				v.gate(t, c.Archived)
			}
			if mode == "committed-revoked" {
				v.revokeSession(t, v.owner)
			}
			if mode == "rollback" {
				v.releaseWriter()
				waitSignal(t, v.held.completed)
				if err := v.writerLock(t); err != nil {
					t.Fatal("rollback writer not terminal", err)
				}
			}
			if mode == "pending" {
				requireProjectLockTimeout(t, v.writerLock(t))
			}
			v.releaseConfirmation()
			var got response
			select {
			case got = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("Unknown confirmation did not return")
			}
			if mode == "committed" || mode == "committed-archived" {
				if got.err != nil || got.receipt.ResourceID == "" {
					t.Fatal("confirmed COMMIT did not recover exact Read receipt", got.err)
				}
				v.writer.mu.Lock()
				original, final := v.writer.original, v.writer.finalFacts
				v.writer.mu.Unlock()
				if !final || original.State() != f.Unknown {
					t.Fatal("did not fault final actual COMMIT")
				}
			} else {
				v.originalUnknown(t, got.err, got.receipt)
			}
			if mode == "pending" {
				v.releaseWriter()
				waitSignal(t, v.held.completed)
				if err := v.writerLock(t); err != nil {
					t.Fatal("held writer not joined", err)
				}
			}
			p, m, commands, audits, events := v.facts(t)
			want := int64(0)
			if committed {
				want = 1
			}
			if p != want || m != 0 || commands != want || audits != want || events != want {
				t.Fatal("Unknown duplicate/partial effects", p, m, commands, audits, events)
			}
			lookup, err := v.service.LookupCommand(testContext(t), model.LookupCommandRequest{Meta: request.CommandMeta, Command: "provider.create"})
			if mode == "committed-revoked" {
				requireCode(t, err, f.SessionRevoked)
				if lookup.Found || lookup.Receipt != nil {
					t.Fatal("revoked confirmation leaked receipt")
				}
			} else if err != nil || lookup.Found != committed || (lookup.Receipt != nil) != committed {
				t.Fatal("public historical lookup", err)
			}
			if v.trace != nil {
				events := strings.Join(v.trace.snapshot(), " ")
				if !strings.Contains(events, "commit-idle-ack-dropped") {
					t.Fatal("committed case lacked PG COMMIT and idle proof")
				}
				t.Log("protocol final COMMIT + idle ACK drop observed")
			}
		})
	}
}
func TestModelProjectRollbackUnknownCannotAdoptDifferentSemanticReceipt(t *testing.T) {
	v := newProjectModelUnknownFixture(t, false)
	request := v.request(t)
	ctx := testContext(t)
	type response struct {
		receipt mc.CommandReceipt
		err     error
	}
	done := make(chan response, 1)
	go func() { r, e := v.service.CreateProvider(ctx, request); done <- response{r, e} }()
	waitSignal(t, v.held.reached)
	waitSignal(t, v.writer.unknownReached)
	v.releaseWriter()
	waitSignal(t, v.held.completed)
	if err := v.writerLock(t); err != nil {
		t.Fatal(err)
	}
	raw := openStore(t, v.db.Config(t, nil))
	other := assembleProjectConfiguration(t, v.db, raw, raw)
	different := request
	different.Input.Name = "different request B"
	b, err := other.service.CreateProvider(ctx, different)
	if err != nil {
		t.Fatal("B after terminal rollback", err)
	}
	v.releaseConfirmation()
	select {
	case got := <-done:
		requireCode(t, got.err, f.IdempotencyKeyReused)
		if got.receipt != (mc.CommandReceipt{}) {
			t.Fatal("A adopted B receipt")
		}
	case <-ctx.Done():
		t.Fatal("A did not join")
	}
	lookup, err := v.service.LookupCommand(ctx, model.LookupCommandRequest{Meta: request.CommandMeta, Command: "provider.create"})
	if err != nil || !lookup.Found || lookup.Receipt == nil || *lookup.Receipt != b {
		t.Fatal("public lookup must report actual B", err)
	}
	p, m, commands, audits, events := v.facts(t)
	if p != 1 || m != 0 || commands != 1 || audits != 1 || events != 1 {
		t.Fatal("only B may have effects")
	}
}

// An owned protocol observer and optional confirmed-COMMIT ACK fault. Only
// command tags, SQLSTATE, backend PID and ReadyForQuery state are retained: no
// startup credentials, cancellation keys, SQL statements, rows or Secret bytes.
type projectModelPGTrace struct {
	listener      net.Listener
	upstream      string
	mu            sync.Mutex
	connections   map[net.Conn]bool
	events        []string
	next          int
	wg            sync.WaitGroup
	dropCommitACK atomic.Bool
	ackDropped    chan struct{}
}

func newProjectModelPGTrace(t *testing.T, upstream string) *projectModelPGTrace {
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
			go p.serve(client, n)
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
	})
	return p
}

func (p *projectModelPGTrace) record(n int, event string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, fmt.Sprintf("%d:%s", n, event))
}
func (p *projectModelPGTrace) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.events...)
}
func (p *projectModelPGTrace) serve(client net.Conn, n int) {
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
	if _, err = server.Write(startup); err != nil {
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
			if _, err = server.Write(frame); err != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		completion := false
		for {
			frame, err := readModelPGFrame(server)
			if err != nil {
				p.record(n, "backend-closed")
				return
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
			if _, err = client.Write(frame); err != nil {
				return
			}
		}
	}()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
}
