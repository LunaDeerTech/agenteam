//go:build integration

package project_test

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestLifecycleAuthorityPersistedCauseMatrix(t *testing.T) {
	f := newR3Fixture(t)
	for _, action := range []c.LifecycleAction{c.Archive, c.Delete} {
		for _, state := range []c.OperationState{c.OperationAccepted, c.OperationStopping, c.OperationFailed, c.OperationCleaning, c.OperationCompleted} {
			if action == c.Archive && state == c.OperationCleaning || action == c.Delete && state == c.OperationCompleted {
				continue
			}
			t.Run(string(action)+"/"+string(state), func(t *testing.T) {
				o, actor, outboxCause := f.accept(t, action)
				if state != c.OperationAccepted {
					f.phase(t, o, state)
				}
				before := f.snapshot(t, o.ProjectID, f.owner)
				err := f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
					return f.authority.ValidateLifecycleInTx(ctx, tx, actor, r3Cause(o), c.OutboxParticipant, c.StopPhase)
				})
				if state == c.OperationStopping {
					if err != nil {
						t.Fatal(err)
					}
				} else {
					requireCode(t, err, foundation.InvalidState)
				}
				err = f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
					return f.facts.ValidateLifecycleInTx(ctx, tx, actor, r3Cause(o), c.OutboxParticipant, c.CleanupPhase)
				})
				requireCode(t, err, foundation.DependencyUnbound)
				resolved, err := f.facts.ResolveLifecycleActor(ctxFor(t), outboxCause)
				if err != nil || !resolved.Equal(actor) {
					t.Fatal("current actor", err)
				}
				if got := f.snapshot(t, o.ProjectID, f.owner); got != before {
					t.Fatal("authority changed persisted facts/activity")
				}
			})
		}
	}
	for _, kind := range []string{"owner", "gate", "project version", "request version", "request action", "pointer cleared", "new pointer", "uninitialized", "missing participant", "extra participant", "participant version", "failed participant", "digest", "manifest", "live plus receipt"} {
		t.Run(kind, func(t *testing.T) {
			o, actor, _ := f.accept(t, c.Archive)
			f.phase(t, o, c.OperationStopping)
			want := foundation.DependencyUnavailable
			request := r3Cause(o)
			switch kind {
			case "owner":
				f.sql(t, `UPDATE agenteam_project.lifecycle_operations SET owner_user_id=$2 WHERE id=$1`, o.ID.String(), id[identity.User](t).String())
			case "gate":
				f.sql(t, `UPDATE agenteam_project.projects SET lifecycle='active' WHERE id=$1`, o.ProjectID.String())
			case "project version":
				f.sql(t, `UPDATE agenteam_project.projects SET version=version+1 WHERE id=$1`, o.ProjectID.String())
			case "request version":
				request.ProjectVersion++
				want = foundation.Forbidden
			case "request action":
				request.Action = c.Delete
				want = foundation.Forbidden
			case "pointer cleared":
				f.sql(t, `UPDATE agenteam_project.projects SET lifecycle='active',current_lifecycle_operation_id=NULL WHERE id=$1`, o.ProjectID.String())
				want = foundation.Forbidden
			case "new pointer":
				other, _, _ := f.accept(t, c.Archive)
				f.sql(t, `UPDATE agenteam_project.projects SET current_lifecycle_operation_id=$2 WHERE id=$1`, o.ProjectID.String(), other.ID.String())
				want = foundation.Forbidden
			case "uninitialized":
				f.sql(t, `UPDATE agenteam_project.projects SET lifecycle='active',version=1,current_lifecycle_operation_id=NULL,initialized_at=NULL WHERE id=$1`, o.ProjectID.String())
				want = foundation.Forbidden
			case "missing participant":
				f.sql(t, `DELETE FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND participant_name='secret'`, o.ID.String())
			case "extra participant":
				f.sql(t, `INSERT INTO agenteam_project.lifecycle_participants(operation_id,participant_name,contract_version,stop_state,cleanup_state,version) VALUES($1,'skills',1,'required','not_applicable',1)`, o.ID.String())
			case "participant version":
				f.sql(t, `UPDATE agenteam_project.lifecycle_participants SET contract_version=2 WHERE operation_id=$1 AND participant_name='outbox'`, o.ID.String())
			case "failed participant":
				f.sql(t, `UPDATE agenteam_project.lifecycle_participants SET stop_state='failed',safe_reason='operation_failed' WHERE operation_id=$1 AND participant_name='outbox'`, o.ID.String())
				want = foundation.InvalidState
			case "digest":
				f.sql(t, `UPDATE agenteam_project.lifecycle_operations SET manifest_digest=$2 WHERE id=$1`, o.ID.String(), "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
			case "manifest":
				f.sql(t, `UPDATE agenteam_project.lifecycle_operations SET required_manifest='[]' WHERE id=$1`, o.ID.String())
			case "live plus receipt":
				f.sql(t, `INSERT INTO agenteam_project.deletion_receipts(operation_id,deleted_project_id,original_owner_user_id,command_key_hash,request_digest,completed_at) VALUES($1,$2,$3,$4,$4,clock_timestamp())`, o.ID.String(), o.ProjectID.String(), f.owner.Details().UserID, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			}
			err := f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
				return f.facts.ValidateLifecycleInTx(ctx, tx, actor, request, c.OutboxParticipant, c.StopPhase)
			})
			requireCode(t, err, want)
		})
	}
	t.Run("completed version must match", func(t *testing.T) {
		o, _, cause := f.accept(t, c.Archive)
		f.phase(t, o, c.OperationCompleted)
		f.sql(t, `UPDATE agenteam_project.projects SET version=version+1 WHERE id=$1`, o.ProjectID.String())
		_, err := f.facts.ResolveLifecycleActor(ctxFor(t), cause)
		requireCode(t, err, foundation.DependencyUnavailable)
	})
}

