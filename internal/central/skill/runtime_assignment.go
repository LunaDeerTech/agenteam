package skill

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"sync/atomic"

	ag "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// RuntimeAssignmentCommands lends the original live Tool source to the same
// canonical command owner used by Human assignments. It has no second ledger.
type RuntimeAssignmentCommands struct {
	commands *AssignmentCommands
	runtime  ag.SkillAssignmentRunAuthority
}

func NewRuntimeAssignmentCommands(commands *AssignmentCommands, runtime ag.SkillAssignmentRunAuthority) (*RuntimeAssignmentCommands, error) {
	if commands == nil || commands.deps.Service.state() == nil || nilPort(runtime) {
		return nil, fault(f.DependencyUnbound)
	}
	return &RuntimeAssignmentCommands{commands, runtime}, nil
}

type assignmentRunKey struct{}
type assignmentRunWitness struct {
	owner     *RuntimeAssignmentCommands
	actor     i.Actor
	project   i.ProjectID
	run       ag.SkillAssignmentRun
	plan      ag.SkillAssignmentRunPlan
	input     assignmentInput
	arguments []byte
	lookup    bool
	active    atomic.Bool
}

func (w *assignmentRunWitness) matches(in assignmentInput) bool {
	return w != nil && w.active.Load() && w.actor.Equal(in.Actor) && w.project == in.Request.ProjectID && in.Run != nil && w.run == *in.Run && w.input.Digest == in.Digest && w.input.Identity.Canonical() == in.Identity.Canonical()
}
func assignmentRun(ctx context.Context, in assignmentInput) (*assignmentRunWitness, error) {
	if ctx == nil {
		return nil, fault(f.Forbidden)
	}
	w, _ := ctx.Value(assignmentRunKey{}).(*assignmentRunWitness)
	if !w.matches(in) {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}
func (s *AssignmentCommands) input(ctx context.Context, actor i.Actor, meta f.CommandMeta, r AssignmentRequest) (assignmentInput, error) {
	if actor.Details().Kind == i.Human {
		return newAssignmentInput(actor, meta, r)
	}
	w, _ := ctx.Value(assignmentRunKey{}).(*assignmentRunWitness)
	if w == nil || w.owner == nil || w.owner.commands != s || !w.active.Load() {
		return assignmentInput{}, fault(f.Forbidden)
	}
	in, err := newAssignmentRunInput(actor, meta, r, &w.run)
	if err != nil {
		return assignmentInput{}, err
	}
	if !w.matches(in) {
		return assignmentInput{}, fault(f.Forbidden)
	}
	return in, nil
}
func requireAssignmentRun(ctx context.Context, tx f.Tx, in assignmentInput, intent i.AccessIntent) error {
	w, err := assignmentRun(ctx, in)
	if err != nil {
		return err
	}
	if w.lookup && intent != i.Read {
		return fault(f.Forbidden)
	}
	return w.owner.runtime.RequireSkillAssignmentRunInTx(ctx, tx, in.agentRequest(ctx), intent)
}
func (s *RuntimeAssignmentCommands) call(ctx context.Context, actor i.Actor, project i.ProjectID, run ag.SkillAssignmentRun, raw []byte, lookup bool) ([]byte, bool, error) {
	if s == nil || s.commands == nil || run.Validate(actor, project) != nil {
		return nil, false, fault(f.Forbidden)
	}
	if f.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(raw))) != run.InputDigest {
		return nil, false, fault(f.Forbidden)
	}
	args, err := sc.DecodeAssignmentToolArguments(raw)
	if err != nil {
		return nil, false, err
	}
	if args.Action == "catalog" {
		if lookup {
			return nil, false, nil
		} // No receipt or replayable mutation exists.
		return s.catalog(ctx, actor, project, run, args)
	}
	plan, err := s.runtime.DiscoverSkillAssignmentRun(ctx, actor, project, run)
	if err != nil {
		return nil, false, err
	}
	if nilPort(plan) {
		return nil, false, fault(f.DependencyUnbound)
	}
	req := AssignmentRequest{ProjectID: project, AgentID: args.AgentID, SkillID: args.SkillID, AssignmentID: args.AssignmentID, Enabled: args.Enabled}
	requestID, err := f.ParseID[f.Request](run.RequestID)
	if err != nil {
		return nil, false, err
	}
	meta := f.CommandMeta{RequestID: requestID, IdempotencyKey: f.IdempotencyKey(run.OperationID), ExpectedVersion: &args.ExpectedVersion}
	in, err := newAssignmentRunInput(actor, meta, req, &run)
	if err != nil {
		return nil, false, err
	}
	w := &assignmentRunWitness{owner: s, actor: actor, project: project, run: run, plan: plan, input: in, arguments: slices.Clone(raw), lookup: lookup}
	w.active.Store(true)
	defer w.active.Store(false)
	ctx = context.WithValue(ctx, assignmentRunKey{}, w)
	if lookup {
		found, err := s.commands.LookupAssignment(ctx, actor, meta, req)
		if err != nil || found.Receipt == nil {
			return nil, false, err
		}
		out, err := json.Marshal(found.Receipt)
		return out, err == nil, err
	}
	receipt, err := s.commands.ExecuteAssignment(ctx, actor, meta, req)
	if err != nil {
		return nil, false, err
	}
	out, err := json.Marshal(receipt)
	return out, err == nil, err
}
func (s *RuntimeAssignmentCommands) Execute(ctx context.Context, a i.Actor, p i.ProjectID, r ag.SkillAssignmentRun, raw []byte) ([]byte, error) {
	out, _, err := s.call(ctx, a, p, r, raw, false)
	return out, err
}
func (s *RuntimeAssignmentCommands) Lookup(ctx context.Context, a i.Actor, p i.ProjectID, r ag.SkillAssignmentRun, raw []byte) ([]byte, bool, error) {
	return s.call(ctx, a, p, r, raw, true)
}
func (s *RuntimeAssignmentCommands) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if s == nil || s.commands == nil {
		return fault(f.DependencyUnbound)
	}
	return s.commands.deps.Service.RequireSkillBuiltinBindingInTx(ctx, tx, store)
}
