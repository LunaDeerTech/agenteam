package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

type installedReadStore struct {
	*initializationWriteStore
	installed          installationRow
	installationValues []any
	canonical          skillRowValues
	absent             bool
}

func installationValues(t *testing.T, r installationRow) []any {
	t.Helper()
	manifest, err := json.Marshal(r.pkg.manifest)
	if err != nil {
		t.Fatal(err)
	}
	values := []any{r.id.String(), r.project.String(), r.user.String(), r.key.String(), r.skill.String(), r.revision.String(), string(r.semantic), string(r.pkg.packageDigest), string(r.pkg.manifestDigest), int64(r.pkg.size), manifest, r.pkg.name, r.pkg.normalized, r.pkg.description, string(r.phase), int64(r.version), r.object.String(), r.upload.String(), r.attempt.String(), r.reason, r.created.Time(), r.updated.Time()}
	if r.execution == nil {
		return append(values, string(id.Human), "", "", "", "", int64(0), "", "", "")
	}
	e := r.execution
	return append(values, string(id.AgentRun), e.agent.String(), e.execution.String(), e.binding.OperationID, e.binding.ToolID.String(), int64(e.binding.SpecRevision), e.binding.Fingerprint.String(), e.binding.AttemptID, e.request.String())
}

func (s *installedReadStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if strings.Contains(q, "FROM agenteam_skill.installations") {
		if s.absent {
			return skillRowValues{err: pgx.ErrNoRows}
		}
		// This is a controlled same-Store repository, not a physical SQL proof.
		return skillRowValues{values: s.installationValues}
	}
	if strings.Contains(q, "JOIN agenteam_skill.installation_attempts") {
		return s.canonical
	}
	return s.initializationWriteStore.QueryRow(ctx, q, args...)
}

func (s *installedReadStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.initializationWriteStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}

func installedReadFixture(t *testing.T) (*Service, *installedReadStore, *skillReaderProjects, id.Actor, installationRow) {
	t.Helper()
	s, base, projects, actor, _ := readerFixture(t)
	r := installRepositoryRow(t)
	r.project, r.user = projects.project.ID, projects.project.OwnerUserID
	r.semantic, _ = installationSemantic(r.project, r.user, r.skill, r.pkg)
	r.phase = installationPublished
	r.object, r.upload, r.attempt = stateID[oc.StoredObject](509), stateID[oc.Upload](510), stateID[oc.Attempt](511)
	store := &installedReadStore{initializationWriteStore: &initializationWriteStore{initializationReadStore: base}, installed: r}
	store.installationValues = installationValues(t, r)
	store.canonical = skillRowValues{values: []any{r.project.String(), r.skill.String(), r.revision.String(), r.id.String(), r.id.String(), true, r.pkg.name, r.pkg.normalized, r.pkg.description, false, int64(1), int64(1), true, int64(1), r.object.String(), int64(1), r.created.Time(), r.updated.Time(), r.attempt.String(), stateID[oc.Process](50).String()}}
	s.state().authority.state().store = store
	return s, store, projects, actor, r
}

func TestInstalledSkillCanonicalRead(t *testing.T) {
	for _, name := range []string{"current", "origin", "protected", "creation", "revision", "attempt", "process", "closed", "sql"} {
		t.Run(name, func(t *testing.T) {
			_, store, _, _, r := installedReadFixture(t)
			switch name {
			case "origin":
				store.canonical.values[4] = stateID[Installation](599).String()
			case "protected":
				store.canonical.values[9] = true
			case "creation":
				store.canonical.values[5] = false
			case "revision":
				store.canonical.values[13] = int64(2)
			case "attempt":
				store.canonical.values[18] = stateID[oc.Attempt](598).String()
			case "process":
				store.canonical.values[19] = "invalid"
			case "closed":
				store.canonical.values[12] = false
			case "sql":
				store.canonical.err = errors.New("private-installed-sql-canary")
			}
			m, revision, err := loadInstalled(context.Background(), store, r)
			if name == "current" {
				if err != nil || m.ID != r.skill || m.Protected || revision.ID != r.revision || revision.PackageSHA256 != r.pkg.packageDigest {
					t.Fatal("canonical ordinary publication missing", err)
				}
			} else if err == nil || m.ID != (sc.SkillID{}) || revision.ID != (sc.RevisionID{}) {
				t.Fatal("bad canonical source returned material")
			}
		})
	}
}

