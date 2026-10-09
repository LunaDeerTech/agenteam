//go:build integration

package skill_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// Account and Project facts below are explicit test seeds. The calls consume
// the real current Session and Owner authorities, not a permissive provider.
// This tests metadata reading; it does not certify Login, Project.Create,
// lifecycle commands, Object reads or the production composition root.
func TestSkillOwnerMetadataCurrentAuthority(t *testing.T) {
	p := newSkillPG(t)
	initializeReaderAccountKeys(t, p)
	v := p.newCase(t)
	owner := seedReaderHuman(t, p, v.owner, "skill-owner", "user")
	foreign := seedReaderHuman(t, p, testID[id.User](t), "skill-foreign", "user")
	admin := seedReaderHuman(t, p, testID[id.User](t), "skill-admin", "admin")
	complete, e := v.service.InitializeProjectSkills(testContext(t), v.actor, v.request)
	if e != nil || complete.State != pc.InitializationCompleted || !complete.Matches(v.request) {
		t.Fatal("Skill publication prerequisite failed", e)
	}
	skill := *complete.AddSkillsID
	physicalSteps := len(v.objects.steps)
	var want sc.Metadata
	var skillID, projectID string
	var revision, version int64
	e = p.store.QueryRow(testContext(t), `SELECT id::text,project_id::text,name,normalized_name,description,protected,current_revision,version FROM agenteam_skill.skills WHERE id=$1`, skill.String()).Scan(&skillID, &projectID, &want.Name, &want.NormalizedName, &want.Description, &want.Protected, &revision, &version)
	if e != nil {
		t.Fatal("published metadata prerequisite failed", e)
	}
	want.ID, e = f.ParseID[pc.Skill](skillID)
	if e != nil {
		t.Fatal(e)
	}
	want.ProjectID, e = f.ParseID[id.Project](projectID)
	want.CurrentRevision, want.Version = f.Revision(revision), f.Version(version)
	if e != nil || want.Validate() != nil || want.ID != skill || want.ProjectID != v.request.ProjectID || !want.Protected || want.CurrentRevision != 1 {
		t.Fatal("published metadata shape mismatch")
	}
	read := func(t *testing.T, actor id.Actor, project id.ProjectID, expected f.Code) {
		t.Helper()
		listed, listErr := v.service.ListSkills(testContext(t), actor, project)
		got, getErr := v.service.GetSkill(testContext(t), actor, project, skill)
		if expected == "" {
			if listErr != nil || getErr != nil || len(listed) != 1 || listed[0] != want || got != want {
				t.Fatal("authorized directory differs from committed metadata", listErr, getErr)
			}
		} else {
			for _, err := range []error{listErr, getErr} {
				var known *f.Fault
				if !errors.As(err, &known) || known.Code != expected {
					t.Fatalf("expected closed read denial %s", expected)
				}
			}
			if len(listed) != 0 || got != (sc.Metadata{}) {
				t.Fatal("denied read exposed metadata")
			}
		}
		if len(v.objects.steps) != physicalSteps {
			t.Fatal("metadata read performed an Object operation")
		}
	}
	t.Run("published_skill_does_not_initialize_project", func(t *testing.T) {
		read(t, owner, v.request.ProjectID, f.ProjectNotActive)
	})
	seedReaderReadyProject(t, p, v, skill)
	t.Run("active_current_owner", func(t *testing.T) { read(t, owner, v.request.ProjectID, "") })
	t.Run("foreign_owner", func(t *testing.T) { read(t, foreign, v.request.ProjectID, f.NotFound) })
	t.Run("foreign_admin", func(t *testing.T) { read(t, admin, v.request.ProjectID, f.NotFound) })
	t.Run("session_user_mismatch", func(t *testing.T) {
		session, e := f.ParseID[id.Session](foreign.Details().SessionID)
		if e != nil {
			t.Fatal(e)
		}
		actor, e := id.NewHuman(v.owner, session)
		if e != nil {
			t.Fatal(e)
		}
		read(t, actor, v.request.ProjectID, f.Unauthenticated)
	})
	t.Run("missing_session", func(t *testing.T) {
		actor, e := id.NewHuman(v.owner, testID[id.Session](t))
		if e != nil {
			t.Fatal(e)
		}
		read(t, actor, v.request.ProjectID, f.Unauthenticated)
	})
	t.Run("revoked_session_rechecked", func(t *testing.T) {
		readerMutation(t, p, v, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, owner.Details().SessionID)
		read(t, owner, v.request.ProjectID, f.SessionRevoked)
		readerMutation(t, p, v, `UPDATE agenteam_account.sessions SET revoked_at=NULL,revoked_reason=NULL WHERE id=$1`, owner.Details().SessionID)
		read(t, owner, v.request.ProjectID, "")
	})
	t.Run("expired_session_rechecked", func(t *testing.T) {
		readerMutation(t, p, v, `UPDATE agenteam_account.sessions SET issued_at=statement_timestamp()-interval '2 hours',last_activity_at=statement_timestamp()-interval '2 hours',absolute_expires_at=statement_timestamp()-interval '1 hour' WHERE id=$1`, owner.Details().SessionID)
		read(t, owner, v.request.ProjectID, f.Unauthenticated)
		readerMutation(t, p, v, `UPDATE agenteam_account.sessions SET issued_at=statement_timestamp()-interval '2 minutes',last_activity_at=statement_timestamp()-interval '2 minutes',absolute_expires_at=statement_timestamp()+interval '1 hour' WHERE id=$1`, owner.Details().SessionID)
		read(t, owner, v.request.ProjectID, "")
	})
	t.Run("unknown_skill", func(t *testing.T) {
		got, e := v.service.GetSkill(testContext(t), owner, v.request.ProjectID, testID[pc.Skill](t))
		var known *f.Fault
		if !errors.As(e, &known) || known.Code != f.NotFound || got != (sc.Metadata{}) {
			t.Fatal("unknown Skill exposed directory metadata")
		}
	})
	t.Run("unknown_project", func(t *testing.T) { read(t, owner, testID[id.Project](t), f.NotFound) })
	t.Run("archived_current_owner", func(t *testing.T) {
		readerMutation(t, p, v, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=statement_timestamp(),updated_at=statement_timestamp(),version=version+1 WHERE id=$1`, v.request.ProjectID.String())
		read(t, owner, v.request.ProjectID, "")
	})
	t.Run("deleting_current_owner", func(t *testing.T) {
		seedReaderDeletingProject(t, p, v)
		read(t, owner, v.request.ProjectID, f.ProjectNotActive)
	})
	if len(v.objects.steps) != physicalSteps {
		t.Fatal("metadata directory issued external work")
	}
	assertPublishedFacts(t, p, v, complete)
}

// This is a valid upstream lifecycle seed, not BeginDelete. Keep the current
// archived Project version/Owner, its real manifest and required participants
// consistent with the operation parent referenced by the Project FK.
func seedReaderDeletingProject(t *testing.T, p *skillPG, v *skillCase) {
	t.Helper()
	manifest := skillStopManifest(t)
	raw, err := json.Marshal(manifest.Entries())
	if err != nil {
		t.Fatal(err)
	}
	digest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	operation := testID[pc.Operation](t)
	result := p.store.WithinTx(testContext(t), testCause(t), func(ctx context.Context, tx f.Tx) error {
		user, _ := f.UserLock(v.owner.String())
		project, _ := f.ProjectLock(v.request.ProjectID.String())
		if err := p.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Exclusive}, {Key: project, Mode: f.Exclusive}}); err != nil {
			return err
		}
		x, err := p.store.InTx(tx)
		if err != nil {
			return err
		}
		var version int64
		if err = x.QueryRow(ctx, `SELECT version FROM agenteam_project.projects WHERE id=$1 AND owner_user_id=$2 AND lifecycle='archived' AND initialized_at IS NOT NULL`, v.request.ProjectID.String(), v.owner.String()).Scan(&version); err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) VALUES($1,$2,$3,'delete',$4,'accepted',1,$5::jsonb,$6,clock_timestamp(),clock_timestamp())`, operation.String(), v.request.ProjectID.String(), v.owner.String(), version+1, raw, digest.String()); err != nil {
			return err
		}
		for _, entry := range manifest.Entries() {
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_participants(operation_id,participant_name,contract_version,stop_state,cleanup_state,version) VALUES($1,$2,$3,'required','required',1)`, operation.String(), string(entry.Name), int64(entry.ContractVersion)); err != nil {
				return err
			}
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='deleting',current_lifecycle_operation_id=$2,updated_at=clock_timestamp(),version=version+1 WHERE id=$1 AND version=$3 AND lifecycle='archived'`, v.request.ProjectID.String(), operation.String(), version)
		if err == nil && tag.RowsAffected() != 1 {
			return f.NewFault(f.InvalidState, f.NotCommitted)
		}
		return err
	})
	requireCommitted(t, result)
}

