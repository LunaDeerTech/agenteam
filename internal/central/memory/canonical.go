package memory

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type plannedAction struct {
	action   c.Action
	target   c.ID
	revision f.Version
}

// planActions checks the model's proposal against exactly the authorized set
// it received. Current-state validation is repeated inside the commit below.
func planActions(proposal c.Proposal, related []c.Revision) ([]plannedAction, error) {
	if proposal.Validate() != nil || len(related) > c.MaxRelated {
		return nil, fault(f.InvalidArgument)
	}
	targets := map[c.ID]f.Version{}
	for _, r := range related {
		if r.Validate() != nil || r.Status != c.Active {
			return nil, fault(f.InvalidArgument)
		}
		if _, ok := targets[r.MemoryID]; ok {
			return nil, fault(f.InvalidArgument)
		}
		targets[r.MemoryID] = r.Revision
	}
	plan := make([]plannedAction, 0, len(proposal.Actions))
	for _, action := range proposal.Actions {
		step := plannedAction{action: action}
		switch action.Kind {
		case c.Add:
			id, err := f.NewID[c.Memory]()
			if err != nil {
				return nil, portError(err)
			}
			step.target = id
			step.revision = 1
		case c.Update, c.Delete:
			id, err := f.ParseID[c.Memory](action.TargetID)
			if err != nil {
				return nil, fault(f.InvalidArgument)
			}
			revision, err := f.ParseVersion(action.ExpectedRevision)
			if err != nil || targets[id] != revision {
				return nil, fault(f.Forbidden)
			}
			if action.Kind == c.Update && revision == math.MaxInt64 {
				return nil, fault(f.InvalidState)
			}
			step.target = id
			step.revision = revision
		}
		plan = append(plan, step)
	}
	return plan, nil
}