func TestLifecycleAuthorityConstructionAndCompatibility(t *testing.T) {
	f := newR3Fixture(t)
	o, actor, cause := f.accept(t, c.Archive)
	f.phase(t, o, c.OperationStopping)
	old := r2Entries(2, false)
	manifest, err := c.NewRequiredManifest(old)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(manifest.Entries())
	digest, _ := manifest.Digest()
	f.sql(t, `UPDATE agenteam_project.lifecycle_operations SET required_manifest=$2::jsonb,manifest_digest=$3 WHERE id=$1`, o.ID.String(), raw, digest.String())
	f.sql(t, `UPDATE agenteam_project.lifecycle_participants SET contract_version=2 WHERE operation_id=$1`, o.ID.String())
	_, err = f.facts.ResolveLifecycleActor(ctxFor(t), cause)
	requireCode(t, err, foundation.DependencyUnbound)
	current, _ := f.registry.Manifest()
	compatible, err := project.NewLifecycleAuthority(f.store, current, old)
	if err != nil {
		t.Fatal(err)
	}
	old[0].ReferenceKinds[0] = "changed" // Construction must have copied this slice.
	err = f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
		return compatible.ValidateLifecycleInTx(ctx, tx, actor, r3Cause(o), c.OutboxParticipant, c.StopPhase)
	})
	if err != nil {
		t.Fatal("retained exact version", err)
	}
	for _, change := range []string{"reference set", "dependency set", "owner"} {
		t.Run(change, func(t *testing.T) {
			entries := r2Entries(2, false)
			switch change {
			case "reference set":
				entries[0].ReferenceKinds = nil
			case "dependency set":
				entries[2].CleanupAfter = entries[2].CleanupAfter[:1]
			case "owner":
				entries[0].OwnerModule = "other"
			}
			a, err := project.NewLifecycleAuthority(f.store, current, entries)
			if err != nil {
				t.Fatal(err)
			}
			_, err = a.ResolveLifecycleActor(ctxFor(t), cause)
			requireCode(t, err, foundation.DependencyUnavailable)
		})
	}
}

type r3ObservedStore struct {
	*postgres.Store
	acquires, transactions int
}

func (s *r3ObservedStore) AcquireAll(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.acquires++
	return s.Store.AcquireAll(ctx, tx, locks)
}
func (s *r3ObservedStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	s.transactions++
	return s.Store.WithinTx(ctx, cause, fn)
}

