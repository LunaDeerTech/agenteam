//go:build linux

package filesystem

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func fixture(t *testing.T) (string, *os.File, *Root) {
	t.Helper()
	path := t.TempDir()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	root, err := NewRoot(file)
	if err != nil {
		t.Fatal("real Linux root", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := root.Drain(ctx); err != nil {
			t.Error("fixture drain", err)
		}
	})
	return path, file, root
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestNativeReadRanges(t *testing.T) {
	path, _, root := fixture(t)
	write(t, filepath.Join(path, "text"), []byte("abc中🙂"))
	for _, tc := range []struct {
		offset int64
		length int
		data   string
		eof    bool
	}{{0, 3, "abc", false}, {3, 3, "中", false}, {6, 4, "🙂", true}, {10, 1, "", true}, {99, 1, "", true}} {
		got, err := root.Read(context.Background(), ReadRequest{"text", tc.offset, tc.length, "utf8"})
		if err != nil || got.Data != tc.data || got.BytesRead != len(tc.data) || got.EOF != tc.eof || got.Offset != tc.offset {
			t.Fatalf("range offset %d failed: error=%v bytes=%d eof=%v", tc.offset, err, got.BytesRead, got.EOF)
		}
	}
	for _, req := range []ReadRequest{{"text", 3, 2, "utf8"}, {"text", 4, 2, "utf8"}} {
		got, err := root.Read(context.Background(), req)
		if !errors.Is(err, ErrType) || got != (ReadResult{}) {
			t.Fatal("split UTF8 accepted", err)
		}
	}
	data := []byte{0, 255, 128, 1}
	write(t, filepath.Join(path, "binary"), data)
	got, err := root.Read(context.Background(), ReadRequest{"binary", 0, 4, "base64"})
	if err != nil || got.Data != base64.StdEncoding.EncodeToString(data) || !got.EOF {
		t.Fatal("binary roundtrip", err)
	}
	write(t, filepath.Join(path, "max"), []byte(strings.Repeat("a", MaxReadBytes+1)))
	got, err = root.Read(context.Background(), ReadRequest{"max", 0, MaxReadBytes, "utf8"})
	if err != nil || got.BytesRead != MaxReadBytes || got.EOF {
		t.Fatal("bounded read", err)
	}
	// Force real pread to return short chunks; short is not EOF. The loop must
	// obtain either a real zero or all Length+1 bytes from this original fd.
	native := root.fs.(*linuxRoot)
	actual := native.calls.pread
	var calls int
	native.calls.pread = func(fd int, b []byte, offset int64) (int, error) {
		calls++
		if len(b) > 2 {
			b = b[:2]
		}
		return actual(fd, b, offset)
	}
	got, err = root.Read(context.Background(), ReadRequest{"text", 0, 3, "utf8"})
	if err != nil || got.Data != "abc" || got.EOF || calls != 2 {
		t.Fatal("short pread was treated as EOF", err, calls)
	}
}

func TestNativeListAndNames(t *testing.T) {
	path, _, root := fixture(t)
	empty, err := root.List(context.Background(), ListRequest{".", 3})
	if err != nil || len(empty.Entries) != 0 || empty.Truncated {
		t.Fatal("kernel dot entries", err)
	}
	write(t, filepath.Join(path, "z"), nil)
	if err := os.Mkdir(filepath.Join(path, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/not-followed", filepath.Join(path, "link")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(path, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := root.List(context.Background(), ListRequest{".", 4})
	want := []Entry{{"dir", "directory"}, {"fifo", "other"}, {"link", "symlink"}, {"z", "file"}}
	if err != nil || got.Truncated || !reflect.DeepEqual(got.Entries, want) {
		t.Fatal("list kinds/order", err)
	}
	got, err = root.List(context.Background(), ListRequest{".", 1})
	if err != nil || !got.Truncated || len(got.Entries) != 1 {
		t.Fatal("observed truncation", err)
	}
	for _, name := range []string{"invalid\xff", "back\\slash"} {
		write(t, filepath.Join(path, "dir", name), nil)
		got, err = root.List(context.Background(), ListRequest{"dir", 128})
		if !errors.Is(err, ErrType) || got.Path != "" || got.Entries != nil || got.Truncated {
			t.Fatal("unrepresentable filename", err)
		}
		if err := os.Remove(filepath.Join(path, "dir", name)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 129; i++ {
		write(t, filepath.Join(path, "dir", fmt.Sprintf("f%03d", i)), nil)
	}
	got, err = root.List(context.Background(), ListRequest{"dir", 128})
	if err != nil || len(got.Entries) != 128 || !got.Truncated || !sort.SliceIsSorted(got.Entries, func(i, j int) bool { return got.Entries[i].Name < got.Entries[j].Name }) {
		t.Fatal("bounded subset", err)
	}
	if err := os.Mkdir(filepath.Join(path, "volatile"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(path, "volatile", "entry"), nil)
	native := root.fs.(*linuxRoot)
	statAt := native.calls.statAt
	native.calls.statAt = func(fd int, name string, st *unix.Stat_t, flags int) error {
		if flags != unix.AT_SYMLINK_NOFOLLOW {
			t.Error("listing followed a link")
		}
		if err := os.Remove(filepath.Join(path, "volatile", name)); err != nil {
			t.Fatal(err)
		}
		return statAt(fd, name, st, flags)
	}
	got, err = root.List(context.Background(), ListRequest{"volatile", 1})
	if !errors.Is(err, ErrConflict) || got.Entries != nil || got.Path != "" {
		t.Fatal("entry disappeared during metadata read", err)
	}
}

func TestNativeContainmentAndSpecialFiles(t *testing.T) {
	path, _, root := fixture(t)
	write(t, filepath.Join(path, "inside"), []byte("inside"))
	out := t.TempDir()
	write(t, filepath.Join(out, "outside"), []byte("OUTSIDE_PRIVATE_CANARY"))
	links := map[string]string{"relative": "inside", "absolute": filepath.Join(path, "inside"), "escape": filepath.Join(out, "outside"), "magic": "/proc/self/fd/0"}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(path, name)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := root.Read(context.Background(), ReadRequest{"relative", 0, 10, "utf8"})
	if err != nil || got.Data != "inside" {
		t.Fatal("contained relative symlink", err)
	}
	for _, name := range []string{"absolute", "escape", "magic"} {
		got, err := root.Read(context.Background(), ReadRequest{name, 0, 10, "utf8"})
		if !errors.Is(err, ErrOutside) || got != (ReadResult{}) {
			t.Fatal("escape read", err)
		}
	}
	if err := os.Mkdir(filepath.Join(path, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../"+filepath.Base(out)+"/outside", filepath.Join(path, "dir", "relative-escape")); err != nil {
		t.Fatal(err)
	}
	if got, err := root.Read(context.Background(), ReadRequest{"dir/relative-escape", 0, 10, "utf8"}); !errors.Is(err, ErrOutside) || got != (ReadResult{}) {
		t.Fatal("relative escape", err)
	}
	if err := unix.Mkfifo(filepath.Join(path, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dir", "fifo"} {
		if got, err := root.Read(context.Background(), ReadRequest{name, 0, 1, "utf8"}); !errors.Is(err, ErrType) || got != (ReadResult{}) {
			t.Fatal("special read", err)
		}
	}
	native := root.fs.(*linuxRoot)
	realOpen := native.calls.open
	native.calls.open = func(fd int, name string, how *unix.OpenHow) (int, error) {
		if how.Resolve != resolution || how.Flags&unix.O_CLOEXEC == 0 {
			t.Error("resolution weakened")
		}
		if name == "inside" {
			if err := os.Rename(filepath.Join(path, "inside"), filepath.Join(path, "original")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(out, "outside"), filepath.Join(path, "inside")); err != nil {
				t.Fatal(err)
			}
		}
		return realOpen(fd, name, how)
	}
	if got, err := root.Read(context.Background(), ReadRequest{"inside", 0, 10, "utf8"}); !errors.Is(err, ErrOutside) || got != (ReadResult{}) {
		t.Fatal("replacement between validation and open", err)
	}
}

func TestNativeBorrowAndConcurrentOffsets(t *testing.T) {
	path, borrowed, root := fixture(t)
	write(t, filepath.Join(path, "data"), []byte("0123456789abcdef"))
	var group sync.WaitGroup
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			got, err := root.Read(context.Background(), ReadRequest{"data", int64(i), 1, "utf8"})
			if err != nil || got.Data != string("0123456789abcdef"[i]) {
				t.Error("independent offset", err)
			}
			list, err := root.List(context.Background(), ListRequest{".", 1})
			if err != nil || len(list.Entries) != 1 || list.Entries[0].Name != "data" {
				t.Error("independent directory offset", err)
			}
		}(i)
	}
	group.Wait()
	if err := root.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := borrowed.Stat(); err != nil {
		t.Fatal("borrowed fd was closed", err)
	}
	second, err := NewRoot(borrowed)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := second.Drain(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Read(context.Background(), ReadRequest{"data", 0, 1, "utf8"}); err != nil {
		t.Fatal("owned duplicate depended on borrowed lifetime", err)
	}
}

func TestNativeConstructorAndErrorOwnership(t *testing.T) {
	path := t.TempDir()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := NewRoot(nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("nil root", err)
	}
	write(t, filepath.Join(path, "regular"), nil)
	regular, err := os.Open(filepath.Join(path, "regular"))
	if err != nil {
		t.Fatal(err)
	}
	defer regular.Close()
	if _, err := NewRoot(regular); !errors.Is(err, ErrType) {
		t.Fatal("non-directory root", err)
	}
	calls := realLinuxCalls()
	actualClose := calls.close
	var closed []int
	calls.open = func(int, string, *unix.OpenHow) (int, error) { return -1, unix.ENOSYS }
	calls.close = func(fd int) error { closed = append(closed, fd); return actualClose(fd) }
	if _, err := openLinuxRoot(file, calls); !errors.Is(err, ErrUnsupported) || len(closed) != 1 {
		t.Fatal("unsupported probe leaked/fell back", err, len(closed))
	}
	if _, err := file.Stat(); err != nil {
		t.Fatal("constructor closed borrowed fd", err)
	}
	for _, raw := range []error{&os.PathError{Op: "PRIVATE", Path: "HOST_CANARY", Err: errors.New("CONTENT_CANARY")}, unix.EACCES, unix.EBADF} {
		if got := nativeError(raw); !errors.Is(got, ErrUnavailable) || strings.Contains(got.Error(), "CANARY") {
			t.Fatal("unsafe error")
		}
	}
}

func waitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("controlled call did not enter")
	}
}
func waitError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("original call did not return")
		return nil
	}
}

func TestStopAndDrainWaitForOriginalIO(t *testing.T) {
	for _, phase := range []string{"pread", "target-close"} {
		t.Run(phase, func(t *testing.T) {
			path, _, root := fixture(t)
			write(t, filepath.Join(path, "data"), []byte("abc"))
			native := root.fs.(*linuxRoot)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unpark := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unpark)
			var target int
			var targetCloses atomic.Int32
			pread, closeFD := native.calls.pread, native.calls.close
			if phase == "pread" {
				native.calls.pread = func(fd int, b []byte, offset int64) (int, error) {
					target = fd
					close(entered)
					<-release
					return pread(fd, b, offset)
				}
			}
			native.calls.close = func(fd int) error {
				if fd != native.fd {
					targetCloses.Add(1)
					if phase == "target-close" {
						target = fd
						close(entered)
						<-release
					}
				}
				return closeFD(fd)
			}
			done := make(chan error, 1)
			go func() {
				got, err := root.Read(context.Background(), ReadRequest{"data", 0, 2, "utf8"})
				if got != (ReadResult{}) {
					err = errors.New("cancelled call published data")
				}
				done <- err
			}()
			joined := false
			t.Cleanup(func() {
				unpark()
				if !joined {
					_ = waitError(t, done)
				}
			})
			waitSignal(t, entered)
			root.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if err := root.Drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("held call falsely drained", err)
			}
			var st unix.Stat_t
			if unix.Fstat(native.fd, &st) != nil || unix.Fstat(target, &st) != nil {
				t.Fatal("in-flight fd was closed")
			}
			root.mu.Lock()
			closed, active := root.closed, len(root.active)
			root.mu.Unlock()
			if closed || active != 1 {
				t.Fatal("held call falsely joined")
			}
			if _, err := root.Read(context.Background(), ReadRequest{"data", 0, 1, "utf8"}); !errors.Is(err, ErrClosed) {
				t.Fatal("stop admitted new read", err)
			}
			unpark()
			callErr := waitError(t, done)
			joined = true
			if !errors.Is(callErr, context.Canceled) {
				t.Fatal("original cancellation", callErr)
			}
			if err := root.Drain(context.Background()); err != nil || targetCloses.Load() != 1 {
				t.Fatal("actual retirement", err, targetCloses.Load())
			}
		})
	}
}

func TestDrainSingleCloseAndOriginalContext(t *testing.T) {
	_, _, root := fixture(t)
	native := root.fs.(*linuxRoot)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var closes atomic.Int32
	unpark := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unpark)
	realClose := native.calls.close
	native.calls.close = func(fd int) error { closes.Add(1); close(entered); <-release; return realClose(fd) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- root.Drain(ctx) }()
	joined := false
	t.Cleanup(func() {
		cancel()
		unpark()
		if !joined {
			_ = waitError(t, done)
		}
	})
	waitSignal(t, entered)
	cancel()
	other, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if err := root.Drain(other); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("second close pretended join", err)
	}
	root.mu.Lock()
	closed := root.closed
	root.mu.Unlock()
	if closed {
		t.Fatal("close is still held")
	}
	unpark()
	drainErr := waitError(t, done)
	joined = true
	if !errors.Is(drainErr, context.Canceled) {
		t.Fatal("original Drain context", drainErr)
	}
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := root.Drain(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	if closes.Load() != 1 {
		t.Fatal("root close repeated", closes.Load())
	}
}