func TestInstalledSkillCurrentOwnerAndUnknown(t *testing.T) {
	for _, name := range []string{"current", "session", "absent", "unknown"} {
		t.Run(name, func(t *testing.T) {
			s, store, projects, actor, r := installedReadFixture(t)
			sentinel := fault(f.SessionRevoked)
			switch name {
			case "session":
				projects.err = sentinel
			case "absent":
				store.absent = true
			case "unknown":
				store.unknownAt = 1
			}
			got, err := s.GetSkill(context.Background(), actor, r.project, r.skill)
			if name == "current" {
				if err != nil || got.ID != r.skill || got.Protected {
					t.Fatal("ordinary current Owner read failed", err)
				}
			} else if err == nil || got.ID != (sc.SkillID{}) {
				t.Fatal("failed current transaction returned metadata")
			}
			if projects.calls != 1 || store.live {
				t.Fatal("current gate or original transaction not joined")
			}
			if name == "session" && !errors.Is(err, sentinel) {
				t.Fatal("current Session error changed")
			}
		})
	}
}

type installedReadObjects struct {
	ObjectPorts
	t     *testing.T
	store *installedReadStore
	actor id.Actor
	row   installationRow
	meta  sc.RevisionMetadata
	body  *packageTestBody
	reads int
}

func (o *installedReadObjects) ReadObject(_ context.Context, actor id.Actor, owner oc.ObjectOwner, object oc.ObjectID, span *oc.ByteRange) (*oc.ObjectReader, error) {
	o.reads++
	want, _ := o.row.owner()
	if !actor.Equal(o.actor) || !owner.Equal(want) || object != o.row.object || span != nil || o.store.live || len(o.store.work) != 1 {
		o.t.Fatal("ordinary reader bypassed committed work/current Owner")
	}
	for _, w := range o.store.work {
		if w[4] != string(installedPackageReaderWork) {
			o.t.Fatal("ordinary reader registered initialization source")
		}
	}
	return oc.NewObjectReader(o.meta.Object, nil, o.body)
}

func TestInstalledPackageKeepsActualReaderTail(t *testing.T) {
	s, store, _, actor, r := installedReadFixture(t)
	_, meta, err := loadInstalled(context.Background(), store, r)
	if err != nil {
		t.Fatal(err)
	}
	body := &packageTestBody{reader: bytes.NewReader(nil), started: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	objects := &installedReadObjects{t: t, store: store, actor: actor, row: r, meta: meta, body: body}
	s.state().objects = objects
	reader, err := s.OpenPackage(context.Background(), actor, r.project, r.skill, 1)
	if err != nil {
		t.Fatal(err)
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(body.release) }) }
	t.Cleanup(func() { release(); _ = reader.Close(); waitPackageCalls(t, s) })
	done := make(chan error, 1)
	go func() { _, e := reader.Read(make([]byte, 1)); done <- e }()
	<-body.started
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	if reader.Joined() {
		t.Fatal("Close falsely joined original blocked Read")
	}
	s.state().mu.Lock()
	active := len(s.state().calls)
	s.state().mu.Unlock()
	if active != 1 {
		t.Fatal("ordinary work retired before original Read")
	}
	release()
	if err = <-done; !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("original Read did not return", err)
	}
	waitPackageCalls(t, s)
	if !reader.Joined() || body.closes.Load() != 1 || objects.reads != 1 || store.live {
		t.Fatal("original reader/Close/work transaction did not join")
	}
	for _, w := range store.work {
		if w[5] != "joined" {
			t.Fatal("ordinary durable work not retired")
		}
	}
}
