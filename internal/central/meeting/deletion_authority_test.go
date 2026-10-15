package meeting

import (
	"context"
	"strings"
	"testing"

	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type deletionRejectStore struct {
	Store
	borrowed int
}

func (s *deletionRejectStore) InTx(f.Tx) (postgres.SQLExecutor, error) {
	s.borrowed++
	return nil, fault(f.Forbidden)
}

func deletionTestRecord(t *testing.T) deletionRecord {
	t.Helper()
	return deletionRecord{id: summaryTestID[c.Deletion](t), project: summaryTestID[i.Project](t), meeting: summaryTestID[c.Meeting](t), actor: summaryTestID[i.User](t), request: summaryTestID[f.Request](t), key: "meeting-delete-original", digest: f.Digest("sha256:" + strings.Repeat("a", 64)), expected: 3, accepted: 4, phase: c.DeletionCancelling}
}

func TestMeetingDeletionRequiresPrivateLiveRetirementTransaction(t *testing.T) {
	store := &deletionRejectStore{}
	a, err := NewDeletionAuthority(&Authority{store: store}, &object.ProcessGuard{}, summaryTestID[oc.Process](t))
	if err != nil {
		t.Fatal(err)
	}
	tx := f.NewTx()
	if err := a.RequireBindingInTx(context.Background(), tx, store); err == nil {
		t.Fatal("public transaction and authority granted deletion")
	}
	for _, change := range []string{"ended", "foreign-tx", "foreign-owner", "foreign-store"} {
		t.Run(change, func(t *testing.T) {
			w := &deletionWitness{authority: a, tx: tx, original: deletionTestRecord(t), active: true}
			var data any = store
			switch change {
			case "ended":
				w.active = false
			case "foreign-tx":
				w.tx = f.NewTx()
			case "foreign-owner":
				copy := *a
				w.authority = &copy
			case "foreign-store":
				data = &deletionRejectStore{}
			}
			if err := a.RequireBindingInTx(context.WithValue(context.Background(), deletionWitnessKey{}, w), tx, data); err == nil {
				t.Fatal("foreign or expired deletion witness accepted")
			}
		})
	}
	if store.borrowed != 0 {
		t.Fatal("untrusted deletion request reached SQL")
	}
}

func TestMeetingDeletionAuditRejectsPublicAndForeignEvidence(t *testing.T) {
	store := &deletionRejectStore{}
	facts, err := NewDeletionAuditFacts(store)
	if err != nil {
		t.Fatal(err)
	}
	original := deletionTestRecord(t)
	original.phase = c.DeletionCompleted
	registration, _ := i.RegisterService(i.MeetingRuntime)
	scope, _ := i.InProject(original.project)
	actor, err := registration.Actor(original.id.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	entry, key, err := deletionAuditEntry(actor, original, 7)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := entry.Fields().Metadata.MeetingDeletionFields()
	if err != nil || meta.Version != original.accepted || meta.DeletedRows != 7 || meta.InitiatorID != original.actor.String() || entry.Fields().Associations != (au.Associations{OperationID: original.id.String()}) {
		t.Fatal("audit lost accepted version, initiator or exact operation", err)
	}
	authority := &DeletionAuthority{original: &Authority{store: store}, guard: &object.ProcessGuard{}, process: summaryTestID[oc.Process](t)}
	tx := f.NewTx()
	if err := facts.CheckProjectAuditInTx(context.Background(), tx, entry, key); err == nil {
		t.Fatal("technical Actor plus metadata granted audit append")
	}
	for _, change := range []string{"ended", "foreign-tx", "foreign-store", "foreign-entry", "foreign-key"} {
		t.Run(change, func(t *testing.T) {
			w := &deletionAuditWitness{authority: authority, tx: tx, original: original, entry: entry, key: key, rows: 7, purged: true, active: true}
			candidate, appendKey := entry, key
			switch change {
			case "ended":
				w.active = false
			case "foreign-tx":
				w.tx = f.NewTx()
			case "foreign-store":
				copy := *authority
				copy.original = &Authority{store: &deletionRejectStore{}}
				w.authority = &copy
			case "foreign-entry":
				candidate, _, err = deletionAuditEntry(actor, original, 8)
			case "foreign-key":
				appendKey, err = au.NewAppendKey(au.MeetingProducer, summaryTestID[c.Deletion](t).String(), 1)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := facts.CheckProjectAuditInTx(context.WithValue(context.Background(), deletionAuditKey{}, w), tx, candidate, appendKey); err == nil {
				t.Fatal("foreign audit evidence accepted")
			}
		})
	}
	if store.borrowed != 0 {
		t.Fatal("untrusted audit request reached SQL")
	}
}

func TestMeetingDeletionLockUnionIncludesHistoricalAttempts(t *testing.T) {
	original := deletionTestRecord(t)
	contribution := contributionRecord{id: summaryTestID[c.Contribution](t), participant: summaryTestID[c.Participant](t), agent: summaryTestID[i.Agent](t), generation: 1, attempt: 1}
	turn := summaryTestID[c.Turn](t)
	intents := []intentRecord{}
	for _, attempt := range []f.Version{1, 2} {
		contribution.attempt = attempt
		request := launchRequest(original.project, original.meeting, turn, contribution)
		request.Meta.RequestID = summaryTestID[f.Request](t)
		digest, err := request.Digest()
		if err != nil {
			t.Fatal(err)
		}
		execution := summaryTestID[i.Execution](t)
		intents = append(intents, intentRecord{id: summaryTestID[c.Intent](t), request: request, requestDigest: digest, execution: &execution})
	}
	locks, err := deletionRetirementLocks(deletionPurge{original: original, intents: intents})
	if err != nil {
		t.Fatal(err)
	}
	for _, intent := range intents {
		key, _ := f.AggregateLock(f.ExecutionAggregate, intent.execution.String())
		found := false
		for _, lock := range locks {
			if lock.Key.Canonical() == key.Canonical() && lock.Mode == f.Exclusive {
				found = true
			}
		}
		if !found {
			t.Fatal("old attempt omitted from deletion retirement union")
		}
	}
	if sameDeletionIntents(intents, intents[1:]) {
		t.Fatal("current pointer substituted for all historical attempts")
	}
}
