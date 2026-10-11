//go:build linux

package workspace

import (
	"context"
	"errors"
	"os"

	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
	"golang.org/x/sys/unix"
)

const resolution = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV

// Instance-local seams permit actual-return and failure controls. Production
// construction always binds the real syscalls; there is no mutable registry.
type linuxCalls struct {
	open    func(int, string, *unix.OpenHow) (int, error)
	mkdir   func(int, string, uint32) error
	stat    func(int, *unix.Stat_t) error
	close   func(int) error
	newRoot func(*os.File) (readerRoot, error)
}

func realLinuxCalls() linuxCalls {
	return linuxCalls{unix.Openat2, unix.Mkdirat, unix.Fstat, unix.Close, func(f *os.File) (readerRoot, error) { return filesystem.NewRoot(f) }}
}

type linuxRoot struct {
	fd    int
	uid   uint32
	calls linuxCalls
}

func openNativeRoot(directory *os.File) (nativeRoot, error) {
	return openLinuxRoot(directory, realLinuxCalls())
}
func openLinuxRoot(directory *os.File, calls linuxCalls) (nativeRoot, error) {
	if directory == nil {
		return nil, ErrInvalid
	}
	raw, err := directory.SyscallConn()
	if err != nil {
		return nil, ErrInvalid
	}
	fd := -1
	var dupErr error
	err = raw.Control(func(original uintptr) { fd, dupErr = unix.FcntlInt(original, unix.F_DUPFD_CLOEXEC, 0) })
	if err != nil || dupErr != nil {
		if fd >= 0 {
			_ = calls.close(fd)
		}
		return nil, ErrUnavailable
	}
	r := &linuxRoot{fd: fd, uid: uint32(os.Geteuid()), calls: calls}
	var st unix.Stat_t
	if err = calls.stat(fd, &st); err != nil {
		_ = r.close()
		return nil, ErrUnavailable
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = r.close()
		return nil, ErrType
	}
	if st.Uid != r.uid || st.Mode&0022 != 0 {
		_ = r.close()
		return nil, ErrPermission
	}
	probe, err := r.open(".")
	if err != nil {
		_ = r.close()
		return nil, err
	}
	if err = calls.close(probe); err != nil {
		_ = r.close()
		return nil, ErrUnavailable
	}
	return r, nil
}
func (r *linuxRoot) open(path string) (int, error) {
	fd, err := r.calls.open(r.fd, path, &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: resolution})
	if err != nil {
		return -1, nativeError(err)
	}
	return fd, nil
}
func (r *linuxRoot) close() error {
	if err := r.calls.close(r.fd); err != nil {
		return ErrUnavailable
	}
	return nil
}
func (r *linuxRoot) owned(fd int) (unix.Stat_t, error) {
	var st unix.Stat_t
	if err := r.calls.stat(fd, &st); err != nil {
		return st, nativeError(err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return st, ErrType
	}
	if st.Uid != r.uid || st.Mode&07777 != 0700 {
		return st, ErrPermission
	}
	return st, nil
}
func same(a, b unix.Stat_t) bool { return a.Dev == b.Dev && a.Ino == b.Ino }
func (r *linuxRoot) confirm(path string, expected unix.Stat_t) error {
	fd, err := r.open(path)
	if err != nil {
		return ErrConflict
	}
	actual, err := r.owned(fd)
	closeErr := r.calls.close(fd)
	if closeErr != nil {
		return ErrUnavailable
	}
	if err != nil {
		return err
	}
	if !same(expected, actual) {
		return ErrConflict
	}
	return nil
}
func (r *linuxRoot) ensure(ctx context.Context, id Identity) (root readerRoot, err error) {
	// One original call owns all intermediate descriptors until actual close.
	var fds []int
	defer func() {
		for i := len(fds) - 1; i >= 0; i-- {
			if closeErr := r.calls.close(fds[i]); closeErr != nil {
				err = ErrUnavailable
			}
		}
		// A constructed Root (even alongside an error) transfers to the
		// registered Manager.Ensure caller. That single owner joins its actual
		// Drain and retains the close result before retiring the operation.
	}()
	segments := []string{string(id.ProjectID), string(id.AgentID), id.Name}
	paths := make([]string, 0, 3)
	observed := make([]unix.Stat_t, 0, 3)
	path := ""
	parent := r.fd
	for index, name := range segments {
		if err = contextError(ctx); err != nil {
			return nil, err
		}
		if index > 0 {
			if err = r.confirm(paths[index-1], observed[index-1]); err != nil {
				return nil, err
			}
			path += "/"
		}
		path += name
		fd, openErr := r.open(path)
		if errors.Is(openErr, errNotFound) {
			// Only a single validated name is passed to mkdirat on its verified parent.
			if mkdirErr := r.calls.mkdir(parent, name, 0700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				return nil, nativeError(mkdirErr)
			}
			if err = contextError(ctx); err != nil {
				return nil, err
			}
			fd, openErr = r.open(path)
		}
		if openErr != nil {
			return nil, openErr
		}
		fds = append(fds, fd)
		st, statErr := r.owned(fd)
		if statErr != nil {
			return nil, statErr
		}
		paths = append(paths, path)
		observed = append(observed, st)
		parent = fd
	}
	for index, path := range paths {
		if err = contextError(ctx); err != nil {
			return nil, err
		}
		if err = r.confirm(path, observed[index]); err != nil {
			return nil, err
		}
	}
	// Transfer just the final temporary descriptor to os.File. NewRoot duplicates
	// it during this call; the File closes once and never exports a host path.
	final := fds[len(fds)-1]
	fds = fds[:len(fds)-1]
	file := os.NewFile(uintptr(final), "workspace")
	root, err = r.calls.newRoot(file)
	if closeErr := file.Close(); closeErr != nil {
		err = ErrUnavailable
	}
	if err != nil {
		return root, err
	}
	if err = contextError(ctx); err != nil {
		return root, err
	}
	return root, nil
}

var errNotFound = errors.New("workspace: not found")

func nativeError(err error) error {
	switch {
	case errors.Is(err, unix.EXDEV), errors.Is(err, unix.ELOOP):
		return ErrOutside
	case errors.Is(err, unix.ENOENT):
		return errNotFound
	case errors.Is(err, unix.ENOTDIR), errors.Is(err, unix.EISDIR):
		return ErrType
	case errors.Is(err, unix.EAGAIN), errors.Is(err, unix.ESTALE):
		return ErrConflict
	case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EOPNOTSUPP):
		return ErrUnsupported
	case errors.Is(err, unix.EACCES), errors.Is(err, unix.EPERM):
		return ErrPermission
	default:
		return ErrUnavailable
	}
}
