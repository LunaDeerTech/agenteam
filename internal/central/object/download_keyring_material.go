package object

import "crypto/subtle"

// ContainsMaterial checks all retained download keys for cross-purpose reuse.
// It is not a projection of any key or of the active signing key.
func (k DownloadKeyring) ContainsMaterial(material []byte) bool {
	if k.data == nil || len(material) != 32 {
		return false
	}
	found := 0
	for _, key := range k.data().keys {
		found |= subtle.ConstantTimeCompare(key[:], material)
	}
	return found == 1
}
