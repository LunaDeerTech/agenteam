package meeting

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/realtime/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type realtimeTestStore struct {
	*controlEventStore
	actor                 i.Actor
	meeting               c.MeetingID
	contribution          c.ContributionID
	execution             i.ExecutionID
	intent                c.IntentID
	input                 c.InputID
	request               ec.LaunchRequest
	deleting, wrongSource bool
	reads                 int
	transactions          int
	beforeTransaction     func(int)
}

func (s *realtimeTestStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.transactions++
	if s.beforeTransaction != nil {
		s.beforeTransaction(s.transactions)
	}
	return s.controlEventStore.WithinTx(ctx, cause, fn)
}

func (s *realtimeTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.controlEventStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *realtimeTestStore) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	return controlEventRow(func(dest ...any) error {
		lockedMeeting := s.meeting
		if strings.HasPrefix(sql, "SELECT status,version,title,") {
			lockedMeeting, _ = f.ParseID[c.Meeting](args[1].(string))
		}
		if err := s.RequireHeldLocks(ctx, s.tx, []f.LockRequest{userLock(s.actor), projectLock(s.request.ProjectID, f.Shared), meetingLock(lockedMeeting, f.Shared)}); err != nil {
			s.t.Fatal("realtime source read escaped the original transaction/lock union", err)
		}
		s.reads++
		raw, err := json.Marshal(s.request)
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(sql, "SELECT status,version,title,"):
			if args[0] != s.request.ProjectID.String() || args[1] != s.meeting.String() {
				return pgx.ErrNoRows
			}
			if err := s.controlEventStore.QueryRow(ctx, sql, args...).Scan(dest...); err != nil {
				return err
			}
			*dest[5].(*bool) = s.deleting
		case strings.HasPrefix(sql, "SELECT c.generation,c.attempt,c.current_execution_id"):
			if args[0] != s.contribution.String() || args[1] != s.meeting.String() || args[2] != s.request.ProjectID.String() {
				return pgx.ErrNoRows
			}
			*dest[0].(*int64), *dest[1].(*int64) = int64(*s.request.Lineage.ContributionGeneration), int64(*s.request.Lineage.ContributionAttempt)
			execution := s.execution.String()
			*dest[2].(**string), *dest[3].(*string), *dest[4].(*string), *dest[5].(*string) = &execution, s.request.Trigger.TurnID, s.request.Trigger.ParticipantID, s.request.AgentID.String()
			if s.wrongSource {
				*dest[5].(*string) = summaryTestID[i.Agent](s.t).String()
			}
		case strings.HasPrefix(sql, "SELECT request_data,request_id,idempotency_key"):
			*dest[0].(*[]byte), *dest[1].(*string), *dest[2].(*string) = raw, s.request.Meta.RequestID.String(), s.request.Meta.IdempotencyKey.String()
		case strings.HasPrefix(sql, "SELECT l.id,l.input_id,l.request_id"):
			d, err := s.request.Digest()
			if err != nil {
				return err
			}
			execution, input := s.execution.String(), []byte("{}")
			*dest[0].(*string), *dest[1].(*string), *dest[2].(*string), *dest[3].(*string) = s.intent.String(), s.input.String(), s.request.Meta.RequestID.String(), s.request.Meta.IdempotencyKey.String()
			*dest[4].(*string), *dest[5].(*[]byte), *dest[6].(**string), *dest[7].(*bool), *dest[8].(*[]byte), *dest[9].(*string) = d.String(), raw, &execution, false, input, digest(input).String()
		default:
			s.t.Fatalf("subscription read another domain or invented a SQL read: %s", sql)
		}
		return nil
	})
}

