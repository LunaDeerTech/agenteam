package contract

import "context"

// CredentialUsageReader reads one exact, currently authorized Model Invocation.
// The request must identify a Model execution/model_call lease and carry the
// Invocation UUID in RequestID. The lease owner and RequestID are not grants.
// Material is issued only after current usage validation, AEAD and its Audit
// have committed. The production Model consumer/runtime binding is separate.
type CredentialUsageReader interface {
	ReadCredentialForUsage(context.Context, UsageRequest) (SecretMaterial, error)
}
