package audit

import (
	"context"
	"errors"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestMeetingAuditRequiresOriginalProjectFactChecker(t *testing.T) {
	p, _ := f.NewID[i.Project]()
	scope, _ := i.InProject(p)
	id, _ := f.NewID[struct{}]()
	initiator, _ := f.NewID[i.User]()
	reg, _ := i.RegisterService(i.MeetingRuntime)
	actor, _ := reg.Actor(id.String(), scope)
	resource, _ := ac.NewResource(ac.MeetingResource, id.String())
	metadata, err := ac.MeetingDeletionMetadata(ac.MeetingDeleteCompleted, ac.MeetingDeletionMetadataFields{MeetingID: id.String(), DeletionID: id.String(), InitiatorID: initiator.String(), Version: 2, DeletedRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := ac.NewEntry(ac.EntryFields{Actor: actor, Scope: scope, Resource: resource, Action: ac.MeetingDeleteCompleted, Outcome: ac.Success, Metadata: metadata, Associations: ac.Associations{OperationID: id.String()}})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ac.NewAppendKey(ac.MeetingProducer, id.String(), 1)
	ctx := context.Background()
	tx := f.NewTx()
	calls := 0
	denied := f.NewFault(f.Forbidden, f.NotStarted)
	s := &Service{auth: Authorizations{Projects: oauthProjectAuditProbe{check: func(got context.Context, actual f.Tx, e ac.Entry, k ac.AppendKey) error {
		calls++
		if got != ctx || actual != tx || !e.Fields().Actor.Equal(actor) || k.Details() != key.Details() {
			t.Fatal("original loan changed")
		}
		return denied
	}}}}
	if serviceOwns(actor, scope, key) {
		t.Fatal("generic service ownership widened")
	}
	if err = s.authorizeAppend(ctx, tx, entry, key); !errors.Is(err, denied) || calls != 1 {
		t.Fatal("domain denial lost", err)
	}
	s.auth.Projects = nil
	if err = s.authorizeAppend(ctx, tx, entry, key); err == nil {
		t.Fatal("public service alone granted append")
	}
}
