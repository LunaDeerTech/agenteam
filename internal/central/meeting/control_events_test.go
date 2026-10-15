package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type controlEventFacts struct {
	status, actor, digest, key string
	version                    int64
	receipt                    []byte
	event                      []any
}
type controlEventStore struct {
	Store
	t       *testing.T
	tx      f.Tx
	active  bool
	unknown bool
	facts   controlEventFacts
	locks   []f.LockRequest
	writes  int
}

func (s *controlEventStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	before := s.facts
	s.tx, s.active, s.locks = f.NewTx(), true, nil
	defer func() { s.active = false }()
	if err := fn(ctx, s.tx); err != nil {
		s.facts = before
		var failure *f.Fault
		if errors.As(err, &failure) {
			return f.NotCommittedResult(failure)
		}
		return f.NotCommittedResult(fault(f.InternalError).WithCause(err))
	}
	if s.unknown && s.writes > 0 {
		return f.UnknownResult(summaryTestID[f.TransactionAttempt](s.t), cause)
	}
	return f.CommittedResult()
}
func (s *controlEventStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.active || tx != s.tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *controlEventStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.locks = locks
	return nil
}
func (s *controlEventStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	for _, needed := range locks {
		found := false
		for _, held := range s.locks {
			found = found || held.Key.Canonical() == needed.Key.Canonical() && (held.Mode == f.Exclusive || held.Mode == needed.Mode)
		}
		if !found {
			return fault(f.Forbidden)
		}
	}
	return nil
}

type controlEventRow func(...any) error

func (r controlEventRow) Scan(dest ...any) error { return r(dest...) }
func (s *controlEventStore) QueryRow(_ context.Context, sql string, args ...any) postgres.Row {
	return controlEventRow(func(dest ...any) error {
		if !s.active {
			s.t.Fatal("control performed SQL outside the original transaction")
		}
		switch {
		case strings.HasPrefix(sql, "SELECT actor_id,semantic_digest,receipt"):
			if s.facts.receipt == nil || s.facts.key != args[1] {
				return pgx.ErrNoRows
			}
			*dest[0].(*string), *dest[1].(*string), *dest[2].(*[]byte) = s.facts.actor, s.facts.digest, s.facts.receipt
		case strings.HasPrefix(sql, "SELECT status,version,title,"):
			*dest[0].(*string), *dest[1].(*int64) = s.facts.status, s.facts.version
			*dest[2].(**string), *dest[3].(**string), *dest[4].(*[]byte), *dest[5].(*bool) = nil, nil, nil, false
			*dest[6].(*time.Time), *dest[7].(*time.Time) = time.Unix(1, 0).UTC(), time.Unix(1, 0).UTC()
		case strings.HasPrefix(sql, "SELECT EXISTS(SELECT 1 FROM agenteam_meeting.lifecycle_events"):
			*dest[0].(*bool) = reflect.DeepEqual(s.facts.event, args)
		default:
			s.t.Fatalf("unexpected control read: %s", sql)
		}
		return nil
	})
}
func (s *controlEventStore) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !s.active {
		s.t.Fatal("control wrote outside the original transaction")
	}
	s.writes++
	switch {
	case strings.HasPrefix(sql, "UPDATE agenteam_meeting.sessions SET status="):
		s.facts.status = args[1].(string)
	case strings.HasPrefix(sql, "UPDATE agenteam_meeting.sessions SET version="):
		s.facts.version++
	case strings.HasPrefix(sql, "INSERT INTO agenteam_meeting.commands"):
		s.facts.key, s.facts.actor, s.facts.digest = args[1].(string), args[2].(string), args[4].(string)
		s.facts.receipt = append([]byte(nil), args[6].([]byte)...)
	case strings.HasPrefix(sql, "INSERT INTO agenteam_meeting.lifecycle_events"):
		s.facts.event = append([]any(nil), args...)
	default:
		s.t.Fatalf("archive/restore changed a Turn, Contribution or external operation: %s", sql)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type controlEventProjects struct {
	pc.ProjectAuthority
	store   *controlEventStore
	actor   i.Actor
	project pc.ProjectRef
}

func (a *controlEventProjects) RequireOwnerInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, _ i.AccessIntent) (pc.ProjectAccess, error) {
	if !actor.Equal(a.actor) || project != a.project.ID {
		return pc.ProjectAccess{}, fault(f.Forbidden)
	}
	if err := a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(actor), projectLock(project, f.Shared)}); err != nil {
		return pc.ProjectAccess{}, err
	}
	return pc.NewProjectAccess(actor, a.project, a.project.CreatedAt)
}

type controlEventJournal struct {
	store             *controlEventStore
	producer          *EventAuthority
	issuer            oc.PlanIssuer
	prepares, appends int
	fail              bool
	last              event.Event
}

