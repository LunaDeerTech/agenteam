//go:build integration

package runnercontrol_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
)

func independentProxiedRunner(t *testing.T, v *runnerServiceFixture) (*service.Service, *commitproxy.Proxy) {
	t.Helper()
	p, e := commitproxy.New(migrationContext(t), net.JoinHostPort("127.0.0.1", v.db.Fixture.Port))
	requireServiceOK(t, e, "owned independent COMMIT proxy")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if p.Close(ctx) != nil {
			t.Error("independent COMMIT proxy workers did not join")
		}
	})
	u, e := url.Parse(v.db.Fixture.URL(v.db.Name))
	requireServiceOK(t, e, "owned independent database URL")
	u.Host = p.Address()
	// Only this owned loopback PG protocol stimulus disables TLS. The regular
	// fixture and independent durable reads retain the verified original path.
	store, e := postgres.Open(migrationContext(t), v.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "CA_FILE": ""}))
	requireServiceOK(t, e, "independent proxied Store")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if store.ForceClose(ctx) != nil {
			t.Error("independent proxied Store did not join")
		}
	})
	aa, e := account.NewAuthority(store, v.keys)
	requireServiceOK(t, e, "independent current Account authority")
	ra, e := service.NewAuthority(store, aa)
	requireServiceOK(t, e, "independent Runner authority")
	ck, e := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))))
	requireServiceOK(t, e, "independent fixture cursor key")
	aud, e := audit.New(store, ck, audit.Authorizations{Sessions: aa, System: aa, Accounts: aa, Runners: ra})
	requireServiceOK(t, e, "independent exact typed Audit")
	s, e := service.New(ra, aud)
	requireServiceOK(t, e, "independent proxied Runner")
	t.Cleanup(func() {
		s.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if s.Drain(ctx) != nil {
			t.Error("independent proxied Runner did not join")
		}
	})
	return s, p
}

func TestIndependentRunnerManagementCommitUnknown(t *testing.T) {
	for _, committed := range []bool{true, false} {
		name := "original-writer-commits"
		if !committed {
			name = "original-writer-rolls-back"
		}
		t.Run(name, func(t *testing.T) {
			v := newRunnerServiceFixture(t)
			s, proxy := independentProxiedRunner(t, v)
			in, e := rc.NewCreate(rc.CreateRequest{RunnerID: serviceID[rc.Runner](t), Name: "independent uncertain runner", Description: "original", Tags: []string{}, RootPath: "/srv/runner"})
			requireServiceOK(t, e, "independent uncertain intent")
			key := serviceKey(t)
			command := independentCommand(t, v, key, in)
			barrier, admin := independentAuditBarrier(t, v, in.Target(), false)
			call := independentStart(t, func(ctx context.Context) independentMutation {
				value, err := s.Execute(ctx, v.actor, key, in)
				return independentMutation{value, err}
			})
			writer := independentWaiters(t, admin, barrier.key, barrier.pid, "ExclusiveLock", 1)[0]
			requireServiceOK(t, proxy.Arm(writer), "arm exact original transaction")
			barrier.release(t)
			independentAwait(t, proxy.Reached(), "original complete COMMIT frame")
			if proxy.WriterPID() != writer {
				t.Fatal("COMMIT intercepted a different writer")
			}
			unknown := independentResult(t, call)
			requireServiceCode(t, unknown.err, f.CommitUnknown)
			var fault *f.Fault
			var provenance interface{ CommitResult() f.CommitResult }
			identity, e := in.Identity(v.actor, key)
			requireServiceOK(t, e, "original unknown identity")
			if !errors.As(unknown.err, &fault) || !errors.As(unknown.err, &provenance) {
				t.Fatal("Unknown discarded original commit provenance")
			}
			result := provenance.CommitResult()
			if fault.CommitState != f.Unknown || fault.RetryHint != "lookup" || result.State() != f.Unknown || result.AttemptID().Validate() != nil || fault.CauseID != result.AttemptID().String() || result.Cause().Kind() != f.CommandsCause || result.Cause().Details().Primary.Canonical() != identity.Canonical() {
				t.Fatal("Unknown changed original attempt or command cause")
			}
			if unknown.value.Material != nil || unknown.value.Receipt.CommandID.Validate() == nil || independentCount(t, v, in.Target()) != (independentFacts{}) {
				t.Fatal("uncertain uncommitted command exposed material or facts")
			}
			lookup := independentStart(t, func(ctx context.Context) independentLookup {
				value, err := v.runner.Lookup(ctx, v.actor, key, in)
				return independentLookup{value, err}
			})
			// An actual second backend waits on the still-live original writer's
			// Command lock. A locally returned Unknown is not transaction retirement.
			waiter := independentWaiters(t, admin, command.AdvisoryKey(), writer, "ExclusiveLock", 1)[0]
			if waiter == writer {
				t.Fatal("Lookup reused original transaction")
			}
			if committed {
				proxy.Release()
				independentAwait(t, proxy.Committed(), "actual COMMIT plus idle ReadyForQuery")
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				e = proxy.Close(ctx) // no Release: original COMMIT never reaches PG
				cancel()
				requireServiceOK(t, e, "actual held upstream close")
				select {
				case <-proxy.Committed():
					t.Fatal("rollback control observed a commit")
				default:
				}
			}
			independentAwait(t, proxy.HeldJoined(), "held proxy callback return")
			observed := independentResult(t, lookup)
			requireServiceOK(t, observed.err, "actual original-key confirmation")
			want := independentFacts{1, 1, 1, 1, 1, 1}
			if committed {
				if observed.value.Receipt == nil || independentCount(t, v, in.Target()) != want {
					t.Fatal("late known commit lost exact facts or public receipt")
				}
				replayed, e := v.runner.Execute(migrationContext(t), v.actor, key, in)
				requireServiceOK(t, e, "explicit replay of late known commit")
				if replayed.Material != nil || !reflect.DeepEqual(replayed.Receipt, *observed.value.Receipt) || independentCount(t, v, in.Target()) != want {
					t.Fatal("late confirmation or replay repeated material/facts")
				}
			} else {
				if observed.value.Receipt != nil || independentCount(t, v, in.Target()) != (independentFacts{}) {
					t.Fatal("rolled-back original transaction left facts")
				}
				retried, e := v.runner.Execute(migrationContext(t), v.actor, key, in)
				requireServiceOK(t, e, "explicit same-key retry after confirmed absence")
				if retried.Material == nil || !retried.Material.Token.Valid() || independentCount(t, v, in.Target()) != want {
					t.Fatal("confirmed-absent retry did not commit exactly once")
				}
			}
		})
	}
}
