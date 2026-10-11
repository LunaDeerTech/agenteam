//go:build linux

package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
	"golang.org/x/sys/unix"
)

func fixture(t *testing.T) (string, *os.File, *Manager) {
	t.Helper()
	path := t.TempDir()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(file)
	if err != nil {
		t.Fatal("new manager", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := m.Drain(ctx); err != nil {
			t.Error("fixture drain", err)
		}
		_ = file.Close()
	})
	return path, file, m
}
func physical(base string, id Identity) string {
	return filepath.Join(base, string(id.ProjectID), string(id.AgentID), id.Name)
}
func ensure(t *testing.T, m *Manager, id Identity) *Handle {
	t.Helper()
	h, err := m.Ensure(context.Background(), id)
	if err != nil {
		t.Fatal("ensure", err)
	}
	return h
}
func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func read(h *Handle) (filesystem.ReadResult, error) {
	return h.Read(context.Background(), filesystem.ReadRequest{Path: "proof.txt", Length: 100, Encoding: "utf8"})
}
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("barrier not reached")
	}
}
func receive(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("original call did not return")
		return nil
	}
}
func deadline(t *testing.T, fn func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := fn(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("actual-return barrier escaped: %v", err)
	}
}

func TestEnsureRealFilesystemAndIsolation(t *testing.T) {
	base, borrowed, m := fixture(t)
	id := identity()
	h := ensure(t, m, id)
	if h.Metadata() != id {
		t.Fatal("logical metadata")
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	} // Manager owns its duplicate.
	write(t, filepath.Join(physical(base, id), "proof.txt"), "original")
	got, err := read(h)
	if err != nil || got.Data != "original" {
		t.Fatal("real read", err)
	}
	listed, err := h.List(context.Background(), filesystem.ListRequest{Path: ".", Limit: 5})
	if err != nil || len(listed.Entries) != 1 || listed.Entries[0].Name != "proof.txt" {
		t.Fatal("real list", err)
	}
	found, err := h.Find(context.Background(), filesystem.FindRequest{Path: ".", Pattern: "*.txt", Match: "name", Limit: 5})
	if err != nil || len(found.Paths) != 1 || found.Paths[0] != "proof.txt" {
		t.Fatal("real find", err)
	}
	for index := 0; index < 2; index++ {
		other := id
		if index == 0 {
			other.AgentID = "01900000-0000-7000-8000-000000000003"
		} else {
			other.ProjectID = "01900000-0000-7000-8000-000000000004"
		}
		second := ensure(t, m, other)
		if result, err := read(second); !errors.Is(err, filesystem.ErrNotFound) || result != (filesystem.ReadResult{}) {
			t.Fatal("cross identity read")
		}
		write(t, filepath.Join(physical(base, other), "proof.txt"), "other")
		if result, err := read(h); err != nil || result.Data != "original" {
			t.Fatal("identity collision")
		}
	}
	for _, path := range []string{filepath.Join(base, string(id.ProjectID)), filepath.Join(base, string(id.ProjectID), string(id.AgentID)), physical(base, id)} {
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != 0700 {
			t.Fatal("directory mode", err)
		}
	}
	if err := h.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(physical(base, id)); err != nil {
		t.Fatal("drain deleted workspace")
	}
}
func TestConcurrentEnsureAndPinnedHandle(t *testing.T) {
	base, _, m := fixture(t)
	id := identity()
	var wg sync.WaitGroup
	handles := make(chan *Handle, 16)
	errs := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := m.Ensure(context.Background(), id)
			if err != nil {
				errs <- err
			} else {
				handles <- h
			}
		}()
	}
	wg.Wait()
	close(handles)
	close(errs)
	for err := range errs {
		t.Fatal("concurrent ensure", err)
	}
	unique := map[*Handle]bool{}
	var first *Handle
	for h := range handles {
		unique[h] = true
		if first == nil {
			first = h
		}
	}
	if len(unique) != 16 {
		t.Fatal("not independent handles")
	}
	write(t, filepath.Join(physical(base, id), "proof.txt"), "old")
	if err := os.Rename(physical(base, id), physical(base, id)+"-old"); err != nil {
		t.Fatal(err)
	}
	fresh := ensure(t, m, id)
	write(t, filepath.Join(physical(base, id), "proof.txt"), "new")
	if r, e := read(first); e != nil || r.Data != "old" {
		t.Fatal("handle lost pinned inode", e)
	}
	if r, e := read(fresh); e != nil || r.Data != "new" {
		t.Fatal("new ensure did not map current directory", e)
	}
	if err := first.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := read(first); !errors.Is(err, ErrClosed) {
		t.Fatal("closed handle read")
	}
	if r, e := read(fresh); e != nil || r.Data != "new" {
		t.Fatal("independent handle closed", e)
	}
}
func TestRejectUnsafeDirectoriesAndInputs(t *testing.T) {
	for _, kind := range []string{"symlink", "file", "mode"} {
		for layer := 0; layer < 3; layer++ {
			t.Run(fmt.Sprintf("%s-layer%d", kind, layer), func(t *testing.T) {
				base, _, m := fixture(t)
				id := identity()
				parts := []string{string(id.ProjectID), string(id.AgentID), id.Name}
				parent := base
				for i := 0; i < layer; i++ {
					parent = filepath.Join(parent, parts[i])
					if err := os.Mkdir(parent, 0700); err != nil {
						t.Fatal(err)
					}
				}
				bad := filepath.Join(parent, parts[layer])
				switch kind {
				case "symlink":
					target := filepath.Join(base, "contained")
					if err := os.Mkdir(target, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, bad); err != nil {
						t.Fatal(err)
					}
				case "file":
					write(t, bad, "private-canary")
				case "mode":
					if err := os.Mkdir(bad, 0755); err != nil {
						t.Fatal(err)
					}
					// The test process may have umask 0077. Establish the intended
					// pre-existing unsafe mode explicitly; the product never chmods.
					if err := os.Chmod(bad, 0755); err != nil {
						t.Fatal(err)
					}
					if st, err := os.Stat(bad); err != nil || st.Mode().Perm() != 0755 {
						t.Fatal("unsafe fixture mode not established", err)
					}
				}
				h, err := m.Ensure(context.Background(), id)
				if h != nil || err == nil {
					t.Fatal("unsafe directory accepted")
				}
				if strings.Contains(err.Error(), base) || strings.Contains(err.Error(), "private-canary") {
					t.Fatal("path leaked")
				}
			})
		}
	}
	base, _, m := fixture(t)
	for _, name := range []string{"../escape", "a/b", "NUL", "a.", ""} {
		id := identity()
		id.Name = name
		if h, e := m.Ensure(context.Background(), id); h != nil || !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid name")
		}
	}
	names, err := os.ReadDir(base)
	if err != nil || len(names) != 0 {
		t.Fatal("invalid input had side effects")
	}
	outside := t.TempDir()
	id := identity()
	h := ensure(t, m, id)
	if err := os.Symlink(outside, filepath.Join(physical(base, id), "escape")); err != nil {
		t.Fatal(err)
	}
	if got, e := h.List(context.Background(), filesystem.ListRequest{Path: "escape", Limit: 1}); !errors.Is(e, filesystem.ErrOutside) || got.Entries != nil {
		t.Fatal("escaping read", e)
	}
}
func TestConstructorAndNativeFailures(t *testing.T) {
	if m, err := New(nil); m != nil || !errors.Is(err, ErrInvalid) {
		t.Fatal("nil root")
	}
	path := t.TempDir()
	file, err := os.Create(filepath.Join(path, "file"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if m, err := New(file); m != nil || !errors.Is(err, ErrType) {
		t.Fatal("file root")
	}
	dir, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	calls := realLinuxCalls()
	calls.open = func(int, string, *unix.OpenHow) (int, error) { return -1, unix.ENOSYS }
	if root, err := openLinuxRoot(dir, calls); root != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("weaker fallback", err)
	}
	if _, err := dir.Stat(); err != nil {
		t.Fatal("borrowed directory closed")
	}
	calls = realLinuxCalls()
	stat := calls.stat
	calls.stat = func(fd int, out *unix.Stat_t) error {
		if err := stat(fd, out); err != nil {
			return err
		}
		out.Uid++
		return nil
	}
	if root, err := openLinuxRoot(dir, calls); root != nil || !errors.Is(err, ErrPermission) {
		t.Fatal("foreign owner accepted")
	}
	if err := dir.Close(); err != nil {
		t.Fatal(err)
	}
	if root, err := New(dir); root != nil || err == nil {
		t.Fatal("closed borrowed fd")
	}
}

// The wrapper delegates real filesystem calls, holding only their return. These
// controls do not claim a kernel syscall failed or a foreign process stopped.
type gatedRoot struct {
	readerRoot
	readEntered, readRelease   chan struct{}
	closeEntered, closeRelease chan struct{}
	closeCalls                 atomic.Int32
}

func (g *gatedRoot) Read(ctx context.Context, r filesystem.ReadRequest) (filesystem.ReadResult, error) {
	result, err := g.readerRoot.Read(ctx, r)
	if g.readEntered != nil {
		close(g.readEntered)
		<-g.readRelease
	}
	return result, err
}
func (g *gatedRoot) Drain(ctx context.Context) error {
	g.closeCalls.Add(1)
	err := g.readerRoot.Drain(ctx)
	if g.closeEntered != nil {
		close(g.closeEntered)
		<-g.closeRelease
	}
	return err
}
func TestHandleAndManagerWaitActualReadAndClose(t *testing.T) {
	base, _, m := fixture(t)
	id := identity()
	h := ensure(t, m, id)
	write(t, filepath.Join(physical(base, id), "proof.txt"), "held")
	gated := &gatedRoot{readerRoot: h.root, readEntered: make(chan struct{}), readRelease: make(chan struct{}), closeEntered: make(chan struct{}), closeRelease: make(chan struct{})}
	h.root = gated
	readDone := make(chan error, 1)
	go func() {
		r, e := read(h)
		if r != (filesystem.ReadResult{}) {
			readDone <- fmt.Errorf("late read published")
			return
		}
		readDone <- e
	}()
	await(t, gated.readEntered)
	m.Stop()
	if _, e := m.Ensure(context.Background(), id); !errors.Is(e, ErrClosed) {
		t.Fatal("stop admitted ensure")
	}
	if _, e := read(h); !errors.Is(e, ErrClosed) {
		t.Fatal("stop admitted read")
	}
	deadline(t, m.Drain)
	deadline(t, h.Drain)
	close(gated.readRelease)
	if e := receive(t, readDone); !errors.Is(e, context.Canceled) {
		t.Fatal("held read cancellation", e)
	}
	handleDone := make(chan error, 1)
	go func() { handleDone <- h.Drain(context.Background()) }()
	await(t, gated.closeEntered)
	deadline(t, m.Drain)
	deadline(t, h.Drain)
	m.mu.Lock()
	_, tracked := m.handles[h]
	m.mu.Unlock()
	if !tracked {
		t.Fatal("handle retired before actual close return")
	}
	close(gated.closeRelease)
	if e := receive(t, handleDone); e != nil {
		t.Fatal(e)
	}
	if err := m.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gated.closeCalls.Load() != 1 {
		t.Fatal("multiple close owners")
	}
}
func TestUnpublishedEnsureCleanupJoins(t *testing.T) {
	_, _, m := fixture(t)
	native := m.native.(*linuxRoot)
	newRoot := native.calls.newRoot
	created := make(chan struct{})
	release := make(chan struct{})
	closing := make(chan struct{})
	closed := make(chan struct{})
	var gated *gatedRoot
	native.calls.newRoot = func(f *os.File) (readerRoot, error) {
		root, e := newRoot(f)
		if e != nil {
			return nil, e
		}
		gated = &gatedRoot{readerRoot: root, closeEntered: closing, closeRelease: closed}
		close(created)
		<-release
		return gated, nil
	}
	done := make(chan error, 1)
	go func() {
		h, e := m.Ensure(context.Background(), identity())
		if h != nil {
			done <- fmt.Errorf("unpublished handle escaped")
			return
		}
		done <- e
	}()
	await(t, created)
	m.Stop()
	deadline(t, m.Drain)
	close(release)
	await(t, closing)
	deadline(t, m.Drain)
	close(closed)
	if e := receive(t, done); !errors.Is(e, context.Canceled) {
		t.Fatal("ensure stop", e)
	}
	if err := m.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gated.closeCalls.Load() != 1 {
		t.Fatal("unpublished root not closed once")
	}
}
func TestEnsureReplacementAndCancellationResidue(t *testing.T) {
	t.Run("replace", func(t *testing.T) {
		base, _, m := fixture(t)
		id := identity()
		ensure(t, m, id)
		native := m.native.(*linuxRoot)
		actual := native.calls.open
		var opens int
		target := string(id.ProjectID) + "/" + string(id.AgentID) + "/" + id.Name
		native.calls.open = func(fd int, path string, how *unix.OpenHow) (int, error) {
			if path == target {
				opens++
				if opens == 2 {
					if err := os.Rename(physical(base, id), physical(base, id)+"-old"); err != nil {
						return -1, err
					}
					if err := os.Mkdir(physical(base, id), 0700); err != nil {
						return -1, err
					}
				}
			}
			return actual(fd, path, how)
		}
		h, err := m.Ensure(context.Background(), id)
		if h != nil || !errors.Is(err, ErrConflict) {
			t.Fatal("replacement accepted", err)
		}
	})
	t.Run("cancel-after-mkdir", func(t *testing.T) {
		base, _, m := fixture(t)
		native := m.native.(*linuxRoot)
		actual := native.calls.mkdir
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		native.calls.mkdir = func(fd int, name string, mode uint32) error { err := actual(fd, name, mode); cancel(); return err }
		h, err := m.Ensure(ctx, identity())
		if h != nil || !errors.Is(err, context.Canceled) {
			t.Fatal("cancel result", err)
		}
		if _, err := os.Stat(filepath.Join(base, string(identity().ProjectID))); err != nil {
			t.Fatal("created directory was removed", err)
		}
		if _, err := os.Stat(physical(base, identity())); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("continued after cancel")
		}
	})
}
func TestManagerRootCloseOwnerAndError(t *testing.T) {
	// A held actual close return retains sole ownership; timeout observers do not
	// close a reused descriptor or turn a close error into successful Drain.
	_, _, m := fixture(t)
	native := m.native.(*linuxRoot)
	actual := native.calls.close
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	native.calls.close = func(fd int) error {
		if fd != native.fd {
			return actual(fd)
		}
		calls.Add(1)
		err := actual(fd)
		close(entered)
		<-release
		return err
	}
	first := make(chan error, 1)
	go func() { first <- m.Drain(context.Background()) }()
	await(t, entered)
	deadline(t, m.Drain)
	close(release)
	if err := receive(t, first); err != nil {
		t.Fatal(err)
	}
	if err := m.Drain(context.Background()); err != nil || calls.Load() != 1 {
		t.Fatal("close owner", err)
	}

	// Separate root avoids cleanup treating the intentionally retained failure as
	// unexpected. The real close occurs, then its error is safely projected.
	file, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	other, err := New(file)
	if err != nil {
		t.Fatal(err)
	}
	n := other.native.(*linuxRoot)
	closeActual := n.calls.close
	var count int
	n.calls.close = func(fd int) error { count++; _ = closeActual(fd); return unix.EINTR }
	for range 2 {
		if err := other.Drain(context.Background()); !errors.Is(err, ErrUnavailable) {
			t.Fatal("close failure accepted", err)
		}
	}
	if count != 1 {
		t.Fatal("Linux EINTR close retried")
	}
}

