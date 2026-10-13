package artifact

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// These are controlled durable rows and a live original Store/Tx. They prove
// the owner/checker boundary, not a real CancelCreation, Project grant or PG
// commit. The App integration test owns the actual cancellation positive.
type maintenanceWitnessStore struct {
	*projectAuditStore
	row      ownerFact
	command  commandRow
	reads    int
	unlocked bool
	intent   bool
}

func (s *maintenanceWitnessStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	if s.unlocked || len(locks) != 1 || locks[0].Key.Canonical() != artifactLock(s.row.id).Key.Canonical() || locks[0].Mode != f.Exclusive {
		return failure(f.Forbidden, nil)
	}
	return ctx.Err()
}

func (s *maintenanceWitnessStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.projectAuditStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *maintenanceWitnessStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	s.reads++
	if strings.HasPrefix(query, "SELECT id,project_id,creation_cause") {
		if len(args) != 1 || args[0] != s.row.id {
			return cleanupOwnerRow{err: pgx.ErrNoRows}
		}
		r := s.row
		return cleanupOwnerRow{values: []any{r.id, r.project, r.cause, r.originalActor, r.object, r.upload, r.commandKey, r.state, "prospective", int64(r.version)}}
	}
	if strings.Contains(query, "FROM agenteam_artifact.commands WHERE command_hash=") {
		if s.intent {
			return cleanupOwnerRow{err: pgx.ErrNoRows}
		}
		c := s.command
		if len(args) != 1 {
			return cleanupOwnerRow{err: errors.New("wrong cancellation command arity")}
		}
		return cleanupOwnerRow{values: []any{string(c.identity.Key()), c.ref.ProjectID.String(), c.ref.ArtifactID.String(), c.ref.FileID.String(), c.originalActor, digestRaw(c.digest), c.cause, c.path, string(c.kind), c.state, c.display.Name, c.display.Description, c.media, c.size, digestRaw(c.sha), []byte(nil), "", c.object.String(), c.upload.String(), c.attempt.String(), string(c.creatorKind), c.creator, c.execution, c.operation, c.tool, c.toolCall, c.created.Time()}}
	}
	if strings.Contains(query, "FROM agenteam_artifact.upload_intents WHERE id=") && s.intent {
		c := s.command
		if len(args) != 1 || args[0] != c.ref.ArtifactID.String() {
			return cleanupOwnerRow{err: pgx.ErrNoRows}
		}
		return cleanupOwnerRow{values: []any{c.ref.ArtifactID.String(), c.ref.FileID.String(), c.ref.ProjectID.String(), string(c.identity.Key()), digestRaw(c.digest), c.originalActor, c.cause, c.state, (*int64)(nil), c.display.Name, c.display.Description, c.media, c.size, []byte(nil), []byte(nil), c.object.String(), c.upload.String(), c.attempt.String(), "", int64(0), (*time.Time)(nil), c.execution, c.operation, c.tool, c.toolCall}}
	}
	if strings.Contains(query, "FROM agenteam_artifact.cleanup WHERE project_id=") {
		return cleanupOwnerRow{values: []any{false}}
	}
	return cleanupOwnerRow{err: errors.New("unexpected cancellation SQL")}
}

type maintenanceWitnessAuthority struct {
	ac.Authority
	check func(context.Context, f.Tx, ac.AccessSubject, i.AccessIntent) (i.AccessGrant, error)
}

func (a maintenanceWitnessAuthority) AuthorizeInTx(ctx context.Context, tx f.Tx, subject ac.AccessSubject, intent i.AccessIntent) (i.AccessGrant, error) {
	return a.check(ctx, tx, subject, intent)
}

type maintenanceWitnessFixture struct {
	base    *projectAuditFixture
	store   *maintenanceWitnessStore
	owner   *OwnerProvider
	actor   i.Actor
	ref     oc.ObjectOwner
	cause   oc.ObjectCleanupCause
	saved   context.Context
	subject ac.AccessSubject
	checks  int
}

