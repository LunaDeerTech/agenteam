package memory

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

type ListQuery struct {
	Limit   int
	Before  string
	Filters c.Filters
}
type Page struct {
	Memories []c.Record `json:"memories"`
	Next     string     `json:"next_cursor,omitempty"`
}
type HistoryPage struct {
	Revisions []c.Revision `json:"revisions"`
	Next      string       `json:"next_cursor,omitempty"`
}
type DeleteReceipt struct {
	MemoryID c.ID      `json:"memory_id"`
	Revision f.Version `json:"revision"`
	Deleted  bool      `json:"deleted"`
}

func (s *Service) List(ctx context.Context, actor i.Actor, n c.Namespace, q ListQuery) (Page, error) {
	out := Page{Memories: []c.Record{}}
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 100 || q.Filters.Validate() != nil {
		return out, fault(f.InvalidArgument)
	}
	if q.Before != "" {
		if _, err := f.ParseID[c.Memory](q.Before); err != nil {
			return out, fault(f.InvalidArgument)
		}
	}
	run, entry, err := s.begin(ctx, actor, n, tc.OperationID{}, "list")
	if err != nil {
		return out, err
	}
	defer s.end(entry)
	cause, err := readCause("list")
	if err != nil {
		return out, err
	}
	types := filterTypes(q.Filters)
	tags := append([]string{}, q.Filters.Tags...)
	err = s.transaction(run, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		rows, err := x.Query(ctx, recordSelect+` WHERE m.project_id=$1 AND m.agent_id=$2 AND m.status='active' AND r.status='active' AND ($3='' OR m.memory_id::text<$3) AND (cardinality($4::text[])=0 OR r.type=ANY($4)) AND r.tags @> $5::text[] ORDER BY m.memory_id DESC LIMIT $6`, n.ProjectID.String(), n.AgentID.String(), q.Before, types, tags, q.Limit+1)
		if err != nil {
			return portError(err)
		}
		defer rows.Close()
		for rows.Next() {
			value, err := scanRecord(rows, n)
			if err != nil {
				return err
			}
			out.Memories = append(out.Memories, *value)
		}
		if err := rows.Err(); err != nil {
			return portError(err)
		}
		if len(out.Memories) > q.Limit {
			out.Memories = out.Memories[:q.Limit]
			out.Next = out.Memories[q.Limit-1].Current.MemoryID.String()
		}
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	return out, nil
}
func filterTypes(filters c.Filters) []string {
	out := make([]string, len(filters.Types))
	for n, v := range filters.Types {
		out[n] = string(v)
	}
	return out
}

func (s *Service) History(ctx context.Context, actor i.Actor, n c.Namespace, id c.ID, before string, limit int) (HistoryPage, error) {
	out := HistoryPage{Revisions: []c.Revision{}}
	if id.Validate() != nil {
		return out, fault(f.InvalidArgument)
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return out, fault(f.InvalidArgument)
	}
	var ceiling int64
	if before != "" {
		v, err := f.ParseVersion(before)
		if err != nil {
			return out, fault(f.InvalidArgument)
		}
		ceiling = int64(v)
	}
	run, entry, err := s.begin(ctx, actor, n, tc.OperationID{}, "history")
	if err != nil {
		return out, err
	}
	defer s.end(entry)
	cause, err := readCause("history")
	if err != nil {
		return out, err
	}
	err = s.transaction(run, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		current, err := readRecord(ctx, x, n, id)
		if err != nil {
			return err
		}
		if current == nil {
			return fault(f.NotFound)
		}
		rows, err := x.Query(ctx, `SELECT revision,status,type,body,tags,importance,provenance,created_at FROM agenteam_memory.revisions WHERE project_id=$1 AND agent_id=$2 AND memory_id=$3 AND ($4::bigint=0 OR revision<$4) ORDER BY revision DESC LIMIT $5`, n.ProjectID.String(), n.AgentID.String(), id.String(), ceiling, limit+1)
		if err != nil {
			return portError(err)
		}
		defer rows.Close()
		for rows.Next() {
			r := c.Revision{MemoryID: id}
			var version int64
			var raw []byte
			var stamp time.Time
			if err := rows.Scan(&version, &r.Status, &r.Type, &r.Text, &r.Tags, &r.Importance, &raw, &stamp); err != nil {
				return portError(err)
			}
			r.Revision = f.Version(version)
			r.CreatedAt, _ = f.NewInstant(stamp)
			if json.Unmarshal(raw, &r.Provenance) != nil || r.Validate() != nil {
				return fault(f.DependencyUnavailable)
			}
			out.Revisions = append(out.Revisions, r)
		}
		if err := rows.Err(); err != nil {
			return portError(err)
		}
		if len(out.Revisions) > limit {
			out.Revisions = out.Revisions[:limit]
			out.Next = out.Revisions[limit-1].Revision.String()
		}
		return nil
	})
	if err != nil {
		return HistoryPage{}, err
	}
	return out, nil
}

