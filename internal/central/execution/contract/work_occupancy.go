package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// MaxWorkOccupancyTasks covers Work's existing per-Project Task limit. Task
// identities use the same canonical UUID strings as Trigger.TaskID: this
// package neither imports Work nor defines a second owner of Task identity.
const MaxWorkOccupancyTasks = 65536

type ActiveTaskExecution struct {
	TaskID      string
	ExecutionID i.ExecutionID
	AgentID     i.AgentID
	Status      Status
}

// ExecutionOccupancy contains no Task content or execution input. History is
// presence in any Execution state, including every active state. Cancellation
// requested without a terminal state still occupies the Task.
// Successful results have nonnil, sorted slices; Active is ordered by TaskID,
// then ExecutionID, and HistoryTaskIDs is a unique TaskID set.
type ExecutionOccupancy struct {
	Active         []ActiveTaskExecution
	HistoryTaskIDs []string
}

// WorkOccupancyReader is a trusted internal fact port, not an authorization or
// a public read API. The caller first authorizes its current Project operation
// and obtains its actual Task set from Work in this same transaction. It holds
// the complete command/Task lock union including Project SH and Schedule EX.
// ReadInTx verifies the original Store/Tx and those shared domain gates; it does
// not begin a transaction, acquire locks, inspect Work SQL or infer permission.
// taskIDs must be a nonnil, sorted, unique canonical UUID list (empty is valid).
// Missing providers, bad facts or read failures never mean no occupancy.
// This port says nothing about D23 pending dispatch; that requires its owner.
type WorkOccupancyReader interface {
	ReadInTx(context.Context, f.Tx, i.ProjectID, []string) (ExecutionOccupancy, error)
}

func (ExecutionOccupancy) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_work_occupancy")
}
func (ExecutionOccupancy) LogValue() slog.Value {
	return slog.StringValue("execution_work_occupancy")
}
func (ExecutionOccupancy) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_work_occupancy"`), nil
}
