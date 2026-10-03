package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const testID = "01900000-0000-7000-8000-000000000001"
const secondID = "01900000-0000-7000-8000-000000000002"

func TestOwnerPartitionsCurrentGrantAndNoProspectiveBoolean(t *testing.T) {
	user, _ := foundation.ParseID[identity.User](testID)
	session, _ := foundation.ParseID[identity.Session](secondID)
	actor, _ := identity.NewHuman(user, session)
	avatar, err := NewObjectOwner(Avatar, testID, "")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := NewObjectOwner(Avatar, secondID, "")
	if avatar.Scope().Details().Kind != identity.System || avatar.Partition() == other.Partition() {
		t.Fatal("Avatar partitions merged")
	}
	project, _ := NewObjectOwner(Artifact, secondID, testID)
	if project.Scope().Details().ProjectID != testID || project.Partition() != testID {
		t.Fatal("Project partition lost")
	}
	for _, c := range []struct {
		k     OwnerKind
		id, p string
	}{{"raw_object", testID, testID}, {Avatar, testID, testID}, {Artifact, testID, ""}, {Knowledge, "raw", testID}} {
		if _, e := NewObjectOwner(c.k, c.id, c.p); e == nil {
			t.Fatal("invalid owner accepted")
		}
	}
	d := OwnerAuthorizationDetails{Actor: actor, Owner: avatar, Intent: identity.Mutate, Existence: ProspectiveOwner, CreationCause: testID, Version: 1}
	grant, err := NewOwnerAuthorization(d)
	if err != nil || !grant.Matches(actor, avatar, identity.Mutate) || grant.Matches(actor, other, identity.Mutate) || grant.Matches(actor, avatar, identity.Read) {
		t.Fatal("grant binding failed")
	}
	d.CreationCause = ""
	if _, e := NewOwnerAuthorization(d); e == nil {
		t.Fatal("prospective creation lacked a durable cause")
	}
	d.CreationCause = testID
	d.Owner = other
	if _, e := NewOwnerAuthorization(d); e == nil {
		t.Fatal("other Avatar user acquired an Owner grant")
	}
	for _, target := range []any{new(ObjectOwner), new(OwnerAuthorization), new(LeaseOwner), new(UploadReceipt), new(PreparedPayload), new(UploadAttempt), new(ObjectCleanupCause), new(ProjectCleanupCause)} {
		if json.Unmarshal([]byte(`{"verified":true,"exists":true,"admin":true}`), target) == nil {
			t.Fatal("client forged a trusted handle")
		}
	}
}
func TestRangesAndMediaTypeStrictBoundaries(t *testing.T) {
	for _, c := range []struct {
		r      ByteRange
		n      int64
		length int64
		ok     bool
	}{{ByteRange{0, 4}, 4, 4, true}, {ByteRange{2, math.MaxInt64 - 2}, 4, 2, true}, {ByteRange{0, 1}, 0, 0, false}, {ByteRange{4, 1}, 4, 0, false}, {ByteRange{-1, 1}, 4, 0, false}, {ByteRange{1, 0}, 4, 0, false}, {ByteRange{1, math.MaxInt64}, 4, 0, false}} {
		got, e := c.r.Resolve(c.n)
		if (e == nil) != c.ok || e == nil && int64(got.Length) != c.length {
			t.Fatal("range boundary failed")
		}
	}
	got, e := NormalizeMediaType("TEXT/PLAIN; CHARSET=utf-8")
	if e != nil || got != "text/plain; charset=utf-8" {
		t.Fatal("MIME canonicalization failed")
	}
	for _, m := range []string{"", "text/plain\r\nx: secret", "a/", strings.Repeat("a", 257)} {
		if _, e := NormalizeMediaType(m); e == nil {
			t.Fatal("invalid MIME accepted")
		}
	}
}

func TestBusinessSourceVariantsRejectRawObjectAndMismatchedRevision(t *testing.T) {
	project, _ := foundation.ParseID[identity.Project](testID)
	for _, d := range []BusinessFileDetails{
		{Kind: "object_id", ProjectID: project, FileID: secondID},
		{Kind: UploadedObject, FileID: secondID},
		{Kind: ArtifactFile, ProjectID: project, ArtifactID: testID},
		{Kind: KnowledgeFile, ProjectID: project, DocumentID: testID},
		{Kind: ExecutionFile, ProjectID: project, ExecutionID: testID, PayloadID: secondID, Revision: 1},
	} {
		if _, e := NewBusinessFileRef(d); e == nil {
			t.Fatal("unqualified or mixed source reference accepted")
		}
	}
	ref, e := NewBusinessFileRef(BusinessFileDetails{Kind: KnowledgeFile, ProjectID: project, DocumentID: secondID, Revision: 2})
	if e != nil {
		t.Fatal(e)
	}
	owner, _ := NewObjectOwner(Knowledge, secondID, testID)
	id, _ := foundation.ParseID[StoredObject](secondID)
	now, _ := foundation.NewInstant(time.Now())
	meta := ObjectMeta{id, owner.Scope(), "text/plain", 0, foundation.Digest("sha256:" + strings.Repeat("a", 64)), Available, 1, now}
	if _, e = NewResolvedSource(ResolvedSourceDetails{ref, owner, meta, 1}); e == nil {
		t.Fatal("source silently changed requested revision")
	}
	if _, e = NewResolvedSource(ResolvedSourceDetails{ref, owner, meta, 2}); e != nil {
		t.Fatal(e)
	}
	if json.Unmarshal([]byte(`{"kind":"object_id","id":"`+testID+`"}`), new(BusinessFileRef)) == nil {
		t.Fatal("raw wire reference bypassed constructor")
	}
}

