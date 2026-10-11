package mount

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	modelc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Controlled SQL/authority protocol checks only. Real PG, Account, Agent
// command durability and migration enforcement require the integration chain.
type runnerMountStore struct {
	*controlledStore
	mappings map[i.MountID]workspaceMapping
}

func (s *runnerMountStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, fault(f.DependencyUnavailable)
	}
	return s, nil
}
func (s *runnerMountStore) RequireHeldLocks(_ context.Context, tx f.Tx, want []f.LockRequest) error {
	if tx != s.tx || s.missingLocks {
		return fault(f.Forbidden)
	}
	for _, request := range want {
		if !slices.ContainsFunc(s.locks, func(held f.LockRequest) bool {
			return held.Key.Canonical() == request.Key.Canonical() && (held.Mode == request.Mode || held.Mode == f.Exclusive)
		}) {
			return fault(f.Forbidden)
		}
	}
	return nil
}
func (s *runnerMountStore) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	switch {
	case strings.HasPrefix(sql, "SELECT COALESCE(jsonb_agg(id"):
		ids := []string{}
		for _, d := range s.definitions {
			if d.Project.String() == args[0] && d.Agent.String() == args[1] && d.Runner.String() == args[2] && d.Workspace == args[3] {
				ids = append(ids, d.ID.String())
			}
		}
		slices.Sort(ids)
		if len(ids) > 2 {
			ids = ids[:2]
		}
		raw, _ := json.Marshal(ids)
		return controlRow{values: []any{raw}}
	case strings.HasPrefix(sql, "SELECT workspace_id,mount_id"):
		id, _ := f.ParseID[i.Mount](args[0].(string))
		m, ok := s.mappings[id]
		if !ok {
			return controlRow{err: pgx.ErrNoRows}
		}
		return controlRow{values: []any{m.binding.WorkspaceID.String(), id.String(), m.project.String(), m.agent.String(), m.binding.RunnerID.String(), m.binding.Workspace}}
	case strings.HasPrefix(sql, "SELECT EXISTS(SELECT 1 FROM agenteam_mount.mounts"):
		id, _ := f.ParseID[i.Mount](args[0].(string))
		_, ok := s.definitions[id]
		return controlRow{values: []any{ok}}
	case strings.HasPrefix(sql, "SELECT EXISTS(SELECT 1 FROM agenteam_mount.runner_workspaces"):
		found := false
		for _, m := range s.mappings {
			if m.binding.WorkspaceID.String() == args[0] || m.binding.RunnerID.String() == args[1] && m.project.String() == args[2] && m.agent.String() == args[3] && m.binding.Workspace == args[4] {
				found = true
			}
		}
		return controlRow{values: []any{found}}
	default:
		return s.controlledStore.QueryRow(ctx, sql, args...)
	}
}
func (s *runnerMountStore) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !s.applied {
		panic("Mount write without original Agent witness")
	}
	switch {
	case strings.HasPrefix(sql, "INSERT INTO agenteam_mount.mounts("):
		id, _ := f.ParseID[i.Mount](args[0].(string))
		p, _ := f.ParseID[i.Project](args[1].(string))
		a, _ := f.ParseID[i.Agent](args[2].(string))
		r, _ := f.ParseID[rc.Runner](args[3].(string))
		if _, ok := s.definitions[id]; ok {
			panic("duplicate definition")
		}
		s.definitions[id] = definition{ID: id, Project: p, Agent: a, Runner: r, Name: id.String(), Workspace: args[4].(string), Lifecycle: "active", Version: 1}
		s.writes++
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.HasPrefix(sql, "INSERT INTO agenteam_mount.runner_workspaces("):
		w, _ := f.ParseID[mc.Workspace](args[0].(string))
		id, _ := f.ParseID[i.Mount](args[1].(string))
		p, _ := f.ParseID[i.Project](args[2].(string))
		a, _ := f.ParseID[i.Agent](args[3].(string))
		r, _ := f.ParseID[rc.Runner](args[4].(string))
		if _, ok := s.mappings[id]; ok {
			panic("duplicate workspace mapping")
		}
		s.mappings[id] = workspaceMapping{ac.RunnerMountBinding{MountID: id, WorkspaceID: w, RunnerID: r, Workspace: args[5].(string)}, p, a}
		s.writes++
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	default:
		return s.controlledStore.Exec(ctx, sql, args...)
	}
}

