package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/authorization"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
)

// No control binds an Object guard. Positive actual Source registration is a
// separate real Object/Skill/Registry composition test, not a nil-interface test.
func TestInstallSourceRequiresLiveGuardAndOriginalTransaction(t *testing.T) {
	store := &operationStoreControl{t: t, tx: f.NewTx(), alive: true}
	process := newOperationID[oc.Process](t)
	a, err := NewInstallAuthority(store, &object.ProcessGuard{}, process)
	if err != nil {
		t.Fatal("authority could not precede Object initialization", err)
	}
	source, err := NewInstallSource(a, &skill.Service{})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"foreign-tx", "missing-lock", "unbound-guard", "stopped", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			tx := store.tx
			ctx := context.Background()
			store.held = []f.LockRequest{registry.RegistryLock(f.Exclusive)}
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "missing-lock":
				store.held = nil
			case "stopped":
				a.Stop()
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			got, e := source.DescribeInTx(ctx, tx)
			if e == nil || got.Active || got.Definition.StableKey != "" || store.queries != 0 || store.inserts != 0 {
				t.Fatal("unready source advertised Active or performed business SQL")
			}
			if mode == "cancelled" && !errors.Is(e, context.Canceled) {
				t.Fatal("original cancellation lost")
			}
			if e = source.CheckBindingInTx(ctx, tx, installRegistration()); e == nil {
				t.Fatal("metadata alone established a binding")
			}
		})
	}
}

func TestInstallSourceExactDefinitionAndSafeCopies(t *testing.T) {
	ctx := context.Background()
	if err := exactInstallRegistration(ctx, installRegistration()); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*tc.BuiltinRegistration){
		func(v *tc.BuiltinRegistration) { v.Active = false },
		func(v *tc.BuiltinRegistration) { v.Binding.HandlerID = "other.install" },
		func(v *tc.BuiltinRegistration) { v.Binding.ContractRevision = 2 },
		func(v *tc.BuiltinRegistration) { v.ScopeResolverID = "other.scope" },
		func(v *tc.BuiltinRegistration) { v.RiskClassifierID = "other.risk" },
		func(v *tc.BuiltinRegistration) { v.Class = tc.CoreTool },
		func(v *tc.BuiltinRegistration) { v.Definition.Name = "other" },
		func(v *tc.BuiltinRegistration) { v.Definition.InputSchema = json.RawMessage(`true`) },
		func(v *tc.BuiltinRegistration) { v.Definition.OutputSchema = nil },
	} {
		v := installRegistration()
		edit(&v)
		if err := exactInstallRegistration(ctx, v); err == nil {
			t.Fatal("foreign definition or code binding accepted")
		}
	}
	copy := installRegistration()
	copy.Definition.InputSchema[0] = 'X'
	copy.Definition.Annotations.Destructive = true
	if err := exactInstallRegistration(ctx, installRegistration()); err != nil {
		t.Fatal("caller changed source definition")
	}
	var log bytes.Buffer
	source := &InstallSource{}
	slog.New(slog.NewJSONHandler(&log, nil)).Info("source", "nested", map[string]any{"sources": []*InstallSource{source}})
	raw, err := json.Marshal(struct{ Source *InstallSource }{source})
	if err != nil || !strings.Contains(string(raw), "skill_install_source") || !strings.Contains(log.String(), "skill_install_source") || fmt.Sprint(source) != "skill_install_source" {
		t.Fatal("default source projection is not safe")
	}
}

func TestInstallExecutorUsesSourceBackendAndStore(t *testing.T) {
	core, store, _, _, _ := operationFixture(t)
	a, err := NewInstallAuthority(store, &object.ProcessGuard{}, newOperationID[oc.Process](t))
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewInstallSource(a, &skill.Service{})
	if err != nil {
		t.Fatal(err)
	}
	permission := &authorization.Service{}
	schemas := &RegistrySchemaValidator{}
	executor, err := NewInstallExecutor(core, permission, source, schemas)
	if err != nil || executor.data().source != source.data() || executor.data().source.adapter == nil {
		t.Fatal("executor substituted a backend", err)
	}
	otherCore, _, _, _, _ := operationFixture(t)
	if got, err := NewInstallExecutor(otherCore, permission, source, schemas); err == nil || got != nil {
		t.Fatal("foreign core Store accepted")
	}
	if got, err := NewInstallExecutor(core, nil, source, schemas); err == nil || got != nil {
		t.Fatal("missing current authorization accepted")
	}
	if got, err := NewInstallSource(a, nil); err == nil || got != nil {
		t.Fatal("missing real service accepted")
	}
	if sameInstallInstance([]byte{1}, []byte{1}) || sameInstallInstance((*skill.Service)(nil), (*skill.Service)(nil)) {
		t.Fatal("incomparable or nil identity accepted")
	}
	// Constructing the graph grants nothing: no private active original call.
	if err = a.RequireCurrentSkillInstall(context.Background(), builtin.SkillInstallCall{}); err == nil {
		t.Fatal("composition manufactured execution authority")
	}
}