func TestLifecycleAuthorityLocksAndActors(t *testing.T) {
	f := newR3Fixture(t)
	o, actor, outboxCause := f.accept(t, c.Archive)
	f.phase(t, o, c.OperationStopping)
	observed := &r3ObservedStore{Store: f.raw}
	manifest, _ := f.registry.Manifest()
	a, err := project.NewLifecycleAuthority(observed, manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ended foundation.Tx
	err = f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
		ended = tx
		return a.ValidateLifecycleInTx(ctx, tx, actor, r3Cause(o), c.OutboxParticipant, c.StopPhase)
	})
	if err != nil || observed.acquires != 0 || observed.transactions != 0 {
		t.Fatal("validation acquired or nested a transaction", err)
	}
	requireCode(t, a.ValidateLifecycleInTx(ctxFor(t), ended, actor, r3Cause(o), c.OutboxParticipant, c.StopPhase), foundation.DependencyUnavailable)
	for _, mode := range []string{"missing", "wrong project", "foreign store"} {
		t.Run(mode, func(t *testing.T) {
			store := f.raw
			if mode == "foreign store" {
				store = openStore(t, f.db.Config(t, nil))
			}
			result := store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if mode != "missing" {
					key := o.ProjectID
					if mode == "wrong project" {
						key = id[identity.Project](t)
					}
					if err := store.AcquireAll(ctx, tx, []foundation.LockRequest{r3Lock(key, foundation.Shared)}); err != nil {
						return err
					}
				}
				validation := a.ValidateLifecycleInTx(ctx, tx, actor, r3Cause(o), c.OutboxParticipant, c.StopPhase)
				requireCode(t, validation, foundation.DependencyUnavailable)
				return validation
			})
			// Store poisons the actual transaction on an invalid capability or
			// missing lock, independently of the safe authority-facing error.
			if result.State() != foundation.NotCommitted {
				t.Fatal("invalid transaction committed")
			}
			requireCode(t, result.Fault(), foundation.InternalError)
		})
	}
	admin := f.human(t, "admin-r3", "admin")
	agent, _ := identity.NewAgentRun(o.ProjectID, id[identity.Agent](t), id[identity.Execution](t))
	scope, _ := identity.InProject(o.ProjectID)
	wrongRole, _ := identity.RegisterService(identity.ObjectMaintenance)
	wrong, _ := wrongRole.Actor(o.ID.String(), scope)
	role, _ := identity.RegisterService(identity.ProjectLifecycle)
	wrongCause, _ := role.Actor(id[c.Operation](t).String(), scope)
	for _, bad := range []identity.Actor{f.owner, admin, agent, wrong, wrongCause} {
		err = f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
			return a.ValidateLifecycleInTx(ctx, tx, bad, r3Cause(o), c.OutboxParticipant, c.StopPhase)
		})
		requireCode(t, err, foundation.Forbidden)
	}
	_, err = f.authority.AuthorizeProject(ctxFor(t), foundation.Tx{}, actor, o.ProjectID, identity.Read)
	requireCode(t, err, foundation.Forbidden)
	request, _ := projectOutboxRequest(actor, outboxCause)
	plan, err := f.authority.Discover(ctxFor(t), request)
	if err != nil {
		t.Fatal(err)
	}
	f.phase(t, o, c.OperationFailed)
	err = f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
		return f.authority.ValidateInTx(ctx, tx, request, plan)
	})
	requireCode(t, err, foundation.InvalidState)
	f.sql(t, `UPDATE agenteam_project.projects SET lifecycle='active',current_lifecycle_operation_id=NULL,version=version+1 WHERE id=$1`, o.ProjectID.String())
	_, err = f.authority.ResolveLifecycleActor(ctxFor(t), outboxCause)
	requireCode(t, err, foundation.Forbidden)

	// Real PostgreSQL COMMIT acknowledgement loss must not release an actor.
	for _, committed := range []bool{true, false} {
		t.Run(map[bool]string{true: "resolver unknown commit", false: "resolver unknown rollback"}[committed], func(t *testing.T) {
			_, _, cause := f.accept(t, c.Archive)
			proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", f.db.Fixture.Port), committed)
			u, _ := url.Parse(f.db.Fixture.URL(f.db.Name))
			u.Host = proxy.listener.Addr().String()
			store := openStore(t, f.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
			a, err := project.NewLifecycleAuthority(store, manifest, nil)
			if err != nil {
				t.Fatal(err)
			}
			proxy.armed.Store(true)
			got, err := a.ResolveLifecycleActor(ctxFor(t), cause)
			requireCode(t, err, foundation.CommitUnknown)
			if got.Validate() == nil {
				t.Fatal("unknown returned actor")
			}
			close(proxy.release)
			await(t, proxy.completed)
			got, err = a.ResolveLifecycleActor(ctxFor(t), cause)
			if err != nil || got.Validate() != nil {
				t.Fatal("retry exact cause", err)
			}
		})
	}
}
