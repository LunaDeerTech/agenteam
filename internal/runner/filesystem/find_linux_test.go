//go:build linux

package filesystem

import (
	"context"
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

func TestNativeFindAndRead(t *testing.T) {
	directory, _, root := fixture(t)
	if err := os.MkdirAll(filepath.Join(directory, "src", "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	files := []string{"root.txt", ".hidden.go", "src/main.go", "src/中.go", "src/nested/deep.go", "literal*.go"}
	for _, name := range files {
		write(t, filepath.Join(directory, name), []byte("body:"+name))
	}
	for _, tc := range []struct {
		request FindRequest
		paths   []string
	}{
		{FindRequest{".", "*.go", "name", 20}, []string{".hidden.go", "literal*.go", "src/main.go", "src/nested/deep.go", "src/中.go"}},
		{FindRequest{"src", "src/*.go", "path", 20}, []string{"src/main.go", "src/中.go"}},
		{FindRequest{".", "src/*/*.go", "path", 20}, []string{"src/nested/deep.go"}},
		{FindRequest{".", `literal\*.go`, "name", 20}, []string{"literal*.go"}},
		{FindRequest{".", "中.?o", "name", 20}, []string{"src/中.go"}},
		{FindRequest{".", "absent", "name", 20}, []string{}},
	} {
		got, err := root.Find(context.Background(), tc.request)
		if err != nil || got.Path != tc.request.Path || got.Truncated || !reflect.DeepEqual(got.Paths, tc.paths) {
			t.Fatal("real find", err, len(got.Paths), len(tc.paths))
		}
		for _, name := range got.Paths {
			read, err := root.Read(context.Background(), ReadRequest{name, 0, 256, "utf8"})
			if err != nil || !read.EOF || read.Data != "body:"+name {
				t.Fatal("find result did not compose with real Read", err)
			}
		}
	}
	for _, limit := range []int{len(files) - 1, len(files), len(files) + 1} {
		got, err := root.Find(context.Background(), FindRequest{".", "*", "name", limit})
		if err != nil || got.Truncated != (limit < len(files)) || len(got.Paths) != min(limit, len(files)) || !sort.StringsAreSorted(got.Paths) {
			t.Fatal("actual extra match semantics", err, limit)
		}
	}
}

func TestNativeFindSymlinksAndTypes(t *testing.T) {
	directory, _, root := fixture(t)
	if err := os.Mkdir(filepath.Join(directory, "target"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(directory, "target", "file"), []byte("inside"))
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret"), []byte("outside"))
	for name, target := range map[string]string{"alias": "target", "file-link": "target/file", "escape": outside, "cycle": "."} {
		if err := os.Symlink(target, filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mkfifo(filepath.Join(directory, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := root.Find(context.Background(), FindRequest{".", "*", "name", 20})
	if err != nil || got.Truncated || !reflect.DeepEqual(got.Paths, []string{"target/file"}) {
		t.Fatal("discovered links or special files followed", err)
	}
	got, err = root.Find(context.Background(), FindRequest{"alias", "*", "name", 20})
	if err != nil || !reflect.DeepEqual(got.Paths, []string{"alias/file"}) {
		t.Fatal("contained start link rejected", err)
	}
	for _, tc := range []struct {
		path string
		err  error
	}{{"escape", ErrOutside}, {"file-link", ErrType}, {"missing", ErrNotFound}} {
		got, err := root.Find(context.Background(), FindRequest{tc.path, "*", "name", 1})
		if !errors.Is(err, tc.err) || !reflect.DeepEqual(got, FindResult{}) {
			t.Fatal("invalid start", err)
		}
	}
}

func TestNativeFindTraversalBudgets(t *testing.T) {
	t.Run("depth", func(t *testing.T) {
		directory, _, root := fixture(t)
		current := directory
		for range MaxFindDepth {
			current = filepath.Join(current, "d")
			if err := os.Mkdir(current, 0700); err != nil {
				t.Fatal(err)
			}
		}
		write(t, filepath.Join(current, "hit"), nil)
		got, err := root.Find(context.Background(), FindRequest{".", "hit", "name", 1})
		if err != nil || len(got.Paths) != 1 || got.Truncated {
			t.Fatal("depth boundary", err)
		}
		if err := os.Mkdir(filepath.Join(current, "too-deep"), 0700); err != nil {
			t.Fatal(err)
		}
		got, err = root.Find(context.Background(), FindRequest{".", "hit", "name", 1})
		if !errors.Is(err, ErrLimit) || !reflect.DeepEqual(got, FindResult{}) {
			t.Fatal("depth exhaustion published partial hits", err)
		}
	})
	t.Run("visited", func(t *testing.T) {
		directory, _, root := fixture(t)
		for index := range MaxFindVisited {
			write(t, filepath.Join(directory, fmt.Sprintf("f%04d", index)), nil)
		}
		got, err := root.Find(context.Background(), FindRequest{".", "absent", "name", 1})
		if err != nil || len(got.Paths) != 0 || got.Truncated {
			t.Fatal("visited boundary", err)
		}
		write(t, filepath.Join(directory, "one-extra"), nil)
		got, err = root.Find(context.Background(), FindRequest{".", "absent", "name", 1})
		if !errors.Is(err, ErrLimit) || !reflect.DeepEqual(got, FindResult{}) {
			t.Fatal("visited budget was treated as match truncation", err)
		}
	})
}

func TestNativeFindRejectsChangedDirectoryAndInvalidNames(t *testing.T) {
	for _, replacement := range []string{"directory", "symlink", "missing"} {
		t.Run(replacement, func(t *testing.T) {
			directory, _, root := fixture(t)
			child := filepath.Join(directory, "child")
			if err := os.Mkdir(child, 0700); err != nil {
				t.Fatal(err)
			}
			native := root.fs.(*linuxRoot)
			actual := native.calls.open
			native.calls.open = func(fd int, name string, how *unix.OpenHow) (int, error) {
				if name == "child" {
					if fd != native.fd || how.Flags&unix.O_NOFOLLOW == 0 || how.Resolve != resolution {
						t.Error("child was not rooted and sealed")
					}
					if err := os.Rename(child, filepath.Join(directory, "old")); err != nil {
						t.Fatal(err)
					}
					switch replacement {
					case "directory":
						if err := os.Mkdir(child, 0700); err != nil {
							t.Fatal(err)
						}
					case "symlink":
						if err := os.Symlink("old", child); err != nil {
							t.Fatal(err)
						}
					}
				}
				return actual(fd, name, how)
			}
			got, err := root.Find(context.Background(), FindRequest{".", "*", "name", 1})
			if !errors.Is(err, ErrConflict) || !reflect.DeepEqual(got, FindResult{}) {
				t.Fatal("replacement accepted", err)
			}
		})
	}
	t.Run("invalid-name", func(t *testing.T) {
		directory, _, root := fixture(t)
		write(t, filepath.Join(directory, string([]byte{255})), nil)
		got, err := root.Find(context.Background(), FindRequest{".", "not-matched", "name", 1})
		if !errors.Is(err, ErrType) || !reflect.DeepEqual(got, FindResult{}) {
			t.Fatal("unmatched invalid name silently skipped", err)
		}
	})
}

func TestNativeFindFailuresAndActualClose(t *testing.T) {
	for _, failure := range []string{"dirents", "length", "statAt", "stat", "close"} {
		t.Run(failure, func(t *testing.T) {
			directory, _, root := fixture(t)
			write(t, filepath.Join(directory, "hit"), nil)
			native := root.fs.(*linuxRoot)
			actualClose := native.calls.close
			closed := 0
			native.calls.close = func(fd int) error {
				if fd == native.fd {
					return actualClose(fd)
				}
				closed++
				err := actualClose(fd)
				if failure == "close" {
					return unix.EINTR
				}
				return err
			}
			switch failure {
			case "dirents":
				native.calls.dirents = func(int, []byte) (int, error) { return 0, unix.EIO }
			case "length":
				native.calls.dirents = func(_ int, b []byte) (int, error) { return len(b) + 1, nil }
			case "statAt":
				native.calls.statAt = func(int, string, *unix.Stat_t, int) error { return unix.ENOENT }
			case "stat":
				native.calls.stat = func(int, *unix.Stat_t) error { return unix.EIO }
			}
			want := ErrUnavailable
			if failure == "statAt" {
				want = ErrConflict
			}
			got, err := root.Find(context.Background(), FindRequest{".", "*", "name", 1})
			if !errors.Is(err, want) || !reflect.DeepEqual(got, FindResult{}) || closed != 1 {
				t.Fatal("failure ownership", err, closed)
			}
		})
	}
}

func TestNativeFindStopWaitsForOriginalTraversalAndClose(t *testing.T) {
	directory, _, root := fixture(t)
	if err := os.Mkdir(filepath.Join(directory, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(directory, "child", "hit"), nil)
	native := root.fs.(*linuxRoot)
	actualOpen, actualRead, actualClose := native.calls.open, native.calls.dirents, native.calls.close
	readEntered, releaseRead := make(chan struct{}), make(chan struct{})
	closeEntered, releaseClose := make(chan struct{}), make(chan struct{})
	var releaseReadOnce, releaseCloseOnce sync.Once
	t.Cleanup(func() {
		releaseReadOnce.Do(func() { close(releaseRead) })
		releaseCloseOnce.Do(func() { close(releaseClose) })
	})
	var childFD int
	var childCloses, rootCloses atomic.Int32
	native.calls.open = func(fd int, name string, how *unix.OpenHow) (int, error) {
		opened, err := actualOpen(fd, name, how)
		if name == "child" && err == nil {
			childFD = opened
		}
		return opened, err
	}
	native.calls.dirents = func(fd int, b []byte) (int, error) {
		if fd == childFD {
			close(readEntered)
			<-releaseRead
		}
		return actualRead(fd, b)
	}
	native.calls.close = func(fd int) error {
		if fd == childFD {
			childCloses.Add(1)
			close(closeEntered)
			<-releaseClose
		}
		if fd == native.fd {
			rootCloses.Add(1)
		}
		return actualClose(fd)
	}
	done := make(chan error, 1)
	go func() {
		got, err := root.Find(context.Background(), FindRequest{".", "*", "name", 1})
		if !reflect.DeepEqual(got, FindResult{}) {
			done <- errors.New("partial canceled result")
			return
		}
		done <- err
	}()
	waitSignal(t, readEntered)
	root.Stop()
	assertHeld := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
		defer cancel()
		if err := root.Drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("drain passed original call", err)
		}
		var st unix.Stat_t
		if unix.Fstat(childFD, &st) != nil || rootCloses.Load() != 0 {
			t.Fatal("original fd closed early")
		}
		root.mu.Lock()
		active := len(root.active)
		root.mu.Unlock()
		if active != 1 {
			t.Fatal("traversal admission released early", active)
		}
	}
	assertHeld()
	releaseReadOnce.Do(func() { close(releaseRead) })
	waitSignal(t, closeEntered)
	assertHeld()
	releaseCloseOnce.Do(func() { close(releaseClose) })
	if err := waitError(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	if err := root.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if childCloses.Load() != 1 || rootCloses.Load() != 1 {
		t.Fatal("close not exactly once")
	}
}

func TestNativeFindIndependentDirectoryOffsets(t *testing.T) {
	directory, _, root := fixture(t)
	for _, name := range []string{"a", "b", "中"} {
		write(t, filepath.Join(directory, name), nil)
	}
	var workers sync.WaitGroup
	for range 6 {
		workers.Go(func() {
			for range 8 {
				got, err := root.Find(context.Background(), FindRequest{".", "*", "name", 3})
				if err != nil || got.Truncated || strings.Join(got.Paths, ",") != "a,b,中" {
					t.Error("shared directory offset", err)
				}
			}
		})
	}
	workers.Wait()
}
