//go:build !linux

package command

import "os"

func newNativeOwner(*os.File) (nativeOwner, error) { return nil, ErrUnsupported }
