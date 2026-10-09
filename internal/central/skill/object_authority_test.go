package skill

import (
	"context"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

type skillAuthorityStore struct {
	*initializationReadStore
	exists, serving bool
}

func (s *skillAuthorityStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, e := s.initializationReadStore.InTx(tx); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *skillAuthorityStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.HasPrefix(query, "SELECT creation_id::text,revision_id::text,protected,serving,version") {
		if !s.exists {
			return skillRowValues{err: pgx.ErrNoRows}
		}
		return skillRowValues{values: []any{s.row.values[1], s.row.values[4], true, s.serving, int64(1)}}
	}
	return s.initializationReadStore.QueryRow(ctx, query, args...)
}
func authorityFixture(t *testing.T) (*Authority, *skillAuthorityStore, *initializationReadProjects, id.Actor, initializationRow) {
	t.Helper()
	s, base, projects, actor, _ := readFixture(t)
	base.row.values = stateValues(t)
	store := &skillAuthorityStore{initializationReadStore: base, serving: true}
	a := s.state().authority
	a.state().store = store
	return a, store, projects, actor, stateRow(t)
}
func armAuthorityTx(t *testing.T, store *skillAuthorityStore, locks []f.LockRequest) f.Tx {
	t.Helper()
	store.live = true
	store.tx = f.NewTx()
	store.held = map[string]f.LockMode{}
	if e := store.AcquireAll(context.Background(), store.tx, locks); e != nil {
		t.Fatal(e)
	}
	return store.tx
}
func TestSkillObjectAuthorityExactInitializationOperations(t *testing.T) {
	a, store, _, actor, row := authorityFixture(t)
	owner, _ := row.owner()
	payload, _ := oc.NewPreparedPayload(oc.PreparedDetails{ID: stateID[oc.Payload](101), MediaType: sc.PackageMediaType, Length: int64(row.bundle.size), SHA256: row.bundle.packageDigest})
	command := f.CommandMeta{RequestID: stateID[f.Request](102), IdempotencyKey: row.request.InitializationKey}
	for _, name := range []string{"prepare", "reserve", "wrong_key", "changed_payload", "stat", "read", "consume", "other_revision"} {
		t.Run(name, func(t *testing.T) {
			d := oc.AccessRequestDetails{Operation: oc.PrepareAccess, Actor: actor, Owner: owner, Intent: id.Mutate}
			bodyRead := false
			switch name {
			case "reserve", "wrong_key", "changed_payload":
				c := command
				if name == "wrong_key" {
					c.IdempotencyKey = "another-command"
				}
				d.Operation = oc.ReserveAccess
				d.Command = &c
				d.Prepared = payload
				if name == "changed_payload" {
					p := payload.Details()
					p.Length++
					d.Prepared, _ = oc.NewPreparedPayload(p)
				}
			case "stat", "read":
				bodyRead = true
				d.Intent = id.Read
				d.ObjectID = stateID[oc.StoredObject](7)
				d.Operation = oc.StatAccess
				if name == "read" {
					d.Operation = oc.ReadAccess
				}
			case "consume":
				d.Operation = oc.ConsumeAccess
				d.Receipt, _ = oc.NewUploadReceipt(oc.ReceiptDetails{ID: stateID[oc.Receipt](103), UploadID: stateID[oc.Upload](8), ObjectID: stateID[oc.StoredObject](7), Owner: owner, CreationCause: row.request.CreationID.String()})
			case "other_revision":
				d.Owner, _ = oc.NewObjectOwner(oc.SkillRevision, stateID[sc.Revision](104).String(), row.request.ProjectID.String())
			}
			request, e := oc.NewOwnerAccess(d)
			if bodyRead {
				request, e = oc.NewObjectReadAccess(d)
			}
			if e != nil {
				t.Fatal(e)
			}
			deps, e := a.Discover(context.Background(), request)
			if name != "prepare" && name != "reserve" {
				if e == nil {
					t.Fatal("widened initialization operation")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			tx := armAuthorityTx(t, store, deps.Locks())
			if e = a.ValidateInTx(context.Background(), tx, request, deps); e != nil {
				t.Fatal(e)
			}
			g, e := a.AuthorizeOwnerInTx(context.Background(), tx, actor, owner, id.Mutate)
			if e != nil || g.Details().Existence != oc.ProspectiveOwner || g.Details().CreationCause != row.request.CreationID.String() {
				t.Fatal("original cause grant", e)
			}
			if g.Details().ReadObjectID != (oc.ObjectID{}) || g.Details().ProtectedLease != nil {
				t.Fatal("initialization obtained body permission")
			}
		})
	}
}
func TestSkillObjectAuthorityRevalidatesCurrentGateAndMapping(t *testing.T) {
	for _, name := range []string{"publish_existing", "revoked_gate", "lost_lock", "ended_tx", "changed_attempt", "tombstone", "unknown_commit"} {
		t.Run(name, func(t *testing.T) {
			a, store, projects, actor, row := authorityFixture(t)
			owner, _ := row.owner()
			row.phase = initializationReserved
			row.object = stateID[oc.StoredObject](7)
			row.upload = stateID[oc.Upload](8)
			row.attempt = stateID[oc.Attempt](6)
			store.row.values[14] = "reserved"
			store.row.values[16] = row.object.String()
			store.row.values[17] = row.upload.String()
			store.row.values[18] = row.attempt.String()
			attempt, _ := oc.NewUploadAttempt(oc.AttemptDetails{ID: row.attempt, ObjectID: row.object, UploadID: row.upload})
			request, e := oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.PublishAccess, Actor: actor, Owner: owner, Intent: id.Mutate, Attempt: attempt})
			if e != nil {
				t.Fatal(e)
			}
			deps, e := a.Discover(context.Background(), request)
			if e != nil {
				t.Fatal(e)
			}
			if name == "unknown_commit" {
				store.unknown = true
				g, e := a.AuthorizeOwner(context.Background(), actor, owner, id.Mutate)
				if e == nil || g.Validate() == nil {
					t.Fatal("unknown authorization escaped")
				}
				if _, ok := UnknownAttempt(e); !ok {
					t.Fatal("unknown lost")
				}
				return
			}
			tx := armAuthorityTx(t, store, deps.Locks())
			store.exists = true
			sentinel := fault(f.Forbidden)
			switch name {
			case "revoked_gate":
				projects.gate = sentinel
			case "lost_lock":
				delete(store.held, skillLock(row.skill, f.Exclusive).Key.Canonical())
			case "ended_tx":
				store.live = false
			case "changed_attempt":
				store.row.values[18] = stateID[oc.Attempt](105).String()
			case "tombstone":
				store.serving = false
			}
			e = a.ValidateInTx(context.Background(), tx, request, deps)
			if name == "publish_existing" {
				if e != nil {
					t.Fatal("same-Tx existence transition invalidated mapping", e)
				}
				grant, e := a.AuthorizeOwnerInTx(context.Background(), tx, actor, owner, id.Mutate)
				if e != nil || grant.Details().Existence != oc.ExistingOwner {
					t.Fatal("publication owner not existing", e)
				}
			} else if e == nil {
				t.Fatal("stale permission accepted")
			}
			if name == "revoked_gate" && e != sentinel {
				t.Fatal("current Project fault replaced")
			}
		})
	}
}
