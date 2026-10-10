package identity

import (
	"testing"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestIdentityAuthenticationPurposeAndPendingRecovery(t *testing.T) {
	v := pending(t)
	nonce, err := p.ParseNonce("AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := v.SignAuthentication(nonce, "1760000000")
	public, keyErr := v.PublicKey()
	if err != nil || keyErr != nil || !auth.Verify(public) || auth.RunnerID() != v.Configuration().RunnerID || v.State() != Pending {
		t.Fatal("pending recovery authentication changed state", err)
	}
	active, err := v.AsActive()
	if err != nil {
		t.Fatal(err)
	}
	activeAuth, err := active.SignAuthentication(nonce, "1760000000")
	if err != nil || auth != activeAuth {
		t.Fatal("activation rotated identity", err)
	}
	if _, err = (Identity{}).SignAuthentication(nonce, "1760000000"); err != ErrInvalid {
		t.Fatal("invalid identity signed")
	}
	if _, err = v.SignAuthentication(nonce, "1760000000\n"); err != ErrInvalid {
		t.Fatal("signature newline injection")
	}
}
