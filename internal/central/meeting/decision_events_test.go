package meeting

import (
	"context"
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
	"github.com/jackc/pgx/v5/pgconn"
)

type decisionEventTestStore struct {
	Store
	t               *testing.T
	tx              f.Tx
	active, unknown bool
	locks           []f.LockRequest
	post            []any
	fact            []any
	delivered       bool
}

type decisionScopeQuery struct {
	postgres.SQLExecutor
	args  []any
	calls int
}

func (q *decisionScopeQuery) Query(_ context.Context, _ string, args ...any) (*postgres.Rows, error) {
	q.args = append([]any(nil), args...)
	q.calls++
	// Stop at the real SQL boundary: this test checks the production binding,
	// not a substitute implementation of PostgreSQL's filtering semantics.
	return nil, errors.New("decision scope query boundary")
}

func TestDecisionCancellationScopeBindsOnlySpecifiedIdentities(t *testing.T) {
	p, m := summaryTestID[i.Project](t), summaryTestID[c.Meeting](t)
	turn, contribution := summaryTestID[c.Turn](t), summaryTestID[c.Contribution](t)
	execution, decision := summaryTestID[i.Execution](t), summaryTestID[c.Decision](t)
	for _, tc := range []struct {
		name  string
		scope decisionEventScope
		want  []any
	}{
		{"meeting", decisionEventScope{project: p, meeting: m}, []any{p.String(), m.String(), "", "", "", ""}},
		{"turn", decisionEventScope{project: p, meeting: m, turn: turn}, []any{p.String(), m.String(), turn.String(), "", "", ""}},
		{"contribution", decisionEventScope{project: p, meeting: m, contribution: contribution}, []any{p.String(), m.String(), "", contribution.String(), "", ""}},
		{"execution", decisionEventScope{project: p, meeting: m, execution: execution}, []any{p.String(), m.String(), "", "", execution.String(), ""}},
		{"decision", decisionEventScope{project: p, meeting: m, decision: decision}, []any{p.String(), m.String(), "", "", "", decision.String()}},
		{"exact", decisionEventScope{project: p, meeting: m, turn: turn, contribution: contribution, execution: execution, decision: decision}, []any{p.String(), m.String(), turn.String(), contribution.String(), execution.String(), decision.String()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := &decisionScopeQuery{}
			if _, err := pendingDecisionEvents(context.Background(), query, tc.scope); err == nil {
				t.Fatal("SQL boundary failure was hidden")
			}
			if query.calls != 1 || !reflect.DeepEqual(query.args, tc.want) {
				t.Fatal("required scope or an explicit filter changed, or an absent filter became a zero UUID")
			}
		})
	}
}

func (s *decisionEventTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.active || tx != s.tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *decisionEventTestStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.locks = locks
	return nil
}
func (s *decisionEventTestStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	for _, required := range locks {
		found := false
		for _, held := range s.locks {
			found = found || held.Key.Canonical() == required.Key.Canonical() && (held.Mode == f.Exclusive || held.Mode == required.Mode)
		}
		if !found {
			return fault(f.Forbidden)
		}
	}
	return nil
}
func (s *decisionEventTestStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	oldPost, oldFact, oldDelivered := s.post, s.fact, s.delivered
	s.tx, s.active, s.locks = f.NewTx(), true, nil
	defer func() { s.active = false }()
	if err := fn(ctx, s.tx); err != nil {
		s.post, s.fact, s.delivered = oldPost, oldFact, oldDelivered
		var failure *f.Fault
		if errors.As(err, &failure) {
			return f.NotCommittedResult(failure)
		}
		return f.NotCommittedResult(fault(f.InternalError).WithCause(err))
	}
	if s.unknown {
		return f.UnknownResult(summaryTestID[f.TransactionAttempt](s.t), cause)
	}
	return f.CommittedResult()
}
func (s *decisionEventTestStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	return controlEventRow(func(dest ...any) error {
		if !s.active {
			s.t.Fatal("Decision event opened a separate read session")
		}
		switch {
		case strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_meeting.decision_requests"):
			*dest[0].(*bool) = reflect.DeepEqual(args, s.post)
		case strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_meeting.decision_events"):
			*dest[0].(*bool) = reflect.DeepEqual(args, s.fact)
		default:
			s.t.Fatalf("unexpected Decision event read: %s", query)
		}
		return nil
	})
}
func (s *decisionEventTestStore) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !s.active || !strings.HasPrefix(query, "INSERT INTO agenteam_meeting.decision_events") {
		s.t.Fatal("event escaped original transaction")
	}
	if s.fact != nil {
		return pgconn.CommandTag{}, fault(f.IdempotencyKeyReused)
	}
	s.fact = append([]any{}, args...)
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type decisionEventTestJournal struct {
	store     *decisionEventTestStore
	authority *EventAuthority
	issuer    oc.PlanIssuer
	reject    bool
}

