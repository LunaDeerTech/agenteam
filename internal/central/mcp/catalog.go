package mcp

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type ResourceEntry struct {
	ID           string       `json:"id"`
	ConnectionID ConnectionID `json:"connection_id"`
	Kind         string       `json:"kind"`
	URI          string       `json:"uri"`
	Name         string       `json:"name"`
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	MIMEType     string       `json:"mime_type"`
	ByteSize     *int64       `json:"byte_size,omitempty"`
	Revision     f.Version    `json:"revision"`
}

func (s *Service) ListResources(ctx context.Context, actor id.Actor, projectID id.ProjectID, connection ConnectionID) ([]ResourceEntry, error) {
	scope, err := id.InProject(projectID)
	if err != nil || connection.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	var out []ResourceEntry
	err = s.read(ctx, actor, scope, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		if _, err := scanConnection(x.QueryRow(ctx, `SELECT `+connectionColumns+` FROM agenteam_mcp.connections WHERE id=$1 AND project_id=$2`, connection.String(), projectID.String())); err != nil {
			return err
		}
		rows, err := x.Query(ctx, `SELECT id,kind,uri,name,title,description,mime_type,byte_size,catalog_revision FROM agenteam_mcp.resource_catalog WHERE connection_id=$1 AND project_id=$2 AND active ORDER BY kind,uri COLLATE "C"`, connection.String(), projectID.String())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r := ResourceEntry{ConnectionID: connection}
			if err = rows.Scan(&r.ID, &r.Kind, &r.URI, &r.Name, &r.Title, &r.Description, &r.MIMEType, &r.ByteSize, &r.Revision); err != nil {
				return err
			}
			if len(out) >= 1024 {
				return fail(f.PayloadTooLarge)
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}