func (s *Service) prepareRetain(ctx context.Context, entry *call, input f.Digest, gate string, owner *ModelAuthority) (c.RetainResult, bool, error) {
	var receipt c.RetainResult
	var found bool
	cause, err := commandCause(entry.namespace, "retain", f.IdempotencyKey("retain:"+entry.operation.String()))
	if err != nil {
		return receipt, false, err
	}
	err = s.transaction(ctx, entry, true, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		// The runtime always supplies its fixed same-Store process owner. The nil
		// path remains only for legacy canonical adapters; such rows are explicitly
		// ineligible for cold failure recovery because they lack a process proof.
		var preparedProcess any
		if owner != nil {
			if owner.domain != s || !sameStore(owner.store, s.store) || owner.guard == nil || owner.process.Validate() != nil {
				return fault(f.Forbidden)
			}
			if err := owner.guard.RequireCurrentProcessInTx(ctx, tx, owner.process); err != nil {
				return portError(err)
			}
			preparedProcess = owner.process.String()
		}
		var project, agent, execution, stored, phase string
		var body []byte
		err := x.QueryRow(ctx, `SELECT project_id::text,agent_id::text,execution_id::text,input_digest,phase,receipt FROM agenteam_memory.operations WHERE operation_id=$1`, entry.operation.String()).Scan(&project, &agent, &execution, &stored, &phase, &body)
		if err == nil {
			if project != entry.namespace.ProjectID.String() || agent != entry.namespace.AgentID.String() || execution != entry.actor.Details().ExecutionID || stored != string(input) {
				return fault(f.IdempotencyKeyReused)
			}
			if phase != "committed" {
				return fault(f.ResourceBusy)
			}
			if json.Unmarshal(body, &receipt) != nil {
				return fault(f.DependencyUnavailable)
			}
			found = true
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return portError(err)
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_memory.operations(operation_id,project_id,agent_id,execution_id,input_digest,gate_outcome,phase,created_at,prepared_process_id) VALUES($1,$2,$3,$4,$5,$6,'prepared',$7,$8)`, entry.operation.String(), entry.namespace.ProjectID.String(), entry.namespace.AgentID.String(), entry.actor.Details().ExecutionID, string(input), gate, now().Time(), preparedProcess)
		return portError(err)
	})
	return receipt, found, err
}

func (s *Service) saveProposal(ctx context.Context, entry *call, number int, proposal c.Proposal, related []c.Revision, modelCall mc.CallID) error {
	if number < 1 || number > 2 || modelCall.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if _, err := planActions(proposal, related); err != nil {
		return err
	}
	targets := make([]c.Target, len(related))
	for n, r := range related {
		targets[n] = c.Target{MemoryID: r.MemoryID, Revision: r.Revision}
	}
	raw, err := encode(proposal)
	if err != nil {
		return err
	}
	input, err := encode(targets)
	if err != nil {
		return err
	}
	cause, err := readCause("proposal")
	if err != nil {
		return err
	}
	return s.transaction(ctx, entry, true, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		count, err := x.Exec(ctx, `INSERT INTO agenteam_memory.proposals(operation_id,proposal_number,input_digest,input_targets,proposal,model_call_id,created_at) SELECT operation_id,$2,$3,$4,$5,$6,$7 FROM agenteam_memory.operations WHERE operation_id=$1 AND project_id=$8 AND agent_id=$9 AND phase='prepared'`, entry.operation.String(), number, string(digest(input)), input, raw, modelCall.String(), now().Time(), entry.namespace.ProjectID.String(), entry.namespace.AgentID.String())
		if err != nil {
			return portError(err)
		}
		if count.RowsAffected() != 1 {
			return fault(f.InvalidState)
		}
		return nil
	})
}

func (s *Service) applyProposal(ctx context.Context, entry *call, proposal c.Proposal, related []c.Revision, number int) (c.RetainResult, error) {
	result := c.RetainResult{Added: []c.Target{}, Updated: []c.Target{}}
	plan, err := planActions(proposal, related)
	if err != nil {
		return result, err
	}
	// A generated credential must not enter canonical storage or any index.
	for _, step := range plan {
		if step.action.Kind == c.Add || step.action.Kind == c.Update {
			content, err := encode(step.action.Content)
			if err != nil {
				return result, err
			}
			checked, err := inspect(ctx, s.deps.Gate, string(content))
			if err != nil {
				return result, err
			}
			if checked.Text != string(content) {
				return result, fault(f.Forbidden)
			}
		}
	}
	cause, err := commandCause(entry.namespace, "retain", f.IdempotencyKey("retain:"+entry.operation.String()))
	if err != nil {
		return result, err
	}
	err = s.transaction(ctx, entry, true, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		n := entry.namespace
		var phase, gate string
		var stored []byte
		if err := x.QueryRow(ctx, `SELECT phase,gate_outcome,receipt FROM agenteam_memory.operations WHERE operation_id=$1 AND project_id=$2 AND agent_id=$3 AND execution_id=$4`, entry.operation.String(), n.ProjectID.String(), n.AgentID.String(), entry.actor.Details().ExecutionID).Scan(&phase, &gate, &stored); err != nil {
			return portError(err)
		}
		if phase == "committed" {
			if json.Unmarshal(stored, &result) != nil {
				return fault(f.DependencyUnavailable)
			}
			return nil
		}
		var evidence []byte
		if err := x.QueryRow(ctx, `SELECT proposal FROM agenteam_memory.proposals WHERE operation_id=$1 AND proposal_number=$2`, entry.operation.String(), number).Scan(&evidence); err != nil {
			return portError(err)
		}
		var saved c.Proposal
		if json.Unmarshal(evidence, &saved) != nil {
			return fault(f.DependencyUnavailable)
		}
		want, _ := encode(proposal)
		actual, _ := encode(saved)
		if digest(want) != digest(actual) {
			return fault(f.Forbidden)
		}
		// Validate every target before performing any domain write.
		for _, step := range plan {
			if step.action.Kind != c.Update && step.action.Kind != c.Delete {
				continue
			}
			current, err := readRecord(ctx, x, n, step.target)
			if err != nil {
				return err
			}
			if current == nil || current.Current.Status != c.Active || current.Current.Revision != step.revision {
				return fault(f.VersionConflict)
			}
		}
		if _, err := x.Exec(ctx, `INSERT INTO agenteam_memory.namespaces(project_id,agent_id,version) VALUES($1,$2,1) ON CONFLICT DO NOTHING`, n.ProjectID.String(), n.AgentID.String()); err != nil {
			return portError(err)
		}
		stamp := now()
		execution, _ := f.ParseID[i.Execution](entry.actor.Details().ExecutionID)
		for _, step := range plan {
			switch step.action.Kind {
			case c.Noop:
				result.NoopCount++
				continue
			case c.Add:
				_, err = x.Exec(ctx, `INSERT INTO agenteam_memory.memories(memory_id,project_id,agent_id,current_revision,status,created_at,updated_at) VALUES($1,$2,$3,1,'active',$4,$4)`, step.target.String(), n.ProjectID.String(), n.AgentID.String(), stamp.Time())
				if err != nil {
					return portError(err)
				}
				result.Added = append(result.Added, c.Target{MemoryID: step.target, Revision: 1})
			case c.Update:
				if _, err = x.Exec(ctx, `UPDATE agenteam_memory.revisions SET status='superseded' WHERE memory_id=$1 AND revision=$2 AND status='active'`, step.target.String(), int64(step.revision)); err != nil {
					return portError(err)
				}
				step.revision++
				if _, err = x.Exec(ctx, `UPDATE agenteam_memory.memories SET current_revision=$2,updated_at=$3 WHERE memory_id=$1`, step.target.String(), int64(step.revision), stamp.Time()); err != nil {
					return portError(err)
				}
				result.Updated = append(result.Updated, c.Target{MemoryID: step.target, Revision: step.revision})
			case c.Delete:
				if err = deleteCurrent(ctx, x, n, step.target, step.revision, stamp); err != nil {
					return err
				}
				result.DeletedCount++
				continue
			}
			derived := []c.ID{}
			if step.action.Kind == c.Update {
				derived = append(derived, step.target)
			}
			provenance := c.Provenance{ExecutionID: execution, OperationID: entry.operation.String(), DerivedFrom: derived}
			raw, _ := encode(provenance)
			_, err = x.Exec(ctx, `INSERT INTO agenteam_memory.revisions(memory_id,revision,project_id,agent_id,status,type,body,tags,importance,provenance,created_by_operation_id,created_at) VALUES($1,$2,$3,$4,'active',$5,$6,$7,$8,$9,$10,$11)`, step.target.String(), int64(step.revision), n.ProjectID.String(), n.AgentID.String(), string(step.action.Content.Type), step.action.Content.Text, step.action.Content.Tags, string(step.action.Content.Importance), raw, entry.operation.String(), stamp.Time())
			if err != nil {
				return portError(err)
			}
			if err = insertIndexIntent(ctx, x, n, step.target, step.revision, "upsert", stamp); err != nil {
				return err
			}
		}
		if len(result.Added)+len(result.Updated)+result.DeletedCount > 0 {
			if _, err := x.Exec(ctx, `UPDATE agenteam_memory.namespaces SET version=version+1 WHERE project_id=$1 AND agent_id=$2`, n.ProjectID.String(), n.AgentID.String()); err != nil {
				return portError(err)
			}
		}
		if err := s.appendAudit(ctx, tx, AuditEvent{Actor: entry.actor, Namespace: n, Action: "retain", OperationID: entry.operation.String(), CauseRef: entry.operation.String(), Gate: gate, Added: len(result.Added), Updated: len(result.Updated), Deleted: result.DeletedCount, Noop: result.NoopCount}); err != nil {
			return portError(err)
		}
		raw, err := encode(result)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_memory.operations SET phase='committed',receipt=$2,committed_at=$3 WHERE operation_id=$1 AND phase='prepared'`, entry.operation.String(), raw, stamp.Time())
		return portError(err)
	})
	if err != nil {
		return c.RetainResult{}, err
	}
	return result, nil
}

