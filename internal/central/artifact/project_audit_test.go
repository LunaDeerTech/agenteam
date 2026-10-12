package artifact

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// Controlled live Tx and real strict row decoding exercise only the Artifact
// fact provider. They do not grant Project permissions or claim a PG COMMIT.
type projectAuditStore struct {
	Store
	tx                f.Tx
	live, nilExecutor bool
	fixture           *projectAuditFixture
}

func (s *projectAuditStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx || !s.live {
		return nil, failure(f.Forbidden, nil)
	}
	if s.nilExecutor {
		return nil, nil
	}
	return s, nil
}

type projectAuditScan func(...any) error

func (r projectAuditScan) Scan(out ...any) error { return r(out...) }
func (s *projectAuditStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	v := s.fixture
	return projectAuditScan(func(out ...any) error {
		v.reads++
		kind := "artifact"
		if strings.Contains(query, "FROM agenteam_artifact.commands ") {
			kind = "command"
		} else if !strings.Contains(query, "FROM agenteam_artifact.artifacts ") {
			return errors.New("unexpected controlled query")
		}
		if kind == "artifact" && len(args) == 1 && v.sourceMeta.Validate() == nil && args[0] == v.sourceMeta.Details().Reference.ArtifactID.String() {
			kind = "source"
		}
		if v.beforeScan != nil {
			v.beforeScan(ctx, kind)
		}
		if v.rowErr != nil {
			return v.rowErr
		}
		if len(args) != 1 {
			return errors.New("wrong row identity arity")
		}
		if kind == "artifact" || kind == "source" {
			if kind == "artifact" && args[0] != v.meta.Details().Reference.ArtifactID.String() {
				return errors.New("wrong artifact lookup")
			}
			if kind == "artifact" && v.missingArtifact || kind == "source" && v.missingSource {
				return pgx.ErrNoRows
			}
			m := v.storedMeta.Details()
			if kind == "source" {
				m = v.storedSource.Details()
			}
			return (cleanupOwnerRow{values: []any{m.Reference.ArtifactID.String(), m.Reference.FileID.String(), m.Reference.ProjectID.String(), m.Object.ID.String(), string(m.Kind), m.Name, m.Description, m.Object.MediaType, int64(m.Object.ByteSize), digestRaw(m.Object.SHA256), int64(m.Object.Version), m.Object.CreatedAt.Time(), int64(m.Version), string(i.Human), m.CreatedByUserID, v.rowCause, m.ExecutionID, m.OperationID, "", "", m.CreatedAt.Time()}}).Scan(out...)
		}
		hash, ok := args[0].([]byte)
		if !ok || !bytes.Equal(hash, digestRaw(digestBytes([]byte(v.command.identity.Canonical())))) {
			return errors.New("wrong command lookup")
		}
		if v.missingCommand {
			return pgx.ErrNoRows
		}
		c := v.storedCommand
		var source []byte
		if c.source.Validate() == nil {
			var err error
			source, err = encodeSource(c.source)
			if err != nil {
				return err
			}
		}
		lease := ""
		if c.sourceLease.Validate() == nil {
			lease = c.sourceLease.String()
		}
		return (cleanupOwnerRow{values: []any{string(c.identity.Key()), c.ref.ProjectID.String(), c.ref.ArtifactID.String(), c.ref.FileID.String(), c.originalActor, digestRaw(c.digest), c.cause, c.path, string(c.kind), c.state, c.display.Name, c.display.Description, c.media, c.size, digestRaw(c.sha), source, lease, c.object.String(), "", "", string(c.creatorKind), c.creator, c.execution, c.operation, c.tool, c.toolCall, c.created.Time()}}).Scan(out...)
	})
}

type projectAuditAppender func(context.Context, f.Tx, au.Entry, au.AppendKey) (au.AppendReceipt, error)

func (p projectAuditAppender) AppendInTx(ctx context.Context, tx f.Tx, e au.Entry, k au.AppendKey) (au.AppendReceipt, error) {
	return p(ctx, tx, e, k)
}

type projectAuditFixture struct {
	store                           *projectAuditStore
	service                         *Service
	authority                       *ProjectAuditAuthority
	meta, storedMeta                ac.Metadata
	sourceMeta, storedSource        ac.Metadata
	missingSource                   bool
	command, storedCommand          commandRow
	request                         createRequest
	read                            bool
	rowCause                        string
	reads                           int
	missingArtifact, missingCommand bool
	rowErr                          error
	beforeScan                      func(context.Context, string)
	saved                           context.Context
	entry                           au.Entry
	key                             au.AppendKey
}

