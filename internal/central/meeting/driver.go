package meeting

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"sync"
	"time"
)

// Run visits durable Meeting work. Each visit retains its original context
// until Advance returns and delegates actual work to AssociatedExecutor.
// Stop cancels visits; Joined remains false until they have all returned.
func (r *Runtime) Run(ctx context.Context) error {
	if r == nil || ctx == nil {
		return fault(f.InvalidArgument)
	}
	r.mu.Lock()
	if r.stopped || r.driverCancel != nil {
		r.mu.Unlock()
		return fault(f.ResourceBusy)
	}
	run, cancel := context.WithCancel(ctx)
	r.driverCancel = cancel
	r.mu.Unlock()
	var visits sync.WaitGroup
	defer func() { cancel(); visits.Wait(); r.mu.Lock(); r.driverCancel = nil; r.mu.Unlock() }()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	type target struct {
		project i.ProjectID
		meeting c.MeetingID
	}
	active := map[c.MeetingID]bool{}
	finished := make(chan c.MeetingID, 16)
	after := ""
	for {
		select {
		case <-run.Done():
			return nil
		case id := <-finished:
			delete(active, id)
			continue
		case <-ticker.C:
		}
		if len(active) >= 16 {
			continue
		}
		// Keyset visitation wraps when exhausted. A long summary or a failing
		// Meeting cannot starve later sessions or create an unbounded retry loop.
		rows, err := r.store.Query(run, `SELECT s.project_id,s.id FROM agenteam_meeting.sessions s WHERE s.id>$1 AND (s.deleting OR (s.status<>'proposed' AND (EXISTS(SELECT 1 FROM agenteam_meeting.turns t WHERE t.meeting_id=s.id AND t.status IN('queued','running','finalizing')) OR EXISTS(SELECT 1 FROM agenteam_meeting.contributions c JOIN agenteam_meeting.turns t ON t.id=c.turn_id WHERE c.meeting_id=s.id AND t.status='completed' AND c.kind='agent' AND c.status IN('pending','waiting_for_agent','running'))))) ORDER BY s.id LIMIT 32`, after)
		if err != nil {
			if run.Err() != nil {
				return nil
			}
			return portError(err)
		}
		pending := []target{}
		seen := false
		for rows.Next() {
			var p, m string
			if err = rows.Scan(&p, &m); err != nil {
				rows.Close()
				return portError(err)
			}
			seen = true
			after = m
			project, e := f.ParseID[i.Project](p)
			if e != nil {
				rows.Close()
				return fault(f.InternalError)
			}
			meeting, e := f.ParseID[c.Meeting](m)
			if e != nil {
				rows.Close()
				return fault(f.InternalError)
			}
			if !active[meeting] {
				pending = append(pending, target{project, meeting})
			}
			if len(pending) >= 16-len(active) {
				break
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return portError(err)
		}
		if !seen {
			after = ""
		}
		for _, v := range pending {
			active[v.meeting] = true
			visits.Add(1)
			go func(v target) {
				defer visits.Done()
				_, _ = r.Advance(run, v.project, v.meeting)
				select {
				case finished <- v.meeting:
				case <-run.Done():
				}
			}(v)
		}
	}
}
