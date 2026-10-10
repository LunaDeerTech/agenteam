//go:build linux

package filesystem

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"sort"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const resolution = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV

// Per-instance private syscall seams make held-call/close failures testable;
// construction always selects these real calls, never a production registry.
type linuxCalls struct {
	open    func(int, string, *unix.OpenHow) (int, error)
	stat    func(int, *unix.Stat_t) error
	statAt  func(int, string, *unix.Stat_t, int) error
	pread   func(int, []byte, int64) (int, error)
	dirents func(int, []byte) (int, error)
	close   func(int) error
}

func realLinuxCalls() linuxCalls {
	return linuxCalls{unix.Openat2, unix.Fstat, unix.Fstatat, unix.Pread, unix.ReadDirent, unix.Close}
}

type linuxRoot struct {
	fd    int
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
	var duplicateErr error
	err = raw.Control(func(original uintptr) {
		fd, duplicateErr = unix.FcntlInt(original, unix.F_DUPFD_CLOEXEC, 0)
	})
	if err != nil || duplicateErr != nil {
		if fd >= 0 {
			_ = calls.close(fd)
		}
		return nil, ErrUnavailable
	}
	root := &linuxRoot{fd: fd, calls: calls}
	var st unix.Stat_t
	if err = calls.stat(fd, &st); err != nil {
		_ = root.close()
		return nil, ErrUnavailable
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = root.close()
		return nil, ErrType
	}
	probe, err := root.open(".", unix.O_PATH|unix.O_DIRECTORY)
	if err != nil {
		_ = root.close()
		return nil, err
	}
	if err = calls.close(probe); err != nil {
		_ = root.close()
		return nil, ErrUnavailable
	}
	return root, nil
}

func (r *linuxRoot) open(path string, flags int) (int, error) {
	flags |= unix.O_CLOEXEC
	if flags&unix.O_PATH == 0 {
		flags |= unix.O_NONBLOCK | unix.O_NOCTTY
	}
	fd, err := r.calls.open(r.fd, path, &unix.OpenHow{Flags: uint64(flags), Resolve: resolution})
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

func (r *linuxRoot) read(ctx context.Context, request ReadRequest) (result ReadResult, err error) {
	if err = contextError(ctx); err != nil {
		return ReadResult{}, err
	}
	fd, err := r.open(request.Path, unix.O_RDONLY)
	if err != nil {
		return ReadResult{}, err
	}
	defer func() {
		// On Linux even an EINTR close consumes this owned descriptor. Never retry.
		if closeErr := r.calls.close(fd); closeErr != nil {
			result, err = ReadResult{}, ErrUnavailable
		}
	}()
	if err = contextError(ctx); err != nil {
		return ReadResult{}, err
	}
	var st unix.Stat_t
	if err = r.calls.stat(fd, &st); err != nil {
		return ReadResult{}, nativeError(err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return ReadResult{}, ErrType
	}
	data := make([]byte, request.Length+1)
	used, eof := 0, false
	for used < len(data) {
		if err = contextError(ctx); err != nil {
			return ReadResult{}, err
		}
		n, readErr := r.calls.pread(fd, data[used:], request.Offset+int64(used))
		if readErr != nil {
			if errors.Is(readErr, unix.EINTR) {
				continue
			}
			return ReadResult{}, nativeError(readErr)
		}
		if n < 0 || n > len(data)-used {
			return ReadResult{}, ErrUnavailable
		}
		if n == 0 {
			eof = true
			break
		}
		used += n
	}
	if err = contextError(ctx); err != nil {
		return ReadResult{}, err
	}
	if used > request.Length {
		used = request.Length
	}
	data = data[:used]
	var content string
	if request.Encoding == "utf8" {
		if !utf8.Valid(data) {
			return ReadResult{}, ErrType
		}
		content = string(data)
	} else {
		content = base64.StdEncoding.EncodeToString(data)
	}
	return ReadResult{request.Path, request.Offset, used, eof, request.Encoding, content}, nil
}

func (r *linuxRoot) list(ctx context.Context, request ListRequest) (result ListResult, err error) {
	if err = contextError(ctx); err != nil {
		return ListResult{}, err
	}
	fd, err := r.open(request.Path, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return ListResult{}, err
	}
	defer func() {
		if closeErr := r.calls.close(fd); closeErr != nil {
			result, err = ListResult{}, ErrUnavailable
		}
	}()
	if err = contextError(ctx); err != nil {
		return ListResult{}, err
	}
	var st unix.Stat_t
	if err = r.calls.stat(fd, &st); err != nil {
		return ListResult{}, nativeError(err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return ListResult{}, ErrType
	}
	names := make([]string, 0, request.Limit+1)
	buffer := make([]byte, 4096)
	for len(names) < request.Limit+1 {
		if err = contextError(ctx); err != nil {
			return ListResult{}, err
		}
		n, readErr := r.calls.dirents(fd, buffer)
		if readErr != nil {
			if errors.Is(readErr, unix.EINTR) {
				continue
			}
			return ListResult{}, nativeError(readErr)
		}
		if n < 0 || n > len(buffer) {
			return ListResult{}, ErrUnavailable
		}
		if n == 0 {
			break
		}
		_, _, names = unix.ParseDirent(buffer[:n], request.Limit+1-len(names), names)
	}
	entries := make([]Entry, 0, min(len(names), request.Limit))
	for index, name := range names {
		if err = contextError(ctx); err != nil {
			return ListResult{}, err
		}
		if !validComponent(name) {
			return ListResult{}, ErrType
		}
		// Do not follow links; even an escaping link may safely be listed by name.
		if err = r.calls.statAt(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ESTALE) {
				return ListResult{}, ErrConflict
			}
			return ListResult{}, ErrUnavailable
		}
		kind := "other"
		switch st.Mode & unix.S_IFMT {
		case unix.S_IFREG:
			kind = "file"
		case unix.S_IFDIR:
			kind = "directory"
		case unix.S_IFLNK:
			kind = "symlink"
		}
		if index < request.Limit {
			entries = append(entries, Entry{name, kind})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return ListResult{request.Path, entries, len(names) > request.Limit}, nil
}

func nativeError(err error) error {
	switch {
	case errors.Is(err, unix.EXDEV), errors.Is(err, unix.ELOOP):
		return ErrOutside
	case errors.Is(err, unix.ENOENT):
		return ErrNotFound
	case errors.Is(err, unix.ENOTDIR), errors.Is(err, unix.EISDIR), errors.Is(err, unix.ENXIO):
		return ErrType
	case errors.Is(err, unix.EAGAIN), errors.Is(err, unix.ESTALE):
		return ErrConflict
	case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EPERM), errors.Is(err, unix.EOPNOTSUPP):
		return ErrUnsupported
	default:
		return ErrUnavailable
	}
}
