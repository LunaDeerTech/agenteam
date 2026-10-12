//go:build !linux

package control

import (
	"os"

	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
)

// Selecting an operation never creates a path-string fallback on other systems.
func openReadFileRoot(string) (*os.File, error) { return nil, workspace.ErrUnsupported }
