package knowledge

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type sourceObjects struct{ oc.Objects }
type sourceRow struct{ values []any }

func (r sourceRow) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		return errors.New("unexpected row shape")
	}
	for j, v := range r.values {
		reflect.ValueOf(dest[j]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

func TestKnowledgeSourceRequiresExactRevisionCurrentOwnerAndStore(t *testing.T) {
	_, actor, q := queryFixture(t)
	document, object, upload := newID[kc.Document](t), newID[oc.StoredObject](t), newID[oc.Upload](t)
	user, _ := f.ParseID[id.User](actor.Details().UserID)
	now, _ := f.NewInstant(time.Now())
	project := pc.ProjectRef{ID: q.project, OwnerUserID: user, Name: "Knowledge", NormalizedName: "knowledge", Lifecycle: pc.Active, Version: 1, CreatedAt: now, UpdatedAt: now}
	grant, err := pc.NewProjectAccess(actor, project, now)
	if err != nil {
		t.Fatal(err)
	}
	gate := &ownerGate{grant: grant}
	store := &authorityStore{tx: f.NewTx()}
	text, kind, media, indexing := "source", "text", kc.PlainText, "pending"
	objectID, uploadID, userID, at := object.String(), upload.String(), user.String(), now.Time()
	store.row = sourceRow{values: []any{document.String(), q.project.String(), (*string)(nil), &text, int64(2), &kind, &media, &objectID, &uploadID, "active", &indexing, &userID, &at, &at, (*time.Time)(nil)}}
	a, err := NewAuthority(store, gate)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := NewSourceResolver(&authorityStore{}, a, &sourceObjects{}); r != nil || err == nil {
		t.Fatal("foreign Store composed")
	}
	r, err := NewSourceResolver(store, a, &sourceObjects{})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: q.project, DocumentID: document.String(), Revision: 2})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := r.sourceInTx(context.Background(), store.tx, actor, ref)
	if err != nil || doc.ObjectID != object || doc.ContentVersion != 2 || gate.tx != store.tx {
		t.Fatal("exact source failed", err)
	}
	old, _ := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: q.project, DocumentID: document.String(), Revision: 1})
	if _, err = r.sourceInTx(context.Background(), store.tx, actor, old); err == nil {
		t.Fatal("source silently advanced to latest")
	}
	gate.err = fault(f.Forbidden)
	before := store.queries
	if _, err = r.sourceInTx(context.Background(), store.tx, actor, ref); err != gate.err || store.queries != before {
		t.Fatal("source read after current Owner revoked")
	}
	external, _ := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.ExecutionFile, ProjectID: q.project, ExecutionID: newID[id.Execution](t).String(), PayloadID: newID[struct{}](t).String()})
	if _, err = r.Resolve(context.Background(), actor, external); err == nil || store.queries != before {
		t.Fatal("unbound source variant read Knowledge")
	}
}

func TestReadTextByteOffsetsAndCharacterBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, source    string
		offset          f.Progress
		limit           int
		text            string
		next            f.Progress
		truncated, fail bool
	}{
		{"empty", "", 0, 1, "", 0, false, false},
		{"complete", "文a", 0, 4, "文a", 4, false, false},
		{"do not split", "a文b", 0, 3, "a", 1, true, false},
		{"too small", "文", 0, 1, "", 0, true, false},
		{"offset", "文ab", 3, 1, "a", 4, true, false},
		{"eof offset", "文", 3, 4, "", 3, false, false},
		{"past eof", "文", 4, 4, "", 0, false, true},
		{"inside character", "文ab", 1, 4, "", 0, false, true},
		{"invalid utf8", "\xff", 0, 4, "", 0, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, err := readText(strings.NewReader(tt.source), kc.ReadRequest{ByteOffset: tt.offset, MaxBytes: tt.limit})
			if tt.fail {
				if err == nil {
					t.Fatal("invalid read accepted")
				}
				return
			}
			if err != nil || out.Text != tt.text || out.NextByteOffset != tt.next || out.Truncated != tt.truncated {
				t.Fatalf("got %#v, err=%v", out, err)
			}
		})
	}
}

type closeProbe struct {
	err   error
	calls int
}

func (*closeProbe) Read([]byte) (int, error) { return 0, io.EOF }
func (r *closeProbe) Close() error           { r.calls++; return r.err }
func TestCanonicalUnderlyingCloseFailureDoesNotReportCallJoined(t *testing.T) {
	scope, err := id.InProject(newID[id.Project](t))
	if err != nil {
		t.Fatal(err)
	}
	now, err := f.NewInstant(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	closeErr := errors.New("controlled close failure")
	probe := &closeProbe{err: closeErr}
	reader, err := oc.NewObjectReader(oc.ObjectMeta{ID: newID[oc.StoredObject](t), Scope: scope, MediaType: kc.PlainText, ByteSize: 0, SHA256: f.Digest("sha256:" + strings.Repeat("0", 64)), State: oc.Available, Version: 1, CreatedAt: now}, nil, probe)
	if err != nil {
		t.Fatal(err)
	}
	joined := 0
	tracked := &trackedRead{body: reader, done: func() { joined++ }}
	if err = tracked.Close(); !errors.Is(err, closeErr) || joined != 0 {
		t.Fatal("failure became join", err, joined)
	}
	probe.err = nil
	if err = tracked.Close(); err != nil || joined != 1 {
		t.Fatal("real close did not join", err, joined)
	}
	if err = tracked.Close(); err != nil || joined != 1 {
		t.Fatal("double join", err, joined)
	}
}
