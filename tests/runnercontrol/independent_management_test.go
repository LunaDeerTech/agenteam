//go:build integration

package runnercontrol_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
)

func TestIndependentRunnerManagementConcurrent(t *testing.T) {
	v := newRunnerServiceFixture(t)
	other := v.newRunner(t)
	for _, kind := range []string{"create", "enrollment", "different-intent"} {
		t.Run(kind, func(t *testing.T) {
			in, e := rc.NewCreate(rc.CreateRequest{RunnerID: serviceID[rc.Runner](t), Name: "independent concurrent", Description: "original", Tags: []string{}, RootPath: "/srv/runner"})
			requireServiceOK(t, e, "independent original intent")
			want := independentFacts{1, 1, 1, 1, 1, 1}
			if kind == "enrollment" {
				created, _, _ := v.create(t, "independent reissue")
				in, e = rc.NewEnrollment(created.Target(), rc.CredentialRequest{ExpectedVersion: 1})
				requireServiceOK(t, e, "independent reissue intent")
				want = independentFacts{1, 2, 2, 1, 2, 2}
			}
			second := in
			if kind == "different-intent" {
				second, e = rc.NewCreate(rc.CreateRequest{RunnerID: in.Target(), Name: "different concurrent intent", Description: "original", Tags: []string{}, RootPath: "/srv/runner"})
				requireServiceOK(t, e, "independent changed intent")
			}
			key := serviceKey(t)
			lock := independentCommand(t, v, key, in)
			barrier := independentHold(t, v, lock)
			admin := v.db.Connect(t)
			start := func(s *service.Service, intent rc.Intent) independentCall[independentMutation] {
				return independentStart(t, func(ctx context.Context) independentMutation {
					value, err := s.Execute(ctx, v.actor, key, intent)
					return independentMutation{value, err}
				})
			}
			left, right := start(v.runner, in), start(other, second)
			independentWaiters(t, admin, lock.AdvisoryKey(), barrier.pid, "ExclusiveLock", 2)
			barrier.release(t)
			results := []independentMutation{independentResult(t, left), independentResult(t, right)}
			materials, successes, rejected := 0, 0, 0
			var receipt rc.Receipt
			for _, result := range results {
				if result.err != nil {
					requireServiceCode(t, result.err, f.IdempotencyKeyReused)
					if result.value.Material != nil || result.value.Receipt.CommandID.Validate() == nil {
						t.Fatal("rejected concurrent intent leaked a receipt/material")
					}
					rejected++
					continue
				}
				if result.value.Material != nil {
					if !result.value.Material.Token.Valid() {
						t.Fatal("known first commit returned invalid material")
					}
					materials++
				}
				if successes > 0 && !reflect.DeepEqual(receipt, result.value.Receipt) {
					t.Fatal("concurrent replay changed the committed public receipt")
				}
				receipt = result.value.Receipt
				successes++
			}
			wantSuccess, wantRejected := 2, 0
			if kind == "different-intent" {
				wantSuccess, wantRejected = 1, 1
			}
			if materials != 1 || successes != wantSuccess || rejected != wantRejected || independentCount(t, v, in.Target()) != want {
				t.Fatal("concurrent command did not have exactly one material/public fact set")
			}
			for i, intent := range []rc.Intent{in, second} {
				lookup, err := other.Lookup(migrationContext(t), v.actor, key, intent)
				if results[i].err != nil {
					requireServiceCode(t, err, f.IdempotencyKeyReused)
					continue
				}
				requireServiceOK(t, err, "independent original receipt lookup")
				if lookup.Receipt == nil || !reflect.DeepEqual(*lookup.Receipt, receipt) {
					t.Fatal("concurrent original receipt not durable")
				}
			}
		})
	}
}

