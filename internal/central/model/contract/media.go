package contract

import (
	"context"
	"io"

	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// MediaSource is supplied only by the App's business-owner composition. An
// input reference, Actor or persisted snapshot alone never grants a read.
type MediaSource interface {
	RequireBound() error
	OpenModelMedia(context.Context, ModelRequest) (MediaSession, error)
}

type MediaSession interface {
	// Read opens exactly the captured source and returns a reader with verified
	// Object metadata. Each attempt must acquire its own currently allowed read.
	Read(context.Context, MediaPart) (MediaRead, error)
	// WithSend revalidates every source in one lock plan and synchronously lends
	// those locks only to the first HTTP application write. No other I/O belongs
	// inside this callback. Close joins all readers and retires their leases.
	WithSend(context.Context, func() (int, error)) (int, error)
	Close(context.Context) error
}

type MediaRead interface {
	io.ReadCloser
	Meta() oc.ObjectMeta
	Filename() string
}
