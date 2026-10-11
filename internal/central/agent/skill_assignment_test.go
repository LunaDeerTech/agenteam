package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type skillAssignmentReferencesControl struct {
	kind c.SkillAssignmentReferenceKind
}

func (p *skillAssignmentReferencesControl) SkillAssignmentReferenceKind() c.SkillAssignmentReferenceKind {
	return p.kind
}
func (*skillAssignmentReferencesControl) SkillAssignmentVersionLocks(r c.SkillAssignmentVersionChange) ([]f.LockRequest, error) {
	return r.Request.RequiredLocks(), r.Validate()
}
func (*skillAssignmentReferencesControl) AdvanceSkillAssignmentVersionInTx(context.Context, f.Tx, c.SkillAssignmentVersionChange) error {
	panic("reference writer must not be reached by rejected Agent owner inputs")
}

func skillAssignmentControls() []c.SkillAssignmentVersionReferences {
	return []c.SkillAssignmentVersionReferences{
		&skillAssignmentReferencesControl{c.SkillAssignmentToolReferences},
		&skillAssignmentReferencesControl{c.SkillAssignmentMountReferences},
		&skillAssignmentReferencesControl{c.SkillAssignmentModelReferences},
		&skillAssignmentReferencesControl{c.SkillAssignmentSecretReferences},
	}
}

func skillAssignmentRequestFixture(t *testing.T) (c.SkillAssignmentRequest, c.AgentConfig) {
	t.Helper()
	in, at := planFixture(t)
	identity, err := f.NewCommandIdentity("project", []string{in.project.String(), in.target.String()}, "skill.assign", "assignment-original-key")
	if err != nil {
		t.Fatal(err)
	}
	before, _, err := planConfiguration(in, nil, at)
	if err != nil {
		t.Fatal(err)
	}
	return c.SkillAssignmentRequest{Actor: in.actor, ProjectID: in.project, AgentID: in.target, Command: identity, ExpectedVersion: 1}, before
}

func TestAgentSkillAssignmentContractAndRequiredOwners(t *testing.T) {
	r, before := skillAssignmentRequestFixture(t)
	if r.Validate() != nil {
		t.Fatal("valid original Skill assignment command rejected")
	}
	locks := r.RequiredLocks()
	if len(locks) != 5 || locks[0].Mode != f.Exclusive || locks[1].Mode != f.Exclusive || locks[2].Mode != f.Shared || locks[3].Mode != f.Exclusive || locks[4].Mode != f.Exclusive {
		t.Fatal("original command, current identity, schedule or Agent write gate omitted")
	}
	locks[0].Mode = f.Shared
	if r.RequiredLocks()[0].Mode != f.Exclusive {
		t.Fatal("caller mutated the lock plan")
	}
	for _, name := range []string{"missing-agent-owner", "reversed-owners", "agent-update", "different-agent", "no-expected"} {
		t.Run(name, func(t *testing.T) {
			bad := r
			switch name {
			case "missing-agent-owner":
				bad.Command, _ = f.NewCommandIdentity("project", []string{r.ProjectID.String()}, "skill.assign", "assignment-original-key")
			case "reversed-owners":
				bad.Command, _ = f.NewCommandIdentity("project", []string{r.AgentID.String(), r.ProjectID.String()}, "skill.assign", "assignment-original-key")
			case "agent-update":
				bad.Command, _ = f.NewCommandIdentity("project", []string{r.ProjectID.String(), r.AgentID.String()}, "agent.update", "assignment-original-key")
			case "different-agent":
				bad.AgentID = commandID[i.Agent](t, "01900000-0000-7000-8000-000000000099")
			default:
				bad.ExpectedVersion = 0
			}
			if bad.Validate() == nil || bad.RequiredLocks() != nil {
				t.Fatal("request changed original owner/command or omitted expected version")
			}
		})
	}
	change := c.SkillAssignmentVersionChange{Request: r, Before: before, ResultVersion: 2}
	if change.Validate() != nil {
		t.Fatal("unchanged configuration version advance rejected")
	}
	bad := change
	bad.Request.ExpectedVersion = 2
	if bad.Validate() == nil {
		t.Fatal("version witness does not bind its exact original preimage")
	}
	for _, value := range []any{r, change} {
		raw, err := json.Marshal(value)
		if err != nil || strings.Contains(string(raw), r.AgentID.String()) || strings.Contains(fmt.Sprintf("%+v", value), "assignment-original-key") {
			t.Fatal("private request leaked through diagnostics")
		}
	}
	a := &Authority{state: &authorityState{}}
	for _, mode := range []string{"missing", "typed-nil", "duplicate", "unknown"} {
		refs := skillAssignmentControls()
		switch mode {
		case "missing":
			refs = refs[:3]
		case "typed-nil":
			var absent *skillAssignmentReferencesControl
			refs[0] = absent
		case "duplicate":
			refs[0] = refs[1]
		default:
			refs[0] = &skillAssignmentReferencesControl{"other"}
		}
		_, err := NewSkillAssignmentAuthority(a, refs)
		requireCode(t, err, f.DependencyUnbound)
	}
	refs := skillAssignmentControls()
	adapter, err := NewSkillAssignmentAuthority(a, refs)
	if err != nil {
		t.Fatal(err)
	}
	refs[0] = nil
	if adapter.references[0] == nil {
		t.Fatal("caller can replace an injected reference owner")
	}
}

