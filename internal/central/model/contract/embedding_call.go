package contract

import "context"

// EmbeddingCaller retains the original admitted owner on error. Callers must
// Close a non-nil handle and observe Joined before retiring their operation.
type EmbeddingCaller interface {
	BeginEmbed(context.Context, EmbeddingRequest) (EmbeddingCall, error)
}

// EmbeddingCall has the same original-call, confirmation and join semantics as
// JSONCall. Repeated Result never sends another provider request.
type EmbeddingCall interface {
	Result(context.Context) (Embeddings, error)
	Close(context.Context) error
	Joined() bool
}
