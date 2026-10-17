package meeting

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func meetingAttachmentFixture(t *testing.T) (i.ProjectID, FrozenMessage) {
	t.Helper()
	source := ec.CapturedArtifactAttachment{
		ProjectID: summaryTestID[i.Project](t), ArtifactID: summaryTestID[ac.ArtifactEntity](t), FileID: summaryTestID[ac.File](t), ArtifactVersion: 1,
		ObjectID: summaryTestID[oc.StoredObject](t), ObjectVersion: 1, SHA256: f.Digest("sha256:" + strings.Repeat("a", 64)), MediaType: "application/pdf", ByteSize: 8, Filename: "review.pdf",
	}
	nodes := []c.InlineNode{{Type: "text", Text: "Review the attachment"}, {Type: "file", Reference: &c.Reference{Kind: "file", ID: source.ArtifactID.String(), Label: "review.pdf"}}}
	raw, err := canonical(nodes)
	if err != nil {
		t.Fatal(err)
	}
	return source.ProjectID, FrozenMessage{ContributionID: summaryTestID[c.Contribution](t), Generation: 1, Attempt: 1, ContentDigest: digest(raw), Message: c.MessageView{ID: summaryTestID[c.Message](t), ParticipantID: summaryTestID[c.Participant](t), Content: nodes, CreatedAt: time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)}, Attachments: []FrozenAttachment{{NodeIndex: 1, Source: source}}}
}

func TestMeetingAttachmentSummaryPreservesCanonicalSource(t *testing.T) {
	project, message := meetingAttachmentFixture(t)
	input := SummaryInput{ProjectID: project, Messages: []FrozenMessage{message, message}}
	messages, _, err := summaryMessages(input)
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, part := range messages[1].Parts {
		if part.File != nil {
			files++
			if !message.Attachments[0].Source.MatchesReference(part.File.Ref) {
				t.Fatal("summary substituted a different attachment")
			}
		}
		if part.Text != nil && strings.Contains(part.Text.Text, "object_id") {
			t.Fatal("summary exposed source control metadata as conversation")
		}
	}
	if files != 1 {
		t.Fatal("repeated reference must materialize once", files)
	}
	raw, err := mc.EncodeCanonicalMessages(messages)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := mc.DecodeCanonicalMessages(raw)
	if err != nil || !message.Attachments[0].Source.MatchesReference(decoded[1].Parts[2].File.Ref) {
		t.Fatal("canonical retry lost Artifact identity", err)
	}
	frozen, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var restored FrozenMessage
	if json.Unmarshal(frozen, &restored) != nil || restored.Attachments[0] != message.Attachments[0] {
		t.Fatal("frozen attachment metadata did not survive durable encoding")
	}
}

func TestMeetingAttachmentRejectsMissingOrSubstitutedPin(t *testing.T) {
	project, message := meetingAttachmentFixture(t)
	if validateFrozenAttachments(project, message) != nil {
		t.Fatal("original binding rejected")
	}
	for _, change := range []func(*FrozenMessage){
		func(v *FrozenMessage) { v.Attachments = nil },
		func(v *FrozenMessage) { v.Attachments[0].NodeIndex = 0 },
		func(v *FrozenMessage) { v.Attachments[0].Source.ArtifactID = summaryTestID[ac.ArtifactEntity](t) },
		func(v *FrozenMessage) { v.Attachments[0].Source.ProjectID = summaryTestID[i.Project](t) },
	} {
		changed := message
		changed.Attachments = append([]FrozenAttachment(nil), message.Attachments...)
		change(&changed)
		if validateFrozenAttachments(project, changed) == nil {
			t.Fatal("substituted message/source binding accepted")
		}
	}
}

func TestMeetingAttachmentLimitCountsRepeatedFileNodes(t *testing.T) {
	_, message := meetingAttachmentFixture(t)
	file := message.Message.Content[1]
	nodes := make([]c.InlineNode, ec.MaxInputAttachments)
	for n := range nodes {
		nodes[n] = file
	}
	ids, err := meetingAttachmentIDs(nodes)
	if err != nil || len(ids) != 1 {
		t.Fatal("four repeated nodes should capture one source", err)
	}
	_, err = meetingAttachmentIDs(append(nodes, file))
	var failure *f.Fault
	if !errors.As(err, &failure) || failure.Code != f.PayloadTooLarge {
		t.Fatal("fifth repeated file node must fail before persistence", err)
	}
}
