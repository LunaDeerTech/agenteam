package service

import (
	"context"
	"errors"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

func TestRunnerAuditCannotMintProofFromPublicDTO(t *testing.T) {
	st := &readerStore{}
	human := actor(t)
	authority, _ := NewAuthority(st, &readerAuthority{store: st, want: human})
	target := sid[c.Runner](t)
	m, _ := ac.RunnerMetadata(ac.RunnerCreate, ac.RunnerMetadataFields{RunnerID: target.String(), Version: 1, CredentialGeneration: 1, ChangedFields: []string{"created"}})
	resource, _ := ac.NewResource(ac.RunnerResource, target.String())
	entry, e := ac.NewEntry(ac.EntryFields{Scope: id.SystemScope(), Actor: human, Action: ac.RunnerCreate, Outcome: ac.Success, Resource: resource, Metadata: m, Associations: ac.Associations{RunnerID: target.String()}})
	if e != nil {
		t.Fatal(e)
	}
	key, _ := ac.NewAppendKey(ac.RunnerProducer, sid[c.Command](t).String(), 0)
	tx := f.NewTx()
	contexts := []context.Context{context.Background(), context.WithValue(context.Background(), auditProofKey{}, &auditProof{authority: authority, tx: f.NewTx(), entry: entry, key: key, record: &commandRecord{}}), context.WithValue(context.Background(), auditProofKey{}, &auditProof{authority: &Authority{}, tx: tx, entry: entry, key: key, record: &commandRecord{}})}
	for _, ctx := range contexts {
		e = authority.CheckAppendInTx(ctx, tx, entry, key)
		var ff *f.Fault
		if !errors.As(e, &ff) || ff.Code != f.Forbidden {
			t.Fatal("public/foreign proof admitted", e)
		}
	}
	if st.executorCalls != 0 {
		t.Fatal("untrusted input reached SQL")
	}
}
