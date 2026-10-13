//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	obj "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

// These are the original default App instances. Only explicit Delete acceptance
// and other domains' Stop handoffs are controlled prerequisites. The default
// Registry remains nil. This is not the old Knowledge Move experiment, a
// network-fault test, a replacement Runtime, or complete Project deletion.
func TestKnowledgeProjectLifecycleDefaultApp(t *testing.T) {
	t.Run("http-source-and-original-store-close", knowledgeAppWholeReturn)
	t.Run("original-technical-unknown-readonly-confirmation", knowledgeAppUnknown)
	t.Run("six-business-tables-and-object-retirement", knowledgeAppCleanup)
}

type knowledgeAppStore struct {
	*executionSchedulerAppStore
	mu    sync.Mutex
	hooks []*usageAppHook
}

func (s *knowledgeAppStore) arm(h *usageAppHook) {
	h.observed, h.release = make(chan usageAppObservation, 1), make(chan struct{})
	s.mu.Lock()
	s.hooks = append(s.hooks, h)
	s.mu.Unlock()
}
func (s *knowledgeAppStore) releaseAll() {
	s.mu.Lock()
	hooks := append([]*usageAppHook(nil), s.hooks...)
	s.mu.Unlock()
	for _, h := range hooks {
		s.mu.Lock()
		h.used = true
		s.mu.Unlock()
		h.open()
	}
}
func knowledgeAppWrites(ctx context.Context, x postgres.SQLExecutor) (int64, error) {
	var n int64
	err := x.QueryRow(ctx, `SELECT COALESCE(sum(n_tup_ins+n_tup_upd+n_tup_del),0)::bigint FROM pg_stat_xact_user_tables WHERE schemaname IN ('agenteam_knowledge','agenteam_object')`).Scan(&n)
	return n, err
}
func (s *knowledgeAppStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	var h *usageAppHook
	for _, candidate := range s.hooks {
		if !candidate.used && candidate.match(cause) {
			h = candidate
			break
		}
	}
	s.mu.Unlock()
	var before, after int64
	selected := false
	r := s.executionSchedulerAppStore.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if h == nil {
			return fn(ctx, tx)
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		before, err = knowledgeAppWrites(ctx, x)
		if err != nil {
			return err
		}
		if err = fn(ctx, tx); err != nil {
			return err
		}
		after, err = knowledgeAppWrites(ctx, x)
		if err != nil {
			return err
		}
		selected, err = h.selectRow(ctx, x, cause, before, after)
		return err
	})
	if !selected || r.State() != f.Committed {
		return r
	}
	s.mu.Lock()
	if h.used {
		s.mu.Unlock()
		return r
	}
	h.used = true
	s.mu.Unlock()
	returned := make(chan struct{})
	defer close(returned)
	masked := r
	if h.mask {
		attempt, err := f.NewID[f.TransactionAttempt]()
		if err != nil {
			return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(err))
		}
		masked = f.UnknownResult(attempt, cause)
	}
	h.observed <- usageAppObservation{r, cause, before, after, returned, masked}
	<-h.release
	return masked
}

type knowledgeLifecycleApp struct {
	*executionSchedulerApp
	tracked                 *knowledgeAppStore
	bound                   *projectUsageAssembly
	knowledgeCalls          *knowledgeCallAssembly
	pair                    *knowledge.ProjectCallOwnership
	acceptor                *project.Service
	accounts                *account.Authority
	stop                    *project.LifecycleStopDriver
	cleaner                 *project.LifecycleCleanupDriver
	processes               outboxProcessAuthority
	cause                   pc.LifecycleCause
	scope                   pc.ScopeRef
	report                  pc.StopReport
	inspecting              bool
	calls                   []*usageAppCall
	needsConfirmation       bool
	unavailableCalls        atomic.Int64
	document, otherDocument kc.DocumentRef
	readers                 []kc.CanonicalRead
}

