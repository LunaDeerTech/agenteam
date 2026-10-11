package workhttp

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Timeline may end a page at the byte budget before reaching the item limit.
// The existing planning list rule intentionally remains unchanged.
func validTimelinePage(request f.PageRequest, page f.Page[c.TaskTimelineEvent]) bool {
	return request.Validate() == nil && page.Items != nil && len(page.Items) <= request.Limit &&
		len(page.NextCursor) <= maxCursorBytes && utf8.ValidString(page.NextCursor) &&
		!strings.ContainsRune(page.NextCursor, 0) && (page.NextCursor == "" || len(page.Items) > 0)
}

func (h *handler) readTimeline(r *http.Request, actor id.Actor, project c.ProjectID, target string, q query) ([]byte, error) {
	task, err := f.ParseID[c.Task](target)
	if err != nil {
		return nil, invalidInput()
	}
	if h.timelineReader == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	page, err := h.timelineReader.ListTaskEvents(r.Context(), actor, project, task, q.timeline, q.page)
	if err != nil {
		return nil, err
	}
	if !validTimelinePage(q.page, page) {
		return nil, badProjection()
	}
	seen := make(map[c.TaskEventID]bool, len(page.Items))
	var previous c.TaskTimelineEventHeader
	itemsBytes := 2 // Array brackets; subsequent entries also need a comma.
	for n, item := range page.Items {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		header, err := item.Header()
		if err != nil || header.ProjectID != project || header.TaskID != task || seen[header.ID] ||
			q.timeline.Types != nil && !slices.Contains(q.timeline.Types, header.Type) {
			return nil, badProjection()
		}
		if n > 0 {
			order := previous.CreatedAt.Time().Compare(header.CreatedAt.Time())
			if order == 0 {
				order = strings.Compare(previous.ID.String(), header.ID.String())
			}
			if q.timeline.Order == c.TaskTimelineAscending && order >= 0 || q.timeline.Order == c.TaskTimelineDescending && order <= 0 {
				return nil, badProjection()
			}
			itemsBytes++
		}
		raw, err := json.Marshal(item)
		if err != nil || len(raw) > c.MaxTaskTimelineItemsBytes-itemsBytes {
			return nil, badProjection()
		}
		itemsBytes += len(raw)
		seen[header.ID] = true
		previous = header
	}
	// The reader signs the last returned event. HTTP neither cuts a page nor
	// invents a cursor, and publishes only the complete validated projection.
	return encodeValue(r.Context(), page, c.MaxTaskTimelineResponseBytes)
}
