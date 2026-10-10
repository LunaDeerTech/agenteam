package skill

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// This controlled provider tests Skill's use of the original private handoff,
// request and transaction. It is not a Runtime issuer or a PG/D05 success.
type installExecutionControlKey struct{}
type installExecutionControl struct {
	store         *installCallStore
	request       sc.InstallExecutionRequest
	binding       sc.InstallExecutionBinding
	locks         []f.LockRequest
	active        bool
	denyAt        int
	reads, writes int
}
type installExecutionControlPlan struct {
	issuer  *installExecutionControl
	request sc.InstallExecutionRequest
	binding sc.InstallExecutionBinding
	locks   []f.LockRequest
}

func (p *installExecutionControlPlan) Binding() sc.InstallExecutionBinding { return p.binding }
func (p *installExecutionControlPlan) RequiredLocks() []f.LockRequest {
	return append([]f.LockRequest(nil), p.locks...)
}
func (p *installExecutionControl) current(ctx context.Context, request sc.InstallExecutionRequest) bool {
	expected := p.request
	expected.Intent = request.Intent
	return ctx.Value(installExecutionControlKey{}) == p && p.active && request.Equal(expected)
}
func (p *installExecutionControl) DiscoverInstallExecution(ctx context.Context, request sc.InstallExecutionRequest) (sc.InstallExecutionPlan, error) {
	if p.store.live || !p.current(ctx, request) {
		return nil, fault(f.Forbidden)
	}
	return &installExecutionControlPlan{p, request, p.binding, append([]f.LockRequest(nil), p.locks...)}, nil
}
func (p *installExecutionControl) RequireInstallExecutionInTx(ctx context.Context, tx f.Tx, request sc.InstallExecutionRequest, plan sc.InstallExecutionPlan) error {
	v, ok := plan.(*installExecutionControlPlan)
	if !ok || v.issuer != p || !v.request.Equal(request) || v.binding != p.binding || !p.current(ctx, request) {
		return fault(f.Forbidden)
	}
	if _, err := p.store.InTx(tx); err != nil {
		return err
	}
	if err := p.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return err
	}
	if p.denyAt != 0 && p.store.txs == p.denyAt {
		return fault(f.Forbidden)
	}
	if request.Intent == id.Read {
		p.reads++
	} else {
		p.writes++
	}
	return nil
}

func executionInstallFixture(t *testing.T) (*Service, *installCallStore, *installCallObjects, *installExecutionControl, context.Context, id.Actor, installationRow, InstallRequest, f.CommandMeta) {
	t.Helper()
	s, store, objects, _, row, request := publicInstallFixture(t)
	agent, execution := stateID[id.Agent](901), stateID[id.Execution](902)
	actor, err := id.NewAgentRun(row.project, agent, execution)
	if err != nil {
		t.Fatal(err)
	}
	binding := sc.InstallExecutionBinding{OperationID: stateID[struct{}](903).String(), AttemptID: stateID[struct{}](904).String(), ToolID: stateID[id.Tool](905), SpecRevision: 1, HandlerID: "skill.install", ContractRevision: 1, Fingerprint: sum([]byte("fixed-operation-input"))}
	meta := f.CommandMeta{RequestID: stateID[f.Request](906), IdempotencyKey: f.IdempotencyKey("tool.skill.install:" + binding.OperationID)}
	row.user, row.key = id.UserID{}, meta.IdempotencyKey
	row.execution = &installationExecutionOrigin{agent, execution, binding, meta.RequestID}
	row.semantic, err = row.semanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	store.installed = row
	store.installationValues = installationValues(t, row)
	objects.actor, objects.row = actor, row
	command, err := installIdentity(row.project, row.key)
	if err != nil {
		t.Fatal(err)
	}
	runtimeCommand, err := f.NewCommandIdentity("project", []string{row.project.String()}, "tool.operation", "original-runtime-command")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := f.SystemConfigLock("tool-registry")
	if err != nil {
		t.Fatal(err)
	}
	// Project EX is a permitted stronger request in this controlled provider;
	// the original fixture's held-lock double intentionally compares exact modes.
	locks, err := row.locks(f.Exclusive)
	if err != nil {
		t.Fatal(err)
	}
	locks = append(locks, commandLock(runtimeCommand), f.LockRequest{Key: registry, Mode: f.Shared})
	locks, err = oc.NormalizeAccessLocks(locks)
	if err != nil {
		t.Fatal(err)
	}
	provider := &installExecutionControl{store: store, active: true, binding: binding, locks: locks, request: sc.InstallExecutionRequest{Actor: actor, ProjectID: row.project, Command: command, RequestID: meta.RequestID, Intent: id.Read, SkillID: row.skill, NormalizedName: row.pkg.normalized, PackageSHA256: row.pkg.packageDigest, ManifestSHA256: row.pkg.manifestDigest, ByteSize: row.pkg.size}}
	a, err := NewAuthorityWithInstallExecution(store, s.state().authority.state().projects, provider)
	if err != nil {
		t.Fatal(err)
	}
	s.state().authority = a
	ctx := context.WithValue(context.Background(), installExecutionControlKey{}, provider)
	return s, store, objects, provider, ctx, actor, row, request, meta
}

