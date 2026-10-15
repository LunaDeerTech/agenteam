package meeting

import (
	"context"
	"encoding/json"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/realtime/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// RealtimeAuthority owns only the Meeting source read gate. It never reads
// another domain's private table or turns a subscription into execution rights.
type RealtimeAuthority struct{ original *Service }

func NewRealtimeAuthority(original *Service) (*RealtimeAuthority, error) {
	if original == nil || nilPort(original.store) || nilPort(original.deps.Projects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &RealtimeAuthority{original: original}, nil
}

// RequireSubscription adapts only the platform's closed topic vocabulary. A
// browser cannot omit the Contribution lineage or alias an Execution through
// a different Meeting in order to receive the same source's notifications.
func (a *RealtimeAuthority) RequireSubscription(ctx context.Context, actor i.Actor, topic rc.Topic) error {
	if err := topic.Validate(); err != nil {
		return portError(err)
	}
	meeting, err := f.ParseID[c.Meeting](topic.MeetingID)
	if err != nil {
		return fault(f.InvalidArgument)
	}
	if topic.Kind == rc.MeetingTimeline {
		return a.RequireTimelineSubscription(ctx, actor, topic.ProjectID, meeting)
	}
	contribution, err := f.ParseID[c.Contribution](topic.ContributionID)
	if err != nil {
		return fault(f.InvalidArgument)
	}
	return a.RequireRuntimeSubscription(ctx, actor, topic.ProjectID, meeting, contribution, topic.Generation, topic.Attempt, topic.ExecutionID)
}

func (a *RealtimeAuthority) ReadSnapshot(ctx context.Context, actor i.Actor, topic rc.Topic) (rc.Snapshot, error) {
	if err := topic.Validate(); err != nil {
		return rc.Snapshot{}, portError(err)
	}
	if topic.Kind == rc.MeetingTimeline {
		return a.readTimelineSnapshot(ctx, actor, topic)
	}
	return a.readRuntimeSnapshot(ctx, actor, topic)
}

func (a *RealtimeAuthority) readRuntimeSnapshot(ctx context.Context, actor i.Actor, topic rc.Topic) (rc.Snapshot, error) {
	if a == nil || a.original == nil {
		return rc.Snapshot{}, fault(f.DependencyUnbound)
	}
	s := a.original
	if s.deps.DecisionAuthority == nil || !sameStore(s.store, s.deps.DecisionAuthority.store) || nilPort(s.deps.RuntimeProjections) {
		return rc.Snapshot{}, fault(f.DependencyUnbound)
	}
	meeting, _ := f.ParseID[c.Meeting](topic.MeetingID)
	contribution, _ := f.ParseID[c.Contribution](topic.ContributionID)
	var initial intentRecord
	// Discovery itself borrows the original Owner transaction. Its result is
	// only a lock budget, never permission to read Execution after it returns.
	err := a.requireCurrent(ctx, actor, topic.ProjectID, meeting, func(ctx context.Context, x postgres.SQLExecutor) error {
		var err error
		initial, err = loadCurrentContributionIntent(ctx, x, topic.ProjectID, meeting, contribution)
		if err != nil {
			return err
		}
		return requireRuntimeTopic(initial, topic)
	})
	if err != nil {
		return rc.Snapshot{}, err
	}
	locks, err := ec.MeetingExecutionReadLocks(topic.ProjectID, initial.association())
	if err != nil {
		return rc.Snapshot{}, portError(err)
	}
	locks, err = normalize(append(locks, userLock(actor), meetingLock(meeting, f.Exclusive)))
	if err != nil {
		return rc.Snapshot{}, err
	}
	cause, err := readCause("meeting.realtime-runtime")
	if err != nil {
		return rc.Snapshot{}, err
	}
	var value ec.RuntimeProjection
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, actor, topic.ProjectID, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		session, err := loadSession(ctx, x, topic.ProjectID, meeting)
		if err != nil {
			return err
		}
		if session.Deleting {
			return fault(f.InvalidState)
		}
		current, err := loadCurrentContributionIntent(ctx, x, topic.ProjectID, meeting, contribution)
		if err != nil {
			return err
		}
		if current.id != initial.id || current.association() != initial.association() {
			return fault(f.ConfirmationStale)
		}
		if err := requireRuntimeTopic(current, topic); err != nil {
			return err
		}
		w := &resultWitness{authority: s.deps.DecisionAuthority, tx: tx, project: topic.ProjectID, meeting: meeting, intent: current, active: true}
		defer func() { w.active = false }()
		value, err = s.deps.RuntimeProjections.ReadMeetingRuntimeProjectionInTx(context.WithValue(ctx, resultWitnessKey{}, w), tx, topic.ProjectID, current.association())
		if err != nil {
			return portError(err)
		}
		if value.ExecutionID != topic.ExecutionID || value.ProjectID != topic.ProjectID {
			return fault(f.ConfirmationStale)
		}
		if err := value.Validate(); err != nil {
			return portError(err)
		}
		return ctx.Err()
	})
	if err := commitError(result); err != nil {
		return rc.Snapshot{}, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return rc.Snapshot{}, portError(err)
	}
	return rc.NewSnapshot(topic, raw)
}

