//go:build integration

package meeting_test

import (
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

// This is a real PostgreSQL domain command test with the original fixture's
// explicit Project/Agent doubles. It does not claim an Executor/model call.
func TestMeetingUserReplyPreservesImmutableTargetAndCommand(t *testing.T) {
	v := setup(t)
	_, original := v.start(t, "sequential")
	_, other := v.start(t, "sequential")
	ctx, a := testContext(t), v.authority
	detail, err := v.service.GetDetail(ctx, a.actor, a.project.ID, original.MeetingID)
	if err != nil || len(detail.Turns) != 1 || detail.Turns[0].Contributions[0].Message == nil {
		t.Fatal("first immutable message", err)
	}
	target := detail.Turns[0].Contributions[0].Message.ID
	foreign, err := v.service.GetDetail(ctx, a.actor, a.project.ID, other.MeetingID)
	if err != nil || foreign.Turns[0].Contributions[0].Message == nil {
		t.Fatal("foreign immutable message", err)
	}
	foreignTarget := foreign.Turns[0].Contributions[0].Message.ID
	request := c.SendRequest{ProjectID: a.project.ID, MeetingID: original.MeetingID, ExpectedVersion: detail.Session.Version, Mode: "sequential", Content: []c.InlineNode{{Type: "text", Text: "Keep the original message as the reply target."}}, ReplyTo: &target, Meta: meta(t)}
	accepted, err := v.service.Send(ctx, a.actor, request)
	if err != nil || accepted.TurnID == nil {
		t.Fatal("reply Send", err)
	}
	replayed, err := v.service.Send(ctx, a.actor, request)
	if err != nil || !replayed.Replayed || replayed.TurnID == nil || *replayed.TurnID != *accepted.TurnID {
		t.Fatal("reply command replay", err)
	}
	observed, found, err := v.service.LookupSend(ctx, a.actor, request)
	if err != nil || !found || !observed.Replayed || observed.TurnID == nil || *observed.TurnID != *accepted.TurnID {
		t.Fatal("reply command lookup", err)
	}
	changed := request
	changed.ReplyTo = &foreignTarget
	if _, err = v.service.Send(ctx, a.actor, changed); err == nil {
		t.Fatal("same command accepted another reply target")
	}
	var failure *f.Fault
	if !errors.As(err, &failure) || failure.Code != f.IdempotencyKeyReused {
		t.Fatal("reply target omitted from command identity", err)
	}
	changed.Meta, changed.ExpectedVersion = meta(t), accepted.Version
	if _, err = v.service.Send(ctx, a.actor, changed); err == nil {
		t.Fatal("fresh reply referenced another Meeting")
	}
	detail, err = v.service.GetDetail(ctx, a.actor, a.project.ID, original.MeetingID)
	if err != nil || len(detail.Turns) != 2 || detail.Session.Version != accepted.Version {
		t.Fatal("rejected reply left a Turn or changed the Meeting", err)
	}
	message := detail.Turns[1].Contributions[0].Message
	if message == nil || message.ReplyTo == nil || *message.ReplyTo != target || message.ID == target || detail.Turns[0].Contributions[0].Message.ID != target {
		t.Fatal("reply failed to preserve both immutable messages")
	}
	// A later Turn may still reply to the first message; its age is not an
	// authorization failure or a reason to retarget it to the newest message.
	request.Meta, request.ExpectedVersion = meta(t), accepted.Version
	next, err := v.service.Send(ctx, a.actor, request)
	if err != nil || next.TurnID == nil {
		t.Fatal("older immutable reply target", err)
	}
	var count int
	if err = v.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_meeting.messages WHERE meeting_id=$1 AND reply_to_id=$2`, original.MeetingID.String(), target.String()).Scan(&count); err != nil || count != 2 {
		t.Fatal("original reply target was replaced or duplicated by replay", count, err)
	}
}
