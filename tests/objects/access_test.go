//go:build integration

package objects_test

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type measuredStore struct {
	*postgres.Store
	calls atomic.Int64
	mu    sync.Mutex
	batch []foundation.LockRequest
}

func (s *measuredStore) AcquireAll(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.calls.Add(1)
	s.mu.Lock()
	s.batch = append([]foundation.LockRequest(nil), locks...)
	s.mu.Unlock()
	return s.Store.AcquireAll(ctx, tx, locks)
}
func plannedService(t *testing.T, f *fixture, store object.Store, planner oc.AccessPlanner) *object.Service {
	t.Helper()
	backend, err := object.NewBackend(f.config)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := object.OpenSpool(filepath.Join(t.TempDir(), "spool"), id[oc.Process](t))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	auditing, err := audit.New(f.store, keys, audit.Authorizations{Projects: auditAuthority{f.authority}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := object.New(store, backend, spool, auditing, object.Authorizations{Planner: planner, Resources: f.authority, Read: f.authority, Gate: f.authority, Cleanup: f.authority, Leases: f.authority})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		service.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil {
			_ = service.Force(ctx)
			t.Error(err)
		}
	})
	if err = service.Initialize(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	return service
}
func waitAdvisory(t *testing.T, f *fixture, key foundation.LockKey) {
	t.Helper()
	ctx, cancel := context.WithTimeout(contextFor(t), 2*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	hash := uint64(key.AdvisoryKey())
	for {
		var blocked bool
		err := f.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a USING(pid) WHERE a.datname=$1 AND locktype='advisory' AND NOT granted AND classid::bigint=$2 AND objid::bigint=$3 AND objsubid=1)`, f.db.Name, int64(hash>>32), int64(hash&0xffffffff)).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("did not wait for actual selected parent gate")
		}
	}
}
func executionFacts(t *testing.T, f *fixture) (identity.AgentID, identity.ExecutionID) {
	t.Helper()
	agent, execution := id[identity.Agent](t), id[identity.Execution](t)
	f.sql(t, `INSERT INTO object_fixture.agents(id,project_id)VALUES($1,$2)`, agent.String(), f.project.String())
	f.sql(t, `INSERT INTO object_fixture.executions(id,agent_id,project_id)VALUES($1,$2,$3)`, execution.String(), agent.String(), f.project.String())
	return agent, execution
}
func TestObjectAccessActualOwnerAndActorGates(t *testing.T) {
	for _, gate := range []string{"payload_parent_execution", "actor_agent", "actor_execution"} {
		t.Run(gate, func(t *testing.T) {
			f := newFixture(t, false)
			agent, execution := executionFacts(t, f)
			actor := f.actor
			owner := f.owner
			var key foundation.LockKey
			query := "UPDATE object_fixture.executions SET active=false WHERE id=$1"
			target := execution.String()
			if gate == "payload_parent_execution" {
				if owner.Details().ID == execution.String() {
					t.Fatal("fixture must distinguish PayloadID and ExecutionID")
				}
				f.sql(t, `UPDATE object_fixture.owners SET kind='execution_payload',parent_id=$1`, execution.String())
				owner, _ = oc.NewObjectOwner(oc.ExecutionPayload, owner.Details().ID, f.project.String())
				key, _ = foundation.AggregateLock(foundation.ExecutionAggregate, execution.String())
			} else {
				actor, _ = identity.NewAgentRun(f.project, agent, execution)
				if gate == "actor_agent" {
					key, _ = foundation.AgentLock(agent.String())
					query = "UPDATE object_fixture.agents SET active=false WHERE id=$1"
					target = agent.String()
				} else {
					key, _ = foundation.AggregateLock(foundation.ExecutionAggregate, execution.String())
				}
			}
			httpProxy := newStorageProxy(t, f)
			service, _ := f.on(t, f.store, httpProxy.server.URL, nil)
			ctx := contextFor(t)
			held, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			stopped := make(chan foundation.CommitResult, 1)
			cause := cause(t)
			go func() {
				stopped <- f.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
					if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
						return err
					}
					close(held)
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
					e, err := f.store.InTx(tx)
					if err != nil {
						return err
					}
					_, err = e.Exec(ctx, query, target)
					return err
				})
			}()
			select {
			case <-held:
			case <-ctx.Done():
				t.Fatal("parent holder did not start")
			}
			done := make(chan error, 1)
			command := command(t, "real-parent-gate")
			go func() {
				_, err := service.PutObject(ctx, actor, owner, command, "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
				done <- err
			}()
			waitAdvisory(t, f, key)
			if httpProxy.puts.Load() != 0 {
				t.Fatal("storage wrote through held real parent gate")
			}
			once.Do(func() { close(release) })
			if r := <-stopped; r.State() != foundation.Committed {
				t.Fatal(r.Fault())
			}
			select {
			case err := <-done:
				requireCode(t, err, foundation.Forbidden)
			case <-ctx.Done():
				t.Fatal("gate rejection did not finish")
			}
			var uploads int64
			if err := f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.uploads`).Scan(&uploads); err != nil || uploads != 0 {
				t.Fatal("revoked parent reserved a payload", err, uploads)
			}
			if httpProxy.puts.Load() != 0 {
				t.Fatal("revoked parent sent payload")
			}
		})
	}
}
func TestObjectAccessMappingAndNewCommandRequireRecollection(t *testing.T) {
	for _, change := range []string{"parent_mapping", "command_object"} {
		t.Run(change, func(t *testing.T) {
			f := newFixture(t, false)
			_, execution := executionFacts(t, f)
			if change == "parent_mapping" {
				f.sql(t, `UPDATE object_fixture.owners SET kind='execution_payload',parent_id=$1`, execution.String())
				f.owner, _ = oc.NewObjectOwner(oc.ExecutionPayload, f.owner.Details().ID, f.project.String())
			}
			p, err := f.service.PreparePayload(contextFor(t), f.actor, f.owner, "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
			if err != nil {
				t.Fatal(err)
			}
			defer f.service.DiscardPrepared(p)
			cmd := command(t, "discovery-drift")
			plan := ownerPlan(t, f.service, f.actor, f.owner, oc.ReserveAccess, oc.AccessRequestDetails{Command: &cmd, Prepared: p})
			var winner oc.ObjectID
			if change == "parent_mapping" {
				_, other := executionFacts(t, f)
				f.sql(t, `UPDATE object_fixture.owners SET parent_id=$1`, other.String())
			} else {
				winner = f.put(t, string(cmd.IdempotencyKey), "body").Meta.ID
				if winner == plan.Details().Objects[0] {
					t.Fatal("fixture did not produce another actual ObjectID")
				}
			}
			result := plannedTx(f.store, f.service, contextFor(t), cause(t), plan, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
				_, err := f.service.ReserveUploadInTx(ctx, tx, f.actor, f.owner, cmd, p, plan, locked)
				return err
			})
			if result.State() != foundation.NotCommitted {
				t.Fatal("changed plan committed", result.State())
			}
			requireCode(t, result.Fault(), foundation.ResourceBusy)
			var uploads, attempts int64
			if err = f.store.QueryRow(contextFor(t), `SELECT (SELECT count(*) FROM agenteam_object.uploads),(SELECT count(*) FROM agenteam_object.upload_attempts)`).Scan(&uploads, &attempts); err != nil {
				t.Fatal(err)
			}
			expected := int64(0)
			if change == "command_object" {
				expected = 1
			}
			if uploads != expected || attempts != expected {
				t.Fatal("plan drift mutated storage facts", uploads, attempts)
			}
		})
	}
}
func TestObjectAccessMultiplePlansAcquireUnionExactlyOnce(t *testing.T) {
	f := newFixture(t, false)
	a, b := f.put(t, "union-a", "a"), f.put(t, "union-b", "b")
	measured := &measuredStore{Store: f.store}
	service := plannedService(t, f, measured, f.authority)
	measured.calls.Store(0)
	pa := ownerPlan(t, service, f.actor, f.owner, oc.ReleaseAccess, oc.AccessRequestDetails{ObjectID: a.Meta.ID})
	pb := ownerPlan(t, service, f.actor, f.owner, oc.ReleaseAccess, oc.AccessRequestDetails{ObjectID: b.Meta.ID})
	project, _ := foundation.ProjectLock(f.project.String())
	extra := []foundation.LockRequest{{Key: project, Mode: foundation.Exclusive}}
	result := f.store.WithinTx(contextFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		locked, err := service.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{pa, pb}, extra)
		if err != nil {
			return err
		}
		e, err := f.store.InTx(tx)
		if err != nil {
			return err
		}
		var exclusive bool
		hash := uint64(project.AdvisoryKey())
		err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory' AND granted AND mode='ExclusiveLock' AND classid::bigint=$1 AND objid::bigint=$2 AND objsubid=1)`, int64(hash>>32), int64(hash&0xffffffff)).Scan(&exclusive)
		if err != nil {
			return err
		}
		if !exclusive {
			t.Fatal("actual extra Project lock was not exclusive")
		}
		if err = service.ReleaseObjectInTx(ctx, tx, f.actor, f.owner, a.Meta.ID, pa, locked); err != nil {
			return err
		}
		return service.ReleaseObjectInTx(ctx, tx, f.actor, f.owner, b.Meta.ID, pb, locked)
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	if measured.calls.Load() != 1 {
		t.Fatal("InTx added another acquisition", measured.calls.Load())
	}
	measured.mu.Lock()
	actual := append([]foundation.LockRequest(nil), measured.batch...)
	measured.mu.Unlock()
	expected, _ := oc.NormalizeAccessLocks(append(append(pa.Details().Locks, pb.Details().Locks...), extra...))
	if len(actual) != len(expected) {
		t.Fatal("union missing locks")
	}
	for i := range actual {
		if actual[i].Key.Canonical() != expected[i].Key.Canonical() || actual[i].Mode != expected[i].Mode {
			t.Fatal("union changed key/mode")
		}
	}
	var canonical int64
	if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_references WHERE kind='canonical'`).Scan(&canonical); err != nil || canonical != 0 {
		t.Fatal("composed releases did not commit", err, canonical)
	}
}
func TestObjectAccessRejectsUnboundAndForeignTokens(t *testing.T) {
	f := newFixture(t, false)
	stored := f.put(t, "bound-object", "body")
	unbound := plannedService(t, f, f.store, nil)
	_, err := unbound.StatObject(contextFor(t), f.actor, f.owner, stored.Meta.ID)
	requireCode(t, err, foundation.DependencyUnbound)
	requireCode(t, unbound.Recover(contextFor(t)), foundation.DependencyUnbound)
	other := plannedService(t, f, f.store, f.authority)
	plan := ownerPlan(t, f.service, f.actor, f.owner, oc.ReleaseAccess, oc.AccessRequestDetails{ObjectID: stored.Meta.ID})
	samePlan := ownerPlan(t, f.service, f.actor, f.owner, oc.ReleaseAccess, oc.AccessRequestDetails{ObjectID: stored.Meta.ID})
	request := plan.Details().Request
	var ended foundation.Tx
	var old oc.LockedAccess
	first := f.store.WithinTx(contextFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		ended = tx
		var err error
		old, err = f.service.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
		return err
	})
	if first.State() != foundation.Committed {
		t.Fatal(first.Fault())
	}
	if err = f.service.ValidateAccessPlanInTx(contextFor(t), ended, request, plan, old); err == nil {
		t.Fatal("expired Tx token accepted")
	}
	for _, bad := range []string{"other_tx", "other_service", "other_plan", "other_request", "zero_token", "public_constructor", "second_acquire"} {
		t.Run(bad, func(t *testing.T) {
			r := f.store.WithinTx(contextFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if bad == "other_service" {
					_, err := other.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
					return err
				}
				locked, err := f.service.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
				if err != nil {
					return err
				}
				switch bad {
				case "other_tx":
					locked = old
				case "other_plan":
					// A separately discovered identical request is still a different plan.
					// It is discovered before this transaction in the outer test below.
					return f.service.ValidateAccessPlanInTx(ctx, tx, request, samePlan, locked)
				case "other_request":
					_, err := f.service.AttachObjectInTx(ctx, tx, f.actor, f.owner, stored.Meta.ID, plan, locked)
					return err
				case "zero_token":
					locked = oc.LockedAccess{}
				case "public_constructor":
					issuer := oc.NewAccessIssuer()
					fake, err := oc.NewAccessLockPlan(issuer, plan.Details())
					if err != nil {
						return err
					}
					locked, err = oc.NewLockedAccess(issuer, tx, []oc.AccessLockPlan{fake}, nil)
					if err != nil {
						return err
					}
				case "second_acquire":
					_, err = f.service.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
					return err
				}
				return f.service.ReleaseObjectInTx(ctx, tx, f.actor, f.owner, stored.Meta.ID, plan, locked)
			})
			if r.State() != foundation.NotCommitted {
				t.Fatal("invalid plan/token committed", bad, r.State())
			}
		})
	}
	var canonical int64
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_references WHERE kind='canonical'`).Scan(&canonical); err != nil || canonical != 1 {
		t.Fatal("rejected token mutated references", err, canonical)
	}
}