func newKnowledgeLifecycleApp(t *testing.T) *knowledgeLifecycleApp {
	t.Helper()
	v := &knowledgeLifecycleApp{}
	v.executionSchedulerApp = newExecutionSchedulerAppWith(t, func(d *dependencies, original *executionSchedulerApp) {
		v.tracked = &knowledgeAppStore{executionSchedulerAppStore: original.store}
		open := d.open
		d.open = func(ctx context.Context, cfg postgres.Config) (database, error) {
			_, err := open(ctx, cfg)
			return v.tracked, err
		}
		d.observeProjectUsage = func(bound *projectUsageAssembly) { v.bound = bound }
	})
	v.assembly.mu.Lock()
	v.knowledgeCalls, _ = v.assembly.knowledge.(*knowledgeCallAssembly)
	v.assembly.mu.Unlock()
	if v.bound == nil || v.bound.lifecycle == nil || v.knowledgeCalls == nil || v.knowledgeCalls.pair == nil || v.knowledgeCalls.cleanup == nil {
		t.Fatal("original installed Knowledge/Life missing")
	}
	v.pair = v.knowledgeCalls.pair
	if v.pair.Service() == nil || v.pair.SourceResolver() == nil || v.pair.SourceReads() == nil {
		t.Fatal("default Knowledge graph split")
	}
	v.processes = outboxProcessAuthority{process: v.objects.process, guard: v.objects.guard}
	t.Cleanup(func() {
		v.tracked.releaseAll()
		for _, reader := range v.readers {
			_ = reader.Close()
		}
		for _, c := range v.calls {
			usageAppWait(t, c.done, "Knowledge original call cleanup")
		}
		if v.needsConfirmation && v.cause.Validate() == nil {
			settled := false
			for n := 0; n < 8; n++ {
				if err := v.stop.Run(v.ctx, v.a, v.cause.OperationID); err == nil && v.report.Details().State == pc.Stopped {
					settled = true
					break
				}
			}
			if !settled {
				t.Error("original Knowledge Unknown did not settle before global Stop")
			}
		}
		// A retained cleanup Unknown must keep its original driver alive until its
		// explicit confirmation returns. Ordinary partial cleanup need not finish
		// the whole project before App shutdown.
		if v.cleaner != nil {
			for n := 0; n < 2; n++ {
				if err := v.cleaner.Run(v.ctx, v.a, v.cause.OperationID); err == nil {
					break
				}
			}
		}
		for _, owner := range []interface {
			Stop()
			Drain(context.Context) error
		}{v.cleaner, v.stop, v.acceptor} {
			if reflect.ValueOf(owner).IsNil() {
				continue
			}
			owner.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := owner.Drain(ctx)
			cancel()
			if err != nil {
				t.Error("explicit Knowledge lifecycle owner did not return", err)
			}
		}
		if v.unavailableCalls.Load() != 0 {
			t.Error("unavailable participant was invoked")
		}
	})
	v.explicitAcceptance(t)
	return v
}
func (v *knowledgeLifecycleApp) start(fn func() error) *usageAppCall {
	c := &usageAppCall{done: make(chan struct{})}
	v.calls = append(v.calls, c)
	go func() { c.err = fn(); close(c.done) }()
	return c
}
func (v *knowledgeLifecycleApp) explicitAcceptance(t *testing.T) {
	t.Helper()
	accounts, err := account.NewAuthority(v.tracked, v.root.cfg.AccountKeyring())
	workAppCheck(t, "knowledge-acceptance-account", err)
	v.accounts = accounts
	catalog := event.NewCatalog()
	events, err := pc.RegisterProjectEvents(catalog)
	workAppCheck(t, "knowledge-acceptance-events", err)
	v.root.owned.mu.Lock()
	auditing := v.root.owned.auditService
	v.root.owned.mu.Unlock()
	journal, err := outbox.New(v.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{pc.ProjectProducer: v.bound.projects}, Projects: v.bound.projects, Sessions: accounts, System: accounts, Audit: auditing, Cursors: v.root.cfg.CursorKeyring(), Processes: v.processes})
	workAppCheck(t, "knowledge-acceptance-outbox", err)
	manifest, err := defaultProjectLifecycleManifest()
	workAppCheck(t, "knowledge-original-manifest", err)
	var bindings []project.LifecycleParticipantBinding
	for _, entry := range manifest.Entries() {
		bindings = append(bindings, project.LifecycleParticipantBinding{Registration: entry, Participant: usageAppUnavailable{entry.Name, &v.unavailableCalls}})
	}
	registry, err := project.NewLifecycleRegistry(manifest, bindings)
	workAppCheck(t, "knowledge-controlled-acceptance-registry", err)
	v.acceptor, err = project.New(v.tracked, project.Dependencies{Authority: v.bound.projects, Activity: accounts, Audit: auditing, Events: journal, ProjectEvents: events, Processes: v.processes, Cursors: v.root.cfg.CursorKeyring(), LifecycleRegistry: registry}, project.DefaultConfig())
	workAppCheck(t, "knowledge-explicit-acceptor", err)
	v.stop, err = project.NewLifecycleStopDriver(v.tracked, v.bound.lifecycle, v.processes, func(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
		var err error
		if v.inspecting {
			v.report, err = v.pair.InspectProjectStop(ctx, actor, cause, scope)
		} else {
			v.report, err = v.pair.RequestProjectStop(ctx, actor, cause, scope)
		}
		return err
	})
	workAppCheck(t, "knowledge-original-stop-driver", err)
}

