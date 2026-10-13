package app

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	mtc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

func lifecycleAssemblyFault(t *testing.T, err error, code f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}

func TestDefaultProjectLifecycleManifest(t *testing.T) {
	manifest, err := defaultProjectLifecycleManifest()
	if err != nil {
		t.Fatal(err)
	}
	entries := manifest.Entries()
	wantNames := []pc.ParticipantName{pc.SkillsParticipant, pc.ArtifactObjectParticipant, pc.AuditParticipant, pc.ExecutionParticipant, pc.KnowledgeParticipant, pc.ModelParticipant, pc.OutboxParticipant, pc.RunnerManagedParticipant, pc.SchedulerParticipant, pc.SecretParticipant, pc.UsageParticipant, pc.WorkParticipant}
	var names []pc.ParticipantName
	byName := map[pc.ParticipantName]pc.ParticipantRegistration{}
	for _, entry := range entries {
		names = append(names, entry.Name)
		byName[entry.Name] = entry
		if entry.ContractVersion != 1 {
			t.Fatal("unexpected first declaration version", entry.Name)
		}
	}
	if !slices.Equal(names, wantNames) {
		t.Fatal("default responsibility set omitted or invented an owner", names)
	}
	wantAfter := map[pc.ParticipantName][]pc.ParticipantName{
		pc.ArtifactObjectParticipant: {pc.SkillsParticipant, pc.KnowledgeParticipant},
		pc.SecretParticipant:         {pc.SkillsParticipant, pc.ModelParticipant},
		pc.SkillsParticipant:         {pc.ExecutionParticipant, pc.RunnerManagedParticipant},
		pc.WorkParticipant:           {pc.ExecutionParticipant, pc.SchedulerParticipant},
		pc.ExecutionParticipant:      {pc.SchedulerParticipant},
		pc.RunnerManagedParticipant:  {pc.ExecutionParticipant},
		pc.UsageParticipant:          {pc.ExecutionParticipant},
		pc.ModelParticipant:          {pc.SkillsParticipant, pc.ExecutionParticipant, pc.UsageParticipant},
		pc.KnowledgeParticipant:      {}, pc.SchedulerParticipant: {},
		pc.AuditParticipant: {pc.OutboxParticipant},
	}
	for name, want := range wantAfter {
		if !slices.Equal(byName[name].CleanupAfter, want) {
			t.Fatal("owner dependency order changed", name, byName[name].CleanupAfter)
		}
	}
	var producers []pc.ParticipantName
	for _, name := range wantNames {
		if name != pc.OutboxParticipant && name != pc.AuditParticipant {
			producers = append(producers, name)
		}
	}
	if !slices.Equal(byName[pc.OutboxParticipant].CleanupAfter, producers) {
		t.Fatal("Outbox must directly follow every declared producer")
	}
	// Declarations alone must not open a partial destructive registry.
	if registry, err := project.NewLifecycleRegistry(manifest, nil); registry != nil {
		t.Fatal("metadata bound participants")
	} else {
		lifecycleAssemblyFault(t, err, f.DependencyUnbound)
	}
	digest, _ := manifest.Digest()
	entries[0].ReferenceKinds[0] = "caller-changed"
	entries[0].CleanupAfter[0] = pc.AuditParticipant
	other, err := defaultProjectLifecycleManifest()
	if err != nil {
		t.Fatal(err)
	}
	otherDigest, _ := other.Digest()
	if otherDigest != digest {
		t.Fatal("a caller changed later default declarations")
	}
}

func TestDefaultProjectLifecycleOwnerMetadata(t *testing.T) {
	manifest, err := defaultProjectLifecycleManifest()
	if err != nil {
		t.Fatal(err)
	}
	entries := map[pc.ParticipantName]pc.ParticipantRegistration{}
	for _, entry := range manifest.Entries() {
		entries[entry.Name] = entry
	}
	for _, tc := range []struct {
		name         pc.ParticipantName
		owner        string
		refs         []pc.ReferenceKind
		registration func() pc.ParticipantRegistration
	}{
		{pc.KnowledgeParticipant, "knowledge", []pc.ReferenceKind{"document", "knowledge-command"}, kc.ProjectLifecycleRegistration},
		{pc.UsageParticipant, "usage", []pc.ReferenceKind{"invocation"}, uc.ProjectLifecycleRegistration},
		{pc.ModelParticipant, "model", []pc.ReferenceKind{"model", "model-call", "provider", "snapshot"}, mc.ProjectLifecycleRegistration},
		{pc.RunnerManagedParticipant, "mount", []pc.ReferenceKind{"mount", "workspace"}, mtc.ProjectLifecycleRegistration},
	} {
		t.Run(string(tc.name), func(t *testing.T) {
			original, returned := tc.registration(), tc.registration()
			if original.Name != tc.name || string(original.OwnerModule) != tc.owner || original.ContractVersion != 1 || !slices.Equal(original.ReferenceKinds, tc.refs) {
				t.Fatal("owner identity declaration changed", original)
			}
			got := entries[tc.name]
			if got.Name != original.Name || got.ContractVersion != original.ContractVersion || got.OwnerModule != original.OwnerModule || !slices.Equal(got.ReferenceKinds, original.ReferenceKinds) || !slices.Equal(got.CleanupAfter, original.CleanupAfter) {
				t.Fatal("App rewrote the owner's versioned declaration")
			}
			returned.ReferenceKinds[0] = "caller-changed"
			if len(returned.CleanupAfter) > 0 {
				returned.CleanupAfter[0] = pc.AuditParticipant
			}
			if !reflect.DeepEqual(original, tc.registration()) {
				t.Fatal("owner metadata aliases a previous caller")
			}
		})
	}
}

