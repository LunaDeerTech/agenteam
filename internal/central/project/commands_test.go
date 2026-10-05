package project

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	auditimpl "github.com/LunaDeerTech/agenteam/internal/central/audit"
	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

func testProject(t *testing.T) c.ProjectRef {
	now, _ := foundation.NewInstant(time.Now())
	return c.ProjectRef{ID: testID[identity.Project](t), OwnerUserID: testID[identity.User](t), Name: "Demo", NormalizedName: "demo", Description: "body", Lifecycle: c.Active, Version: 1, CreatedAt: now, UpdatedAt: now}
}
func TestUpdateSemanticChangeAndNoOpBoundary(t *testing.T) {
	p := testProject(t)
	name := "DEMO"
	description := ""
	changed, fields, e := updateValues(p, c.UpdateProjectRequest{Name: &name, Description: &description})
	if e != nil || changed.Version != 2 || changed.Name != "DEMO" || changed.NormalizedName != "demo" || changed.Description != "" || len(fields) != 2 {
		t.Fatal("case-only/presence update", e)
	}
	original := p.Description
	p.Version = foundation.Version(math.MaxInt64)
	next, fields, e := updateValues(p, c.UpdateProjectRequest{Description: &original})
	if e != nil || len(fields) != 0 || next.Version != p.Version {
		t.Fatal("no-op bumped or overflowed", e)
	}
	_, _, e = updateValues(p, c.UpdateProjectRequest{Name: &name})
	hasCode(t, e, foundation.InvalidState)
}
func TestSemanticReplayIgnoresOldVersionButNotDigestOrOwner(t *testing.T) {
	actor := testActor(t)
	p := testProject(t)
	p.OwnerUserID, _ = foundation.ParseID[identity.User](actor.Details().UserID)
	d := digest([]byte("one"))
	r := &commandRecord{user: actor.Details().UserID, semantic: d, state: "completed", result: &c.CommandResult{Command: c.UpdateCommand, Project: &p}}
	got, e := semanticReplay(r, actor, d)
	if e != nil || got.Version != 1 {
		t.Fatal(e)
	}
	_, e = semanticReplay(r, actor, digest([]byte("two")))
	hasCode(t, e, foundation.IdempotencyKeyReused)
	_, e = semanticReplay(r, testActor(t), d)
	hasCode(t, e, foundation.NotFound)
}
func TestAuditIdentityUsesExistingCanonicalDigest(t *testing.T) {
	id, _ := c.CommandIdentity(testID[identity.Project](t), c.UpdateCommand, "raw-key")
	expected, e := auditimpl.CommandAppendKey(audit.ProjectProducer, id, 0)
	if e != nil {
		t.Fatal(e)
	}
	actual, e := auditCommandKey(id, 0)
	if e != nil || actual.Details() != expected.Details() {
		t.Fatal("Audit key diverged", e)
	}
}
func TestPlanSurvivesJSONBObjectOrdering(t *testing.T) {
	p := testProject(t)
	p.Version = 2
	a := &updatePlan{ExpectedVersion: 1, Project: p, Changed: []c.ChangedField{c.NameChanged}, Header: json.RawMessage(`{"b":"two","a":"one"}`), Payload: json.RawMessage(`{"changed_fields":["name"]}`)}
	b := *a
	b.Header = json.RawMessage(`{ "a": "one", "b": "two" }`)
	if !sameUpdatePlan(a, &b) {
		t.Fatal("JSONB reordering invalidated the durable plan")
	}
	b.ExpectedVersion = 2
	if sameUpdatePlan(a, &b) {
		t.Fatal("version mutation accepted")
	}
}

func TestOwnerNameCollisionUsesEstablishedFieldCode(t *testing.T) {
	err := uniqueName(&pgconn.PgError{Code: "23505", ConstraintName: "projects_owner_name_key"})
	var f *foundation.Fault
	if !errors.As(err, &f) || f.Code != foundation.ResourceBusy || len(f.FieldErrors) != 1 || f.FieldErrors[0] != (foundation.FieldError{Path: "/name", Code: "NAME_TAKEN"}) {
		t.Fatal("owner name collision diverged from the public field contract", err)
	}
}

