package project

import (
	"context"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestMeetingAuditChecksOwnerThenExactDeletionFacts(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	id := testID[struct{}](t).String()
	scope, _ := i.InProject(x.project)
	resource, _ := ac.NewResource(ac.MeetingResource, id)
	calls := 0
	denied := fault(f.Forbidden)
	x.a.state().auditFacts[ac.MeetingProducer] = auditFactFunc(func(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
		calls++
		if tx != x.store.tx || key.Details().CauseRef != id {
			t.Fatal("private transaction changed")
		}
		return denied
	})
	for _, action := range []ac.Action{ac.MeetingDeleteAccepted, ac.MeetingDeleteCompleted} {
		actor := x.actor
		ordinal := int64(0)
		count := int64(0)
		if action == ac.MeetingDeleteCompleted {
			reg, _ := i.RegisterService(i.MeetingRuntime)
			actor, _ = reg.Actor(id, scope)
			ordinal = 1
			count = 2
		}
		metadata, err := ac.MeetingDeletionMetadata(action, ac.MeetingDeletionMetadataFields{MeetingID: id, DeletionID: id, InitiatorID: x.actor.Details().UserID, Version: 2, DeletedRows: count})
		if err != nil {
			t.Fatal(err)
		}
		entry, err := ac.NewEntry(ac.EntryFields{Actor: actor, Scope: scope, Action: action, Outcome: ac.Success, Resource: resource, Metadata: metadata, Associations: ac.Associations{OperationID: id}})
		if err != nil {
			t.Fatal(err)
		}
		key, _ := ac.NewAppendKey(ac.MeetingProducer, id, ordinal)
		before := calls
		hasCode(t, x.a.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.Forbidden)
		if calls != before+1 {
			t.Fatal("domain checker missing")
		}
		wrong, _ := ac.NewAppendKey(ac.MeetingProducer, id, 1-ordinal)
		hasCode(t, x.a.CheckAppendInTx(context.Background(), x.store.tx, entry, wrong), f.Forbidden)
		if calls != before+1 {
			t.Fatal("wrong accepted/completed cause reached proof")
		}
		if action == ac.MeetingDeleteAccepted {
			x.sessionErr = fault(f.SessionRevoked)
			hasCode(t, x.a.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.SessionRevoked)
			x.sessionErr = nil
			if calls != before+1 {
				t.Fatal("revoked Owner reached private source")
			}
		}
	}
}