func TestDefaultProjectLifecycleOriginalConstruction(t *testing.T) {
	cfg := testConfig(t, "1s")
	store := &projectUsageRootStore{} // All constructors must remain SQL-free.
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := createProjectLifecycleAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	base, err := createProjectUsageAuthorityWithLifecycle(store, accounts, lifecycle)
	if err != nil || base.lifecycle != lifecycle || base.projects == nil || base.reader != nil {
		t.Fatal("default first Project authority not bound", err)
	}
	bound, err := bindProjectUsage(cfg, store, accounts, base, nil)
	if err != nil || bound == base || bound.projects != base.projects || bound.lifecycle != lifecycle || bound.variables != base.variables || bound.objectFacts != base.objectFacts || bound.reader == nil || base.reader != nil {
		t.Fatal("Usage binding replaced an original authority", err)
	}
	legacy, err := createProjectUsage(cfg, store, accounts)
	if err != nil || legacy.lifecycle != nil || legacy.projects == nil || legacy.reader == nil {
		t.Fatal("legacy constructor changed", err)
	}
	for _, db := range []database{nil, (*projectUsageRootStore)(nil), &unitDatabase{}} {
		if got, err := createProjectLifecycleAuthority(db); err == nil || got != nil {
			t.Fatal("invalid store accepted")
		}
	}
	foreignLife, err := createProjectLifecycleAuthority(&projectUsageRootStore{})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []*project.LifecycleAuthority{nil, {}, foreignLife} {
		if got, err := createProjectUsageAuthorityWithLifecycle(store, accounts, bad); got != nil {
			t.Fatal("missing or foreign Life accepted")
		} else {
			lifecycleAssemblyFault(t, err, f.DependencyUnbound)
		}
	}
}

// A canonical lifecycle call must enter the exact original Store. It stops at
// the executor boundary: no fake Project rows, participant grant or SQL success.
type lifecycleAssemblyRouteStore struct {
	projectUsageRootStore
	tx      f.Tx
	entered int
}

func (s *lifecycleAssemblyRouteStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		panic("lifecycle substituted original Tx")
	}
	s.entered++
	return nil, f.NewFault(f.DependencyUnavailable, f.NotCommitted)
}

func TestDefaultProjectLifecycleOriginalRoute(t *testing.T) {
	store := &lifecycleAssemblyRouteStore{tx: f.NewTx()}
	cfg := testConfig(t, "1s")
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := createProjectLifecycleAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	base, err := createProjectUsageAuthorityWithLifecycle(store, accounts, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindProjectUsage(cfg, store, accounts, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(updateRootID[i.Project]())
	if err != nil {
		t.Fatal(err)
	}
	cause := pc.LifecycleCause{OperationID: updateRootID[pc.Operation](), Action: pc.Delete, ProjectVersion: 3}
	actor, err := registration.Actor(cause.OperationID.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	for n, authority := range []*project.Authority{base.projects, bound.projects} {
		lifecycleAssemblyFault(t, authority.ValidateLifecycleInTx(context.Background(), store.tx, actor, cause, pc.WorkParticipant, pc.StopPhase), f.DependencyUnavailable)
		if store.entered != n+1 {
			t.Fatal("default or bound Usage lost original Life routing")
		}
	}
	legacy, err := createProjectUsageAuthority(store, accounts)
	if err != nil {
		t.Fatal(err)
	}
	lifecycleAssemblyFault(t, legacy.projects.ValidateLifecycleInTx(context.Background(), store.tx, actor, cause, pc.WorkParticipant, pc.StopPhase), f.DependencyUnbound)
	for _, name := range []pc.ParticipantName{pc.KnowledgeParticipant, pc.UsageParticipant, pc.ModelParticipant, pc.RunnerManagedParticipant} {
		lifecycleAssemblyFault(t, bound.projects.ValidateLifecycleInTx(context.Background(), store.tx, actor, cause, name, pc.CleanupPhase), f.DependencyUnbound)
	}
	if store.entered != 2 {
		t.Fatal("unbound legacy or declared-only cleanup reached SQL")
	}
}