func (j *decisionEventTestJournal) PrepareAppend(ctx context.Context, actor i.Actor, ev event.Event) (oc.AppendPlan, error) {
	deps, err := j.authority.discoverDecisionAppend(ctx, actor, ev.Summary())
	if err != nil {
		return oc.AppendPlan{}, err
	}
	binding, err := oc.SemanticDigest(actor, ev)
	if err != nil {
		return oc.AppendPlan{}, err
	}
	// The real Outbox also contributes locks; this independent extra EX lock
	// verifies that the domain carries the complete prepared union forward.
	p, _ := f.ParseID[i.Project](ev.Header().Scope.ProjectID.String())
	locks := append(deps.Locks(), projectLock(p, f.Exclusive))
	project, err := oc.NewDependencies(j.issuer, binding, locks, nil)
	if err != nil {
		return oc.AppendPlan{}, err
	}
	return oc.NewAppendPlan(j.issuer, oc.AppendPlanDetails{Event: ev, Semantic: binding, Producer: deps, Project: project, Locks: locks})
}
func (j *decisionEventTestJournal) AppendEventInTx(ctx context.Context, tx f.Tx, actor i.Actor, ev event.Event, plan oc.AppendPlan) (oc.AppendReceipt, error) {
	if !plan.Matches(j.issuer, actor, ev) {
		return oc.AppendReceipt{}, fault(f.Forbidden)
	}
	if err := j.authority.validateDecisionAppendInTx(ctx, tx, actor, ev.Summary(), plan.Details().Producer, oc.NewFact); err != nil {
		return oc.AppendReceipt{}, err
	}
	if j.reject {
		return oc.AppendReceipt{}, fault(f.DependencyUnavailable)
	}
	j.store.delivered = true
	return oc.AppendReceipt{EventID: ev.Header().EventID, Sequence: 1}, nil
}
func TestDecisionEventRequiresOriginalLiveTransition(t *testing.T) {
	for _, status := range []string{"pending", "answered", "skipped", "cancelled"} {
		for _, outcome := range []string{"committed", "outbox-rejected", "unknown", "wrong-postimage", "missing-proof", "missing-lock"} {
			t.Run(status+"/"+outcome, func(t *testing.T) {
				store := &decisionEventTestStore{t: t, unknown: outcome == "unknown"}
				authority := &EventAuthority{store: store, issuer: oc.NewPlanIssuer()}
				journal := &decisionEventTestJournal{store: store, authority: authority, issuer: oc.NewPlanIssuer(), reject: outcome == "outbox-rejected"}
				types, err := c.RegisterDecisionEvents(event.NewCatalog())
				if err != nil {
					t.Fatal(err)
				}
				events, err := NewDecisionEvents(authority, types, journal)
				if err != nil {
					t.Fatal(err)
				}
				actor, _ := i.NewHuman(summaryTestID[i.User](t), summaryTestID[i.Session](t))
				project := summaryTestID[i.Project](t)
				payload := c.DecisionPayload{DecisionID: summaryTestID[c.Decision](t), MeetingID: summaryTestID[c.Meeting](t), TurnID: summaryTestID[c.Turn](t), ContributionID: summaryTestID[c.Contribution](t), ParticipantID: summaryTestID[c.Participant](t), ExecutionID: summaryTestID[i.Execution](t), Status: status}
				version := f.Version(2)
				if status == "pending" {
					version = 1
				}
				at := time.Unix(100, 0).UTC()
				plan, err := events.prepare(context.Background(), actor, project, payload, version, at, []f.LockRequest{userLock(actor)})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = authority.discoverDecisionAppend(context.Background(), actor, plan.event.Summary()); err == nil {
					t.Fatal("public actor/event authorized an append")
				}
				cause, _ := readCause("meeting.decision.event-test")
				proofCalls := 0
				result := store.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
					locks := plan.locks
					if outcome == "missing-lock" {
						locks = []f.LockRequest{meetingLock(payload.MeetingID, f.Exclusive)}
					}
					if err := store.AcquireAll(ctx, tx, locks); err != nil {
						return err
					}
					store.post = []any{payload.DecisionID.String(), project.String(), payload.MeetingID.String(), payload.TurnID.String(), payload.ContributionID.String(), payload.ParticipantID.String(), payload.ExecutionID.String(), int64(version), status, at}
					if outcome == "wrong-postimage" {
						store.post[6] = summaryTestID[i.Execution](t).String()
					}
					var check func(context.Context, f.Tx) error
					if outcome != "missing-proof" {
						check = func(_ context.Context, borrowed f.Tx) error {
							proofCalls++
							if borrowed != tx || !store.active {
								return fault(f.Forbidden)
							}
							return nil
						}
					}
					return events.appendInTx(ctx, tx, plan, check)
				})
				switch outcome {
				case "committed", "unknown":
					if outcome == "unknown" && result.State() != f.Unknown || outcome == "committed" && result.State() != f.Committed || store.fact == nil || !store.delivered || proofCalls < 2 {
						t.Fatal("canonical fact and original delivery did not share commit", result.State(), proofCalls)
					}
				default:
					if result.State() != f.NotCommitted || store.fact != nil || store.post != nil || store.delivered {
						t.Fatal("rejection did not roll back the original state/event", result.State())
					}
				}
				if err = authority.validateDecisionAppendInTx(context.WithValue(context.Background(), decisionEventKey{}, plan), store.tx, actor, plan.event.Summary(), plan.append.Details().Producer, oc.NewFact); err == nil {
					t.Fatal("ended transition witness was reused")
				}
			})
		}
	}
}
