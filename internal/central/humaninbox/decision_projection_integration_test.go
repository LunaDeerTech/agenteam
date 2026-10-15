//go:build integration

package humaninbox

import (
	"context"
	"slices"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/humaninbox/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// This fixture controls the Project and canonical SourceProvider ports to test
// Inbox SQL against real PostgreSQL. It does not claim a Meeting mutation or
// an Outbox delivery: those enter the separate original App journeys.
type inboxProjectionProject struct {
	pc.ProjectAuthority
	store   *postgres.Store
	actor   i.Actor
	project pc.ProjectRef
}

func (p *inboxProjectionProject) RequireOwnerInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, _ i.AccessIntent) (pc.ProjectAccess, error) {
	if !p.actor.Equal(actor) || project != p.project.ID {
		return pc.ProjectAccess{}, fault(f.Forbidden)
	}
	if _, err := p.store.InTx(tx); err != nil {
		return pc.ProjectAccess{}, err
	}
	return pc.NewProjectAccess(actor, p.project, p.project.CreatedAt)
}

type inboxProjectionSources struct {
	authority            *Authority
	proposals, decisions map[string]c.Source
}

func (s *inboxProjectionSources) ReadSourceInTx(ctx context.Context, tx f.Tx, p i.ProjectID, id string) (c.Source, error) {
	if err := s.authority.RequireSourceReadInTx(ctx, tx, s.authority.store, p, id, false); err != nil {
		return c.Source{}, err
	}
	v, ok := s.proposals[id]
	if !ok {
		return c.Source{}, fault(f.NotFound)
	}
	return v, nil
}
func (s *inboxProjectionSources) ScanSourcesInTx(ctx context.Context, tx f.Tx, p i.ProjectID) ([]c.Source, error) {
	if err := s.authority.RequireSourceReadInTx(ctx, tx, s.authority.store, p, "", true); err != nil {
		return nil, err
	}
	out := []c.Source{}
	for _, v := range s.proposals {
		out = append(out, v)
	}
	return out, nil
}
func (s *inboxProjectionSources) ReadDecisionSourceInTx(ctx context.Context, tx f.Tx, p i.ProjectID, meeting, decision string) (c.Source, error) {
	if err := s.authority.RequireDecisionSourceReadInTx(ctx, tx, s.authority.store, p, meeting, decision, false); err != nil {
		return c.Source{}, err
	}
	v, ok := s.decisions[decision]
	if !ok || v.Context.MeetingID != meeting {
		return c.Source{}, fault(f.NotFound)
	}
	return v, nil
}
func (s *inboxProjectionSources) ScanDecisionSourcesInTx(ctx context.Context, tx f.Tx, p i.ProjectID) ([]c.Source, error) {
	if err := s.authority.RequireDecisionSourceReadInTx(ctx, tx, s.authority.store, p, "", "", true); err != nil {
		return nil, err
	}
	out := []c.Source{}
	for _, v := range s.decisions {
		out = append(out, v)
	}
	return out, nil
}

