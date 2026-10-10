package skill

import (
	"context"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type auditFactControl func(context.Context, f.Tx, ac.Entry, ac.AppendKey) error

func (fn auditFactControl) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) error {
	return fn(ctx, tx, e, k)
}

type skillAuditStore struct {
	*skillAuthorityStore
	attempts map[string]bool
}

func (s *skillAuditStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, e := s.skillAuthorityStore.InTx(tx); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *skillAuditStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_skill.object_attempts") {
		want := []any{s.row.values[0], s.row.values[1], s.row.values[3], s.row.values[4], s.row.values[16], s.row.values[17]}
		ok := s.attempts[args[0].(string)]
		for n, v := range want {
			ok = ok && args[n+1] == v
		}
		return skillRowValues{values: []any{ok}}
	}
	return s.skillAuthorityStore.QueryRow(ctx, query, args...)
}
func auditEntry(t *testing.T, row initializationRow, action ac.Action, change func(*ac.EntryFields, *ac.ObjectMetadataFields, *ac.AppendKeyDetails)) (ac.Entry, ac.AppendKey) {
	t.Helper()
	scope, _ := id.InProject(row.request.ProjectID)
	resource, _ := ac.NewResource(ac.ObjectResource, row.object.String())
	k := ac.AppendKeyDetails{Producer: ac.ObjectProducer, CauseRef: row.upload.String()}
	m := ac.ObjectMetadataFields{ObjectID: row.object.String(), InitiatorKind: id.Service, InitiatorID: row.request.CreationID.String(), MediaType: sc.PackageMediaType, ByteSize: row.bundle.size, Phase: ac.PublishedPhase}
	e := ac.EntryFields{Scope: scope, Action: action, Outcome: ac.Success, Resource: resource}
	if action == ac.ObjectUploadFailed {
		k.CauseRef = row.attempt.String()
		e.Outcome = ac.Unknown
		m.Phase = ac.FailedPhase
		m.Reason = ac.IntegrityMismatch
	}
	if action == ac.ObjectDelete {
		k.CauseRef = string(sum([]byte("exact object cleanup cause")))
		k.Ordinal = 1
		m.Phase = ac.DeletedPhase
	}
	if change != nil {
		change(&e, &m, &k)
	}
	role, _ := id.RegisterService(id.ObjectService)
	e.Actor, _ = role.Actor(k.CauseRef, e.Scope)
	var err error
	e.Metadata, err = ac.ObjectMetadata(action, m)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := ac.NewEntry(e)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ac.NewAppendKey(k.Producer, k.CauseRef, k.Ordinal)
	if err != nil {
		t.Fatal(err)
	}
	return entry, key
}
func TestSkillInitializationAuditExactMappingAndOriginalWitnessContext(t *testing.T) {
	for _, name := range []string{"complete", "failed_current", "failed_old", "delete", "foreign_attempt", "wrong_initiator", "wrong_bytes", "wrong_ordinal", "missing_skill", "missing_lock", "current_gate", "delegate_unknown", "real_checker_without_witness"} {
		t.Run(name, func(t *testing.T) {
			a, base, projects, _, row := authorityFixture(t)
			row.phase = initializationReserved
			row.object = stateID[oc.StoredObject](7)
			row.upload = stateID[oc.Upload](8)
			row.attempt = stateID[oc.Attempt](6)
			base.row.values[14] = "reserved"
			base.row.values[16] = row.object.String()
			base.row.values[17] = row.upload.String()
			base.row.values[18] = row.attempt.String()
			base.exists = true
			old := stateID[oc.Attempt](106)
			store := &skillAuditStore{skillAuthorityStore: base, attempts: map[string]bool{row.attempt.String(): true, old.String(): true}}
			a.state().store = store
			locks, _ := row.locks(f.Exclusive, row.object)
			tx := armAuthorityTx(t, base, locks)
			ctx := context.WithValue(context.Background(), struct{}{}, "private-witness-canary")
			action := ac.ObjectUploadComplete
			if strings.HasPrefix(name, "failed") || name == "foreign_attempt" {
				action = ac.ObjectUploadFailed
			}
			if name == "delete" {
				action = ac.ObjectDelete
			}
			entry, key := auditEntry(t, row, action, func(e *ac.EntryFields, m *ac.ObjectMetadataFields, k *ac.AppendKeyDetails) {
				switch name {
				case "failed_old":
					k.CauseRef = old.String()
					k.Ordinal = 1
				case "foreign_attempt":
					k.CauseRef = stateID[oc.Attempt](107).String()
				case "wrong_initiator":
					m.InitiatorID = stateID[struct{}](108).String()
				case "wrong_bytes":
					m.ByteSize++
				case "wrong_ordinal":
					k.Ordinal = 2
				}
			})
			calls := 0
			sentinel := f.NewFault(f.DependencyUnavailable, f.Unknown)
			var checker ac.ProjectFactAuthority = auditFactControl(func(gotctx context.Context, gottx f.Tx, got ac.Entry, gotkey ac.AppendKey) error {
				calls++
				if gotctx != ctx || gottx != tx || !got.Fields().Actor.Equal(entry.Fields().Actor) || string(got.Fields().Metadata.JSON()) != string(entry.Fields().Metadata.JSON()) || gotkey.Details() != key.Details() {
					t.Fatal("private witness input replaced")
				}
				if name == "delegate_unknown" {
					return sentinel
				}
				return nil
			})
			if name == "real_checker_without_witness" {
				var err error
				checker, err = object.NewProjectAuditAuthority(store)
				if err != nil {
					t.Fatal(err)
				}
			}
			facts, err := NewInitializationAuditFacts(a, checker)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "missing_skill":
				base.exists = false
			case "missing_lock":
				delete(base.held, skillLock(row.skill, f.Exclusive).Key.Canonical())
			case "current_gate":
				projects.gate = sentinel
			}
			err = facts.CheckProjectAuditInTx(ctx, tx, entry, key)
			switch name {
			case "complete", "failed_current", "failed_old", "delete":
				if err != nil || calls != 1 {
					t.Fatal("valid exact mapping rejected", err)
				}
			case "delegate_unknown":
				if err != sentinel || calls != 1 {
					t.Fatal("delegate Unknown replaced")
				}
			default:
				if err == nil || calls != 0 {
					t.Fatal("unproven mapping/witness accepted")
				}
				if name == "current_gate" && err != sentinel {
					t.Fatal("current gate error lost")
				}
			}
		})
	}
}
