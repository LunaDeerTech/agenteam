package audit

import (
	"context"
	"errors"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type oauthProjectAuditProbe struct {
	ac.ProjectAuthority
	check func(context.Context, f.Tx, ac.Entry, ac.AppendKey) error
}

func (p oauthProjectAuditProbe) CheckAppendInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	return p.check(ctx, tx, entry, key)
}

func TestOAuthSecretAuditRequiresProjectFactProof(t *testing.T) {
	project, _ := f.ParseID[id.Project]("01900000-0000-7000-8000-000000000001")
	scope, _ := id.InProject(project)
	registration, _ := id.RegisterService(id.SecretService)
	actor, _ := registration.Actor("01900000-0000-7000-8000-000000000002", scope)
	resource, _ := ac.NewResource(ac.SecretResource, "01900000-0000-7000-8000-000000000003")
	key, _ := ac.NewAppendKey(ac.SecretProducer, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 0)
	for _, action := range []ac.Action{ac.SecretCreate, ac.SecretUpdate, ac.SecretDelete} {
		t.Run(string(action), func(t *testing.T) {
			changed := []ac.ChangedField{ac.ValueChanged}
			if action == ac.SecretDelete {
				changed = nil
			}
			metadata, err := ac.SecretMutationMetadata(action, 1, changed)
			if err != nil {
				t.Fatal(err)
			}
			entry, err := ac.NewEntry(ac.EntryFields{Actor: actor, Scope: scope, Resource: resource, Action: action, Outcome: ac.Success, Metadata: metadata})
			if err != nil {
				t.Fatal(err)
			}
			if serviceOwns(actor, scope, key) {
				t.Fatal("generic service ownership was widened")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tx := f.NewTx()
			calls := 0
			denied := f.NewFault(f.Forbidden, f.NotStarted)
			service := &Service{auth: Authorizations{Projects: oauthProjectAuditProbe{check: func(got context.Context, actual f.Tx, e ac.Entry, k ac.AppendKey) error {
				calls++
				if got != ctx || actual != tx || !e.Fields().Actor.Equal(actor) || e.Fields().Action != action || k.Details() != key.Details() {
					t.Fatal("original write context or identities changed")
				}
				return denied
			}}}}
			var problem *f.Fault
			if err = service.authorizeAppend(ctx, tx, entry, key); !errors.As(err, &problem) || problem.Code != f.Forbidden || calls != 1 {
				t.Fatalf("Project rejection not required: calls=%d err=%v", calls, err)
			}
			service.auth.Projects = nil
			if err = service.authorizeAppend(ctx, tx, entry, key); !errors.As(err, &problem) || problem.Code != f.DependencyUnbound {
				t.Fatalf("missing original proof port allowed: %v", err)
			}
			fields := entry.Fields()
			fields.Actor, _ = registration.Actor(actor.Details().CauseRef, id.SystemScope())
			fields.Scope = id.SystemScope()
			system, err := ac.NewEntry(fields)
			if err != nil {
				t.Fatal(err)
			}
			if err = service.authorizeAppend(ctx, tx, system, key); !errors.As(err, &problem) || problem.Code != f.Forbidden {
				t.Fatalf("Project exception escaped to System: %v", err)
			}
		})
	}
}
