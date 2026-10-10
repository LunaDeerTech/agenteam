package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/jackc/pgx/v5"
)

func TestKnowledgeRootProcessEnvironmentConfiguration(t *testing.T) {
	// Exercise the very environment used by launchFixture without starting a
	// child or opening its listeners. The product's strict loader is unchanged.
	values := map[string]string{}
	for _, entry := range appProcessEnvironment(t, "configuration-control", "1s") {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatal("malformed fixture environment")
		}
		values[key] = value
	}
	load := func() error {
		cfg, err := config.Load(func(key string) (string, bool) { value, ok := values[key]; return value, ok }, nil)
		if err == nil {
			return cfg.Validate()
		}
		return err
	}
	if err := load(); err != nil {
		t.Fatal("actual fixture inputs rejected", err)
	}
	field := config.Prefix + "KNOWLEDGE_CONFIRMATION_KEYRING"
	delete(values, field)
	for _, mode := range []string{"missing", "reuse_account"} {
		if mode == "reuse_account" {
			values[field] = values[config.Prefix+"ACCOUNT_KEYRING"]
		}
		var issue *config.Error
		if err := load(); !errors.As(err, &issue) || issue.Field() != field || issue.Reason() != "invalid" {
			t.Fatal("missing or reused fixture material accepted", mode)
		}
	}
}

type contentRootNoRow struct{}

func (contentRootNoRow) Scan(...any) error { return pgx.ErrNoRows }

type contentRootHeldStore struct {
	projectUsageRootStore
	entered, release chan struct{}
	once             sync.Once
}

func (*contentRootHeldStore) QueryRow(context.Context, string, ...any) postgres.Row {
	// Skill discovery finds no initialization. Only the original transaction
	// is held below; no Store authorization callback is forged as successful.
	return contentRootNoRow{}
}
func (s *contentRootHeldStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}