func TestAgentInstallCurrentTransactionAndRecovery(t *testing.T) {
	for _, name := range []string{"publish", "publish-revoked", "published-recovery"} {
		t.Run(name, func(t *testing.T) {
			s, store, objects, provider, ctx, actor, row, request, meta := executionInstallFixture(t)
			if name != "published-recovery" {
				store.installationValues[14] = "planned"
				store.installationValues[16], store.installationValues[17], store.installationValues[18] = "", "", ""
			}
			if name == "publish-revoked" {
				provider.denyAt = 4
			}
			receipt, err := s.Install(ctx, actor, meta, row.project, request)
			if name == "publish-revoked" {
				var known *f.Fault
				if !errors.As(err, &known) || known.Code != f.Forbidden || receipt != (InstallReceipt{}) || store.skillInserted || store.installationValues[14] != "reserved" {
					t.Fatal("revoked final transaction published or returned receipt", err)
				}
				if strings.Join(objects.steps, ",") != "prepare,reserve,upload,discard" {
					t.Fatal("failed publication lost original physical cleanup")
				}
			} else {
				if err != nil || receipt.Validate() != nil || receipt.SkillID != row.skill || receipt.ObjectID != row.object {
					t.Fatal("controlled original publication/recovery failed", err)
				}
				before := len(objects.steps)
				// A new physical recovery call keeps the semantic Operation and
				// current permission, without replacing first-attempt provenance.
				provider.binding.AttemptID = stateID[struct{}](907).String()
				meta.RequestID = stateID[f.Request](908)
				provider.request.RequestID = meta.RequestID
				got, e := s.LookupInstallExecution(ctx, actor, meta, row.project, request)
				if e != nil || got != receipt || len(objects.steps) != before {
					t.Fatal("current original-operation lookup resent or changed receipt", e)
				}
				provider.active = false
				got, e = s.LookupInstallExecution(ctx, actor, meta, row.project, request)
				if e == nil || got != (InstallReceipt{}) || len(objects.steps) != before {
					t.Fatal("retired handoff recovered using its old context")
				}
			}
			if store.live || provider.reads == 0 || name != "published-recovery" && provider.writes == 0 || s.state().authority.state().projects.(*installCallProjects).calls != 0 {
				t.Fatal("Agent bypassed original Runtime Tx or borrowed Human grant")
			}
		})
	}
}

func TestAgentInstallRejectsMissingOrChangedHandoff(t *testing.T) {
	for _, name := range []string{"unbound", "no-handoff", "closed", "request", "package", "command", "legacy-lookup"} {
		t.Run(name, func(t *testing.T) {
			s, store, objects, provider, ctx, actor, row, request, meta := executionInstallFixture(t)
			switch name {
			case "unbound":
				a, err := NewAuthority(store, s.state().authority.state().projects)
				if err != nil {
					t.Fatal(err)
				}
				s.state().authority = a
			case "no-handoff":
				ctx = context.Background()
			case "closed":
				provider.active = false
			case "request":
				meta.RequestID = stateID[f.Request](909)
			case "package":
				provider.request.PackageSHA256 = sum([]byte("different-private-package"))
			case "command":
				meta.IdempotencyKey = "other-command"
			}
			var got InstallReceipt
			var err error
			if name == "legacy-lookup" {
				got, err = s.LookupInstall(ctx, actor, row.project, row.key, request)
			} else {
				got, err = s.Install(ctx, actor, meta, row.project, request)
			}
			if err == nil || got != (InstallReceipt{}) || store.txs != 0 || len(objects.steps) != 0 {
				t.Fatal("unbound or changed input reached transaction/Object")
			}
		})
	}
}

func TestInstallationExecutionOriginKeepsHumanCompatibility(t *testing.T) {
	_, store, _, _, _, _, row, _, _ := executionInstallFixture(t)
	decoded, err := scanInstallation(skillRowValues{values: store.installationValues})
	if err != nil || decoded == nil || !sameInstallation(*decoded, row) {
		t.Fatal("exclusive Agent provenance failed round trip", err)
	}
	corrupt := append([]any(nil), store.installationValues...)
	corrupt[2] = stateID[id.User](910).String()
	if got, err := scanInstallation(skillRowValues{values: corrupt}); err == nil || got != nil {
		t.Fatal("Agent row accepted an additional Human origin")
	}
	human := installRepositoryRow(t)
	semantic, err := human.semanticDigest()
	if err != nil || semantic != human.semantic {
		t.Fatal("new origin format rewrote existing Human command")
	}
	// Current Human Owner read of an Agent publication remains an ordinary
	// protected read. Installation provenance cannot require a live old Agent.
	s, humanStore, _, actor, _ := installedReadFixture(t)
	row.project = humanStore.installed.project
	humanStore.installed, humanStore.installationValues = row, installationValues(t, row)
	got, err := s.GetSkill(context.Background(), actor, row.project, row.skill)
	if err != nil || got.ID != row.skill || got.Protected {
		t.Fatal("published Agent source blocked current Human Owner", err)
	}
}
