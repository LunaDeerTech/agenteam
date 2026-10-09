//go:build integration

package knowledge_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ev "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/minio/minio-go/v7"
)

// Only upstream Account/Project facts are seeded. Canonical documents, Object
// uploads/references, Audit and Outbox facts are produced by their real services
// on one Store. This fixture supplies no successful authorization stub.
func newPublicationFixture(t *testing.T) *ownerTreeFixture {
	t.Helper()
	x := newOwnerTreeFixture(t)
	accounts := x.deps.Activity.(*account.Authority)
	knowledgeFacts, err := knowledge.NewProjectAuditAuthority(x.raw)
	if err != nil {
		t.Fatal(err)
	}
	objectFacts, err := object.NewProjectAuditAuthority(x.raw)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := project.NewAuthority(x.raw, project.AuthorityDependencies{
		Sessions: accounts, Routes: accounts,
		AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.KnowledgeProducer: knowledgeFacts, ac.ObjectProducer: objectFacts},
	})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := knowledge.NewAuthority(x.raw, projects)
	if err != nil {
		t.Fatal(err)
	}
	auditing, err := audit.New(x.raw, x.deps.Cursors, audit.Authorizations{Projects: projects, Sessions: accounts, System: accounts})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := objectfixture.Load()
	if err != nil {
		t.Fatal("owned Object fixture unavailable")
	}
	s3, transport, err := remote.Client()
	if err != nil {
		t.Fatal("owned Object client unavailable")
	}
	t.Cleanup(transport.CloseIdleConnections)
	suffix, err := pgfixture.RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	bucket := "d05-" + remote.Nonce[:12] + "-" + suffix
	if err = s3.MakeBucket(knowledgeContext(t), bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatal("owned bucket creation failed")
	}
	values := map[string]string{"ENDPOINT": remote.Endpoint(), "BUCKET": bucket, "ACCESS_KEY": remote.AccessKey, "SECRET_KEY": remote.SecretKey, "TLS_MODE": "verify-full", "CA_FILE": remote.CAFile}
	config, err := object.LoadStorageConfig(func(name string) (string, bool) {
		v, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return v, ok
	})
	if err != nil {
		t.Fatal("owned storage configuration invalid")
	}
	backend, err := object.NewBackend(config)
	if err != nil {
		t.Fatal("owned storage backend unavailable")
	}
	process, err := f.ParseID[oc.Process](x.deps.Processes.CurrentProcess().String())
	if err != nil {
		t.Fatal(err)
	}
	spool, err := object.OpenSpool(filepath.Join(t.TempDir(), "spool"), process)
	if err != nil {
		_ = backend.Close()
		t.Fatal(err)
	}
	objects, err := object.New(x.raw, backend, spool, auditing, object.Authorizations{Planner: authority, Resources: authority, Read: authority, Gate: authority, Cleanup: authority})
	if err != nil {
		_ = spool.Close()
		_ = backend.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		objects.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := objects.Drain(ctx); err != nil {
			_ = objects.Force(ctx)
			t.Error("Object actual drain", err)
		}
	})
	if err = objects.Initialize(knowledgeContext(t)); err != nil {
		t.Fatal("Object initialization", err)
	}
	sources, err := knowledge.NewSourceResolver(x.raw, authority, objects)
	if err != nil {
		t.Fatal(err)
	}
	reads, err := object.NewSourceReads(objects, sources)
	if err != nil {
		t.Fatal(err)
	}
	catalog := ev.NewCatalog()
	events, err := kc.RegisterKnowledgeEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	appender, err := outbox.New(x.raw, catalog, outbox.Authorizations{Projects: projects, Sessions: accounts, System: accounts, Processes: x.deps.Processes, Audit: auditing, Cursors: x.deps.Cursors, Producers: map[ev.StableName]ob.ProducerAuthority{kc.KnowledgeProducer: authority}})
	if err != nil {
		t.Fatal(err)
	}
	x.deps.Projects, x.deps.Audit, x.deps.Outbox, x.deps.Events = projects, auditing, appender, events
	x.deps.Objects, x.deps.Uploads, x.deps.ReferenceCleanup, x.deps.ObjectCleanup = objects, objects, objects, objects
	x.deps.Sources, x.deps.SourceReads = sources, reads
	x.service = publicationService(t, x, nil)
	return x
}