type sensitiveReader struct{ raw string }

func (r sensitiveReader) Read([]byte) (int, error) { return 0, io.EOF }
func (r sensitiveReader) Close() error             { return nil }
func TestOpaqueHandlesKeepPrivateReadersAndLookupMaterialOutOfProjection(t *testing.T) {
	id, _ := foundation.ParseID[StoredObject](testID)
	upload, _ := foundation.ParseID[Upload](testID)
	receiptID, _ := foundation.ParseID[Receipt](testID)
	payloadID, _ := foundation.ParseID[Payload](testID)
	owner, _ := NewObjectOwner(Artifact, secondID, testID)
	digest := foundation.Digest("sha256:" + strings.Repeat("a", 64))
	receipt, e := NewUploadReceipt(ReceiptDetails{receiptID, upload, id, owner, testID})
	if e != nil {
		t.Fatal(e)
	}
	payload, e := NewPreparedPayload(PreparedDetails{payloadID, "text/plain", 0, digest})
	if e != nil {
		t.Fatal(e)
	}
	now, _ := foundation.NewInstant(time.Now())
	meta := ObjectMeta{id, owner.Scope(), "text/plain", 0, digest, Available, 1, now}
	reader, e := NewObjectReader(meta, nil, sensitiveReader{"private-locator-secret-canary"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := NewObjectReader(meta, &ResolvedRange{Offset: 0, Length: 1, Total: 0}, sensitiveReader{}); e == nil {
		t.Fatal("empty object acquired an impossible range")
	}
	pending := meta
	pending.State = Pending
	if _, e := NewObjectReader(pending, nil, sensitiveReader{}); e == nil {
		t.Fatal("pending object acquired a readable body")
	}
	for _, value := range []any{receipt, payload, reader, struct{ private any }{reader}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(format, value), "private-locator-secret-canary") {
				t.Fatal("recursive formatting exposed storage")
			}
		}
		raw, _ := json.Marshal(value)
		if strings.Contains(string(raw), "private-locator-secret-canary") {
			t.Fatal("JSON exposed storage")
		}
		for _, text := range []bool{false, true} {
			var b bytes.Buffer
			var h slog.Handler = slog.NewJSONHandler(&b, nil)
			if text {
				h = slog.NewTextHandler(&b, nil)
			}
			slog.New(h).LogAttrs(context.Background(), slog.LevelInfo, "event", slog.Any("value", value))
			if strings.Contains(b.String(), "private-locator-secret-canary") {
				t.Fatal("log exposed storage")
			}
		}
	}
	if reader.Meta().ID != id || reader.Range() != nil {
		t.Fatal("safe reader metadata unavailable")
	}
	if _, e = NewPreparedPayload(PreparedDetails{payloadID, "text/plain", MaxObjectSize + 1, digest}); e == nil {
		t.Fatal("oversized payload handle accepted")
	}
}

