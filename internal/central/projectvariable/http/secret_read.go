package projectvariablehttp

import (
	"net/http"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func (h *secretHandler) read(r *http.Request, actor id.Actor, project c.ProjectID, route route) ([]byte, error) {
	q, err := parseQuery(r, route.kind)
	if err != nil {
		return nil, err
	}
	if err = emptyBody(r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	switch route.kind {
	case variables:
		page, err := h.secrets.ListSecretVariables(r.Context(), actor, project, q)
		if err != nil {
			return nil, err
		}
		if page.Items == nil || !validPage(q, len(page.Items), page.NextCursor) {
			return nil, badProjection()
		}
		seen := map[c.VariableID]bool{}
		for n, item := range page.Items {
			v := item.Fields()
			if item.Validate() != nil || v.ProjectID != project || seen[v.ID] {
				return nil, badProjection()
			}
			seen[v.ID] = true
			if n > 0 {
				previous := page.Items[n-1].Fields()
				if v.Name < previous.Name || v.Name == previous.Name && v.ID.String() <= previous.ID.String() {
					return nil, badProjection()
				}
			}
		}
		return encodeValue(r.Context(), page, listLimit)
	case variable:
		target, err := f.ParseID[id.ProjectVariable](route.target)
		if err != nil {
			return nil, invalidInput()
		}
		out, err := h.secrets.GetSecretVariable(r.Context(), actor, project, target)
		if err != nil {
			return nil, err
		}
		v := out.Fields()
		if out.Validate() != nil || v.ProjectID != project || v.ID != target {
			return nil, badProjection()
		}
		return encodeValue(r.Context(), out, bodyLimit)
	}
	return nil, invalidInput()
}