func newRealtimeTest(t *testing.T) (*RealtimeAuthority, *realtimeTestStore, *controlEventProjects) {
	t.Helper()
	user := summaryTestID[i.User](t)
	actor, err := i.NewHuman(user, summaryTestID[i.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	at, _ := f.NewInstant(time.Unix(1, 0).UTC())
	project := pc.ProjectRef{ID: summaryTestID[i.Project](t), OwnerUserID: user, Name: "Meeting", NormalizedName: "meeting", Lifecycle: pc.Active, Version: 1, CreatedAt: at, UpdatedAt: at}
	s := &realtimeTestStore{controlEventStore: &controlEventStore{t: t, facts: controlEventFacts{status: "active", version: 1}}, actor: actor, meeting: summaryTestID[c.Meeting](t), contribution: summaryTestID[c.Contribution](t), execution: summaryTestID[i.Execution](t), intent: summaryTestID[c.Intent](t), input: summaryTestID[c.Input](t)}
	generation, attempt := f.Version(2), f.Version(3)
	s.request = ec.LaunchRequest{ProjectID: project.ID, AgentID: summaryTestID[i.Agent](t), Purpose: "meeting/response", Trigger: ec.Trigger{Kind: "meeting", MeetingID: s.meeting.String(), TurnID: summaryTestID[c.Turn](t).String(), ContributionID: s.contribution.String(), ParticipantID: summaryTestID[c.Participant](t).String()}, Policy: ec.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{}}, Lineage: ec.Lineage{ContributionGeneration: &generation, ContributionAttempt: &attempt}, Meta: f.CommandMeta{RequestID: summaryTestID[f.Request](t), IdempotencyKey: "realtime-original-launch"}}
	if err := s.request.Validate(); err != nil {
		t.Fatal(err)
	}
	projects := &controlEventProjects{store: s.controlEventStore, actor: actor, project: project}
	authority, err := NewRealtimeAuthority(&Service{store: s, deps: Dependencies{Projects: projects}})
	if err != nil {
		t.Fatal(err)
	}
	return authority, s, projects
}

func TestMeetingRealtimeReadsCurrentOwnerAndExactAssociation(t *testing.T) {
	for _, change := range []string{"current", "archived", "wrong-execution", "old-generation", "old-attempt", "foreign-contribution", "foreign-meeting", "foreign-project", "foreign-actor", "revoked-owner", "deleting", "foreign-launch-source", "cancelled-context"} {
		t.Run(change, func(t *testing.T) {
			a, s, projects := newRealtimeTest(t)
			ctx := context.Background()
			actor, project, meeting, contribution, execution := s.actor, s.request.ProjectID, s.meeting, s.contribution, s.execution
			generation, attempt := *s.request.Lineage.ContributionGeneration, *s.request.Lineage.ContributionAttempt
			if err := a.RequireRuntimeSubscription(ctx, actor, project, meeting, contribution, generation, attempt, execution); err != nil {
				t.Fatal("current tuple was rejected", err)
			}
			switch change {
			case "archived":
				s.facts.status = "archive"
			case "wrong-execution":
				execution = summaryTestID[i.Execution](t)
			case "old-generation":
				generation--
			case "old-attempt":
				attempt--
			case "foreign-contribution":
				contribution = summaryTestID[c.Contribution](t)
			case "foreign-meeting":
				meeting = summaryTestID[c.Meeting](t)
			case "foreign-project":
				project = summaryTestID[i.Project](t)
			case "foreign-actor":
				actor, _ = i.NewHuman(summaryTestID[i.User](t), summaryTestID[i.Session](t))
			case "revoked-owner":
				projects.actor, _ = i.NewHuman(summaryTestID[i.User](t), summaryTestID[i.Session](t))
			case "deleting":
				s.deleting = true
			case "foreign-launch-source":
				s.wrongSource = true
			case "cancelled-context":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			err := a.RequireRuntimeSubscription(ctx, actor, project, meeting, contribution, generation, attempt, execution)
			valid := change == "current" || change == "archived"
			if (err == nil) != valid {
				t.Fatal("subscription did not re-read current ownership and exact original association", err)
			}
			if s.writes != 0 || s.active {
				t.Fatal("read authority mutated facts or retained its transaction")
			}
		})
	}
}

func TestMeetingRealtimeTimelineDoesNotRequireOrExposeExecution(t *testing.T) {
	a, s, _ := newRealtimeTest(t)
	for _, status := range []string{"proposed", "active", "archive"} {
		s.facts.status = status
		before := s.reads
		if err := a.RequireTimelineSubscription(context.Background(), s.actor, s.request.ProjectID, s.meeting); err != nil || s.reads-before != 1 {
			t.Fatal("timeline borrowed an Execution or rejected a readable session", err)
		}
	}
	s.deleting = true
	if err := a.RequireTimelineSubscription(context.Background(), s.actor, s.request.ProjectID, s.meeting); err == nil {
		t.Fatal("deleting session retained its subscription grant")
	}
	if _, err := NewRealtimeAuthority(nil); err == nil {
		t.Fatal("unbound service supplied realtime authority")
	}
}

func TestMeetingRealtimeUsesOnlyPlatformTopics(t *testing.T) {
	a, s, _ := newRealtimeTest(t)
	topic := rc.Topic{Kind: rc.MeetingExecution, ProjectID: s.request.ProjectID, MeetingID: s.meeting.String(), ContributionID: s.contribution.String(), Generation: *s.request.Lineage.ContributionGeneration, Attempt: *s.request.Lineage.ContributionAttempt, ExecutionID: s.execution}
	if err := a.RequireSubscription(context.Background(), s.actor, topic); err != nil {
		t.Fatal("platform topic did not reach the original read gate", err)
	}
	for _, change := range []func(*rc.Topic){
		func(v *rc.Topic) { v.Kind = "task.execution" },
		func(v *rc.Topic) { v.Kind = rc.MeetingTimeline },
		func(v *rc.Topic) { v.ContributionID = "" },
		func(v *rc.Topic) { v.Generation = 0 },
		func(v *rc.Topic) { v.Attempt = 0 },
		func(v *rc.Topic) { v.ExecutionID = i.ExecutionID{} },
	} {
		other := topic
		change(&other)
		before := s.reads
		if err := a.RequireSubscription(context.Background(), s.actor, other); err == nil || s.reads != before {
			t.Fatal("invalid topic escaped the shared shape gate", err)
		}
	}
}

type realtimeProjectionReader struct {
	store     *realtimeTestStore
	authority *Authority
	value     ec.RuntimeProjection
	reads     int
	borrowed  context.Context
}

func (r *realtimeProjectionReader) ReadMeetingRuntimeProjectionInTx(ctx context.Context, tx f.Tx, project i.ProjectID, association ec.AssociatedMeeting) (ec.RuntimeProjection, error) {
	r.reads++
	if err := r.authority.RequireBindingInTx(ctx, tx, r.store); err != nil {
		return ec.RuntimeProjection{}, err
	}
	if err := r.authority.RequireMeetingResultInTx(ctx, tx, project, association); err != nil {
		return ec.RuntimeProjection{}, err
	}
	locks, err := ec.MeetingExecutionReadLocks(project, association)
	if err != nil {
		return ec.RuntimeProjection{}, err
	}
	if err := r.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return ec.RuntimeProjection{}, err
	}
	r.borrowed = ctx
	return r.value, nil
}

func TestMeetingRealtimeRuntimeSnapshotUsesOriginalTransactionAndCurrentTuple(t *testing.T) {
	for _, change := range []string{"running", "revoked-between-reads", "deleting-between-reads", "replaced-intent", "replaced-execution", "new-generation", "foreign-projection", "invalid-projection"} {
		t.Run(change, func(t *testing.T) {
			a, s, projects := newRealtimeTest(t)
			authority, err := NewAuthority(s)
			if err != nil {
				t.Fatal(err)
			}
			reader := &realtimeProjectionReader{store: s, authority: authority, value: ec.RuntimeProjection{Schema: ec.PublicRuntimeSchema, ExecutionID: s.execution, ProjectID: s.request.ProjectID, ExecutionVersion: 3, Status: ec.Running, Items: []ec.RuntimeProjectionItem{{ID: "state", Seq: 1, Revision: 3, Type: "state", State: &ec.RuntimeStateData{Status: ec.Running}}}}}
			a.original.deps.DecisionAuthority, a.original.deps.RuntimeProjections = authority, reader
			topic := rc.Topic{Kind: rc.MeetingExecution, ProjectID: s.request.ProjectID, MeetingID: s.meeting.String(), ContributionID: s.contribution.String(), Generation: *s.request.Lineage.ContributionGeneration, Attempt: *s.request.Lineage.ContributionAttempt, ExecutionID: s.execution}
			s.beforeTransaction = func(number int) {
				if number != 2 {
					return
				}
				switch change {
				case "revoked-between-reads":
					projects.actor, _ = i.NewHuman(summaryTestID[i.User](t), summaryTestID[i.Session](t))
				case "deleting-between-reads":
					s.deleting = true
				case "replaced-intent":
					s.intent = summaryTestID[c.Intent](t)
				case "replaced-execution":
					s.execution = summaryTestID[i.Execution](t)
				case "new-generation":
					next := topic.Generation + 1
					s.request.Lineage.ContributionGeneration = &next
				case "foreign-projection":
					reader.value.ExecutionID = summaryTestID[i.Execution](t)
				case "invalid-projection":
					reader.value.Items = nil
				}
			}
			out, err := a.ReadSnapshot(context.Background(), s.actor, topic)
			if (err == nil) != (change == "running") {
				t.Fatal("snapshot reused a stale subscription or accepted a foreign projection", err)
			}
			if change == "running" {
				if err := out.Validate(); err != nil || out.Topic != topic || out.Schema != "meeting_execution_runtime_v1" {
					t.Fatal("snapshot did not use the canonical platform envelope", err)
				}
				var got ec.RuntimeProjection
				if json.Unmarshal(out.Content, &got) != nil || got.Validate() != nil || got.Status != ec.Running || got.TranscriptThrough != 0 || len(got.Items) != 1 {
					t.Fatal("active committed state became a terminal-only snapshot or token progress")
				}
			}
			wantReads := 0
			if change == "running" || change == "foreign-projection" || change == "invalid-projection" {
				wantReads = 1
			}
			if reader.reads != wantReads || s.active || s.writes != 0 {
				t.Fatal("failed current gate reached Execution or retained its transaction", reader.reads)
			}
			if reader.borrowed != nil && authority.RequireBindingInTx(reader.borrowed, s.tx, s) == nil {
				t.Fatal("snapshot retained the private result witness after the read")
			}
		})
	}
}

type realtimeMutationTestStore struct {
	*controlEventStore
	noRows bool
}

func (s *realtimeMutationTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.controlEventStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *realtimeMutationTestStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if s.noRows {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	return s.controlEventStore.Exec(ctx, query, args...)
}

type realtimeMutationProjects struct {
	store   *realtimeMutationTestStore
	project i.ProjectID
	meeting c.MeetingID
}

func (p realtimeMutationProjects) RequirePreparingProjectInTx(ctx context.Context, tx f.Tx, project i.ProjectID) (pc.ProjectRef, error) {
	if project != p.project {
		return pc.ProjectRef{}, fault(f.Forbidden)
	}
	if err := p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Shared), meetingLock(p.meeting, f.Exclusive)}); err != nil {
		return pc.ProjectRef{}, err
	}
	return pc.ProjectRef{ID: project}, nil
}

