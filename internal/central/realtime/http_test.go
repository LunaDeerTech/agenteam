package realtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	outbox "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/realtime/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
)

// Controlled SQL facts feed the real Account Service/Authority/HTTPBoundary.
// No test supplies a manufactured Actor to the gateway. These tests exercise
// real loopback WebSockets but do not claim real PostgreSQL or App assembly.
type sessionFacts struct {
	account.Store // unused write/query capabilities panic if reached
	user          i.UserID
	session       i.SessionID
	verifier      [32]byte
	revoked       atomic.Bool
	lookups       atomic.Int64
	current       atomic.Int64
	txMu          sync.Mutex
	tx            f.Tx
	lookupHook    func(context.Context)
}

type factRow func(...any) error

func (row factRow) Scan(dest ...any) error { return row(dest...) }

func (s *sessionFacts) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	return factRow(func(dest ...any) error {
		switch {
		case strings.Contains(query, "WHERE token_verifier="):
			s.lookups.Add(1)
			if s.lookupHook != nil {
				s.lookupHook(ctx)
			}
			if !bytes.Equal(args[0].([]byte), s.verifier[:]) {
				return pgx.ErrNoRows
			}
			*dest[0].(*string), *dest[1].(*string) = s.user.String(), s.session.String()
		case strings.Contains(query, "FROM agenteam_account.sessions WHERE id="):
			s.current.Add(1)
			*dest[0].(*string), *dest[1].(*string), *dest[2].(*string) = s.session.String(), s.user.String(), "a"
			at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
			*dest[3].(*time.Time), *dest[4].(*time.Time), *dest[5].(*time.Time), *dest[8].(*time.Time) = at, at.Add(time.Hour), at.Add(time.Hour), at
			*dest[6].(*bool), *dest[9].(*bool) = s.revoked.Load(), true
		case strings.Contains(query, "FROM agenteam_account.users WHERE id="):
			*dest[0].(*string), *dest[1].(*string), *dest[2].(*string), *dest[3].(*string) = s.user.String(), "owner@example.test", "owner", "Owner"
			*dest[4].(*ac.Role), *dest[5].(*ac.Theme) = ac.RegularUser, ac.SystemTheme
			*dest[6].(*int64), *dest[9].(*int64), *dest[10].(*int64) = 1, 1, 1
		default:
			panic("unexpected realtime Account query")
		}
		return ctx.Err()
	})
}

func (s *sessionFacts) WithinTx(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.txMu.Lock()
	defer s.txMu.Unlock()
	s.tx = f.NewTx()
	if err := fn(ctx, s.tx); err != nil {
		var fault *f.Fault
		if !errors.As(err, &fault) {
			panic("untyped Account failure")
		}
		return f.NotCommittedResult(fault)
	}
	return f.CommittedResult()
}

func (s *sessionFacts) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx || !tx.Valid() {
		panic("Account read escaped its original transaction")
	}
	return s, nil
}

func (s *sessionFacts) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	return s.RequireHeldLocks(context.Background(), tx, locks)
}

func (s *sessionFacts) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	key, _ := f.UserLock(s.user.String())
	if tx != s.tx || len(locks) != 1 || f.CompareLockKeys(locks[0].Key, key) != 0 || locks[0].Mode != f.Shared {
		panic("Account authentication lost its original User lock")
	}
	return nil
}

type noAudit struct{ audit.Appender }
type noEvents struct{ outbox.Appender }
type noChallenge struct{ ac.ChallengeAuthority }
type fixtureProcess struct {
	ac.ProcessAuthority
	id ac.ProcessID
}

func (p fixtureProcess) CurrentProcess() ac.ProcessID { return p.id }

func fixtureKey(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }

