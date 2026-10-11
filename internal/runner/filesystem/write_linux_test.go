//go:build linux

package filesystem

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type controlledWriteRoot struct {
	nativeRoot
	writeCall func(context.Context, WriteRequest) (WriteResult, error)
}

func (r controlledWriteRoot) write(ctx context.Context, q WriteRequest) (WriteResult, error) {
	return r.writeCall(ctx, q)
}

func useWriteCalls(root *Root, calls linuxWriteCalls) *linuxRoot {
	r := root.fs.(*linuxRoot)
	root.fs = controlledWriteRoot{r, func(ctx context.Context, q WriteRequest) (WriteResult, error) { return r.writeWith(ctx, q, calls) }}
	return r
}
func noTemporaryWrites(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".agenteam-write-") {
			t.Fatal("temporary file not retired")
		}
	}
}
func readWritten(t *testing.T, root *Root, path string, expected []byte) {
	t.Helper()
	var all []byte
	for offset := 0; ; offset += MaxReadBytes {
		got, err := root.Read(context.Background(), ReadRequest{path, int64(offset), MaxReadBytes, "base64"})
		if err != nil {
			t.Fatal("read written file", err)
		}
		part, err := base64.StdEncoding.DecodeString(got.Data)
		if err != nil {
			t.Fatal("read encoding", err)
		}
		all = append(all, part...)
		if got.EOF {
			break
		}
	}
	if !bytes.Equal(all, expected) {
		t.Fatal("write/read bytes differ")
	}
}

