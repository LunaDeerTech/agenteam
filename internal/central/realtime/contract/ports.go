package contract

import (
	"context"

	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// Authority is implemented by the original domain reader. RequireSubscription
// must recheck the current Human Owner, nondeleting Meeting and exact current
// Contribution/generation/attempt/Execution, using its original Store/locks.
// A syntactically valid Topic or previously successful Subscribe is no grant.
// ReadSnapshot repeats that authorization and checks its actual public result.
type Authority interface {
	RequireSubscription(context.Context, i.Actor, Topic) error
	ReadSnapshot(context.Context, i.Actor, Topic) (Snapshot, error)
}

// Publisher is a nonblocking invalidation sink, never a business write or an
// authorization provider. Only the original producer invokes it after its
// transaction is known committed (including a conclusive original read-only
// observation). Unknown outcomes publish no invented success.
// No caller can inject a body, Model frame, token, or arbitrary browser topic.
type Publisher interface {
	InvalidateMeeting(i.ProjectID, string)
	InvalidateExecution(i.ProjectID, i.ExecutionID)
}

// Transport obligations:
//   - authenticate the original browser Session and exact configured Origin;
//     revalidate that Session and Authority before each bounded outgoing batch;
//   - admit the subscription and enqueue its ack under the publication lock;
//   - capture a watermark BEFORE ReadSnapshot begins. Deliver that same
//     watermark with the actual snapshot. Buffer later invalidations so a
//     commit during the read cannot disappear behind a newer watermark;
//   - bound connections, subscriptions, notification count/bytes and all I/O;
//     overflow requires explicit resync or connection closure, never omission;
//   - a new epoch/reconnect requires a new authorized subscription/snapshot;
//   - Stop rejects new admission, closes owned sockets and cancels readers;
//     Joined requires original reader/writer goroutines and callbacks to return.
// These are process-local notifications. Canonical domain state remains the
// only recovery source; this contract does not promise a durable event log.
