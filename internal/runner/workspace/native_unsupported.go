//go:build !linux

package workspace

import "os"

// No path-string fallback on platforms without the required resolver.
func openNativeRoot(*os.File) (nativeRoot, error) { return nil, ErrUnsupported }
