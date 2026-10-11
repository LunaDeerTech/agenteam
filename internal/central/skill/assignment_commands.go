package skill

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"slices"
	"time"

	ag "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// AssignmentRequest is a closed full intent. A remove supplies AssignmentID;
// an assign supplies SkillID and Enabled. RequestID is transport-only.
type AssignmentRequest struct {
	ProjectID    i.ProjectID      `json:"project_id"`
	AgentID      i.AgentID        `json:"agent_id"`
	SkillID      *sc.SkillID      `json:"skill_id,omitempty"`
	AssignmentID *sc.AssignmentID `json:"assignment_id,omitempty"`
	Enabled      *bool            `json:"enabled,omitempty"`
}

func (r AssignmentRequest) validate() error {
	if r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil {
		return invalid()
	}
	if r.SkillID != nil {
		if r.SkillID.Validate() != nil || r.Enabled == nil || r.AssignmentID != nil {
			return invalid()
		}
	} else if r.AssignmentID == nil || r.AssignmentID.Validate() != nil || r.Enabled != nil {
		return invalid()
	}
	return nil
}
func (r AssignmentRequest) clone() AssignmentRequest {
	if r.SkillID != nil {
		v := *r.SkillID
		r.SkillID = &v
	}
	if r.AssignmentID != nil {
		v := *r.AssignmentID
		r.AssignmentID = &v
	}
	if r.Enabled != nil {
		v := *r.Enabled
		r.Enabled = &v
	}
	return r
}
func (r AssignmentRequest) name() string {
	if r.SkillID != nil {
		return "skill.assign"
	}
	return "skill.remove"
}

type assignmentIntent struct {
	User     string            `json:"actor_user_id"`
	Expected f.Version         `json:"expected_version"`
	Request  AssignmentRequest `json:"request"`
}
type assignmentInput struct {
	Actor    i.Actor
	Meta     f.CommandMeta
	Request  AssignmentRequest
	Identity f.CommandIdentity
	Raw      []byte
	Digest   f.Digest
}

func newAssignmentInput(actor i.Actor, meta f.CommandMeta, r AssignmentRequest) (assignmentInput, error) {
	var zero assignmentInput
	if actor.Validate() != nil || actor.Details().Kind != i.Human || meta.Validate() != nil || meta.ExpectedVersion == nil || r.validate() != nil {
		return zero, invalid()
	}
	identity, err := f.NewCommandIdentity("project", []string{r.ProjectID.String(), r.AgentID.String()}, r.name(), meta.IdempotencyKey)
	if err != nil {
		return zero, err
	}
	d, raw, err := captureDigest(assignmentIntent{actor.Details().UserID, *meta.ExpectedVersion, r})
	if err != nil {
		return zero, err
	}
	expected := *meta.ExpectedVersion
	meta.ExpectedVersion = &expected
	return assignmentInput{actor, meta, r.clone(), identity, raw, d}, nil
}
func (in assignmentInput) agentRequest() ag.SkillAssignmentRequest {
	return ag.SkillAssignmentRequest{Actor: in.Actor, ProjectID: in.Request.ProjectID, AgentID: in.Request.AgentID, Command: in.Identity, ExpectedVersion: *in.Meta.ExpectedVersion}
}

// Construction is explicit. No default registry, runtime ingress or audit
// provider is synthesized by a nonnil facade.
type AssignmentDependencies struct {
	Service       *Service
	Agents        ag.SkillAssignmentOwner
	CurrentAgents ag.WorkReferences
	Runtime       sc.AssignmentRuntimeSink
	Audit         au.Appender
	Outbox        ob.Appender
	Events        sc.AssignmentEvents
}
type AssignmentCommands struct {
	deps  AssignmentDependencies
	facts *AssignmentAuthority
}