func newMaintenanceWitnessFixture(t *testing.T) *maintenanceWitnessFixture {
	t.Helper()
	b := newProjectAuditFixture(t, "content")
	c := b.storedCommand
	c.state = "cancel_requested"
	c.upload, c.attempt = newTestID[oc.Upload](t), newTestID[oc.Attempt](t)
	v := &maintenanceWitnessFixture{base: b, ref: artifactOwner(c.ref)}
	v.store = &maintenanceWitnessStore{projectAuditStore: b.store, command: c, row: ownerFact{id: c.ref.ArtifactID.String(), project: c.ref.ProjectID.String(), cause: c.cause, originalActor: c.originalActor, object: c.object.String(), upload: c.upload.String(), commandKey: string(c.identity.Key()), state: c.state, version: 1}}
	registration, _ := i.RegisterService(i.ObjectMaintenance)
	var err error
	v.actor, err = registration.Actor(c.upload.String(), v.ref.Scope())
	if err != nil {
		t.Fatal(err)
	}
	id, err := f.ParseID[oc.CleanupOperation](c.upload.String())
	if err != nil {
		t.Fatal(err)
	}
	v.cause, err = oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: id, Owner: v.ref, Reason: oc.CancelledUpload})
	if err != nil {
		t.Fatal(err)
	}
	v.owner, err = NewOwnerProvider(v.store, maintenanceWitnessAuthority{check: func(ctx context.Context, tx f.Tx, s ac.AccessSubject, intent i.AccessIntent) (i.AccessGrant, error) {
		v.saved, v.subject = ctx, s
		v.checks++
		if intent != i.Converge {
			return i.AccessGrant{}, failure(f.Forbidden, nil)
		}
		if err := CheckCancelledUploadConvergenceInTx(ctx, tx, v.store, s); err != nil {
			return i.AccessGrant{}, err
		}
		return i.NewAccessGrant(s.Actor, v.ref.Scope(), intent, b.meta.Details().CreatedAt, 1)
	}})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestArtifactCancelledUploadOriginalWitness(t *testing.T) {
	for _, mode := range []string{"owner", "project-gate", "cleanup", "intent-cleanup"} {
		t.Run(mode, func(t *testing.T) {
			v := newMaintenanceWitnessFixture(t)
			var err error
			switch mode {
			case "owner":
				_, err = v.owner.AuthorizeOwnerInTx(context.Background(), v.store.tx, v.actor, v.ref, i.Converge)
			case "project-gate":
				err = v.owner.CheckInTx(context.Background(), v.store.tx, v.actor, v.ref, i.Converge)
			case "cleanup", "intent-cleanup":
				v.store.intent = mode == "intent-cleanup"
				err = v.owner.CheckCleanupInTx(context.Background(), v.store.tx, v.cause, v.store.command.object)
			}
			if err != nil || v.checks != 1 {
				t.Fatal("exact original callback did not reach the single checker", err, v.checks)
			}
			before := v.store.reads
			projectAuditCode(t, CheckCancelledUploadConvergenceInTx(v.saved, v.store.tx, v.store, v.subject), f.Forbidden)
			if v.store.reads != before {
				t.Fatal("expired callback proof read persistent rows")
			}
		})
	}
}

func TestArtifactCancelledUploadExactTuple(t *testing.T) {
	for _, mode := range []string{"wrong-object", "wrong-upload", "wrong-owner", "changed-cause", "changed-command-key", "pending", "agent-provenance", "wrong-original-human", "read-intent", "missing-owner-lock"} {
		t.Run(mode, func(t *testing.T) {
			v := newMaintenanceWitnessFixture(t)
			intent := i.Converge
			switch mode {
			case "wrong-object":
				v.store.command.object = newTestID[oc.StoredObject](t)
			case "wrong-upload":
				v.store.command.upload = newTestID[oc.Upload](t)
			case "wrong-owner":
				v.store.command.ref.ProjectID = newTestID[i.Project](t)
			case "changed-cause":
				v.store.command.cause = newTestID[struct{}](t).String()
			case "changed-command-key":
				v.store.row.commandKey = "different-command"
			case "pending":
				v.store.row.state, v.store.command.state = "pending", "pending"
			case "agent-provenance":
				v.store.command.execution = newTestID[i.Execution](t).String()
			case "wrong-original-human":
				v.store.command.creator = newTestID[i.User](t).String()
			case "read-intent":
				intent = i.Read
			case "missing-owner-lock":
				v.store.unlocked = true
			}
			_, err := v.owner.AuthorizeOwnerInTx(context.Background(), v.store.tx, v.actor, v.ref, intent)
			if err == nil || v.checks != 0 {
				t.Fatal("foreign/incomplete cancellation reached the Project grant", mode, err, v.checks)
			}
		})
	}
}

func TestArtifactCancelledUploadLiveChecker(t *testing.T) {
	v := newMaintenanceWitnessFixture(t)
	called := false
	v.owner, _ = NewOwnerProvider(v.store, maintenanceWitnessAuthority{check: func(ctx context.Context, tx f.Tx, s ac.AccessSubject, intent i.AccessIntent) (i.AccessGrant, error) {
		called = true
		if err := CheckCancelledUploadConvergenceInTx(ctx, tx, v.store, s); err != nil {
			t.Fatal(err)
		}
		before := v.store.reads
		projectAuditCode(t, CheckCancelledUploadConvergenceInTx(ctx, f.NewTx(), v.store, s), f.Forbidden)
		projectAuditCode(t, CheckCancelledUploadConvergenceInTx(ctx, tx, v.base.store, s), f.Forbidden)
		projectAuditCode(t, CheckCancelledUploadConvergenceInTx(context.Background(), tx, v.store, s), f.Forbidden)
		if v.store.reads != before {
			t.Fatal("wrong Store/Tx or bare subject reached the original rows")
		}
		v.store.command.digest = digestBytes([]byte("changed original request"))
		projectAuditCode(t, CheckCancelledUploadConvergenceInTx(ctx, tx, v.store, s), f.Forbidden)
		return i.AccessGrant{}, failure(f.Forbidden, nil)
	}})
	_, err := v.owner.AuthorizeOwnerInTx(context.Background(), v.store.tx, v.actor, v.ref, i.Converge)
	if err == nil || !called {
		t.Fatal("original live verifier was not exercised", err)
	}
}
