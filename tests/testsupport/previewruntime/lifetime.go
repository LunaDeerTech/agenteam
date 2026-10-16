// Package previewruntime bounds the lifetime of an explicitly requested,
// isolated Account preview. It is never imported by production code.
package previewruntime

import (
	"os"
	"time"
)

func publicPreview() bool {
	return os.Getenv("AGENTEAM_ACCOUNT_PREVIEW") == "1" &&
		os.Getenv("AGENTEAM_ACCOUNT_PREVIEW_PORT") == "5254" &&
		os.Getenv("AGENTEAM_ACCOUNT_PREVIEW_LIFETIME") == "24h"
}

// Lifetime keeps ordinary local previews short. The launcher validates these
// inputs before allocating any resources; all three must also match here.
func Lifetime() time.Duration {
	if publicPreview() {
		return 24 * time.Hour
	}
	return 45 * time.Minute
}

// CertificateValidity preserves each ordinary fixture's existing validity.
// The public preview needs a bounded setup/cleanup margin beyond its lifetime.
func CertificateValidity(defaultValidity time.Duration) time.Duration {
	if publicPreview() {
		return 25 * time.Hour
	}
	return defaultValidity
}
