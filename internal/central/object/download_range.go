package object

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// Only an authorized, confirmed grant check can produce this error. The total
// is for the authenticated HTTP adapter's Content-Range: bytes */N response;
// arbitrary signature/permission errors have no such projection.
type downloadRangeError struct{ total func() int64 }

func (*downloadRangeError) Error() string { return "RANGE_NOT_SATISFIABLE" }
func (*downloadRangeError) Unwrap() error {
	return foundation.NewFault(foundation.RangeNotSatisfiable, foundation.NotCommitted)
}
func (downloadRangeError) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "RANGE_NOT_SATISFIABLE")
}
func (downloadRangeError) MarshalJSON() ([]byte, error) {
	return []byte(`"RANGE_NOT_SATISFIABLE"`), nil
}
func (downloadRangeError) LogValue() slog.Value { return slog.StringValue("RANGE_NOT_SATISFIABLE") }
func DownloadUnsatisfiedSize(err error) (foundation.Progress, bool) {
	var r *downloadRangeError
	if !errors.As(err, &r) || r == nil || r.total == nil {
		return 0, false
	}
	return foundation.Progress(r.total()), true
}