func TestExactProtectedReadGrantAndImmutableLease(t *testing.T) {
	user, _ := foundation.ParseID[identity.User](testID)
	session, _ := foundation.ParseID[identity.Session](secondID)
	actor, _ := identity.NewHuman(user, session)
	owner, _ := NewObjectOwner(Knowledge, testID, secondID)
	object, _ := foundation.ParseID[StoredObject](testID)
	other, _ := foundation.ParseID[StoredObject](secondID)
	leaseID, _ := foundation.ParseID[Lease](testID)
	leaseOwner, _ := NewLeaseOwner(HistoryOwner, secondID)
	lease := ObjectLease{ID: leaseID, ObjectID: object, Owner: leaseOwner}
	d := OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: identity.Read, Existence: ExistingOwner, Version: 1, ReadObjectID: object, ProtectedLease: &lease}
	g, e := NewOwnerAuthorization(d)
	if e != nil || !g.MatchesObjectRead(actor, owner, object) || g.MatchesObjectRead(actor, owner, other) {
		t.Fatal("exact read binding failed")
	}
	lease.ObjectID = other
	returned := g.Details()
	returned.ProtectedLease.ID = LeaseID{}
	if g.Details().ProtectedLease.ObjectID != object || g.Details().ProtectedLease.ID != leaseID {
		t.Fatal("external mutation changed protected read authority")
	}
	done := make(chan struct{}, 20)
	for range 20 {
		go func() {
			for range 100 {
				projected := g.Details()
				projected.ProtectedLease.ObjectID = other
			}
			done <- struct{}{}
		}()
	}
	for range 20 {
		<-done
	}
	if !g.MatchesObjectRead(actor, owner, object) || g.Details().ProtectedLease.ObjectID != object {
		t.Fatal("concurrent mutation changed grant")
	}
	base := g.Details()
	for _, change := range []func(*OwnerAuthorizationDetails){
		func(d *OwnerAuthorizationDetails) { d.ReadObjectID = ObjectID{} },
		func(d *OwnerAuthorizationDetails) { d.ReadObjectID = other },
		func(d *OwnerAuthorizationDetails) { d.Intent = identity.Mutate },
		func(d *OwnerAuthorizationDetails) { d.Existence = ProspectiveOwner; d.CreationCause = testID },
		func(d *OwnerAuthorizationDetails) { d.ProtectedLease.ID = LeaseID{} },
		func(d *OwnerAuthorizationDetails) { d.ProtectedLease.Owner = LeaseOwner{} },
		func(d *OwnerAuthorizationDetails) { d.ProtectedLease.Owner, _ = NewLeaseOwner(ReaderOwner, secondID) },
		func(d *OwnerAuthorizationDetails) { d.ProtectedLease.Owner, _ = NewLeaseOwner(SourceOwner, secondID) },
		func(d *OwnerAuthorizationDetails) { d.ProtectedLease.Owner, _ = NewLeaseOwner(WriterOwner, secondID) },
	} {
		candidate := copyAuthorization(base)
		change(&candidate)
		if _, e := NewOwnerAuthorization(candidate); e == nil {
			t.Fatal("invalid protected grant accepted")
		}
	}
	for _, kind := range []LeaseOwnerKind{ExecutionOwner, HistoryOwner, TransferOwner} {
		candidate := copyAuthorization(base)
		candidate.ProtectedLease.Owner, _ = NewLeaseOwner(kind, secondID)
		if _, e := NewOwnerAuthorization(candidate); e != nil {
			t.Fatal("stable protected use rejected")
		}
	}
	base.ProtectedLease = nil
	if g, e := NewOwnerAuthorization(base); e != nil || !g.MatchesObjectRead(actor, owner, object) {
		t.Fatal("canonical exact grant rejected")
	}
	base.ReadObjectID = ObjectID{}
	g, e = NewOwnerAuthorization(base)
	if e != nil || g.MatchesObjectRead(actor, owner, object) {
		t.Fatal("generic owner grant treated as exact object grant")
	}
}

type nilReaderFunc func([]byte) (int, error)

func (f nilReaderFunc) Read(p []byte) (int, error) { return f(p) }
func (f nilReaderFunc) Close() error               { return nil }

type nilReaderMap map[string]string

func (m nilReaderMap) Read([]byte) (int, error) { panic("nil map reader called") }
func (m nilReaderMap) Close() error             { return nil }

type nilReaderSlice []byte

func (s nilReaderSlice) Read([]byte) (int, error) { panic("nil slice reader called") }
func (s nilReaderSlice) Close() error             { return nil }

type nilReaderChannel chan byte

func (c nilReaderChannel) Read([]byte) (int, error) { panic("nil channel reader called") }
func (c nilReaderChannel) Close() error             { return nil }
func TestReaderRejectsJSONAndEveryNilableBody(t *testing.T) {
	id, _ := foundation.ParseID[StoredObject](testID)
	now, _ := foundation.NewInstant(time.Now())
	meta := ObjectMeta{id, identity.SystemScope(), "text/plain", 1, foundation.Digest("sha256:" + strings.Repeat("a", 64)), Available, 1, now}
	for _, body := range []io.ReadCloser{nil, (*io.PipeReader)(nil), nilReaderFunc(nil), nilReaderMap(nil), nilReaderSlice(nil), nilReaderChannel(nil)} {
		if _, e := NewObjectReader(meta, nil, body); e == nil {
			t.Fatal("typed nil body accepted")
		}
	}
	for _, raw := range []string{`{}`, `null`, `{"verified":true}`, `"object_reader"`} {
		r, e := NewObjectReader(meta, nil, io.NopCloser(strings.NewReader("X")))
		if e != nil {
			t.Fatal(e)
		}
		if json.Unmarshal([]byte(raw), r) == nil {
			t.Fatal("opaque body accepted JSON")
		}
		b, e := io.ReadAll(r)
		if e != nil || string(b) != "X" {
			t.Fatal("rejected input changed body")
		}
		_ = r.Close()
	}
}
