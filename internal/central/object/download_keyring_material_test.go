package object

import (
	"bytes"
	"testing"
)

func TestDownloadContainsRetainedMaterial(t *testing.T) {
	k := downloadTestKeys(t)
	for _, b := range []byte{5, 6} {
		if !k.ContainsMaterial(bytes.Repeat([]byte{b}, 32)) {
			t.Fatal("retained material omitted")
		}
	}
	for _, b := range [][]byte{nil, make([]byte, 31), make([]byte, 33), bytes.Repeat([]byte{9}, 32)} {
		if k.ContainsMaterial(b) {
			t.Fatal("invalid match")
		}
	}
	if (DownloadKeyring{}).ContainsMaterial(make([]byte, 32)) {
		t.Fatal("zero ring")
	}
}
