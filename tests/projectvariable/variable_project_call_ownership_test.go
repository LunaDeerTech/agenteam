//go:build integration

package projectvariable_test

import (
	"context"
	"os"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func TestVariableProjectCallOwnership(t *testing.T) {
	if path := os.Getenv(variableOwnershipChildEnv); path != "" {
		variableOwnershipChildMain(t, path)
		return
	}
	if !t.Run("committed-lanes-registration-and-exact-row-fence", testVariableOwnershipLanes) {
		return
	}
	if !t.Run("lifecycle-selection-and-original-confirmation", testVariableOwnershipReturns) {
		return
	}
	t.Run("foreign-death-before-original-project-sql-terminal", testVariableOwnershipForeign)
}

func variableOwnershipRun(t *testing.T, v *variableOwnershipFixture, project vc.ProjectID, lane, class string, fn func(context.Context) error) *variableOwnershipCall {
	t.Helper()
	call := variableOwnershipStart(t, &variableOwnershipCall{}, fn)
	variableOwnershipWait(t, call.done)
	if call.err != nil {
		t.Fatal("Variables actual public call", agentOwnershipSafeCode(call.err))
	}
	variableOwnershipRow(t, v.p2.base.raw, call.callID, project.String(), v.process.String(), lane, class, "business_closed")
	return call
}

func testVariableOwnershipLanes(t *testing.T) {
	v := newVariableOwnershipFixture(t)
	p, actor := v.p2.project.ID, v.p2.base.ownerBrowser.actor
	ordinary, secrets := v.pair.Service(), v.pair.SecretService()
	input, secretInput := createInput(t, "OWNED_PLAIN", "owned-nonempty"), secretCreateInput(t, "OWNED_SECRET", []byte("owned-secret-nonempty"))
	createMeta, secretMeta := meta(t, "owned-ordinary-create", nil), meta(t, "owned-secret-create", nil)
	var first vc.VariableMutation
	var firstSecret vc.SecretVariableMutation
	variableOwnershipRun(t, v, p, "ordinary", "mutation", func(ctx context.Context) (err error) {
		first, err = ordinary.CreateVariable(ctx, actor, createMeta, p, input)
		return
	})
	variableOwnershipRun(t, v, p, "secret", "mutation", func(ctx context.Context) (err error) {
		firstSecret, err = secrets.CreateSecretVariable(ctx, actor, secretMeta, p, secretInput)
		return
	})
	if !first.Fields().Changed || first.Fields().Variable.Fields().Version != 1 || !firstSecret.Fields().Changed || firstSecret.Fields().AuditID == nil || firstSecret.Fields().EventID == nil || firstSecret.Fields().Variable.Fields().Version != 1 {
		t.Fatal("Variables nonempty create receipt facts")
	}
	var got vc.Variable
	var gotSecret vc.SecretVariable
	variableOwnershipRun(t, v, p, "ordinary", "read", func(ctx context.Context) (err error) {
		got, err = ordinary.GetVariable(ctx, actor, p, input.Fields().ID)
		return
	})
	variableOwnershipRun(t, v, p, "secret", "read", func(ctx context.Context) (err error) {
		gotSecret, err = secrets.GetSecretVariable(ctx, actor, p, secretInput.Fields().ID)
		return
	})
	if got.Fields().ID != input.Fields().ID || got.Fields().Value != input.Fields().Value || gotSecret.Fields().ID != secretInput.Fields().ID {
		t.Fatal("Variables actual nonempty Get")
	}
	var page f.Page[vc.VariableSummary]
	var secretPage f.Page[vc.SecretVariable]
	variableOwnershipRun(t, v, p, "ordinary", "read", func(ctx context.Context) (err error) {
		page, err = ordinary.ListVariables(ctx, actor, p, f.PageRequest{Limit: 10})
		return
	})
	variableOwnershipRun(t, v, p, "secret", "read", func(ctx context.Context) (err error) {
		secretPage, err = secrets.ListSecretVariables(ctx, actor, p, f.PageRequest{Limit: 10})
		return
	})
	if len(page.Items) != 1 || page.Items[0].Fields().ID != input.Fields().ID || len(secretPage.Items) != 1 || secretPage.Items[0].Fields().ID != secretInput.Fields().ID {
		t.Fatal("Variables actual nonempty typed Lists")
	}
	description := "owned changed metadata"
	version, secretVersion := f.Version(1), f.Version(1)
	update, secretUpdate := updateInput(t, vc.VariableUpdateFields{Description: &description}), secretUpdateInput(t, vc.SecretVariableUpdateFields{Description: &description})
	updateMeta, secretUpdateMeta := meta(t, "owned-ordinary-update", &version), meta(t, "owned-secret-update", &secretVersion)
	var updated vc.VariableMutation
	var updatedSecret vc.SecretVariableMutation
	variableOwnershipRun(t, v, p, "ordinary", "mutation", func(ctx context.Context) (err error) {
		updated, err = ordinary.UpdateVariable(ctx, actor, updateMeta, p, input.Fields().ID, update)
		return
	})
	variableOwnershipRun(t, v, p, "secret", "mutation", func(ctx context.Context) (err error) {
		updatedSecret, err = secrets.UpdateSecretVariable(ctx, actor, secretUpdateMeta, p, secretInput.Fields().ID, secretUpdate)
		return
	})
	if !updated.Fields().Changed || updated.Fields().Variable.Fields().Version != 2 || !updatedSecret.Fields().Changed || updatedSecret.Fields().Variable.Fields().Version != 2 {
		t.Fatal("Variables actual changed Updates")
	}
	query := queryFor(t, actor, createMeta, p, input.Fields().ID, vc.CreateCommand, input)
	secretQuery := secretLookup(t, p, secretInput.Fields().ID, vc.SecretCreateCommand, secretMeta)
	var lookup vc.VariableCommandLookup
	var secretResult vc.SecretVariableCommandLookup
	variableOwnershipRun(t, v, p, "ordinary", "read", func(ctx context.Context) (err error) {
		lookup, err = ordinary.LookupVariableCommand(ctx, actor, query)
		return
	})
	variableOwnershipRun(t, v, p, "secret", "read", func(ctx context.Context) (err error) {
		secretResult, err = secrets.LookupSecretVariableCommand(ctx, actor, secretQuery)
		return
	})
	if lookup.Status() != vc.LookupCommitted || lookup.Receipt() == nil || secretResult.Status() != vc.SecretLookupCommitted || secretResult.Receipt() == nil {
		t.Fatal("Variables actual original receipt Lookup")
	}
	sameReceipt(t, first, *lookup.Receipt())
	sameSecretReceipt(t, firstSecret, *secretResult.Receipt())
	deleteVersion, secretDeleteVersion := f.Version(2), f.Version(2)
	deleteMeta, secretDeleteMeta := meta(t, "owned-ordinary-delete", &deleteVersion), meta(t, "owned-secret-delete", &secretDeleteVersion)
	variableOwnershipRun(t, v, p, "ordinary", "mutation", func(ctx context.Context) error {
		_, err := ordinary.DeleteVariable(ctx, actor, deleteMeta, p, input.Fields().ID)
		return err
	})
	variableOwnershipRun(t, v, p, "secret", "mutation", func(ctx context.Context) error {
		_, err := secrets.DeleteSecretVariable(ctx, actor, secretDeleteMeta, p, secretInput.Fields().ID)
		return err
	})
	var calls, instances, plainMutations, plainReads, secretMutations, secretReads, closed, history, secretHistory, receipts int
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT count(*),count(DISTINCT instance_id),count(*) FILTER(WHERE lane='ordinary' AND class='mutation'),count(*) FILTER(WHERE lane='ordinary' AND class='read'),count(*) FILTER(WHERE lane='secret' AND class='mutation'),count(*) FILTER(WHERE lane='secret' AND class='read'),count(*) FILTER(WHERE phase='business_closed'),(SELECT count(*) FROM agenteam_projectvariable.history WHERE project_id=$1),(SELECT count(*) FROM agenteam_projectvariable.secret_history WHERE project_id=$1),(SELECT count(*) FROM agenteam_secret.project_variable_receipts WHERE project_id=$1) FROM agenteam_projectvariable.project_calls WHERE project_id=$1 AND process_id=$2`, p.String(), v.process.String()).Scan(&calls, &instances, &plainMutations, &plainReads, &secretMutations, &secretReads, &closed, &history, &secretHistory, &receipts)
	if err != nil || calls != 12 || instances != 1 || plainMutations != 3 || plainReads != 3 || secretMutations != 3 || secretReads != 3 || closed != 12 || history != 3 || secretHistory != 3 || receipts != 3 {
		t.Fatal("Variables twelve original calls and durable business facts", agentOwnershipSafeCode(err))
	}
	// Old constructors keep their no-ledger behavior using the same real ports.
	legacy, err := pv.New(v.p2.base.tracked, v.deps.ordinary)
	if err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	legacySecret, err := pv.NewSecret(v.p2.base.tracked, v.deps.secrets)
	if err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	if _, err = legacy.ListVariables(ctxFor(t), actor, p, f.PageRequest{Limit: 10}); err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	if _, err = legacySecret.ListSecretVariables(ctxFor(t), actor, p, f.PageRequest{Limit: 10}); err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	legacy.Stop()
	legacySecret.Stop()
	if err = legacy.Drain(ctxFor(t)); err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	if err = legacySecret.Drain(ctxFor(t)); err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	if err = v.p2.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_projectvariable.project_calls WHERE project_id=$1`, p.String()).Scan(&calls); err != nil || calls != 12 {
		t.Fatal("legacy constructors changed owned ledger")
	}

	for _, lane := range []string{"ordinary", "secret"} {
		plain := createInput(t, "UNCONFIRMED_PLAIN", "nonempty")
		private := secretCreateInput(t, "UNCONFIRMED_SECRET", []byte("nonempty-private"))
		m := meta(t, "unconfirmed-"+lane, nil)
		before := variableOwnershipSnapshot(t, v.p2.base.raw, p, true)
		call := variableOwnershipStart(t, &variableOwnershipCall{registrationUnknown: true, register: variableOwnershipNewBarrier()}, func(ctx context.Context) error {
			if lane == "ordinary" {
				_, err := ordinary.CreateVariable(ctx, actor, m, p, plain)
				return err
			}
			_, err := secrets.CreateSecretVariable(ctx, actor, m, p, private)
			return err
		})
		call.await(t, call.register)
		if call.register.physical.State() != f.Committed {
			t.Fatal("registration did not physically commit")
		}
		variableOwnershipRow(t, v.p2.base.raw, call.callID, p.String(), v.process.String(), lane, "mutation", "running")
		if variableOwnershipSnapshot(t, v.p2.base.raw, p, true) != before {
			t.Fatal("business or nonce ran before registration actual return")
		}
		call.register.release.release()
		variableOwnershipWait(t, call.done)
		requireCode(t, call.err, f.CommitUnknown)
		if variableOwnershipSnapshot(t, v.p2.base.raw, p, true) != before || call.businessCalls != 0 {
			t.Fatal("registration Unknown entered business or D04 nonce")
		}
		variableOwnershipRow(t, v.p2.base.raw, call.callID, p.String(), v.process.String(), lane, "mutation", "business_closed")
	}

	for _, lane := range []string{"ordinary", "secret"} {
		before := variableOwnershipSnapshot(t, v.p2.base.raw, p, false)
		plain := createInput(t, "FENCED_PLAIN", "nonempty")
		private := secretCreateInput(t, "FENCED_SECRET", []byte("nonempty-private"))
		m := meta(t, "late-fence-"+lane, nil)
		point := 1
		if lane == "secret" {
			point = 2
		} // after actual Discover+Prepare, before original Match's RequiredLocks.
		call := variableOwnershipStart(t, &variableOwnershipCall{business: variableOwnershipNewBarrier(), businessAt: point}, func(ctx context.Context) error {
			if lane == "ordinary" {
				_, err := ordinary.CreateVariable(ctx, actor, m, p, plain)
				return err
			}
			_, err := secrets.CreateSecretVariable(ctx, actor, m, p, private)
			return err
		})
		call.await(t, call.business)
		variableOwnershipCloseRow(t, v, call, p, lane, "mutation")
		call.held(t, call.business, false)
		if v.pair.Joined() {
			t.Fatal("terminal row substituted for local original return")
		}
		call.business.release.release()
		variableOwnershipWait(t, call.done)
		requireCode(t, call.err, f.InvalidState)
		// D04 Prepare may reserve technical nonces. They are deliberately not
		// equated with project business, nor asserted rolled back by this fence.
		if variableOwnershipSnapshot(t, v.p2.base.raw, p, false) != before {
			t.Fatal("late terminal-row fence allowed project business")
		}
	}
	t.Log("variable_project_call_ownership actual_public_calls=12 ordinary=6 secret=6 nonempty=true default_ledger_delta=0 registration_unknown_lanes=2 late_row_fixture_lanes=2")
}