func publicationService(t *testing.T, x *ownerTreeFixture, change func(*knowledge.Dependencies)) *knowledge.Service {
	t.Helper()
	deps := x.deps
	if change != nil {
		change(&deps)
	}
	s, err := knowledge.New(x.raw, deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.Drain(ctx); err != nil {
			t.Error("Knowledge actual drain", err)
		}
	})
	return s
}
func publicationText(t *testing.T, body string) kc.SourceInput {
	t.Helper()
	s, err := kc.NewTextSource("text/plain", body)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func publicationCount(t *testing.T, x *ownerTreeFixture, p id.ProjectID) (audits, events int) {
	t.Helper()
	err := x.raw.QueryRow(knowledgeContext(t), `SELECT (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='object.upload.complete'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1)`, p.String()).Scan(&audits, &events)
	if err != nil {
		t.Fatal(err)
	}
	return
}
func publicationFacts(t *testing.T, x *ownerTreeFixture, d kc.DocumentRef, body []byte) {
	t.Helper()
	var state, sha, media string
	var size int64
	var refs, published int
	err := x.raw.QueryRow(knowledgeContext(t), `SELECT o.state,encode(o.sha256,'hex'),o.media_type,o.byte_size,
 (SELECT count(*) FROM agenteam_object.object_references r WHERE r.object_id=o.id AND r.owner_kind='knowledge' AND r.owner_id=$2 AND r.kind='canonical'),
 (SELECT count(*) FROM agenteam_object.uploads u WHERE u.object_id=o.id AND u.state='committed' AND u.disposition='attached')
 FROM agenteam_object.objects o WHERE o.id=$1`, d.ObjectID.String(), d.ID.String()).Scan(&state, &sha, &media, &size, &refs, &published)
	sum := sha256.Sum256(body)
	if err != nil || state != "available" || sha != hex.EncodeToString(sum[:]) || media != d.MediaType || size != int64(len(body)) || refs != 1 || published != 1 {
		t.Fatal("real canonical Object/reference/publication facts", err)
	}
}
func publicationRead(t *testing.T, x *ownerTreeFixture, actor id.Actor, d kc.DocumentRef, want []byte) {
	t.Helper()
	read, err := x.service.OpenCanonical(knowledgeContext(t), actor, d.ProjectID, d.ID, nil)
	if err != nil {
		t.Fatal("open canonical", err)
	}
	got, readErr := io.ReadAll(read)
	closeErr := read.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(got, want) {
		t.Fatal("canonical bytes/actual Close", readErr, closeErr)
	}
	titleSameDocument(t, d, read.Document())
}

func TestKnowledgeB02DirectPublication(t *testing.T) {
	x := newPublicationFixture(t)
	t.Run("create_read_replay_noop", func(t *testing.T) {
		actor := x.human(t)
		p := x.project(t, actor, true)
		req := kc.CreateRequest{ProjectID: p, DocumentID: treeID[kc.Document](t), Title: "public text"}
		meta := treeMeta(t)
		body := "first 世界\n"
		d, err := x.service.CreateDocument(knowledgeContext(t), actor, meta, req, publicationText(t, body))
		if err != nil {
			t.Fatal("public Create", err)
		}
		if d.ID != req.DocumentID || d.ProjectID != p || d.ContentVersion != 1 || d.SourceKind != kc.Text || d.Title != req.Title {
			t.Fatal("Create result")
		}
		publicationFacts(t, x, d, []byte(body))
		publicationRead(t, x, actor, d, []byte(body))
		text, err := x.service.ReadDocument(knowledgeContext(t), actor, p, d.ID, kc.DefaultReadRequest())
		if err != nil || text.Text == nil || text.Text.Text != body || text.Text.Truncated || text.Text.NextByteOffset != f.Progress(len(body)) {
			t.Fatal("public text read", err)
		}
		if a, e := publicationCount(t, x, p); a != 1 || e != 1 {
			t.Fatal("create did not commit exactly one Object Audit and Outbox fact")
		}
		at := x.activity(t, actor)
		replay, err := x.service.CreateDocument(knowledgeContext(t), actor, meta, req, publicationText(t, body))
		if err != nil {
			t.Fatal("Create replay", err)
		}
		titleSameDocument(t, d, replay)
		noMeta := titleMeta(t, 1)
		noReq := kc.UpdateRequest{ReplaceSource: true}
		source := publicationText(t, body)
		noop, err := x.service.UpdateDocument(knowledgeContext(t), actor, noMeta, p, d.ID, noReq, &source)
		if err != nil {
			t.Fatal("same content no-op", err)
		}
		titleSameDocument(t, d, noop)
		digest, err := kc.UpdateDigest(actor, noMeta, p, d.ID, noReq, &source)
		if err != nil {
			t.Fatal(err)
		}
		lookup, err := x.service.LookupCommand(knowledgeContext(t), actor, kc.LookupRequest{ProjectID: p, Command: kc.Update, Key: noMeta.IdempotencyKey, SemanticDigest: digest})
		if err != nil || lookup.State != kc.Committed || lookup.Receipt == nil || lookup.Receipt.Changed {
			t.Fatal("no-op confirmed receipt", err)
		}
		if a, e := publicationCount(t, x, p); a != 1 || e != 1 || !x.activity(t, actor).Equal(at) {
			t.Fatal("replay/no-op added facts or Activity")
		}
	})
	t.Run("replace_and_current_owner", func(t *testing.T) {
		actor := x.human(t)
		p := x.project(t, actor, true)
		req := kc.CreateRequest{ProjectID: p, DocumentID: treeID[kc.Document](t), Title: "replace"}
		before, err := x.service.CreateDocument(knowledgeContext(t), actor, treeMeta(t), req, publicationText(t, "old"))
		if err != nil {
			t.Fatal(err)
		}
		body := []byte("%PDF-1.7\n\x00\xff\n%%EOF")
		sum := sha256.Sum256(body)
		source, err := kc.NewUploadSource("application/pdf", f.Progress(len(body)), f.Digest("sha256:"+hex.EncodeToString(sum[:])), io.NopCloser(bytes.NewReader(body)))
		if err != nil {
			t.Fatal(err)
		}
		after, err := x.service.UpdateDocument(knowledgeContext(t), actor, titleMeta(t, 1), p, before.ID, kc.UpdateRequest{ReplaceSource: true}, &source)
		if err != nil {
			t.Fatal("public PDF replacement", err)
		}
		if after.ContentVersion != 2 || after.ObjectID == before.ObjectID || after.SourceKind != kc.File || after.Title != before.Title {
			t.Fatal("replacement result")
		}
		publicationFacts(t, x, after, body)
		publicationRead(t, x, actor, after, body)
		var oldRefs, cleanup int
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT (SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$1),(SELECT count(*) FROM agenteam_knowledge.object_cleanup WHERE project_id=$2 AND object_id=$1)`, before.ObjectID.String(), p.String()).Scan(&oldRefs, &cleanup); err != nil || oldRefs != 0 || cleanup != 1 {
			t.Fatal("old reference release and durable cleanup", err)
		}
		if a, e := publicationCount(t, x, p); a != 2 || e != 2 {
			t.Fatal("replacement facts")
		}
		foreign := x.human(t)
		_, err = x.service.ReadDocument(knowledgeContext(t), foreign, p, after.ID, kc.DefaultReadRequest())
		treeCode(t, err, f.NotFound)
		x.archive(t, p)
		source = publicationText(t, "forbidden")
		_, err = x.service.UpdateDocument(knowledgeContext(t), actor, titleMeta(t, 2), p, after.ID, kc.UpdateRequest{ReplaceSource: true}, &source)
		treeCode(t, err, f.ProjectNotActive)
		publicationRead(t, x, actor, after, body)
		if a, e := publicationCount(t, x, p); a != 2 || e != 2 {
			t.Fatal("rejected replacement added facts")
		}
	})
	t.Run("final_transaction_rollback", func(t *testing.T) {
		actor := x.human(t)
		p := x.project(t, actor, true)
		at := x.activity(t, actor)
		injected := errors.New("direct publication final rollback")
		var activity *titleFailActivity
		s := publicationService(t, x, func(d *knowledge.Dependencies) {
			activity = &titleFailActivity{ActivityAuthority: d.Activity, err: injected}
			d.Activity = activity
		})
		req := kc.CreateRequest{ProjectID: p, DocumentID: treeID[kc.Document](t), Title: "rollback"}
		meta := treeMeta(t)
		_, err := s.CreateDocument(knowledgeContext(t), actor, meta, req, publicationText(t, "rollback bytes"))
		if !errors.Is(err, injected) || activity.calls != 1 {
			t.Fatal("final Activity injection not reached", err)
		}
		_, err = x.service.GetDocument(knowledgeContext(t), actor, p, req.DocumentID)
		treeCode(t, err, f.NotFound)
		if a, e := publicationCount(t, x, p); a != 0 || e != 0 || !x.activity(t, actor).Equal(at) {
			t.Fatal("rolled-back canonical transaction leaked Audit/Outbox/Activity")
		}
		var refs, published int
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT (SELECT count(*) FROM agenteam_object.object_references WHERE owner_kind='knowledge' AND owner_id=$1 AND kind='canonical'),(SELECT count(*) FROM agenteam_object.uploads WHERE owner_kind='knowledge' AND owner_id=$1 AND state='committed')`, req.DocumentID.String()).Scan(&refs, &published); err != nil || refs != 0 || published != 0 {
			t.Fatal("Object publication escaped final rollback", err)
		}
		// This top proves the atomic final failure, not crash/Unknown recovery.
		source := publicationText(t, "rollback bytes")
		digest, err := kc.CreateDigest(actor, meta, req, source)
		if err != nil {
			t.Fatal(err)
		}
		lookup, err := s.LookupCommand(knowledgeContext(t), actor, kc.LookupRequest{ProjectID: p, Command: kc.Create, Key: meta.IdempotencyKey, SemanticDigest: digest})
		if err != nil || lookup.State != kc.InProgress || lookup.Receipt != nil {
			t.Fatal("rollback has a success receipt", err)
		}
	})
}

