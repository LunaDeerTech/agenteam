package workhttp

import (
	"encoding/json"
	"net/http"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type sprintStartIntent struct {
	meta   f.CommandMeta
	target c.SprintID
	lookup bool
}

type sprintStartLookupWire struct {
	Status  c.LookupState          `json:"status"`
	Receipt *c.SprintStartMutation `json:"receipt"`
}

func decodeSprintStartIntent(w http.ResponseWriter, r *http.Request, route route) (sprintStartIntent, error) {
	var out sprintStartIntent
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return out, invalidInput()
	}
	for name := range r.Header {
		if strings.EqualFold(name, "Content-Encoding") {
			return out, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
		}
	}
	key, err := commandKey(r)
	if err != nil {
		return out, err
	}
	var body struct {
		Command  json.RawMessage `json:"command,omitempty"`
		Target   json.RawMessage `json:"target_id,omitempty"`
		Expected json.RawMessage `json:"expected_version"`
		Request  json.RawMessage `json:"request"`
	}
	if err = httpapi.DecodeJSON(w, r, &body, bodyLimit); err != nil {
		return out, err
	}
	out.lookup = route.kind == sprintLifecycleLookup
	target := route.target
	if out.lookup {
		var command string
		if json.Unmarshal(body.Command, &command) != nil || command != c.SprintStartCommandName || json.Unmarshal(body.Target, &target) != nil {
			return sprintStartIntent{}, invalidInput()
		}
	} else if len(body.Command) != 0 || len(body.Target) != 0 {
		return sprintStartIntent{}, invalidInput()
	}
	out.target, err = f.ParseID[pc.Sprint](target)
	if err != nil {
		return sprintStartIntent{}, invalidInput()
	}
	var expected f.Version
	if json.Unmarshal(body.Expected, &expected) != nil {
		return sprintStartIntent{}, invalidInput()
	}
	// Start has no business-body arguments; the shared mutation envelope still
	// requires an explicit empty object. Null, arrays and every field reject.
	var empty map[string]json.RawMessage
	if json.Unmarshal(body.Request, &empty) != nil || empty == nil || len(empty) != 0 {
		return sprintStartIntent{}, invalidInput()
	}
	out.meta = f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: key, ExpectedVersion: &expected}
	if out.meta.Validate() != nil {
		return sprintStartIntent{}, invalidInput()
	}
	return out, nil
}

func sprintStartReceiptMatches(v c.SprintStartMutation, actor id.Actor, project c.ProjectID, in sprintStartIntent) bool {
	return v.Validate() == nil && v.Sprint.ProjectID == project && v.Sprint.ID == in.target &&
		validVersion(v.Sprint.Version, in.meta.ExpectedVersion, true) &&
		v.Project.OwnerUserID.String() == actor.Details().UserID &&
		v.Sprint.StartedBy.Kind == id.Human && v.Sprint.StartedBy.UserID == actor.Details().UserID
}
