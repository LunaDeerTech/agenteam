//go:build integration

package knowledge_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ev "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// These helpers observe actual services. They do not create canonical rows,
// claims, receipts, authorization issuers, or private witnesses.
type independentBody struct {
	*bytes.Reader
	bytes, eof, closes int
}

func (b *independentBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.bytes += n
	if err == io.EOF {
		b.eof++
	}
	return n, err
}
func (b *independentBody) Close() error { b.closes++; return nil }

func independentUpload(t *testing.T, media string, raw []byte, length int, digest f.Digest) (kc.SourceInput, *independentBody) {
	t.Helper()
	body := &independentBody{Reader: bytes.NewReader(raw)}
	source, err := kc.NewUploadSource(media, f.Progress(length), digest, body)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	return source, body
}
func independentDigest(raw []byte) f.Digest {
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

// A fixed, valid OPC archive. Binary ZIP bytes (including NUL and non-UTF8)
// are the canonical input; this test makes no parser/chunker assertion.
func independentDOCX(t *testing.T) []byte {
	t.Helper()
	var raw bytes.Buffer
	w := zip.NewWriter(&raw)
	for _, part := range []struct{ name, body string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>独立原字节 / exact canonical source</w:t></w:r></w:p></w:body></w:document>`},
	} {
		entry, err := w.CreateHeader(&zip.FileHeader{Name: part.name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.WriteString(entry, part.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return raw.Bytes()
}

type independentFacts struct{ documents, canonical, committedUploads, audits, events, completed int }

func independentSnapshot(t *testing.T, x *ownerTreeFixture, p id.ProjectID) independentFacts {
	t.Helper()
	var out independentFacts
	err := x.raw.QueryRow(knowledgeContext(t), `SELECT
 (SELECT count(*) FROM agenteam_knowledge.documents WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_object.object_references WHERE partition_id=$1 AND owner_kind='knowledge' AND kind='canonical'),
 (SELECT count(*) FROM agenteam_object.uploads WHERE project_id=$1 AND owner_kind='knowledge' AND state='committed'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_knowledge.commands WHERE project_id=$1 AND state='completed')`, p.String()).Scan(&out.documents, &out.canonical, &out.committedUploads, &out.audits, &out.events, &out.completed)
	if err != nil {
		t.Fatal("independent durable facts", err)
	}
	return out
}

func independentJoined(t *testing.T, x *ownerTreeFixture, s *knowledge.Service, p id.ProjectID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := s.Drain(ctx); err != nil {
		t.Fatal("original Knowledge calls did not actually join", err)
	}
	var active int
	if err := x.raw.QueryRow(ctx, `SELECT count(*) FROM agenteam_knowledge.work_claims WHERE project_id=$1 AND phase='active'`, p.String()).Scan(&active); err != nil || active != 0 {
		t.Fatal("original publication work not retired", active, err)
	}
}

func independentRead(t *testing.T, x *ownerTreeFixture, actor id.Actor, doc kc.DocumentRef, want []byte) {
	t.Helper()
	publicationRead(t, x, actor, doc, want)
	var active int
	if err := x.raw.QueryRow(knowledgeContext(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='reader' AND state='active'`, doc.ObjectID.String()).Scan(&active); err != nil || active != 0 {
		t.Fatal("canonical reader Close left an active exact lease", active, err)
	}
	independentJoined(t, x, x.service, doc.ProjectID)
}

// The real Outbox plan has already returned and D05 bytes have been sent.
// The hook changes only the declared interleaving before the final Tx.
type independentAfterPlan struct {
	ob.Appender
	after func()
	calls int
}

func (p *independentAfterPlan) PrepareAppend(ctx context.Context, actor id.Actor, event ev.Event) (ob.AppendPlan, error) {
	plan, err := p.Appender.PrepareAppend(ctx, actor, event)
	if err == nil {
		p.calls++
		p.after()
	}
	return plan, err
}

func independentCause(t *testing.T) f.TransactionCause {
	t.Helper()
	cause, err := f.NewRecoveryCause("knowledge.independent", knowledgeID(t), "")
	if err != nil {
		t.Fatal(err)
	}
	return cause
}

func independentRevoke(t *testing.T, x *ownerTreeFixture, actor id.Actor) {
	t.Helper()
	key, err := f.UserLock(actor.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	result := x.raw.WithinTx(knowledgeContext(t), independentCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := x.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
			return err
		}
		sql, err := x.raw.InTx(tx)
		if err != nil {
			return err
		}
		tag, err := sql.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, actor.Details().SessionID, actor.Details().UserID)
		if err == nil && tag.RowsAffected() != 1 {
			return f.NewFault(f.InvalidState, f.NotCommitted)
		}
		return err
	})
	if result.State() != f.Committed {
		t.Fatal("locked upstream Session revocation", result.Fault())
	}
}

