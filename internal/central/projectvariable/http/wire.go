package projectvariablehttp

import (
	"context"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"strings"
	"unicode/utf8"
)

const bodyLimit = 1 << 20
const listLimit = 5 << 20
const maxQueryBytes = 32 << 10
const maxCursorBytes = 8192

func invalidInput() error  { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func badProjection() error { return f.NewFault(f.DependencyUnavailable, f.NotStarted) }
func encodeValue(ctx context.Context, v any, limit int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > limit {
		return nil, badProjection()
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}
func validPage(request f.PageRequest, n int, next string) bool {
	return request.Validate() == nil && n <= request.Limit && len(next) <= maxCursorBytes && utf8.ValidString(next) && !strings.ContainsRune(next, 0) && (next == "" || n == request.Limit)
}