func deleteDigest(actor i.Actor, id c.ID, expected f.Version) (f.Digest, error) {
	raw, err := encode(struct {
		User     string
		Memory   c.ID
		Expected f.Version
	}{actor.Details().UserID, id, expected})
	return digest(raw), err
}
func (s *Service) Delete(ctx context.Context, actor i.Actor, n c.Namespace, id c.ID, meta f.CommandMeta) (DeleteReceipt, error) {
	var out DeleteReceipt
	if id.Validate() != nil || meta.Validate() != nil || meta.ExpectedVersion == nil {
		return out, fault(f.InvalidArgument)
	}
	run, entry, err := s.begin(ctx, actor, n, tc.OperationID{}, "delete")
	if err != nil {
		return out, err
	}
	defer s.end(entry)
	input, err := deleteDigest(actor, id, *meta.ExpectedVersion)
	if err != nil {
		return out, err
	}
	cause, err := commandCause(n, "delete", meta.IdempotencyKey)
	if err != nil {
		return out, err
	}
	err = s.transaction(run, entry, true, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		stored, found, err := readDeleteReceipt(ctx, x, n, meta.IdempotencyKey, input)
		if err != nil {
			return err
		}
		if found {
			out = stored
			return nil
		}
		current, err := readRecord(ctx, x, n, id)
		if err != nil {
			return err
		}
		if current == nil {
			return fault(f.NotFound)
		}
		if current.Current.Status != c.Active {
			return fault(f.ResourceDeleted)
		}
		if current.Current.Revision != *meta.ExpectedVersion {
			return fault(f.VersionConflict)
		}
		if err := deleteCurrent(ctx, x, n, id, current.Current.Revision, now()); err != nil {
			return err
		}
		if _, err := x.Exec(ctx, `UPDATE agenteam_memory.namespaces SET version=version+1 WHERE project_id=$1 AND agent_id=$2`, n.ProjectID.String(), n.AgentID.String()); err != nil {
			return portError(err)
		}
		if err := s.appendAudit(ctx, tx, AuditEvent{Actor: actor, Namespace: n, Action: "delete", MemoryID: id.String(), CauseRef: string(input), Deleted: 1}); err != nil {
			return portError(err)
		}
		out = DeleteReceipt{id, current.Current.Revision, true}
		raw, err := encode(out)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_memory.commands(project_id,agent_id,command_key,input_digest,actor_user_id,memory_id,receipt) VALUES($1,$2,$3,$4,$5,$6,$7)`, n.ProjectID.String(), n.AgentID.String(), string(meta.IdempotencyKey), string(input), actor.Details().UserID, id.String(), raw)
		return portError(err)
	})
	if err != nil {
		return DeleteReceipt{}, err
	}
	return out, nil
}
func readDeleteReceipt(ctx context.Context, x postgres.SQLExecutor, n c.Namespace, key f.IdempotencyKey, input f.Digest) (DeleteReceipt, bool, error) {
	var out DeleteReceipt
	var stored string
	var raw []byte
	err := x.QueryRow(ctx, `SELECT input_digest,receipt FROM agenteam_memory.commands WHERE project_id=$1 AND agent_id=$2 AND command_key=$3`, n.ProjectID.String(), n.AgentID.String(), string(key)).Scan(&stored, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, false, nil
	}
	if err != nil {
		return out, false, portError(err)
	}
	if stored != string(input) {
		return out, false, fault(f.IdempotencyKeyReused)
	}
	if json.Unmarshal(raw, &out) != nil || out.MemoryID.Validate() != nil || out.Revision.Validate() != nil || !out.Deleted {
		return DeleteReceipt{}, false, fault(f.DependencyUnavailable)
	}
	return out, true, nil
}
func (s *Service) LookupDelete(ctx context.Context, actor i.Actor, n c.Namespace, id c.ID, meta f.CommandMeta) (DeleteReceipt, bool, error) {
	var out DeleteReceipt
	var found bool
	if id.Validate() != nil || meta.Validate() != nil || meta.ExpectedVersion == nil {
		return out, false, fault(f.InvalidArgument)
	}
	run, entry, err := s.begin(ctx, actor, n, tc.OperationID{}, "lookup-delete")
	if err != nil {
		return out, false, err
	}
	defer s.end(entry)
	input, err := deleteDigest(actor, id, *meta.ExpectedVersion)
	if err != nil {
		return out, false, err
	}
	cause, err := readCause("lookup-delete")
	if err != nil {
		return out, false, err
	}
	err = s.transaction(run, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		var e error
		out, found, e = readDeleteReceipt(ctx, x, n, meta.IdempotencyKey, input)
		return e
	})
	if err != nil {
		return DeleteReceipt{}, false, err
	}
	return out, found, nil
}