func (j *controlEventJournal) PrepareAppend(ctx context.Context, actor i.Actor, ev event.Event) (oc.AppendPlan, error) {
	j.prepares++
	producer, err := j.producer.DiscoverAppend(ctx, actor, ev.Summary())
	if err != nil {
		return oc.AppendPlan{}, err
	}
	binding, err := oc.SemanticDigest(actor, ev)
	if err != nil {
		return oc.AppendPlan{}, err
	}
	project, _ := f.ParseID[i.Project](ev.Header().Scope.ProjectID.String())
	extra := []f.LockRequest{projectLock(project, f.Exclusive)}
	access, err := oc.NewDependencies(j.issuer, binding, extra, nil)
	if err != nil {
		return oc.AppendPlan{}, err
	}
	return oc.NewAppendPlan(j.issuer, oc.AppendPlanDetails{Event: ev, Semantic: binding, Producer: producer, Project: access, Locks: append(producer.Locks(), extra...)})
}
func (j *controlEventJournal) AppendEventInTx(ctx context.Context, tx f.Tx, actor i.Actor, ev event.Event, plan oc.AppendPlan) (oc.AppendReceipt, error) {
	j.appends++
	if !plan.Matches(j.issuer, actor, ev) || j.store.facts.receipt == nil {
		j.store.t.Fatal("lifecycle event did not share its original control receipt")
	}
	if err := j.store.RequireHeldLocks(ctx, tx, plan.Locks()); err != nil {
		return oc.AppendReceipt{}, err
	}
	if err := j.producer.ValidateAppendInTx(ctx, tx, actor, ev.Summary(), plan.Details().Producer, oc.NewFact); err != nil {
		return oc.AppendReceipt{}, err
	}
	j.last = ev
	if j.fail {
		return oc.AppendReceipt{}, fault(f.DependencyUnavailable)
	}
	return oc.AppendReceipt{EventID: ev.Header().EventID, Sequence: 1}, nil
}

func TestControlLifecycleEventCommitsWithOriginalReceipt(t *testing.T) {
	for _, action := range []string{"archive", "unarchive"} {
		for _, outcome := range []string{"committed", "event-rejected", "commit-unknown"} {
			t.Run(action+"/"+outcome, func(t *testing.T) {
				from, to, name := "active", "archive", c.ArchivedName
				if action == "unarchive" {
					from, to, name = "archive", "active", c.RestoredName
				}
				store := &controlEventStore{t: t, facts: controlEventFacts{status: from, version: 7}}
				user, session := summaryTestID[i.User](t), summaryTestID[i.Session](t)
				actor, err := i.NewHuman(user, session)
				if err != nil {
					t.Fatal(err)
				}
				at, _ := f.NewInstant(time.Unix(1, 0).UTC())
				project := pc.ProjectRef{ID: summaryTestID[i.Project](t), OwnerUserID: user, Name: "Meeting", NormalizedName: "meeting", Lifecycle: pc.Active, Version: 1, CreatedAt: at, UpdatedAt: at}
				projects := &controlEventProjects{store: store, actor: actor, project: project}
				authority, err := NewEventAuthority(store, projects)
				if err != nil {
					t.Fatal(err)
				}
				types, err := c.RegisterEvents(event.NewCatalog())
				if err != nil {
					t.Fatal(err)
				}
				journal := &controlEventJournal{store: store, producer: authority, issuer: oc.NewPlanIssuer(), fail: outcome == "event-rejected"}
				events, err := NewProposalEvents(authority, types, journal)
				if err != nil {
					t.Fatal(err)
				}
				service := &Service{store: store, deps: Dependencies{Projects: projects, LifecycleEvents: events}}
				request := c.ControlRequest{ProjectID: project.ID, MeetingID: summaryTestID[c.Meeting](t), Action: action, ExpectedVersion: 7, Meta: f.CommandMeta{RequestID: summaryTestID[f.Request](t), IdempotencyKey: f.IdempotencyKey(summaryTestID[struct{}](t).String())}}
				store.unknown = outcome == "commit-unknown"
				receipt, err := service.Control(context.Background(), actor, request)
				var failure *f.Fault
				switch outcome {
				case "event-rejected":
					if !errors.As(err, &failure) || failure.Code != f.DependencyUnavailable || store.facts.status != from || store.facts.version != 7 || store.facts.receipt != nil || store.facts.event != nil {
						t.Fatal("event rejection did not roll back the whole control fact", err)
					}
					return
				case "commit-unknown":
					if !errors.As(err, &failure) || failure.Code != f.CommitUnknown {
						t.Fatal("uncertain original commit became success", err)
					}
				default:
					if err != nil || receipt.Version != 8 || receipt.Replayed {
						t.Fatal("control did not return its original receipt", err)
					}
				}
				payload, err := types.Decode(journal.last)
				if err != nil || payload.MeetingID != request.MeetingID || payload.Status != to || journal.last.Header().EventType != name || *journal.last.Header().AggregateVersion != 8 || store.facts.status != to || store.facts.version != 8 || journal.prepares != 1 || journal.appends != 1 {
					t.Fatal("canonical lifecycle event lost the original control transition", err)
				}
				store.unknown = false
				// Receipt lookup/replay requires no new event dependency, including
				// after an original COMMIT response was uncertain.
				service.deps.LifecycleEvents = nil
				replayed, found, err := service.LookupControl(context.Background(), actor, request)
				if err != nil || !found || !replayed.Replayed || replayed.Version != 8 {
					t.Fatal("original lifecycle command lookup failed", err)
				}
				_, err = service.Control(context.Background(), actor, request)
				if err != nil || journal.prepares != 1 || journal.appends != 1 || store.writes != 4 {
					t.Fatal("original replay emitted or mutated another lifecycle fact", err)
				}
				var saved c.Receipt
				if json.Unmarshal(store.facts.receipt, &saved) != nil || saved.MeetingID != request.MeetingID || saved.Version != 8 {
					t.Fatal("stored lifecycle receipt differs from the committed event")
				}
			})
		}
	}
}
