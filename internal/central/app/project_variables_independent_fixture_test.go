//go:build integration

package app

// Project preparation uses the existing persisted Skills fixture contract. It
// runs actual Project Create and same-Tx confirmation; it is not production
// Skills publication and supplies no Variable authorization/result oracle.
import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

func independentForceProject(t *testing.T, db *pgfixture.Database, cfg config.Config, actor identity.Actor) pc.ProjectRef {
	t.Helper()
	store, err := postgres.Open(databaseTestContext(t), db.Config(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := store.ForceClose(ctx); err != nil {
			t.Error("Project preparation store did not close", err)
		}
	}()
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	pa, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts})
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: pa})
	if err != nil {
		t.Fatal(err)
	}
	catalog := ec.NewCatalog()
	events, err := pc.RegisterProjectEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	processID, err := foundation.NewID[oc.Process]()
	if err != nil {
		t.Fatal(err)
	}
	processes := independentForceProcess{processID}
	box, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[ec.StableName]oc.ProducerAuthority{pc.ProjectProducer: pa}, Projects: pa, Sessions: accounts, System: accounts, Processes: processes, Audit: aud, Cursors: cfg.CursorKeyring()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Exec(databaseTestContext(t), `CREATE SCHEMA independent_force_fixture; CREATE TABLE independent_force_fixture.skills(creation_id uuid PRIMARY KEY,project_id uuid NOT NULL UNIQUE,init_key text NOT NULL,skill_id uuid NOT NULL UNIQUE,revision bigint NOT NULL,protected boolean NOT NULL,published boolean NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	skills := &independentForceSkills{store: store, authority: pa, issuer: pc.NewInitializationPlanIssuer()}
	projects, err := project.New(store, project.Dependencies{Authority: pa, Activity: accounts, Audit: aud, Events: box, ProjectEvents: events, Initializer: skills, Processes: processes, Cursors: cfg.CursorKeyring()}, project.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		projects.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := projects.Drain(ctx); err != nil {
			t.Error("Project preparation commands did not join", err)
		}
	}()
	projectID, err := foundation.NewID[identity.Project]()
	if err != nil {
		t.Fatal(err)
	}
	requestID, err := foundation.NewID[foundation.Request]()
	if err != nil {
		t.Fatal(err)
	}
	meta := foundation.CommandMeta{RequestID: requestID, IdempotencyKey: foundation.IdempotencyKey(guardID[foundation.Request](t).String())}
	result, err := projects.CreateProject(databaseTestContext(t), actor, meta, pc.CreateProjectRequest{ProjectID: projectID, Name: "independent-force", Description: "owned Project prepared through real service"})
	if err != nil || result.State != pc.CreationReady || result.Project == nil {
		t.Fatal("actual Project creation with fixture Skills", err)
	}
	return *result.Project
}

type independentForceProcess struct{ id oc.ProcessID }

func (p independentForceProcess) CurrentProcess() oc.ProcessID { return p.id }
func (p independentForceProcess) ConfirmStopped(context.Context, oc.ProcessID) error {
	return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
}

type independentForceSkills struct {
	store     project.Store
	authority *project.Authority
	issuer    pc.InitializationPlanIssuer
}

func (s *independentForceSkills) InspectProjectSkills(ctx context.Context, actor identity.Actor, r pc.InitializationRequest) (pc.InitializationResult, error) {
	if actor.Details().ServiceName != identity.ProjectInitialization || actor.Details().CauseRef != r.CreationID.String() || actor.Details().ProjectID != r.ProjectID.String() {
		return pc.InitializationResult{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	var project, key, skill string
	var revision int64
	e := s.store.QueryRow(ctx, `SELECT project_id::text,init_key,skill_id::text,revision FROM independent_force_fixture.skills WHERE creation_id=$1`, r.CreationID.String()).Scan(&project, &key, &skill, &revision)
	if errors.Is(e, pgx.ErrNoRows) {
		return pc.InitializationResult{State: pc.InitializationResultPending, CreationID: r.CreationID, ProjectID: r.ProjectID, SafeReason: pc.ReasonWorkPending}, nil
	}
	if e != nil {
		return pc.InitializationResult{}, e
	}
	if project != r.ProjectID.String() || key != string(r.InitializationKey) {
		return pc.InitializationResult{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	skillID, e := foundation.ParseID[pc.Skill](skill)
	if e != nil {
		return pc.InitializationResult{}, e
	}
	rev := foundation.Revision(revision)
	return pc.InitializationResult{State: pc.InitializationCompleted, CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: &skillID, Revision: &rev}, nil
}
func (s *independentForceSkills) InitializeProjectSkills(ctx context.Context, actor identity.Actor, r pc.InitializationRequest) (pc.InitializationResult, error) {
	projectKey, _ := foundation.ProjectLock(r.ProjectID.String())
	command, _ := foundation.NewRecoveryCause("project.fixture-skill", r.CreationID.String(), "")
	result := s.store.WithinTx(ctx, command, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: projectKey, Mode: foundation.Exclusive}}); e != nil {
			return e
		}
		if e := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); e != nil {
			return e
		}
		x, e := s.store.InTx(tx)
		if e != nil {
			return e
		}
		skill, e := foundation.NewID[pc.Skill]()
		if e != nil {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO independent_force_fixture.skills(creation_id,project_id,init_key,skill_id,revision,protected,published) VALUES($1,$2,$3,$4,1,true,true) ON CONFLICT(creation_id) DO NOTHING`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), skill.String())
		return e
	})
	if result.State() != foundation.Committed {
		return pc.InitializationResult{}, foundation.NewFault(foundation.DependencyUnavailable, result.State())
	}
	return s.InspectProjectSkills(ctx, actor, r)
}
func (s *independentForceSkills) DiscoverConfirmation(ctx context.Context, actor identity.Actor, r pc.InitializationRequest) (pc.InitializationConfirmationPlan, error) {
	result, e := s.InspectProjectSkills(ctx, actor, r)
	if e != nil {
		return pc.InitializationConfirmationPlan{}, e
	}
	if result.State != pc.InitializationCompleted {
		return pc.InitializationConfirmationPlan{}, foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
	}
	key, _ := foundation.ProjectLock(r.ProjectID.String())
	locks := []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}
	return s.issuer.Plan(actor, r, pc.InitializationReceipt{CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: *result.AddSkillsID, Revision: *result.Revision}, locks)
}
func (s *independentForceSkills) ConfirmInitializedInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, r pc.InitializationRequest, plan pc.InitializationConfirmationPlan) (pc.InitializationReceipt, error) {
	if !s.issuer.Matches(plan, actor, r) {
		return pc.InitializationReceipt{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	locks := plan.RequiredLocks()
	if e := s.store.RequireHeldLocks(ctx, tx, locks); e != nil {
		return pc.InitializationReceipt{}, e
	}
	if e := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); e != nil {
		return pc.InitializationReceipt{}, e
	}
	x, e := s.store.InTx(tx)
	if e != nil {
		return pc.InitializationReceipt{}, e
	}
	receipt := plan.ProposedReceipt()
	var valid bool
	e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM independent_force_fixture.skills WHERE creation_id=$1 AND project_id=$2 AND init_key=$3 AND skill_id=$4 AND revision=$5 AND protected AND published)`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), receipt.AddSkillsID.String(), int64(receipt.Revision)).Scan(&valid)
	if e != nil {
		return pc.InitializationReceipt{}, e
	}
	if !valid {
		return pc.InitializationReceipt{}, foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
	}
	return receipt, nil
}
