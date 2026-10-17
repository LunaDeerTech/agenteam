package model

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// ModelProviderSummary is display metadata, never a credential or selection grant.
type ModelProviderSummary struct {
	ID       mc.ProviderID `json:"id"`
	Name     string        `json:"name"`
	Protocol mc.Protocol   `json:"protocol"`
	Enabled  bool          `json:"enabled"`
	Version  f.Version     `json:"version"`
}

type SystemModelListItem struct {
	mc.ModelView
	Provider ModelProviderSummary `json:"provider"`
}

type SystemModelPage struct {
	Items      []SystemModelListItem `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

func normalizeModelSearch(raw string) (string, error) {
	if len(raw) > 128 || !utf8.ValidString(raw) || strings.ContainsFunc(raw, unicode.IsControl) {
		return "", fault(f.InvalidArgument)
	}
	value := strings.ToLower(strings.TrimSpace(raw))
	if len(value) > 128 {
		return "", fault(f.InvalidArgument)
	}
	return value, nil
}

func modelListBinding(actor id.Actor, provider, search string, typ mc.ModelType) (cursor.Binding, error) {
	raw, err := json.Marshal(struct{ Format, User, Provider, Search, Type string }{"system-model-list-v3", actor.Details().UserID, provider, search, string(typ)})
	if err != nil {
		return cursor.Binding{}, unavailable(err)
	}
	digest, err := cursor.Digest(raw)
	if err != nil {
		return cursor.Binding{}, err
	}
	return cursor.Binding{Scope: id.SystemScope(), QueryDigest: digest, Order: cursor.AuditOrder}, nil
}

type modelProviderScanner struct {
	scanner
	provider *ModelProviderSummary
}

func (r modelProviderScanner) Scan(values ...any) error {
	return r.scanner.Scan(append(values, &r.provider.Name, &r.provider.Protocol, &r.provider.Enabled, &r.provider.Version)...)
}

// ListModels accepts a zero provider to search the whole System catalog. Each
// row and its provider display fields come from one SQL statement snapshot.
func (s *Service) ListModels(ctx context.Context, actor id.Actor, provider mc.ProviderID, q SystemQuery) (SystemModelPage, error) {
	out := SystemModelPage{Items: []SystemModelListItem{}}
	if q.Type != "" && !q.Type.Valid() {
		return out, fault(f.InvalidArgument)
	}
	filtered := provider != (mc.ProviderID{})
	if filtered && provider.Validate() != nil {
		return out, fault(f.InvalidArgument)
	}
	search, err := normalizeModelSearch(q.Search)
	if err != nil {
		return out, err
	}
	providerKey := ""
	var locks []f.LockRequest
	if filtered {
		providerKey = provider.String()
		locks = []f.LockRequest{aggregateLock(f.ProviderAggregate, providerKey, f.Shared)}
	}
	err = s.read(ctx, actor, locks, func(ctx context.Context, x postgres.SQLExecutor) error {
		if filtered {
			current, err := loadProvider(ctx, x, providerKey)
			if err != nil {
				return err
			}
			if current == nil {
				return fault(f.NotFound)
			}
		}
		binding, err := modelListBinding(actor, providerKey, search, q.Type)
		if err != nil {
			return err
		}
		position, err := s.pagePosition(q, binding)
		if err != nil {
			return err
		}
		query := `SELECT ` + modelColumns + `,p.name,p.protocol,p.enabled,p.version FROM agenteam_model.models m JOIN agenteam_model.providers p ON p.id=m.provider_id WHERE p.scope='system' AND ($1::uuid IS NULL OR m.provider_id=$1::uuid) AND ($3::text='' OR strpos(lower(p.name),$3)>0 OR strpos(lower(m.notes),$3)>0 OR strpos(lower(m.provider_model_id),$3)>0) AND ($4::text='' OR m.type=$4)`
		args := []any{null(providerKey), q.Limit + 1, search, string(q.Type)}
		if position.Present {
			query += ` AND (m.created_at,m.id)<=($5,$6::uuid) AND (m.created_at,m.id)<($7,$8::uuid)`
			args = append(args, position.WaterAt.Time(), position.WaterID, position.AfterAt.Time(), position.AfterID)
		}
		query += ` ORDER BY m.created_at DESC,m.id DESC LIMIT $2`
		rows, err := x.Query(ctx, query, args...)
		if err != nil {
			return unavailable(err)
		}
		defer rows.Close()
		for rows.Next() {
			var display ModelProviderSummary
			r, err := scanModel(modelProviderScanner{rows, &display})
			if err != nil {
				return err
			}
			v, err := r.view()
			if err != nil {
				return err
			}
			display.ID = v.ProviderID
			if !display.Protocol.Valid() || display.Version.Validate() != nil || len(display.Name) == 0 || len(display.Name) > 512 || utf8.RuneCountInString(display.Name) > 128 || !utf8.ValidString(display.Name) || strings.ContainsRune(display.Name, 0) {
				return unavailable(nil)
			}
			out.Items = append(out.Items, SystemModelListItem{v, display})
		}
		if err = rows.Err(); err != nil {
			return unavailable(err)
		}
		if len(out.Items) > q.Limit {
			out.Items = out.Items[:q.Limit]
			first, last := out.Items[0], out.Items[len(out.Items)-1]
			out.NextCursor, err = s.signPage(binding, position, first.CreatedAt, first.ID.String(), last.CreatedAt, last.ID.String())
			return err
		}
		return nil
	})
	if err != nil {
		return SystemModelPage{}, err
	}
	return out, nil
}
