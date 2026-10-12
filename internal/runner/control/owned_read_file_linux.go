//go:build linux

package control

import (
	"errors"
	"os"

	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	"golang.org/x/sys/unix"
)

// The deployment root is already a validated absolute configuration path. Open
// only that existing directory; workspace.New then verifies its owner/mode and
// required openat2 resolver on the pinned fd. No workspace path is assembled here.
func openReadFileRoot(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		switch {
		case errors.Is(err, unix.EACCES), errors.Is(err, unix.EPERM):
			return nil, workspace.ErrPermission
		case errors.Is(err, unix.ENOTDIR):
			return nil, workspace.ErrType
		case errors.Is(err, unix.ELOOP):
			return nil, workspace.ErrOutside
		default:
			return nil, workspace.ErrUnavailable
		}
	}
	file := os.NewFile(uintptr(fd), "runner_workspace_root")
	if file == nil {
		_ = unix.Close(fd)
		return nil, workspace.ErrUnavailable
	}
	return file, nil
}
