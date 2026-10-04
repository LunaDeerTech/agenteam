package object

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func digestOf(p []byte) foundation.Digest {
	s := sha256.Sum256(p)
	return foundation.Digest("sha256:" + hex.EncodeToString(s[:]))
}
func newTestSpool(t *testing.T) *Spool {
	t.Helper()
	id, _ := foundation.NewID[oc.Process]()
	s, e := OpenSpool(t.TempDir(), id)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func codeIs(err error, code foundation.Code) bool {
	var f *foundation.Fault
	return errors.As(err, &f) && f.Code == code
}

type countedSource struct {
	reader  io.Reader
	maximum int
	reads   int
	closed  bool
}

func (r *countedSource) Read(p []byte) (int, error) {
	r.maximum = max(r.maximum, len(p))
	r.reads++
	return r.reader.Read(p)
}
func (r *countedSource) Close() error { r.closed = true; return nil }

type seekSource struct{ *bytes.Reader }

func (r seekSource) Close() error { return nil }

func TestSpoolExactBodyBudgetAndSeekablePrefixTrap(t *testing.T) {
	s := newTestSpool(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		value    []byte
		length   int64
		expected *foundation.Digest
		ok       bool
	}{
		{"empty", nil, 0, nil, true}, {"small", []byte("abc"), 3, nil, true}, {"short", []byte("ab"), 3, nil, false}, {"long", []byte("abcd"), 3, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &countedSource{reader: bytes.NewReader(tc.value)}
			p, e := s.Prepare(ctx, "text/plain", tc.length, tc.expected, source)
			if (e == nil) != tc.ok || !source.closed {
				t.Fatal("preparation boundary failed", e)
			}
			if tc.ok {
				if p.Details().SHA256 != digestOf(tc.value) {
					t.Fatal("measured digest wrong")
				}
				if e = s.Discard(p); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
	wrong := digestOf([]byte("wrong"))
	if _, e := s.Prepare(ctx, "text/plain", 3, &wrong, io.NopCloser(bytes.NewReader([]byte("abc")))); !codeIs(e, foundation.InvalidArgument) {
		t.Fatal("wrong checksum accepted")
	}
	if _, e := s.Prepare(ctx, "text/plain", 3, nil, seekSource{bytes.NewReader([]byte("abc-trailing"))}); !codeIs(e, foundation.InvalidArgument) {
		t.Fatal("seekable SDK prefix trap survived preparation")
	}
	body := bytes.Repeat([]byte("0123456789abcdef"), 131073)
	source := &countedSource{reader: bytes.NewReader(body)}
	p, e := s.Prepare(ctx, "application/octet-stream", int64(len(body)), nil, source)
	if e != nil {
		t.Fatal(e)
	}
	if source.maximum > oc.StreamBufferSize || p.Details().SHA256 != digestOf(body) {
		t.Fatal("spool read a whole object into memory")
	}
	stream, e := s.open(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Discard(p); !codeIs(e, foundation.ResourceBusy) {
		t.Fatal("spool deleted an active upload")
	}
	if e = stream.Close(); e != nil {
		t.Fatal(e)
	}
	if e = s.Discard(p); e != nil {
		t.Fatal(e)
	}
	first, e := s.Prepare(ctx, "text/plain", 0, nil, io.NopCloser(bytes.NewReader(nil)))
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.Prepare(ctx, "text/plain", 0, nil, io.NopCloser(bytes.NewReader(nil)))
	if e != nil {
		t.Fatal(e)
	}
	third := &countedSource{reader: bytes.NewReader(nil)}
	if _, e = s.Prepare(ctx, "text/plain", 0, nil, third); !codeIs(e, foundation.RateLimited) || third.reads != 0 {
		t.Fatal("spool admission queued or read excess input")
	}
	_ = s.Discard(first)
	_ = s.Discard(second)
}
func TestSpoolCancellationClosesPipeAndReleasesReservation(t *testing.T) {
	s := newTestSpool(t)
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { _, e := s.Prepare(ctx, "application/octet-stream", 1<<20, nil, reader); done <- e }()
	if _, e := writer.Write([]byte("started")); e != nil {
		t.Fatal(e)
	}
	cancel()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancelled spool succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked source was not joined")
	}
	if _, e := writer.Write([]byte("late")); e == nil {
		t.Fatal("source was not closed")
	}
	_ = writer.Close()
	s.state().mu.Lock()
	remaining, preparing, files := s.state().reserved, s.state().preparing, len(s.state().files)
	s.state().mu.Unlock()
	if remaining != 0 || preparing != 0 || files != 0 {
		t.Fatal("cancelled preparation retained budget")
	}
}

type stoppedProcess func(context.Context, oc.ProcessID) error

func (f stoppedProcess) ConfirmStopped(ctx context.Context, id oc.ProcessID) error { return f(ctx, id) }
func TestSpoolExclusiveOwnershipAndConfirmedOrphanCleanup(t *testing.T) {
	dir := t.TempDir()
	process, _ := foundation.NewID[oc.Process]()
	s, e := OpenSpool(dir, process)
	if e != nil {
		t.Fatal(e)
	}
	other, _ := foundation.NewID[oc.Process]()
	if _, e = OpenSpool(dir, other); !codeIs(e, foundation.ResourceBusy) {
		t.Fatal("two instances share a spool")
	}
	p, e := s.Prepare(context.Background(), "text/plain", 6, nil, io.NopCloser(bytes.NewBufferString("secret")))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	restarted, e := OpenSpool(dir, other)
	if e != nil {
		t.Fatal(e)
	}
	defer restarted.Close()
	if e = restarted.RecoverOrphans(context.Background(), nil); !codeIs(e, foundation.DependencyUnbound) {
		t.Fatal("missing death evidence silently reclaimed a spool")
	}
	if _, e = os.Stat(filepath.Join(dir, p.Details().ID.String()+".payload")); e != nil {
		t.Fatal("unconfirmed orphan was removed")
	}
	confirmed := false
	e = restarted.RecoverOrphans(context.Background(), stoppedProcess(func(_ context.Context, id oc.ProcessID) error {
		if id != process {
			t.Fatal("wrong process recovery")
		}
		confirmed = true
		return nil
	}))
	if e != nil || !confirmed {
		t.Fatal("confirmed recovery failed", e)
	}
	if _, e = os.Stat(filepath.Join(dir, p.Details().ID.String()+".payload")); !os.IsNotExist(e) {
		t.Fatal("confirmed owned body remains")
	}
	symlink := filepath.Join(t.TempDir(), "link")
	if e = os.Symlink(dir, symlink); e != nil {
		t.Fatal(e)
	}
	if _, e = OpenSpool(symlink, other); e == nil {
		t.Fatal("symlinked spool accepted")
	}
}

type closeProofSource struct {
	mu     sync.Mutex
	reader *bytes.Reader
	closed bool
	max    int
}

func TestSpoolDiscardCrashRetainsExactDeathBoundary(t *testing.T) {
	dir := t.TempDir()
	previous, _ := foundation.NewID[oc.Process]()
	current, _ := foundation.NewID[oc.Process]()
	s, err := OpenSpool(dir, previous)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Prepare(context.Background(), "text/plain", 4, nil, io.NopCloser(bytes.NewBufferString("body")))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(dir, p.Details().ID.String()+".payload")); err != nil {
		t.Fatal(err)
	}
	next, err := OpenSpool(dir, current)
	if err != nil {
		t.Fatal("sealed manifest left by a normal interrupted discard", err)
	}
	defer next.Close()
	manifest := filepath.Join(dir, p.Details().ID.String()+".json")
	if err = next.RecoverOrphans(context.Background(), stoppedProcess(func(_ context.Context, id oc.ProcessID) error {
		if id != previous {
			t.Fatal("proof requested for wrong process")
		}
		return failure(foundation.ResourceBusy, nil)
	})); !codeIs(err, foundation.ResourceBusy) {
		t.Fatal("live owner was reclaimed", err)
	}
	if _, err = os.Stat(manifest); err != nil {
		t.Fatal("unconfirmed manifest removed", err)
	}
	if _, err = next.open(p); err == nil {
		t.Fatal("missing payload accepted as readable")
	}
	if err = next.RecoverOrphans(context.Background(), stoppedProcess(func(_ context.Context, id oc.ProcessID) error {
		if id != previous {
			t.Fatal("proof requested for wrong process")
		}
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(manifest); !os.IsNotExist(err) {
		t.Fatal("confirmed residue did not converge", err)
	}
}

func (r *closeProofSource) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	r.max = max(r.max, len(p))
	return r.reader.Read(p)
}
func (r *closeProofSource) Close() error { r.mu.Lock(); r.closed = true; r.mu.Unlock(); return nil }
func TestIntegrityHoldbackRejectsBeforeCompleteLength(t *testing.T) {
	for _, size := range []int{0, 1, oc.StreamBufferSize, oc.StreamBufferSize + 1, 4*oc.StreamBufferSize + 31} {
		for _, corrupt := range []bool{false, true} {
			body := bytes.Repeat([]byte("a"), size)
			expected := digestOf(body)
			if corrupt {
				expected = digestOf([]byte("different-content"))
			}
			source := &closeProofSource{reader: bytes.NewReader(body)}
			released := 0
			r, e := newIntegrityReader(context.Background(), source, int64(size), expected, func() error {
				source.mu.Lock()
				defer source.mu.Unlock()
				if !source.closed {
					t.Fatal("lease released before actual source close")
				}
				released++
				return nil
			})
			if corrupt && size <= oc.StreamBufferSize {
				if !codeIs(e, foundation.ObjectIntegrityMismatch) || r != nil || released != 1 {
					t.Fatal("small corrupt content reached caller")
				}
				continue
			}
			if e != nil {
				t.Fatal(e)
			}
			got, e := io.ReadAll(r)
			if corrupt {
				if !codeIs(e, foundation.ObjectIntegrityMismatch) || len(got) >= size || size-len(got) < oc.StreamBufferSize {
					t.Fatal("corrupt stream delivered its full length")
				}
			} else if e != nil || !bytes.Equal(got, body) {
				t.Fatal("verified stream changed bytes", e)
			}
			_ = r.Close()
			if released != 1 || source.max > oc.StreamBufferSize {
				t.Fatal("stream release/buffer contract changed")
			}
		}
	}
}
func TestIntegrityCancellationJoinsBlockedReadBeforeLeaseRelease(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source, writer := io.Pipe()
	var releases atomic.Int32
	created := make(chan struct {
		r *integrityReader
		e error
	}, 1)
	go func() {
		r, e := newIntegrityReader(ctx, source, 10*oc.StreamBufferSize, "", func() error { releases.Add(1); return nil })
		created <- struct {
			r *integrityReader
			e error
		}{r, e}
	}()
	if _, e := writer.Write(make([]byte, oc.StreamBufferSize)); e != nil {
		t.Fatal(e)
	}
	value := <-created
	if value.e != nil {
		t.Fatal(value.e)
	}
	readDone := make(chan error, 1)
	go func() { _, e := value.r.Read(make([]byte, 1)); readDone <- e }()
	cancel()
	select {
	case e := <-readDone:
		if e == nil {
			t.Fatal("cancelled blocked read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("source I/O did not join")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- value.r.Close() }()
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("cancel monitor did not join")
	}
	if releases.Load() != 1 {
		t.Fatal("lease did not release exactly once")
	}
	_ = writer.Close()
}
