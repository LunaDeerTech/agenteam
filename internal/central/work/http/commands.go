package workhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type intent struct {
	name    string
	target  string
	meta    f.CommandMeta
	request json.RawMessage
	lookup  bool
}

func commandKey(r *http.Request) (f.IdempotencyKey, error) {
	var values []string
	for key, v := range r.Header {
		if strings.EqualFold(key, "Idempotency-Key") {
			values = append(values, v...)
		}
	}
	if len(values) != 1 || f.IdempotencyKey(values[0]).Validate() != nil {
		return "", invalidInput()
	}
	return f.IdempotencyKey(values[0]), nil
}
func decodeIntent(w http.ResponseWriter, r *http.Request, route route) (intent, error) {
	var out intent
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return out, invalidInput()
	}
	key, err := commandKey(r)
	if err != nil {
		return out, err
	}
	var v struct {
		Command  json.RawMessage `json:"command"`
		Target   json.RawMessage `json:"target_id"`
		Expected json.RawMessage `json:"expected_version"`
		Request  json.RawMessage `json:"request"`
	}
	if err = httpapi.DecodeJSON(w, r, &v, bodyLimit); err != nil {
		return out, err
	}
	out = intent{target: route.target, meta: f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: key}, request: v.Request, lookup: route.lookup()}
	if len(bytes.TrimSpace(v.Request)) == 0 || bytes.TrimSpace(v.Request)[0] != '{' {
		return intent{}, invalidInput()
	}
	if out.lookup {
		if json.Unmarshal(v.Command, &out.name) != nil {
			return intent{}, invalidInput()
		}
		switch route.kind {
		case structureLookup:
			if c.CommandName(out.name).Validate() != nil {
				return intent{}, invalidInput()
			}
		case taskLookup:
			if c.TaskCommandName(out.name).Validate() != nil {
				return intent{}, invalidInput()
			}
		case blockerLookup:
			if c.TaskBlockerCommandName(out.name).Validate() != nil {
				return intent{}, invalidInput()
			}
		}
	} else {
		if len(v.Command) != 0 || len(v.Target) != 0 {
			return intent{}, invalidInput()
		}
		switch route.kind {
		case milestones:
			out.name = string(c.MilestoneCreate)
		case milestone:
			out.name = string(c.MilestoneUpdate)
		case milestoneReorder:
			out.name = string(c.MilestoneReorder)
		case sprints:
			out.name = string(c.SprintCreate)
		case sprint:
			out.name = string(c.SprintUpdate)
		case sprintReorder:
			out.name = string(c.SprintReorder)
		case tasks:
			out.name = string(c.TaskCommandCreate)
		case task:
			out.name = string(c.TaskCommandUpdate)
		case taskReorder:
			out.name = string(c.TaskCommandReorder)
		case blockers:
			out.name = string(c.TaskBlockerCommandAdd)
		case blockerResolve:
			out.name = string(c.TaskBlockerCommandResolve)
		default:
			return intent{}, invalidInput()
		}
	}
	create := out.name == string(c.MilestoneCreate) || out.name == string(c.SprintCreate) || out.name == string(c.TaskCommandCreate)
	if create {
		if len(v.Expected) != 0 || len(v.Target) != 0 {
			return intent{}, invalidInput()
		}
	} else {
		var expected f.Version
		if json.Unmarshal(v.Expected, &expected) != nil {
			return intent{}, invalidInput()
		}
		out.meta.ExpectedVersion = &expected
		if out.lookup && route.kind != blockerLookup {
			if json.Unmarshal(v.Target, &out.target) != nil {
				return intent{}, invalidInput()
			}
		} else if len(v.Target) != 0 {
			return intent{}, invalidInput()
		}
	}
	if out.meta.Validate() != nil {
		return intent{}, invalidInput()
	}
	return out, nil
}
func decodeRequest[T any](raw []byte) (T, error) {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		var fault *f.Fault
		if errors.As(err, &fault) {
			return v, err
		}
		return v, invalidInput()
	}
	return v, nil
}
func (h *handler) command(w http.ResponseWriter, r *http.Request, a id.Actor, p c.ProjectID, route route) ([]byte, error) {
	i, err := decodeIntent(w, r, route)
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	switch route.kind {
	case milestones, milestone, milestoneReorder, sprints, sprint, sprintReorder, structureLookup:
		return h.structureCommand(r, a, p, i)
	case tasks, task, taskReorder, taskLookup:
		return h.taskCommand(r, a, p, i)
	case blockers, blockerResolve, blockerLookup:
		return h.blockerCommand(r, a, p, i)
	}
	return nil, invalidInput()
}
func validVersion(version f.Version, expected *f.Version, changed bool) bool {
	if expected == nil {
		return changed && version == 1
	}
	if !changed {
		return version == *expected
	}
	return *expected < f.Version(math.MaxInt64) && version == *expected+1
}
func (h *handler) structureCommand(r *http.Request, a id.Actor, p c.ProjectID, in intent) ([]byte, error) {
	ctx := r.Context()
	name := c.CommandName(in.name)
	var digest f.Digest
	var err error
	var call func() (c.StructureMutation, error)
	var match func(c.StructureMutation) bool
	switch name {
	case c.MilestoneCreate:
		q, e := decodeRequest[c.CreateMilestoneRequest](in.request)
		if e != nil {
			return nil, e
		}
		digest, err = c.CreateMilestoneDigest(a, in.meta, p, q)
		call = func() (c.StructureMutation, error) { return h.structure.CreateMilestone(ctx, a, in.meta, p, q) }
		match = func(v c.StructureMutation) bool {
			return v.Milestone != nil && v.Milestone.ID == q.MilestoneID && v.Milestone.Title == q.Title && v.Milestone.Description == q.Description
		}
	case c.MilestoneUpdate:
		target, e := f.ParseID[c.Milestone](in.target)
		if e != nil {
			return nil, invalidInput()
		}
		q, e := decodeRequest[c.UpdateFields](in.request)
		if e != nil {
			return nil, e
		}
		digest, err = c.UpdateMilestoneDigest(a, in.meta, p, target, q)
		call = func() (c.StructureMutation, error) { return h.structure.UpdateMilestone(ctx, a, in.meta, p, target, q) }
		match = func(v c.StructureMutation) bool {
			return v.Milestone != nil && v.Milestone.ID == target && (q.Title == nil || v.Milestone.Title == *q.Title) && (q.Description == nil || v.Milestone.Description == *q.Description)
		}
	case c.MilestoneReorder:
		target, e := f.ParseID[c.Milestone](in.target)
		if e != nil {
			return nil, invalidInput()
		}
		q, e := decodeRequest[c.ReorderMilestoneRequest](in.request)
		if e != nil {
			return nil, e
		}
		digest, err = c.ReorderMilestoneDigest(a, in.meta, p, target, q)
		call = func() (c.StructureMutation, error) {
			return h.structure.ReorderMilestone(ctx, a, in.meta, p, target, q)
		}
		match = func(v c.StructureMutation) bool { return v.Milestone != nil && v.Milestone.ID == target }
	case c.SprintCreate:
		q, e := decodeRequest[c.CreateSprintRequest](in.request)
		if e != nil {
			return nil, e
		}
		digest, err = c.CreateSprintDigest(a, in.meta, p, q)
		call = func() (c.StructureMutation, error) { return h.structure.CreateSprint(ctx, a, in.meta, p, q) }
		match = func(v c.StructureMutation) bool {
			return v.Sprint != nil && v.Sprint.ID == q.SprintID && v.Sprint.MilestoneID == q.MilestoneID && v.Sprint.Title == q.Title && v.Sprint.Description == q.Description && v.Sprint.State == c.Planned
		}
	case c.SprintUpdate:
		target, e := f.ParseID[pc.Sprint](in.target)
		if e != nil {
			return nil, invalidInput()
		}
		q, e := decodeRequest[c.UpdateFields](in.request)
		if e != nil {
			return nil, e
		}
		digest, err = c.UpdateSprintDigest(a, in.meta, p, target, q)
		call = func() (c.StructureMutation, error) { return h.structure.UpdateSprint(ctx, a, in.meta, p, target, q) }
		match = func(v c.StructureMutation) bool {
			return v.Sprint != nil && v.Sprint.ID == target && (q.Title == nil || v.Sprint.Title == *q.Title) && (q.Description == nil || v.Sprint.Description == *q.Description)
		}
	case c.SprintReorder:
		target, e := f.ParseID[pc.Sprint](in.target)
		if e != nil {
			return nil, invalidInput()
		}
		q, e := decodeRequest[c.ReorderSprintRequest](in.request)
		if e != nil {
			return nil, e
		}
		digest, err = c.ReorderSprintDigest(a, in.meta, p, target, q)
		call = func() (c.StructureMutation, error) { return h.structure.ReorderSprint(ctx, a, in.meta, p, target, q) }
		match = func(v c.StructureMutation) bool {
			return v.Sprint != nil && v.Sprint.ID == target && v.Sprint.MilestoneID == q.MilestoneID
		}
	default:
		return nil, invalidInput()
	}
	if err != nil {
		return nil, err
	}
	valid := func(v c.StructureMutation) bool {
		if v.Validate() != nil || v.Command != name || !match(v) {
			return false
		}
		if v.Sprint != nil {
			return v.Sprint.ProjectID == p && validVersion(v.Sprint.Version, in.meta.ExpectedVersion, v.Changed)
		}
		return v.Milestone.ProjectID == p && validVersion(v.Milestone.Version, in.meta.ExpectedVersion, v.Changed)
	}
	if in.lookup {
		v, e := h.structure.LookupCommand(ctx, a, c.CommandLookupRequest{ProjectID: p, Command: name, Key: in.meta.IdempotencyKey, Semantic: digest})
		if e != nil {
			return nil, e
		}
		if v.Validate() != nil || v.Result != nil && !valid(*v.Result) {
			return nil, badProjection()
		}
		return encodeValue(ctx, v, bodyLimit)
	}
	v, e := call()
	if e != nil {
		return nil, e
	}
	if !valid(v) {
		panic(http.ErrAbortHandler)
	}
	raw, e := encodeValue(ctx, v, bodyLimit)
	if e != nil {
		panic(http.ErrAbortHandler)
	}
	return raw, nil
}
func (h *handler) taskCommand(r *http.Request, a id.Actor, p c.ProjectID, in intent) ([]byte, error) {
	ctx := r.Context()
	name := c.TaskCommandName(in.name)
	var digest f.Digest
	var err error
	var call func() (c.TaskMutation, error)
	var match func(c.Task) bool
	switch name {
	case c.TaskCommandCreate:
		q, e := decodeRequest[c.TaskCreate](in.request)
		if e != nil {
			return nil, e
		}
		digest, err = c.TaskCreateDigest(a, in.meta, p, q)
		call = func() (c.TaskMutation, error) { return h.tasks.CreateTask(ctx, a, in.meta, p, q) }
		match = func(v c.Task) bool {
			return v.ID == q.TaskID && v.SprintID == q.SprintID && v.Title == q.Title && v.Description == q.Description && v.Plan == q.Plan && v.Type == q.Type && v.Priority == q.Priority && v.State == q.EffectiveState() && sameAgent(v.AssigneeAgentID, q.AssigneeAgentID)
		}
	case c.TaskCommandUpdate:
		target, e := f.ParseID[c.Task](in.target)
		if e != nil {
			return nil, invalidInput()
		}
		q, e := decodeRequest[c.TaskFieldsUpdate](in.request)
		if e != nil {
			return nil, e
		}
		digest, err = c.TaskUpdateDigest(a, in.meta, p, target, q)
		call = func() (c.TaskMutation, error) { return h.tasks.UpdateTask(ctx, a, in.meta, p, target, q) }
		match = func(v c.Task) bool {
			return v.ID == target && (q.Title == nil || v.Title == *q.Title) && (q.Description == nil || v.Description == *q.Description) && (q.Plan == nil || v.Plan == *q.Plan) && (q.Type == nil || v.Type == *q.Type) && (q.Priority == nil || v.Priority == *q.Priority)
		}
	case c.TaskCommandReorder:
		target, e := f.ParseID[c.Task](in.target)
		if e != nil {
			return nil, invalidInput()
		}
		q, e := decodeRequest[c.TaskReorder](in.request)
		if e != nil {
			return nil, e
		}
		digest, err = c.TaskReorderDigest(a, in.meta, p, target, q)
		call = func() (c.TaskMutation, error) { return h.tasks.ReorderTask(ctx, a, in.meta, p, target, q) }
		match = func(v c.Task) bool { return v.ID == target }
	default:
		return nil, invalidInput()
	}
	if err != nil {
		return nil, err
	}
	valid := func(v c.TaskMutation) bool {
		return v.Validate() == nil && v.Task.ProjectID == p && v.Task.State == c.TaskStateBacklog && v.Task.AssigneeAgentID == nil && match(v.Task) && validVersion(v.Task.Version, in.meta.ExpectedVersion, v.Changed)
	}
	if in.lookup {
		v, e := h.tasks.LookupTaskCommand(ctx, a, c.TaskCommandLookupRequest{ProjectID: p, Command: name, IdempotencyKey: in.meta.IdempotencyKey, SemanticDigest: digest})
		if e != nil {
			return nil, e
		}
		if v.Validate() != nil || v.Receipt != nil && !valid(*v.Receipt) {
			return nil, badProjection()
		}
		return encodeValue(ctx, v, bodyLimit)
	}
	v, e := call()
	if e != nil {
		return nil, e
	}
	if !valid(v) {
		panic(http.ErrAbortHandler)
	}
	raw, e := encodeValue(ctx, v, bodyLimit)
	if e != nil {
		panic(http.ErrAbortHandler)
	}
	return raw, nil
}
func sameAgent(a, b *id.AgentID) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
func sameString(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func (h *handler) blockerCommand(r *http.Request, a id.Actor, p c.ProjectID, in intent) ([]byte, error) {
	ctx := r.Context()
	name := c.TaskBlockerCommandName(in.name)
	target, err := f.ParseID[c.Task](in.target)
	if err != nil {
		return nil, invalidInput()
	}
	var digest f.Digest
	var call func() (c.TaskBlockerMutation, error)
	var match func(c.TaskBlocker) bool
	switch name {
	case c.TaskBlockerCommandAdd:
		q, e := decodeRequest[c.TaskBlockerCreate](in.request)
		if e != nil {
			return nil, e
		}
		digest, err = c.TaskBlockerAddDigest(a, in.meta, p, target, q)
		call = func() (c.TaskBlockerMutation, error) { return h.blockers.AddTaskBlocker(ctx, a, in.meta, p, target, q) }
		match = func(v c.TaskBlocker) bool {
			if v.ID != q.BlockerID || v.Type != q.Type || v.Description != q.Description || v.ResolvedAt != nil || v.CreatedBy.Type != id.Human || v.CreatedBy.UserID.String() != a.Details().UserID {
				return false
			}
			return q.Type != c.TaskBlockerRelyOn || v.Metadata.RelyOn.RelatedTaskID == q.Metadata.RelyOn.RelatedTaskID
		}
	case c.TaskBlockerCommandResolve:
		q, e := decodeRequest[c.TaskBlockerResolve](in.request)
		if e != nil {
			return nil, e
		}
		digest, err = c.TaskBlockerResolveDigest(a, in.meta, p, target, q)
		call = func() (c.TaskBlockerMutation, error) {
			return h.blockers.ResolveTaskBlocker(ctx, a, in.meta, p, target, q)
		}
		match = func(v c.TaskBlocker) bool {
			return v.ID == q.BlockerID && v.ResolvedAt != nil && v.ResolvedBy != nil && v.ResolvedBy.Type == id.Human && v.ResolvedBy.UserID.String() == a.Details().UserID && sameString(v.ResolutionComment, q.ResolutionComment)
		}
	default:
		return nil, invalidInput()
	}
	if err != nil {
		return nil, err
	}
	valid := func(v c.TaskBlockerMutation) bool {
		return v.Validate() == nil && v.Task.ProjectID == p && v.Task.ID == target && v.Task.State == c.TaskStateBacklog && v.Task.AssigneeAgentID == nil && validVersion(v.Task.Version, in.meta.ExpectedVersion, true) && match(v.Blocker)
	}
	if in.lookup {
		v, e := h.blockers.LookupTaskBlockerCommand(ctx, a, c.TaskBlockerCommandLookupRequest{ProjectID: p, Command: name, IdempotencyKey: in.meta.IdempotencyKey, SemanticDigest: digest})
		if e != nil {
			return nil, e
		}
		if v.Validate() != nil || v.Receipt != nil && !valid(*v.Receipt) {
			return nil, badProjection()
		}
		return encodeValue(ctx, v, bodyLimit)
	}
	v, e := call()
	if e != nil {
		return nil, e
	}
	if !valid(v) {
		panic(http.ErrAbortHandler)
	}
	raw, e := encodeValue(ctx, v, bodyLimit)
	if e != nil {
		panic(http.ErrAbortHandler)
	}
	return raw, nil
}
