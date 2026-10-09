//go:build integration

package account_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type projectWorkIdentityFacts struct {
	RevokedSession, ExpiredSession, RenamedProject, ReusedProject string
	OriginalName, RenamedName                                     string
	PriorExpiry                                                   time.Time
}

// Only the two genuinely issued browser Sessions and the fixture's one main
// Project are admitted. No IPC caller can supply a name, SQL, arbitrary owner,
// Work fact or another Project to this stimulus.
func projectWorkIdentityRequest(mode string, r projectWorkPlanningWebIPC) error {
	if mode != "identity" || (r.Project != "" && r.Project != "main") || r.Resource != "" || r.Domain != "" || r.Text != nil {
		return errors.New("closed Work identity stimulus required")
	}
	switch r.Action {
	case "identity-revoked", "identity-expire":
		_, err := foundation.ParseID[identity.Session](r.Target)
		return err
	case "identity-rename", "identity-reuse-name":
		if r.Target != "" {
			return errors.New("Project identity stimulus has no caller target")
		}
		return nil
	default:
		return errors.New("unknown Work identity stimulus")
	}
}

func (f *projectWorkPlanningWebFixture) identityIPC(ctx context.Context, r projectWorkPlanningWebIPC) map[string]any {
	if err := projectWorkIdentityRequest(f.mode, r); err != nil {
		f.t.Fatal("owned Work identity IPC rejected")
	}
	out := map[string]any{"sequence": r.Sequence}
	facts := &f.identityFacts
	switch r.Action {
	case "identity-revoked":
		if facts.RevokedSession != "" || r.Target == f.ownerActor.Details().SessionID {
			f.t.Fatal("original browser Session revocation already observed or targets preparation Session")
		}
		var revoked bool
		if err := f.store.QueryRow(ctx, `SELECT revoked_at IS NOT NULL AND revoked_reason='logout' FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`, r.Target, f.owner.UserID).Scan(&revoked); err != nil || !revoked {
			f.t.Fatal("genuine browser logout has no matching Owner Session revocation")
		}
		facts.RevokedSession = r.Target
		out["revoked"] = true
	case "identity-expire":
		if facts.ExpiredSession != "" || facts.RevokedSession == "" || r.Target == facts.RevokedSession || r.Target == f.ownerActor.Details().SessionID {
			f.t.Fatal("expiry requires the distinct genuinely logged-in browser Session")
		}
		lock, err := foundation.UserLock(f.owner.UserID)
		if err != nil {
			f.t.Fatal("owned expiry User lock unavailable")
		}
		result := f.store.WithinTx(ctx, cause(f.t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: lock, Mode: foundation.Exclusive}}); err != nil {
				return err
			}
			x, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			var issued, activity, absolute time.Time
			if err := x.QueryRow(ctx, `SELECT issued_at,last_activity_at,absolute_expires_at FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND absolute_expires_at>clock_timestamp() AND last_activity_at+idle_seconds*interval '1 second'>clock_timestamp()`, r.Target, f.owner.UserID).Scan(&issued, &activity, &absolute); err != nil {
				return err
			}
			// Reuse the existing Account expiry stimulus, under the real User EX
			// lock and exact preimage. Authentication itself is never bypassed.
			tag, err := x.Exec(ctx, `UPDATE agenteam_account.sessions SET issued_at=clock_timestamp()-interval '2 days',last_activity_at=clock_timestamp()-interval '1 day',absolute_expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1 AND user_id=$2 AND issued_at=$3 AND last_activity_at=$4 AND absolute_expires_at=$5 AND revoked_at IS NULL`, r.Target, f.owner.UserID, issued, activity, absolute)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return errors.New("exact owned expiry preimage changed")
			}
			facts.PriorExpiry = absolute
			return nil
		})
		if result.State() != foundation.Committed || result.Fault() != nil || facts.PriorExpiry.IsZero() {
			f.t.Fatal("owned browser Session expiry stimulus did not commit")
		}
		facts.ExpiredSession = r.Target
		out["expired"] = true
	case "identity-rename":
		if facts.RenamedProject != "" {
			f.t.Fatal("owned stable-ID rename stimulus already used")
		}
		current := f.setup.setupRequest(ctx, f.ownerClient, http.MethodGet, projectOwnerWebPath+"/"+f.ids["main"], nil, "", false, http.StatusOK)
		facts.OriginalName = httpString(f.t, current, "name")
		name := "work.identity-renamed"
		changed := f.projectOwnerWebFixture.ipc(ctx, projectOwnerWebIPC{Sequence: r.Sequence, Action: "update", Project: "main", Name: &name})
		p, ok := changed["project"].(map[string]any)
		if !ok || p["id"] != f.ids["main"] || p["name"] != name || p["version"] == current["version"] {
			f.t.Fatal("real Project rename did not preserve identity and advance version")
		}
		facts.RenamedProject, facts.RenamedName = f.ids["main"], name
		out["project"] = p
	case "identity-reuse-name":
		if facts.RenamedProject != f.ids["main"] || facts.ReusedProject != "" || facts.OriginalName == "" {
			f.t.Fatal("name reuse requires the completed owned main Project rename")
		}
		// The original Project service/Skills fixture creates the replacement.
		// There is deliberately no Work seed for its new stable Project ID.
		p := f.create(ctx, f.ownerActor, facts.OriginalName)
		if p.ID.String() == facts.RenamedProject {
			f.t.Fatal("old name unexpectedly retained the former Project ID")
		}
		facts.ReusedProject = p.ID.String()
		out["project"] = p
	}
	return out
}

