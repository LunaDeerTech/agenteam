//go:build linux

package filesystem

import (
	"context"
	"errors"
	"path"
	"sort"

	"golang.org/x/sys/unix"
)

type findWalk struct {
	root    *linuxRoot
	ctx     context.Context
	request FindRequest
	visited int
	paths   []string
}

func (r *linuxRoot) find(ctx context.Context, request FindRequest) (FindResult, error) {
	if err := contextError(ctx); err != nil {
		return FindResult{}, err
	}
	fd, err := r.open(request.Path, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return FindResult{}, err
	}
	walk := findWalk{root: r, ctx: ctx, request: request, paths: make([]string, 0, request.Limit)}
	truncated, err := walk.directory(fd, request.Path, 0, nil)
	if err != nil {
		return FindResult{}, err
	}
	sort.Strings(walk.paths)
	return FindResult{request.Path, walk.paths, truncated}, nil
}

// directory takes ownership immediately, including failed identity checks. Each
// recursive frame closes exactly its own fd before returning to its parent.
func (w *findWalk) directory(fd int, relative string, depth int, observed *unix.Stat_t) (truncated bool, err error) {
	defer func() {
		if closeErr := w.root.calls.close(fd); closeErr != nil {
			truncated, err = false, ErrUnavailable
		}
	}()
	if err = contextError(w.ctx); err != nil {
		return false, err
	}
	var actual unix.Stat_t
	if err = w.root.calls.stat(fd, &actual); err != nil {
		return false, nativeError(err)
	}
	if actual.Mode&unix.S_IFMT != unix.S_IFDIR {
		return false, ErrType
	}
	if observed != nil && (observed.Dev != actual.Dev || observed.Ino != actual.Ino) {
		return false, ErrConflict
	}
	buffer := make([]byte, 4096)
	for {
		if err = contextError(w.ctx); err != nil {
			return false, err
		}
		n, readErr := w.root.calls.dirents(fd, buffer)
		if readErr != nil {
			if errors.Is(readErr, unix.EINTR) {
				continue
			}
			return false, nativeError(readErr)
		}
		if n < 0 || n > len(buffer) {
			return false, ErrUnavailable
		}
		if n == 0 {
			return false, nil
		}
		consumed, _, names := unix.ParseDirent(buffer[:n], -1, nil)
		if consumed != n {
			return false, ErrUnavailable
		}
		for _, name := range names {
			if err = contextError(w.ctx); err != nil {
				return false, err
			}
			w.visited++
			if w.visited > MaxFindVisited {
				return false, ErrLimit
			}
			if !validComponent(name) {
				return false, ErrType
			}
			child := name
			if relative != "." {
				child = relative + "/" + name
			}
			if len(child) > 4096 {
				return false, ErrLimit
			}
			var st unix.Stat_t
			if err = w.root.calls.statAt(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ESTALE) {
					return false, ErrConflict
				}
				return false, ErrUnavailable
			}
			switch st.Mode & unix.S_IFMT {
			case unix.S_IFREG:
				target := child
				if w.request.Match == "name" {
					target = name
				}
				matched, matchErr := path.Match(w.request.Pattern, target)
				if matchErr != nil {
					return false, ErrInvalid
				}
				if matched {
					if len(w.paths) == w.request.Limit {
						return true, nil
					}
					w.paths = append(w.paths, child)
				}
			case unix.S_IFDIR:
				if depth == MaxFindDepth {
					return false, ErrLimit
				}
				// Re-resolve from the same root, never a potentially moved parent
				// fd. NOFOLLOW blocks a replacement leaf symlink; directory then
				// verifies that the opened inode is the one just observed.
				childFD, openErr := w.root.open(child, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW)
				if openErr != nil {
					if errors.Is(openErr, ErrNotFound) || errors.Is(openErr, ErrType) {
						return false, ErrConflict
					}
					return false, openErr
				}
				truncated, err = w.directory(childFD, child, depth+1, &st)
				if err != nil || truncated {
					return truncated, err
				}
			}
		}
	}
}