func publicationBusiness(t *testing.T, d kc.DocumentRef) kc.SourceInput {
	t.Helper()
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: d.ProjectID, DocumentID: d.ID.String(), Revision: d.ContentVersion})
	if err != nil {
		t.Fatal(err)
	}
	source, err := kc.NewBusinessSource(ref)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func publicationSeedContent(t *testing.T, x *ownerTreeFixture, actor id.Actor, p id.ProjectID, body string) kc.DocumentRef {
	t.Helper()
	d, err := x.service.CreateDocument(knowledgeContext(t), actor, treeMeta(t), kc.CreateRequest{ProjectID: p, DocumentID: treeID[kc.Document](t), Title: "source"}, publicationText(t, body))
	if err != nil {
		t.Fatal("real source creation", err)
	}
	return d
}

func publicationSourceRetired(t *testing.T, x *ownerTreeFixture, source oc.ObjectID, target id.ProjectID) {
	t.Helper()
	var total, active, checkpoints int
	err := x.raw.QueryRow(knowledgeContext(t), `SELECT
 (SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='source'),
 (SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='source' AND state='active'),
 (SELECT count(*) FROM agenteam_knowledge.publications WHERE project_id=$2 AND source_lease_id IS NOT NULL)`, source.String(), target.String()).Scan(&total, &active, &checkpoints)
	if err != nil || total < 1 || active != 0 || checkpoints != 0 {
		t.Fatal("actual source lease and checkpoint retirement", err)
	}
}

