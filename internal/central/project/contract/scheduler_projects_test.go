package contract

import (
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestSchedulerProjectPagesBoundOneDiscoveryCycle(t *testing.T) {
	one, two, three := testID[identity.Project](1), testID[identity.Project](2), testID[identity.Project](3)
	first := SchedulerProjectPageRequest{Limit: 1}
	page := SchedulerProjectPage{ProjectIDs: []ProjectID{one}, Through: &three}
	if err := page.ValidateFor(first); err != nil {
		t.Fatal(err)
	}
	next := SchedulerProjectPageRequest{After: &one, Through: &three, Limit: 1}
	last := SchedulerProjectPage{ProjectIDs: []ProjectID{three}, Through: &three, Complete: true}
	if err := last.ValidateFor(next); err != nil {
		t.Fatal(err)
	}
	empty := SchedulerProjectPage{ProjectIDs: []ProjectID{}, Complete: true}
	if err := empty.ValidateFor(first); err != nil || empty.Clone().ProjectIDs == nil {
		t.Fatalf("empty first page: %v", err)
	}
	empty.Through = &three
	if err := empty.ValidateFor(next); err != nil {
		t.Fatalf("membership may disappear after the highwater is fixed: %v", err)
	}
	for _, r := range []SchedulerProjectPageRequest{
		{Limit: 0}, {Limit: MaxSchedulerProjectPageSize + 1},
		{After: &one, Limit: 1}, {After: &three, Through: &three, Limit: 1},
		{After: &three, Through: &one, Limit: 1},
	} {
		requireCode(t, r.Validate(), foundation.InvalidArgument)
	}
	for _, p := range []SchedulerProjectPage{
		{Complete: true},
		{ProjectIDs: []ProjectID{}, Through: &three},
		{ProjectIDs: []ProjectID{one}, Complete: true},
		{ProjectIDs: []ProjectID{three}, Through: &three},
		{ProjectIDs: []ProjectID{one, one}, Through: &three, Complete: true},
		{ProjectIDs: []ProjectID{two, one}, Through: &three, Complete: true},
		{ProjectIDs: []ProjectID{three}, Through: &two, Complete: true},
	} {
		requireCode(t, p.Validate(), foundation.InvalidArgument)
	}
	for _, p := range []SchedulerProjectPage{
		{ProjectIDs: []ProjectID{one}, Through: &three, Complete: true},
		{ProjectIDs: []ProjectID{two}, Through: &two, Complete: true},
		{ProjectIDs: []ProjectID{two, three}, Through: &three, Complete: true},
	} {
		requireCode(t, p.ValidateFor(next), foundation.InvalidArgument)
	}
	wide := next.Clone()
	wide.Limit = 2
	requireCode(t, (SchedulerProjectPage{ProjectIDs: []ProjectID{two}, Through: &three}).ValidateFor(wide), foundation.InvalidArgument)
	copyRequest, copyPage := next.Clone(), page.Clone()
	*copyRequest.After, *copyRequest.Through = two, two
	copyPage.ProjectIDs[0], *copyPage.Through = two, two
	if *next.After != one || *next.Through != three || page.ProjectIDs[0] != one || *page.Through != three {
		t.Fatal("discovery position or page aliases caller data")
	}
}
