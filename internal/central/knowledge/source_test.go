package knowledge

import (
	"context"
	"encoding/json"
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
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
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

type businessLeaseStore struct {
	publicationTransferStore
	lease string
}

func (s *businessLeaseStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.publicationClaimStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *businessLeaseStore) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	row := s.publicationTransferStore.QueryRow(ctx, sql, args...)
	if strings.Contains(sql, "FROM agenteam_knowledge.work_claims") {
		return publicationCheckpointRow(func(values ...any) error {
			if err := row.Scan(values...); err != nil {
				return err
			}
			if s.work.source != nil {
				project := s.work.source.String()
				*values[2].(**string) = &project
			}
			return nil
		})
	}
	return row
}
func (s *businessLeaseStore) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !s.active || s.acquires != s.calls || !strings.Contains(sql, "SET source_lease_id=$2") || len(args) != 2 || args[0] != s.record.id.String() {
		return pgconn.CommandTag{}, errors.New("unexpected source lease checkpoint")
	}
	s.lease = args[1].(string)
	s.updates++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type businessLeasePort struct {
	oc.Objects
	oc.SourceReads
	t                                              *testing.T
	store                                          *businessLeaseStore
	input                                          contentInput
	resolved                                       oc.ResolvedSource
	request                                        oc.AccessRequest
	issuer                                         oc.AccessIssuer
	lease                                          oc.SourceLease
	resolveErr, validateErr, acquireErr, cancelErr error
	resolves, validates, acquires, cancels         int
}

func (p *businessLeasePort) Resolve(_ context.Context, actor id.Actor, ref oc.BusinessFileRef) (oc.ResolvedSource, error) {
	p.resolves++
	if p.store.active || !actor.Equal(p.input.actor) {
		p.t.Fatal("resolver ran inside Tx or changed actor")
	}
	s, err := kc.NewBusinessSource(ref)
	if err != nil {
		p.t.Fatal(err)
	}
	d, err := describePublicationSource(s)
	if err != nil || !publicationSourceEqual(d, *p.input.request.Source) {
		p.t.Fatal("resolver changed original reference", err)
	}
	return p.resolved, p.resolveErr
}
func (p *businessLeasePort) DiscoverAccess(_ context.Context, request oc.AccessRequest) (oc.AccessLockPlan, error) {
	if p.store.active || request.Details().Operation != oc.AcquireSourceAccess {
		p.t.Fatal("source discovery phase")
	}
	p.request = request
	locks, err := scopeLocks(p.input.actor, p.input.project, true)
	if err != nil {
		return oc.AccessLockPlan{}, err
	}
	deps, err := oc.NewAccessDependencies(ob.DigestBytes([]byte("source facts")), locks)
	if err != nil {
		return oc.AccessLockPlan{}, err
	}
	return oc.NewAccessLockPlan(p.issuer, oc.AccessPlanDetails{Request: request, DependencyRequest: request, Dependencies: deps, DomainBinding: ob.DigestBytes([]byte("source binding")), Objects: []oc.ObjectID{p.resolved.Details().Meta.ID}, Locks: locks})
}
func (p *businessLeasePort) AcquireAccessPlansInTx(ctx context.Context, tx f.Tx, plans []oc.AccessLockPlan, extra []f.LockRequest) (oc.LockedAccess, error) {
	if len(plans) != 1 || !plans[0].Details().Request.Equal(p.request) {
		p.t.Fatal("source plan identity changed")
	}
	locked, err := oc.NewLockedAccess(p.issuer, tx, plans, extra)
	if err != nil {
		return locked, err
	}
	origin, _ := p.input.request.Source.sourceProject()
	for _, project := range []id.ProjectID{p.input.project, *origin} {
		key, _ := f.ProjectLock(project.String())
		found := false
		for _, lock := range locked.Locks() {
			if lock.Key.Canonical() == key.Canonical() {
				found = true
			}
		}
		if !found {
			p.t.Fatal("source or target Project omitted from single union")
		}
	}
	return locked, p.store.AcquireAll(ctx, tx, locked.Locks())
}
func (p *businessLeasePort) ValidateInTx(_ context.Context, tx f.Tx, actor id.Actor, source oc.ResolvedSource, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	p.validates++
	if !p.store.active || tx != p.store.tx || !locked.Matches(p.issuer, tx, plan, p.request) || !actor.Equal(p.input.actor) || source.Details().Meta.ID != p.resolved.Details().Meta.ID {
		p.t.Fatal("source validation lost original Tx/plan/source")
	}
	return p.validateErr
}
func (p *businessLeasePort) AcquireSourceInTx(_ context.Context, tx f.Tx, actor id.Actor, source oc.ResolvedSource, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.SourceLease, error) {
	p.acquires++
	if !p.store.active || tx != p.store.tx || p.validates != 1 || !locked.Matches(p.issuer, tx, plan, p.request) || !actor.Equal(p.input.actor) || source.Details().Meta.ID != p.resolved.Details().Meta.ID {
		p.t.Fatal("source lease acquired before current validation")
	}
	return p.lease, p.acquireErr
}
func (p *businessLeasePort) CancelSourceLease(_ context.Context, lease oc.SourceLease) error {
	p.cancels++
	if p.store.active || lease.ID() != p.lease.ID() {
		p.t.Fatal("source cancellation changed identity or ran in origin Tx")
	}
	return p.cancelErr
}

