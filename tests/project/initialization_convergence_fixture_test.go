//go:build integration

package project_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// These are explicitly test-owned Project authority inputs. No Skills schema,
// initializer, Object service/runtime, Human Session or ready HTTP is installed.
// The real Account authority only satisfies NewAuthority's existing dependency;
// this registered-service gate must never borrow its Human authorization route.
type initializationConvergencePG struct {
	db        *pgfixture.Database
	store     *postgres.Store
	other     *postgres.Store
	authority *project.Authority
}

type initializationConvergenceCase struct {
	request c.InitializationRequest
	actor   identity.Actor
	owner   identity.UserID
	initial c.ProjectRef
}

func convergenceOpenStore(t *testing.T, db *pgfixture.Database) *postgres.Store {
	t.Helper()
	store, err := postgres.Open(ctxFor(t), db.Config(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := store.Drain(ctx)
		cancel()
		if err != nil {
			t.Error("convergence Store Drain did not finish", err)
			force, stop := context.WithTimeout(context.Background(), time.Second)
			forceErr := store.ForceClose(force)
			stop()
			if forceErr != nil {
				t.Error("convergence Store ForceClose did not finish", forceErr)
			}
			// A bounded Force return is not a join. Retain the failure above,
			// then actually wait for all Store operations/pool/socket owners.
			// The outer 120s top budget includes this Cleanup; exceeding it
			// fails the owned process instead of reporting a successful join.
			if err := store.Drain(context.Background()); err != nil {
				t.Error("convergence Store terminal Drain failed", err)
			}
		}
	})
	return store
}
func newInitializationConvergencePG(t *testing.T) *initializationConvergencePG {
	t.Helper()
	// newDatabase is only the accepted nonce-owned PG database + real migrator.
	// Do not call newFixture/assemble and their persistent Skills test adapter.
	db := newDatabase(t)
	store := convergenceOpenStore(t, db)
	other := convergenceOpenStore(t, db)
	accountKeys, _ := keys(t) // Accepted pure keyring construction; no runtime.
	accounts, err := account.NewAuthority(store, accountKeys)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts})
	if err != nil {
		t.Fatal(err)
	}
	return &initializationConvergencePG{db: db, store: store, other: other, authority: authority}
}
func convergenceLock(project c.ProjectID, mode foundation.LockMode) foundation.LockRequest {
	key, _ := foundation.ProjectLock(project.String())
	return foundation.LockRequest{Key: key, Mode: mode}
}
func convergenceActor(t *testing.T, request c.InitializationRequest) identity.Actor {
	t.Helper()
	registration, err := identity.RegisterService(identity.ProjectInitialization)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := identity.InProject(request.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(request.CreationID.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return actor
}
func convergenceCommitted(t *testing.T, result foundation.CommitResult) {
	t.Helper()
	if result.State() != foundation.Committed {
		t.Fatal("convergence fixture transaction did not commit", result.State(), result.Fault())
	}
}
func (f *initializationConvergencePG) seed(t *testing.T, state c.CreationState) initializationConvergenceCase {
	t.Helper()
	projectID, creationID, owner := id[identity.Project](t), id[c.Creation](t), id[identity.User](t)
	skillID, eventID := id[c.Skill](t), id[struct{}](t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	created, err := foundation.NewInstant(now)
	if err != nil {
		t.Fatal(err)
	}
	name, description := "Converge-"+projectID.String(), "owned Project authority fixture"
	ref := c.ProjectRef{ID: projectID, OwnerUserID: owner, Name: name, NormalizedName: "converge-" + projectID.String(), Description: description, Lifecycle: c.Active, Version: 1, CreatedAt: created, UpdatedAt: created}
	if err := ref.Validate(); err != nil {
		t.Fatal(err)
	}
	request := c.InitializationRequest{CreationID: creationID, ProjectID: projectID, InitializationKey: foundation.IdempotencyKey("original-" + creationID.String())}
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(projectID, foundation.Exclusive)}); err != nil {
			return err
		}
		x, err := f.store.InTx(tx)
		if err != nil {
			return err
		}
		var initialized, requestName, requestDescription, skill, revision, reason, header, payload, safeResult any
		requestName, requestDescription = name, description
		if state == c.CreationFailed {
			reason = string(c.ReasonOperationFailed)
		}
		if state == c.CreationCompleted {
			initialized, requestName, requestDescription = now, nil, nil
			skill, revision = skillID.String(), int64(1)
			header, payload = []byte(`{}`), []byte(`{}`)
			raw, err := json.Marshal(ref)
			if err != nil {
				return err
			}
			safeResult = raw
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_project.projects(id,owner_user_id,name,normalized_name,description,lifecycle,version,created_at,updated_at,creation_id,initialized_at) VALUES($1,$2,$3,$4,$5,'active',1,$6,$6,$7,$8)`, projectID.String(), owner.String(), name, ref.NormalizedName, description, now, creationID.String(), initialized); err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_project.creations(id,project_id,owner_user_id,command_key,semantic_digest,request_name,request_description,state,initialization_key,protected_skill_id,protected_revision,safe_reason,version,created_at,updated_at,event_id,event_header,event_payload,safe_result) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,1,$13,$13,$14,$15::jsonb,$16::jsonb,$17::jsonb)`, creationID.String(), projectID.String(), owner.String(), "create-"+creationID.String(), "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", requestName, requestDescription, string(state), string(request.InitializationKey), skill, revision, reason, now, eventID.String(), header, payload, safeResult)
		return err
	})
	convergenceCommitted(t, result) // SQL/CHECK acceptance precedes the tested gate.
	return initializationConvergenceCase{request: request, actor: convergenceActor(t, request), owner: owner, initial: ref}
}
func (f *initializationConvergencePG) change(t *testing.T, v initializationConvergenceCase, sql string, args ...any) {
	t.Helper()
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(v.request.ProjectID, foundation.Exclusive)}); err != nil {
			return err
		}
		x, err := f.store.InTx(tx)
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, sql, args...)
		if err == nil && tag.RowsAffected() != 1 {
			return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
		}
		return err
	})
	convergenceCommitted(t, result)
}
func (f *initializationConvergencePG) snapshot(t *testing.T, v initializationConvergenceCase) string {
	t.Helper()
	var raw string
	err := f.store.QueryRow(ctxFor(t), `SELECT jsonb_build_array(
(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE p.id=$1),
(SELECT to_jsonb(c) FROM agenteam_project.creations c WHERE c.id=$2),
(SELECT count(*) FROM agenteam_project.commands),
(SELECT count(*) FROM agenteam_project.work_claims),
(SELECT count(*) FROM agenteam_audit.audit_records),
(SELECT count(*) FROM agenteam_outbox.events),
(SELECT count(*) FROM agenteam_account.users),
(SELECT count(*) FROM agenteam_account.sessions))::text`, v.request.ProjectID.String(), v.request.CreationID.String()).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}
func (f *initializationConvergencePG) check(t *testing.T, v initializationConvergenceCase, want foundation.Code) {
	t.Helper()
	before, called := f.snapshot(t, v), false
	var gateError error
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(v.request.ProjectID, foundation.Exclusive)}); err != nil {
			return err
		}
		called = true
		gateError = f.authority.ValidateInitializationConvergenceInTx(ctx, tx, v.actor, v.request)
		return gateError
	})
	if !called {
		t.Fatal("fixture did not reach convergence gate", result.State(), result.Fault())
	}
	if want == "" {
		if gateError != nil {
			t.Fatal(gateError)
		}
		convergenceCommitted(t, result)
	} else {
		requireCode(t, gateError, want)
		if result.State() != foundation.NotCommitted {
			t.Fatal("rejected gate transaction was not rolled back", result.State())
		}
	}
	if after := f.snapshot(t, v); after != before {
		t.Fatal("convergence changed Project facts or wrote side effects")
	}
}
