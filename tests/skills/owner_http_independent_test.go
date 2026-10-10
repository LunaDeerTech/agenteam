//go:build integration

package skill_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// The HTTP boundary must not turn its initially valid Actor into a grant for a
// later P2 transaction. The original read has authenticated before this barrier;
// real Logout commits before that same transaction obtains its current locks.
// Project/Creation/ready and Object retain the fixture's declared limitations.
func TestSkillOwnerReadHTTPIndependentCurrentSession(t *testing.T) {
	skillOwnerHTTPTop(t)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		name := "get_detail"
		if method == http.MethodHead {
			name = "head_detail"
		}
		t.Run(name, func(t *testing.T) {
			v := newSkillOwnerHTTPFixture(t)
			listed, err := v.service.ListSkills(testContext(t), v.ownerBrowser.actor, v.project)
			if err != nil || len(listed) != 1 {
				t.Fatal("real published Skill prerequisite", err)
			}
			path := skillOwnerHTTPPath(v.project, "/"+listed[0].ID.String())
			browser := v.login(t, v.ownerBrowser.email)
			v.request(t, browser, http.MethodGet, path, "", "").want(t, http.StatusOK)

			// A separately opened Store observes committed facts independently
			// of the original read connection and the Account command Store.
			observer := openSkillStore(t, v.db.Config(t, nil))
			facts := &skillOwnerHTTPFixture{skillPG: &skillPG{store: observer}}
			gate, release := skillOwnerHTTPGate()
			defer release()
			type originalRead struct {
				tx  f.Tx
				pid int32
			}
			entered := make(chan originalRead, 1)
			hook := &skillOwnerHTTPReadStore{Store: v.store, result: make(chan f.CommitResult, 1)}
			hook.before = func(ctx context.Context, tx f.Tx) error {
				pid, err := skillOwnerHTTPTxPID(ctx, v.store, tx)
				if err != nil {
					return err
				}
				entered <- originalRead{tx, pid}
				return skillOwnerHTTPWait(ctx, gate)
			}
			skillOwnerHTTPInstall(t, v, skillOwnerHTTPService(t, v, hook))
			replies, readDone := skillOwnerHTTPRun(t, v, skillOwnerHTTPRequest(testContext(t), browser, method, path, "", ""))
			var original originalRead
			select {
			case original = <-entered:
				if original.pid <= 0 || !original.tx.Valid() {
					t.Fatal("original physical read identity missing")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("original authenticated read did not reach its transaction")
			}
			// The held read has no domain locks yet, so this must complete using
			// the normal Account command and its real User EX/transaction path.
			if err = v.core.Logout(testContext(t), account.LogoutRequest{Actor: browser.actor, Key: f.IdempotencyKey(testID[struct{}](t).String())}); err != nil {
				t.Fatal("actual Logout did not commit before current authority", err)
			}
			var revoked bool
			if err = observer.QueryRow(testContext(t), `SELECT EXISTS(SELECT 1 FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NOT NULL AND revoked_reason='logout')`, browser.actor.Details().SessionID, browser.actor.Details().UserID).Scan(&revoked); err != nil || !revoked {
				t.Fatal("independent observer did not see the real revocation", err)
			}
			before := facts.facts(t)
			objectCalls := len(v.seed.objects.steps)
			release()
			response := skillOwnerHTTPReply(t, replies, readDone)
			if response.aborted || response.status != http.StatusUnauthorized {
				t.Fatal("initial HTTP authentication bypassed current Session", response.status, response.aborted)
			}
			if response.header.Get("Content-Type") != "application/problem+json" || response.header.Get("Cache-Control") != "no-store" {
				t.Fatal("revocation did not keep safe Problem headers")
			}
			length, err := strconv.Atoi(response.header.Get("Content-Length"))
			if err != nil || length <= 0 {
				t.Fatal("revocation representation length missing")
			}
			if method == http.MethodHead {
				if len(response.body) != 0 {
					t.Fatal("HEAD leaked a response body after revocation")
				}
			} else {
				body := response.want(t, http.StatusUnauthorized)
				if body["code"] != string(f.SessionRevoked) || body["instance"] != "/api/v1" || body["items"] != nil || body["name"] != nil || body["cause_id"] != nil {
					t.Fatal("original current Session refusal was not projected safely")
				}
			}
			select {
			case result := <-hook.result:
				if result.State() != f.NotCommitted || result.Fault() == nil || result.Fault().Code != f.SessionRevoked {
					t.Fatal("original read transaction refusal or identity changed", result.State(), result.Fault())
				}
			default:
				t.Fatal("HTTP returned before its original transaction result")
			}
			if _, err = v.store.InTx(original.tx); err == nil {
				t.Fatal("original refused transaction remains live")
			}
			if facts.facts(t) != before || len(v.seed.objects.steps) != objectCalls {
				t.Fatal("refused metadata read changed durable facts or used Object")
			}
			// Revoking one Session must not accidentally deny its owner's other
			// current Session; the positive control uses the same detail route.
			v.request(t, v.ownerBrowser, http.MethodGet, path, "", "").want(t, http.StatusOK)
			if facts.facts(t) != before {
				t.Fatal("fresh authorized control changed read-only facts")
			}
		})
	}
}