type runnerMountCandidates struct {
	ids   []rc.RunnerID
	deny  bool
	calls int
}

func (*runnerMountCandidates) ConfigurationCandidateLocks(ids []rc.RunnerID) ([]f.LockRequest, error) {
	key, _ := f.SystemConfigLock("runner-management")
	return []f.LockRequest{{Key: key, Mode: f.Shared}}, nil
}
func (r *runnerMountCandidates) RequireConfigurationCandidatesInTx(_ context.Context, _ f.Tx, _ i.Actor, _ i.ProjectID, ids []rc.RunnerID) ([]rc.ConfigurationCandidate, error) {
	r.calls++
	r.ids = slices.Clone(ids)
	if r.deny {
		return nil, fault(f.NotFound)
	}
	out := make([]rc.ConfigurationCandidate, 0, len(ids))
	for _, id := range ids {
		out = append(out, rc.ConfigurationCandidate{ID: id, Name: "owned candidate", Version: 1, Status: rc.Offline})
	}
	return out, nil
}
func runnerMountFixture(t *testing.T) (*Configuration, *runnerMountStore, *controlledProject, *controlledOwner, *runnerMountCandidates, ac.MountConfigurationChange, ac.RunnerMountPlanRequest) {
	t.Helper()
	_, base, p, o, change := fixture(t)
	s := &runnerMountStore{controlledStore: base, mappings: map[i.MountID]workspaceMapping{}}
	runners := &runnerMountCandidates{}
	c, err := NewRunnerMountConfiguration(s, p, o, runners)
	if err != nil {
		t.Fatal(err)
	}
	r := ac.RunnerMountPlanRequest{Actor: change.Actor, ProjectID: change.ProjectID, AgentID: change.AgentID, Command: change.Command, Declarations: []ac.RunnerMountDeclaration{{RunnerID: testID[rc.Runner](t, 5), Workspace: "source"}, {RunnerID: testID[rc.Runner](t, 5), Workspace: strings.Repeat("w", 255)}}}
	return c, s, p, o, runners, change, r
}
func runnerMountPlan(t *testing.T, c *Configuration, s *runnerMountStore, r ac.RunnerMountPlanRequest) []ac.RunnerMountBinding {
	t.Helper()
	locks, err := c.RunnerMountPlanningLocks(r)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AcquireAll(context.Background(), s.tx, locks); err != nil {
		t.Fatal(err)
	}
	writes := s.writes
	s.applied = false
	bindings, err := c.PlanRunnerMountsInTx(context.Background(), s.tx, r)
	if err != nil {
		t.Fatal(err)
	}
	if s.writes != writes {
		t.Fatal("planning wrote Mount storage")
	}
	return bindings
}
func runnerMountChange(t *testing.T, r ac.MountConfigurationChange, b []ac.RunnerMountBinding) ac.MountConfigurationChange {
	t.Helper()
	ids, err := ac.RunnerMountIDs(b)
	if err != nil {
		t.Fatal(err)
	}
	r.After = ids
	r.RunnerMounts = &b
	if err = r.Validate(); err != nil {
		t.Fatal(err)
	}
	return r
}
func runnerMountApply(t *testing.T, c *Configuration, s *runnerMountStore, o *controlledOwner, r ac.MountConfigurationChange) {
	t.Helper()
	s.applied = false
	plan, err := c.DiscoverMountConfiguration(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.RequireMountConfigurationInTx(context.Background(), s.tx, r, plan); err != nil {
		t.Fatal(err)
	}
	writes := s.writes
	requireCode(t, c.ApplyMountConfigurationInTx(context.Background(), s.tx, r, plan), f.Forbidden)
	if s.writes != writes {
		t.Fatal("missing same-Tx witness wrote")
	}
	if err = c.ApplyMountConfigurationInTx(context.WithValue(context.Background(), witnessKey{}, o), s.tx, r, plan); err != nil {
		t.Fatal(err)
	}
}
func runnerMountBefore(t *testing.T, r ac.MountConfigurationChange) ac.AgentConfig {
	t.Helper()
	at, _ := f.NewInstant(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
	v, err := ac.NewAgentConfig(ac.AgentConfigFields{Core: ac.AgentCore{ID: r.AgentID, ProjectID: r.ProjectID, Name: "mount-agent", NormalizedName: "mount-agent", ModelRef: testID[modelc.Model](t, 10), ApprovalPolicy: ac.ApprovalDefault, Lifecycle: ac.AgentActive, Version: r.ResultOwnerVersion, CreatedAt: at, UpdatedAt: at}, AllowedMountIDs: r.After, AllowedToolIDs: []i.ToolID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{}})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRunnerMountPlanningApplyAndStableRemoval(t *testing.T) {
	c, s, _, o, runners, change, request := runnerMountFixture(t)
	bindings := runnerMountPlan(t, c, s, request)
	if len(bindings) != 2 || bindings[0].Workspace != "source" || len(bindings[1].Workspace) != 255 || len(runners.ids) != 1 || len(s.definitions) != 0 || len(s.mappings) != 0 {
		t.Fatal("declarations reordered, truncated, not deduplicated for Runner validation, or materialized during prepare")
	}
	change = runnerMountChange(t, change, bindings)
	runnerMountApply(t, c, s, o, change)
	if len(s.definitions) != 2 || len(s.mappings) != 2 || !slices.Equal(s.head.ids, change.After) {
		t.Fatal("final definitions/mappings/references incomplete")
	}
	for _, b := range bindings {
		d := s.definitions[b.MountID]
		if d.Name != b.MountID.String() || d.Workspace != b.Workspace || s.mappings[b.MountID].binding != b {
			t.Fatal("stable identities or full workspace lost")
		}
	}
	before := runnerMountBefore(t, change)
	version := f.Version(1)
	command, _ := ac.AgentCommandIdentity(change.ProjectID, ac.UpdateAgentCommand, "retain")
	request.Command, request.ExpectedOwnerVersion, request.Before = command, &version, &before
	// Declaration order affects the original intent but not sorted allowlist.
	slices.Reverse(request.Declarations)
	same := runnerMountPlan(t, c, s, request)
	if same[0] != bindings[1] || same[1] != bindings[0] {
		t.Fatal("existing tuple got fresh IDs")
	}
	update := change.Clone()
	update.Command = command
	update.ExpectedOwnerVersion = &version
	update.Before = change.After
	update = runnerMountChange(t, update, same)
	writes := s.writes
	runnerMountApply(t, c, s, o, update)
	if s.writes != writes {
		t.Fatal("unchanged configuration wrote definitions or references")
	}
	items, err := c.RunnerMountBindingsInTx(context.Background(), s.tx, change.Actor, change.ProjectID, change.AgentID, 1, change.After)
	if err != nil || len(items) != 2 {
		t.Fatal("current complete safe read", err)
	}
	command, _ = ac.AgentCommandIdentity(change.ProjectID, ac.UpdateAgentCommand, "clear")
	clear := update.Clone()
	clear.Command = command
	clear.ResultOwnerVersion = 2
	clear = runnerMountChange(t, clear, []ac.RunnerMountBinding{})
	runnerMountApply(t, c, s, o, clear)
	if len(s.head.ids) != 0 || len(s.definitions) != 2 || len(s.mappings) != 2 {
		t.Fatal("unreference removed durable identity")
	}
	before = runnerMountBefore(t, clear)
	version = 2
	request.Before = &before
	request.ExpectedOwnerVersion = &version
	request.Command, _ = ac.AgentCommandIdentity(change.ProjectID, ac.UpdateAgentCommand, "re-add")
	again := runnerMountPlan(t, c, s, request)
	if !slices.Equal(same, again) {
		t.Fatal("re-add failed to reuse original tuple identities")
	}
}

func TestRunnerMountPlanningAndFinalRejection(t *testing.T) {
	t.Run("complete-union-and-current-authority", func(t *testing.T) {
		c, s, p, _, runners, _, r := runnerMountFixture(t)
		locks, err := c.RunnerMountPlanningLocks(r)
		if err != nil {
			t.Fatal(err)
		}
		s.locks = slices.DeleteFunc(slices.Clone(locks), func(r f.LockRequest) bool {
			return strings.HasPrefix(r.Key.Canonical(), "system-config:")
		})
		_, err = c.PlanRunnerMountsInTx(context.Background(), s.tx, r)
		requireCode(t, err, f.Forbidden)
		s.locks = locks
		p.reject = true
		_, err = c.PlanRunnerMountsInTx(context.Background(), s.tx, r)
		requireCode(t, err, f.Forbidden)
		p.reject = false
		runners.deny = true
		_, err = c.PlanRunnerMountsInTx(context.Background(), s.tx, r)
		requireCode(t, err, f.NotFound)
		if s.writes != 0 {
			t.Fatal("rejected planner wrote")
		}
	})
	t.Run("planning-locks-precede-canonical-before", func(t *testing.T) {
		c, s, _, _, _, _, r := runnerMountFixture(t)
		version := f.Version(1)
		r.ExpectedOwnerVersion = &version
		r.Command, _ = ac.AgentCommandIdentity(r.ProjectID, ac.UpdateAgentCommand, "update-before")
		locks, err := c.RunnerMountPlanningLocks(r)
		if err != nil || len(locks) == 0 {
			t.Fatal("lock planning incorrectly requires the unread preimage", err)
		}
		s.locks = locks
		_, err = c.PlanRunnerMountsInTx(context.Background(), s.tx, r)
		if err == nil || s.writes != 0 {
			t.Fatal("actual planning accepted an absent canonical preimage")
		}
	})
	t.Run("frozen-binding-and-current-runner", func(t *testing.T) {
		c, s, _, _, runners, change, r := runnerMountFixture(t)
		bindings := runnerMountPlan(t, c, s, r)
		change = runnerMountChange(t, change, bindings)
		plan, err := c.DiscoverMountConfiguration(context.Background(), change)
		if err != nil {
			t.Fatal(err)
		}
		altered := change.Clone()
		(*altered.RunnerMounts)[0].Workspace = "changed"
		requireCode(t, c.RequireMountConfigurationInTx(context.Background(), s.tx, altered, plan), f.InvalidArgument)
		runners.deny = true
		requireCode(t, c.RequireMountConfigurationInTx(context.Background(), s.tx, change, plan), f.NotFound)
		if s.writes != 0 {
			t.Fatal("final rejection wrote")
		}
	})
	t.Run("legacy-adoption-and-ambiguity", func(t *testing.T) {
		c, s, _, o, _, change, r := runnerMountFixture(t)
		r.Declarations = r.Declarations[:1]
		id := testID[i.Mount](t, 20)
		s.definitions[id] = definition{ID: id, Project: r.ProjectID, Agent: r.AgentID, Runner: r.Declarations[0].RunnerID, Name: "original alias", Workspace: r.Declarations[0].Workspace, Lifecycle: "active", Version: 7}
		bindings := runnerMountPlan(t, c, s, r)
		if bindings[0].MountID != id {
			t.Fatal("legacy identity not adopted")
		}
		change = runnerMountChange(t, change, bindings)
		runnerMountApply(t, c, s, o, change)
		if s.definitions[id].Name != "original alias" || s.definitions[id].Version != 7 || s.mappings[id].binding != bindings[0] {
			t.Fatal("legacy definition rewritten")
		}
		duplicate := s.definitions[id]
		duplicate.ID = testID[i.Mount](t, 21)
		s.definitions[duplicate.ID] = duplicate
		_, err := c.RunnerMountBindingsInTx(context.Background(), s.tx, r.Actor, r.ProjectID, r.AgentID, 1, change.After)
		requireCode(t, err, f.InvalidState)
		delete(s.definitions, duplicate.ID)
		delete(s.mappings, id)
		items, err := c.RunnerMountBindingsInTx(context.Background(), s.tx, r.Actor, r.ProjectID, r.AgentID, 1, change.After)
		requireCode(t, err, f.DependencyUnbound)
		if items != nil {
			t.Fatal("partial mapping published")
		}
	})
}
