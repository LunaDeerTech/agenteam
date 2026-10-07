package projecthttp

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProjectOwnerUpdateIndependentPureBoundaryOwnership(t *testing.T) {
	t.Run("flush-capability-before-mutation", func(t *testing.T) {
		for _, lookup := range []bool{false, true} {
			for _, wrapped := range []bool{false, true} {
				label := "patch"
				method, path, body := "PATCH", httpDetailPath, updateTestBody
				if lookup {
					label = "lookup"
					method, path, body = "POST", path+updateLookupSuffix, `{"command":"update"}`
				}
				if wrapped {
					label += "-unwrap"
				}
				t.Run(label, func(t *testing.T) {
					h, _, s := updateFixture()
					w := newHandlerWriter()
					var writer http.ResponseWriter = handlerNoFlush{w: w}
					if wrapped {
						writer = independentUpdateUnwrap{writer}
					}
					b := &handlerBody{Reader: strings.NewReader(body)}
					r := updateRequest(method, path, body)
					r.Body = b
					if !updateServe(h, writer, r) || s.updates.Load()+s.lookups.Load() != 0 || w.Body.Len() != 0 || b.closes.Load() != 1 {
						t.Fatal("missing Flush capability executed service, published body, or lost close")
					}
					w.assertCleared(t)
				})
			}
		}
	})
	t.Run("exact-encoded-body-caps", func(t *testing.T) {
		for _, lookup := range []bool{false, true} {
			for _, extra := range []int{0, 1} {
				label := "patch"
				method, path, body, limit := "PATCH", httpDetailPath, updateTestBody, 64<<10
				if lookup {
					label = "lookup"
					method, path, body, limit = "POST", httpDetailPath+updateLookupSuffix, `{"command":"update"}`, 1<<10
				}
				if extra == 1 {
					label += "-overflow"
				} else {
					label += "-exact"
				}
				t.Run(label, func(t *testing.T) {
					h, _, s := updateFixture()
					w := newHandlerWriter()
					payload := body + strings.Repeat(" ", limit-len(body)+extra)
					aborted := updateServe(h, w, updateRequest(method, path, payload))
					want, calls := http.StatusOK, int32(1)
					if extra == 1 {
						want, calls = http.StatusRequestEntityTooLarge, 0
					}
					if aborted || w.Code != want || s.updates.Load()+s.lookups.Load() != calls {
						t.Fatalf("body cap: status=%d calls=%d", w.Code, s.updates.Load()+s.lookups.Load())
					}
					w.assertCleared(t)
				})
			}
		}
	})
	t.Run("original-close-and-cancel-callback-both-join", func(t *testing.T) {
		h, _, s := updateFixture()
		w := newHandlerWriter()
		closeEntered, closeRelease := make(chan struct{}), make(chan struct{})
		callbackEntered, callbackRelease := make(chan struct{}), make(chan struct{})
		var closeOnce, callbackOnce, enterOnce sync.Once
		releaseClose := func() { closeOnce.Do(func() { close(closeRelease) }) }
		releaseCallback := func() { callbackOnce.Do(func() { close(callbackRelease) }) }
		ctx, cancel := context.WithCancel(context.Background())
		body := &handlerBody{Reader: strings.NewReader(updateTestBody), close: func() error { close(closeEntered); <-closeRelease; return nil }}
		w.setWrite = func(at time.Time) error {
			if !at.IsZero() && !at.After(time.Now()) {
				enterOnce.Do(func() { close(callbackEntered) })
				<-callbackRelease
			}
			return nil
		}
		req := updateRequest("PATCH", httpDetailPath, updateTestBody).WithContext(ctx)
		req.Body = body
		done := make(chan struct{})
		var aborted bool
		go func() { defer close(done); aborted = updateServe(h, w, req) }()
		t.Cleanup(func() {
			cancel()
			releaseClose()
			releaseCallback()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("private handler did not actually return during cleanup")
				<-done
			}
		})
		await := func(ch <-chan struct{}) {
			t.Helper()
			select {
			case <-ch:
			case <-time.After(time.Second):
				t.Fatal("private checkpoint not reached")
			}
		}
		await(closeEntered)
		cancel()
		await(callbackEntered)
		if w.Body.Len() != 0 || body.closes.Load() != 1 || s.updates.Load() != 1 {
			t.Fatal("publication or ownership before close returned")
		}
		select {
		case <-done:
			t.Fatal("returned with two tails live")
		default:
		}
		releaseClose()
		select {
		case <-done:
			t.Fatal("returned before callback join")
		case <-time.After(20 * time.Millisecond):
		}
		releaseCallback()
		await(done)
		if !aborted || w.Body.Len() != 0 || body.closes.Load() != 1 || s.updates.Load() != 1 {
			t.Fatal("late publish, repeated close/call, or wrong terminal")
		}
		w.assertCleared(t)
	})
}

type independentUpdateUnwrap struct{ http.ResponseWriter }

func (w independentUpdateUnwrap) Unwrap() http.ResponseWriter { return w.ResponseWriter }
