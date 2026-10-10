package contract

import "context"

// JSONCaller starts exactly one logical JSON call. An error with a non-nil
// handle retains that original admitted owner, including CommitUnknown. The
// caller must Close that handle and observe Joined before releasing its own
// execution resources. An error with nil means no call was admitted by this
// invocation; it never transfers ownership of an existing duplicate call.
type JSONCaller interface {
	BeginChat(context.Context, ModelRequest) (JSONCall, error)
}

// JSONCall addresses only the original call, not a caller-supplied CallID.
// The first Result consumes its response. Repeated Result never dispatches or
// retries: following an error, Close performs original confirmation/retirement;
// once Joined, Result may expose the retained confirmed response or original
// failure. Close is cancellation plus actual join, not an acknowledgement.
// Model remains the sole owner of AgentRetry within the first Result call.
type JSONCall interface {
	Result(context.Context) (ModelResponse, error)
	Close(context.Context) error
	Joined() bool
}