func (v *knowledgeLifecycleApp) accept(t *testing.T) {
	t.Helper()
	p, err := v.projects.service.GetProject(v.ctx, v.actor, v.a)
	workAppCheck(t, "knowledge-original-project-read", err)
	var username string
	user, _ := f.UserLock(v.actor.Details().UserID)
	readCause, _ := f.NewRecoveryCause("knowledge-app-owner-route", guardID[struct{}](t).String(), v.a.String())
	result := v.tracked.WithinTx(v.ctx, readCause, func(ctx context.Context, tx f.Tx) error {
		if err := v.tracked.AcquireAll(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Shared}}); err != nil {
			return err
		}
		route, err := v.accounts.CurrentUserRouteInTx(ctx, tx, v.actor)
		username = route.Username
		return err
	})
	if result.State() != f.Committed {
		t.Fatal("original current owner route failed", result.Fault())
	}
	path, err := pc.NormalizeProjectPath(username, p.Name)
	workAppCheck(t, "knowledge-delete-path", err)
	_, unbound := v.projects.service.BeginDeleteProject(v.ctx, v.actor, executionSchedulerAppMeta(t, &p.Version), v.a, pc.DeleteProjectRequest{NormalizedCurrentPath: path, Permanent: true})
	var unavailable *f.Fault
	if !errors.As(unbound, &unavailable) || unavailable.Code != f.DependencyUnbound {
		t.Fatal("default Registry unexpectedly accepted Delete", unbound)
	}
	r, err := v.acceptor.BeginDeleteProject(v.ctx, v.actor, executionSchedulerAppMeta(t, &p.Version), v.a, pc.DeleteProjectRequest{NormalizedCurrentPath: path, Permanent: true})
	workAppCheck(t, "knowledge-formal-delete-acceptance", err)
	if r.Operation == nil {
		t.Fatal("formal Knowledge Delete operation absent")
	}
	v.cause = pc.LifecycleCause{OperationID: r.Operation.ID, Action: pc.Delete, ProjectVersion: r.Operation.ProjectVersion}
	v.scope = pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.a}
}
func (v *knowledgeLifecycleApp) stopRun(t *testing.T, want pc.StopState) {
	t.Helper()
	err := v.stop.Run(v.ctx, v.a, v.cause.OperationID)
	workAppCheck(t, "knowledge-original-stop-return", err)
	if v.report.Details().State != want {
		t.Fatalf("Knowledge stop state=%s want=%s", v.report.Details().State, want)
	}
}
func (v *knowledgeLifecycleApp) settle(t *testing.T) {
	t.Helper()
	for n := 0; n < 8; n++ {
		err := v.stop.Run(v.ctx, v.a, v.cause.OperationID)
		workAppCheck(t, "knowledge-original-stop-convergence", err)
		if v.report.Details().State == pc.Stopped {
			v.needsConfirmation = false
			return
		}
	}
	t.Fatal("Knowledge original owners did not stop in the bounded explicit rounds")
}