func requireRuntimeTopic(current intentRecord, topic rc.Topic) error {
	lineage, trigger := current.request.Lineage, current.request.Trigger
	if current.execution == nil || *current.execution != topic.ExecutionID || current.request.ProjectID != topic.ProjectID || trigger.MeetingID != topic.MeetingID || trigger.ContributionID != topic.ContributionID || lineage.ContributionGeneration == nil || lineage.ContributionAttempt == nil || *lineage.ContributionGeneration != topic.Generation || *lineage.ContributionAttempt != topic.Attempt {
		return fault(f.ConfirmationStale)
	}
	return nil
}

func (a *RealtimeAuthority) readTimelineSnapshot(ctx context.Context, actor i.Actor, topic rc.Topic) (rc.Snapshot, error) {
	if topic.Kind != rc.MeetingTimeline {
		return rc.Snapshot{}, fault(f.InvalidArgument)
	}
	if err := a.RequireSubscription(ctx, actor, topic); err != nil {
		return rc.Snapshot{}, err
	}
	meeting, _ := f.ParseID[c.Meeting](topic.MeetingID)
	detail, err := a.original.GetDetail(ctx, actor, topic.ProjectID, meeting)
	if err != nil {
		return rc.Snapshot{}, err
	}
	if detail.Session.ID != meeting || detail.Session.ProjectID != topic.ProjectID || detail.Session.Deleting {
		return rc.Snapshot{}, fault(f.ConfirmationStale)
	}
	// The original read owns its full transaction. Do not carry a grant from
	// the earlier subscription across a revocation/deletion during that read.
	if err := a.RequireSubscription(ctx, actor, topic); err != nil {
		return rc.Snapshot{}, err
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return rc.Snapshot{}, portError(err)
	}
	return rc.NewSnapshot(topic, raw)
}

func (a *RealtimeAuthority) RequireTimelineSubscription(ctx context.Context, actor i.Actor, project i.ProjectID, meeting c.MeetingID) error {
	return a.requireCurrent(ctx, actor, project, meeting, nil)
}

func (a *RealtimeAuthority) RequireRuntimeSubscription(ctx context.Context, actor i.Actor, project i.ProjectID, meeting c.MeetingID, contribution c.ContributionID, generation, attempt f.Version, execution i.ExecutionID) error {
	if contribution.Validate() != nil || generation.Validate() != nil || attempt.Validate() != nil || execution.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	return a.requireCurrent(ctx, actor, project, meeting, func(ctx context.Context, x postgres.SQLExecutor) error {
		// Meeting SH protects the current pointer and the immutable launch
		// association together. This performs no Execution read; that provider
		// must acquire its own full original lock union when serving a snapshot.
		current, err := loadCurrentContributionIntent(ctx, x, project, meeting, contribution)
		if err != nil {
			return err
		}
		lineage := current.request.Lineage
		if current.execution == nil || *current.execution != execution || lineage.ContributionGeneration == nil || lineage.ContributionAttempt == nil || *lineage.ContributionGeneration != generation || *lineage.ContributionAttempt != attempt {
			return fault(f.ConfirmationStale)
		}
		return nil
	})
}

func (a *RealtimeAuthority) requireCurrent(ctx context.Context, actor i.Actor, project i.ProjectID, meeting c.MeetingID, check func(context.Context, postgres.SQLExecutor) error) error {
	if a == nil || a.original == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || project.Validate() != nil || meeting.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := human(actor); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s := a.original
	locks, err := normalize([]f.LockRequest{userLock(actor), projectLock(project, f.Shared), meetingLock(meeting, f.Shared)})
	if err != nil {
		return err
	}
	cause, err := readCause("meeting.realtime-read")
	if err != nil {
		return err
	}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, actor, project, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		session, err := loadSession(ctx, x, project, meeting)
		if err != nil {
			return err
		}
		if session.Deleting {
			return fault(f.InvalidState)
		}
		if check != nil {
			if err := check(ctx, x); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
	return commitError(result)
}

var _ c.RealtimeReadAuthority = (*RealtimeAuthority)(nil)
var _ rc.Authority = (*RealtimeAuthority)(nil)

func invalidateMeeting(publisher rc.Publisher, project i.ProjectID, meeting c.MeetingID) {
	if !nilPort(publisher) {
		publisher.InvalidateMeeting(project, meeting.String())
	}
}

func (s *Service) invalidateMeeting(project i.ProjectID, meeting c.MeetingID) {
	invalidateMeeting(s.deps.Realtime, project, meeting)
}

// runtimeMutation observes only the returned command tag of the original
// borrowed executor. It retains no SQL/arguments, opens no session and grants
// no additional authority. Runtime mutations use Exec; read-only/idle visits
// must never be converted into a continuous stream of invented progress.
type runtimeMutation struct {
	postgres.SQLExecutor
	changed bool
}

func (x *runtimeMutation) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tag, err := x.SQLExecutor.Exec(ctx, query, args...)
	if err == nil && tag.RowsAffected() > 0 && (tag.Insert() || tag.Update() || tag.Delete()) {
		x.changed = true
	}
	return tag, err
}

func publishRuntimeMutation(publisher rc.Publisher, project i.ProjectID, meeting c.MeetingID, result f.CommitResult, writes *runtimeMutation) error {
	if err := commitError(result); err != nil {
		return err
	}
	if writes != nil && writes.changed {
		invalidateMeeting(publisher, project, meeting)
	}
	return nil
}
