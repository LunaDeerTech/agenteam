//go:build linux

package filesystem

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"golang.org/x/sys/unix"
)

// These per-call seams are private. Production always uses the real syscalls;
// tests may hold an actual call's return without introducing a public hook.
type linuxWriteCalls struct {
	create func(int, string, *unix.OpenHow) (int, error)
	write  func(int, []byte) (int, error)
	sync   func(int) error
	chmod  func(int, uint32) error
	rename func(int, string, int, string, uint) error
	unlink func(int, string, int) error
}

func realLinuxWriteCalls() linuxWriteCalls {
	return linuxWriteCalls{unix.Openat2, unix.Write, unix.Fsync, unix.Fchmod, unix.Renameat2, unix.Unlinkat}
}

const writeResolution = resolution | unix.RESOLVE_NO_SYMLINKS

func (r *linuxRoot) writeOpen(parent int, path string, flags int) (int, error) {
	flags |= unix.O_CLOEXEC | unix.O_NOFOLLOW
	if flags&unix.O_PATH == 0 {
		flags |= unix.O_NONBLOCK
	}
	fd, err := r.calls.open(parent, path, &unix.OpenHow{
		Flags: uint64(flags), Resolve: writeResolution,
	})
	if err != nil {
		return -1, nativeError(err)
	}
	return fd, nil
}

func (r *linuxRoot) writeTarget(parent int, name string, overwrite bool) (err error) {
	fd, err := r.writeOpen(parent, name, unix.O_PATH)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() {
		if r.calls.close(fd) != nil {
			err = errors.Join(err, ErrUnavailable)
		}
	}()
	var st unix.Stat_t
	if err = r.calls.stat(fd, &st); err != nil {
		return nativeError(err)
	}
	if st.Mode&unix.S_IFMT == unix.S_IFLNK {
		return ErrOutside
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return ErrType
	}
	if !overwrite {
		return ErrConflict
	}
	return nil
}

func (r *linuxRoot) writeParentCurrent(path string, expected unix.Stat_t) (err error) {
	fd, err := r.writeOpen(r.fd, path, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return ErrConflict
	}
	var actual unix.Stat_t
	if r.calls.stat(fd, &actual) != nil {
		err = ErrUnavailable
	} else if actual.Dev != expected.Dev || actual.Ino != expected.Ino {
		err = ErrConflict
	}
	if r.calls.close(fd) != nil {
		err = errors.Join(err, ErrUnavailable)
	}
	return err
}

func (r *linuxRoot) write(ctx context.Context, request WriteRequest) (WriteResult, error) {
	return r.writeWith(ctx, request, realLinuxWriteCalls())
}

func (r *linuxRoot) writeWith(ctx context.Context, request WriteRequest, calls linuxWriteCalls) (result WriteResult, err error) {
	result.State = WriteNotCommitted
	if err = contextError(ctx); err != nil {
		return result, err
	}
	parentPath, name := ".", request.Path
	if index := strings.LastIndexByte(request.Path, '/'); index >= 0 {
		parentPath, name = request.Path[:index], request.Path[index+1:]
	}
	parent, err := r.writeOpen(r.fd, parentPath, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return result, err
	}
	fd, temporary := -1, ""
	defer func() {
		// Close once even on EINTR; no retry can safely identify a reused fd.
		if fd >= 0 && r.calls.close(fd) != nil {
			err = errors.Join(err, ErrUnavailable)
		}
		if temporary != "" {
			if e := calls.unlink(parent, temporary, 0); e != nil && !errors.Is(e, unix.ENOENT) {
				err = errors.Join(err, ErrUnavailable)
			}
		}
		if r.calls.close(parent) != nil {
			err = errors.Join(err, ErrUnavailable)
		}
	}()
	var originalParent unix.Stat_t
	if err = r.calls.stat(parent, &originalParent); err != nil {
		return result, nativeError(err)
	}
	if err = r.writeTarget(parent, name, request.Overwrite); err != nil {
		return result, err
	}
	for attempt := 0; attempt < 8; attempt++ {
		if err = contextError(ctx); err != nil {
			return result, err
		}
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return result, ErrUnavailable
		}
		candidate := ".agenteam-write-" + hex.EncodeToString(nonce[:])
		fd, err = calls.create(parent, candidate, &unix.OpenHow{
			Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK,
			Mode:  0600, Resolve: writeResolution,
		})
		if errors.Is(err, unix.EEXIST) {
			fd = -1
			continue
		}
		if err != nil {
			fd = -1
			return result, nativeError(err)
		}
		temporary = candidate
		break
	}
	if fd < 0 {
		return result, ErrConflict
	}
	// Exact new-file mode, independent of umask. Never chmod the existing target.
	if err = calls.chmod(fd, 0600); err != nil {
		return result, nativeError(err)
	}
	for written := 0; written < len(request.Data); {
		if err = contextError(ctx); err != nil {
			return result, err
		}
		end := min(len(request.Data), written+(64<<10))
		n, e := calls.write(fd, request.Data[written:end])
		if e != nil {
			if errors.Is(e, unix.EINTR) {
				continue
			}
			return result, nativeError(e)
		}
		if n <= 0 || n > end-written {
			return result, ErrUnavailable
		}
		written += n
	}
	if err = calls.sync(fd); err != nil {
		return result, nativeError(err)
	}
	closeErr := r.calls.close(fd)
	fd = -1
	if closeErr != nil {
		return result, ErrUnavailable
	}
	if err = r.writeParentCurrent(parentPath, originalParent); err != nil {
		return result, err
	}
	if err = r.writeTarget(parent, name, request.Overwrite); err != nil {
		return result, err
	}
	if err = contextError(ctx); err != nil {
		return result, err
	}
	flags := uint(0)
	if !request.Overwrite {
		flags = unix.RENAME_NOREPLACE
	}
	// Rename is the publication point. Cancellation during the syscall cannot
	// assert it did not run. In particular no post-publication failure rolls back.
	result.State = WriteUnknown
	if err = calls.rename(parent, temporary, parent, name, flags); err != nil {
		if errors.Is(err, unix.EEXIST) || errors.Is(err, unix.ENOTEMPTY) {
			result.State = WriteNotCommitted
			return result, ErrConflict
		}
		return result, nativeError(err)
	}
	temporary = ""
	result.State, result.BytesWritten = WriteCommitted, len(request.Data)
	// Complete this durability step even if cancellation followed publication.
	if err = calls.sync(parent); err != nil {
		return result, nativeError(err)
	}
	result.Durable = true
	return result, nil
}