func assertProjectWorkIdentityFacts(t *testing.T, f *projectWorkPlanningWebFixture) {
	t.Helper()
	facts := f.identityFacts
	if facts.RevokedSession == "" || facts.ExpiredSession == "" || facts.RevokedSession == facts.ExpiredSession || facts.PriorExpiry.IsZero() || facts.RenamedProject != f.seeds["main"].ProjectID || facts.ReusedProject == "" || facts.ReusedProject == facts.RenamedProject {
		t.Fatal("actual identity stimuli are incomplete")
	}
	var revoked, expired bool
	if err := f.store.QueryRow(f.ctx, `SELECT revoked_at IS NOT NULL AND revoked_reason='logout' FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`, facts.RevokedSession, f.owner.UserID).Scan(&revoked); err != nil || !revoked {
		t.Fatal("original browser revocation fact missing")
	}
	if err := f.store.QueryRow(f.ctx, `SELECT absolute_expires_at<clock_timestamp() AND absolute_expires_at<$3 AND revoked_at IS NULL FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`, facts.ExpiredSession, f.owner.UserID, facts.PriorExpiry).Scan(&expired); err != nil || !expired {
		t.Fatal("expired browser Session was not kept distinct from logout revocation")
	}
	for _, expected := range []struct{ id, name string }{{facts.RenamedProject, facts.RenamedName}, {facts.ReusedProject, facts.OriginalName}} {
		var name, owner string
		if err := f.store.QueryRow(f.ctx, `SELECT name,owner_user_id::text FROM agenteam_project.projects WHERE id=$1`, expected.id).Scan(&name, &owner); err != nil || name != expected.name || owner != f.owner.UserID {
			t.Fatal("real Project rename/name reuse identity mismatch")
		}
	}
	for _, observed := range f.observations() {
		if observed.Method != http.MethodGet {
			t.Fatal("identity-only browser unexpectedly emitted a Work command")
		}
	}
	for _, count := range f.facts(f.ctx, facts.ReusedProject) {
		if count.(int) != 0 {
			t.Fatal("reused name acquired old Project Work facts")
		}
	}
	var providers int
	if err := f.store.QueryRow(f.ctx, `SELECT count(*) FROM agenteam_model.providers WHERE project_id=$1`, facts.RenamedProject).Scan(&providers); err != nil || providers != 0 {
		t.Fatal("canceled real Model draft unexpectedly acquired a Provider fact")
	}
}

func TestProjectWorkIdentityIPCClosedInputs(t *testing.T) {
	valid := "01900000-0000-7000-8000-000000000001"
	for _, action := range []string{"identity-revoked", "identity-expire", "identity-rename", "identity-reuse-name"} {
		t.Run(action, func(t *testing.T) {
			r := projectWorkPlanningWebIPC{Action: action}
			if action == "identity-revoked" || action == "identity-expire" {
				r.Target = valid
			}
			if err := projectWorkIdentityRequest("identity", r); err != nil {
				t.Fatal("closed valid identity action rejected", err)
			}
			if projectWorkIdentityRequest("read", r) == nil {
				t.Fatal("other browser top admitted identity stimulus")
			}
			for _, changed := range []projectWorkPlanningWebIPC{
				{Action: action, Target: r.Target, Project: "duplicate"},
				{Action: action, Target: r.Target, Resource: "task"},
				{Action: action, Target: r.Target, Domain: "task"},
				{Action: action, Target: r.Target, Text: new(string)},
				{Action: action, Target: "not-a-session"},
			} {
				if projectWorkIdentityRequest("identity", changed) == nil {
					t.Fatal("extra or foreign identity input admitted")
				}
			}
		})
	}
	if projectWorkIdentityRequest("identity", projectWorkPlanningWebIPC{Action: "arbitrary"}) == nil {
		t.Fatal("unregistered identity action admitted")
	}
}
