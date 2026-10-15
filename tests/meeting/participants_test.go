//go:build integration

package meeting_test

import (
	"bytes"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

func TestMeetingParticipantsPreserveExistingTurnsAndMembershipHistory(t *testing.T) {
	v := setup(t)
	_, start := v.start(t, "sequential")
	ctx, a := testContext(t), v.authority
	before, err := v.service.GetDetail(ctx, a.actor, a.project.ID, start.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	oldA, oldB := before.Participants[1], before.Participants[2]
	var original []byte
	if err = v.store.QueryRow(ctx, `SELECT participants FROM agenteam_meeting.turns WHERE id=$1`, start.TurnID.String()).Scan(&original); err != nil {
		t.Fatal(err)
	}
	request := c.UpdateParticipantsRequest{ProjectID: a.project.ID, MeetingID: start.MeetingID, ExpectedVersion: before.Session.Version, Agents: []i.AgentID{a.agents[1].ID}, Meta: meta(t)}
	removed, err := v.service.UpdateParticipants(ctx, a.actor, request)
	if err != nil {
		t.Fatal("remove", err)
	}
	if replay, found, err := v.service.LookupUpdateParticipants(ctx, a.actor, request); err != nil || !found || !replay.Replayed || replay.Version != removed.Version {
		t.Fatal("original lookup", err)
	}
	a.agents[0].Name = "alpha-renamed"
	a.agents[1].Name = "beta-renamed"
	request.ExpectedVersion, request.Meta = removed.Version, meta(t)
	request.Agents = []i.AgentID{a.agents[1].ID, a.agents[0].ID}
	added, err := v.service.UpdateParticipants(ctx, a.actor, request)
	if err != nil {
		t.Fatal("rejoin and reorder", err)
	}
	detail, err := v.service.GetDetail(ctx, a.actor, a.project.ID, start.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	var newA c.ParticipantView
	for _, p := range detail.Participants {
		switch p.ID {
		case oldA.ID:
			if p.Active || p.Name != oldA.Name {
				t.Fatal("removed identity changed")
			}
		case oldB.ID:
			if !p.Active || p.Order != 1 || p.Name != oldB.Name {
				t.Fatal("reorder changed historical identity")
			}
		default:
			if p.Kind == "agent" && p.Active {
				newA = p
			}
		}
	}
	if newA.ID.Validate() != nil || newA.ID == oldA.ID || newA.Name != "alpha-renamed" || newA.Order != 2 {
		t.Fatal("rejoin did not create fresh snapshot")
	}
	var unchanged []byte
	if err = v.store.QueryRow(ctx, `SELECT participants FROM agenteam_meeting.turns WHERE id=$1`, start.TurnID.String()).Scan(&unchanged); err != nil || !bytes.Equal(original, unchanged) {
		t.Fatal("existing Turn reordered", err)
	}
	sent, err := v.service.Send(ctx, a.actor, c.SendRequest{ProjectID: a.project.ID, MeetingID: start.MeetingID, ExpectedVersion: added.Version, Mode: "sequential", Content: []c.InlineNode{{Type: "text", Text: "Use the revised roster"}}, Meta: meta(t)})
	if err != nil {
		t.Fatal("next Turn", err)
	}
	detail, err = v.service.GetDetail(ctx, a.actor, a.project.ID, start.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, turn := range detail.Turns {
		if turn.ID == *sent.TurnID {
			found = true
			if len(turn.Contributions) != 3 || turn.Contributions[1].ParticipantID != oldB.ID || turn.Contributions[2].ParticipantID != newA.ID {
				t.Fatal("next Turn did not capture revised order")
			}
		}
	}
	if !found {
		t.Fatal("next Turn missing")
	}
	if replay, err := v.service.UpdateParticipants(ctx, a.actor, request); err != nil || !replay.Replayed || replay.Version != added.Version {
		t.Fatal("replay after newer Turn", err)
	}
	request.Meta = meta(t)
	_, err = v.service.UpdateParticipants(ctx, a.actor, request)
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.VersionConflict {
		t.Fatal("stale roster accepted", err)
	}
	other, _ := i.NewHuman(ident[i.User](t), ident[i.Session](t))
	if _, _, err = v.service.LookupUpdateParticipants(ctx, other, request); err == nil {
		t.Fatal("non-owner lookup accepted")
	}
}
