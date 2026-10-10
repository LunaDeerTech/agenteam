//go:build !linux

package filesystem

import "os"

// No weaker path-resolution fallback is offered on another platform.
func openNativeRoot(*os.File) (nativeRoot, error) { return nil, ErrUnsupported }