func TestBusinessPublicationResolutionAndLeaseKeepExactSourceAndPhysicalCommit(t *testing.T) {
	for _, mode := range []string{"valid", "unknown", "error with lease", "source stale", "owner revoked", "work drift", "wrong reference", "unsupported media", "resolve error", "cancel error"} {
		t.Run(mode, func(t *testing.T) {
			origin, document := newID[id.Project](t), newID[kc.Document](t)
			ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: origin, DocumentID: document.String(), Revision: 3})
			if err != nil {
				t.Fatal(err)
			}
			source, err := kc.NewBusinessSource(ref)
			if err != nil {
				t.Fatal(err)
			}
			s, input, intent, work, retirement, _ := directPublicationFixture(t, source)
			defer retirement.done()
			work.source = &origin
			retirement.work = work.clone()
			now, _ := f.NewInstant(time.Now())
			intent.record.created = now
			intent.header, _ = newContentHeader(input.project, input.document, 1, now)
			request, _ := json.Marshal(input.request)
			header, _ := json.Marshal(intent.header)
			store := &businessLeaseStore{publicationTransferStore: publicationTransferStore{publicationClaimStore: publicationClaimStore{publicationCheckpointStore: publicationCheckpointStore{record: intent.record, work: work, attempt: newID[f.TransactionAttempt](t)}, request: request, header: header}}}
			rawSource, _ := json.Marshal(input.request.Source)
			store.reservation = publicationReservationExecutor{record: intent.record, source: rawSource, project: input.project.String(), document: input.document.String(), phase: "planned"}
			user, _ := f.ParseID[id.User](input.actor.Details().UserID)
			grant, err := pc.NewProjectAccess(input.actor, pc.ProjectRef{ID: input.project, OwnerUserID: user, Name: "Target", NormalizedName: "target", Lifecycle: pc.Active, Version: 1, CreatedAt: now, UpdatedAt: now}, now)
			if err != nil {
				t.Fatal(err)
			}
			gate := &ownerGate{grant: grant}
			s.state().store, s.state().deps.Projects = store, gate
			owner, _ := oc.NewObjectOwner(oc.Knowledge, document.String(), origin.String())
			scope, _ := id.InProject(origin)
			meta := oc.ObjectMeta{ID: newID[oc.StoredObject](t), Scope: scope, MediaType: kc.PlainText, ByteSize: 0, SHA256: ob.DigestBytes(nil), State: oc.Available, Version: 1, CreatedAt: now}
			if mode == "wrong reference" {
				ref, _ = oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: origin, DocumentID: document.String(), Revision: 4})
			}
			if mode == "unsupported media" {
				meta.MediaType = "image/png"
			}
			resolved, err := oc.NewResolvedSource(oc.ResolvedSourceDetails{Reference: ref, Owner: owner, Meta: meta, Revision: ref.Details().Revision})
			if err != nil {
				t.Fatal(err)
			}
			lease, _ := oc.NewSourceLease(oc.NewSourceIssuer(), newID[oc.Lease](t))
			port := &businessLeasePort{t: t, store: store, input: input, resolved: resolved, issuer: oc.NewAccessIssuer(), lease: lease}
			s.state().deps.Sources, s.state().deps.SourceReads, s.state().deps.Objects = port, port, port
			physicalCause := errors.New("source physical error")
			injected := fault(f.DependencyUnavailable).WithCause(physicalCause)
			if mode == "resolve error" {
				port.resolveErr = injected
			}
			bound, err := s.resolveBusinessPublication(context.Background(), input, intent, source, work, retirement)
			if mode == "wrong reference" || mode == "unsupported media" || mode == "resolve error" {
				if err == nil || store.calls != 0 || port.acquires != 0 {
					t.Fatal("bad resolver output reached a lease", err)
				}
				if mode == "resolve error" && !errors.Is(err, physicalCause) {
					t.Fatal("resolver cause lost", err)
				}
				if mode == "unsupported media" {
					var known *f.Fault
					if !errors.As(err, &known) || known.Code != f.UnsupportedMediaType {
						t.Fatal("unsupported business media not classified", err)
					}
				}
				return
			}
			if err != nil || port.resolves != 1 {
				t.Fatal("exact source resolution", err)
			}
			switch mode {
			case "unknown":
				store.unknownAt = 1
			case "error with lease":
				port.acquireErr = injected
			case "source stale":
				port.validateErr = fault(f.VersionConflict)
			case "owner revoked":
				gate.err = fault(f.NotFound)
			case "work drift":
				store.work.fence++
			case "cancel error":
				port.cancelErr = injected
			}
			got, replay, err := s.acquireBusinessPublicationLease(context.Background(), input, intent, work, bound, retirement)
			if store.active || replay != nil || port.cancels != 0 {
				t.Fatal("lease escaped original transaction or cancelled before retirement")
			}
			if mode == "source stale" || mode == "owner revoked" || mode == "work drift" {
				if err == nil || got.Validate() == nil || store.updates != 0 || port.acquires != 0 {
					t.Fatal("denied source acquired lease", err)
				}
				return
			}
			if got.ID() != lease.ID() || port.acquires != 1 || store.acquires != 1 {
				t.Fatal("lease original identity/full union lost", err)
			}
			if mode == "unknown" {
				var failure commitFailure
				if !errors.As(err, &failure) || failure.result.AttemptID() != store.attempt || failure.result.Cause().Details().Primary.Canonical() != store.cause.Details().Primary.Canonical() {
					t.Fatal("source lease Unknown lost physical cause", err)
				}
			} else if mode == "error with lease" {
				if !errors.Is(err, physicalCause) || store.updates != 0 {
					t.Fatal("handle plus error was treated as successful checkpoint", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			joinErr := retirement.join()
			if port.cancels != 1 {
				t.Fatal("returned lease was not actually retired")
			}
			if mode == "cancel error" {
				if !errors.Is(joinErr, physicalCause) || len(s.state().calls) != 1 {
					t.Fatal("failed source lease cancellation became join", joinErr)
				}
			} else if joinErr != nil || len(s.state().calls) != 0 {
				t.Fatal("source lease retirement failed", joinErr)
			}
		})
	}
}
