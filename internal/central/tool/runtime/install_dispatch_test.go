package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/authorization"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type terminalServiceControl struct {
	receipt skill.InstallReceipt
	calls   int
	cancel  context.CancelFunc
}

func (p *terminalServiceControl) Install(context.Context, id.Actor, f.CommandMeta, id.ProjectID, skill.InstallRequest) (skill.InstallReceipt, error) {
	p.calls++
	if p.cancel != nil {
		p.cancel()
	}
	return p.receipt, nil
}

type terminalAuthorityControl struct{}

func (terminalAuthorityControl) RequireCurrentSkillInstall(context.Context, builtin.SkillInstallCall) error {
	return nil
}

// Private state below is a controlled method fixture. It does not manufacture a
// bound ProcessGuard, actual current authority, SQL Attempt or dispatch result.
func installHandoffFixture(t *testing.T) (*installHandoff, sc.InstallExecutionRequest) {
	t.Helper()
	s, _, _, binding, raw := operationFixture(t)
	prepared, err := s.PrepareInstall(context.Background(), binding, raw)
	if err != nil {
		t.Fatal(err)
	}
	r := prepared.data()
	pkg, _, err := parseInstall(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	call, err := builtin.NewSkillInstallCall(context.Background(), binding.Actor, binding.Spec, r.ID.String(), r.SkillID, pkg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := &installHandoff{executor: &installExecutorData{core: s, permission: &authorization.Service{}, schemas: &RegistrySchemaValidator{}}, record: r, binding: binding, call: call, attempt: newOperationID[tc.Attempt](t), request: newOperationID[f.Request](t), cancel: cancel, ctx: ctx, done: make(chan struct{}), live: true}
	command, err := f.NewCommandIdentity("project", []string{binding.ProjectID.String()}, "skill.install", r.Key)
	if err != nil {
		t.Fatal(err)
	}
	request := sc.InstallExecutionRequest{Actor: binding.Actor, ProjectID: binding.ProjectID, Command: command, RequestID: h.request, Intent: id.Mutate, SkillID: r.SkillID, NormalizedName: r.Input.NormalizedName, PackageSHA256: r.Input.PackageDigest, ManifestSHA256: r.Input.ManifestDigest, ByteSize: r.Input.PackageBytes}
	return h, request
}

func TestInstallRuntimePrivateHandoffAndOriginalJoin(t *testing.T) {
	h, request := installHandoffFixture(t)
	d := &installAuthorityState{store: h.executor.core.data().store, active: map[tc.OperationID]*installHandoff{h.record.ID: h}}
	h.issuer = d
	a := &InstallAuthority{data: func() *installAuthorityState { return d }}
	service := &skill.Service{}
	adapter, err := builtin.NewSkillInstallAdapter(service, a)
	if err != nil {
		t.Fatal(err)
	}
	h.executor.source = &installSourceState{authority: a, service: service, adapter: adapter}
	if _, err := a.DiscoverInstallExecution(context.Background(), request); err == nil {
		t.Fatal("public DTO manufactured a handoff")
	}
	ctx := context.WithValue(context.Background(), installHandoffKey{}, installHandoffContext{d, h})
	plan, err := a.DiscoverInstallExecution(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Binding().Matches(request) {
		t.Fatal("original identity lost")
	}
	originalSource := h.executor.source
	h.executor.source = &installSourceState{authority: &InstallAuthority{data: func() *installAuthorityState { return &installAuthorityState{} }}, service: service, adapter: adapter}
	if _, err = a.DiscoverInstallExecution(ctx, request); err == nil {
		t.Fatal("foreign executor/backend admitted to original issuer")
	}
	h.executor.source = originalSource
	changed := request
	changed.RequestID = newOperationID[f.Request](t)
	if _, err = a.DiscoverInstallExecution(ctx, changed); err == nil {
		t.Fatal("foreign physical request accepted")
	}
	changed = request
	changed.ByteSize++
	if _, err = a.DiscoverInstallExecution(ctx, changed); err == nil {
		t.Fatal("package identity changed")
	}
	other := &installAuthorityState{active: d.active}
	foreign := context.WithValue(context.Background(), installHandoffKey{}, installHandoffContext{other, h})
	if _, err = a.DiscoverInstallExecution(foreign, request); err == nil {
		t.Fatal("foreign issuer accepted")
	}
	a.Stop()
	if a.Joined() {
		t.Fatal("cancellation became actual retirement")
	}
	deadline, cancel := context.WithCancel(context.Background())
	cancel()
	if err = a.Drain(deadline); !errors.Is(err, context.Canceled) {
		t.Fatal("caller deadline was replaced")
	}
	h.mu.Lock()
	h.live = false
	h.retirement = commitError(f.UnknownResult(newOperationID[f.TransactionAttempt](t), mustInstallCause(t, h)))
	h.mu.Unlock()
	close(h.done)
	if err = a.Drain(context.Background()); err == nil || a.Joined() {
		t.Fatal("unknown durable tail became joined")
	}
	if _, err = a.DiscoverInstallExecution(ctx, request); err == nil {
		t.Fatal("returned handoff revived")
	}
}

func mustInstallCause(t *testing.T, h *installHandoff) f.TransactionCause {
	t.Helper()
	v, e := inputCause(h.binding)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestInstallRuntimeTerminalReceiptAndUnknownProvenance(t *testing.T) {
	h, _ := installHandoffFixture(t)
	receipt := skill.InstallReceipt{InstallationID: newOperationID[skill.Installation](t), SkillID: h.record.SkillID, ProjectID: h.binding.ProjectID, RevisionID: newOperationID[sc.Revision](t), Revision: 1, Version: 1, ObjectID: newOperationID[oc.StoredObject](t), PackageSHA256: h.record.Input.PackageDigest}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	port := &terminalServiceControl{receipt: receipt, cancel: cancel}
	adapter, err := builtin.NewSkillInstallAdapter(port, terminalAuthorityControl{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Execute(ctx, h.call, h.request)
	if err != nil || port.calls != 1 || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("controlled original result lost")
	}
	terminal := terminalInstall(h, result, nil)
	if err = terminal.validate(h); err != nil || terminal.Receipt == nil || terminal.Receipt.receipt() != receipt || !terminal.Known || terminal.Status != "success" {
		t.Fatal("late cancellation erased committed receipt")
	}
	raw, err := encode(terminal)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("private-operation-canary")) {
		t.Fatal("body entered result metadata")
	}
	var persisted installTerminal
	if err = json.Unmarshal(raw, &persisted); err != nil || persisted.Receipt == nil || persisted.Receipt.receipt() != receipt {
		t.Fatal("full receipt not recoverable")
	}
	copy := terminal.clone()
	copy.Output[0] = ' '
	copy.Receipt.Object = newOperationID[oc.StoredObject](t)
	if terminal.Output[0] == ' ' || terminal.Receipt.receipt() != receipt {
		t.Fatal("terminal projection aliased")
	}
	cause := mustInstallCause(t, h)
	physical := newOperationID[f.TransactionAttempt](t)
	unknown := terminalInstall(h, builtin.SkillInstallResult{}, commitError(f.UnknownResult(physical, cause)))
	if unknown.Known || unknown.Status != "error" || unknown.Category != "unknown_outcome" || unknown.Unknown == nil || unknown.Unknown.Attempt != physical || unknown.Unknown.Primary != cause.Details().Primary.Canonical() {
		t.Fatal("original commit uncertainty lost")
	}
	if err = unknown.validate(h); err != nil {
		t.Fatal(err)
	}
	bad := terminalInstall(h, builtin.SkillInstallResult{}, builtin.ErrSkillInstallReceipt)
	if bad.Known || bad.Category != "backend_contract_violation" || bad.Receipt != nil {
		t.Fatal("bad receipt upgraded")
	}
	denied := terminalInstall(h, builtin.SkillInstallResult{}, fail(f.Forbidden))
	if !denied.Known || denied.Category != "authorization_denied" {
		t.Fatal("explicit refusal became unknown")
	}
	wrapped := errors.New("private-terminal-error-canary")
	errout := terminalInstall(h, builtin.SkillInstallResult{}, wrapped)
	raw, err = encode(errout)
	if err != nil || strings.Contains(string(raw), "private-terminal-error-canary") || errout.Known {
		t.Fatal("unsafe error projection")
	}
}