func variableOwnershipReport(t *testing.T, pair *pv.ProjectCallOwnership, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, pending int) {
	t.Helper()
	report, err := pair.ObserveStop(ctxFor(t), actor, cause, scope)
	if err != nil || !report.Matches(cause, scope) || report.Details().PendingCalls != pending || report.Details().More || report.Details().RegisteredProjectBusinessJoined != (pending == 0) {
		t.Fatal("Variables exact project-business observation", agentOwnershipSafeCode(err))
	}
}

func testVariableOwnershipReturns(t *testing.T) {
	v := newVariableOwnershipFixture(t)
	actor := v.p2.base.ownerBrowser.actor
	ordinary, secrets := v.pair.Service(), v.pair.SecretService()
	// D04 has not prepared a write in this instance, so this first actual
	// refill executes the original nonce UPDATE in its independent SQL Tx.
	nonceProject := v.project(t, "variable-nonce-return")
	private := secretCreateInput(t, "NONCE_RETURN", []byte("owned-private-nonce-input"))
	nonceMeta := meta(t, "nonce-original-return", nil)
	before := variableOwnershipSnapshot(t, v.p2.base.raw, nonceProject.ID, true)
	nonceCall := variableOwnershipStart(t, &variableOwnershipCall{nonce: variableOwnershipNewBarrier()}, func(ctx context.Context) error {
		_, err := secrets.CreateSecretVariable(ctx, actor, nonceMeta, nonceProject.ID, private)
		return err
	})
	nonceCall.await(t, nonceCall.nonce)
	var noProjectLock bool
	key, _ := f.ProjectLock(nonceProject.ID.String())
	bits := uint64(key.AdvisoryKey())
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND datname=current_database() AND xact_start IS NOT NULL) AND NOT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory' AND classid::bigint=$2 AND objid::bigint=$3 AND objsubid=1)`, nonceCall.nonce.pid, int64(bits>>32), int64(bits&0xffffffff)).Scan(&noProjectLock)
	if err != nil || !noProjectLock {
		t.Fatal("actual nonce Tx / absent Project lock", agentOwnershipSafeCode(err))
	}
	cause, scope, stopActor := v.phase(t, nonceProject, pc.Archive) // Actual P EX succeeds while nonce Tx is still held.
	local, err := v.pair.LocalStopper().RequestStop(ctxFor(t), stopActor, cause, scope)
	requireLocalStop(t, local, err, cause, scope, 1)
	nonceCall.held(t, nonceCall.nonce, true)
	variableOwnershipReport(t, v.pair, stopActor, cause, scope, 1)
	if v.pair.Joined() {
		t.Fatal("Project EX substituted for original D04 Prepare return")
	}
	nonceCall.nonce.release.release()
	variableOwnershipWait(t, nonceCall.done)
	if nonceCall.err == nil || nonceCall.noncePhysical.State() != f.NotCommitted {
		t.Fatal("original canceled nonce Tx did not actually return rollback")
	}
	if variableOwnershipSnapshot(t, v.p2.base.raw, nonceProject.ID, true) != before {
		t.Fatal("canceled nonce preparation published facts")
	}
	variableOwnershipReport(t, v.pair, stopActor, cause, scope, 0)

	p := v.p2.project
	plain := createInput(t, "LIFECYCLE_PLAIN", "nonempty")
	private = secretCreateInput(t, "LIFECYCLE_SECRET", []byte("nonempty-private"))
	pm, sm := meta(t, "lifecycle-owned-plain", nil), meta(t, "lifecycle-owned-secret", nil)
	variableOwnershipRun(t, v, p.ID, "ordinary", "mutation", func(ctx context.Context) error {
		_, err := ordinary.CreateVariable(ctx, actor, pm, p.ID, plain)
		return err
	})
	variableOwnershipRun(t, v, p.ID, "secret", "mutation", func(ctx context.Context) error {
		_, err := secrets.CreateSecretVariable(ctx, actor, sm, p.ID, private)
		return err
	})
	before = variableOwnershipSnapshot(t, v.p2.base.raw, p.ID, true)
	plainMutation := variableOwnershipStart(t, &variableOwnershipCall{business: variableOwnershipNewBarrier(), businessAt: 1, checkpoint: variableOwnershipNewBarrier()}, func(ctx context.Context) error {
		_, err := ordinary.CreateVariable(ctx, actor, pm, p.ID, plain)
		return err
	})
	secretMutation := variableOwnershipStart(t, &variableOwnershipCall{business: variableOwnershipNewBarrier(), businessAt: 1}, func(ctx context.Context) error {
		_, err := secrets.CreateSecretVariable(ctx, actor, sm, p.ID, private)
		return err
	})
	plainRead := variableOwnershipStart(t, &variableOwnershipCall{business: variableOwnershipNewBarrier(), businessAt: 1}, func(ctx context.Context) error {
		_, err := ordinary.GetVariable(ctx, actor, p.ID, plain.Fields().ID)
		return err
	})
	secretRead := variableOwnershipStart(t, &variableOwnershipCall{business: variableOwnershipNewBarrier(), businessAt: 1}, func(ctx context.Context) error {
		_, err := secrets.GetSecretVariable(ctx, actor, p.ID, private.Fields().ID)
		return err
	})
	otherProject := v.project(t, "variable-unselected-project")
	otherRead := variableOwnershipStart(t, &variableOwnershipCall{business: variableOwnershipNewBarrier(), businessAt: 1}, func(ctx context.Context) error {
		_, err := ordinary.ListVariables(ctx, actor, otherProject.ID, f.PageRequest{Limit: 10})
		return err
	})
	for _, call := range []*variableOwnershipCall{plainMutation, secretMutation, plainRead, secretRead, otherRead} {
		call.await(t, call.business)
	}
	cause, scope, stopActor = v.phase(t, p, pc.Archive)
	local, err = v.pair.LocalStopper().RequestStop(ctxFor(t), stopActor, cause, scope)
	requireLocalStop(t, local, err, cause, scope, 2)
	for n, call := range []*variableOwnershipCall{plainMutation, secretMutation, plainRead, secretRead, otherRead} {
		call.held(t, call.business, n < 2)
	}
	variableOwnershipReport(t, v.pair, stopActor, cause, scope, 2)
	plainMutation.business.release.release()
	plainMutation.await(t, plainMutation.checkpoint)
	if plainMutation.checkpoint.physical.State() != f.Committed {
		t.Fatal("actual original return checkpoint COMMIT missing")
	}
	variableOwnershipRow(t, v.p2.base.raw, plainMutation.callID, p.ID.String(), v.process.String(), "ordinary", "mutation", "business_closed")
	variableOwnershipReport(t, v.pair, stopActor, cause, scope, 2) // Disk terminal cannot forget the held original return.
	plainMutation.checkpoint.release.release()
	variableOwnershipWait(t, plainMutation.done)
	if plainMutation.err == nil {
		t.Fatal("stopped original mutation succeeded")
	}
	variableOwnershipReport(t, v.pair, stopActor, cause, scope, 1)
	secretMutation.business.release.release()
	variableOwnershipWait(t, secretMutation.done)
	if secretMutation.err == nil {
		t.Fatal("stopped Secret original mutation succeeded")
	}
	variableOwnershipReport(t, v.pair, stopActor, cause, scope, 0)
	for _, call := range []*variableOwnershipCall{plainRead, secretRead, otherRead} {
		call.held(t, call.business, false)
		call.business.release.release()
		variableOwnershipWait(t, call.done)
		if call.err != nil {
			t.Fatal("Archive changed an unselected original read", agentOwnershipSafeCode(call.err))
		}
	}
	if variableOwnershipSnapshot(t, v.p2.base.raw, p.ID, true) != before {
		t.Fatal("Archive cancellation/observation changed original facts")
	}

	deleted := v.project(t, "variable-delete-reads")
	dp, ds := createInput(t, "DELETE_PLAIN", "nonempty"), secretCreateInput(t, "DELETE_SECRET", []byte("nonempty-private"))
	dpm, dsm := meta(t, "delete-plain-seed", nil), meta(t, "delete-secret-seed", nil)
	variableOwnershipRun(t, v, deleted.ID, "ordinary", "mutation", func(ctx context.Context) error {
		_, err := ordinary.CreateVariable(ctx, actor, dpm, deleted.ID, dp)
		return err
	})
	variableOwnershipRun(t, v, deleted.ID, "secret", "mutation", func(ctx context.Context) error {
		_, err := secrets.CreateSecretVariable(ctx, actor, dsm, deleted.ID, ds)
		return err
	})
	reads := []*variableOwnershipCall{
		variableOwnershipStart(t, &variableOwnershipCall{business: variableOwnershipNewBarrier(), businessAt: 1}, func(ctx context.Context) error {
			_, err := ordinary.GetVariable(ctx, actor, deleted.ID, dp.Fields().ID)
			return err
		}),
		variableOwnershipStart(t, &variableOwnershipCall{business: variableOwnershipNewBarrier(), businessAt: 1}, func(ctx context.Context) error {
			_, err := secrets.GetSecretVariable(ctx, actor, deleted.ID, ds.Fields().ID)
			return err
		}),
	}
	for _, call := range reads {
		call.await(t, call.business)
	}
	cause, scope, stopActor = v.phase(t, deleted, pc.Delete)
	local, err = v.pair.LocalStopper().RequestStop(ctxFor(t), stopActor, cause, scope)
	requireLocalStop(t, local, err, cause, scope, 2)
	variableOwnershipReport(t, v.pair, stopActor, cause, scope, 2)
	for _, call := range reads {
		call.held(t, call.business, true)
		call.business.release.release()
		variableOwnershipWait(t, call.done)
		if call.err == nil {
			t.Fatal("Delete allowed stopped original read")
		}
	}
	variableOwnershipReport(t, v.pair, stopActor, cause, scope, 0)
	// Only one original completed-business COMMIT loss per lane is selected.
	// Their same-parent confirmations use real bound guards and proxy Stores.
	variableOwnershipConfirm(t, v, "ordinary")
	variableOwnershipConfirm(t, v, "secret")
	t.Log("variable_project_call_ownership archive_mutation_only=true delete_reads=true checkpoint_actual_return=true nonce_project_ex_not_technical_join=true confirmation_lanes=2")
}
