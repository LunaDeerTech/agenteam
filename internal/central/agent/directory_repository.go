package agent

import (
	"context"
	"encoding/json"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Read only the public metadata and the publication receipt. The latter is
// validated using the original typed Agent mutation codec, never returned.
// LIMIT 2 makes duplicate create receipts corruption, not another publication.
const directoryPageSQL = `SELECT a.id::text,a.project_id::text,a.name,a.display_name,a.tag_color,a.description,a.version,a.created_at,a.updated_at,c.receipts
 FROM agenteam_agent.agents a
 CROSS JOIN LATERAL (
  SELECT jsonb_agg(r.receipt) AS receipts,count(*) AS receipt_count FROM (
   SELECT receipt FROM agenteam_agent.commands WHERE project_id=a.project_id AND target_id=a.id AND command_name='agent.create' AND state='completed' LIMIT 2
  ) r
 ) c
 WHERE a.project_id=$1 AND a.lifecycle='active' AND c.receipt_count>0`

func directoryCreation(raw []byte, v c.DirectoryEntry) error {
	if len(raw) > 2*c.MaxAgentCoreBytes+2 {
		return unavailable(nil)
	}
	var receipts []json.RawMessage
	if err := json.Unmarshal(raw, &receipts); err != nil || len(receipts) != 1 {
		return unavailable(err)
	}
	receipt, err := c.DecodeAgentMutation(receipts[0])
	if err != nil {
		return unavailable(err)
	}
	initial := receipt.Fields()
	core := initial.Agent.Fields().Core
	if !initial.Changed || core.ID != v.ID || core.ProjectID != v.ProjectID || core.Version != 1 || core.Lifecycle != c.AgentActive || core.CreatedAt != v.CreatedAt {
		return unavailable(nil)
	}
	return nil
}
func loadDirectoryPage(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, limit int, after *directoryPosition) ([]c.DirectoryEntry, error) {
	query := directoryPageSQL
	args := []any{project.String(), limit + 1}
	if after != nil {
		query += ` AND (a.created_at,a.id)<($3::timestamptz,$4::uuid)`
		args = append(args, after.at.Time(), after.id.String())
	}
	query += ` ORDER BY a.created_at DESC,a.id DESC LIMIT $2`
	rows, err := x.Query(ctx, query, args...)
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	items := []c.DirectoryEntry{}
	previous := after
	for rows.Next() {
		var v c.DirectoryEntry
		var aid, pid string
		var version int64
		var created, updated time.Time
		var receipt []byte
		if err = rows.Scan(&aid, &pid, &v.Name, &v.DisplayName, &v.TagColor, &v.Description, &version, &created, &updated, &receipt); err != nil {
			return nil, unavailable(err)
		}
		if v.ID, err = f.ParseID[i.Agent](aid); err != nil {
			return nil, unavailable(err)
		}
		if v.ProjectID, err = f.ParseID[i.Project](pid); err != nil {
			return nil, unavailable(err)
		}
		v.Version = f.Version(version)
		if v.CreatedAt, err = f.NewInstant(created); err != nil {
			return nil, unavailable(err)
		}
		if v.UpdatedAt, err = f.NewInstant(updated); err != nil {
			return nil, unavailable(err)
		}
		if v.Validate() != nil || v.ProjectID != project || !directoryBefore(v, previous) || len(items) >= limit+1 {
			return nil, unavailable(nil)
		}
		if err = directoryCreation(receipt, v); err != nil {
			return nil, err
		}
		items = append(items, v)
		previous = &directoryPosition{v.CreatedAt, v.ID}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
	}
	if err = rows.Err(); err != nil {
		return nil, unavailable(err)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
