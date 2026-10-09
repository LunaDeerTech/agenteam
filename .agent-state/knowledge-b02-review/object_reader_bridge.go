// Independent offline review bridge. Overlay only; never a product export.
package object

import (
	"context"
	"io"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func ReviewB02IntegrityReader(ctx context.Context, source io.ReadCloser, size int64, digest f.Digest, release func() error) (io.ReadCloser, error) {
	return newIntegrityReader(ctx, source, size, digest, release)
}
