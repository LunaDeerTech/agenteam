package executionhttp

import (
	"encoding/json"
	"net/http"
	"strings"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func (h *handler) cancelCommand(w http.ResponseWriter, r *http.Request, actor i.Actor, route route) ([]byte, error) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, invalidInput()
	}
	for _, name := range []string{"Content-Encoding", "Range", "If-Range"} {
		if len(r.Header.Values(name)) != 0 {
			return nil, invalidInput()
		}
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || keys[0] != strings.TrimSpace(keys[0]) {
		return nil, invalidInput()
	}
	key := f.IdempotencyKey(keys[0])
	if key.Validate() != nil {
		return nil, invalidInput()
	}
	project, err := f.ParseID[i.Project](route.project)
	if err != nil || project.String() != route.project {
		return nil, invalidInput()
	}
	execution, err := f.ParseID[i.Execution](route.execution)
	if err != nil || execution.String() != route.execution {
		return nil, invalidInput()
	}
	task, err := f.ParseID[struct{}](route.task)
	if err != nil || task.String() != route.task {
		return nil, invalidInput()
	}
	var body struct {
		Expected    f.Version       `json:"expected_version"`
		TaskVersion f.Version       `json:"expected_task_version"`
		Reason      json.RawMessage `json:"reason,omitempty"`
	}
	if err = httpapi.DecodeJSON(w, r, &body, 4096); err != nil {
		return nil, err
	}
	request := ec.CancelRequest{ProjectID: project, TaskID: task.String(), ExecutionID: execution, ExpectedVersion: body.Expected, ExpectedTaskVersion: body.TaskVersion, Meta: f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: key}}
	if len(body.Reason) != 0 {
		var reason string
		if strings.TrimSpace(string(body.Reason)) == "null" || json.Unmarshal(body.Reason, &reason) != nil {
			return nil, invalidInput()
		}
		request.Reason = &reason
	}
	if request.Validate() != nil {
		return nil, invalidInput()
	}
	if h.controls == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if route.lookup {
		out, err := h.controls.LookupCancel(r.Context(), actor, request)
		if err != nil {
			return nil, err
		}
		if out.Found != (out.Receipt != nil) || out.Receipt != nil && !validCancelReceipt(*out.Receipt, request) {
			return nil, unavailableOutput()
		}
		return json.Marshal(out)
	}
	out, err := h.controls.Cancel(r.Context(), actor, request)
	if err != nil {
		return nil, err
	}
	// Mutation may already be committed; an invalid receipt must abort, not
	// turn into a false NotStarted response that encourages a second command.
	if !validCancelReceipt(out.Receipt, request) {
		panic(http.ErrAbortHandler)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	return raw, nil
}

func validCancelReceipt(receipt ec.CancelReceipt, r ec.CancelRequest) bool {
	e := receipt.Execution
	return validExecution(e, r.ProjectID, r.TaskID) == nil && e.ID == r.ExecutionID && !e.Status.Terminal() && e.CancelRequestedAt != nil && e.Version == r.ExpectedVersion+1 && receipt.Task.Validate() == nil && receipt.Task.ID == r.TaskID && receipt.Task.Version == r.ExpectedTaskVersion+1
}
