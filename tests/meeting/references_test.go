//go:build integration

package meeting_test

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

type pinnedReferenceAuthority struct {
	fixture *fixture
	allowed string
	allow   bool
	checks  int
}

func (a *pinnedReferenceAuthority) ReferenceLocks(ctx context.Context, actor i.Actor, project i.ProjectID, refs []c.Reference) ([]f.LockRequest, error) {
	if !actor.Equal(a.fixture.authority.actor) || project != a.fixture.authority.project.ID || len(refs) != 1 || refs[0].Kind != "task" || refs[0].ID != a.allowed {
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	key, err := f.ProjectScheduleLock(project.String())
	if err != nil {
		return nil, err
	}
	return []f.LockRequest{{Key: key, Mode: f.Shared}}, ctx.Err()
}

func (a *pinnedReferenceAuthority) RequireReferencesInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, refs []c.Reference) error {
	locks, err := a.ReferenceLocks(ctx, actor, project, refs)
	if err != nil {
		return err
	}
	if err = a.fixture.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return err
	}
	a.checks++
	if !a.allow {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}

func TestMeetingPinnedReferencesCanonicalReplayAndCurrentResourceGate(t *testing.T) {
	v := setup(t)
	_, start := v.start(t, "sequential")
	ctx, a := testContext(t), v.authority
	request := c.UpdateReferencesRequest{ProjectID: a.project.ID, MeetingID: start.MeetingID, ExpectedVersion: start.Version, References: []c.Reference{{Kind: "link", ID: "HTTPS://Example.COM:443", Label: "Design"}}, Meta: meta(t)}
	pinned, err := v.service.UpdateReferences(ctx, a.actor, request)
	if err != nil {
		t.Fatal("link identity without resource provider", err)
	}
	request.References[0].ID = "https://example.com/"
	if replay, found, err := v.service.LookupUpdateReferences(ctx, a.actor, request); err != nil || !found || replay.Version != pinned.Version {
		t.Fatal("canonical replay", err)
	}
	resource := c.Reference{Kind: "task", ID: ident[struct{}](t).String(), Label: "Task"}
	request.ExpectedVersion, request.Meta = pinned.Version, meta(t)
	request.References = append(request.References, resource)
	_, err = v.service.UpdateReferences(ctx, a.actor, request)
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.DependencyUnbound {
		t.Fatal("unbound provider accepted resource", err)
	}
	refs := &pinnedReferenceAuthority{fixture: v, allowed: resource.ID}
	service, err := meeting.NewService(v.store, meeting.Dependencies{Projects: a, Agents: a, Directory: a, References: refs})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.UpdateReferences(ctx, a.actor, request)
	if !errors.As(err, &fault) || fault.Code != f.Forbidden {
		t.Fatal("resource revoked at mutation accepted", err)
	}
	detail, err := service.GetDetail(ctx, a.actor, a.project.ID, start.MeetingID)
	if err != nil || detail.Session.Version != pinned.Version || len(detail.References) != 1 || detail.References[0].ID != "https://example.com/" {
		t.Fatal("failed mutation was not atomic", err)
	}
	if _, found, err := service.LookupUpdateReferences(ctx, a.actor, request); err != nil || found {
		t.Fatal("rejected write has receipt", err)
	}
	refs.allow = true
	added, err := service.UpdateReferences(ctx, a.actor, request)
	if err != nil {
		t.Fatal("authorized resource", err)
	}
	checks := refs.checks
	refs.allow = false
	request.ExpectedVersion, request.Meta = added.Version, meta(t)
	request.References = []c.Reference{resource, {Kind: "link", ID: "https://example.com/", Label: "Design"}}
	reordered, err := service.UpdateReferences(ctx, a.actor, request)
	if err != nil || refs.checks != checks {
		t.Fatal("existing unavailable identity cannot reorder", err)
	}
	request.ExpectedVersion, request.Meta = reordered.Version, meta(t)
	request.References = []c.Reference{}
	removed, err := service.UpdateReferences(ctx, a.actor, request)
	if err != nil {
		t.Fatal("unpin unavailable resource", err)
	}
	detail, err = service.GetDetail(ctx, a.actor, a.project.ID, start.MeetingID)
	if err != nil || detail.Session.Version != removed.Version || len(detail.References) != 0 || len(detail.Turns) != 1 || len(detail.Turns[0].Contributions) != 3 {
		t.Fatal("unpin postimage", err)
	}
	request.ExpectedVersion, request.Meta = removed.Version, meta(t)
	request.References = []c.Reference{resource}
	if _, err = service.UpdateReferences(ctx, a.actor, request); err == nil {
		t.Fatal("re-pin bypassed current authorization")
	}
}
