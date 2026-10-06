package secret

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type metadataUnitStore struct {
	*lookupUnitStore
	ref      sc.CredentialRef
	deadline time.Time
}

func (s *metadataUnitStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.lookupUnitStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *metadataUnitStore) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	s.queries++
	if !strings.Contains(sql, "SELECT purpose,version,current_payload_id::text FROM agenteam_secret.secrets WHERE id=$1 AND scope=$2 AND scope_key=$3") || len(args) != 3 || args[0] != s.ref.Details().ID.String() || args[1] != string(s.ref.Details().Scope.Details().Kind) || args[2] != scopeKey(s.ref.Details().Scope) {
		s.t.Fatal("metadata read changed full scope or read material")
	}
	var ok bool
	s.deadline, ok = ctx.Deadline()
	if !ok || time.Until(s.deadline) > 3*time.Second {
		s.t.Fatal("missing read budget")
	}
	return s.row
}

type metadataProject struct {
	sc.ProjectAuthority
	read func(context.Context, f.Tx, id.Actor, id.ProjectID, id.AccessIntent) (id.AccessGrant, error)
}

func (p *metadataProject) AuthorizeProject(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID, intent id.AccessIntent) (id.AccessGrant, error) {
	return p.read(ctx, tx, actor, project, intent)
}

func metadataFixture(t *testing.T, project bool) (*Service, *metadataUnitStore, id.Actor, sc.CredentialRef) {
	t.Helper()
	s, base, request := lookupFixture(t)
	scope := id.SystemScope()
	if project {
		key, _ := f.NewID[id.Project]()
		scope, _ = id.InProject(key)
	}
	key, _ := f.NewID[sc.Credential]()
	ref, _ := sc.NewCredentialRef(key, scope)
	payload, _ := f.NewID[payloadMarker]()
	store := &metadataUnitStore{lookupUnitStore: base, ref: ref}
	store.row = lookupRow{values: []any{"model", int64(7), payload.String()}}
	s.state().store = store
	if project {
		s.state().auth.System = lookupNilSystem(nil)
		s.state().auth.Projects = &metadataProject{read: func(_ context.Context, tx f.Tx, actor id.Actor, key id.ProjectID, intent id.AccessIntent) (id.AccessGrant, error) {
			if tx != store.tx || intent != id.Read || key.String() != scope.Details().ProjectID {
				t.Fatal("Project read left original transaction")
			}
			now, _ := f.NewInstant(time.Now())
			return id.NewAccessGrant(actor, scope, intent, now, 1)
		}}
	}
	return s, store, request.Actor, ref
}

func TestSecretMetadataScopeLocksAndOnlyCommittedCurrentValue(t *testing.T) {
	for _, project := range []bool{false, true} {
		for _, state := range []f.CommitState{f.Committed, f.Unknown, f.NotCommitted} {
			t.Run(map[bool]string{false: "system", true: "project"}[project]+"/"+string(state), func(t *testing.T) {
				s, store, actor, ref := metadataFixture(t, project)
				store.result = state
				out, err := s.Metadata(context.Background(), actor, ref)
				if state == f.Committed {
					if err != nil || !out.CredentialRef.Equal(ref) || out.Purpose != sc.Model || out.Version != 7 {
						t.Fatal("current metadata", err)
					}
				} else if err == nil || !reflect.DeepEqual(out, sc.Metadata{}) {
					t.Fatal("unconfirmed value escaped")
				}
				user, _ := f.UserLock(actor.Details().UserID)
				credential, _ := f.AggregateLock(f.CredentialRefAggregate, ref.Details().ID.String())
				want := []f.LockRequest{{Key: user, Mode: f.Shared}, {Key: credential, Mode: f.Shared}}
				if project {
					key, _ := f.ProjectLock(ref.Details().Scope.Details().ProjectID)
					want = append(want, f.LockRequest{Key: key, Mode: f.Shared})
				}
				if !lookupSameLocks(store.locks, want) || store.acquires != 1 || store.held != 1 || store.queries != 1 {
					t.Fatal("incomplete metadata lock union")
				}
				if len(s.state().nonces) != 0 || s.state().initialized || s.state().epoch != 0 {
					t.Fatal("read consumed write state")
				}
			})
		}
	}
}

