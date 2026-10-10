package workhttp

import (
	"encoding/json"
	"net/http"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type transitionIntent struct {
	meta    f.CommandMeta
	target  c.TaskID
	request c.TaskTransfer
	lookup  bool
}

// Lookup carries the original intent, never a caller-supplied digest or current
// GET substituted for the original version/request. Both envelopes retain the
// original raw request for the domain's strict TaskTransfer decoder.
func decodeTransitionIntent(w http.ResponseWriter, r *http.Request, route route) (transitionIntent, error) {
	var out transitionIntent
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
	out.lookup = route.kind == taskTransitionLookup
	target := route.target
	if out.lookup {
		var command c.TaskTransitionCommandName
		if json.Unmarshal(body.Command, &command) != nil || command != c.TaskTransitionTransfer || json.Unmarshal(body.Target, &target) != nil {
			return transitionIntent{}, invalidInput()
		}
	} else if len(body.Command) != 0 || len(body.Target) != 0 {
		return transitionIntent{}, invalidInput()
	}
	out.target, err = f.ParseID[c.Task](target)
	if err != nil {
		return transitionIntent{}, invalidInput()
	}
	var version f.Version
	if json.Unmarshal(body.Expected, &version) != nil {
		return transitionIntent{}, invalidInput()
	}
	if raw := strings.TrimSpace(string(body.Request)); len(raw) < 2 || raw[0] != '{' {
		return transitionIntent{}, invalidInput()
	}
	out.request, err = decodeRequest[c.TaskTransfer](body.Request)
	if err != nil {
		return transitionIntent{}, err
	}
	out.meta = f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: key, ExpectedVersion: &version}
	if out.meta.Validate() != nil {
		return transitionIntent{}, invalidInput()
	}
	return out, nil
}

func transitionReceiptMatches(v c.TaskTransitionMutation, project c.ProjectID, in transitionIntent) bool {
	return v.Validate() == nil && v.Task.ProjectID == project && v.Task.ID == in.target &&
		validVersion(v.Task.Version, in.meta.ExpectedVersion, true) && v.Task.State == in.request.TargetState &&
		(in.request.AssigneeAgentID == nil || sameAgent(v.Task.AssigneeAgentID, in.request.AssigneeAgentID))
}
