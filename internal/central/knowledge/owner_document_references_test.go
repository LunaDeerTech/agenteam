package knowledge

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

func TestOwnerDocumentReferencesOriginalBinding(t *testing.T) {
	v := newKnowledgeProjectPureFixture(t)
	port, err := NewOwnerDocumentReferences(v.pair.Service())
	if err != nil || port.RequireStore(v.store) != nil || port.RequireStore(&knowledgeProjectPureStore{}) == nil {
		t.Fatal("original bound store required", err)
	}
	if _, err = NewOwnerDocumentReferences(nil); err == nil {
		t.Fatal("unbound service accepted")
	}
	unbound, _, _, _, _ := contentReadFixture(t)
	if _, err = NewOwnerDocumentReferences(unbound); err == nil {
		t.Fatal("service without original Project ownership accepted")
	}
	locks, err := port.DocumentReferenceLocks(v.actor, v.a, v.document)
	want, wantErr := scopeLocks(v.actor, v.a, false)
	if err != nil || wantErr != nil || len(locks) != 3 || !contentReadSameLocks(locks, want) {
		t.Fatal("user/project/canonical tree lock union missing", err)
	}
	if err = port.RequireDocumentReferenceInTx(context.Background(), f.NewTx(), v.actor, v.a, v.document); err == nil {
		t.Fatal("fabricated transaction accepted")
	}
	if transactions, queries, writes := v.store.counts(); transactions != 0 || queries != 0 || writes != 0 {
		t.Fatal("identity port opened an independent transaction or read")
	}
	serviceActor, _, _ := v.stopIdentity(t, v.a)
	for _, actor := range []id.Actor{{}, serviceActor} {
		if _, err = port.DocumentReferenceLocks(actor, v.a, v.document); err == nil {
			t.Fatal("non-Human actor admitted")
		}
	}
	if _, err = port.DocumentReferenceLocks(v.actor, v.a, kc.DocumentID{}); err == nil {
		t.Fatal("invalid document accepted")
	}
}

// This controls the borrowed transaction and current authority. It does not
// claim native PostgreSQL eligibility or a complete Meeting reference command.
func TestOwnerDocumentReferencesBorrowCallerTransaction(t *testing.T) {
	for _, mode := range []string{"active", "absent", "revoked", "wrong-project-grant", "missing-lock", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			v := newKnowledgeProjectPureFixture(t)
			port, err := NewOwnerDocumentReferences(v.pair.Service())
			if err != nil {
				t.Fatal(err)
			}
			v.store.documents[v.a.String()+"/"+v.document.String()] = publicationCheckpointRow(func(dest ...any) error {
				if len(dest) != 1 {
					t.Fatal("identity check requested content fields")
				}
				value, ok := dest[0].(*bool)
				if !ok {
					t.Fatal("identity check requested document content")
				}
				*value = mode != "absent"
				return nil
			})
			wantQueries := 0
			switch mode {
			case "active", "absent":
				wantQueries = 1
			case "revoked":
				delete(v.gate.projects, v.a)
			case "wrong-project-grant":
				v.gate.projects[v.a] = v.gate.projects[v.b]
			}
			locks, err := port.DocumentReferenceLocks(v.actor, v.a, v.document)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "missing-lock" {
				locks = locks[:len(locks)-1]
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause, err := readCause()
			if err != nil {
				t.Fatal(err)
			}
			var originalTx f.Tx
			var observed error
			result := v.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
				originalTx = tx
				if err := v.store.AcquireAll(ctx, tx, locks); err != nil {
					return err
				}
				if mode == "cancelled" {
					cancel()
				}
				observed = port.RequireDocumentReferenceInTx(ctx, tx, v.actor, v.a, v.document)
				return observed
			})
			if (mode == "active") != (observed == nil) || (mode == "active") != (result.State() == f.Committed) {
				t.Fatal("wrong identity/authority outcome", observed)
			}
			if transactions, queries, writes := v.store.counts(); transactions != 1 || queries != wantQueries || writes != 0 || len(v.store.ledger) != 0 {
				t.Fatal("borrowed check opened a session, wrote a ledger, or read before authorization")
			}
			if err := port.RequireDocumentReferenceInTx(context.Background(), originalTx, v.actor, v.a, v.document); err == nil {
				t.Fatal("ended original transaction accepted")
			}
		})
	}
}