func NewAssignmentCommands(d AssignmentDependencies, facts *AssignmentAuthority) (*AssignmentCommands, error) {
	if d.Service.state() == nil || facts == nil || facts.owner != d.Service.state().authority || nilPort(d.Agents) || nilPort(d.CurrentAgents) || nilPort(d.Runtime) || nilPort(d.Audit) || nilPort(d.Outbox) || !d.Events.Valid() {
		return nil, fault(f.DependencyUnbound)
	}
	return &AssignmentCommands{d, facts}, nil
}
func (s *AssignmentCommands) store() Store { return s.deps.Service.state().authority.state().store }
func (s *AssignmentCommands) owner(ctx context.Context, tx f.Tx, in assignmentInput, intent i.AccessIntent) error {
	grant, err := s.deps.Service.state().authority.state().projects.RequireOwnerInTx(ctx, tx, in.Actor, in.Request.ProjectID, intent)
	if err != nil {
		return captureError(err)
	}
	if !grant.Matches(in.Actor, in.Request.ProjectID) {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

type AssignmentPlan interface{ RequiredLocks() []f.LockRequest }
type assignmentPlan struct {
	owner  *AssignmentCommands
	input  assignmentInput
	record *assignmentCommand
	agent  ag.SkillAssignmentPlan
	event  ec.Event
	append ob.AppendPlan
	locks  []f.LockRequest
}

func (p *assignmentPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}

// Discover stores only a recoverable command plan, never a partial assignment.
// Outbox discovery happens after that physical transaction has returned and
// before the final complete union is acquired. Unknown does not auto-replay.
func (s *AssignmentCommands) DiscoverAssignment(ctx context.Context, actor i.Actor, meta f.CommandMeta, request AssignmentRequest) (AssignmentPlan, error) {
	if s == nil || s.deps.Service.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	call, err := s.deps.Service.begin(ctx, false)
	if err != nil {
		return nil, err
	}
	defer s.deps.Service.end(call)
	return s.discover(call.ctx, actor, meta, request)
}
func (s *AssignmentCommands) discover(ctx context.Context, actor i.Actor, meta f.CommandMeta, request AssignmentRequest) (AssignmentPlan, error) {
	in, err := newAssignmentInput(actor, meta, request)
	if err != nil {
		return nil, err
	}
	agentPlan, err := s.deps.Agents.DiscoverSkillAssignment(ctx, in.agentRequest())
	if err != nil {
		return nil, captureError(err)
	}
	if nilPort(agentPlan) {
		return nil, unavailable(nil)
	}
	locks := agentPlan.RequiredLocks()
	// Remove's Skill is discovered only as metadata; final Tx verifies that
	// exact original grant under the new complete union, never acquires late.
	target := sc.SkillID{}
	if request.SkillID != nil {
		target = *request.SkillID
	} else {
		cause, _ := f.NewCommandsCause(in.Identity)
		result := s.store().WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			if err := s.store().AcquireAll(ctx, tx, locks); err != nil {
				return captureError(err)
			}
			if err := s.owner(ctx, tx, in, i.Read); err != nil {
				return err
			}
			x, err := s.store().InTx(tx)
			if err != nil {
				return captureError(err)
			}
			old, err := loadAssignmentCommand(ctx, x, in)
			if err != nil {
				return err
			}
			if old != nil {
				target = old.Candidate.Skill
				return nil
			}
			var raw string
			err = x.QueryRow(ctx, `SELECT skill_id::text FROM agenteam_skill.agent_assignments WHERE project_id=$1 AND agent_id=$2 AND id=$3`, request.ProjectID.String(), request.AgentID.String(), request.AssignmentID.String()).Scan(&raw)
			if err != nil {
				return assignmentReadError(err)
			}
			target, err = f.ParseID[pc.Skill](raw)
			return captureError(err)
		})
		if err = commitError(result); err != nil {
			return nil, err
		}
	}
	locks = append(locks, skillLock(target, f.Shared))
	locks, err = ob.NormalizeLocks(locks)
	if err != nil {
		return nil, err
	}
	var record *assignmentCommand
	cause, _ := f.NewCommandsCause(in.Identity)
	result := s.store().WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store().AcquireAll(ctx, tx, locks); err != nil {
			return captureError(err)
		}
		return s.deps.Agents.WithSkillAssignmentInTx(ctx, tx, in.agentRequest(), agentPlan, func(ctx context.Context, current ag.AgentRef) (bool, error) {
			x, err := s.store().InTx(tx)
			if err != nil {
				return false, captureError(err)
			}
			record, err = loadAssignmentCommand(ctx, x, in)
			if err != nil {
				return false, err
			}
			if record != nil {
				return false, nil
			}
			if err = s.newAccess(ctx, tx, in, current); err != nil {
				return false, err
			}
			record, err = planAssignmentCommand(ctx, x, in, current, target)
			if err != nil {
				return false, err
			}
			return false, insertAssignmentCommand(ctx, x, record)
		}, func(context.Context, ag.AgentRef) error { return fault(f.InvalidState) })
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if record == nil {
		return nil, unavailable(nil)
	}
	p := &assignmentPlan{owner: s, input: in, record: record, agent: agentPlan, locks: locks}
	if record.State == "completed" || !record.Candidate.Changed {
		return p, nil
	}
	p.event, err = assignmentEvent(s.deps.Events, record)
	if err != nil {
		return nil, err
	}
	p.append, err = s.deps.Outbox.PrepareAppend(ctx, actor, p.event)
	if err != nil {
		return nil, captureError(err)
	}
	p.locks, err = ob.NormalizeLocks(append(p.locks, p.append.Locks()...))
	if err != nil {
		return nil, err
	}
	return p, ctx.Err()
}
func (s *AssignmentCommands) newAccess(ctx context.Context, tx f.Tx, in assignmentInput, current ag.AgentRef) error {
	if err := s.owner(ctx, tx, in, i.Mutate); err != nil {
		return err
	}
	if current.ConfigVersion != *in.Meta.ExpectedVersion {
		return fault(f.VersionConflict)
	}
	_, err := s.deps.CurrentAgents.RequireCurrentInTx(ctx, tx, in.Actor, in.Request.ProjectID, in.Request.AgentID)
	return captureError(err)
}

