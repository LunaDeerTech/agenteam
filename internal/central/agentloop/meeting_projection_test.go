package agentloop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

func TestMeetingProjectionUsesFrozenConversationWithoutRuntimeControl(t *testing.T) {
	fields := requestFields(t, "task/work")
	meetingID := requestID[c.Meeting](t, 201)
	participant := requestID[c.Participant](t, 202)
	turn := requestID[c.Turn](t, 203)
	contribution := requestID[c.Contribution](t, 204)
	fields.Request.Launch.Trigger = ec.Trigger{Kind: "meeting", MeetingID: meetingID.String(), ParticipantID: participant.String(), TurnID: turn.String(), ContributionID: contribution.String()}
	fields.Request.Launch.Purpose = "meeting/response"
	one := f.Version(1)
	fields.Request.Launch.Lineage.ContributionGeneration = &one
	fields.Request.Launch.Lineage.ContributionAttempt = &one
	nodes := []c.InlineNode{{Type: "text", Text: "Earlier Agent's verified public finding"}}
	body, err := json.Marshal(nodes)
	requestOK(t, err)
	frozen := meeting.FrozenContext{SchemaVersion: 1, ProjectID: fields.Request.Launch.ProjectID, Meeting: meeting.MeetingIdentity{ID: meetingID, Status: "active"}, Participants: []c.ParticipantView{{ID: participant, MeetingID: meetingID, Kind: "agent", SourceID: fields.Request.Launch.AgentID.String(), Name: "Reviewer", Active: true}}, References: []c.Reference{}, Messages: []meeting.FrozenMessage{{ContributionID: contribution, Generation: 1, Attempt: 1, ContentDigest: ec.TriggerInputDigest(body), Message: c.MessageView{ID: requestID[c.Message](t, 205), ParticipantID: participant, Content: nodes, CreatedAt: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)}}}}
	// The input digest uses canonical JSON, as the Meeting repository does.
	canonicalBody, err := cursor.CanonicalJSON(body)
	requestOK(t, err)
	frozen.Messages[0].ContentDigest = ec.TriggerInputDigest(canonicalBody)
	raw, err := json.Marshal(frozen)
	requestOK(t, err)
	fields.Trigger, err = ec.NewCapturedTriggerInput(ec.TriggerInputRef{ProviderType: "meeting", SchemaVersion: 1, InputID: requestID[ec.TriggerInput](t, 206), Digest: ec.TriggerInputDigest(raw)}, raw)
	requestOK(t, err)
	input, err := ec.NewPreparationInput(fields)
	requestOK(t, err)
	trigger, err := (meeting.ContextBuilder{}).BuildTriggerContext(context.Background(), fields.Request, fields.Trigger)
	requestOK(t, err)
	built, err := ec.NewExecutionContext(input, trigger)
	requestOK(t, err)
	messages, err := projectMessages(context.Background(), built)
	requestOK(t, err)
	encoded, err := json.Marshal(messages)
	requestOK(t, err)
	text := string(encoded)
	if !strings.Contains(text, "Earlier Agent") || !strings.Contains(text, "Reviewer") {
		t.Fatal("captured conversation missing")
	}
	for _, forbidden := range []string{turn.String(), contribution.String(), "contribution_id", "generation", "attempt", "source_id", "waiting_reference"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("runtime control leaked: %s", forbidden)
		}
	}
	wrong := fields.Request
	wrong.Launch.AgentID = requestID[i.Agent](t, 207)
	if _, err = (meeting.ContextBuilder{}).BuildTriggerContext(context.Background(), wrong, fields.Trigger); err == nil {
		t.Fatal("foreign target agent accepted")
	}
}