func insertIndexIntent(ctx context.Context, x postgres.SQLExecutor, n c.Namespace, id c.ID, revision f.Version, action string, stamp f.Instant) error {
	_, err := x.Exec(ctx, `INSERT INTO agenteam_memory.index_intents(project_id,agent_id,memory_id,revision,action,created_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, n.ProjectID.String(), n.AgentID.String(), id.String(), int64(revision), action, stamp.Time())
	return portError(err)
}
func deleteCurrent(ctx context.Context, x postgres.SQLExecutor, n c.Namespace, id c.ID, revision f.Version, stamp f.Instant) error {
	if _, err := x.Exec(ctx, `UPDATE agenteam_memory.revisions SET status='deleted' WHERE memory_id=$1 AND revision=$2 AND status='active'`, id.String(), int64(revision)); err != nil {
		return portError(err)
	}
	if _, err := x.Exec(ctx, `UPDATE agenteam_memory.memories SET status='deleted',updated_at=$2,deleted_at=$2 WHERE memory_id=$1`, id.String(), stamp.Time()); err != nil {
		return portError(err)
	}
	return insertIndexIntent(ctx, x, n, id, revision, "delete", stamp)
}

const recordSelect = `SELECT m.memory_id::text,m.current_revision,m.status,r.type,r.body,r.tags,r.importance,r.provenance,r.created_at,m.updated_at FROM agenteam_memory.memories m JOIN agenteam_memory.revisions r ON (r.memory_id,r.revision)=(m.memory_id,m.current_revision)`

func scanRecord(row postgres.Row, n c.Namespace) (*c.Record, error) {
	var record c.Record
	var id string
	var revision int64
	var provenance []byte
	var created, updated time.Time
	err := row.Scan(&id, &revision, &record.Current.Status, &record.Current.Type, &record.Current.Text, &record.Current.Tags, &record.Current.Importance, &provenance, &created, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	record.Namespace = n
	record.Current.MemoryID, err = f.ParseID[c.Memory](id)
	if err != nil {
		return nil, portError(err)
	}
	record.Current.Revision = f.Version(revision)
	record.Current.CreatedAt, _ = f.NewInstant(created)
	record.UpdatedAt, _ = f.NewInstant(updated)
	if json.Unmarshal(provenance, &record.Current.Provenance) != nil || record.Validate() != nil {
		return nil, fault(f.DependencyUnavailable)
	}
	return &record, nil
}
func readRecord(ctx context.Context, x postgres.SQLExecutor, n c.Namespace, id c.ID) (*c.Record, error) {
	return scanRecord(x.QueryRow(ctx, recordSelect+` WHERE m.project_id=$1 AND m.agent_id=$2 AND m.memory_id=$3`, n.ProjectID.String(), n.AgentID.String(), id.String()), n)
}
