//go:build !linux

package workspace

import (
	"errors"
	"testing"
)

func TestUnsupportedPlatform(t *testing.T) {
	if manager, err := New(nil); manager != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("platform fallback", err)
	}
}
