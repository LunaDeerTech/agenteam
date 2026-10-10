package authorization

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// These are explicitly controlled authority/Store ports. Passing them tests
// ordering and binding only, not a real Agent, Registry, SQL Tx or grant.
type authControl struct {
	tx                        f.Tx
	held                      bool
	policy                    ac.ApprovalPolicy
	executionErr, registryErr error
	steps                     []string
}
type executionPlanControl struct {
	owner   *authControl
	binding tc.ToolCallBinding
}

func (executionPlanControl) RequiredLocks() []f.LockRequest { return nil }
func (c *authControl) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != c.tx || !tx.Valid() {
		return nil, fail(f.InvalidState)
	}
	return nil, nil
}
func (c *authControl) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if tx != c.tx || !c.held || len(locks) != 5 {
		return fail(f.Forbidden)
	}
	for n, v := range locks {
		if n > 0 && f.CompareLockKeys(locks[n-1].Key, v.Key) >= 0 {
			return fail(f.InvalidState)
		}
	}
	c.steps = append(c.steps, "held")
	return nil
}
func (c *authControl) DiscoverToolCall(_ context.Context, b tc.ToolCallBinding) (tc.ToolCallPlan, error) {
	return executionPlanControl{c, b}, nil
}
func (c *authControl) RequireToolCallInTx(_ context.Context, tx f.Tx, b tc.ToolCallBinding, p tc.ToolCallPlan) error {
	plan, ok := p.(executionPlanControl)
	if !ok || plan.owner != c || tx != c.tx || !plan.binding.Actor.Equal(b.Actor) || plan.binding.InputBindingID != b.InputBindingID {
		return fail(f.Forbidden)
	}
	c.steps = append(c.steps, "execution")
	return c.executionErr
}
func (c *authControl) ToolApprovalPolicyInTx(_ context.Context, tx f.Tx, _ tc.ToolCallBinding, p tc.ToolCallPlan) (ac.ApprovalPolicy, error) {
	plan, ok := p.(executionPlanControl)
	if !ok || plan.owner != c || tx != c.tx {
		return "", fail(f.Forbidden)
	}
	c.steps = append(c.steps, "policy")
	return c.policy, nil
}
func (c *authControl) RequireCurrentBuiltinInTx(_ context.Context, tx f.Tx, spec tc.SpecRef, binding tc.BuiltinBinding, scope tc.ScopeResolverID, risk tc.RiskClassifierID) error {
	if tx != c.tx || spec.Validate() != nil || binding.HandlerID != builtin.SkillInstallHandlerID || binding.ContractRevision != 1 || scope != builtin.SkillInstallScopeID || risk != builtin.SkillInstallRiskID {
		return fail(f.Forbidden)
	}
	c.steps = append(c.steps, "registry")
	return c.registryErr
}
func authID[T any](t *testing.T) f.ID[T] {
	t.Helper()
	v, e := f.NewID[T]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func authorizationFixture(t *testing.T) (*Service, *authControl, InstallInput) {
	t.Helper()
	project, agent, execution := authID[id.Project](t), authID[id.Agent](t), authID[id.Execution](t)
	actor, e := id.NewAgentRun(project, agent, execution)
	if e != nil {
		t.Fatal(e)
	}
	tool := authID[id.Tool](t)
	digest := f.Digest("sha256:" + strings.Repeat("a", 64))
	b := tc.ToolCallBinding{Actor: actor, ProjectID: project, AgentID: agent, ExecutionID: execution, RoundID: authID[struct{}](t).String(), SnapshotID: authID[struct{}](t).String(), InputBindingID: authID[struct{}](t).String(), LogicalCallID: authID[mc.Call](t), InvocationID: authID[mc.Invocation](t), CallID: "original", ModelVisibleName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), Spec: tc.SpecRef{ToolID: tool, SpecRevision: 1}, Binding: tc.BuiltinBinding{HandlerID: builtin.SkillInstallHandlerID, ContractRevision: 1}, Input: tc.ToolInputReference{PayloadID: authID[struct{}](t).String(), SHA256: digest, Bytes: 1}, CanonicalArguments: digest}
	pkg, e := builtin.ParseSkillInstall(context.Background(), []byte(`{"mode":"create","source":{"kind":"text_files","files":[{"path":"SKILL.md","utf8_text":"---\nname: Example\ndescription: package\n---\nprivate-authorization-canary\n"}]}}`))
	if e != nil {
		t.Fatal(e)
	}
	call, e := builtin.NewSkillInstallCall(context.Background(), actor, b.Spec, authID[tc.Operation](t).String(), authID[pc.Skill](t), pkg)
	if e != nil {
		t.Fatal(e)
	}
	c := &authControl{tx: f.NewTx(), held: true, policy: ac.ApprovalDefault}
	s, e := New(c, c, c)
	if e != nil {
		t.Fatal(e)
	}
	return s, c, InstallInput{b, call, digest}
}