type preparedOnlyAppender struct{ oc.Appender }

func (preparedOnlyAppender) PrepareAppend(_ context.Context, actor identity.Actor, ev event.Event) (oc.AppendPlan, error) {
	issuer := oc.NewPlanIssuer()
	semantic, err := oc.SemanticDigest(actor, ev)
	if err != nil {
		return oc.AppendPlan{}, err
	}
	deps, err := oc.NewDependencies(issuer, semantic, nil, []byte("test-only-unexecuted-final-attempt"))
	if err != nil {
		return oc.AppendPlan{}, err
	}
	return oc.NewAppendPlan(issuer, oc.AppendPlanDetails{Event: ev, Semantic: semantic, Producer: deps, Project: deps})
}

func TestUnknownUpdateDistinguishesPlanningFromFinalAttempt(t *testing.T) {
	for _, phase := range []string{"planning", "final"} {
		t.Run(phase, func(t *testing.T) {
			order := []string{}
			store := &roundTestStore{authorityStore: &authorityStore{tx: foundation.NewTx(), order: &order}, result: foundation.UnknownResult(testID[foundation.TransactionAttempt](t), foundation.TransactionCause{}), unknownBefore: 1}
			if phase == "final" {
				store.unknownBefore = 2
			}
			service, actor := roundTestService(t, store)
			p := testProject(t)
			p.OwnerUserID, _ = foundation.ParseID[identity.User](actor.Details().UserID)
			meta := foundation.CommandMeta{RequestID: testID[foundation.Request](t), IdempotencyKey: "same-plan", ExpectedVersion: &p.Version}
			description := "after"
			request := c.UpdateProjectRequest{Description: &description}
			semantic, err := c.UpdateDigest(actor, meta, p.ID, request)
			if err != nil {
				t.Fatal(err)
			}
			registered, err := c.RegisterProjectEvents(event.NewCatalog())
			if err != nil {
				t.Fatal(err)
			}
			service.state().deps.ProjectEvents = registered
			service.state().deps.Events = preparedOnlyAppender{}
			next, fields, err := updateValues(p, request)
			if err != nil {
				t.Fatal(err)
			}
			ev, err := service.updateEvent(next, fields, next.UpdatedAt)
			if err != nil {
				t.Fatal(err)
			}
			header, err := ev.HeaderJSON()
			if err != nil {
				t.Fatal(err)
			}
			plan, err := json.Marshal(updatePlan{ExpectedVersion: p.Version, Project: next, Changed: fields, Header: header, Payload: ev.PayloadBytes()})
			if err != nil {
				t.Fatal(err)
			}
			commandID, creation, eventID := testID[struct{}](t).String(), testID[c.Creation](t), ev.Header().EventID.String()
			store.row = func(q string, _ ...any) postgres.Row {
				switch {
				case strings.Contains(q, "FROM agenteam_project.projects"):
					return valuesRow(p.ID.String(), p.OwnerUserID.String(), p.Name, p.NormalizedName, p.Description, "active", int64(1), nil, p.CreatedAt.Time(), p.UpdatedAt.Time(), nil, creation.String(), true, nil)
				case strings.Contains(q, "FROM agenteam_project.commands"):
					return valuesRow(commandID, p.ID.String(), p.OwnerUserID.String(), c.UpdateCommand, meta.IdempotencyKey, semantic, "planned", []byte(nil), plan, &eventID)
				default:
					t.Fatalf("unexpected query: %s", q)
					return nil
				}
			}
			_, err = service.UpdateProject(context.Background(), actor, meta, p.ID, request)
			want := foundation.Unknown
			if phase == "final" {
				want = foundation.NotCommitted
			}
			var f *foundation.Fault
			if !errors.As(err, &f) || f.CommitState != want {
				t.Fatal("planned state misinterpreted for original physical phase", err, phase)
			}
			original, ok := UnknownAttempt(err)
			if !ok || original.AttemptID() != store.result.AttemptID() || original.Cause().Details().Primary.Canonical() != store.result.Cause().Details().Primary.Canonical() {
				t.Fatal("original update cause lost")
			}
		})
	}
}
