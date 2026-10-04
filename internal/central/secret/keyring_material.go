package secret

import "crypto/subtle"

// ContainsMaterial is solely for rejecting deployment keys reused across
// cryptographic purposes. It considers retained versions as well as the active
// version; it neither exports key material nor selects a key for encryption.
func (k Keyring) ContainsMaterial(material []byte) bool {
	if k.data == nil || len(material) != 32 {
		return false
	}
	found := 0
	for _, key := range k.data().keys {
		found |= subtle.ConstantTimeCompare(key[:], material)
	}
	return found == 1
}
