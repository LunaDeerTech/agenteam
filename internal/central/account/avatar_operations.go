package account

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// Capacity is process-wide, including multiple trusted profile compositions.
var avatarCapacity = make(chan struct{}, 18)
var avatarSlots = make(chan struct{}, 2)

func withAvatarSlot(ctx context.Context, fn func() error) error {
	select {
	case avatarCapacity <- struct{}{}:
		defer func() { <-avatarCapacity }()
	default:
		return fault(foundation.RateLimited, nil)
	}
	wait, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	select {
	case avatarSlots <- struct{}{}:
		defer func() { <-avatarSlots }()
	case <-wait.Done():
		if ctx.Err() != nil {
			return unavailable(ctx.Err())
		}
		return fault(foundation.RateLimited, nil)
	}
	if ctx.Err() != nil {
		return unavailable(ctx.Err())
	}
	return fn()
}

// closeOwnedBody is safe under caller cancellation. Its returned join is called
// before the account operation ends, even when Close itself is slow.
func closeOwnedBody(ctx context.Context, body io.ReadCloser) func() {
	var once sync.Once
	done := make(chan struct{})
	closeBody := func() { once.Do(func() { defer close(done); _ = body.Close() }); <-done }
	stop := context.AfterFunc(ctx, closeBody)
	return func() { stop(); closeBody() }
}
func readAvatarInput(ctx context.Context, r c.AvatarUpload) (avatarImage, error) {
	var image avatarImage
	e := withAvatarSlot(ctx, func() error {
		raw, e := io.ReadAll(io.LimitReader(r.Body, avatarInputLimit+1))
		if e != nil {
			var limit *http.MaxBytesError
			if errors.As(e, &limit) {
				return fault(foundation.PayloadTooLarge, e)
			}
			return unavailable(e)
		}
		defer clear(raw)
		if len(raw) > avatarInputLimit {
			return fault(foundation.PayloadTooLarge, nil)
		}
		if r.ByteSize >= 0 && int64(len(raw)) != r.ByteSize {
			return invalidAvatarImage()
		}
		if ctx.Err() != nil {
			return unavailable(ctx.Err())
		}
		image, e = prepareAvatarImage(raw, r.MediaType)
		// Decode and encode run synchronously: cancellation never releases a slot
		// or shared ProcessGuard before these noninterruptible calls have returned.
		if ctx.Err() != nil {
			clear(image.jpeg)
			return unavailable(ctx.Err())
		}
		return e
	})
	return image, e
}

type avatarReader struct {
	source  *oc.ObjectReader
	core    *Service
	op      *operation
	mu      sync.Mutex
	closing bool
	reads   sync.WaitGroup
	once    sync.Once
	done    chan struct{}
	err     error
	stop    func() bool
}

func (r *avatarReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	r.reads.Add(1)
	r.mu.Unlock()
	n, e := r.source.Read(p)
	r.reads.Done()
	if e != nil {
		closeErr := r.Close()
		if errors.Is(e, io.EOF) && closeErr != nil {
			e = closeErr
		}
	}
	return n, e
}
func (r *avatarReader) Close() error {
	r.once.Do(func() {
		r.mu.Lock()
		r.closing = true
		r.mu.Unlock()
		r.err = r.source.Close()
		r.reads.Wait()
		if r.stop != nil {
			r.stop()
		}
		r.core.finish(r.op)
		close(r.done)
	})
	<-r.done
	return r.err
}
func (p *ProfileService) ReadAvatar(ctx context.Context, actor identity.Actor, requested c.AvatarRange) (*oc.ObjectReader, error) {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human || requested.Validate() != nil {
		return nil, invalid()
	}
	core := p.state().core
	st := core.state()
	op, e := core.begin(ctx, false)
	if e != nil {
		return nil, e
	}
	handed := false
	defer func() {
		if !handed {
			core.finish(op)
		}
	}()
	ctx = op.ctx
	var id oc.ObjectID
	cause, e := recoveryCause("avatar-read")
	if e != nil {
		return nil, e
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared)}); e != nil {
			return unavailable(e)
		}
		if e := st.deps.Authority.RequireCurrentSession(ctx, tx, actor); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		id, e = currentAvatar(ctx, x, actor.Details().UserID)
		return e
	})
	if e = resultError(result); e != nil {
		return nil, e
	}
	if id.Validate() != nil {
		return nil, fault(foundation.NotFound, nil)
	}
	owner := avatarOwner(actor.Details().UserID)
	meta, e := p.state().objects.StatObject(ctx, actor, owner, id)
	if e != nil {
		return nil, portError(e)
	}
	resolved, e := avatarRange(requested, int64(meta.ByteSize))
	if e != nil {
		return nil, e
	}
	source, e := p.state().objects.ReadObject(ctx, actor, owner, id, resolved)
	if e != nil {
		return nil, portError(e)
	}
	reader := &avatarReader{source: source, core: core, op: op, done: make(chan struct{})}
	out, e := oc.NewObjectReader(source.Meta(), source.Range(), reader)
	if e != nil {
		_ = reader.Close()
		return nil, e
	}
	// Assignment and callback access are synchronized. A cancelled context may
	// start the callback immediately; it cannot race the callback handle field.
	reader.mu.Lock()
	reader.stop = context.AfterFunc(ctx, func() { _ = reader.Close() })
	reader.mu.Unlock()
	handed = true
	return out, nil
}
func avatarRange(r c.AvatarRange, total int64) (*oc.ByteRange, error) {
	if r.Validate() != nil {
		return nil, invalid()
	}
	if r.Kind == c.AvatarRangeAll {
		return nil, nil
	}
	if total <= 0 {
		return nil, fault(foundation.RangeNotSatisfiable, nil)
	}
	var offset, length int64
	switch r.Kind {
	case c.AvatarRangeClosed:
		offset = r.Offset
		length = r.Length
	case c.AvatarRangeFrom:
		offset = r.Offset
		if offset < total {
			length = total - offset
		}
	case c.AvatarRangeSuffix:
		length = min(r.Length, total)
		offset = total - length
	}
	if offset >= total || length <= 0 {
		return nil, fault(foundation.RangeNotSatisfiable, nil)
	}
	length = min(length, total-offset)
	return &oc.ByteRange{Offset: offset, Length: length}, nil
}
