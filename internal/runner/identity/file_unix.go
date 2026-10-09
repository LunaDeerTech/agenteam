//go:build linux || darwin

package identity

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// LockedFile owns the stable sidecar lock until Close. The caller releases it
// only after all users of the device identity, including the connection, join.
// None of its methods disclose the path or credentials through an error.
type LockedFile struct {
	mu     sync.Mutex
	parent *os.File
	lock   *os.File
	name   string
	closed bool
}

func (f *LockedFile) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_identity_file") }
func (f *LockedFile) MarshalJSON() ([]byte, error) { return []byte(`"runner_identity_file"`), nil }
func (f *LockedFile) LogValue() slog.Value         { return slog.StringValue("runner_identity_file") }

// Open never creates directories. A pre-existing private parent is an explicit
// deployment prerequisite; each ancestor is opened without following symlinks.
func Open(path string) (*LockedFile, error) {
	parent, name, err := openParent(path)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(parent.Fd()), name+".lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		_ = parent.Close()
		return nil, fileError(err)
	}
	lock := os.NewFile(uintptr(fd), "runner-identity-lock")
	f := &LockedFile{parent: parent, lock: lock, name: name}
	if err = safeRegular(fd); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, ErrUnavailable
	}
	if err = f.validLock(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func openParent(path string) (*os.File, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(path) > 4096 || strings.ContainsAny(path, "\\\x00") || path == "/" {
		return nil, "", ErrUnsafeFile
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	name := parts[len(parts)-1]
	// Leave room for the stable lock suffix and bounded temporary basename.
	if len(name) == 0 || len(name) > 240 {
		return nil, "", ErrUnsafeFile
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", fileError(err)
	}
	for i := 0; ; i++ {
		last := i == len(parts)-1
		if err = safeDirectory(fd, last); err != nil {
			_ = unix.Close(fd)
			return nil, "", err
		}
		if last {
			return os.NewFile(uintptr(fd), "runner-identity-directory"), name, nil
		}
		next, openErr := unix.Openat(fd, parts[i], unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			return nil, "", fileError(openErr)
		}
		fd = next
	}
}

func safeDirectory(fd int, private bool) error {
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil {
		return ErrUnavailable
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Mode&0022 != 0 || (st.Uid != 0 && st.Uid != uint32(os.Geteuid())) {
		return ErrUnsafeFile
	}
	if private && (st.Uid != uint32(os.Geteuid()) || st.Mode&07777 != 0700) {
		return ErrUnsafeFile
	}
	return nil
}

func safeRegular(fd int) error {
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil {
		return ErrUnavailable
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
		return ErrUnsafeFile
	}
	return nil
}

func fileError(err error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
		return ErrUnsafeFile
	}
	return ErrUnavailable
}

// The directory and the lock inode remain the originally admitted ones. In
// particular, an unlinked/replaced lock cannot silently create two owners.
func (f *LockedFile) validLock() error {
	if f.closed || f.parent == nil || f.lock == nil {
		return ErrClosed
	}
	if err := safeDirectory(int(f.parent.Fd()), true); err != nil {
		return err
	}
	if err := safeRegular(int(f.lock.Fd())); err != nil {
		return err
	}
	var held, named unix.Stat_t
	if unix.Fstat(int(f.lock.Fd()), &held) != nil {
		return ErrUnavailable
	}
	if unix.Fstatat(int(f.parent.Fd()), f.name+".lock", &named, unix.AT_SYMLINK_NOFOLLOW) != nil || held.Dev != named.Dev || held.Ino != named.Ino {
		return ErrUnsafeFile
	}
	return nil
}

func (f *LockedFile) Load() (Identity, error) {
	if f == nil {
		return Identity{}, ErrClosed
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.validLock(); err != nil {
		return Identity{}, err
	}
	return readAt(f.parent, f.name)
}

// ReadOnly validates and reads an atomic snapshot without creating a sidecar or
// acquiring ownership. It is for offline --check-config, never for Runner Run.
func ReadOnly(path string) (Identity, error) {
	parent, name, err := openParent(path)
	if err != nil {
		return Identity{}, err
	}
	defer parent.Close()
	return readAt(parent, name)
}

func readAt(parent *os.File, name string) (Identity, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if errors.Is(err, unix.ENOENT) {
		return Identity{}, os.ErrNotExist
	}
	if err != nil {
		return Identity{}, fileError(err)
	}
	file := os.NewFile(uintptr(fd), "runner-identity")
	defer file.Close()
	if err = safeRegular(fd); err != nil {
		return Identity{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	defer clear(raw)
	if err != nil {
		return Identity{}, ErrUnavailable
	}
	return decodeFile(raw)
}

func (f *LockedFile) Save(v Identity) error {
	if f == nil {
		return ErrClosed
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.validLock(); err != nil {
		return err
	}
	raw, err := encodeFile(v)
	if err != nil {
		return err
	}
	defer clear(raw)
	return f.replace(raw, diskWrite{})
}

// Only the atomic persistence steps are injectable in package tests, so real
// file/permission/lock checks still execute when testing a failed write/fsync.
type persistence interface {
	write(*os.File, []byte) error
	sync(*os.File) error
	rename(int, string, string) error
}
type diskWrite struct{}

func (diskWrite) write(f *os.File, b []byte) error {
	n, err := f.Write(b)
	if err == nil && n != len(b) {
		return io.ErrShortWrite
	}
	return err
}
func (diskWrite) sync(f *os.File) error                { return f.Sync() }
func (diskWrite) rename(fd int, old, new string) error { return unix.Renameat(fd, old, fd, new) }

func (f *LockedFile) replace(raw []byte, ioOps persistence) error {
	fd := int(f.parent.Fd())
	var st unix.Stat_t
	if err := unix.Fstatat(fd, f.name, &st, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
			return ErrUnsafeFile
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return fileError(err)
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ErrUnavailable
	}
	temporary := ".runner-identity-" + hex.EncodeToString(nonce[:]) + ".tmp"
	tempFD, err := unix.Openat(fd, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return fileError(err)
	}
	file := os.NewFile(uintptr(tempFD), "runner-identity-pending-write")
	defer file.Close()
	defer unix.Unlinkat(fd, temporary, 0)
	if safeRegular(tempFD) != nil || ioOps.write(file, raw) != nil || ioOps.sync(file) != nil {
		return ErrUnavailable
	}
	if file.Close() != nil {
		return ErrUnavailable
	}
	if f.validLock() != nil {
		return ErrUnsafeFile
	}
	if ioOps.rename(fd, temporary, f.name) != nil {
		return ErrUnavailable
	}
	// A failure here means durability is unknown; the new complete file may
	// already be visible. It is never reported as a successful enrollment.
	if ioOps.sync(f.parent) != nil {
		return ErrUnavailable
	}
	return nil
}

func (f *LockedFile) Close() error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	var err error
	if f.parent != nil {
		err = f.parent.Close()
	}
	if f.lock != nil {
		if e := f.lock.Close(); e != nil {
			err = e
		}
	}
	if err != nil {
		return ErrUnavailable
	}
	return nil
}
