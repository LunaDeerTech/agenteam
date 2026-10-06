//go:build integration

package project_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type projectMetadataStore struct {
	*postgres.Store
	foreign                       *postgres.Store
	drop                          string
	tx                            foundation.Tx
	locks                         []foundation.LockRequest
	transactions, acquires, reads int
}

func (s *projectMetadataStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	s.transactions++
	return s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error { s.tx = tx; return fn(ctx, tx) })
}
func (s *projectMetadataStore) AcquireAll(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.acquires++
	s.locks = append([]foundation.LockRequest(nil), locks...)
	actual := []foundation.LockRequest{}
	for _, lock := range locks {
		if s.drop != "" && strings.HasPrefix(lock.Key.Canonical(), s.drop) {
			continue
		}
		actual = append(actual, lock)
	}
	return s.Store.AcquireAll(ctx, tx, actual)
}
func (s *projectMetadataStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if s.foreign != nil {
		return s.foreign.InTx(tx)
	}
	x, err := s.Store.InTx(tx)
	if err != nil {
		return nil, err
	}
	return projectMetadataExecutor{x, s}, nil
}

type projectMetadataExecutor struct {
	postgres.SQLExecutor
	store *projectMetadataStore
}

func (x projectMetadataExecutor) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if strings.Contains(q, "FROM agenteam_secret.secrets WHERE id=") {
		x.store.reads++
	}
	return x.SQLExecutor.QueryRow(ctx, q, args...)
}

type projectMetadataSessions struct {
	identity.SessionAuthority
	store *projectMetadataStore
	t     *testing.T
}

func (s projectMetadataSessions) RequireCurrentSession(ctx context.Context, tx foundation.Tx, a identity.Actor) error {
	if tx != s.store.tx || s.store.transactions != 1 || s.store.acquires != 1 {
		s.t.Fatal("Secret Session check left original transaction")
	}
	if err := s.store.Store.RequireHeldLocks(ctx, tx, s.store.locks); err != nil {
		s.t.Fatal("Session before complete metadata locks", err)
	}
	return s.SessionAuthority.RequireCurrentSession(ctx, tx, a)
}

type projectMetadataAuthority struct {
	sc.ProjectAuthority
	store *projectMetadataStore
	t     *testing.T
}

func (a projectMetadataAuthority) AuthorizeProject(ctx context.Context, tx foundation.Tx, actor identity.Actor, project identity.ProjectID, intent identity.AccessIntent) (identity.AccessGrant, error) {
	if tx != a.store.tx || intent != identity.Read {
		a.t.Fatal("Project metadata changed Tx or intent")
	}
	return a.ProjectAuthority.AuthorizeProject(ctx, tx, actor, project, intent)
}

