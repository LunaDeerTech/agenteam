package contract

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type blockedBody struct {
	readEntered, closeEntered, readRelease, closeRelease chan struct{}
	readOnce                                             sync.Once
	calls                                                atomic.Int32
	closeErr                                             error
}

func (b *blockedBody) Read([]byte) (int, error) {
	b.readOnce.Do(func() { close(b.readEntered) })
	<-b.readRelease
	return 0, io.EOF
}
func (b *blockedBody) Close() error {
	b.calls.Add(1)
	close(b.closeEntered)
	<-b.closeRelease
	return b.closeErr
}
func waitReadTest(t *testing.T, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(5 * time.Second):
		t.Fatal("barrier timeout")
	}
}
func TestPackageReaderActualReadAndCloseJoin(t *testing.T) {
	v := sampleRevision(t)
	body := &blockedBody{readEntered: make(chan struct{}), closeEntered: make(chan struct{}), readRelease: make(chan struct{}), closeRelease: make(chan struct{}), closeErr: errors.New("close failed")}
	object, e := oc.NewObjectReader(v.Object, nil, body)
	if e != nil {
		t.Fatal(e)
	}
	r, e := NewPackageReader(v, object)
	if e != nil {
		t.Fatal(e)
	}
	readDone := make(chan struct{})
	go func() { _, _ = r.Read(make([]byte, 1)); close(readDone) }()
	waitReadTest(t, body.readEntered)
	closeDone := make(chan struct{})
	go func() {
		if !errors.Is(r.Close(), body.closeErr) {
			t.Error("lost close error")
		}
		close(closeDone)
	}()
	waitReadTest(t, body.closeEntered)
	if r.Joined() {
		t.Fatal("joined blocked IO")
	}
	close(body.closeRelease)
	waitReadTest(t, closeDone)
	if r.Joined() {
		t.Fatal("Close return equated to Read join")
	}
	if _, e = r.Read(nil); !errors.Is(e, io.ErrClosedPipe) {
		t.Fatal("read after close", e)
	}
	close(body.readRelease)
	waitReadTest(t, readDone)
	if !r.Joined() {
		t.Fatal("did not join")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !errors.Is(r.Close(), body.closeErr) {
				t.Error("cached Close error")
			}
		}()
	}
	wg.Wait()
	if body.calls.Load() != 1 {
		t.Fatal("multiple Close")
	}
}

type panicClose struct{}

func (panicClose) Read([]byte) (int, error) { return 0, io.EOF }
func (panicClose) Close() error             { panic("test close") }
func TestPackageReaderRejectsMismatchAndCannotClaimPanickedClose(t *testing.T) {
	v := sampleRevision(t)
	object, _ := oc.NewObjectReader(v.Object, nil, panicClose{})
	r, e := NewPackageReader(v, object)
	if e != nil {
		t.Fatal(e)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("missing panic")
			}
		}()
		_ = r.Close()
	}()
	if r.Joined() || r.Close() == nil {
		t.Fatal("panicked Close was success/joined")
	}
	bad := v
	bad.Object.ByteSize++
	if _, e = NewPackageReader(bad, object); e == nil {
		t.Fatal("wrong object accepted")
	}
	ranged, _ := oc.NewObjectReader(v.Object, &oc.ResolvedRange{Offset: 0, Length: 1, Total: v.Object.ByteSize}, panicClose{})
	if _, e = NewPackageReader(v, ranged); e == nil {
		t.Fatal("partial package accepted")
	}
	if _, e = NewPackageReader(v, nil); e == nil {
		t.Fatal("nil reader")
	}
}