type realtimeTestPublisher struct {
	t       *testing.T
	store   *controlEventStore
	project i.ProjectID
	meeting c.MeetingID
	count   int
}

func (p *realtimeTestPublisher) InvalidateMeeting(project i.ProjectID, meeting string) {
	if p.store.active || project != p.project || meeting != p.meeting.String() {
		p.t.Fatal("notification escaped its original project/meeting or preceded commit")
	}
	p.count++
}
func (p *realtimeTestPublisher) InvalidateExecution(i.ProjectID, i.ExecutionID) {
	p.t.Fatal("Meeting producer invented an Execution update")
}

func TestMeetingRealtimeRuntimePublishesOnlyKnownAffectedCommit(t *testing.T) {
	for _, outcome := range []string{"idle", "zero-rows", "committed", "rejected", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			s := &realtimeMutationTestStore{controlEventStore: &controlEventStore{t: t, unknown: outcome == "unknown", facts: controlEventFacts{status: "active", version: 1}}, noRows: outcome == "zero-rows"}
			project, meeting := summaryTestID[i.Project](t), summaryTestID[c.Meeting](t)
			publisher := &realtimeTestPublisher{t: t, store: s.controlEventStore, project: project, meeting: meeting}
			runtime := &Runtime{store: s, deps: RuntimeDependencies{Projects: realtimeMutationProjects{s, project, meeting}, Realtime: publisher}}
			err := runtime.transaction(context.Background(), project, meeting, "notification-test", func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
				if outcome == "idle" {
					return nil
				}
				if _, err := x.Exec(ctx, "UPDATE agenteam_meeting.sessions SET version=version+1 WHERE id=$1", meeting.String()); err != nil {
					return err
				}
				if outcome == "rejected" {
					return fault(f.ConfirmationStale)
				}
				return nil
			})
			want := 0
			if outcome == "committed" {
				want = 1
			}
			if publisher.count != want || (err != nil) != (outcome == "rejected" || outcome == "unknown") || s.active {
				t.Fatal("idle, rollback or Unknown became a committed notification", publisher.count, err)
			}
		})
	}
}