func (v *knowledgeLifecycleApp) http(t *testing.T, project id.ProjectID, document kc.DocumentID, suffix string, body any) []byte {
	t.Helper()
	method := http.MethodGet
	var raw []byte
	key := ""
	if body != nil {
		method = http.MethodPost
		raw = workAppJSON(t, body)
		key = guardID[struct{}](t).String()
	}
	r, err := v.request(v.ctx, method, "/api/v1/projects/"+project.String()+"/knowledge/documents/"+document.String()+suffix, raw, key)
	workAppCheck(t, "knowledge-http-"+suffix, err)
	if r.status != http.StatusOK {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(r.body, &problem)
		t.Fatalf("Knowledge HTTP %s status=%d code=%s", suffix, r.status, problem.Code.Safe())
	}
	return r.body
}
func (v *knowledgeLifecycleApp) seed(t *testing.T) {
	t.Helper()
	a, b := guardID[kc.Document](t), guardID[kc.Document](t)
	v.http(t, v.a, a, "/create-text", map[string]any{"parent_document_id": nil, "title": "Original Knowledge", "media_type": kc.PlainText, "text": "before canonical 世界"})
	v.http(t, v.a, a, "/replace-text", map[string]any{"expected_version": "1", "media_type": kc.PlainText, "text": "after canonical 世界"})
	v.http(t, v.b, b, "/create-text", map[string]any{"parent_document_id": nil, "title": "Other Knowledge", "media_type": kc.PlainText, "text": "source canonical 🌍"})
	head, err := v.pair.Service().GetDocument(v.ctx, v.actor, v.a, a)
	workAppCheck(t, "knowledge-original-document", err)
	if head.Active == nil || head.Active.ContentVersion != 2 {
		t.Fatal("HTTP replacement did not publish version 2")
	}
	v.document = *head.Active
	head, err = v.pair.Service().GetDocument(v.ctx, v.actor, v.b, b)
	workAppCheck(t, "knowledge-original-other-document", err)
	if head.Active == nil {
		t.Fatal("other document absent")
	}
	v.otherDocument = *head.Active
	var content struct {
		Text *struct {
			Text      string `json:"text"`
			Truncated bool   `json:"truncated"`
		} `json:"text"`
	}
	workAppCheck(t, "knowledge-http-canonical-content", json.Unmarshal(v.http(t, v.a, a, "/content", nil), &content))
	if content.Text == nil || content.Text.Text != "after canonical 世界" || content.Text.Truncated {
		t.Fatal("HTTP canonical bytes differ")
	}
	source, err := kc.NewTextSource(kc.PlainText, "child canonical")
	workAppCheck(t, "knowledge-child-source", err)
	defer source.Close()
	_, err = v.pair.Service().CreateDocument(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), kc.CreateRequest{ProjectID: v.a, DocumentID: guardID[kc.Document](t), ParentDocumentID: &a, Title: "Child"}, source)
	workAppCheck(t, "knowledge-original-child-create", err)
	ref, err := obj.NewBusinessFileRef(obj.BusinessFileDetails{Kind: obj.KnowledgeFile, ProjectID: v.b, DocumentID: b.String(), Revision: v.otherDocument.ContentVersion})
	workAppCheck(t, "knowledge-exact-source-ref", err)
	business, err := kc.NewBusinessSource(ref)
	workAppCheck(t, "knowledge-exact-source-input", err)
	defer business.Close()
	copied, err := v.pair.Service().CreateDocument(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), kc.CreateRequest{ProjectID: v.a, DocumentID: guardID[kc.Document](t), Title: "Cross project copy"}, business)
	workAppCheck(t, "knowledge-original-cross-project-create", err)
	if copied.ProjectID != v.a || copied.ObjectID == v.otherDocument.ObjectID || copied.ContentVersion != 1 {
		t.Fatal("cross-project copy lost its independent object")
	}
	read, err := v.pair.Service().OpenCanonical(v.ctx, v.actor, v.a, copied.ID, nil)
	workAppCheck(t, "knowledge-original-copied-canonical", err)
	body, readErr := io.ReadAll(read)
	closeErr := read.Close()
	workAppCheck(t, "knowledge-copied-read", readErr)
	workAppCheck(t, "knowledge-copied-original-close", closeErr)
	if string(body) != "source canonical 🌍" {
		t.Fatal("business Source copied wrong bytes")
	}
	var dual, leased, active int
	workAppCheck(t, "knowledge-real-dual-project-and-source-lease", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_knowledge.project_calls WHERE project_id=$1::uuid AND source_project_id=$2::uuid AND lane='command' AND phase='business_closed'),
 (SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$3::uuid AND owner_kind='source'),
 (SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$3::uuid AND owner_kind='source' AND state='active')`, v.a.String(), v.b.String(), v.otherDocument.ObjectID.String()).Scan(&dual, &leased, &active))
	if dual != 1 || leased < 1 || active != 0 {
		t.Fatal("original Source acquisition/return facts absent")
	}
}
func (v *knowledgeLifecycleApp) other(t *testing.T) {
	t.Helper()
	head, err := v.pair.Service().GetDocument(v.ctx, v.actor, v.b, v.otherDocument.ID)
	workAppCheck(t, "knowledge-unrelated-project-read", err)
	// CreatorRef is opaque and contains a function; compare its complete formal
	// document projection, including created_by, rather than function identity.
	if head.Active == nil || !bytes.Equal(workAppJSON(t, *head.Active), workAppJSON(t, v.otherDocument)) {
		t.Fatal("unrelated Knowledge document changed")
	}
}
func knowledgeAppReadReturn(project id.ProjectID) *usageAppHook {
	return &usageAppHook{match: usageAppOwner("knowledge-call-return", project), selectRow: func(ctx context.Context, x postgres.SQLExecutor, c f.TransactionCause, _, _ int64) (bool, error) {
		var found bool
		err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.project_calls WHERE id=$1::uuid AND lane='read' AND phase='business_closed')`, c.Details().RecoveryRunID).Scan(&found)
		return found, err
	}}
}
func knowledgeAppWholeReturn(t *testing.T) {
	v := newKnowledgeLifecycleApp(t)
	v.seed(t)
	reader, err := v.pair.Service().OpenCanonical(v.ctx, v.actor, v.a, v.document.ID, nil)
	workAppCheck(t, "knowledge-held-canonical-open", err)
	v.readers = append(v.readers, reader)
	raw, err := io.ReadAll(reader)
	workAppCheck(t, "knowledge-held-canonical-read", err)
	if string(raw) != "after canonical 世界" {
		t.Fatal("held reader returned wrong content")
	}
	h := knowledgeAppReadReturn(v.a)
	v.tracked.arm(h)
	get := v.start(func() error { _, err := v.pair.Service().GetDocument(v.ctx, v.actor, v.a, v.document.ID); return err })
	observed := usageAppObserve(t, h)
	closing := knowledgeAppReadReturn(v.a)
	v.tracked.arm(closing)
	closeCall := v.start(reader.Close)
	closeObserved := usageAppObserve(t, closing)
	v.accept(t)
	v.stopRun(t, pc.StopPending)
	v.other(t)
	v.requireGuard(t)
	for _, done := range []<-chan struct{}{get.done, closeCall.done, observed.returned, closeObserved.returned} {
		select {
		case <-done:
			t.Fatal("original call escaped held Store return")
		default:
		}
	}
	h.open()
	usageAppWait(t, get.done, "original Knowledge Get return")
	if get.err != nil && !errors.Is(get.err, context.Canceled) {
		workAppCheck(t, "knowledge-held-get-return", get.err)
	}
	v.stopRun(t, pc.StopPending)
	select {
	case <-closeCall.done:
		t.Fatal("canonical Close joined before its original Store returned")
	default:
	}
	closing.open()
	usageAppWait(t, closeCall.done, "original typed CanonicalRead.Close")
	if closeCall.err != nil && !errors.Is(closeCall.err, context.Canceled) {
		workAppCheck(t, "knowledge-held-canonical-close", closeCall.err)
	}
	v.settle(t)
	t.Log("KNOWLEDGE_DEFAULT_APP_RETURN real_http_create_replace_content=true exact_cross_project_source=true source_lease_released=true original_get_store_held=true original_typed_close_store_held=true closed_row_not_return=true unrelated_project=true no_source_project_stop_claim=true default_registry_unbound=true")
}

