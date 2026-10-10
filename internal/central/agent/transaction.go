package agent

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type commitFailure struct{ result f.CommitResult }

func (commitFailure) Error() string              { return string(f.CommitUnknown) }
func (commitFailure) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "agent_commit_outcome") }
func (commitFailure) LogValue() slog.Value       { return slog.StringValue("agent_commit_outcome") }

// UnknownAttempt preserves the original physical attempt/cause. It is not an
// instruction or authorization to repeat the original writer.
func UnknownAttempt(err error) (f.CommitResult, bool) {
	var cause commitFailure
	if errors.As(err, &cause) {
		return cause.result, true
	}
	return f.CommitResult{}, false
}
func commitError(result f.CommitResult) error {
	switch result.State() {
	case f.Committed:
		return nil
	case f.NotCommitted:
		if err := result.Fault(); err != nil {
			return err
		}
		return f.NewFault(f.InternalError, f.NotCommitted)
	default:
		err := f.NewFault(f.CommitUnknown, f.Unknown)
		err.RetryHint = "lookup"
		if result.AttemptID().Validate() == nil {
			err.CauseID = result.AttemptID().String()
		}
		return err.WithCause(commitFailure{result})
	}
}
