package meeting

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func TestMeetingReplyPreservesExistingSendDigest(t *testing.T) {
	actor, _ := i.NewHuman(summaryTestID[i.User](t), summaryTestID[i.Session](t))
	r := c.SendRequest{ProjectID: summaryTestID[i.Project](t), MeetingID: summaryTestID[c.Meeting](t), ExpectedVersion: 4, Mode: "sequential", Content: []c.InlineNode{{Type: "text", Text: "Continue the discussion."}}, Meta: f.CommandMeta{RequestID: summaryTestID[f.Request](t), IdempotencyKey: "original-send"}}
	// This is the original persisted command shape. Adding an absent ReplyTo
	// must not strand old receipt lookups behind a different semantic digest.
	legacy := struct {
		ProjectID       i.ProjectID    `json:"project_id"`
		MeetingID       c.MeetingID    `json:"meeting_id"`
		ExpectedVersion f.Version      `json:"expected_version"`
		Mode            string         `json:"mode"`
		Content         []c.InlineNode `json:"content_nodes"`
		Interrupt       bool           `json:"interrupt"`
		Meta            f.CommandMeta  `json:"-"`
	}{r.ProjectID, r.MeetingID, r.ExpectedVersion, r.Mode, r.Content, r.Interrupt, r.Meta}
	want, err := commandDigest(actor, "send", legacy)
	if err != nil {
		t.Fatal(err)
	}
	got, err := commandDigest(actor, "send", r)
	if err != nil || got != want || r.Validate() != nil {
		t.Fatal("optional reply changed an existing Send identity", err)
	}
	first := summaryTestID[c.Message](t)
	r.ReplyTo = &first
	replyDigest, err := commandDigest(actor, "send", r)
	if err != nil || replyDigest == want || r.Validate() != nil {
		t.Fatal("reply target was not part of the original command", err)
	}
	second := summaryTestID[c.Message](t)
	r.ReplyTo = &second
	changed, err := commandDigest(actor, "send", r)
	if err != nil || changed == replyDigest {
		t.Fatal("different reply targets reused a command digest", err)
	}
	zero := c.MessageID{}
	r.ReplyTo = &zero
	if r.Validate() == nil {
		t.Fatal("invalid reply identity accepted")
	}
	agent, _ := i.NewAgentRun(r.ProjectID, summaryTestID[i.Agent](t), summaryTestID[i.Execution](t))
	r.ReplyTo = &first
	if _, err := (&Service{}).Send(context.Background(), agent, r); err == nil {
		t.Fatal("Agent acquired the Human reply entry point")
	}
}

type replyTestStore struct {
	*controlEventStore
	meeting c.MeetingID
	message c.MessageID
	reads   int
}

func (s *replyTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.controlEventStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *replyTestStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	return controlEventRow(func(dest ...any) error {
		if query != `SELECT EXISTS(SELECT 1 FROM agenteam_meeting.messages WHERE meeting_id=$1 AND id=$2)` || len(args) != 2 {
			s.t.Fatal("reply read crossed its immutable Meeting identity boundary")
		}
		if _, err := s.InTx(s.tx); err != nil {
			return err
		}
		s.reads++
		*dest[0].(*bool) = args[0] == s.meeting.String() && args[1] == s.message.String()
		return ctx.Err()
	})
}

func TestMeetingReplyRequiresSameMeetingAndOriginalLockedTransaction(t *testing.T) {
	for _, shape := range []string{"immutable-message", "absent", "foreign-meeting", "foreign-message", "missing-lock", "foreign-transaction"} {
		t.Run(shape, func(t *testing.T) {
			s := &replyTestStore{controlEventStore: &controlEventStore{t: t}, meeting: summaryTestID[c.Meeting](t), message: summaryTestID[c.Message](t)}
			service := &Service{store: s}
			meeting, message := s.meeting, &s.message
			if shape == "absent" {
				message = nil
			}
			if shape == "foreign-meeting" {
				meeting = summaryTestID[c.Meeting](t)
			}
			if shape == "foreign-message" {
				id := summaryTestID[c.Message](t)
				message = &id
			}
			cause, _ := readCause("meeting.reply-test")
			result := s.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if shape != "missing-lock" {
					if err := s.AcquireAll(ctx, tx, []f.LockRequest{meetingLock(meeting, f.Exclusive)}); err != nil {
						return err
					}
				}
				if shape == "foreign-transaction" {
					tx = f.NewTx()
				}
				return service.requireReplyInTx(ctx, tx, meeting, message)
			})
			valid := shape == "immutable-message" || shape == "absent"
			if (commitError(result) == nil) != valid || s.writes != 0 || s.active {
				t.Fatal("reply lookup lost original scope/transaction", commitError(result))
			}
			if (shape == "absent" || shape == "missing-lock" || shape == "foreign-transaction") && s.reads != 0 {
				t.Fatal("invalid or absent reply performed a read")
			}
		})
	}
}
