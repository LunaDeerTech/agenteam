package meeting

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

// GetContributionRuntime borrows only this current Contribution's original
// reader. Discovery never grants access to a previous generation or another E.
func (s *Service) GetContributionRuntime(ctx context.Context, actor i.Actor, project i.ProjectID, meeting c.MeetingID, contribution c.ContributionID) (c.ContributionRuntimeView, error) {
	if s == nil || ctx == nil || project.Validate() != nil || meeting.Validate() != nil || contribution.Validate() != nil {
		return c.ContributionRuntimeView{}, fault(f.InvalidArgument)
	}
	if err := human(actor); err != nil {
		return c.ContributionRuntimeView{}, err
	}
	if s.deps.DecisionAuthority == nil || !sameStore(s.store, s.deps.DecisionAuthority.store) || nilPort(s.deps.RuntimeViews) {
		return c.ContributionRuntimeView{}, fault(f.DependencyUnbound)
	}
	if _, err := s.GetSession(ctx, actor, project, meeting); err != nil {
		return c.ContributionRuntimeView{}, err
	}
	initial, err := loadCurrentContributionIntent(ctx, s.store, project, meeting, contribution)
	if err != nil {
		return c.ContributionRuntimeView{}, err
	}
	locks, err := ec.MeetingExecutionReadLocks(project, initial.association())
	if err != nil {
		return c.ContributionRuntimeView{}, err
	}
	locks, err = normalize(append(locks, userLock(actor), meetingLock(meeting, f.Exclusive)))
	if err != nil {
		return c.ContributionRuntimeView{}, err
	}
	cause, err := readCause("meeting.contribution-runtime")
	if err != nil {
		return c.ContributionRuntimeView{}, err
	}
	var out c.ContributionRuntimeView
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
		current, err := loadCurrentContributionIntent(ctx, x, project, meeting, contribution)
		if err != nil {
			return err
		}
		if current.id != initial.id || current.association() != initial.association() {
			return fault(f.ConfirmationStale)
		}
		w := &resultWitness{authority: s.deps.DecisionAuthority, tx: tx, project: project, meeting: meeting, intent: current, active: true}
		defer func() { w.active = false }()
		value, err := s.deps.RuntimeViews.ReadMeetingRuntimeInTx(context.WithValue(ctx, resultWitnessKey{}, w), tx, project, current.association())
		if err != nil {
			return portError(err)
		}
		out, err = projectContributionRuntime(current, value)
		return err
	})
	if err = commitError(result); err != nil {
		return c.ContributionRuntimeView{}, err
	}
	return out, nil
}

func projectContributionRuntime(intent intentRecord, value ec.MeetingRuntimeResult) (c.ContributionRuntimeView, error) {
	if err := value.ValidateProjection(); err != nil {
		return c.ContributionRuntimeView{}, portError(err)
	}
	execution, err := projectContributionExecution(intent, value.Execution)
	if err != nil {
		return c.ContributionRuntimeView{}, err
	}
	out := c.ContributionRuntimeView{Execution: execution, Availability: value.Availability, Snapshot: value.Snapshot}
	if err := out.Validate(); err != nil {
		return c.ContributionRuntimeView{}, portError(err)
	}
	return out, nil
}