type independentAuditCapture struct {
	ac.Appender
	entry ac.Entry
	key   ac.AppendKey
	calls int
}

func (a *independentAuditCapture) AppendInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) (ac.AppendReceipt, error) {
	receipt, err := a.Appender.AppendInTx(ctx, tx, entry, key)
	if err == nil {
		a.entry, a.key = entry, key
		a.calls++
	}
	return receipt, err
}

func TestKnowledgeB02IndependentContent(t *testing.T) {
	x := newPublicationFixture(t)
	t.Run("docx_original_bytes_and_measured_rejections", func(t *testing.T) {
		actor := x.human(t)
		p := x.project(t, actor, true)
		raw := independentDOCX(t)
		beforeActivity := x.activity(t, actor)
		input, body := independentUpload(t, kc.DOCX, raw, len(raw), independentDigest(raw))
		meta := treeMeta(t)
		request := kc.CreateRequest{ProjectID: p, DocumentID: treeID[kc.Document](t), Title: "binary DOCX"}
		doc, err := x.service.CreateDocument(knowledgeContext(t), actor, meta, request, input)
		if err != nil || doc.SourceKind != kc.File || doc.MediaType != kc.DOCX || body.bytes != len(raw) || body.eof != 1 || body.closes != 1 {
			t.Fatal("actual DOCX stream/publication", err)
		}
		publicationFacts(t, x, doc, raw)
		independentRead(t, x, actor, doc, raw)
		digest, err := kc.CreateDigest(actor, meta, request, input)
		if err != nil {
			t.Fatal(err)
		}
		lookup, err := x.service.LookupCommand(knowledgeContext(t), actor, kc.LookupRequest{ProjectID: p, Command: kc.Create, Key: meta.IdempotencyKey, SemanticDigest: digest})
		if err != nil || lookup.State != kc.Committed || lookup.Receipt == nil || lookup.Receipt.Document == nil || !lookup.Receipt.Changed {
			t.Fatal("DOCX actual receipt", err)
		}
		titleSameDocument(t, doc, *lookup.Receipt.Document)
		want := independentFacts{documents: 1, canonical: 1, committedUploads: 1, audits: 1, events: 1, completed: 1}
		if independentSnapshot(t, x, p) != want || !x.activity(t, actor).After(beforeActivity) {
			t.Fatal("DOCX canonical/Audit/Event/receipt/Activity facts")
		}
		for _, tc := range []struct {
			name   string
			length int
			sha    f.Digest
		}{{"declared_short", len(raw) - 1, independentDigest(raw)}, {"declared_long", len(raw) + 1, independentDigest(raw)}, {"wrong_sha", len(raw), independentDigest([]byte("different original bytes"))}} {
			input, body := independentUpload(t, kc.DOCX, raw, tc.length, tc.sha)
			meta := treeMeta(t)
			request.DocumentID = treeID[kc.Document](t)
			at := x.activity(t, actor)
			_, err := x.service.CreateDocument(knowledgeContext(t), actor, meta, request, input)
			treeCode(t, err, f.InvalidArgument)
			if body.closes != 1 || body.bytes == 0 || independentSnapshot(t, x, p) != want || !x.activity(t, actor).Equal(at) {
				t.Fatal(tc.name, "measured rejection changed published facts or lost original Close")
			}
			digest, err := kc.CreateDigest(actor, meta, request, input)
			if err != nil {
				t.Fatal(err)
			}
			lookup, err := x.service.LookupCommand(knowledgeContext(t), actor, kc.LookupRequest{ProjectID: p, Command: kc.Create, Key: meta.IdempotencyKey, SemanticDigest: digest})
			if err != nil || lookup.State != kc.InProgress || lookup.Receipt != nil {
				t.Fatal(tc.name, "must retain only original planned command", err)
			}
			independentJoined(t, x, x.service, p)
		}
	})
	t.Run("sent_upload_rechecks_current_session_before_final", func(t *testing.T) {
		actor := x.human(t)
		p := x.project(t, actor, true)
		meta := treeMeta(t)
		request := kc.CreateRequest{ProjectID: p, DocumentID: treeID[kc.Document](t), Title: "revoked after actual send"}
		raw := []byte("binary final authorization\x00\xff")
		input, body := independentUpload(t, kc.DOCX, raw, len(raw), independentDigest(raw))
		before, at := independentSnapshot(t, x, p), x.activity(t, actor)
		var hook *independentAfterPlan
		s := publicationService(t, x, func(deps *knowledge.Dependencies) {
			hook = &independentAfterPlan{Appender: deps.Outbox, after: func() {
				var sent int
				if err := x.raw.QueryRow(knowledgeContext(t), `SELECT count(*) FROM agenteam_knowledge.publications p JOIN agenteam_knowledge.commands c ON c.id=p.command_id WHERE c.project_id=$1 AND c.command_key=$2 AND c.state='planned' AND p.phase='uploaded'`, p.String(), string(meta.IdempotencyKey)).Scan(&sent); err != nil || sent != 1 || body.closes != 1 {
					t.Fatal("did not reach actual sent-and-closed pre-final boundary", sent, err)
				}
				independentRevoke(t, x, actor)
			}}
			deps.Outbox = hook
		})
		_, err := s.CreateDocument(knowledgeContext(t), actor, meta, request, input)
		treeCode(t, err, f.SessionRevoked)
		if hook.calls != 1 || body.closes != 1 || body.bytes != len(raw) || independentSnapshot(t, x, p) != before || !x.activity(t, actor).Equal(at) {
			t.Fatal("current revoked final gate admitted a durable publication")
		}
		independentJoined(t, x, s, p)
		user, err := f.ParseID[id.User](actor.Details().UserID)
		if err != nil {
			t.Fatal(err)
		}
		current := x.session(t, user)
		digest, err := kc.CreateDigest(actor, meta, request, input)
		if err != nil {
			t.Fatal(err)
		}
		lookup, err := x.service.LookupCommand(knowledgeContext(t), current, kc.LookupRequest{ProjectID: p, Command: kc.Create, Key: meta.IdempotencyKey, SemanticDigest: digest})
		if err != nil || lookup.State != kc.InProgress || lookup.Receipt != nil {
			t.Fatal("revoked final invented a canonical receipt", err)
		}
		// A new valid Session of the same User can resume the original command;
		// the rejected request's source is never silently reopened or reused.
		retry, freshBody := independentUpload(t, kc.DOCX, raw, len(raw), independentDigest(raw))
		doc, err := x.service.CreateDocument(knowledgeContext(t), current, meta, request, retry)
		if err != nil || doc.ID != request.DocumentID || freshBody.closes != 1 {
			t.Fatal("authorized original-command recovery", err)
		}
		publicationFacts(t, x, doc, raw)
		independentRead(t, x, current, doc, raw)
		if got := independentSnapshot(t, x, p); got != (independentFacts{documents: 1, canonical: 1, committedUploads: 1, audits: 1, events: 1, completed: 1}) {
			t.Fatal("recovery did not produce exactly one canonical fact")
		}
	})
	t.Run("public_audit_and_event_fields_are_not_private_authority", func(t *testing.T) {
		actor := x.human(t)
		p := x.project(t, actor, true)
		doc := publicationSeedContent(t, x, actor, p, "actual publication authority control")
		var header, payload []byte
		if err := x.raw.QueryRow(knowledgeContext(t), `SELECT header,payload FROM agenteam_outbox.events WHERE project_id=$1`, p.String()).Scan(&header, &payload); err != nil {
			t.Fatal(err)
		}
		h, err := ev.DecodeHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		var value kc.ContentChangedPayload
		if err = json.Unmarshal(payload, &value); err != nil {
			t.Fatal(err)
		}
		original, err := x.deps.Events.ContentChanged(h, value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = x.deps.Outbox.PrepareAppend(knowledgeContext(t), actor, original); err != nil {
			t.Fatal("real original event is not a valid positive control", err)
		}
		before, at := independentSnapshot(t, x, p), x.activity(t, actor)
		h.EventID = treeID[ev.EventIdentity](t)
		forged, err := x.deps.Events.ContentChanged(h, value)
		if err != nil {
			t.Fatal("negative event must retain valid public shape", err)
		}
		_, err = x.deps.Outbox.PrepareAppend(knowledgeContext(t), actor, forged)
		treeCode(t, err, f.Forbidden)
		if independentSnapshot(t, x, p) != before || !x.activity(t, actor).Equal(at) {
			t.Fatal("unplanned event fields created facts")
		}
		capture := &independentAuditCapture{Appender: x.deps.Audit}
		s := publicationService(t, x, func(d *knowledge.Dependencies) { d.Audit = capture })
		preview, err := s.PrepareDeleteSubtree(knowledgeContext(t), actor, p, doc.ID)
		if err != nil {
			t.Fatal(err)
		}
		meta := treeMeta(t)
		deleted, err := s.DeleteSubtree(knowledgeContext(t), actor, meta, p, doc.ID, preview.Confirmation)
		if err != nil || len(deleted.DeletedIDs) != 1 || capture.calls != 1 || capture.entry.Validate() != nil || capture.key.Validate() != nil {
			t.Fatal("real delete private witness positive control", err)
		}
		before, at = independentSnapshot(t, x, p), x.activity(t, actor)
		identity, err := kc.CommandIdentity(p, kc.DeleteSubtree, meta.IdempotencyKey)
		if err != nil {
			t.Fatal(err)
		}
		user, _ := f.UserLock(actor.Details().UserID)
		project, _ := f.ProjectLock(p.String())
		tree, _ := f.KnowledgeTreeLock(p.String())
		command, _ := f.CommandLock(identity)
		locks, err := ob.NormalizeLocks([]f.LockRequest{{Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared}, {Key: tree, Mode: f.Exclusive}, {Key: command, Mode: f.Exclusive}})
		if err != nil {
			t.Fatal(err)
		}
		result := x.raw.WithinTx(knowledgeContext(t), independentCause(t), func(ctx context.Context, tx f.Tx) error {
			if err := x.raw.AcquireAll(ctx, tx, locks); err != nil {
				return err
			}
			_, err := x.deps.Audit.AppendInTx(ctx, tx, capture.entry, capture.key)
			return err
		})
		if result.State() != f.NotCommitted {
			t.Fatal("public fields replayed Audit without original private context")
		}
		treeCode(t, result.Fault(), f.Forbidden)
		if independentSnapshot(t, x, p) != before || !x.activity(t, actor).Equal(at) {
			t.Fatal("missing-witness attempt changed actual facts")
		}
		independentJoined(t, x, s, p)
	})
}