type realtimeDecisionTools struct {
	ec.MeetingDecisionToolAuthority
	store *controlEventStore
}

func (p realtimeDecisionTools) RequireMeetingDecisionToolBindingInTx(_ context.Context, tx f.Tx, store any) error {
	if store != p.store {
		return fault(f.Forbidden)
	}
	_, err := p.store.InTx(tx)
	return err
}

func TestMeetingRealtimeDecisionResumePublishesAfterKnownCommit(t *testing.T) {
	for _, phase := range []string{"pending", "resolved", "unknown", "failed-resume"} {
		t.Run(phase, func(t *testing.T) {
			store := &controlEventStore{t: t, unknown: phase == "unknown"}
			project, meeting := summaryTestID[i.Project](t), summaryTestID[c.Meeting](t)
			publisher := &realtimeTestPublisher{t: t, store: store, project: project, meeting: meeting}
			service := &DecisionService{store: store, tools: realtimeDecisionTools{store: store}, realtime: publisher}
			var call decisionCall
			call.call.Call.ProjectID, call.call.Call.ExecutionID = project, summaryTestID[i.Execution](t)
			call.call.OperationID = summaryTestID[tc.Operation](t)
			call.intent.request.Trigger.MeetingID = meeting.String()
			err := service.transaction(context.Background(), call, "wait", func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, w *decisionWitness) error {
				if phase == "pending" {
					return nil
				}
				w.phase = "resume"
				if phase == "failed-resume" {
					return fault(f.ConfirmationStale)
				}
				// Simulate the existing same-Tx Tool provider's acknowledged
				// transition. The original CommitResult remains decisive.
				_, err := x.Exec(ctx, "UPDATE agenteam_meeting.sessions SET version=version+1 WHERE id=$1", meeting.String())
				return err
			})
			want := 0
			if phase == "resolved" {
				want = 1
			}
			if publisher.count != want || (err != nil) != (phase == "unknown" || phase == "failed-resume") || store.active {
				t.Fatal("Decision idle/Unknown resume notified or committed resume was omitted", publisher.count, err)
			}
		})
	}
}
