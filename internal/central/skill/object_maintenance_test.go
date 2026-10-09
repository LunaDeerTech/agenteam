package skill

import (
	"context"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type maintenanceStore struct {
	*initializationReadStore
	missingObject, missingAttempt, foreignWork bool
	process                                    oc.ProcessID
	seen                                       bool
}

func (s *maintenanceStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, e := s.initializationReadStore.InTx(tx); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *maintenanceStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	switch {
	case strings.HasPrefix(q, "SELECT project_id::text FROM"):
		if s.missingObject || args[0] != s.row.values[16] {
			return skillRowValues{err: pgx.ErrNoRows}
		}
		return skillRowValues{values: []any{s.row.values[0]}}
	case strings.HasPrefix(q, "SELECT attempt_id::text,process_id::text"):
		s.seen = true
		if s.missingAttempt || len(args) != 7 || args[1] != s.row.values[0] || args[2] != s.row.values[1] || args[3] != s.row.values[3] || args[4] != s.row.values[4] || args[5] != s.row.values[16] || args[6] != s.row.values[17] {
			return skillRowValues{err: pgx.ErrNoRows}
		}
		return skillRowValues{values: []any{args[0], s.process.String()}}
	case strings.HasPrefix(q, "SELECT EXISTS(SELECT 1 FROM agenteam_skill.work"):
		return skillRowValues{values: []any{!s.foreignWork && args[0] == s.row.values[0] && args[1] == s.row.values[3] && args[2] == s.process.String()}}
	}
	return s.initializationReadStore.QueryRow(ctx, q, args...)
}
func TestSkillMaintenanceOnlyMapsOwnedTechnicalTails(t *testing.T) {
	for _, name := range []string{"finish_writer", "join_old_attempt", "release_reader", "release_process", "inspect", "wrong_instance", "wrong_original_process", "missing_object", "missing_attempt", "foreign_work", "changed_process", "lost_lock", "ended_tx", "cleanup_unbound"} {
		t.Run(name, func(t *testing.T) {
			s, base, projects, _, request := readFixture(t)
			store := &maintenanceStore{initializationReadStore: base, process: stateID[oc.Process](50)}
			a := s.state().authority
			a.state().store = store
			// Technical return remains possible after the old active gate closes;
			// no publication, read or reference-release grant is returned here.
			projects.gate = fault(f.InvalidState)
			d := oc.AccessRequestDetails{Operation: oc.FinishWriterAccess, InstanceID: store.process, ObjectID: stateID[oc.StoredObject](7), AttemptID: stateID[oc.Attempt](6)}
			switch name {
			case "join_old_attempt":
				d.Operation = oc.JoinAttemptAccess
				d.AttemptID = stateID[oc.Attempt](177)
				d.ProcessID = store.process
				d.InstanceID = stateID[oc.Process](178)
			case "release_reader":
				d.Operation = oc.ReleaseReaderAccess
				d.AttemptID = oc.AttemptID{}
				d.LeaseID = stateID[oc.Lease](179)
			case "release_process", "foreign_work":
				d.Operation = oc.ReleaseProcessAccess
				d.AttemptID = oc.AttemptID{}
				d.ProcessID = store.process
				store.foreignWork = name == "foreign_work"
			case "inspect":
				d.Operation = oc.InspectAccess
				d.AttemptID = oc.AttemptID{}
			case "wrong_instance":
				d.InstanceID = stateID[oc.Process](180)
			case "wrong_original_process":
				d.Operation = oc.JoinAttemptAccess
				d.ProcessID = stateID[oc.Process](180)
			case "missing_object":
				store.missingObject = true
			case "missing_attempt":
				store.missingAttempt = true
			case "cleanup_unbound":
				d.Operation = oc.ClaimCleanupAccess
			}
			r, e := oc.NewMaintenanceAccess(d)
			if e != nil {
				t.Fatal(e)
			}
			deps, e := a.Discover(context.Background(), r)
			initialReject := name == "wrong_instance" || name == "wrong_original_process" || name == "missing_object" || name == "missing_attempt" || name == "foreign_work" || name == "cleanup_unbound"
			if initialReject {
				if e == nil {
					t.Fatal("unowned/irreversible maintenance admitted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			base.live = true
			base.tx = f.NewTx()
			base.held = map[string]f.LockMode{}
			if e = base.AcquireAll(context.Background(), base.tx, deps.Locks()); e != nil {
				t.Fatal(e)
			}
			switch name {
			case "changed_process":
				store.process = stateID[oc.Process](181)
			case "lost_lock":
				delete(base.held, projectLock(request.ProjectID, f.Exclusive).Key.Canonical())
			case "ended_tx":
				base.live = false
			}
			e = a.ValidateInTx(context.Background(), base.tx, r, deps)
			if name == "changed_process" || name == "lost_lock" || name == "ended_tx" {
				if e == nil {
					t.Fatal("stale maintenance accepted")
				}
				return
			}
			if e != nil || projects.initializes != 0 || projects.converges != 0 {
				t.Fatal("technical-only tail incorrectly reauthorized", e)
			}
			if d.AttemptID != (oc.AttemptID{}) && !store.seen {
				t.Fatal("attempt mapping omitted")
			}
		})
	}
}