// Consume an issuer-owned plan only inside the caller's original transaction.
// A returned value is a candidate until that caller observes physical COMMIT.
func (s *AssignmentCommands) ApplyAssignmentInTx(ctx context.Context, tx f.Tx, plan AssignmentPlan) (sc.AssignmentMutation, error) {
	if s == nil {
		return sc.AssignmentMutation{}, fault(f.DependencyUnbound)
	}
	call, err := s.deps.Service.begin(ctx, false)
	if err != nil {
		return sc.AssignmentMutation{}, err
	}
	defer s.deps.Service.end(call)
	return s.applyAssignmentInTx(call.ctx, tx, plan)
}
func (s *AssignmentCommands) applyAssignmentInTx(ctx context.Context, tx f.Tx, plan AssignmentPlan) (sc.AssignmentMutation, error) {
	var out sc.AssignmentMutation
	p, ok := plan.(*assignmentPlan)
	if !ok || p == nil || p.owner != s || ctx == nil {
		return out, fault(f.Forbidden)
	}
	if err := s.store().RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return out, captureError(err)
	}
	var selected *assignmentCommand
	err := s.deps.Agents.WithSkillAssignmentInTx(ctx, tx, p.input.agentRequest(), p.agent, func(ctx context.Context, ref ag.AgentRef) (bool, error) {
		x, err := s.store().InTx(tx)
		if err != nil {
			return false, captureError(err)
		}
		selected, err = loadAssignmentCommand(ctx, x, p.input)
		if err != nil {
			return false, err
		}
		if selected == nil {
			return false, unavailable(nil)
		}
		if selected.State == "completed" {
			out, err = selected.receipt()
			return false, err
		}
		if err = s.newAccess(ctx, tx, p.input, ref); err != nil {
			return false, err
		}
		if selected.Digest != p.record.Digest || !sameAssignmentCandidate(selected.Candidate, p.record.Candidate) {
			return false, fault(f.ConfirmationStale)
		}
		current, err := loadAssignmentSet(ctx, x, p.input.Request.ProjectID, p.input.Request.AgentID)
		if err != nil {
			return false, err
		}
		d, _, err := captureDigest(current)
		if err != nil {
			return false, err
		}
		if d != selected.Candidate.BeforeDigest {
			return false, fault(f.VersionConflict)
		}
		if !selected.Candidate.Changed {
			if p.input.Request.SkillID != nil {
				if _, _, err = loadAssignmentRevision(ctx, x, p.input.Request.ProjectID, *p.input.Request.SkillID); err != nil {
					return false, err
				}
			}
			out = selected.mutation(nil)
			return false, completeAssignmentCommand(ctx, x, selected, out, nil)
		}
		return true, nil
	}, func(ctx context.Context, ref ag.AgentRef) error {
		if selected == nil || ref.ConfigVersion != selected.Candidate.Version {
			return unavailable(nil)
		}
		x, err := s.store().InTx(tx)
		if err != nil {
			return captureError(err)
		}
		if err = applyAssignmentState(ctx, x, selected); err != nil {
			return err
		}
		witness := s.facts.witness(ctx, tx, selected)
		defer witness.expire()
		var receipt *sc.AssignmentIngressReceipt
		if selected.Candidate.Runtime != nil {
			r, err := s.deps.Runtime.RecordInTx(witness.ctx, tx, *selected.Candidate.Runtime)
			if err != nil {
				return captureError(err)
			}
			if r.Validate() != nil || r.ChangeID != selected.ID {
				return unavailable(nil)
			}
			frozen := r.Clone()
			receipt = &frozen
		}
		entry, key, err := assignmentAudit(selected, p.input.Actor)
		if err != nil {
			return err
		}
		audit, err := s.deps.Audit.AppendInTx(witness.ctx, tx, entry, key)
		if err != nil {
			return captureError(err)
		}
		if audit.AuditID.Validate() != nil || audit.CreatedAt.Validate() != nil {
			return unavailable(nil)
		}
		event, err := s.deps.Outbox.AppendEventInTx(witness.ctx, tx, p.input.Actor, p.event, p.append)
		if err != nil {
			return captureError(err)
		}
		if event.EventID != p.event.Header().EventID || event.Sequence.Validate() != nil {
			return unavailable(nil)
		}
		out = selected.mutation(receipt)
		return completeAssignmentCommand(ctx, x, selected, out, receipt)
	})
	if err != nil {
		return sc.AssignmentMutation{}, captureError(err)
	}
	if ctx.Err() != nil {
		return sc.AssignmentMutation{}, ctx.Err()
	}
	if out.Validate() != nil {
		return sc.AssignmentMutation{}, unavailable(nil)
	}
	return out.Clone(), nil
}
func (s *AssignmentCommands) ExecuteAssignment(ctx context.Context, actor i.Actor, meta f.CommandMeta, request AssignmentRequest) (sc.AssignmentMutation, error) {
	var zero sc.AssignmentMutation
	if s == nil {
		return zero, fault(f.DependencyUnbound)
	}
	call, err := s.deps.Service.begin(ctx, false)
	if err != nil {
		return zero, err
	}
	defer s.deps.Service.end(call)
	plan, err := s.discover(call.ctx, actor, meta, request)
	if err != nil {
		return zero, err
	}
	p := plan.(*assignmentPlan)
	var out sc.AssignmentMutation
	cause, _ := f.NewCommandsCause(p.input.Identity)
	result := s.store().WithinTx(call.ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store().AcquireAll(ctx, tx, p.locks); err != nil {
			return captureError(err)
		}
		var err error
		out, err = s.applyAssignmentInTx(ctx, tx, p)
		return err
	})
	if err = commitError(result); err != nil {
		return zero, err
	}
	return out.Clone(), nil
}
func (s *AssignmentCommands) AssignSkill(ctx context.Context, actor i.Actor, meta f.CommandMeta, p i.ProjectID, a i.AgentID, skill sc.SkillID, enabled bool) (sc.AssignmentMutation, error) {
	return s.ExecuteAssignment(ctx, actor, meta, AssignmentRequest{ProjectID: p, AgentID: a, SkillID: &skill, Enabled: &enabled})
}
func (s *AssignmentCommands) RemoveSkill(ctx context.Context, actor i.Actor, meta f.CommandMeta, p i.ProjectID, a i.AgentID, assignment sc.AssignmentID) (sc.AssignmentMutation, error) {
	return s.ExecuteAssignment(ctx, actor, meta, AssignmentRequest{ProjectID: p, AgentID: a, AssignmentID: &assignment})
}