func TestDecisionInboxProjectionSQLMixedCursorAndCanonicalRebuild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	migrator, err := postgres.NewMigrator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result := migrator.Migrate(ctx); !result.Migrated {
		t.Fatal("global migration", result.Fault)
	}
	store, err := postgres.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if err := store.ForceClose(closeCtx); err != nil {
			t.Error(err)
		}
	})
	user, project := inboxID[i.User](t), inboxID[i.Project](t)
	actor, err := i.NewHuman(user, inboxID[i.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	at, _ := f.NewInstant(time.Now().UTC().Truncate(time.Microsecond))
	projectPort := &inboxProjectionProject{store: store, actor: actor, project: pc.ProjectRef{ID: project, OwnerUserID: user, Name: "Inbox-fixture", NormalizedName: "inbox-fixture", Lifecycle: pc.Active, Version: 1, CreatedAt: at, UpdatedAt: at}}
	authority, err := NewAuthority(store, projectPort)
	if err != nil {
		t.Fatal(err)
	}
	sources := &inboxProjectionSources{authority: authority, proposals: map[string]c.Source{}, decisions: map[string]c.Source{}}
	catalog := event.NewCatalog()
	lifecycle, err := mc.RegisterEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := mc.RegisterDecisionEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewServiceWithDecisions(authority, sources, lifecycle, sources, decisions)
	if err != nil {
		t.Fatal(err)
	}
	proposalHandler, err := service.Handler()
	if err != nil {
		t.Fatal(err)
	}
	decisionHandler, err := service.DecisionHandler()
	if err != nil {
		t.Fatal(err)
	}
	deliver := func(handler oc.HandlerDefinition, ev event.Event) {
		t.Helper()
		plan, err := handler.Handler.Prepare(ctx, ev)
		if err != nil {
			t.Fatal(err)
		}
		cause, err := readCause()
		if err != nil {
			t.Fatal(err)
		}
		result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			if err := store.AcquireAll(ctx, tx, plan.Locks()); err != nil {
				return err
			}
			if err := handler.Handler.ValidateInTx(ctx, tx, ev, plan); err != nil {
				return err
			}
			out := handler.Handler.HandleInTx(ctx, tx, ev, plan)
			if out.Kind() != oc.Acknowledged {
				t.Errorf("projection failed: %s %s", out.Kind(), out.Reason())
				return fault(f.InvalidState)
			}
			return nil
		})
		if err := commitError(result); err != nil {
			t.Fatal(err)
		}
	}
	header := func(id string, aggregate, name event.StableName) event.Header {
		ag, _ := f.ParseID[event.Aggregate](id)
		p, _ := f.ParseID[event.Project](project.String())
		version := f.Version(1)
		return event.Header{EventID: inboxID[event.EventIdentity](t), EventType: name, SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: p}, AggregateType: aggregate, AggregateID: ag, AggregateVersion: &version}
	}
	proposalIDs := []string{}
	for range 2 {
		id := inboxID[mc.Meeting](t)
		sources.proposals[id.String()] = c.Source{ProjectID: project, ID: id.String(), Version: 1, Relevant: true, Open: true, Title: "Proposal", OccurredAt: at.Time()}
		ev, err := lifecycle.New(header(id.String(), mc.MeetingAggregate, mc.ProposedName), mc.LifecyclePayload{MeetingID: id, Status: "proposed"})
		if err != nil {
			t.Fatal(err)
		}
		deliver(proposalHandler, ev)
		proposalIDs = append(proposalIDs, id.String())
	}
	decisionIDs, originalEvents := []string{}, []event.Event{}
	for range 2 {
		id, meeting := inboxID[mc.Decision](t), inboxID[mc.Meeting](t)
		sources.decisions[id.String()] = c.Source{ProjectID: project, ID: id.String(), Context: c.SourceContext{MeetingID: meeting.String()}, Version: 1, Relevant: true, Open: true, Title: "Decision", OccurredAt: at.Time()}
		ev, err := decisions.New(header(id.String(), mc.DecisionAggregate, mc.DecisionRequestedName), mc.DecisionPayload{DecisionID: id, MeetingID: meeting, TurnID: inboxID[mc.Turn](t), ContributionID: inboxID[mc.Contribution](t), ParticipantID: inboxID[mc.Participant](t), ExecutionID: inboxID[i.Execution](t), Status: "pending"})
		if err != nil {
			t.Fatal(err)
		}
		deliver(decisionHandler, ev)
		decisionIDs, originalEvents = append(decisionIDs, id.String()), append(originalEvents, ev)
	}
	// Force sort boundaries independently of wall-clock timing. These are only
	// projection display fields; the source identity/version/status stay intact.
	conn := db.Connect(t)
	for n, id := range append(slices.Clone(decisionIDs), proposalIDs...) {
		priority := 0
		if n < 2 {
			priority = 2 - n
		}
		if _, err = conn.Exec(ctx, `UPDATE agenteam_humaninbox.items SET updated_at=$1,priority=$2 WHERE project_id=$3 AND source_id=$4`, at.Time().Add(time.Duration(n+1)*time.Second), priority, project.String(), id); err != nil {
			t.Fatal(err)
		}
	}
	list := func(status string) []c.Item {
		t.Helper()
		out := []c.Item{}
		var before *c.ItemID
		for range 5 {
			page, err := service.List(ctx, actor, c.ListRequest{ProjectID: project, Status: status, Limit: 1, Before: before})
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, page.Items...)
			if page.NextBefore == nil {
				return out
			}
			before = page.NextBefore
		}
		t.Fatal("cursor did not terminate")
		return nil
	}
	got := []string{}
	for _, item := range list("open") {
		got = append(got, item.SourceID)
	}
	want := []string{decisionIDs[0], decisionIDs[1], proposalIDs[1], proposalIDs[0]}
	if !slices.Equal(got, want) {
		t.Fatalf("mixed cursor skipped/reordered: got %v want %v", got, want)
	}
	partial, err := NewService(authority, sources, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	delete(sources.proposals, proposalIDs[0])
	if err = partial.Rebuild(ctx, actor, project); err != nil {
		t.Fatal(err)
	}
	open := list("open")
	if len(open) != 3 {
		t.Fatalf("proposal-only rebuild changed Decisions: %d open", len(open))
	}
	for _, id := range decisionIDs {
		if !slices.ContainsFunc(open, func(v c.Item) bool { return v.SourceType == "decision_request" && v.SourceID == id }) {
			t.Fatal("Decision resolved by another provider")
		}
	}
	resolved := sources.decisions[decisionIDs[0]]
	resolved.Version, resolved.Open = 2, false
	sources.decisions[resolved.ID] = resolved
	if _, err = conn.Exec(ctx, `DELETE FROM agenteam_humaninbox.items WHERE project_id=$1`, project.String()); err != nil {
		t.Fatal(err)
	}
	if err = service.Rebuild(ctx, actor, project); err != nil {
		t.Fatal(err)
	}
	// The old requested event must reread the now-resolved canonical provider.
	deliver(decisionHandler, originalEvents[0])
	if items := list("resolved"); len(items) != 1 || items[0].SourceID != resolved.ID || items[0].SourceVersion != 2 || items[0].Context != resolved.Context {
		t.Fatal("rebuild/late event lost canonical Decision", items)
	}
	if items := list("open"); len(items) != 2 || slices.ContainsFunc(items, func(v c.Item) bool { return v.SourceID == resolved.ID }) {
		t.Fatal("late requested reopened handled Decision", items)
	}
}