var knowledgeAppTables = []string{"documents", "commands", "command_events", "publications", "work_claims", "object_cleanup", "project_object_retirements", "project_calls"}

func (v *knowledgeLifecycleApp) snapshot(t *testing.T, project id.ProjectID, originalCalls []string) []byte {
	t.Helper()
	var queries []string
	for _, table := range knowledgeAppTables {
		condition := "project_id=$1::uuid"
		if table == "project_calls" && originalCalls != nil {
			condition += " AND id=ANY($2::uuid[])"
		}
		queries = append(queries, "(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb) FROM agenteam_knowledge."+table+" t WHERE "+condition+")")
	}
	args := []any{project.String()}
	if originalCalls != nil {
		args = append(args, originalCalls)
	}
	var raw []byte
	workAppCheck(t, "knowledge-complete-eight-table-image", v.admin.QueryRow(v.ctx, "SELECT jsonb_build_array("+strings.Join(queries, ",")+")", args...).Scan(&raw))
	return raw
}
func knowledgeAppRequireUnknown(t *testing.T, err error, original usageAppObservation) {
	t.Helper()
	var problem *f.Fault
	if !errors.As(err, &problem) || problem.Code != f.CommitUnknown || problem.CommitState != f.Unknown || problem.CauseID != original.masked.AttemptID().String() {
		t.Fatal("original masked Knowledge attempt identity absent", err)
	}
}
func knowledgeAppUnknown(t *testing.T) {
	v := newKnowledgeLifecycleApp(t)
	v.seed(t)
	h := knowledgeAppReadReturn(v.a)
	h.mask = true
	v.tracked.arm(h)
	v.needsConfirmation = true
	c := v.start(func() error { _, err := v.pair.Service().GetDocument(v.ctx, v.actor, v.a, v.document.ID); return err })
	original := usageAppObserve(t, h)
	h.open()
	usageAppWait(t, c.done, "original Knowledge technical Unknown")
	knowledgeAppRequireUnknown(t, c.err, original)
	v.accept(t)
	before := v.snapshot(t, v.a, nil)
	confirm := &usageAppHook{match: func(c f.TransactionCause) bool { return usageAppSameCause(c, original.cause) }, selectRow: usageAppAny}
	v.tracked.arm(confirm)
	v.inspecting = true
	read := v.start(func() error { return v.stop.Run(v.ctx, v.a, v.cause.OperationID) })
	observed := usageAppObserve(t, confirm)
	if observed.after-observed.before != 0 || !reflect.DeepEqual(before, v.snapshot(t, v.a, nil)) {
		t.Fatal("original Knowledge confirmation wrote or changed complete image")
	}
	select {
	case <-read.done:
		t.Fatal("Inspect returned before original confirmation Store")
	default:
	}
	v.other(t)
	v.requireGuard(t)
	confirm.open()
	usageAppWait(t, read.done, "original Knowledge readonly confirmation")
	workAppCheck(t, "knowledge-original-confirmation-return", read.err)
	if v.report.Details().State != pc.StopPending {
		t.Fatal("confirmation advanced to Stopped in the same explicit call")
	}
	v.inspecting = false
	v.settle(t)
	t.Log("KNOWLEDGE_DEFAULT_APP_UNKNOWN original_technical_return_unknown=true exact_attempt=true same_original_cause_confirmation=true inspect_same_tx_zero_knowledge_object_writes=true complete_image_equal=true original_store_return=true later_explicit_stop=true no_network_fault=true")
}

