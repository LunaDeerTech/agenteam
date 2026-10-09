package identity

import (
	"crypto/ed25519"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// SignAuthentication signs only this identity's Runner ID and the fixed v1
// purpose. Pending identities may authenticate to recover a lost enroll reply;
// a verified response is still required before persisting the active state.
func (v Identity) SignAuthentication(nonce p.Nonce, timestamp p.UnixSeconds) (p.Authentication, error) {
	if v.Validate() != nil {
		return p.Authentication{}, ErrInvalid
	}
	message, err := p.SigningBytes(v.config.RunnerID, nonce, timestamp)
	if err != nil {
		return p.Authentication{}, ErrInvalid
	}
	private := ed25519.NewKeyFromSeed(v.seed[:])
	defer clear(private)
	signature := ed25519.Sign(private, message)
	defer clear(signature)
	return p.NewAuthentication(v.config.RunnerID, nonce, timestamp, signature)
}