func newProjectAuditFixture(t *testing.T, path string) *projectAuditFixture {
	t.Helper()
	actor, err := i.NewHuman(newTestID[i.User](t), newTestID[i.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	project := newTestID[i.Project](t)
	scope, err := i.InProject(project)
	if err != nil {
		t.Fatal(err)
	}
	now, err := f.NewInstant(time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	kind := ac.Generated
	if path == "upload" {
		kind = ac.UserUpload
	}
	meta, err := ac.NewMetadata(ac.MetadataDetails{Reference: ac.ArtifactRef{ProjectID: project, ArtifactID: newTestID[ac.ArtifactEntity](t), FileID: newTestID[ac.File](t)}, Kind: kind, Name: "fixture.txt", Description: "safe fixture", Object: oc.ObjectMeta{ID: newTestID[oc.StoredObject](t), Scope: scope, MediaType: "text/plain", ByteSize: 3, SHA256: digestBytes([]byte("one")), State: oc.Available, Version: 1, CreatedAt: now}, CreatedByUserID: actor.Details().UserID, CreatedAt: now, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := ac.NewInvocation(ac.InvocationDetails{Actor: actor, ProjectID: project, AttemptID: newTestID[ac.CallAttempt](t)})
	if err != nil {
		t.Fatal(err)
	}
	id, err := commandIdentity(project, "audit-fact-command", "create")
	if err != nil {
		t.Fatal(err)
	}
	m := meta.Details()
	v := &projectAuditFixture{meta: meta, storedMeta: meta, read: path == "read"}
	v.command = commandRow{identity: id, ref: m.Reference, originalActor: stableActor(actor), digest: digestBytes([]byte("request")), cause: newTestID[struct{}](t).String(), path: path, state: "pending", kind: kind, display: ac.Display{Name: m.Name, Description: m.Description}, media: m.Object.MediaType, size: int64(m.Object.ByteSize), sha: m.Object.SHA256, object: m.Object.ID, creatorKind: i.Human, creator: actor.Details().UserID, created: now}
	v.storedCommand, v.rowCause = v.command, v.command.cause
	v.request = createRequest{invocation: invocation, identity: id, digest: v.command.digest}
	if path == "upload" {
		receipt, err := oc.NewUploadReceipt(oc.ReceiptDetails{ID: newTestID[oc.Receipt](t), UploadID: newTestID[oc.Upload](t), ObjectID: m.Object.ID, Owner: artifactOwner(m.Reference), CreationCause: v.command.cause})
		if err != nil {
			t.Fatal(err)
		}
		v.request.receipt = receipt
	}
	v.store = &projectAuditStore{tx: f.NewTx(), live: true, fixture: v}
	v.authority, err = NewProjectAuditAuthority(v.store)
	if err != nil {
		t.Fatal(err)
	}
	state := &serviceState{store: v.store}
	v.service = &Service{data: func() *serviceState { return state }}
	state.audit = projectAuditAppender(func(ctx context.Context, tx f.Tx, e au.Entry, k au.AppendKey) (au.AppendReceipt, error) {
		v.saved, v.entry, v.key = ctx, e, k
		return au.AppendReceipt{}, v.authority.CheckProjectAuditInTx(ctx, tx, e, k)
	})
	t.Cleanup(func() { v.store.live = false })
	return v
}
func (v *projectAuditFixture) append(ctx context.Context) error {
	if !v.read {
		return v.service.appendCreate(ctx, v.store.tx, v.request, v.command, v.meta)
	}
	m := v.meta.Details()
	resource, err := au.NewResource(au.ArtifactResource, m.Reference.ArtifactID.String())
	if err != nil {
		return err
	}
	metadata, err := au.ArtifactMetadata(au.ArtifactRead, au.ArtifactMetadataFields{ArtifactID: m.Reference.ArtifactID.String(), ObjectID: m.Object.ID.String(), MediaType: m.Object.MediaType, ByteSize: m.Object.ByteSize, SentBytes: 1, Phase: au.ReadPhase})
	if err != nil {
		return err
	}
	return v.service.appendReadAudit(ctx, v.store.tx, v.request.invocation, au.ArtifactRead, resource, metadata, digestBytes([]byte("read-intent")), v.meta)
}

func projectAuditIssuedInput(t *testing.T, v *projectAuditFixture) (oc.DownloadTarget, oc.DownloadEvent, au.Entry, au.AppendKey) {
	t.Helper()
	m := v.meta.Details()
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.ArtifactFile, ProjectID: m.Reference.ProjectID, ArtifactID: m.Reference.ArtifactID.String(), FileID: m.Reference.FileID.String()})
	if err != nil {
		t.Fatal(err)
	}
	source, err := artifactSource(artifactRow{meta: v.meta, cause: v.rowCause}, ref)
	if err != nil {
		t.Fatal(err)
	}
	target, err := oc.NewDownloadTarget(oc.DownloadTargetDetails{Source: source, Filename: m.Name})
	if err != nil {
		t.Fatal(err)
	}
	event := oc.DownloadEvent{GrantID: newTestID[oc.DownloadGrant](t), Phase: oc.DownloadIssued, Length: m.Object.ByteSize}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	meta, err := au.ArtifactMetadata(au.ArtifactDownload, au.ArtifactMetadataFields{ArtifactID: m.Reference.ArtifactID.String(), ObjectID: m.Object.ID.String(), MediaType: auditMedia(m.Object.MediaType), ByteSize: event.Length, Phase: au.IssuedPhase})
	if err != nil {
		t.Fatal(err)
	}
	resource, err := au.NewResource(au.ArtifactResource, m.Reference.ArtifactID.String())
	if err != nil {
		t.Fatal(err)
	}
	entry, err := au.NewEntry(au.EntryFields{Scope: m.Object.Scope, Actor: v.request.invocation.Details().Actor, Action: au.ArtifactDownload, Outcome: au.Success, Resource: resource, Metadata: meta})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := jsonDigest([]string{"download", event.GrantID.String(), event.AttemptID.String(), string(event.Phase)})
	if err != nil {
		t.Fatal(err)
	}
	key, err := au.NewAppendKey(au.ArtifactProducer, digest.String(), 0)
	if err != nil {
		t.Fatal(err)
	}
	return target, event, entry, key
}

func TestProjectAuditDownloadIssuedScope(t *testing.T) {
	for _, mode := range []string{"missing-artifact", "no-object-issuing-scope", "expired"} {
		t.Run(mode, func(t *testing.T) {
			v := newProjectAuditFixture(t, "read")
			target, event, entry, key := projectAuditIssuedInput(t, v)
			owners, err := NewOwnerProvider(v.store, nil)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			state := sourceState{owners: owners, audit: projectAuditAppender(func(ctx context.Context, tx f.Tx, e au.Entry, k au.AppendKey) (au.AppendReceipt, error) {
				calls++
				v.saved = ctx
				w := ctx.Value(projectAuditWitnessKey{}).(*projectAuditWitness)
				if !w.active.Load() || w.command != nil || w.download == nil || w.download.event != event || w.download.target.Details().Filename != target.Details().Filename || !sameSource(w.download.target.Details().Source, target.Details().Source) || !sameProjectAuditEntry(e, entry) || k.Details() != key.Details() {
					t.Fatal("actual Source append lost its exact captured issued facts")
				}
				if mode == "expired" {
					return au.AppendReceipt{}, nil
				}
				// A public event plus Artifact's witness cannot impersonate the
				// private Object grant INSERT scope. The real success is a PG test.
				return au.AppendReceipt{}, v.authority.CheckProjectAuditInTx(ctx, tx, e, k)
			})}
			sources := &Sources{data: func() sourceState { return state }}
			if mode == "missing-artifact" {
				v.missingArtifact = true
			}
			err = sources.appendDownloadIssuedAudit(context.Background(), v.store.tx, entry, key, target, event)
			if mode == "expired" {
				if err != nil || calls != 1 {
					t.Fatal("original controlled Append did not return", err)
				}
			} else {
				projectAuditCode(t, err, f.Forbidden)
			}
			if mode == "missing-artifact" {
				if calls != 0 || v.reads != 1 {
					t.Fatal("missing canonical Artifact minted an issuing witness")
				}
				return
			}
			if mode == "no-object-issuing-scope" && (calls != 1 || v.reads != 2) {
				t.Fatal("original canonical facts not read before Object scope rejection", v.reads)
			}
			if v.saved.Value(projectAuditWitnessKey{}).(*projectAuditWitness).active.Load() {
				t.Fatal("issued witness survived Append")
			}
			before := v.reads
			projectAuditCode(t, v.authority.CheckProjectAuditInTx(v.saved, v.store.tx, entry, key), f.Forbidden)
			if v.reads != before {
				t.Fatal("expired Source witness reached canonical SQL")
			}
		})
	}
}
func projectAuditCode(t *testing.T, err error, code f.Code) {
	t.Helper()
	var actual *f.Fault
	if !errors.As(err, &actual) || actual.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}
func TestProjectAuditOriginalFactsAndCommands(t *testing.T) {
	for _, mode := range []string{"read", "content", "upload", "missing-artifact", "changed-canonical", "missing-command", "completed-command", "changed-object", "changed-digest", "changed-cause", "changed-path", "foreign-actor", "execution-command", "changed-row-cause", "row-error"} {
		t.Run(mode, func(t *testing.T) {
			path := mode
			if path != "read" && path != "upload" {
				path = "content"
			}
			v := newProjectAuditFixture(t, path)
			switch mode {
			case "missing-artifact":
				v.missingArtifact = true
			case "changed-canonical":
				m := v.meta.Details()
				m.Version++
				var err error
				v.storedMeta, err = ac.NewMetadata(m)
				if err != nil {
					t.Fatal(err)
				}
			case "missing-command":
				v.missingCommand = true
			case "completed-command":
				v.storedCommand.state = "completed"
			case "changed-object":
				v.storedCommand.object = newTestID[oc.StoredObject](t)
			case "changed-digest":
				v.storedCommand.digest = digestBytes([]byte("different"))
			case "changed-cause":
				v.storedCommand.cause = newTestID[struct{}](t).String()
			case "changed-path":
				v.storedCommand.path = "upload"
			case "foreign-actor":
				v.storedCommand.originalActor = "human:" + newTestID[i.User](t).String()
			case "execution-command":
				v.storedCommand.execution = newTestID[i.Execution](t).String()
			case "changed-row-cause":
				v.rowCause = newTestID[struct{}](t).String()
			case "row-error":
				v.rowErr = errors.New("original Scan failed")
			}
			err := v.append(context.Background())
			if mode == "row-error" {
				projectAuditCode(t, err, f.DependencyUnavailable)
				if !errors.Is(err, v.rowErr) {
					t.Fatal("original Scan error lost", err)
				}
				return
			}
			if mode != "read" && mode != "content" && mode != "upload" {
				projectAuditCode(t, err, f.Forbidden)
				return
			}
			want := 2
			if mode == "read" {
				want = 1
			}
			if err != nil || v.reads != want {
				t.Fatal("original strict facts not consumed", v.reads, err)
			}
			if v.saved.Value(projectAuditWitnessKey{}).(*projectAuditWitness).active.Load() {
				t.Fatal("witness survived original Append return")
			}
			before := v.reads
			projectAuditCode(t, v.authority.CheckProjectAuditInTx(v.saved, v.store.tx, v.entry, v.key), f.Forbidden)
			if v.reads != before {
				t.Fatal("expired witness reached SQL")
			}
		})
	}
}
func TestProjectAuditWitnessAndTransaction(t *testing.T) {
	var absent *projectAuditStore
	for _, store := range []Store{nil, absent} {
		if _, err := NewProjectAuditAuthority(store); err == nil {
			t.Fatal("nil Store bound")
		}
	}
	for _, mode := range []string{"nil-context", "invalid-tx", "no-witness", "foreign-tx", "foreign-store", "foreign-project", "different-key", "closed-tx", "nil-executor"} {
		t.Run(mode, func(t *testing.T) {
			v := newProjectAuditFixture(t, "read")
			v.service.state().audit = projectAuditAppender(func(ctx context.Context, tx f.Tx, entry au.Entry, key au.AppendKey) (au.AppendReceipt, error) {
				provider := v.authority
				switch mode {
				case "nil-context":
					ctx = nil
				case "invalid-tx":
					tx = f.Tx{}
				case "no-witness":
					ctx = context.Background()
				case "foreign-tx":
					tx = f.NewTx()
				case "foreign-store":
					var err error
					provider, err = NewProjectAuditAuthority(&projectAuditStore{})
					if err != nil {
						t.Fatal(err)
					}
				case "foreign-project":
					fields := entry.Fields()
					var err error
					fields.Scope, err = i.InProject(newTestID[i.Project](t))
					if err != nil {
						t.Fatal(err)
					}
					entry, err = au.NewEntry(fields)
					if err != nil {
						t.Fatal(err)
					}
				case "different-key":
					var err error
					key, err = au.NewAppendKey(au.ArtifactProducer, digestBytes([]byte("other-key")).String(), 0)
					if err != nil {
						t.Fatal(err)
					}
				case "closed-tx":
					v.store.live = false
				case "nil-executor":
					v.store.nilExecutor = true
				}
				return au.AppendReceipt{}, provider.CheckProjectAuditInTx(ctx, tx, entry, key)
			})
			err := v.append(context.Background())
			code := f.Forbidden
			if mode == "nil-context" || mode == "invalid-tx" {
				code = f.InvalidArgument
			}
			if mode == "nil-executor" {
				code = f.DependencyUnbound
			}
			projectAuditCode(t, err, code)
			if v.reads != 0 {
				t.Fatal("invalid original witness/Tx reached SQL")
			}
		})
	}
}
func TestProjectAuditWaitsForOriginalAppend(t *testing.T) {
	v := newProjectAuditFixture(t, "read")
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	joined := false
	t.Cleanup(func() {
		cancel()
		free()
		if !joined {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("original Append did not return")
			}
		}
	})
	sentinel := errors.New("original appender failure")
	var witness *projectAuditWitness
	v.service.state().audit = projectAuditAppender(func(ctx context.Context, tx f.Tx, e au.Entry, k au.AppendKey) (au.AppendReceipt, error) {
		witness = ctx.Value(projectAuditWitnessKey{}).(*projectAuditWitness)
		if err := v.authority.CheckProjectAuditInTx(ctx, tx, e, k); err != nil {
			return au.AppendReceipt{}, err
		}
		close(entered)
		<-release
		return au.AppendReceipt{}, sentinel
	})
	go func() { done <- v.append(ctx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("original Append not entered")
	}
	cancel()
	if !witness.active.Load() {
		t.Fatal("cancellation closed active original Append")
	}
	select {
	case <-done:
		joined = true
		t.Fatal("cancellation replaced original Append return")
	default:
	}
	free()
	select {
	case err := <-done:
		joined = true
		if !errors.Is(err, sentinel) {
			t.Fatal("original appender failure lost", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("released Append not returned")
	}
	if witness.active.Load() {
		t.Fatal("original return did not retire witness")
	}
}
func TestProjectAuditWaitsForOriginalScan(t *testing.T) {
	for _, kind := range []string{"artifact", "command"} {
		t.Run(kind, func(t *testing.T) {
			v := newProjectAuditFixture(t, "content")
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				free()
				if !joined {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("original Scan did not return")
					}
				}
			})
			v.beforeScan = func(_ context.Context, got string) {
				if got == kind {
					close(entered)
					<-release
				}
			}
			go func() { done <- v.append(ctx) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("original Scan not entered")
			}
			cancel()
			select {
			case <-done:
				joined = true
				t.Fatal("cancel replaced original Scan return")
			default:
			}
			free()
			select {
			case err := <-done:
				joined = true
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancelled scan became authorization", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("released Scan not returned")
			}
			if v.saved.Value(projectAuditWitnessKey{}).(*projectAuditWitness).active.Load() {
				t.Fatal("cancelled Append left live witness")
			}
		})
	}
}

func projectAuditSourceFor(t *testing.T, meta ac.Metadata) oc.ResolvedSource {
	t.Helper()
	m := meta.Details()
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.ArtifactFile, ProjectID: m.Reference.ProjectID, ArtifactID: m.Reference.ArtifactID.String(), FileID: m.Reference.FileID.String()})
	if err != nil {
		t.Fatal(err)
	}
	source, err := artifactSource(artifactRow{meta: meta}, ref)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func newProjectAuditSourceFixture(t *testing.T) *projectAuditFixture {
	t.Helper()
	v := newProjectAuditFixture(t, "source")
	m := v.meta.Details()
	m.Reference.ArtifactID, m.Reference.FileID = newTestID[ac.ArtifactEntity](t), newTestID[ac.File](t)
	m.Object.ID = newTestID[oc.StoredObject](t)
	m.Name = "existing-source.txt"
	var err error
	v.sourceMeta, err = ac.NewMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	v.storedSource = v.sourceMeta
	v.command.source = projectAuditSourceFor(t, v.sourceMeta)
	v.command.sourceLease = newTestID[oc.Lease](t)
	v.storedCommand = v.command
	return v
}

// The publication helper supplies the complete original source/lease. These
// controls check local facts, not SourceReads authority, bytes or a PG commit.
func TestProjectAuditSourceCreation(t *testing.T) {
	for _, mode := range []string{"same-project", "missing-source", "canonical-object", "canonical-revision", "lease-drift", "missing-lease", "command-source-drift", "foreign-project", "other-kind", "self-source", "content-with-source"} {
		t.Run(mode, func(t *testing.T) {
			v := newProjectAuditSourceFixture(t)
			switch mode {
			case "missing-source":
				v.missingSource = true
			case "canonical-object", "canonical-revision":
				m := v.sourceMeta.Details()
				if mode == "canonical-object" {
					m.Object.ID = newTestID[oc.StoredObject](t)
				} else {
					m.Version++
				}
				var err error
				v.storedSource, err = ac.NewMetadata(m)
				if err != nil {
					t.Fatal(err)
				}
			case "lease-drift":
				v.storedCommand.sourceLease = newTestID[oc.Lease](t)
			case "missing-lease":
				v.command.sourceLease = oc.LeaseID{}
			case "command-source-drift":
				d := v.command.source.Details()
				d.Revision++
				var err error
				v.storedCommand.source, err = oc.NewResolvedSource(d)
				if err != nil {
					t.Fatal(err)
				}
			case "foreign-project":
				m := v.sourceMeta.Details()
				m.Reference.ProjectID = newTestID[i.Project](t)
				var err error
				m.Object.Scope, err = i.InProject(m.Reference.ProjectID)
				if err != nil {
					t.Fatal(err)
				}
				v.sourceMeta, err = ac.NewMetadata(m)
				if err != nil {
					t.Fatal(err)
				}
				v.storedSource = v.sourceMeta
				v.command.source = projectAuditSourceFor(t, v.sourceMeta)
				v.storedCommand.source = v.command.source
			case "other-kind":
				d := v.command.source.Details()
				document := newTestID[struct{}](t).String()
				var err error
				d.Reference, err = oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: v.meta.Details().Reference.ProjectID, DocumentID: document, Revision: d.Revision})
				if err != nil {
					t.Fatal(err)
				}
				d.Owner, err = oc.NewObjectOwner(oc.Knowledge, document, v.meta.Details().Reference.ProjectID.String())
				if err != nil {
					t.Fatal(err)
				}
				v.command.source, err = oc.NewResolvedSource(d)
				if err != nil {
					t.Fatal(err)
				}
				v.storedCommand.source = v.command.source
			case "self-source":
				v.command.source = projectAuditSourceFor(t, v.meta)
				v.storedCommand.source = v.command.source
			case "content-with-source":
				v.command.path, v.storedCommand.path = "content", "content"
			}
			err := v.append(context.Background())
			if mode != "same-project" {
				projectAuditCode(t, err, f.Forbidden)
				return
			}
			if err != nil || v.reads != 3 {
				t.Fatal("actual source publication did not read target, command and source", err, v.reads)
			}
			w := v.saved.Value(projectAuditWitnessKey{}).(*projectAuditWitness)
			if w.command == nil || !sameSource(w.command.source, v.command.source) || w.command.sourceLease != v.command.sourceLease || w.active.Load() {
				t.Fatal("original source/lease capture or return lifetime changed")
			}
			before := v.reads
			projectAuditCode(t, v.authority.CheckProjectAuditInTx(v.saved, v.store.tx, v.entry, v.key), f.Forbidden)
			if v.reads != before {
				t.Fatal("returned source witness reached canonical SQL")
			}
		})
	}
}

func TestProjectAuditSourceScanReturn(t *testing.T) {
	v := newProjectAuditSourceFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	joined := false
	t.Cleanup(func() {
		cancel()
		free()
		if !joined {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("original source Scan did not return")
			}
		}
	})
	v.beforeScan = func(_ context.Context, kind string) {
		if kind == "source" {
			close(entered)
			<-release
		}
	}
	go func() { done <- v.append(ctx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("original source canonical Scan not reached")
	}
	cancel()
	w := v.saved.Value(projectAuditWitnessKey{}).(*projectAuditWitness)
	if !w.active.Load() {
		t.Fatal("cancel retired the active original source witness")
	}
	select {
	case <-done:
		joined = true
		t.Fatal("cancel substituted for source Scan return")
	default:
	}
	free()
	select {
	case err := <-done:
		joined = true
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled original source Scan became authorization", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("released source Scan did not return")
	}
	if w.active.Load() {
		t.Fatal("source witness survived original Append return")
	}
}