func fixtureBoundary(t *testing.T, origin string, dev bool, facts *sessionFacts) *account.HTTPBoundary {
	t.Helper()
	pagination, err := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"p","keys":[{"kid":"p","key_b64":%q}]}`, fixtureKey(1)))
	if err != nil {
		t.Fatal(err)
	}
	encryption, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, fixtureKey(2)), pagination)
	if err != nil {
		t.Fatal(err)
	}
	download, err := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, fixtureKey(3)), pagination, encryption)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := account.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, fixtureKey(4)), pagination, encryption, download)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := account.NewAuthority(facts, keys)
	if err != nil {
		t.Fatal(err)
	}
	catalog := event.NewCatalog()
	sessions, err := ac.DefineSessionsRevoked(catalog)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := ac.DefineDeliveryRequested(catalog)
	if err != nil {
		t.Fatal(err)
	}
	process, _ := f.NewID[ac.Process]()
	core, err := account.New(account.Dependencies{DevMode: dev, Authority: authority, Audit: noAudit{}, Secrets: &secret.Service{}, Events: noEvents{}, Challenges: noChallenge{}, Processes: fixtureProcess{id: process}, RecoveryLog: &recoverylog.Sink{}, SessionsRevoked: sessions, DeliveryRequested: delivery})
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		t.Fatal(err)
	}
	return boundary
}

type domainFacts struct {
	accounts *sessionFacts
	topic    c.Topic
	denied   atomic.Bool
	mu       sync.Mutex
	check    func(context.Context)
	read     func(context.Context) (c.Snapshot, error)
}

func (d *domainFacts) RequireSubscription(ctx context.Context, actor i.Actor, topic c.Topic) error {
	if actor.Validate() != nil || actor.Details().Kind != i.Human || actor.Details().UserID != d.accounts.user.String() || actor.Details().SessionID != d.accounts.session.String() || d.denied.Load() || topic != d.topic {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	d.mu.Lock()
	check := d.check
	d.mu.Unlock()
	if check != nil {
		check(ctx)
	}
	return ctx.Err()
}

func (d *domainFacts) ReadSnapshot(ctx context.Context, actor i.Actor, topic c.Topic) (c.Snapshot, error) {
	if err := d.RequireSubscription(ctx, actor, topic); err != nil {
		return c.Snapshot{}, err
	}
	d.mu.Lock()
	read := d.read
	d.mu.Unlock()
	if read != nil {
		return read(ctx)
	}
	return c.NewSnapshot(topic, []byte(`{"version":"1","items":[]}`))
}

type gatewayFixture struct {
	hub      *Hub
	server   *httptest.Server
	accounts *sessionFacts
	domain   *domainFacts
	cookie   string
	returned chan struct{}
}

func newGatewayFixture(t *testing.T, dev bool, hubOptions HubOptions, httpOptions HTTPOptions, configure func(*gatewayFixture)) *gatewayFixture {
	t.Helper()
	user, _ := f.NewID[i.User]()
	session, _ := f.NewID[i.Session]()
	cookieRaw := bytes.Repeat([]byte{17}, 32)
	verifier := sha256.Sum256(append([]byte("agenteam.account.session.v1\x00"), cookieRaw...))
	fixture := &gatewayFixture{accounts: &sessionFacts{user: user, session: session, verifier: verifier}, cookie: base64.RawURLEncoding.EncodeToString(cookieRaw), returned: make(chan struct{}, 32)}
	fixture.domain = &domainFacts{accounts: fixture.accounts, topic: topicFixture(t)}
	var err error
	fixture.hub, err = NewHub(hubOptions)
	if err != nil {
		t.Fatal(err)
	}
	fixture.server = httptest.NewUnstartedServer(nil)
	origin := "http://" + fixture.server.Listener.Addr().String()
	httpOptions.PublicOrigin = origin
	if configure != nil {
		configure(fixture)
	}
	boundary := fixtureBoundary(t, origin, dev, fixture.accounts)
	handler, err := NewHTTPHandler(fixture.hub, boundary, fixture.domain, httpOptions)
	if err != nil {
		t.Fatal(err)
	}
	fixture.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { fixture.returned <- struct{}{} }()
		handler.ServeHTTP(w, r)
	})
	fixture.server.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := fixture.hub.Stop(ctx); err != nil {
			t.Errorf("gateway did not join original calls: %v", err)
		}
		fixture.server.Close()
	})
	return fixture
}

func (f *gatewayFixture) headers() http.Header {
	return http.Header{"Origin": {f.server.URL}, "Cookie": {"agenteam_local_session=" + f.cookie}}
}

func (f *gatewayFixture) dial(t *testing.T) *websocket.Conn {
	t.Helper()
	socket, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.server.URL, "http")+Path, f.headers())
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		t.Fatal("authorized websocket handshake", err)
	}
	t.Cleanup(func() { _ = socket.Close() })
	return socket
}

func sendClient(t *testing.T, socket *websocket.Conn, message c.ClientMessage) {
	t.Helper()
	if err := socket.WriteJSON(message); err != nil {
		t.Fatal(err)
	}
}

func readServer(t *testing.T, socket *websocket.Conn) c.ServerMessage {
	t.Helper()
	_ = socket.SetReadDeadline(time.Now().Add(3 * time.Second))
	var message c.ServerMessage
	if err := socket.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if err := message.Validate(); err != nil {
		t.Fatal("invalid server frame", err)
	}
	return message
}

func subscribeSocket(t *testing.T, socket *websocket.Conn, topic c.Topic) c.SubscriptionID {
	t.Helper()
	id, _ := f.NewID[c.Subscription]()
	sendClient(t, socket, c.ClientMessage{Type: c.Subscribe, SubscriptionID: id, Topic: &topic})
	ack := readServer(t, socket)
	if ack.Type != c.Subscribed || ack.SubscriptionID != id || *ack.Topic != topic {
		t.Fatal("first subscription frame was not its authorized ack")
	}
	return id
}

func awaitSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for", label)
	}
}

func requireClosed(t *testing.T, socket *websocket.Conn) {
	t.Helper()
	_ = socket.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := socket.ReadMessage(); err == nil {
		t.Fatal("protected frame escaped after rejection")
	} else if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
		t.Fatal("connection was not closed; only client timeout elapsed")
	}
}

func TestHTTPRequiresExactOriginAndOriginalCurrentSession(t *testing.T) {
	for _, dev := range []bool{false, true} {
		t.Run(fmt.Sprint("dev=", dev), func(t *testing.T) {
			fixture := newGatewayFixture(t, dev, HubOptions{}, HTTPOptions{}, nil)
			for _, tc := range []struct {
				name   string
				mutate func(http.Header)
				status int
			}{
				{"missing-origin", func(h http.Header) { h.Del("Origin") }, 403},
				{"foreign-origin", func(h http.Header) { h.Set("Origin", "https://other.example.test") }, 403},
				{"duplicate-origin", func(h http.Header) { h.Add("Origin", fixture.server.URL) }, 403},
				{"opaque-origin", func(h http.Header) { h.Set("Origin", "null") }, 403},
				{"no-session", func(h http.Header) { h.Del("Cookie") }, 401},
				{"unknown-session", func(h http.Header) {
					h.Set("Cookie", "agenteam_local_session="+base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{18}, 32)))
				}, 401},
			} {
				t.Run(tc.name, func(t *testing.T) {
					headers := fixture.headers()
					tc.mutate(headers)
					before := fixture.accounts.lookups.Load()
					socket, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(fixture.server.URL, "http")+Path, headers)
					if socket != nil {
						_ = socket.Close()
					}
					if response != nil {
						defer response.Body.Close()
					}
					if err == nil || response == nil || response.StatusCode != tc.status {
						t.Fatalf("handshake status: response=%v err=%v", response, err)
					}
					if tc.status == 403 && fixture.accounts.lookups.Load() != before {
						t.Fatal("invalid Origin reached Session lookup")
					}
				})
			}
			socket := fixture.dial(t)
			subscribeSocket(t, socket, fixture.domain.topic)
			if fixture.accounts.current.Load() < 3 {
				t.Fatal("handshake, subscribe and outgoing ack did not recheck original Session")
			}
			fixture.accounts.revoked.Store(true)
			fixture.hub.InvalidateMeeting(fixture.domain.topic.ProjectID, fixture.domain.topic.MeetingID)
			requireClosed(t, socket)
		})
	}
}

func TestHTTPSnapshotRetainsReadStartWatermarkAcrossConcurrentCommit(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	fixture := newGatewayFixture(t, false, HubOptions{}, HTTPOptions{}, func(fixture *gatewayFixture) {
		fixture.domain.read = func(ctx context.Context) (c.Snapshot, error) {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return c.Snapshot{}, ctx.Err()
			}
			return c.NewSnapshot(fixture.domain.topic, []byte(`{"version":"1","text":"<public>"}`))
		}
	})
	socket := fixture.dial(t)
	sub := subscribeSocket(t, socket, fixture.domain.topic)
	sendClient(t, socket, c.ClientMessage{Type: c.ReadSnapshot, SubscriptionID: sub})
	awaitSignal(t, entered, "original snapshot read")
	fixture.hub.InvalidateMeeting(fixture.domain.topic.ProjectID, fixture.domain.topic.MeetingID)
	update := readServer(t, socket)
	if update.Type != c.Invalidated || update.Watermark.Sequence != 1 {
		t.Fatal("commit during read was omitted")
	}
	once.Do(func() { close(release) })
	snapshot := readServer(t, socket)
	if snapshot.Type != c.SnapshotRead || snapshot.Watermark.Sequence != 0 || snapshot.Watermark.Epoch != update.Watermark.Epoch || !bytes.Contains(snapshot.Snapshot.Content, []byte(`"version":"1"`)) {
		t.Fatal("snapshot claimed a watermark newer than its read start")
	}
	fixture.domain.mu.Lock()
	fixture.domain.read = nil
	fixture.domain.mu.Unlock()
	sendClient(t, socket, c.ClientMessage{Type: c.ReadSnapshot, SubscriptionID: sub})
	if fresh := readServer(t, socket); fresh.Type != c.SnapshotRead || fresh.Watermark.Sequence != 1 {
		t.Fatal("next canonical read did not advance to its own start watermark")
	}
	sendClient(t, socket, c.ClientMessage{Type: c.Unsubscribe, SubscriptionID: sub})
	if ack := readServer(t, socket); ack.Type != c.Unsubscribed {
		t.Fatal("unsubscribe not acknowledged")
	}
}

func TestHTTPRechecksSessionAndOwnerAfterSnapshotRead(t *testing.T) {
	for _, revoke := range []string{"session", "owner"} {
		t.Run(revoke, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			fixture := newGatewayFixture(t, false, HubOptions{}, HTTPOptions{}, func(fixture *gatewayFixture) {
				fixture.domain.read = func(context.Context) (c.Snapshot, error) {
					close(entered)
					<-release
					return c.NewSnapshot(fixture.domain.topic, []byte(`{"text":"protected-result"}`))
				}
			})
			socket := fixture.dial(t)
			sub := subscribeSocket(t, socket, fixture.domain.topic)
			sendClient(t, socket, c.ClientMessage{Type: c.ReadSnapshot, SubscriptionID: sub})
			awaitSignal(t, entered, "authorized snapshot read")
			if revoke == "session" {
				fixture.accounts.revoked.Store(true)
			} else {
				fixture.domain.denied.Store(true)
			}
			once.Do(func() { close(release) })
			requireClosed(t, socket)
			awaitSignal(t, fixture.returned, "reader and writer exit")
		})
	}
}

func TestHTTPRejectsForeignTupleAndStrictClientFrames(t *testing.T) {
	fixture := newGatewayFixture(t, false, HubOptions{}, HTTPOptions{}, nil)
	for _, foreign := range []bool{false, true} {
		socket := fixture.dial(t)
		topic := fixture.domain.topic
		if foreign {
			topic.ProjectID, _ = f.NewID[i.Project]()
		}
		id, _ := f.NewID[c.Subscription]()
		raw, _ := json.Marshal(c.ClientMessage{Type: c.Subscribe, SubscriptionID: id, Topic: &topic})
		if !foreign {
			raw = append(raw[:len(raw)-1], []byte(`,"actor":"owner"}`)...)
		}
		if err := socket.WriteMessage(websocket.TextMessage, raw); err != nil {
			t.Fatal(err)
		}
		requireClosed(t, socket)
	}
}

func TestHTTPWriteDeadlineClosesSlowSocket(t *testing.T) {
	fixture := newGatewayFixture(t, false, HubOptions{}, HTTPOptions{WriteTimeout: 50 * time.Millisecond}, func(fixture *gatewayFixture) {
		fixture.domain.read = func(context.Context) (c.Snapshot, error) {
			return c.NewSnapshot(fixture.domain.topic, []byte(`{"text":"`+strings.Repeat("x", 8<<20)+`"}`))
		}
	})
	socket := fixture.dial(t)
	sub := subscribeSocket(t, socket, fixture.domain.topic)
	sendClient(t, socket, c.ClientMessage{Type: c.ReadSnapshot, SubscriptionID: sub})
	// The client deliberately stops reading. The real TCP writer must return
	// under its own deadline; a separate Stop must not be required to unblock it.
	awaitSignal(t, fixture.returned, "slow writer deadline and original handler return")
	fixture.hub.mu.Lock()
	active := len(fixture.hub.connections)
	fixture.hub.mu.Unlock()
	if active != 0 {
		t.Fatal("timed-out writer left a live Hub connection")
	}
}

func TestGatewayConstructorRejectsUnboundDependencies(t *testing.T) {
	hub, _ := NewHub(HubOptions{})
	var typedNil *domainFacts
	for _, candidate := range []*Hub{nil, {}, hub} {
		if handler, err := NewHTTPHandler(candidate, &account.HTTPBoundary{}, typedNil, HTTPOptions{PublicOrigin: "https://example.test"}); handler != nil || err == nil {
			t.Fatal("unbound Hub or typed-nil authority accepted")
		}
	}
}