// The underlying real Root has actually drained. Only its returned result is
// decorated to distinguish joining a close from successfully closing it.
type failedDrainRoot struct {
	readerRoot
	calls atomic.Int32
}

func (r *failedDrainRoot) Drain(ctx context.Context) error {
	r.calls.Add(1)
	if err := r.readerRoot.Drain(ctx); err != nil {
		return err
	}
	return ErrUnavailable
}
func TestUnpublishedEnsureRetainsCloseFailure(t *testing.T) {
	file, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	m, err := New(file)
	if err != nil {
		t.Fatal(err)
	}
	native := m.native.(*linuxRoot)
	actual := native.calls.newRoot
	created := make(chan struct{})
	release := make(chan struct{})
	var failed *failedDrainRoot
	native.calls.newRoot = func(f *os.File) (readerRoot, error) {
		root, err := actual(f)
		if err != nil {
			return nil, err
		}
		failed = &failedDrainRoot{readerRoot: root}
		close(created)
		<-release
		return failed, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		h, err := m.Ensure(ctx, identity())
		if h != nil {
			done <- fmt.Errorf("cancelled handle published")
			return
		}
		done <- err
	}()
	await(t, created)
	cancel()
	close(release)
	if err := receive(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal("ensure did not retain cancellation", err)
	}
	// No outstanding syscall remains. A nil Drain would lose a real returned
	// close failure even though Ensure correctly suppressed its Handle.
	if err := m.Drain(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Errorf("unpublished close failure lost: %v", err)
	}
	if failed.calls.Load() != 1 {
		t.Errorf("unpublished close called %d times", failed.calls.Load())
	}
}