func (v *knowledgeLifecycleApp) cleanupEntry(t *testing.T) {
	t.Helper()
	v.accept(t)
	v.settle(t)
	key, _ := f.ProjectLock(v.a.String())
	cause, _ := f.NewRecoveryCause("knowledge-app-controlled-stop-handoffs", guardID[struct{}](t).String(), v.a.String())
	result := v.tracked.WithinTx(v.ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := v.tracked.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
			return err
		}
		x, err := v.tracked.InTx(tx)
		if err != nil {
			return err
		}
		// Only unavailable domains' Stop handoffs are controlled. Knowledge has no
		// cleanup predecessor; in particular Execution Completed is not fabricated.
		if _, err = x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET stop_state='stopped',version=version+1 WHERE operation_id=$1::uuid`, v.cause.OperationID.String()); err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET state='cleaning',cleanup_stage='domains',version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid AND state='stopping'`, v.cause.OperationID.String())
		return err
	})
	if result.State() != f.Committed {
		t.Fatal("Knowledge controlled Stop handoff failed", result.State(), result.Fault())
	}
	var err error
	v.cleaner, err = project.NewLifecycleCleanupDriverForKnowledge(v.tracked, v.bound.lifecycle, v.processes, v.knowledgeCalls.cleanup)
	workAppCheck(t, "knowledge-original-fixed-cleanup-driver", err)
}
func (v *knowledgeLifecycleApp) objectSnapshot(t *testing.T, ids []string) []byte {
	t.Helper()
	var parts []string
	for _, table := range []string{"objects", "uploads", "upload_attempts", "object_references", "object_leases", "cleanup_operations"} {
		key := "object_id"
		if table == "objects" {
			key = "id"
		}
		parts = append(parts, "(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb) FROM agenteam_object."+table+" t WHERE "+key+"=ANY($1::uuid[]))")
	}
	var raw []byte
	workAppCheck(t, "knowledge-object-owned-image", v.admin.QueryRow(v.ctx, "SELECT jsonb_build_array("+strings.Join(parts, ",")+")", ids).Scan(&raw))
	return raw
}
func (v *knowledgeLifecycleApp) eventSnapshot(t *testing.T, ids []string) []byte {
	t.Helper()
	var raw []byte
	workAppCheck(t, "knowledge-outbox-original-event-image", v.admin.QueryRow(v.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events t WHERE id=ANY($1::uuid[])`, ids).Scan(&raw))
	return raw
}

// Only the failed original round is diagnosed. Do not log driver messages,
// SQL, identifiers, document material, or private error causes.
func (v *knowledgeLifecycleApp) cleanupFailure(t *testing.T, round int, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var databaseError *postgres.Error
	databaseCode, sqlState := "none", ""
	if errors.As(err, &databaseError) {
		databaseCode, sqlState = string(databaseError.Code()), databaseError.SQLState()
	}
	var reference, object, metadata, completed int
	var missingUpload bool
	// This is a post-failure durable image, not proof of which SQL statement failed.
	phaseErr := v.admin.QueryRow(v.ctx, `SELECT
 count(*) FILTER (WHERE phase='reference'), count(*) FILTER (WHERE phase='object'),
 count(*) FILTER (WHERE phase='metadata'), count(*) FILTER (WHERE phase='completed'),
 EXISTS(SELECT 1 FROM agenteam_knowledge.project_object_retirements l
 WHERE l.project_id=$1::uuid AND l.phase<>'completed' AND NOT EXISTS(
 SELECT 1 FROM agenteam_object.uploads u WHERE u.id=l.upload_id AND u.object_id=l.object_id))
 FROM agenteam_knowledge.project_object_retirements WHERE project_id=$1::uuid`, v.a.String()).Scan(&reference, &object, &metadata, &completed, &missingUpload)
	t.Logf("KNOWLEDGE_CLEANUP_FAILURE round=%d database_code=%s sqlstate=%s canceled=%t deadline=%t no_rows=%t leaf_snapshot_ok=%t reference=%d object=%d metadata=%d completed=%d unfinished_leaf_missing_upload=%t", round, databaseCode, sqlState, errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, pgx.ErrNoRows), phaseErr == nil, reference, object, metadata, completed, missingUpload)
}

func knowledgeAppCleanup(t *testing.T) {
	v := newKnowledgeLifecycleApp(t)
	v.seed(t)
	var originals, otherIDs, bCalls []string
	workAppCheck(t, "knowledge-exact-original-object-ids", v.admin.QueryRow(v.ctx, `SELECT ARRAY(SELECT object_id::text FROM agenteam_object.uploads WHERE project_id=$1::uuid AND owner_kind='knowledge' ORDER BY object_id)`, v.a.String()).Scan(&originals))
	if len(originals) != 4 {
		t.Fatalf("Knowledge real publication object count=%d want=4", len(originals))
	}
	for _, table := range knowledgeAppTables[:6] {
		var n int
		workAppCheck(t, "knowledge-nonempty-"+table, v.admin.QueryRow(v.ctx, "SELECT count(*) FROM agenteam_knowledge."+table+" WHERE project_id=$1::uuid", v.a.String()).Scan(&n))
		if n == 0 {
			t.Fatal("real business history missing for", table)
		}
	}
	workAppCheck(t, "knowledge-other-object-ids", v.admin.QueryRow(v.ctx, `SELECT ARRAY(SELECT id::text FROM agenteam_object.objects WHERE NOT(id=ANY($1::uuid[])) ORDER BY id)`, originals).Scan(&otherIDs))
	workAppCheck(t, "knowledge-other-call-ids", v.admin.QueryRow(v.ctx, `SELECT ARRAY(SELECT id::text FROM agenteam_knowledge.project_calls WHERE project_id=$1::uuid ORDER BY id)`, v.b.String()).Scan(&bCalls))
	if len(otherIDs) == 0 || len(bCalls) == 0 {
		t.Fatal("other project's actual history absent")
	}
	otherBefore := v.snapshot(t, v.b, bCalls)
	objectsBefore := v.objectSnapshot(t, otherIDs)
	v.cleanupEntry(t)
	var eventIDs []string
	workAppCheck(t, "knowledge-original-outbox-event-ids", v.admin.QueryRow(v.ctx, `SELECT ARRAY(SELECT id::text FROM agenteam_outbox.events WHERE project_id=ANY($1::uuid[]) ORDER BY id)`, []string{v.a.String(), v.b.String()}).Scan(&eventIDs))
	if len(eventIDs) == 0 {
		t.Fatal("formal original Outbox events missing")
	}
	eventsBefore := v.eventSnapshot(t, eventIDs)
	h := &usageAppHook{match: usageAppOwner("knowledge-project-cleanup", v.a), mask: true, selectRow: func(_ context.Context, _ postgres.SQLExecutor, _ f.TransactionCause, before, after int64) (bool, error) {
		return after > before, nil
	}}
	v.tracked.arm(h)
	// Four actual canonical/replaced objects each traverse bounded original
	// leaf phases, followed by six history tables and four one-row commands.
	// These are separate explicit calls, not retries of a failed native run.
	const rounds = 40
	var original usageAppObservation
	var held *usageAppCall
	for n := 0; n < rounds && held == nil; n++ {
		c := v.start(func() error { return v.cleaner.Run(v.ctx, v.a, v.cause.OperationID) })
		select {
		case original = <-h.observed:
			held = c
		case <-c.done:
			v.cleanupFailure(t, n+1, c.err)
			workAppCheck(t, "knowledge-object-leaf-next-explicit-round", c.err)
		case <-time.After(5 * time.Second):
			t.Fatal("Knowledge cleanup round did not return or reach actual business deletion")
		}
	}
	if held == nil || original.result.State() != f.Committed {
		t.Fatal("original Knowledge business deletion was not observed")
	}
	h.open()
	usageAppWait(t, held.done, "Knowledge cleanup original Unknown")
	knowledgeAppRequireUnknown(t, held.err, original)
	before := v.snapshot(t, v.a, nil)
	confirm := &usageAppHook{match: func(c f.TransactionCause) bool { return usageAppSameCause(c, original.cause) }, selectRow: usageAppAny}
	v.tracked.arm(confirm)
	read := v.start(func() error { return v.cleaner.Run(v.ctx, v.a, v.cause.OperationID) })
	observed := usageAppObserve(t, confirm)
	if observed.after-observed.before != 0 || !reflect.DeepEqual(before, v.snapshot(t, v.a, nil)) {
		t.Fatal("Knowledge cleanup confirmation wrote or altered original history")
	}
	other, err := project.NewLifecycleCleanupDriverForKnowledge(v.tracked, v.bound.lifecycle, v.processes, v.knowledgeCalls.cleanup)
	workAppCheck(t, "knowledge-same-life-second-driver", err)
	err = other.Run(v.ctx, v.a, v.cause.OperationID)
	var busy *f.Fault
	if !errors.As(err, &busy) || busy.Code != f.ResourceBusy {
		t.Fatal("same-Life driver bypassed original confirmation return", err)
	}
	other.Stop()
	workAppCheck(t, "knowledge-second-driver-drain", other.Drain(v.ctx))
	v.other(t)
	confirm.open()
	usageAppWait(t, read.done, "Knowledge original cleanup confirmation")
	workAppCheck(t, "knowledge-cleanup-confirmation-return", read.err)
	if !reflect.DeepEqual(before, v.snapshot(t, v.a, nil)) {
		t.Fatal("confirmation performed next deletion after its original Store returned")
	}
	complete := false
	for n := 0; n < rounds; n++ {
		workAppCheck(t, "knowledge-later-explicit-cleanup", v.cleaner.Run(v.ctx, v.a, v.cause.OperationID))
		workAppCheck(t, "knowledge-final-cleanup-state", v.admin.QueryRow(v.ctx, `SELECT cleanup_state='completed' FROM agenteam_project.lifecycle_participants WHERE operation_id=$1::uuid AND participant_name='knowledge'`, v.cause.OperationID.String()).Scan(&complete))
		if complete {
			break
		}
	}
	if !complete {
		t.Fatal("Knowledge bounded original cleanup did not complete")
	}
	for _, table := range knowledgeAppTables {
		var n int
		workAppCheck(t, "knowledge-final-empty-"+table, v.admin.QueryRow(v.ctx, "SELECT count(*) FROM agenteam_knowledge."+table+" WHERE project_id=$1::uuid", v.a.String()).Scan(&n))
		if n != 0 {
			t.Fatal("Knowledge retained its own history", table, n)
		}
	}
	var objectRows [][]json.RawMessage
	workAppCheck(t, "knowledge-final-object-metadata", json.Unmarshal(v.objectSnapshot(t, originals), &objectRows))
	for _, rows := range objectRows {
		if len(rows) != 0 {
			t.Fatal("Knowledge original Object metadata did not actually retire")
		}
	}
	if !reflect.DeepEqual(otherBefore, v.snapshot(t, v.b, bCalls)) || !reflect.DeepEqual(objectsBefore, v.objectSnapshot(t, otherIDs)) || !reflect.DeepEqual(eventsBefore, v.eventSnapshot(t, eventIDs)) {
		t.Fatal("Knowledge cleanup changed B or foreign Object facts")
	}
	var preserved bool
	workAppCheck(t, "knowledge-preserve-project-and-other-domain-checkpoints", v.admin.QueryRow(v.ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.projects WHERE id=$1::uuid) AND NOT EXISTS(SELECT 1 FROM agenteam_project.lifecycle_participants WHERE operation_id=$2::uuid AND participant_name<>'knowledge' AND cleanup_state='completed')`, v.a.String(), v.cause.OperationID.String()).Scan(&preserved))
	if !preserved {
		t.Fatal("Knowledge fixed driver advanced another domain or deleted Project")
	}
	v.other(t)
	t.Log("KNOWLEDGE_DEFAULT_APP_CLEANUP six_nonempty_business_tables=true original_four_object_leaf_metadata_retired=true closed_call_ledger_removed=true original_unknown_zero_writes=true same_life_busy_until_original_return=true later_explicit_run=true other_project_objects_outbox_events_preserved=true no_execution_predecessor=true controlled_other_stop_handoffs=true no_default_registry_or_full_delete=true")
}