func TestKnowledgeSkillRootActualDrain(t *testing.T) {
	for _, domain := range []string{"knowledge", "skill"} {
		t.Run(domain, func(t *testing.T) {
			store := &contentRootHeldStore{entered: make(chan struct{}), release: make(chan struct{})}
			var release sync.Once
			defer release.Do(func() { close(store.release) })
			cfg := testConfig(t, "1s")
			accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
			if err != nil {
				t.Fatal(err)
			}
			projects, err := createProjectUsage(cfg, store, accounts)
			if err != nil {
				t.Fatal(err)
			}
			authorities, err := createKnowledgeSkillAuthorities(store, projects)
			if err != nil {
				t.Fatal(err)
			}
			actor, _ := id.NewHuman(updateRootID[id.User](), updateRootID[id.Session]())
			project := updateRootID[id.Project]()
			var work accountWork
			var call func() error
			assembly := &accountAssembly{}
			if domain == "knowledge" {
				events, err := kc.RegisterKnowledgeEvents(ec.NewCatalog())
				if err != nil {
					t.Fatal(err)
				}
				// These unused concrete ports would reject/panic if invoked. This
				// control proves real Service call retirement around a held Store,
				// not Object I/O, authorization, or default-root integration.
				objects := &object.Service{}
				service, err := knowledge.New(store, knowledge.Dependencies{Projects: projects.projects, Activity: accounts,
					Objects: objects, Uploads: objects, Sources: &knowledge.SourceResolver{}, SourceReads: &object.SourceReads{}, ReferenceCleanup: objects, ObjectCleanup: objects,
					Audit: &audit.Service{}, Outbox: &outbox.Service{}, Events: events,
					Processes: outboxProcessAuthority{process: updateRootID[oc.Process](), guard: &object.ProcessGuard{}}, Cursors: cfg.CursorKeyring(), Confirmations: cfg.KnowledgeConfirmationKeys()})
				if err != nil {
					t.Fatal(err)
				}
				work = &knowledgeWork{service: service}
				assembly.knowledge = work
				call = func() error {
					_, err := service.GetDocument(context.Background(), actor, project, updateRootID[kc.Document]())
					return err
				}
			} else {
				bundle, err := skill.AddSkills(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				service, err := skill.New(skill.Dependencies{Authority: authorities.skills, Objects: &object.Service{}, Processes: &object.ProcessGuard{}, ProcessID: updateRootID[oc.Process](), Bundle: bundle})
				if err != nil {
					t.Fatal(err)
				}
				work = &skillWork{service: service}
				assembly.skills = work
				call = func() error { _, err := service.ListSkills(context.Background(), actor, project); return err }
			}
			if work.Joined() {
				t.Fatal("new service already considered joined")
			}
			done := make(chan error, 1)
			go func() { done <- call() }()
			select {
			case <-store.entered:
			case <-time.After(time.Second):
				t.Fatal("original transaction not entered")
			}
			ended, cancel := context.WithCancel(context.Background())
			cancel()
			if err := assembly.Force(ended); !errors.Is(err, context.Canceled) || work.Joined() || assembly.Joined() {
				t.Fatal("cancellation or attempted Drain pretended domain join", err)
			}
			owned := &resources{stopping: true, accountService: assembly}
			if owned.producersJoined() {
				t.Fatal("shared Object guard became releasable before the actual domain tail")
			}
			select {
			case <-done:
				t.Fatal("original call was abandoned")
			default:
			}
			release.Do(func() { close(store.release) })
			if err := <-done; err == nil {
				t.Fatal("controlled failed transaction became successful")
			}
			if err := assembly.Drain(context.Background()); err != nil || !work.Joined() || !owned.producersJoined() {
				t.Fatal("original call completion did not retire its actual owner", err)
			}
		})
	}
}

func TestKnowledgeSkillRootProviderOrder(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "drain", true: "force"}[force], func(t *testing.T) {
			projects, skills, documents, core := &b04Work{}, &b04Work{}, &b04Work{}, &b04Work{}
			assembly := &accountAssembly{projects: projects, skills: skills, knowledge: documents, core: core}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var order []string
			for _, entry := range []struct {
				name string
				work *b04Work
			}{{"project", projects}, {"skill", skills}, {"knowledge", documents}, {"account", core}} {
				finish := func(got context.Context) error {
					if got != ctx || !projects.stopped.Load() || !skills.stopped.Load() || !documents.stopped.Load() || !core.stopped.Load() {
						t.Fatal("shutdown renewed context or left a caller admitted")
					}
					order = append(order, entry.name)
					entry.work.joined.Store(true)
					return nil
				}
				entry.work.drain, entry.work.force = finish, finish
			}
			var err error
			if force {
				err = assembly.Force(ctx)
			} else {
				err = assembly.Drain(ctx)
			}
			if err != nil || !assembly.Joined() || !reflect.DeepEqual(order, []string{"project", "skill", "knowledge", "account"}) {
				t.Fatal("caller/provider retirement order lost", order, err)
			}
		})
	}
}

func TestKnowledgeSkillRootRoutes(t *testing.T) {
	const project = "/api/v1/projects/01900000-0000-7000-8000-000000000001"
	const document = "/01900000-0000-7000-8000-000000000002"
	for _, tc := range []struct{ path, want string }{
		{project + "/knowledge/documents", "knowledge"},
		{project + "/knowledge/documents" + document, "knowledge"},
		{project + "/knowledge/documents" + document + "/move", "command"},
		{project + "/knowledge/documents" + document + "/content", "content"},
		{project + "/skills", "skill"},
		{"/api/v1/account/profile", "existing"},
		{project + "/milestones", "existing"},
		{project + "/variables", "existing"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			var got string
			handler := func(name string) http.Handler {
				return http.HandlerFunc(func(http.ResponseWriter, *http.Request) { got = name })
			}
			routes := knowledgeSkillRoutes(handler("existing"), handler("knowledge"), handler("command"), handler("content"), handler("skill"))
			routes.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", tc.path, nil))
			if got != tc.want {
				t.Fatal("root shadowed another handler", got, tc.want)
			}
		})
	}
}
