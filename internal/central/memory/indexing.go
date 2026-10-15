package memory

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
)

func (r *Runtime) updateIndex(ctx context.Context, entry *call, serving rc.ServingIdentity) error {
	batchLimit := 262144 / serving.Dimension
	if batchLimit > 128 {
		batchLimit = 128
	}
	for {
		cause, err := readCause("index-missing")
		if err != nil {
			return err
		}
		records := []c.Record{}
		err = r.domain.transaction(ctx, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
			if err := requireServing(ctx, x, entry.namespace, serving); err != nil {
				return err
			}
			rows, err := x.Query(ctx, recordSelect+` WHERE m.project_id=$1 AND m.agent_id=$2 AND m.status='active' AND r.status='active' AND NOT EXISTS(SELECT 1 FROM agenteam_memory.projections p WHERE (p.project_id,p.agent_id,p.memory_id,p.revision)=(m.project_id,m.agent_id,m.memory_id,m.current_revision) AND p.generation_id=$3) ORDER BY m.memory_id LIMIT $4`, entry.namespace.ProjectID.String(), entry.namespace.AgentID.String(), serving.GenerationID, batchLimit)
			if err != nil {
				return portError(err)
			}
			defer rows.Close()
			for rows.Next() {
				record, err := scanRecord(rows, entry.namespace)
				if err != nil {
					return err
				}
				records = append(records, *record)
			}
			return portError(rows.Err())
		})
		if err != nil {
			return err
		}
		encoded := []string{}
		if len(records) > 0 {
			texts := make([]string, len(records))
			for n, record := range records {
				texts[n] = record.Current.Text
			}
			response, _, err := r.models.embed(ctx, entry, texts, serving.Dimension, &serving)
			if err != nil {
				return err
			}
			encoded = make([]string, len(records))
			for _, item := range response.Items {
				encoded[item.Index], err = vectorText(item.Values, serving.Dimension)
				if err != nil {
					return err
				}
			}
		}
		cause, err = readCause("index-project")
		if err != nil {
			return err
		}
		err = r.domain.indexTransaction(ctx, entry, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
			if err := requireServing(ctx, x, entry.namespace, serving); err != nil {
				return err
			}
			for n, record := range records {
				current, err := readRecord(ctx, x, entry.namespace, record.Current.MemoryID)
				if err != nil {
					return err
				}
				if current == nil || current.Current.Status != c.Active || current.Current.Revision != record.Current.Revision {
					return fault(f.VersionConflict)
				}
				if _, err := x.Exec(ctx, `INSERT INTO agenteam_memory.projections(generation_id,project_id,agent_id,memory_id,revision,embedding,index_text) VALUES($1,$2,$3,$4,$5,$6::vector,$7) ON CONFLICT(generation_id,memory_id,revision) DO NOTHING`, serving.GenerationID, entry.namespace.ProjectID.String(), entry.namespace.AgentID.String(), record.Current.MemoryID.String(), int64(record.Current.Revision), encoded[n], record.Current.Text); err != nil {
					return portError(err)
				}
			}
			// Eligibility is always checked at recall, so even before this
			// physical cleanup a deleted/superseded revision cannot leak.
			if _, err := x.Exec(ctx, `DELETE FROM agenteam_memory.projections p WHERE p.project_id=$1 AND p.agent_id=$2 AND p.generation_id=$3 AND NOT EXISTS(SELECT 1 FROM agenteam_memory.memories m WHERE (m.project_id,m.agent_id,m.memory_id,m.current_revision)=(p.project_id,p.agent_id,p.memory_id,p.revision) AND m.status='active')`, entry.namespace.ProjectID.String(), entry.namespace.AgentID.String(), serving.GenerationID); err != nil {
				return portError(err)
			}
			_, err := x.Exec(ctx, `DELETE FROM agenteam_memory.index_intents j WHERE j.project_id=$1 AND j.agent_id=$2 AND (j.action='delete' OR EXISTS(SELECT 1 FROM agenteam_memory.projections p WHERE (p.project_id,p.agent_id,p.memory_id,p.revision)=(j.project_id,j.agent_id,j.memory_id,j.revision) AND p.generation_id=$3))`, entry.namespace.ProjectID.String(), entry.namespace.AgentID.String(), serving.GenerationID)
			return portError(err)
		})
		if err != nil {
			return err
		}
		if len(records) < batchLimit {
			return nil
		}
	}
}

// Projection maintenance may follow any original retrieval operation. The
// canonical mutation gate remains retain-only; this gate grants no such write.
func (s *Service) indexTransaction(ctx context.Context, entry *call, cause f.TransactionCause, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	if entry == nil || entry.service != s || ctx.Value(callKey{}) != entry || entry.intent != "retain" && entry.intent != "recall" && entry.intent != "reflect" {
		return fault(f.Forbidden)
	}
	locks, err := normalizeLocks(append(append([]f.LockRequest{}, entry.locks...), namespaceLock(entry.namespace, f.Exclusive)))
	if err != nil {
		return err
	}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.requireAccess(ctx, tx, entry, false); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return fn(ctx, tx, x)
	})
	return transactionError(result)
}