// Controlled SQL/Owner boundaries exercise current authorization and the real
// canonical point read. They deliberately stop at NOT_FOUND; initialized
// receipts, actual version/ref mutations and transaction outcomes require PG.
func TestAgentSkillAssignmentCurrentOwnerAndTransaction(t *testing.T) {
	for _, mode := range []string{"foreign-tx", "closed-tx", "missing-lock", "owner-revoked", "read-current", "mutate-current", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			r, before := skillAssignmentRequestFixture(t)
			er, _, grant := executionConfigurationFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := &executionConfigurationStore{t: t, ctx: ctx, tx: f.NewTx(), request: er, locks: r.RequiredLocks(), live: true}
			calls := 0
			projects := &executionConfigurationProjects{check: func(got context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
				calls++
				want := i.Read
				if mode == "mutate-current" {
					want = i.Mutate
				}
				if got != ctx || tx != s.tx || !actor.Equal(r.Actor) || project != r.ProjectID || intent != want {
					t.Fatal("changed original current Owner intent or identity")
				}
				if mode == "owner-revoked" {
					return pc.ProjectAccess{}, fault(f.Forbidden)
				}
				s.providerPassed = true
				return grant, nil
			}}
			a, err := NewAuthority(s, projects)
			if err != nil {
				t.Fatal(err)
			}
			adapter, err := NewSkillAssignmentAuthority(a, skillAssignmentControls())
			if err != nil {
				t.Fatal(err)
			}
			plan := &skillAssignmentPlan{issuer: adapter, request: r, before: before, locks: slices.Clone(s.locks)}
			tx := s.tx
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "closed-tx":
				s.live = false
			case "missing-lock":
				s.lockErr = fault(f.Forbidden)
			case "cancel":
				cancel()
			}
			if mode == "mutate-current" {
				_, _, err = a.skillAssignmentCurrent(ctx, tx, r, s.locks, i.Mutate)
			} else {
				err = adapter.WithSkillAssignmentInTx(ctx, tx, r, plan, func(context.Context, c.AgentRef) (bool, error) {
					t.Fatal("invalid Agent owner reached Skill command inspection")
					return false, nil
				}, func(context.Context, c.AgentRef) error {
					t.Fatal("invalid Agent owner reached Skill write")
					return nil
				})
			}
			switch mode {
			case "read-current", "mutate-current":
				requireCode(t, err, f.NotFound)
				if calls != 1 || s.reads != 1 {
					t.Fatal("current Owner did not precede the sole Agent existence read")
				}
			case "cancel":
				if err != context.Canceled || calls+s.reads != 0 {
					t.Fatal("cancelled caller reached current facts", err)
				}
			default:
				requireCode(t, err, f.Forbidden)
				if s.reads != 0 {
					t.Fatal("unauthorized caller reached Agent SQL")
				}
			}
		})
	}
}

func TestAgentSkillAssignmentVersionRejectsDetachedWitness(t *testing.T) {
	r, before := skillAssignmentRequestFixture(t)
	change := c.SkillAssignmentVersionChange{Request: r, Before: before, ResultVersion: 2}
	a := &Authority{state: &authorityState{}}
	tx := f.NewTx()
	ctx := context.Background()
	requireCode(t, a.RequireSkillAssignmentVersionAppliedInTx(ctx, tx, change), f.Forbidden)
	w := &skillAssignmentVersionWitness{issuer: a.state, tx: tx, change: change}
	w.ctx = context.WithValue(ctx, skillAssignmentVersionKey{}, w)
	// Expired contexts are rejected before touching a Store, as are unrelated
	// Authority/Tx/child contexts even if their public DTO is unchanged.
	requireCode(t, a.RequireSkillAssignmentVersionAppliedInTx(w.ctx, tx, change), f.Forbidden)
	w.active.Store(true)
	requireCode(t, a.RequireSkillAssignmentVersionAppliedInTx(w.ctx, f.NewTx(), change), f.Forbidden)
	other := &Authority{state: &authorityState{}}
	requireCode(t, other.RequireSkillAssignmentVersionAppliedInTx(w.ctx, tx, change), f.Forbidden)
	child := context.WithValue(w.ctx, struct{}{}, true)
	requireCode(t, a.RequireSkillAssignmentVersionAppliedInTx(child, tx, change), f.Forbidden)
	changed := change
	changed.Request.Command, _ = f.NewCommandIdentity("project", []string{r.ProjectID.String(), r.AgentID.String()}, "skill.remove", "assignment-original-key")
	requireCode(t, a.RequireSkillAssignmentVersionAppliedInTx(w.ctx, tx, changed), f.Forbidden)
	w.active.Store(false)
	requireCode(t, a.RequireSkillAssignmentVersionAppliedInTx(w.ctx, tx, change), f.Forbidden)
}