func TestIndependentRunnerManagementLogoutOrder(t *testing.T) {
	for _, logoutFirst := range []bool{true, false} {
		name := "runner-before-logout"
		if logoutFirst {
			name = "logout-before-runner"
		}
		t.Run(name, func(t *testing.T) {
			v := newRunnerServiceFixture(t)
			created, originalKey, _ := v.create(t, "independent authorization")
			before := independentCount(t, v, created.Target())
			description := "authorized update"
			in, e := rc.NewUpdate(created.Target(), rc.UpdateRequest{ExpectedVersion: 1, Description: &description})
			requireServiceOK(t, e, "independent update intent")
			key := serviceKey(t)
			barrier, admin := independentAuditBarrier(t, v, created.Target(), logoutFirst)
			user, e := f.UserLock(v.actor.Details().UserID)
			requireServiceOK(t, e, "independent User gate")
			logoutKey := serviceKey(t)
			startLogout := func() independentCall[error] {
				return independentStart(t, func(ctx context.Context) error {
					return v.accounts.Logout(ctx, account.LogoutRequest{Actor: v.actor, Key: logoutKey})
				})
			}
			startMutation := func() independentCall[independentMutation] {
				return independentStart(t, func(ctx context.Context) independentMutation {
					value, err := v.runner.Execute(ctx, v.actor, key, in)
					return independentMutation{value, err}
				})
			}
			if logoutFirst {
				logout := startLogout()
				writer := independentWaiters(t, admin, barrier.key, barrier.pid, "ExclusiveLock", 1)[0]
				mutation := startMutation()
				mutationPID := independentWaiters(t, admin, user.AdvisoryKey(), writer, "ShareLock", 1)[0]
				lookup := independentStart(t, func(ctx context.Context) independentLookup {
					value, err := v.runner.Lookup(ctx, v.actor, originalKey, created)
					return independentLookup{value, err}
				})
				// Canonical ordering acquires this target control gate before
				// runner-management. Lookup must wait here on the original
				// mutation, which is itself still blocked at the User gate.
				control, err := f.SystemConfigLock("runner-control-" + created.Target().String())
				requireServiceOK(t, err, "independent target control gate")
				independentWaiters(t, admin, control.AdvisoryKey(), mutationPID, "ShareLock", 1)
				barrier.release(t)
				requireServiceOK(t, independentResult(t, logout), "actual Logout first")
				mutated, looked := independentResult(t, mutation), independentResult(t, lookup)
				requireServiceCode(t, mutated.err, f.SessionRevoked)
				requireServiceCode(t, looked.err, f.SessionRevoked)
				if mutated.value.Material != nil || mutated.value.Receipt.CommandID.Validate() == nil || looked.value.Receipt != nil || independentCount(t, v, created.Target()) != before {
					t.Fatal("revoked waiter observed a receipt or changed Runner facts")
				}
			} else {
				mutation := startMutation()
				writer := independentWaiters(t, admin, barrier.key, barrier.pid, "ExclusiveLock", 1)[0]
				logout := startLogout()
				independentWaiters(t, admin, user.AdvisoryKey(), writer, "ExclusiveLock", 1)
				barrier.release(t)
				mutated := independentResult(t, mutation)
				requireServiceOK(t, mutated.err, "actual Runner commit first")
				requireServiceOK(t, independentResult(t, logout), "actual Logout after Runner")
				if !mutated.value.Receipt.Changed || mutated.value.Receipt.Runner.Version != 2 || mutated.value.Material != nil || independentCount(t, v, created.Target()) != (independentFacts{1, 2, 1, 1, 1, 2}) {
					t.Fatal("authorized first writer did not commit exactly once")
				}
				_, e = v.runner.Lookup(migrationContext(t), v.actor, key, in)
				requireServiceCode(t, e, f.SessionRevoked)
				fresh := v.login(t)
				replay, e := v.runner.Execute(migrationContext(t), fresh, key, in)
				requireServiceOK(t, e, "same User recovery after ordered revocation")
				if replay.Material != nil || !reflect.DeepEqual(replay.Receipt, mutated.value.Receipt) {
					t.Fatal("new Session changed ordered historical receipt")
				}
			}
		})
	}
}