func projectMetadataReader(t *testing.T, f *bindingFixture, store *projectMetadataStore, system bool, project bool) *secret.Service {
	t.Helper()
	_, ck := keys(t)
	key, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))), ck)
	if err != nil {
		t.Fatal(err)
	}
	auth := secret.Authorizations{Sessions: projectMetadataSessions{f.bundle.accounts, store, t}}
	if system {
		auth.System = f.bundle.accounts
	}
	if project {
		auth.Projects = projectMetadataAuthority{f.bundle.delegate, store, t}
	}
	s, err := secret.New(store, key, f.bundle.audit, auth)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func projectMetadataFacts(t *testing.T, f *bindingFixture) [5]int64 {
	t.Helper()
	var out [5]int64
	if err := f.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_secret.secrets),(SELECT count(*) FROM agenteam_secret.secret_command_receipts),(SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='secret'),(SELECT count(*) FROM agenteam_secret.secret_leases),(SELECT nonce_high_water FROM agenteam_secret.secret_master_registry WHERE version=1)`).Scan(&out[0], &out[1], &out[2], &out[3], &out[4]); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProjectSecretMetadataReadCompatibility(t *testing.T) {
	f := newBindingFixture(t)
	credential := f.credential(t)
	admin := f.human(t, "metadata-system-admin", "admin")
	r := f.request(t, admin, sc.Create, "system-metadata")
	r.Scope = identity.SystemScope()
	r.Identity, _ = foundation.NewCommandIdentity("secret", []string{admin.Details().UserID}, "create", foundation.IdempotencyKey(id[struct{}](t).String()))
	system, err := f.bundle.secret.ExecuteWrite(ctxFor(t), r)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("scope-specific-ports-and-complete-lock-union", func(t *testing.T) {
		for _, isProject := range []bool{false, true} {
			store := &projectMetadataStore{Store: f.raw}
			reader := projectMetadataReader(t, f, store, !isProject, isProject)
			actor, ref := admin, system.Metadata.CredentialRef
			if isProject {
				actor, ref = f.owner, credential.Metadata.CredentialRef
			}
			before := projectMetadataFacts(t, f)
			out, err := reader.Metadata(ctxFor(t), actor, ref)
			if err != nil || !out.CredentialRef.Equal(ref) || out.Purpose != sc.Model || out.Version != 1 {
				t.Fatal("same Tx scope metadata", err)
			}
			want := 2
			if isProject {
				want = 3
			}
			if len(store.locks) != want || store.acquires != 1 || store.transactions != 1 || store.reads != 1 {
				t.Fatal("wrong metadata lock/transaction union")
			}
			user, _ := foundation.UserLock(actor.Details().UserID)
			aggregate, _ := foundation.AggregateLock(foundation.CredentialRefAggregate, ref.Details().ID.String())
			keys := map[string]bool{user.Canonical(): true, aggregate.Canonical(): true}
			if isProject {
				p, _ := foundation.ProjectLock(f.project.ID.String())
				keys[p.Canonical()] = true
			}
			for _, lock := range store.locks {
				if !keys[lock.Key.Canonical()] || lock.Mode != foundation.Shared {
					t.Fatal("wrong read lock")
				}
				delete(keys, lock.Key.Canonical())
			}
			if len(keys) != 0 {
				t.Fatal("missing lock")
			}
			if after := projectMetadataFacts(t, f); after != before {
				t.Fatal("metadata changed business records or nonce")
			}
			if reader.Status().Available {
				t.Fatal("read required initialization")
			}
		}
	})
	t.Run("system-admin-does-not-own-project", func(t *testing.T) {
		for _, actor := range []identity.Actor{admin, f.human(t, "metadata-outsider", "user")} {
			out, err := f.bundle.secret.Metadata(ctxFor(t), actor, credential.Metadata.CredentialRef)
			requireCode(t, err, foundation.NotFound)
			if !reflect.DeepEqual(out, sc.Metadata{}) {
				t.Fatal("foreign Project metadata leaked")
			}
		}
	})
	t.Run("missing-lock-or-foreign-store", func(t *testing.T) {
		for _, kind := range []string{"user", "project", "credential", "foreign"} {
			store := &projectMetadataStore{Store: f.raw}
			switch kind {
			case "user":
				key, _ := foundation.UserLock(f.owner.Details().UserID)
				store.drop = key.Canonical()
			case "project":
				key, _ := foundation.ProjectLock(f.project.ID.String())
				store.drop = key.Canonical()
			case "credential":
				key, _ := foundation.AggregateLock(foundation.CredentialRefAggregate, credential.Metadata.CredentialRef.Details().ID.String())
				store.drop = key.Canonical()
			case "foreign":
				store.foreign = openStore(t, f.db.Config(t, nil))
			}
			reader := projectMetadataReader(t, f, store, false, true)
			out, err := reader.Metadata(ctxFor(t), f.owner, credential.Metadata.CredentialRef)
			if err == nil || !reflect.DeepEqual(out, sc.Metadata{}) || store.reads != 0 {
				t.Fatal("bad transaction witness accepted", kind)
			}
		}
	})
	t.Run("revoked-current-session-precedes-project-existence", func(t *testing.T) {
		before := projectMetadataFacts(t, f)
		f.sql(t, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, f.owner.Details().SessionID)
		requireCode(t, f.bundle.accounts.RequireCurrentSession(ctxFor(t), foundation.Tx{}, f.owner), foundation.SessionRevoked)
		for _, ref := range []sc.CredentialRef{credential.Metadata.CredentialRef, func() sc.CredentialRef { r, _ := sc.NewCredentialRef(id[sc.Credential](t), f.scope); return r }()} {
			out, err := f.bundle.secret.Metadata(ctxFor(t), f.owner, ref)
			requireCode(t, err, foundation.SessionRevoked)
			if !reflect.DeepEqual(out, sc.Metadata{}) {
				t.Fatal("revoked metadata escaped")
			}
		}
		if projectMetadataFacts(t, f) != before {
			t.Fatal("rejected metadata changed facts")
		}
	})
	for _, state := range []c.Lifecycle{c.Archiving, c.Archived, c.Deleting} {
		t.Run("original-read-gate-"+string(state), func(t *testing.T) {
			g := newBindingFixture(t)
			credential := g.credential(t)
			g.gate(t, state)
			before := projectMetadataFacts(t, g)
			out, err := g.bundle.secret.Metadata(ctxFor(t), g.owner, credential.Metadata.CredentialRef)
			if state == c.Deleting {
				requireCode(t, err, foundation.ProjectNotActive)
				if !reflect.DeepEqual(out, sc.Metadata{}) {
					t.Fatal("deleting metadata leaked")
				}
			} else if err != nil || out.Version != 1 {
				t.Fatal("read incorrectly required Mutate gate", err)
			}
			if projectMetadataFacts(t, g) != before {
				t.Fatal("read gate changed Secret facts")
			}
		})
	}
}