type AssignmentLookup struct {
	State   string
	Receipt *sc.AssignmentMutation
}

func (s *AssignmentCommands) LookupAssignment(ctx context.Context, actor i.Actor, meta f.CommandMeta, request AssignmentRequest) (AssignmentLookup, error) {
	var zero AssignmentLookup
	if s == nil {
		return zero, fault(f.DependencyUnbound)
	}
	call, err := s.deps.Service.begin(ctx, false)
	if err != nil {
		return zero, err
	}
	defer s.deps.Service.end(call)
	in, err := newAssignmentInput(actor, meta, request)
	if err != nil {
		return zero, err
	}
	locks, err := ob.NormalizeLocks(in.agentRequest().RequiredLocks())
	if err != nil {
		return zero, err
	}
	out := AssignmentLookup{State: "not_observed"}
	cause, _ := f.NewCommandsCause(in.Identity)
	result := s.store().WithinTx(call.ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store().AcquireAll(ctx, tx, locks); err != nil {
			return captureError(err)
		}
		if err := s.owner(ctx, tx, in, i.Read); err != nil {
			return err
		}
		x, err := s.store().InTx(tx)
		if err != nil {
			return captureError(err)
		}
		r, err := loadAssignmentCommand(ctx, x, in)
		if err != nil {
			return err
		}
		if r != nil && r.State == "completed" {
			v, err := r.receipt()
			if err != nil {
				return err
			}
			out = AssignmentLookup{State: "committed", Receipt: &v}
		}
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return zero, err
	}
	return out, nil
}

func assignmentNow(ctx context.Context, x postgres.SQLExecutor) (f.Instant, error) {
	var at time.Time
	if err := x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
		return f.Instant{}, unavailable(err)
	}
	return f.NewInstant(at)
}
func nextAssignmentVersion(v f.Version) (f.Version, error) {
	if v < 1 || v == f.Version(math.MaxInt64) {
		return 0, fault(f.InvalidState)
	}
	return v + 1, nil
}
func sameAssignmentCandidate(a, b assignmentCandidate) bool {
	left, _, e := captureDigest(a)
	right, _, e2 := captureDigest(b)
	return e == nil && e2 == nil && left == right
}

func (*assignmentPlan) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "skill_assignment_plan") }
func (*assignmentPlan) LogValue() slog.Value         { return slog.StringValue("skill_assignment_plan") }
func (*assignmentPlan) MarshalJSON() ([]byte, error) { return []byte(`"skill_assignment_plan"`), nil }
