package workhttp

import (
	"cmp"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type query struct {
	page      f.PageRequest
	milestone c.MilestoneID
	filter    c.TaskFilter
	status    c.TaskBlockerStatus
}

func parseQuery(r *http.Request, kind resource) (query, error) {
	q := query{page: f.DefaultPageRequest(), status: c.TaskBlockersUnresolved}
	if r == nil || r.URL == nil || len(r.URL.RawQuery) > maxQueryBytes || r.URL.RawQuery == "" && r.URL.ForceQuery {
		return q, invalidInput()
	}
	list := kind == milestones || kind == sprints || kind == tasks || kind == blockers
	if !list && r.URL.RawQuery != "" {
		return q, invalidInput()
	}
	values := map[string]string{}
	if r.URL.RawQuery != "" {
		if strings.Contains(r.URL.RawQuery, ";") {
			return q, invalidInput()
		}
		for _, pair := range strings.Split(r.URL.RawQuery, "&") {
			key, value, ok := strings.Cut(pair, "=")
			if !ok {
				return q, invalidInput()
			}
			key, err := url.QueryUnescape(key)
			if err != nil {
				return q, invalidInput()
			}
			value, err = url.QueryUnescape(value)
			if err != nil || key == "" || value == "" || !utf8.ValidString(key) || !utf8.ValidString(value) || strings.ContainsRune(key, 0) || strings.ContainsRune(value, 0) {
				return q, invalidInput()
			}
			if _, exists := values[key]; exists {
				return q, invalidInput()
			}
			if key != "limit" && key != "cursor" && !(kind == sprints && key == "milestone_id") && !(kind == blockers && key == "status") && !(kind == tasks && strings.Contains("|state|priority|type|milestone_id|sprint_id|text|assignee_agent_id|", "|"+key+"|")) {
				return q, invalidInput()
			}
			values[key] = value
		}
	}
	if v, ok := values["limit"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || strconv.Itoa(n) != v || n < 1 || n > 200 {
			return q, invalidInput()
		}
		q.page.Limit = n
	}
	q.page.Cursor = values["cursor"]
	if len(q.page.Cursor) > maxCursorBytes {
		return q, f.NewFault(f.CursorInvalid, f.NotStarted)
	}
	if kind == sprints {
		v, err := f.ParseID[c.Milestone](values["milestone_id"])
		if err != nil {
			return q, invalidInput()
		}
		q.milestone = v
	}
	if v, ok := values["status"]; ok {
		q.status = c.TaskBlockerStatus(v)
		if q.status.Validate() != nil {
			return q, invalidInput()
		}
	}
	if kind == tasks {
		if v, ok := values["state"]; ok {
			x := c.TaskState(v)
			q.filter.State = &x
		}
		if v, ok := values["priority"]; ok {
			x := c.TaskPriority(v)
			q.filter.Priority = &x
		}
		if v, ok := values["type"]; ok {
			x := c.TaskType(v)
			q.filter.Type = &x
		}
		if v, ok := values["text"]; ok {
			q.filter.Text = &v
		}
		if v, ok := values["milestone_id"]; ok {
			x, err := f.ParseID[c.Milestone](v)
			if err != nil {
				return q, invalidInput()
			}
			q.filter.MilestoneID = &x
		}
		if v, ok := values["sprint_id"]; ok {
			x, err := f.ParseID[pc.Sprint](v)
			if err != nil {
				return q, invalidInput()
			}
			q.filter.SprintID = &x
		}
		if v, ok := values["assignee_agent_id"]; ok {
			q.filter.AssigneeAgentID.Present = true
			if v != "null" {
				x, err := f.ParseID[id.Agent](v)
				if err != nil {
					return q, invalidInput()
				}
				q.filter.AssigneeAgentID.AgentID = &x
			}
		}
		if q.filter.Validate() != nil {
			return q, invalidInput()
		}
	}
	return q, nil
}

func (h *handler) read(r *http.Request, a id.Actor, p c.ProjectID, route route) ([]byte, error) {
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
	ctx := r.Context()
	switch route.kind {
	case milestones:
		page, err := h.structureReader.ListMilestones(ctx, a, p, q.page)
		if err != nil {
			return nil, err
		}
		if !validPage(q.page, len(page.Items), page.NextCursor) {
			return nil, badProjection()
		}
		out := f.Page[milestoneSummary]{Items: make([]milestoneSummary, 0, len(page.Items)), NextCursor: page.NextCursor}
		seen := map[c.MilestoneID]bool{}
		for n, v := range page.Items {
			if v.Validate() != nil || v.ProjectID != p || seen[v.ID] || n > 0 && compareRank(page.Items[n-1].ManualRank, page.Items[n-1].ID.String(), v.ManualRank, v.ID.String()) >= 0 {
				return nil, badProjection()
			}
			seen[v.ID] = true
			out.Items = append(out.Items, summarizeMilestone(v))
		}
		return encodeValue(ctx, out, listLimit)
	case milestone:
		target, err := f.ParseID[c.Milestone](route.target)
		if err != nil {
			return nil, invalidInput()
		}
		v, err := h.structureReader.GetMilestone(ctx, a, p, target)
		if err != nil {
			return nil, err
		}
		if v.Validate() != nil || v.ProjectID != p || v.ID != target {
			return nil, badProjection()
		}
		return encodeValue(ctx, v, bodyLimit)
	case sprints:
		page, err := h.structureReader.ListSprints(ctx, a, p, q.milestone, q.page)
		if err != nil {
			return nil, err
		}
		if !validPage(q.page, len(page.Items), page.NextCursor) {
			return nil, badProjection()
		}
		out := f.Page[sprintSummary]{Items: make([]sprintSummary, 0, len(page.Items)), NextCursor: page.NextCursor}
		seen := map[c.SprintID]bool{}
		for n, v := range page.Items {
			if v.Validate() != nil || v.ProjectID != p || v.MilestoneID != q.milestone || seen[v.ID] || n > 0 && compareRank(page.Items[n-1].ManualRank, page.Items[n-1].ID.String(), v.ManualRank, v.ID.String()) >= 0 {
				return nil, badProjection()
			}
			seen[v.ID] = true
			out.Items = append(out.Items, summarizeSprint(v))
		}
		return encodeValue(ctx, out, listLimit)
	case sprint:
		target, err := f.ParseID[pc.Sprint](route.target)
		if err != nil {
			return nil, invalidInput()
		}
		v, err := h.structureReader.GetSprint(ctx, a, p, target)
		if err != nil {
			return nil, err
		}
		if v.Validate() != nil || v.ProjectID != p || v.ID != target {
			return nil, badProjection()
		}
		return encodeValue(ctx, v, bodyLimit)
	case tasks:
		page, err := h.taskReader.ListTasks(ctx, a, p, q.filter, q.page)
		if err != nil {
			return nil, err
		}
		if !validPage(q.page, len(page.Items), page.NextCursor) {
			return nil, badProjection()
		}
		out := f.Page[taskSummary]{Items: make([]taskSummary, 0, len(page.Items)), NextCursor: page.NextCursor}
		seen := map[c.TaskID]bool{}
		for n, v := range page.Items {
			if v.Validate() != nil || v.ProjectID != p || !matchesTask(v, q.filter) || seen[v.ID] || n > 0 && compareTask(page.Items[n-1], v) >= 0 {
				return nil, badProjection()
			}
			seen[v.ID] = true
			out.Items = append(out.Items, summarizeTask(v))
		}
		return encodeValue(ctx, out, listLimit)
	case task:
		target, err := f.ParseID[c.Task](route.target)
		if err != nil {
			return nil, invalidInput()
		}
		v, err := h.taskReader.GetTask(ctx, a, p, target)
		if err != nil {
			return nil, err
		}
		if v.Validate() != nil || v.ProjectID != p || v.ID != target {
			return nil, badProjection()
		}
		return encodeValue(ctx, v, bodyLimit)
	case blockers:
		target, err := f.ParseID[c.Task](route.target)
		if err != nil {
			return nil, invalidInput()
		}
		page, err := h.blockerReader.ListTaskBlockersPage(ctx, a, p, target, q.status, q.page)
		if err != nil {
			return nil, err
		}
		if !validPage(q.page, len(page.Items), page.NextCursor) {
			return nil, badProjection()
		}
		seen := map[c.TaskBlockerID]bool{}
		for n, v := range page.Items {
			if v.Validate() != nil || v.ProjectID != p || v.TaskID != target || seen[v.ID] || q.status == c.TaskBlockersResolved && v.ResolvedAt == nil || q.status == c.TaskBlockersUnresolved && v.ResolvedAt != nil {
				return nil, badProjection()
			}
			if n > 0 {
				prev := page.Items[n-1]
				if prev.CreatedAt.Time().After(v.CreatedAt.Time()) || prev.CreatedAt.Time().Equal(v.CreatedAt.Time()) && prev.ID.String() >= v.ID.String() {
					return nil, badProjection()
				}
			}
			seen[v.ID] = true
		}
		return encodeValue(ctx, page, listLimit)
	}
	return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
}
func compareRank(ar, ai, br, bi string) int {
	if n := strings.Compare(ar, br); n != 0 {
		return n
	}
	return strings.Compare(ai, bi)
}
func compareTask(a, b c.Task) int {
	if n := strings.Compare(a.SprintID.String(), b.SprintID.String()); n != 0 {
		return n
	}
	if n := cmp.Compare(a.State.Order(), b.State.Order()); n != 0 {
		return n
	}
	if n := cmp.Compare(a.Priority.Order(), b.Priority.Order()); n != 0 {
		return n
	}
	return compareRank(a.ManualRank, a.ID.String(), b.ManualRank, b.ID.String())
}
func matchesTask(v c.Task, q c.TaskFilter) bool {
	if q.State != nil && v.State != *q.State || q.Priority != nil && v.Priority != *q.Priority || q.Type != nil && v.Type != *q.Type || q.MilestoneID != nil && v.MilestoneID != *q.MilestoneID || q.SprintID != nil && v.SprintID != *q.SprintID {
		return false
	}
	if q.AssigneeAgentID.Present {
		want := q.AssigneeAgentID.AgentID
		if (want == nil) != (v.AssigneeAgentID == nil) || want != nil && *want != *v.AssigneeAgentID {
			return false
		}
	}
	return q.Text == nil || strings.Contains(v.Title, *q.Text) || strings.Contains(v.Description, *q.Text) || strings.Contains(v.Plan, *q.Text)
}