func TestSecretMetadataValidationAndAuthorityPrecedeSQL(t *testing.T) {
	s, _, actor, ref := metadataFixture(t, false)
	for _, zero := range []*Service{nil, {}, {data: func() *serviceState { return nil }}} {
		_, err := zero.Metadata(context.Background(), actor, sc.CredentialRef{})
		projectAuditCode(t, err, f.InvalidArgument)
		_, err = zero.Metadata(context.Background(), actor, ref)
		projectAuditCode(t, err, f.DependencyUnbound)
	}
	_, err := s.Metadata(nil, actor, ref)
	projectAuditCode(t, err, f.InvalidArgument)
	for _, branch := range []string{"actor", "store", "sessions", "scope", "revoked-before-scope", "acquire", "executor", "held", "grant", "not-found", "bad-version", "bad-payload"} {
		t.Run(branch, func(t *testing.T) {
			s, store, actor, ref := metadataFixture(t, false)
			code := f.DependencyUnbound
			switch branch {
			case "actor":
				actor = id.Actor{}
				code = f.Forbidden
			case "store":
				var missing *metadataUnitStore
				s.state().store = missing
			case "sessions":
				s.state().auth.Sessions = lookupNilSessions(nil)
			case "scope":
				s.state().auth.System = lookupNilSystem(nil)
			case "revoked-before-scope":
				s.state().auth.System = lookupNilSystem(nil)
				s.state().auth.Sessions = lookupSessions(func(context.Context, f.Tx, id.Actor) error { return f.NewFault(f.SessionRevoked, f.NotStarted) })
				code = f.SessionRevoked
			case "acquire":
				store.acquireErr = f.NewFault(f.ResourceBusy, f.NotStarted)
				code = f.ResourceBusy
			case "executor":
				store.inTxErr = f.NewFault(f.InvalidArgument, f.NotStarted)
				code = f.InvalidArgument
			case "held":
				store.heldErr = f.NewFault(f.InvalidState, f.NotStarted)
				code = f.InvalidState
			case "grant":
				s.state().auth.System = lookupSystem(func(context.Context, f.Tx, id.Actor, id.AccessIntent) (id.AccessGrant, error) {
					return id.AccessGrant{}, nil
				})
				code = f.DependencyUnavailable
			case "not-found":
				store.row = lookupRow{err: pgx.ErrNoRows}
				code = f.NotFound
			case "bad-version":
				store.row = lookupRow{values: []any{"model", int64(0), ref.Details().ID.String()}}
				code = f.DependencyUnavailable
			case "bad-payload":
				store.row = lookupRow{values: []any{"model", int64(1), "invalid"}}
				code = f.DependencyUnavailable
			}
			out, err := s.Metadata(context.Background(), actor, ref)
			projectAuditCode(t, err, code)
			if !reflect.DeepEqual(out, sc.Metadata{}) {
				t.Fatal("failure retained candidate")
			}
			if branch != "not-found" && branch != "bad-version" && branch != "bad-payload" && store.queries != 0 {
				t.Fatal("metadata before authority")
			}
		})
	}
}

func TestSecretMetadataBudgetAndCommittedCancellation(t *testing.T) {
	s, store, actor, ref := metadataFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	parent, _ := ctx.Deadline()
	if _, err := s.Metadata(ctx, actor, ref); err != nil || !store.deadline.Equal(parent) {
		t.Fatal("read extended caller budget", err)
	}
	s, store, actor, ref = metadataFixture(t, false)
	store.afterCommit = cancel
	out, err := s.Metadata(ctx, actor, ref)
	var ff *f.Fault
	if !errors.As(err, &ff) || ff.Code != f.DependencyUnavailable || ff.CommitState != f.Committed || !reflect.DeepEqual(out, sc.Metadata{}) {
		t.Fatal("post-commit cancellation published metadata", err)
	}
}
