//go:build linux || darwin

package identity

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func privatePath(t *testing.T) string {
	t.Helper()
	// The production contract rejects a world-writable ancestor, including /tmp.
	// Create this owned fixture under the checked-out package, not the ambient
	// temporary root. No privileged ownership changes or relaxed check are used.
	dir, err := os.MkdirTemp(".", ".identity-test-")
	if err != nil {
		t.Fatal(err)
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "identity.json")
}

func TestIdentityFileLifecycleAndLock(t *testing.T) {
	path := privatePath(t)
	if _, err := ReadOnly(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatal("read-only probe created lock")
	}
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.Load(); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	v := pending(t)
	if err = f.Save(v); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path); err != ErrLocked {
		t.Fatal("second owner admitted", err)
	}
	var before, after unix.Stat_t
	if err = unix.Stat(path+".lock", &before); err != nil {
		t.Fatal(err)
	}
	active, _ := v.AsActive()
	if err = f.Save(active); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadOnly(path); err != nil || got != active {
		t.Fatal("offline snapshot", err)
	}
	if got, err := f.Load(); err != nil || got != active {
		t.Fatal("active persistence", err)
	}
	if err = unix.Stat(path+".lock", &after); err != nil || before.Ino != after.Ino || before.Dev != after.Dev {
		t.Fatal("rename replaced lock inode")
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Save(v); err != ErrClosed {
		t.Fatal("save after close", err)
	}
	f2, err := Open(path)
	if err != nil {
		t.Fatal("lock not released", err)
	}
	if err = f2.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityFileUnsafePaths(t *testing.T) {
	for _, variant := range []string{"file_mode", "parent_mode", "ancestor_mode", "symlink", "ancestor_symlink", "hardlink", "directory", "fifo", "lock_symlink", "lock_mode", "lock_replaced"} {
		t.Run(variant, func(t *testing.T) {
			path := privatePath(t)
			raw, _ := encodeFile(pending(t))
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			var actionErr error
			switch variant {
			case "file_mode":
				actionErr = os.Chmod(path, 0644)
			case "parent_mode":
				actionErr = os.Chmod(filepath.Dir(path), 0750)
			case "ancestor_mode":
				parent := filepath.Dir(path)
				nested := filepath.Join(parent, "private")
				if err := os.Mkdir(nested, 0700); err != nil {
					t.Fatal(err)
				}
				actionErr = os.Chmod(parent, 0777)
				path = filepath.Join(nested, "identity.json")
			case "symlink":
				if err := os.Rename(path, path+".real"); err != nil {
					t.Fatal(err)
				}
				actionErr = os.Symlink(path+".real", path)
			case "ancestor_symlink":
				link := filepath.Join(filepath.Dir(path), "link")
				actionErr = os.Symlink(filepath.Dir(path), link)
				path = filepath.Join(link, "identity.json")
			case "hardlink":
				actionErr = os.Link(path, path+".link")
			case "directory", "fifo":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if variant == "directory" {
					actionErr = os.Mkdir(path, 0600)
				} else {
					actionErr = unix.Mkfifo(path, 0600)
				}
			case "lock_symlink":
				actionErr = os.Symlink(path, path+".lock")
			case "lock_mode":
				actionErr = os.WriteFile(path+".lock", nil, 0644)
				if actionErr == nil {
					actionErr = os.Chmod(path+".lock", 0644)
				}
			}
			if actionErr != nil {
				t.Fatal(actionErr)
			}
			f, err := Open(path)
			if err == nil {
				defer f.Close()
				if variant == "lock_replaced" {
					if err := os.Remove(path + ".lock"); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path+".lock", nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if _, err = f.Load(); err != ErrUnsafeFile {
					t.Fatal("unsafe read accepted", err)
				}
				if err = f.Save(pending(t)); err != ErrUnsafeFile {
					t.Fatal("unsafe replacement accepted", err)
				}
			} else if err != ErrUnsafeFile {
				t.Fatal("wrong safe error", err)
			}
		})
	}
}

type failedPersistence struct {
	diskWrite
	fail  string
	syncs int
}

func (f *failedPersistence) write(file *os.File, raw []byte) error {
	if f.fail == "write" {
		_, _ = file.Write(raw[:3])
		return io.ErrShortWrite
	}
	return f.diskWrite.write(file, raw)
}
func (f *failedPersistence) sync(file *os.File) error {
	f.syncs++
	if f.fail == "file_sync" && f.syncs == 1 || f.fail == "directory_sync" && f.syncs == 2 {
		return unix.EIO
	}
	return f.diskWrite.sync(file)
}
func (f *failedPersistence) rename(fd int, old, new string) error {
	if f.fail == "rename" {
		return unix.EIO
	}
	return f.diskWrite.rename(fd, old, new)
}

func TestIdentityAtomicFailurePreservesRecoverableFile(t *testing.T) {
	for _, failure := range []string{"write", "file_sync", "rename", "directory_sync"} {
		t.Run(failure, func(t *testing.T) {
			path := privatePath(t)
			f, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			v := pending(t)
			if err = f.Save(v); err != nil {
				t.Fatal(err)
			}
			active, _ := v.AsActive()
			raw, _ := encodeFile(active)
			if err = f.replace(raw, &failedPersistence{fail: failure}); err != ErrUnavailable {
				t.Fatal("failure reported as saved", err)
			}
			want := v
			if failure == "directory_sync" {
				want = active
			}
			if got, err := f.Load(); err != nil || got != want {
				t.Fatal("lost complete recovery image", err)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 2 {
				t.Fatal("own temporary file leaked", err)
			}
		})
	}
}

func TestIdentityConcurrentUseAndCorruptRecovery(t *testing.T) {
	path := privatePath(t)
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	v := pending(t)
	if err = f.Save(v); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 8 {
				if err := f.Save(v); err != nil {
					t.Error(err)
					return
				}
				if got, err := f.Load(); err != nil || got != v {
					t.Error("concurrent read", err)
					return
				}
			}
		})
	}
	wg.Wait()
	if err = os.WriteFile(path, bytes.Repeat([]byte{'x'}, MaxFileBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := f.Load(); err != ErrInvalid || got != (Identity{}) {
		t.Fatal("oversized corrupt credential accepted")
	}
	// Save does not infer a new key from a corrupt file. The enrollment owner is
	// responsible for rejecting regeneration after a failed Load.
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}
