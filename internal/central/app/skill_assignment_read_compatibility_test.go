//go:build integration

package app

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// This isolated control synthesizes only an uncommitted lifecycle selector.
// It proves the narrow historical-read gate, not a retirement producer, HTTP
// deleting lookup or a valid persisted deleting state. All seed is formal;
// the synthetic UPDATE must roll back and restore the exact canonical preimage.
func TestSkillAssignmentDeletingReadCompatibility(t *testing.T) {
	v := newWorkAppFixture(t)
	agent := skillAssignmentAppAgent(t, v)
	path := "/api/v1/projects/" + v.a.String() + "/agents/" + agent.String() + "/skills"
	var initial skillAssignmentHTTPPage
	skillAssignmentAppCall(t, v, http.MethodGet, path, nil, "", 200, &initial)
	if len(initial.Items) != 1 {
		t.Fatal("formal initial grant missing")
	}
	var changed sc.AssignmentMutation
	skillAssignmentAppCall(t, v, http.MethodPost, path+"/assign", map[string]any{"expected_version": f.Version(1), "request": map[string]any{"skill_id": initial.Items[0].SkillID, "enabled": false}}, guardID[struct{}](t).String(), 200, &changed)
	if !changed.Changed || changed.AgentVersion != 2 {
		t.Fatal("formal current Agent version did not advance")
	}
	accounts, err := account.NewAuthority(v.store, v.root.cfg.AccountKeyring())
	workAppCheck(t, "compatibility-account-reader", err)
	projects, err := createProjectUsageAuthority(v.store, accounts)
	workAppCheck(t, "compatibility-project-reader", err)
	agents, err := createAgentConfigurationAuthority(v.store, projects.projects)
	workAppCheck(t, "compatibility-agent-reader", err)
	owner, err := createSkillAssignmentOwner(v.store, agents)
	workAppCheck(t, "compatibility-fixed-reference-owners", err)
	identity, err := f.NewCommandIdentity("project", []string{v.a.String(), agent.String()}, "skill.assign", "deleting-read-compatibility")
	workAppCheck(t, "compatibility-command", err)
	request := ac.SkillAssignmentRequest{Actor: v.actor, ProjectID: v.a, AgentID: agent, Command: identity, ExpectedVersion: 1}
	locks, err := oc.NormalizeLocks(request.RequiredLocks())
	workAppCheck(t, "compatibility-original-locks", err)
	cause, err := f.NewCommandsCause(identity)
	workAppCheck(t, "compatibility-original-cause", err)
	var before, after string
	workAppCheck(t, "compatibility-canonical-preimage", v.admin.QueryRow(v.ctx, `SELECT to_jsonb(a)::text FROM agenteam_agent.agents a WHERE project_id::text=$1 AND id::text=$2`, v.a.String(), agent.String()).Scan(&before))
	rollback := errors.New("synthetic-lifecycle-read-control-rollback")
	readAccepted := false
	stage := "locks"
	result := v.store.WithinTx(v.ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := v.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		stage = "same-store-executor"
		x, err := v.store.InTx(tx)
		if err != nil {
			return err
		}
		stage = "synthetic-exact-row"
		tag, err := x.Exec(ctx, `UPDATE agenteam_agent.agents SET lifecycle='deleting' WHERE project_id=$1 AND id=$2 AND lifecycle='active' AND version=2`, v.a.String(), agent.String())
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errors.New("synthetic-lifecycle-exact-row-missing")
		}
		stage = "initialized-deleting-read"
		ref, err := owner.ReadSkillAssignmentInTx(ctx, tx, request)
		if err != nil {
			return err
		}
		readAccepted = ref.ProjectID == v.a && ref.AgentID == agent && ref.ConfigVersion == 2
		if !readAccepted {
			return errors.New("synthetic-lifecycle-read-scope-mismatch")
		}
		stage = "rollback"
		return rollback
	})
	if result.State() != f.NotCommitted || !readAccepted {
		t.Fatalf("initialized deleting read control failed at %s or synthetic transaction committed", stage)
	}
	workAppCheck(t, "compatibility-rollback-postimage", v.admin.QueryRow(v.ctx, `SELECT to_jsonb(a)::text FROM agenteam_agent.agents a WHERE project_id::text=$1 AND id::text=$2`, v.a.String(), agent.String()).Scan(&after))
	if after != before {
		t.Fatal("synthetic lifecycle escaped rollback")
	}
	v.joined(t)
	t.Log("SKILL_ASSIGNMENT_DELETING_READ isolated_synthetic_selector=true initialized_receipt_real=true historical_expected_accepted=true same_store_original_tx=true rollback_exact_preimage=true not_retirement_or_http_deleting_proof=true")
}