// Register the same explicit test keys used by newSkillPG through the actual
// Account initializer. It is not a replacement Session authority.
func initializeReaderAccountKeys(t *testing.T, p *skillPG) {
	t.Helper()
	b64 := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	cursors, e := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, b64(1)))
	if e != nil {
		t.Fatal(e)
	}
	secrets, e := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, b64(2)), cursors)
	if e != nil {
		t.Fatal(e)
	}
	downloads, e := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, b64(3)), cursors, secrets)
	if e != nil {
		t.Fatal(e)
	}
	keys, e := account.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, b64(4)), cursors, secrets, downloads)
	if e != nil {
		t.Fatal(e)
	}
	authority, e := account.NewAuthority(p.store, keys)
	if e != nil {
		t.Fatal(e)
	}
	if e = authority.Initialize(testContext(t)); e != nil {
		t.Fatal("Account key registration failed", e)
	}
}

func seedReaderHuman(t *testing.T, p *skillPG, user id.UserID, username, role string) id.Actor {
	t.Helper()
	session := testID[id.Session](t)
	verifier := sha256.Sum256([]byte(session.String()))
	r := p.store.WithinTx(testContext(t), testCause(t), func(ctx context.Context, tx f.Tx) error {
		key, e := f.UserLock(user.String())
		if e != nil {
			return e
		}
		if e = p.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); e != nil {
			return e
		}
		x, e := p.store.InTx(tx)
		if e != nil {
			return e
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.users(id,email,username,display_name,role,password_phc,password_version,version,auth_sequence,initial_password_suggestion,theme) VALUES($1,$2,$3,'Skill read fixture',$4,'fixture-not-a-login-hash',1,1,1,false,'system')`, user.String(), username+"@example.test", username, role); e != nil {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.sessions(id,user_id,token_verifier,csrf_kid,issued_at,last_activity_at,idle_seconds,absolute_expires_at) VALUES($1,$2,$3,'a',statement_timestamp()-interval '2 minutes',statement_timestamp()-interval '2 minutes',3600,statement_timestamp()+interval '1 hour')`, session.String(), user.String(), verifier[:])
		return e
	})
	requireCommitted(t, r)
	actor, e := id.NewHuman(user, session)
	if e != nil {
		t.Fatal(e)
	}
	return actor
}