func TestWriteRealRoundTripAndReplacement(t *testing.T) {
	path, _, root := fixture(t)
	for _, body := range [][]byte{nil, {0, 255, 128, '\r', '\n', '\t'}, bytes.Repeat([]byte{0, 255, 1, 127}, MaxWriteBytes/4)} {
		got, err := root.Write(context.Background(), WriteRequest{"data", body, true})
		if err != nil || got != (WriteResult{WriteCommitted, len(body), true}) {
			t.Fatal("whole write", got, err)
		}
		readWritten(t, root, "data", body)
	}
	old := []byte("old complete inode")
	write(t, filepath.Join(path, "data"), old)
	if err := os.Chmod(filepath.Join(path, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(filepath.Join(path, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := os.Link(filepath.Join(path, "data"), filepath.Join(path, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if got, err := root.Write(context.Background(), WriteRequest{"data", []byte("new"), true}); err != nil || got.State != WriteCommitted {
		t.Fatal("replace", err)
	}
	stillOld, err := io.ReadAll(held)
	if err != nil || !bytes.Equal(stillOld, old) {
		t.Fatal("in-place truncation observed", err)
	}
	readWritten(t, root, "hardlink", old)
	readWritten(t, root, "data", []byte("new"))
	info, err := os.Stat(filepath.Join(path, "data"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("replacement mode", err)
	}
	// The native call receives an owned copy, cleared before its owner retires.
	native := root.fs.(*linuxRoot)
	var borrowed []byte
	root.fs = controlledWriteRoot{native, func(ctx context.Context, q WriteRequest) (WriteResult, error) {
		borrowed = q.Data
		return native.write(ctx, q)
	}}
	input := []byte("caller-owned bytes")
	if _, err := root.Write(context.Background(), WriteRequest{"copy", input, false}); err != nil {
		t.Fatal("private input", err)
	}
	if string(input) != "caller-owned bytes" || !bytes.Equal(borrowed, make([]byte, len(input))) {
		t.Fatal("write changed caller input or retained its private copy")
	}
	noTemporaryWrites(t, path)
}

func TestWriteNoReplaceRacesAcrossRoots(t *testing.T) {
	path, file, root := fixture(t)
	other, err := NewRoot(file)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Drain(context.Background())
	var succeeded atomic.Int32
	var wg sync.WaitGroup
	for index := 0; index < 12; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			r := root
			if index%2 == 1 {
				r = other
			}
			got, err := r.Write(context.Background(), WriteRequest{"winner", bytes.Repeat([]byte{byte(index)}, 1024), false})
			if err == nil && got.State == WriteCommitted && got.Durable {
				succeeded.Add(1)
			} else if !errors.Is(err, ErrConflict) || got.State != WriteNotCommitted {
				t.Error("unexpected no-replace outcome", got, err)
			}
		}(index)
	}
	wg.Wait()
	body, err := os.ReadFile(filepath.Join(path, "winner"))
	if err != nil || succeeded.Load() != 1 || len(body) != 1024 {
		t.Fatal("no-replace did not choose one whole file", err)
	}
	if !bytes.Equal(body, bytes.Repeat(body[:1], len(body))) {
		t.Fatal("mixed concurrent file")
	}
	noTemporaryWrites(t, path)
}

func TestWriteShortIOPublishesOnlyCompleteFile(t *testing.T) {
	path, _, root := fixture(t)
	write(t, filepath.Join(path, "target"), []byte("old"))
	calls := realLinuxWriteCalls()
	realWrite := calls.write
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unpark := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unpark)
	first := true
	calls.write = func(fd int, data []byte) (int, error) {
		n, err := realWrite(fd, data[:min(len(data), 2)])
		if first {
			first = false
			close(entered)
			<-release
		}
		return n, err
	}
	useWriteCalls(root, calls)
	done := make(chan error, 1)
	go func() {
		_, err := root.Write(context.Background(), WriteRequest{"target", []byte("complete new value"), true})
		done <- err
	}()
	waitSignal(t, entered)
	readWritten(t, root, "target", []byte("old"))
	unpark()
	if err := waitError(t, done); err != nil {
		t.Fatal("short write continuation", err)
	}
	readWritten(t, root, "target", []byte("complete new value"))
	noTemporaryWrites(t, path)
}

func TestWriteRejectsUnsafeTargets(t *testing.T) {
	path, _, root := fixture(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "target"), []byte("unchanged"))
	if err := os.Mkdir(filepath.Join(path, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(path, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, link := range []struct{ name, target string }{{"link", filepath.Join(outside, "target")}, {"parent", outside}, {"inside", "dir"}} {
		if err := os.Symlink(link.target, filepath.Join(path, link.name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"dir", "fifo", "link", "parent/target", "inside/new", "missing/new"} {
		got, err := root.Write(context.Background(), WriteRequest{name, []byte("must not publish"), true})
		if err == nil || got.State != WriteNotCommitted {
			t.Fatal("unsafe target accepted", got, err)
		}
	}
	current, err := os.ReadFile(filepath.Join(outside, "target"))
	if err != nil || string(current) != "unchanged" {
		t.Fatal("outside target changed", err)
	}
	noTemporaryWrites(t, path)
}

func TestWriteConcurrentNamespaceReplacement(t *testing.T) {
	for _, phase := range []string{"parent", "target-link", "target-directory"} {
		t.Run(phase, func(t *testing.T) {
			path, _, root := fixture(t)
			outside := t.TempDir()
			write(t, filepath.Join(outside, "victim"), []byte("outside"))
			if err := os.Mkdir(filepath.Join(path, "dir"), 0700); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(path, "dir", "target"), []byte("old"))
			calls := realLinuxWriteCalls()
			realSync, realRename := calls.sync, calls.rename
			if phase == "parent" {
				calls.sync = func(fd int) error {
					if err := realSync(fd); err != nil {
						return err
					}
					if err := os.Rename(filepath.Join(path, "dir"), filepath.Join(path, "old-dir")); err != nil {
						return err
					}
					return os.Symlink(outside, filepath.Join(path, "dir"))
				}
			} else {
				calls.rename = func(a int, b string, c int, d string, flags uint) error {
					if err := os.Remove(filepath.Join(path, "dir", "target")); err != nil {
						return err
					}
					var err error
					if phase == "target-link" {
						err = os.Symlink(filepath.Join(outside, "victim"), filepath.Join(path, "dir", "target"))
					} else {
						err = os.Mkdir(filepath.Join(path, "dir", "target"), 0700)
					}
					if err != nil {
						return err
					}
					return realRename(a, b, c, d, flags)
				}
			}
			useWriteCalls(root, calls)
			got, err := root.Write(context.Background(), WriteRequest{"dir/target", []byte("new"), true})
			if phase == "target-link" {
				if err != nil || got.State != WriteCommitted {
					t.Fatal("atomic replacement did not replace link entry", err)
				}
				readWritten(t, root, "dir/target", []byte("new"))
			} else if err == nil || got.State == WriteCommitted {
				t.Fatal("namespace conflict accepted", got, err)
			}
			victim, err := os.ReadFile(filepath.Join(outside, "victim"))
			if err != nil || string(victim) != "outside" {
				t.Fatal("outside write", err)
			}
			if phase == "parent" {
				noTemporaryWrites(t, filepath.Join(path, "old-dir"))
			} else {
				noTemporaryWrites(t, filepath.Join(path, "dir"))
			}
		})
	}
}

func TestWritePublicationFailureFacts(t *testing.T) {
	for _, phase := range []string{"file-sync", "parent-sync", "rename-unknown", "temporary-close", "parent-close"} {
		t.Run(phase, func(t *testing.T) {
			path, _, root := fixture(t)
			write(t, filepath.Join(path, "target"), []byte("old"))
			calls := realLinuxWriteCalls()
			realSync, realRename, realCreate := calls.sync, calls.rename, calls.create
			var parent, temporary int = -1, -1
			calls.create = func(fd int, name string, how *unix.OpenHow) (int, error) {
				parent = fd
				var err error
				temporary, err = realCreate(fd, name, how)
				return temporary, err
			}
			calls.sync = func(fd int) error {
				if phase == "file-sync" && fd == temporary || phase == "parent-sync" && fd == parent {
					return unix.EIO
				}
				return realSync(fd)
			}
			calls.rename = func(a int, b string, c int, d string, flags uint) error {
				err := realRename(a, b, c, d, flags)
				if err == nil && phase == "rename-unknown" {
					return unix.EIO
				}
				return err
			}
			native := useWriteCalls(root, calls)
			realClose := native.calls.close
			var injected bool
			native.calls.close = func(fd int) error {
				err := realClose(fd)
				if !injected && (phase == "temporary-close" && fd == temporary || phase == "parent-close" && fd == parent) {
					injected = true
					return unix.EIO
				}
				return err
			}
			got, err := root.Write(context.Background(), WriteRequest{"target", []byte("new"), true})
			if !errors.Is(err, ErrUnavailable) {
				t.Fatal("failure projection", err)
			}
			expected, state, durable := "new", WriteCommitted, false
			if phase == "file-sync" || phase == "temporary-close" {
				expected, state = "old", WriteNotCommitted
			}
			if phase == "rename-unknown" {
				state = WriteUnknown
			}
			if phase == "parent-close" {
				durable = true
			}
			actual, err := os.ReadFile(filepath.Join(path, "target"))
			if err != nil || string(actual) != expected || got.State != state || got.Durable != durable {
				t.Fatal("publication fact lost or invented", got, err)
			}
			noTemporaryWrites(t, path)
		})
	}
}

func TestWriteStopWaitsForActualCallAndKeepsPublication(t *testing.T) {
	for _, phase := range []string{"write", "rename-return", "parent-close"} {
		t.Run(phase, func(t *testing.T) {
			path, _, root := fixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unpark := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unpark)
			calls := realLinuxWriteCalls()
			realWrite, realRename, realCreate := calls.write, calls.rename, calls.create
			parent := -1
			calls.create = func(fd int, name string, how *unix.OpenHow) (int, error) {
				parent = fd
				return realCreate(fd, name, how)
			}
			calls.write = func(fd int, b []byte) (int, error) {
				if phase == "write" {
					close(entered)
					<-release
				}
				return realWrite(fd, b)
			}
			calls.rename = func(a int, b string, c int, d string, flags uint) error {
				err := realRename(a, b, c, d, flags)
				if phase == "rename-return" {
					close(entered)
					<-release
				}
				return err
			}
			native := useWriteCalls(root, calls)
			realClose := native.calls.close
			if phase == "parent-close" {
				native.calls.close = func(fd int) error {
					err := realClose(fd)
					if fd == parent {
						close(entered)
						<-release
					}
					return err
				}
			}
			type outcome struct {
				result WriteResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				got, err := root.Write(context.Background(), WriteRequest{"target", []byte("complete"), false})
				done <- outcome{got, err}
			}()
			waitSignal(t, entered)
			root.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			if err := root.Drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Error("Drain escaped held original return", err)
			}
			cancel()
			if _, err := root.Write(context.Background(), WriteRequest{Path: "new"}); !errors.Is(err, ErrClosed) {
				t.Error("Stop admitted write", err)
			}
			unpark()
			var out outcome
			select {
			case out = <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("write did not return")
			}
			if !errors.Is(out.err, context.Canceled) {
				t.Fatal("cancellation lost", out.err)
			}
			if phase == "write" {
				if out.result.State != WriteNotCommitted {
					t.Fatal("prepublish cancellation committed", out.result)
				}
			} else if out.result != (WriteResult{WriteCommitted, 8, true}) {
				t.Fatal("postpublish cancellation erased actual write", out.result)
			}
			if err := root.Drain(context.Background()); err != nil {
				t.Fatal("actual Drain", err)
			}
			noTemporaryWrites(t, path)
		})
	}
}
