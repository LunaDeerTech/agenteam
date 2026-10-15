//go:build integration

package meeting_test

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

type heldSummary struct {
	started chan meeting.SummaryInput
	release chan struct{}
}

func (s *heldSummary) GenerateSummary(ctx context.Context, input meeting.SummaryInput) (meeting.SummaryCandidate, error) {
	select {
	case s.started <- input:
	case <-ctx.Done():
		return meeting.SummaryCandidate{}, ctx.Err()
	}
	select {
	case <-s.release:
	case <-ctx.Done():
		return meeting.SummaryCandidate{}, ctx.Err()
	}
	title := "A late summary must never publish"
	return meeting.SummaryCandidate{Title: &title, Goals: "late"}, nil
}

// These are durable domain transitions with an explicit Summary double. The
// actual Model handle cancellation and snapshot release require their App gate.
func TestMeetingDomainCancelSummary(t *testing.T) {
	for _, phase := range []string{"generating", "failed"} {
		t.Run(phase, func(t *testing.T) {
			v := setup(t)
			_, receipt := v.start(t, "sequential")
			held := &heldSummary{started: make(chan meeting.SummaryInput, 1), release: make(chan struct{})}
			var generator meeting.SummaryGenerator = held
			if phase == "failed" {
				generator = &failedSummary{}
			}
			runtime, _ := domainRuntime(t, v, generator)
			ctx, stop := context.WithCancel(testContext(t))
			defer stop()
			for n := 0; n < 2; n++ {
				if _, err := runtime.Advance(ctx, v.authority.project.ID, receipt.MeetingID); err != nil {
					t.Fatal("contribution", err)
				}
			}
			done := make(chan error, 1)
			joined := false
			go func() { _, err := runtime.Advance(ctx, v.authority.project.ID, receipt.MeetingID); done <- err }()
			defer func() {
				stop()
				if !joined {
					<-done
				}
			}()
			if phase == "generating" {
				select {
				case <-held.started:
				case <-ctx.Done():
					<-done
					joined = true
					t.Fatal("summary did not start")
				}
			} else {
				err := <-done
				joined = true
				if err == nil {
					t.Fatal("expected first summary failure")
				}
			}
			detail, err := v.service.GetDetail(ctx, v.authority.actor, v.authority.project.ID, receipt.MeetingID)
			if err != nil {
				t.Fatal(err)
			}
			request := c.ControlRequest{ProjectID: v.authority.project.ID, MeetingID: receipt.MeetingID, Action: "cancel-turn", TurnID: receipt.TurnID, ExpectedVersion: detail.Session.Version, Meta: meta(t)}
			if _, err = v.service.Control(ctx, v.authority.actor, request); err != nil {
				t.Fatal("cancel finalizing turn", err)
			}
			if phase == "generating" {
				close(held.release)
				err = <-done
				joined = true
				if err != nil {
					t.Fatal("late result cancellation", err)
				}
			}
			for n := 0; n < 2; n++ {
				if _, err = runtime.Advance(ctx, v.authority.project.ID, receipt.MeetingID); err != nil {
					t.Fatal("finish cancellation", err)
				}
			}
			detail, err = v.service.GetDetail(ctx, v.authority.actor, v.authority.project.ID, receipt.MeetingID)
			if err != nil || detail.Turns[0].Status != "cancelled" || detail.Session.Summary != nil || detail.Session.Title != nil {
				t.Fatal("cancelled summary became visible", err)
			}
			var state string
			if err = v.store.QueryRow(ctx, `SELECT status FROM agenteam_meeting.summary_generations WHERE turn_id=$1`, receipt.TurnID.String()).Scan(&state); err != nil || state != "cancelled" {
				t.Fatal("generation retained after cancellation", state, err)
			}
			runtime.Stop()
			if !runtime.Joined() {
				t.Fatal("summary visit escaped join")
			}
		})
	}
}
