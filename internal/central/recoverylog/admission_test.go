package recoverylog

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type admissionFunc func(context.Context, RecordIdentity, GrantOnce) error

func (f admissionFunc) Authorize(ctx context.Context, id RecordIdentity, g GrantOnce) error {
	return f(ctx, id, g)
}
func inviteRecord(t *testing.T) InvitationRecord {
	t.Helper()
	m, e := sc.NewSecretMaterial([]byte("https://app.example/invite#" + testID + "." + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Destroy)
	r, e := NewInvitationRecord(testID, testAttempt, "user@example.com", m)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func waitTicket(t *testing.T, ticket WriteTicket) (Result, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return ticket.Wait(ctx)
}
func TestTicketGrantIsIrrevocableButDoneWaitsForActualSync(t *testing.T) {
	writer := &observedWriter{syncEntered: make(chan struct{}), syncRelease: make(chan struct{})}
	sink := newSink(writer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var grant GrantOnce
	ticket, e := sink.SubmitInvitation(ctx, inviteRecord(t), admissionFunc(func(_ context.Context, id RecordIdentity, g GrantOnce) error {
		if id.Purpose != "invitation" || id.ResourceID != testID || id.AttemptID != testAttempt {
			t.Error("wrong work binding")
		}
		grant = g
		return g.Grant()
	}))
	if e != nil {
		t.Fatal(e)
	}
	<-writer.syncEntered
	cancel()
	ticket.Cancel()
	r, e := ticket.Wait(ctx)
	if r.State != Unknown || !errors.Is(e, context.Canceled) {
		t.Fatal(r, e)
	}
	select {
	case <-ticket.Done():
		t.Fatal("cancel treated as actual join")
	default:
	}
	if grant.Grant() == nil {
		t.Fatal("retained or duplicate grant accepted")
	}
	close(writer.syncRelease)
	for range 2 {
		r, e = waitTicket(t, ticket)
		if e != nil || r.State != Written {
			t.Fatal(r, e)
		}
	}
	closeSink(t, sink)
}
func TestTicketCancelBeforeGrantAndStaleGrantCannotWrite(t *testing.T) {
	writer := &observedWriter{}
	sink := newSink(writer)
	entered, release := make(chan struct{}), make(chan struct{})
	var grant GrantOnce
	ticket, e := sink.SubmitInvitation(context.Background(), inviteRecord(t), admissionFunc(func(_ context.Context, _ RecordIdentity, g GrantOnce) error {
		grant = g
		close(entered)
		<-release
		return g.Grant()
	}))
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	ticket.Cancel()
	close(release)
	r, e := waitTicket(t, ticket)
	if e == nil || r.State != NotWritten {
		t.Fatal(r, e)
	}
	if grant.Grant() == nil {
		t.Fatal("late grant accepted")
	}
	closeSink(t, sink)
	if writer.writes != 0 {
		t.Fatal("cancelled work wrote")
	}
}
func TestTicketQueueUsesCurrentAdmissionOnlyAtWriterHead(t *testing.T) {
	writer := &observedWriter{syncEntered: make(chan struct{}), syncRelease: make(chan struct{})}
	sink := newSink(writer)
	var allowed atomic.Bool
	allowed.Store(true)
	var calls atomic.Int32
	admission := admissionFunc(func(_ context.Context, _ RecordIdentity, g GrantOnce) error {
		calls.Add(1)
		if !allowed.Load() {
			return errors.New("current authorization refused")
		}
		return g.Grant()
	})
	first, e := sink.SubmitInvitation(context.Background(), inviteRecord(t), admission)
	if e != nil {
		t.Fatal(e)
	}
	<-writer.syncEntered
	second, e := sink.SubmitInvitation(context.Background(), inviteRecord(t), admission)
	if e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 1 {
		t.Fatal("queued work was authorized before head")
	}
	allowed.Store(false)
	close(writer.syncRelease)
	if r, e := waitTicket(t, first); e != nil || r.State != Written {
		t.Fatal(r, e)
	}
	if r, e := waitTicket(t, second); e == nil || r.State != NotWritten {
		t.Fatal(r, e)
	}
	closeSink(t, sink)
	if writer.writes != 1 || calls.Load() != 2 {
		t.Fatal("queue bypassed current authorization")
	}
}
func TestAdmissionPanicDoesNotKillWriterOrLeakValue(t *testing.T) {
	for _, granted := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "after"}[granted], func(t *testing.T) {
			writer := &observedWriter{}
			sink := newSink(writer)
			ticket, e := sink.SubmitInvitation(context.Background(), inviteRecord(t), admissionFunc(func(_ context.Context, _ RecordIdentity, g GrantOnce) error {
				if granted {
					_ = g.Grant()
				}
				panic("private-token")
			}))
			if e != nil {
				t.Fatal(e)
			}
			r, e := waitTicket(t, ticket)
			if e == nil {
				t.Fatal("panic lost")
			}
			want := NotWritten
			if granted {
				want = Unknown
			}
			if r.State != want {
				t.Fatal(r)
			}
			if e.Error() == "private-token" {
				t.Fatal("panic leaked")
			}
			next, e := sink.SubmitInvitation(context.Background(), inviteRecord(t), admissionFunc(func(_ context.Context, _ RecordIdentity, g GrantOnce) error { return g.Grant() }))
			if e != nil {
				t.Fatal(e)
			}
			if r, e := waitTicket(t, next); e != nil || r.State != Written {
				t.Fatal(r, e)
			}
			closeSink(t, sink)
			if writer.writes != 1 {
				t.Fatal("panic issued write")
			}
		})
	}
}