func TestInstallAuthorizationCurrentFactsAndBoundPlan(t *testing.T) {
	for _, policy := range []ac.ApprovalPolicy{ac.ApprovalDefault, ac.ApprovalAuto, ac.ApprovalAllow} {
		s, c, input := authorizationFixture(t)
		c.policy = policy
		plan, e := s.DiscoverInstall(context.Background(), input)
		if e != nil {
			t.Fatal(e)
		}
		if e = s.AuthorizeInstallInTx(context.Background(), c.tx, input, plan); e != nil {
			t.Fatal(e)
		}
		if strings.Join(c.steps, ",") != "held,execution,registry,policy" {
			t.Fatal("permission ordering lost", c.steps)
		}
		copy := plan.RequiredLocks()
		copy[0].Mode = f.Exclusive
		if plan.RequiredLocks()[0].Mode == f.Exclusive {
			t.Fatal("locks aliased")
		}
	}
	for _, name := range []string{"execution", "registry", "policy", "foreign-plan", "foreign-tx", "changed-binding", "missing-locks"} {
		t.Run(name, func(t *testing.T) {
			s, c, input := authorizationFixture(t)
			plan, e := s.DiscoverInstall(context.Background(), input)
			if e != nil {
				t.Fatal(e)
			}
			tx := c.tx
			switch name {
			case "execution":
				c.executionErr = fail(f.Forbidden)
			case "registry":
				c.registryErr = fail(f.DependencyUnbound)
			case "policy":
				c.policy = "caller-invented"
			case "foreign-plan":
				other, _, _ := authorizationFixture(t)
				plan, e = other.DiscoverInstall(context.Background(), input)
				if e != nil {
					t.Fatal(e)
				}
			case "foreign-tx":
				tx = f.NewTx()
			case "changed-binding":
				input.Binding.InputBindingID = authID[struct{}](t).String()
			case "missing-locks":
				c.held = false
			}
			if e = s.AuthorizeInstallInTx(context.Background(), tx, input, plan); e == nil {
				t.Fatal("unproven current authority allowed")
			}
			if name == "execution" && strings.Contains(strings.Join(c.steps, ","), "registry") {
				t.Fatal("base refusal reached Registry")
			}
			if name == "registry" && strings.Contains(strings.Join(c.steps, ","), "policy") {
				t.Fatal("missing code binding reached policy")
			}
		})
	}
}

func TestInstallAuthorizationDependenciesCancellationAndSafeProjection(t *testing.T) {
	s, c, input := authorizationFixture(t)
	var empty *authControl
	if _, err := New(c, empty, c); err == nil {
		t.Fatal("typed nil execution accepted")
	}
	if _, err := New(c, c, empty); err == nil {
		t.Fatal("typed nil registration accepted")
	}
	plan, err := s.DiscoverInstall(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = s.AuthorizeInstallInTx(ctx, c.tx, input, plan); !errors.Is(err, context.Canceled) || len(c.steps) != 0 {
		t.Fatal("cancelled authorization accessed ports")
	}
	if _, err = s.DiscoverInstall(ctx, input); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled discovery passed")
	}
	if strings.Contains(fmt.Sprintf("%+v", map[string]any{"input": input, "plan": plan}), "private-authorization-canary") {
		t.Fatal("body escaped through recursive formatting")
	}
}
