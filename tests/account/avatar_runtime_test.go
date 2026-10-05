//go:build integration

package account_test

import (
	"bytes"
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type avatarCountBody struct {
	*bytes.Reader
	closes atomic.Int32
}

func (b *avatarCountBody) Close() error { b.closes.Add(1); return nil }

type avatarHeldBody struct {
	read, close, releaseRead, releaseClose chan struct{}
	readOnce, closeOnce                    sync.Once
	closes                                 atomic.Int32
}

func (b *avatarHeldBody) Read([]byte) (int, error) {
	b.readOnce.Do(func() { close(b.read) })
	<-b.releaseRead
	return 0, io.EOF
}
func (b *avatarHeldBody) Close() error {
	b.closes.Add(1)
	b.closeOnce.Do(func() { close(b.close) })
	<-b.releaseClose
	return nil
}

func TestAccountAvatarBodyOwnershipCancellationAndRuntimeJoin(t *testing.T) {
	f := newAvatarFixture(t)
	for _, invalid := range []bool{true, false} {
		raw := avatarPNG(t, 70)
		body := &avatarCountBody{Reader: bytes.NewReader(raw)}
		r := c.AvatarUpload{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "body-close", ExpectedVersion: 1}, MediaType: "image/png", ByteSize: int64(len(raw)), Body: body}
		if invalid {
			r.MediaType = "image/svg+xml"
		}
		_, e := f.profiles.PutAvatar(ctxFor(t), r)
		if invalid && e == nil || !invalid && e != nil {
			t.Fatal("body ownership path", e, safeFailure(e))
		}
		if body.closes.Load() != 1 {
			t.Fatal("body did not close exactly once", body.closes.Load())
		}
	}
	runtime, e := account.NewRuntime(f.service, f.profiles)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = account.NewRuntime(f.service, f.profiles); e == nil {
		t.Fatal("duplicate Runtime reservation")
	}
	if e = runtime.Start(ctxFor(t)); e != nil {
		t.Fatal("runtime start", e, safeFailure(e))
	}
	if e = runtime.Check(ctxFor(t)); e != nil {
		t.Fatal("runtime healthy", e)
	}
	body := &avatarHeldBody{read: make(chan struct{}), close: make(chan struct{}), releaseRead: make(chan struct{}), releaseClose: make(chan struct{})}
	var finishRead, finishClose sync.Once
	readDone := func() { finishRead.Do(func() { close(body.releaseRead) }) }
	closeDone := func() { finishClose.Do(func() { close(body.releaseClose) }) }
	defer readDone()
	defer closeDone()
	ctx, cancel := context.WithCancel(ctxFor(t))
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, e := f.profiles.PutAvatar(ctx, c.AvatarUpload{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "held-body", ExpectedVersion: 2}, MediaType: "image/png", ByteSize: -1, Body: body})
		done <- e
	}()
	await(t, body.read)
	cancel()
	await(t, body.close)
	runtime.StopAdmission()
	budget, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	e = runtime.Drain(budget)
	stop()
	if e == nil || runtime.Joined() {
		t.Fatal("drain claimed a running Read/Close joined")
	}
	readDone()
	budget, stop = context.WithTimeout(context.Background(), 30*time.Millisecond)
	e = runtime.Force(budget)
	stop()
	if e == nil || runtime.Joined() {
		t.Fatal("force claimed blocked Close joined")
	}
	select {
	case <-done:
		t.Fatal("PutAvatar returned before Body.Close joined")
	default:
	}
	closeDone()
	select {
	case e = <-done:
		if e == nil {
			t.Fatal("cancelled input succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("actual body completion not joined")
	}
	if body.closes.Load() != 1 {
		t.Fatal("concurrent force duplicated Body.Close", body.closes.Load())
	}
	if e = runtime.Drain(ctxFor(t)); e != nil || !runtime.Joined() {
		t.Fatal("runtime did not converge after actual join", e)
	}
	if _, e = f.profiles.GetProfile(ctxFor(t), f.actor); !hasCode(e, foundation.ShuttingDown) {
		t.Fatal("late profile admission", e)
	}
}

func TestAccountAvatarInvalidLengthAndMediaDoNotReserve(t *testing.T) {
	f := newAvatarFixture(t)
	for _, input := range []struct {
		media  string
		raw    []byte
		length int64
		code   foundation.Code
	}{
		{"image/png", avatarPNG(t, 20), 1, foundation.InvalidArgument},
		{"image/jpeg", avatarPNG(t, 20), -1, foundation.InvalidArgument},
		{"image/png", bytes.Repeat([]byte{0}, (5<<20)+1), -1, foundation.PayloadTooLarge},
	} {
		body := &avatarCountBody{Reader: bytes.NewReader(input.raw)}
		_, e := f.profiles.PutAvatar(ctxFor(t), c.AvatarUpload{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "invalid-input", ExpectedVersion: 1}, MediaType: input.media, ByteSize: input.length, Body: body})
		requireAvatarFault(t, e, input.code)
		if body.closes.Load() != 1 {
			t.Fatal("invalid input body leaked")
		}
	}
	var changes, objects int
	if e := f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.avatar_changes),(SELECT count(*) FROM agenteam_object.objects)`).Scan(&changes, &objects); e != nil || changes != 0 || objects != 0 {
		t.Fatal("invalid input created durable work", changes, objects, e)
	}
}