func readerMutation(t *testing.T, p *skillPG, v *skillCase, query string, args ...any) {
	t.Helper()
	r := p.store.WithinTx(testContext(t), testCause(t), func(ctx context.Context, tx f.Tx) error {
		user, _ := f.UserLock(v.owner.String())
		project, _ := f.ProjectLock(v.request.ProjectID.String())
		if e := p.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Exclusive}, {Key: project, Mode: f.Exclusive}}); e != nil {
			return e
		}
		x, e := p.store.InTx(tx)
		if e != nil {
			return e
		}
		tag, e := x.Exec(ctx, query, args...)
		if e == nil && tag.RowsAffected() != 1 {
			return f.NewFault(f.InvalidState, f.NotStarted)
		}
		return e
	})
	requireCommitted(t, r)
}

// Seed a mutually consistent completed Project/Creation after the real Skill
// publication. This is an explicit downstream read prerequisite, not execution
// of Project's completion command, Audit or Outbox.
func seedReaderReadyProject(t *testing.T, p *skillPG, v *skillCase, skill sc.SkillID) {
	t.Helper()
	r := p.store.WithinTx(testContext(t), testCause(t), func(ctx context.Context, tx f.Tx) error {
		key, _ := f.ProjectLock(v.request.ProjectID.String())
		if e := p.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); e != nil {
			return e
		}
		x, e := p.store.InTx(tx)
		if e != nil {
			return e
		}
		var created, now time.Time
		var name, description, eventID string
		if e = x.QueryRow(ctx, `SELECT p.name,p.description,p.created_at,clock_timestamp(),c.event_id::text FROM agenteam_project.projects p JOIN agenteam_project.creations c ON c.id=p.creation_id WHERE p.id=$1 AND c.state='initializing'`, v.request.ProjectID.String()).Scan(&name, &description, &created, &now, &eventID); e != nil {
			return e
		}
		createdAt, e := f.NewInstant(created)
		if e != nil {
			return e
		}
		at, e := f.NewInstant(now)
		if e != nil {
			return e
		}
		normalized, e := pc.NormalizeName(name)
		if e != nil {
			return e
		}
		ref := pc.ProjectRef{ID: v.request.ProjectID, OwnerUserID: v.owner, Name: name, NormalizedName: normalized, Description: description, Lifecycle: pc.Active, Version: 1, CreatedAt: createdAt, UpdatedAt: at}
		safeResult, e := json.Marshal(ref)
		if e != nil {
			return e
		}
		evID, e := f.ParseID[ec.EventIdentity](eventID)
		if e != nil {
			return e
		}
		scope, _ := f.ParseID[ec.Project](v.request.ProjectID.String())
		aggregate, _ := f.ParseID[ec.Aggregate](v.request.ProjectID.String())
		version := f.Version(1)
		h := ec.Header{EventID: evID, EventType: pc.CreatedEventName, SchemaVersion: pc.ProjectEventSchemaVersion, OccurredAt: at, Scope: ec.Scope{Kind: ec.ProjectScope, ProjectID: scope}, AggregateType: pc.ProjectAggregate, AggregateID: aggregate, AggregateVersion: &version}
		if e = h.Validate(); e != nil {
			return e
		}
		header, e := json.Marshal(h)
		if e != nil {
			return e
		}
		payload, e := json.Marshal(pc.CreatedPayload{OwnerUserID: v.owner, CreationID: v.request.CreationID})
		if e != nil {
			return e
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_project.projects SET initialized_at=$2,updated_at=$2 WHERE id=$1`, v.request.ProjectID.String(), now); e != nil {
			return e
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_project.creations SET state='completed',version=version+1,updated_at=$2,request_name=NULL,request_description=NULL,protected_skill_id=$3,protected_revision=1,event_header=$4::jsonb,event_payload=$5::jsonb,safe_result=$6::jsonb WHERE id=$1`, v.request.CreationID.String(), now, skill.String(), header, payload, safeResult)
		return e
	})
	requireCommitted(t, r)
}
