//go:build integration

package security_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

func masterKeys(t *testing.T, current int64, versions ...int64) secret.Keyring {
	t.Helper()
	type key struct {
		Version string `json:"version"`
		Key     string `json:"key_b64"`
	}
	wire := struct {
		Format  int    `json:"format"`
		Current string `json:"current_version"`
		Keys    []key  `json:"keys"`
	}{Format: 1, Current: fmt.Sprint(current)}
	for _, v := range versions {
		material := make([]byte, 32)
		for i := range material {
			material[i] = byte(v*32 + int64(i))
		}
		wire.Keys = append(wire.Keys, key{fmt.Sprint(v), base64.StdEncoding.EncodeToString(material)})
	}
	raw, _ := json.Marshal(wire)
	result, err := secret.LoadKeyring(string(raw), auditKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

type secretFixture struct {
	*auditFixture
	secret *secret.Service
	usage  *secretAuthority
}

func newSecretFixture(t *testing.T) *secretFixture {
	t.Helper()
	f := newAuditFixture(t)
	_, err := f.store.Exec(auditContext(t), `CREATE TABLE audit_fixture.secret_bindings(owner_id uuid PRIMARY KEY,credential_id uuid NOT NULL,consumer text NOT NULL,owner_kind text NOT NULL,active boolean NOT NULL DEFAULT true,retained boolean NOT NULL DEFAULT false,mcp_valid boolean NOT NULL DEFAULT true);`)
	if err != nil {
		t.Fatal(err)
	}
	auth := &secretAuthority{auditAuthority: f.auth}
	s, err := secret.New(f.store, masterKeys(t, 1, 1), f.service, secret.Authorizations{Sessions: f.auth, System: f.auth, Projects: auth, Usage: auth})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(auditContext(t)); err != nil {
		t.Fatal(err)
	}
	return &secretFixture{f, s, auth}
}

type secretAuthority struct{ *auditAuthority }

func (a *secretAuthority) CheckMutationInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef) error {
	e, err := a.executor(tx)
	if err != nil {
		return err
	}
	var state string
	err = e.QueryRow(ctx, `SELECT state FROM audit_fixture.projects WHERE id=$1`, ref.Details().Scope.Details().ProjectID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && state != "active" {
		return deny(foundation.ProjectNotActive)
	}
	return err
}
func (a *secretAuthority) CheckCleanupInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, cause sc.LifecycleCause, id identity.ProjectID) error {
	e, err := a.executor(tx)
	if err != nil {
		return err
	}
	c := cause.Details()
	var allowed bool
	err = e.QueryRow(ctx, `SELECT state='deleting' AND stopped AND operation_id=$2 AND version=$3 FROM audit_fixture.projects WHERE id=$1`, id.String(), c.OperationID.String(), int64(c.ProjectVersion)).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !allowed {
		return deny(foundation.InvalidState)
	}
	return err
}
func (a *secretAuthority) CheckReferenceInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, consumer sc.Purpose, owner string, retain bool) error {
	if actor.Details().Kind == identity.Human {
		if err := a.RequireCurrentSession(ctx, tx, actor); err != nil {
			return err
		}
		if ref.Details().Scope.Details().Kind == identity.ProjectScope {
			project, _ := foundation.ParseID[identity.Project](ref.Details().Scope.Details().ProjectID)
			if _, err := a.AuthorizeProject(ctx, tx, actor, project, identity.Mutate); err != nil {
				return err
			}
			if retain {
				if err := a.CheckMutationInTx(ctx, tx, actor, ref); err != nil {
					return err
				}
			}
		} else {
			if _, err := a.AuthorizeSystem(ctx, tx, actor, identity.Mutate); err != nil {
				return err
			}
		}
	} else if retain || actor.Details().Kind != identity.Service || actor.Details().ServiceName != identity.SecretService || actor.Details().CauseRef != owner || actor.Details().ProjectID != ref.Details().Scope.Details().ProjectID {
		return deny(foundation.Forbidden)
	}
	e, err := a.executor(tx)
	if err != nil {
		return err
	}
	var allowed bool
	err = e.QueryRow(ctx, `SELECT credential_id=$2 AND consumer=$3 AND (active OR NOT $4) FROM audit_fixture.secret_bindings WHERE owner_id=$1`, owner, ref.Details().ID.String(), string(consumer), retain).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !allowed {
		return deny(foundation.Forbidden)
	}
	return err
}
func (a *secretAuthority) AuthorizeLeaseInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	e, err := a.executor(tx)
	if err != nil {
		return sc.UseGrant{}, err
	}
	if actor.Details().Kind != identity.Service || actor.Details().ServiceName != identity.SecretService || actor.Details().CauseRef != owner.Details().ID || actor.Details().ProjectID != ref.Details().Scope.Details().ProjectID {
		return sc.UseGrant{}, deny(foundation.Forbidden)
	}
	var id, purpose, kind string
	var active, retained, mcpValid bool
	err = e.QueryRow(ctx, `SELECT credential_id::text,consumer,owner_kind,active,retained,mcp_valid FROM audit_fixture.secret_bindings WHERE owner_id=$1`, owner.Details().ID).Scan(&id, &purpose, &kind, &active, &retained, &mcpValid)
	if errors.Is(err, pgx.ErrNoRows) {
		return sc.UseGrant{}, deny(foundation.Forbidden)
	}
	if err != nil {
		return sc.UseGrant{}, err
	}
	if id != ref.Details().ID.String() || kind != string(owner.Details().Kind) {
		return sc.UseGrant{}, deny(foundation.Forbidden)
	}
	if action != sc.ReleaseLease {
		if ref.Details().Scope.Details().Kind == identity.ProjectScope {
			if err = a.CheckMutationInTx(ctx, tx, actor, ref); err != nil {
				return sc.UseGrant{}, err
			}
		}
		if action == sc.AcquireLease && !active || action == sc.ReadLease && !(active || retained) || purpose == string(sc.MCP) && !mcpValid {
			return sc.UseGrant{}, deny(foundation.Forbidden)
		}
	}
	return sc.UseGrant{Subject: actor, Consumer: sc.Purpose(purpose)}, nil
}
func (f *secretFixture) request(t *testing.T, kind sc.MutationKind, value []byte) sc.WriteRequest {
	t.Helper()
	key := foundation.IdempotencyKey(newID[struct{}](t).String())
	command, err := foundation.NewCommandIdentity("secret", []string{f.project.String(), f.actor.Details().UserID}, string(kind), key)
	if err != nil {
		t.Fatal(err)
	}
	r := sc.WriteRequest{Actor: f.actor, Scope: f.scope, Identity: command, Kind: kind, Purpose: sc.Model}
	if kind != sc.Delete {
		r.Value, err = sc.NewSecretMaterial(value)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(r.Value.Destroy)
	}
	return r
}
func (f *secretFixture) create(t *testing.T, value []byte) sc.MutationResult {
	t.Helper()
	r, err := f.secret.ExecuteWrite(auditContext(t), f.request(t, sc.Create, value))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (f *secretFixture) bind(t *testing.T, ref sc.CredentialRef, purpose sc.Purpose) (sc.CredentialLeaseOwner, identity.Actor) {
	t.Helper()
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, newID[struct{}](t).String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.Exec(auditContext(t), `INSERT INTO audit_fixture.secret_bindings(owner_id,credential_id,consumer,owner_kind) VALUES($1,$2,$3,$4)`, owner.Details().ID, ref.Details().ID.String(), string(purpose), string(owner.Details().Kind)); err != nil {
		t.Fatal(err)
	}
	registration, _ := identity.RegisterService(identity.SecretService)
	actor, err := registration.Actor(owner.Details().ID, ref.Details().Scope)
	if err != nil {
		t.Fatal(err)
	}
	return owner, actor
}
func (f *secretFixture) acquire(t *testing.T, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, actor identity.Actor) sc.CredentialLease {
	t.Helper()
	var lease sc.CredentialLease
	r := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		var err error
		lease, err = f.secret.AcquireCredentialLeaseInTx(ctx, tx, actor, ref, owner)
		return err
	})
	if r.State() != foundation.Committed {
		t.Fatal(r.Fault())
	}
	return lease
}
func readMaterial(t *testing.T, s *secret.Service, actor identity.Actor, id sc.LeaseID) []byte {
	t.Helper()
	material, err := s.ReadCredentialForRequest(auditContext(t), actor, id)
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	var value []byte
	if err = material.Use(func(v []byte) error { value = append([]byte(nil), v...); return nil }); err != nil {
		t.Fatal(err)
	}
	return value
}