func TestCloseErrorIsNotRetried(t *testing.T) {
	for _, phase := range []string{"target", "root"} {
		t.Run(phase, func(t *testing.T) {
			// Cleanup expects the persisted error for root close, so this fixture is
			// deliberately assembled here instead of the success-only helper.
			path := t.TempDir()
			write(t, filepath.Join(path, "data"), []byte("a"))
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			root, err := NewRoot(file)
			if err != nil {
				t.Fatal(err)
			}
			native := root.fs.(*linuxRoot)
			realClose := native.calls.close
			var attempts atomic.Int32
			native.calls.close = func(fd int) error {
				if (fd == native.fd) == (phase == "root") {
					attempts.Add(1)
					if err := realClose(fd); err != nil {
						return err
					}
					return unix.EINTR
				}
				return realClose(fd)
			}
			if phase == "target" {
				got, err := root.Read(context.Background(), ReadRequest{"data", 0, 1, "utf8"})
				if !errors.Is(err, ErrUnavailable) || got != (ReadResult{}) {
					t.Error("close error published result", err)
				}
			}
			for i := 0; i < 3; i++ {
				err := root.Drain(context.Background())
				if (phase == "root" && !errors.Is(err, ErrUnavailable)) || (phase == "target" && err != nil) {
					t.Error("cached actual close result", err)
				}
			}
			if attempts.Load() != 1 {
				t.Fatal("retried consumed fd", attempts.Load())
			}
		})
	}
}

func TestSealAndStopOrdering(t *testing.T) {
	_, _, root := fixture(t)
	// Directly exercise the same seal used by both public methods. No syscall
	// is claimed here; held real-target tests above cover their operation tail.
	op, err := root.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := root.finish(op, nil); err != nil {
		t.Fatal("success seal", err)
	}
	root.Stop()
	if err := root.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, other := fixture(t)
	op, err = other.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := other.Drain(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled drain", err)
	}
	if err := other.finish(op, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("stop did not win seal", err)
	}
	if err := other.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}
