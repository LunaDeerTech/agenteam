package skill

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

// These are controlled row-decoding tests, not a migration/SQL acceptance.
type agentReceiptRows struct {
	postgres.SQLExecutor
	row postgres.Row
}

func (s agentReceiptRows) QueryRow(context.Context, string, ...any) postgres.Row { return s.row }

func TestAgentInitializationStoredSetRejectsInconsistentFacts(t *testing.T) {
	r := stateRow(t)
	agent := stateID[id.Agent](221)
	for _, name := range []string{"enabled", "disabled", "missing", "missing_assignment", "extra_assignment", "foreign_agent", "bad_sequence"} {
		t.Run(name, func(t *testing.T) {
			values := []any{r.request.ProjectID.String(), agent.String(), "planned-agent-create", int64(1), string(r.semantic), true, int64(1), r.skill.String(), int64(1), stateID[sc.Assignment](222).String(), r.created.Time(), int64(1), int64(1)}
			row := skillRowValues{values: values}
			switch name {
			case "disabled":
				values[5] = false
				values[9] = ""
				values[11] = int64(0)
				values[12] = int64(0)
			case "missing":
				row.err = pgx.ErrNoRows
			case "missing_assignment":
				values[12] = int64(0)
			case "extra_assignment":
				values[11] = int64(2)
			case "foreign_agent":
				values[1] = stateID[id.Agent](223).String()
			case "bad_sequence":
				values[6] = int64(2)
			}
			got, err := loadAgentInitialization(context.Background(), agentReceiptRows{row: row}, r.request.ProjectID, agent)
			if name == "missing" {
				if err != nil || got != nil {
					t.Fatal("absent head was manufactured")
				}
				return
			}
			if name != "enabled" && name != "disabled" {
				if err == nil || got != nil {
					t.Fatal("inconsistent stored initial set accepted")
				}
				return
			}
			if err != nil || got == nil {
				t.Fatal("valid stored set rejected", err)
			}
			receipt, err := got.receipt()
			if err != nil || receipt.AssignmentSequence != 1 || (receipt.Assignment != nil) != (name == "enabled") {
				t.Fatal("stored set changed", err)
			}
		})
	}
	private := errors.New("private-store-canary")
	_, err := loadAgentInitialization(context.Background(), agentReceiptRows{row: skillRowValues{err: private}}, r.request.ProjectID, agent)
	var known *f.Fault
	if !errors.Is(err, private) || !errors.As(err, &known) || known.Code != f.DependencyUnavailable || err.Error() == private.Error() {
		t.Fatal("store failure boundary changed")
	}
}