func TestKnowledgeB02BusinessPublication(t *testing.T) {
	x := newPublicationFixture(t)
	t.Run("exact_revision_copy_and_replay", func(t *testing.T) {
		actor := x.human(t)
		origin, target := x.project(t, actor, true), x.project(t, actor, true)
		original := publicationSeedContent(t, x, actor, origin, "original 世界")
		req := kc.CreateRequest{ProjectID: target, DocumentID: treeID[kc.Document](t), Title: "copied"}
		meta := treeMeta(t)
		copy, err := x.service.CreateDocument(knowledgeContext(t), actor, meta, req, publicationBusiness(t, original))
		if err != nil {
			t.Fatal("public exact business copy", err)
		}
		if copy.ObjectID == original.ObjectID || copy.ProjectID != target || copy.ContentVersion != 1 {
			t.Fatal("business copy did not own new canonical object")
		}
		publicationFacts(t, x, copy, []byte("original 世界"))
		publicationRead(t, x, actor, copy, []byte("original 世界"))
		publicationSourceRetired(t, x, original.ObjectID, target)
		changed := publicationText(t, "new original")
		newOriginal, err := x.service.UpdateDocument(knowledgeContext(t), actor, titleMeta(t, 1), origin, original.ID, kc.UpdateRequest{ReplaceSource: true}, &changed)
		if err != nil || newOriginal.ContentVersion != 2 {
			t.Fatal("actual source revision advance", err)
		}
		replay, err := x.service.CreateDocument(knowledgeContext(t), actor, meta, req, publicationBusiness(t, original))
		if err != nil {
			t.Fatal("completed receipt accessed stale source", err)
		}
		titleSameDocument(t, copy, replay)
		req.DocumentID = treeID[kc.Document](t)
		_, err = x.service.CreateDocument(knowledgeContext(t), actor, treeMeta(t), req, publicationBusiness(t, original))
		treeCode(t, err, f.VersionConflict)
		if a, e := publicationCount(t, x, target); a != 1 || e != 1 {
			t.Fatal("stale source/new command produced canonical facts")
		}
	})
	t.Run("reuse_title_and_replacement", func(t *testing.T) {
		actor := x.human(t)
		origin, target := x.project(t, actor, true), x.project(t, actor, true)
		original := publicationSeedContent(t, x, actor, origin, "equal")
		before := publicationSeedContent(t, x, actor, target, "equal")
		source := publicationBusiness(t, original)
		meta := titleMeta(t, 1)
		request := kc.UpdateRequest{ReplaceSource: true}
		at := x.activity(t, actor)
		noop, err := x.service.UpdateDocument(knowledgeContext(t), actor, meta, target, before.ID, request, &source)
		if err != nil {
			t.Fatal("business no-op", err)
		}
		titleSameDocument(t, before, noop)
		digest, err := kc.UpdateDigest(actor, meta, target, before.ID, request, &source)
		if err != nil {
			t.Fatal(err)
		}
		lookup, err := x.service.LookupCommand(knowledgeContext(t), actor, kc.LookupRequest{ProjectID: target, Command: kc.Update, Key: meta.IdempotencyKey, SemanticDigest: digest})
		if err != nil || lookup.Receipt == nil || lookup.Receipt.Changed || lookup.State != kc.Committed {
			t.Fatal("business no-op receipt", err)
		}
		if a, e := publicationCount(t, x, target); a != 1 || e != 1 || !x.activity(t, actor).Equal(at) {
			t.Fatal("no-op published extra fact")
		}
		title := "same object new title"
		source = publicationBusiness(t, original)
		renamed, err := x.service.UpdateDocument(knowledgeContext(t), actor, titleMeta(t, 1), target, before.ID, kc.UpdateRequest{Title: &title, ReplaceSource: true}, &source)
		if err != nil || renamed.ContentVersion != 2 || renamed.ObjectID != before.ObjectID || renamed.Title != title {
			t.Fatal("source-backed title reuse", err)
		}
		if a, e := publicationCount(t, x, target); a != 1 || e != 2 {
			t.Fatal("reuse uploaded another object or omitted content event")
		}
		publicationSourceRetired(t, x, original.ObjectID, target)
		other := publicationSeedContent(t, x, actor, origin, "different body")
		source = publicationBusiness(t, other)
		after, err := x.service.UpdateDocument(knowledgeContext(t), actor, titleMeta(t, 2), target, before.ID, kc.UpdateRequest{ReplaceSource: true}, &source)
		if err != nil || after.ContentVersion != 3 || after.ObjectID == before.ObjectID {
			t.Fatal("business replacement with both source and cleanup plans", err)
		}
		publicationFacts(t, x, after, []byte("different body"))
		publicationRead(t, x, actor, after, []byte("different body"))
		publicationSourceRetired(t, x, other.ObjectID, target)
		var refs int
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$1`, before.ObjectID.String()).Scan(&refs); err != nil || refs != 0 {
			t.Fatal("source plan displaced old reference cleanup plan", err)
		}
		if a, e := publicationCount(t, x, target); a != 2 || e != 3 {
			t.Fatal("replacement canonical facts")
		}
	})
	t.Run("source_revision_rechecked_in_final", func(t *testing.T) {
		actor := x.human(t)
		origin, target := x.project(t, actor, true), x.project(t, actor, true)
		original := publicationSeedContent(t, x, actor, origin, "before final")
		var hook *titleAfterPrepare
		s := publicationService(t, x, func(d *knowledge.Dependencies) {
			hook = &titleAfterPrepare{Appender: d.Outbox, after: func() {
				source := publicationText(t, "changed between plan and final")
				_, err := x.service.UpdateDocument(knowledgeContext(t), actor, titleMeta(t, 1), origin, original.ID, kc.UpdateRequest{ReplaceSource: true}, &source)
				if err != nil {
					t.Fatal("actual source mutation between plans and final", err)
				}
			}}
			d.Outbox = hook
		})
		req := kc.CreateRequest{ProjectID: target, DocumentID: treeID[kc.Document](t), Title: "must stay absent"}
		_, err := s.CreateDocument(knowledgeContext(t), actor, treeMeta(t), req, publicationBusiness(t, original))
		treeCode(t, err, f.VersionConflict)
		if hook.calls != 1 {
			t.Fatal("source changed before the actual final planning boundary")
		}
		_, err = x.service.GetDocument(knowledgeContext(t), actor, target, req.DocumentID)
		treeCode(t, err, f.NotFound)
		if a, e := publicationCount(t, x, target); a != 0 || e != 0 {
			t.Fatal("stale final source published canonical facts")
		}
		publicationSourceRetired(t, x, original.ObjectID, target)
	})
	t.Run("source_owner_and_archived_read", func(t *testing.T) {
		actor, other := x.human(t), x.human(t)
		origin, target, foreign := x.project(t, actor, true), x.project(t, actor, true), x.project(t, other, true)
		unowned := publicationSeedContent(t, x, other, foreign, "private source")
		req := kc.CreateRequest{ProjectID: target, DocumentID: treeID[kc.Document](t), Title: "owner gate"}
		_, err := x.service.CreateDocument(knowledgeContext(t), actor, treeMeta(t), req, publicationBusiness(t, unowned))
		treeCode(t, err, f.NotFound)
		var leases int
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='source'`, unowned.ObjectID.String()).Scan(&leases); err != nil || leases != 0 {
			t.Fatal("non-owner acquired source lease", err)
		}
		original := publicationSeedContent(t, x, actor, origin, "archived source")
		x.archive(t, origin)
		req.DocumentID = treeID[kc.Document](t)
		copy, err := x.service.CreateDocument(knowledgeContext(t), actor, treeMeta(t), req, publicationBusiness(t, original))
		if err != nil {
			t.Fatal("archived current Owner source read", err)
		}
		publicationRead(t, x, actor, copy, []byte("archived source"))
		publicationSourceRetired(t, x, original.ObjectID, target)
	})
}
