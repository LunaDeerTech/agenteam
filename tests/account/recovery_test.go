//go:build integration

package account_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestAccountResponseRecoveryPreservesActiveUseAndDeletesAfterJoin(t *testing.T) {
	f := newAccount(t)
	password := f.bootstrap(t)
	r, _ := loginRequest(t, f, password, "admin@mail.com")
	first, e := f.service.Login(ctxFor(t), r)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	second, e := f.service.Login(ctxFor(t), r)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	if e = second.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	used := make(chan error, 1)
	go func() {
		used <- first.UseCookie(func(b []byte) error {
			close(entered)
			<-release
			if len(b) == 0 {
				t.Error("active material disappeared")
			}
			return nil
		})
	}()
	<-entered
	// Revoke the real Session while the admitted material callback is still
	// running. Keep every immutable response binding, including expires_at.
	_, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='logout' WHERE id=$1`, first.Session().ID.String())
	if e != nil {
		t.Fatal(e)
	}
	status, e := f.service.Recover(ctxFor(t))
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	if status.Pending < 1 {
		t.Fatal("live use must remain pending")
	}
	var leases, refs, materials int
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_secret.secret_leases WHERE NOT released),(SELECT count(*) FROM agenteam_secret.secret_references),(SELECT count(*) FROM agenteam_secret.secrets)`).Scan(&leases, &refs, &materials); e != nil {
		t.Fatal(e)
	}
	if leases != 1 || refs != 0 || materials != 1 {
		t.Fatalf("live protection: leases=%d refs=%d materials=%d", leases, refs, materials)
	}
	closing, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	if e = first.Close(closing); e == nil {
		t.Fatal("Close falsely joined blocked callback")
	}
	cancel()
	unblock()
	if e = <-used; e != nil {
		t.Fatal(e)
	}
	if e = first.Close(ctxFor(t)); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	if _, e = f.service.Recover(ctxFor(t)); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_secret.secret_leases WHERE NOT released),(SELECT count(*) FROM agenteam_account.response_plans),(SELECT count(*) FROM agenteam_secret.secrets)`).Scan(&leases, &refs, &materials); e != nil {
		t.Fatal(e)
	}
	if leases != 0 || refs != 0 || materials != 0 {
		t.Fatalf("after actual join: leases=%d plans=%d materials=%d", leases, refs, materials)
	}
	var complete, remaining int
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.material_cleanup WHERE phase='completed'),(SELECT count(*) FROM agenteam_account.commands WHERE response_secret_ref IS NOT NULL OR planned_secret_ref IS NOT NULL)`).Scan(&complete, &remaining); e != nil {
		t.Fatal(e)
	}
	if complete != 1 || remaining != 0 {
		t.Fatalf("cleanup checkpoint: complete=%d bindings=%d", complete, remaining)
	}
	if _, e = f.service.Login(ctxFor(t), r); !hasCode(e, foundation.Unauthenticated) && !hasCode(e, foundation.SessionRevoked) {
		t.Fatal("expired replay restored a cookie", e)
	}
}

func TestAccountResponseForceKeepsRealUseRegistered(t *testing.T) {
	f := newAccount(t)
	password := f.bootstrap(t)
	r, _ := loginRequest(t, f, password, "admin@mail.com")
	response, e := f.service.Login(ctxFor(t), r)
	if e != nil {
		t.Fatal(e)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	done := make(chan error, 1)
	go func() { done <- response.UseCookie(func([]byte) error { close(entered); <-release; return nil }) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if e = f.service.Force(ctx); e == nil || f.service.Joined() {
		t.Fatal("force claimed active material had joined", e)
	}
	var active int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE NOT released`).Scan(&active); e != nil || active != 1 {
		t.Fatal("force released live lease", active, e)
	}
	unblock()
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	_ = response.Close(ctxFor(t)) // Shared force budget is already exhausted.
	if !f.service.Joined() {
		t.Fatal("completed local callback not joined")
	}
	if _, e = f.service.Recover(ctxFor(t)); !hasCode(e, foundation.DependencyUnavailable) && !hasCode(e, foundation.ShuttingDown) {
		t.Fatal("late cleanup refreshed force budget", e)
	}
}

func TestAccountResponseReleaseRejectsChangedExactFence(t *testing.T) {
	f := newAccount(t)
	password := f.bootstrap(t)
	r, _ := loginRequest(t, f, password, "admin@mail.com")
	response, e := f.service.Login(ctxFor(t), r)
	if e != nil {
		t.Fatal(e)
	}
	id := response.AttemptID().String()
	if _, e = f.store.Exec(ctxFor(t), `WITH a AS (UPDATE agenteam_account.auth_attempts SET fence=2 WHERE id=$1) UPDATE agenteam_account.response_plans SET fence=2 WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	if e = response.Close(ctxFor(t)); !hasCode(e, foundation.ResourceBusy) {
		t.Fatal("old actual operation released new fence", e, safeFailure(e))
	}
	status, e := f.service.Recover(ctxFor(t))
	if e != nil || status.Pending == 0 {
		t.Fatal("old local join proof reused for new fence", status, e)
	}
	var active int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE NOT released`).Scan(&active); e != nil || active != 1 {
		t.Fatal("changed-fence lease unprotected", active, e)
	}
	if _, e = f.store.Exec(ctxFor(t), `WITH a AS (UPDATE agenteam_account.auth_attempts SET fence=1 WHERE id=$1) UPDATE agenteam_account.response_plans SET fence=1 WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		if _, e = f.service.Recover(ctxFor(t)); e != nil {
			t.Fatal(e, safeFailure(e))
		}
	}
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE NOT released`).Scan(&active); e != nil || active != 0 {
		t.Fatal("restored original fence not released", active, e)
	}
}
