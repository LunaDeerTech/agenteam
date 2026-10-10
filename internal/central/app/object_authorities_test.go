package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// Only Object's ownership lookup is supplied a row. The actual concrete
// Knowledge/Skill providers must reach their own Store query and reject it;
// these controls do not pretend that a lookup or dispatch grants access.
type rootDispatchStore struct {
	projectUsageRootStore
	outside, inside        oc.OwnerKind
	owner, project, object string
	tx                     f.Tx
	ctx                    context.Context
	lookupErr, providerErr error
	current                bool
	lookups                int
	providers              []oc.OwnerKind
	held                   []f.LockRequest
}

type rootDispatchRow struct {
	store *rootDispatchStore
	kind  oc.OwnerKind
	err   error
}

func (r rootDispatchRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*oc.OwnerKind) = r.kind
	*dest[1].(*string) = r.store.owner
	*dest[2].(*string) = r.store.project
	return nil
}

func (s *rootDispatchStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if ctx != s.ctx || len(args) != 1 || args[0] != s.object {
		panic("dispatch changed original lookup context/object")
	}
	if strings.Contains(query, "FROM agenteam_object.uploads") {
		s.lookups++
		kind := s.outside
		if s.current {
			kind = s.inside
		}
		return rootDispatchRow{s, kind, s.lookupErr}
	}
	if !strings.Contains(query, "FROM agenteam_skill.initializations") {
		panic("unexpected domain query")
	}
	s.providers = append(s.providers, oc.SkillRevision)
	return rootDispatchRow{err: s.providerErr}
}

func (s *rootDispatchStore) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	if ctx != s.ctx || len(args) != 1 || args[0] != s.object || !strings.Contains(query, "FROM agenteam_knowledge.documents") {
		panic("dispatch changed Knowledge request")
	}
	s.providers = append(s.providers, oc.Knowledge)
	return nil, s.providerErr
}

func (s *rootDispatchStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, f.NewFault(f.Forbidden, f.NotCommitted)
	}
	s.current = true
	return s, nil
}

func (s *rootDispatchStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if ctx != s.ctx || tx != s.tx {
		panic("dispatch replaced original context/transaction")
	}
	s.held = append([]f.LockRequest(nil), locks...)
	return nil
}

func TestKnowledgeSkillRootFixedDispatch(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "original")
	request, err := oc.NewMaintenanceAccess(oc.AccessRequestDetails{Operation: oc.InspectAccess, ObjectID: updateRootID[oc.StoredObject](), InstanceID: updateRootID[oc.Process]()})
	if err != nil {
		t.Fatal(err)
	}
	store := &rootDispatchStore{outside: oc.SkillRevision, inside: oc.Knowledge, owner: request.Details().ObjectID.String(), project: request.Details().InstanceID.String(), object: request.Details().ObjectID.String(), tx: f.NewTx(), ctx: ctx, providerErr: errors.New("controlled domain denial")}
	cfg := testConfig(t, "1s")
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	projects, err := createProjectUsage(cfg, store, accounts)
	if err != nil {
		t.Fatal(err)
	}
	authorities, err := createKnowledgeSkillAuthorities(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	// Avatar is only a selection sentinel here. Actual Avatar maintenance and
	// cleanup are exercised by the real default-root composition case.
	avatar := &account.AvatarAuthority{}
	routes, err := newObjectAuthorities(store, avatar, authorities.knowledge, authorities.skills)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind oc.OwnerKind
		want objectOwnerAuthority
	}{{oc.Avatar, avatar}, {oc.Knowledge, authorities.knowledge}, {oc.SkillRevision, authorities.skills}} {
		project := store.project
		if tc.kind == oc.Avatar {
			project = ""
		}
		owner, e := oc.NewObjectOwner(tc.kind, store.owner, project)
		if e != nil {
			t.Fatal(e)
		}
		got, e := routes.owner(owner)
		if e != nil || got != tc.want {
			t.Fatal("fixed owner selected a different concrete provider", tc.kind)
		}
	}
	for _, kind := range []oc.OwnerKind{oc.Artifact, oc.MCPContent, oc.ExecutionPayload, oc.MeetingFile} {
		owner, e := oc.NewObjectOwner(kind, store.owner, store.project)
		if e != nil {
			t.Fatal(e)
		}
		var fault *f.Fault
		if got, e := routes.owner(owner); got != nil || !errors.As(e, &fault) || fault.Code != f.DependencyUnbound {
			t.Fatal("unsupported owner gained a fallback", kind)
		}
	}
	if _, err = routes.Discover(ctx, request); !errors.Is(err, store.providerErr) || store.lookups != 1 || len(store.providers) != 1 || store.providers[0] != oc.SkillRevision {
		t.Fatal("discovery did not return original selected Skill denial", err)
	}
	lock, err := f.ProjectLock(store.project)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := oc.NewAccessDependencies(f.Digest("sha256:"+strings.Repeat("1", 64)), []f.LockRequest{{Key: lock, Mode: f.Shared}})
	if err != nil {
		t.Fatal(err)
	}
	// The current Object row changes domains. Validate must re-read in this
	// exact Tx and let the new concrete provider reject; never cache discovery.
	if err = routes.ValidateInTx(ctx, store.tx, request, expected); !errors.Is(err, store.providerErr) || store.lookups != 2 || len(store.providers) != 2 || store.providers[1] != oc.Knowledge {
		t.Fatal("current mapping was cached or provider denial fell through", err)
	}
	want := expected.Locks()
	if len(store.held) != len(want) || f.CompareLockKeys(store.held[0].Key, want[0].Key) != 0 || store.held[0].Mode != want[0].Mode {
		t.Fatal("original expected locks were replaced")
	}
	before := store.lookups
	if err = routes.ValidateInTx(ctx, f.NewTx(), request, expected); err == nil || store.lookups != before {
		t.Fatal("foreign Tx reached lookup or a provider")
	}
	for _, kind := range []oc.OwnerKind{oc.OwnerKind("unknown"), oc.Artifact} {
		store.inside = kind
		if _, err = routes.Discover(ctx, request); err == nil || len(store.providers) != 2 {
			t.Fatal("unknown or unsupported mapping fell back")
		}
	}
	store.lookupErr = pgx.ErrNoRows
	if _, err = routes.Discover(ctx, request); err == nil || len(store.providers) != 2 {
		t.Fatal("missing Object mapping fell back")
	}
}
