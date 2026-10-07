//go:build integration

package model_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type projectCredentialTargetKey struct{}
type projectCredentialTaggedWrites struct {
	sc.HumanWriteCommands
	writes, lookups atomic.Int32
}

func (w *projectCredentialTaggedWrites) ExecuteWrite(ctx context.Context, r sc.WriteRequest) (sc.MutationResult, error) {
	w.writes.Add(1)
	return w.HumanWriteCommands.ExecuteWrite(context.WithValue(ctx, projectCredentialTargetKey{}, r.Identity.Canonical()), r)
}
func (w *projectCredentialTaggedWrites) LookupWriteCommand(ctx context.Context, r sc.WriteCommandLookupRequest) (sc.WriteCommandObservation, error) {
	w.lookups.Add(1)
	return w.HumanWriteCommands.LookupWriteCommand(ctx, r)
}
func TestModelProjectCredentialHTTPUnknown(t *testing.T) {
	projectCredentialTop(t)
	v := newProjectCredentialFixture(t)
	for _, mode := range []string{"prepare_nonce", "final_commit", "final_rollback", "final_pending_revoke"} {
		t.Run(mode, func(t *testing.T) {
			commit := mode != "final_rollback"
			proxy := newProjectUpdateHeldProxy(t, net.JoinHostPort("127.0.0.1", v.db.Fixture.Port), commit)
			var once sync.Once
			release := func() { once.Do(func() { close(proxy.release) }) }
			defer release()
			u, e := url.Parse(v.db.Fixture.URL(v.db.Name))
			if e != nil {
				t.Fatal(e)
			}
			u.Host = proxy.listener.Addr().String()
			raw := openStore(t, v.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
			tracked := &projectUsageHTTPStore{Store: raw}
			service, _ := projectCredentialService(t, v.projectUsageHTTPFixture, tracked)
			port := &projectCredentialTaggedWrites{HumanWriteCommands: service}
			old := v.handler
			v.install(t, port)
			defer func() { v.handler = old }()
			browser := v.login(t, v.ownerBrowser.email)
			key := "unknown-" + mode
			lookup := v.lookup(t, browser, key, sc.Create, "", 0)
			digest, e := cursor.Digest([]byte(lookup.Identity.Canonical()))
			if e != nil {
				t.Fatal(e)
			}
			command, _ := f.CommandLock(lookup.Identity)
			project, _ := f.ProjectLock(v.project.ID.String())
			user, _ := f.UserLock(browser.actor.Details().UserID)
			var armed atomic.Bool
			var armedPID atomic.Int32
			var hookStage, hookSQLState atomic.Value
			hookStage.Store("not-entered")
			hookSQLState.Store("")
			recordHookError := func(stage string, err error) error {
				hookStage.Store(stage)
				var state interface{ SQLState() string }
				if err != nil && errors.As(err, &state) {
					// postgres.Error exposes only its validated five-character SQLSTATE.
					hookSQLState.Store(state.SQLState())
				}
				return err
			}
			tracked.hooks(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
				if ctx.Value(projectCredentialTargetKey{}) != lookup.Identity.Canonical() || armed.Load() {
					return nil
				}
				phase := mode == "prepare_nonce" && c.Kind() == f.RecoveryCause && c.Details().Owner == "secret-nonce" || mode != "prepare_nonce" && c.Kind() == f.CommandsCause && c.Details().Primary.Canonical() == lookup.Identity.Canonical()
				if !phase {
					return nil
				}
				hookStage.Store("target-phase-matched")
				x, e := raw.InTx(tx)
				if e != nil {
					return recordHookError("same-tx-executor", e)
				}
				var pid int32
				if mode == "prepare_nonce" {
					var high int64
					e = x.QueryRow(ctx, `SELECT pg_backend_pid(),nonce_high_water FROM agenteam_secret.secret_master_registry WHERE version=1`).Scan(&pid, &high)
					if e != nil {
						return recordHookError("nonce-high-water", e)
					}
					if high < 1024 {
						return f.NewFault(f.DependencyUnavailable, f.NotStarted)
					}
				} else {
					if e = raw.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: command, Mode: f.Exclusive}, {Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared}}); e != nil {
						return recordHookError("required-command-user-project-locks", e)
					}
					var count int
					e = x.QueryRow(ctx, `SELECT pg_backend_pid(),count(*) FROM agenteam_secret.secret_command_receipts WHERE project_id=$1::uuid AND scope='project' AND scope_key=$2::text AND command_digest=$3 AND mutation_kind='create' AND purpose='model' AND result_version=1 AND NOT deleted`, v.project.ID.String(), v.project.ID.String(), string(digest)).Scan(&pid, &count)
					if e != nil {
						return recordHookError("final-project-safe-receipt", e)
					}
					if count != 1 {
						hookStage.Store("final-project-safe-receipt-count")
						return f.NewFault(f.DependencyUnavailable, f.NotStarted)
					}
					n := uint64(command.AdvisoryKey())
					var owners int
					var unique int32
					e = x.QueryRow(ctx, `SELECT count(*),COALESCE(min(pid),0) FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classid::bigint=$1 AND objid::bigint=$2 AND objsubid=1 AND granted AND mode='ExclusiveLock'`, int64(uint32(n>>32)), int64(uint32(n))).Scan(&owners, &unique)
					if e != nil {
						return recordHookError("unique-command-writer", e)
					}
					if owners != 1 || unique != pid {
						hookStage.Store("unique-command-writer-binding")
						return f.NewFault(f.DependencyUnavailable, f.NotStarted)
					}
				}
				hookStage.Store("target-backend-armed")
				armedPID.Store(pid)
				armed.Store(true)
				proxy.targetPID.Store(pid)
				return nil
			}, nil)
			before := v.facts(t)
			response := v.request(t, browser, "POST", v.collection(), key, projectUpdateJSON(t, map[string]any{"value": "physical-private-material"}))
			t.Logf("target diagnostic phase=%s stage=%s sqlstate=%s status=%d", mode, hookStage.Load(), hookSQLState.Load(), response.status)
			response.problem(t, 503, f.CommitUnknown)
			waitSignal(t, proxy.reached)
			if !armed.Load() || armedPID.Load() != proxy.backendPID.Load() || port.writes.Load() != 1 || port.lookups.Load() != 0 {
				t.Fatal("physical target or no-confirm ownership lost")
			}
			if mode != "prepare_nonce" {
				pendingCtx, cancel := context.WithTimeout(testContext(t), 120*time.Millisecond)
				observed, e := v.writer.LookupWriteCommand(pendingCtx, lookup)
				cancel()
				if e == nil || observed.Observed || observed.Result != nil {
					t.Fatal("pending writer became absence")
				}
			}
			if mode == "final_pending_revoke" {
				logoutDone := make(chan error, 1)
				logoutCtx, cancel := context.WithCancel(testContext(t))
				joined := false
				defer func() {
					release()
					cancel()
					if !joined {
						<-logoutDone
					}
				}()
				go func() {
					logoutDone <- v.core.Logout(logoutCtx, account.LogoutRequest{Actor: browser.actor, Key: f.IdempotencyKey("revoke-" + mode)})
				}()
				managementWaitBlocked(t, &systemHTTPFixture{fixture: v.fixture}, user)
				release()
				waitSignal(t, proxy.completed)
				e = <-logoutDone
				joined = true
				if e != nil {
					t.Fatal(e)
				}
				_, e = v.writer.LookupWriteCommand(testContext(t), lookup)
				requireCode(t, e, f.SessionRevoked)
			} else {
				release()
				waitSignal(t, proxy.completed)
				observed, e := v.writer.LookupWriteCommand(testContext(t), lookup)
				if e != nil {
					t.Fatal("explicit current lookup", e)
				}
				want := mode == "final_commit"
				if observed.Observed != want {
					t.Fatal("physical terminal history", observed.Observed, want)
				}
			}
			tracked.hooks(nil, nil)
			after := v.facts(t)
			if mode == "prepare_nonce" || mode == "final_rollback" {
				if after != before {
					t.Fatal("preparation/rollback leaked canonical receipt Audit")
				}
			} else if after.Canonical != before.Canonical+1 || after.Receipts != before.Receipts+1 || after.Audits != before.Audits+1 || after.Events != before.Events {
				t.Fatal("committed unknown facts incomplete")
			}
			for _, material := range []string{"physical-private-material", browser.cookie, browser.csrf} {
				if strings.Contains(v.logs.text(), material) {
					t.Fatal("Unknown evidence leaked material")
				}
			}
			t.Log("physical target phase", mode, "backend", armedPID.Load(), "HTTP one Execute returned Unknown; private original writer terminal separately released and observed; no raw protocol persisted")
		})
	}
}
