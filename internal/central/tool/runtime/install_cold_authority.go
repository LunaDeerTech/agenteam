package runtime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
)

type installColdLookupKey struct{}
type installColdLookup struct {
	read   *installColdRead
	ctx    context.Context
	active atomic.Bool
}

func newInstallColdLookup(ctx context.Context, r *installColdRead) (context.Context, func()) {
	run, cancel := context.WithCancel(ctx)
	w := &installColdLookup{read: r, ctx: run}
	w.active.Store(true)
	return context.WithValue(run, installColdLookupKey{}, w), func() { w.active.Store(false); cancel() }
}

func (a *InstallAuthority) coldLookup(ctx context.Context) (*installColdLookup, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if a == nil || a.data == nil {
		return nil, fail(f.DependencyUnbound)
	}
	w, _ := ctx.Value(installColdLookupKey{}).(*installColdLookup)
	if w == nil || !w.active.Load() || w.ctx == nil || w.ctx.Err() != nil || w.read == nil {
		return nil, fail(f.Forbidden)
	}
	r := w.read
	if r.issuer != a.data() || r.call == nil || r.executor == nil || r.ctx == nil || r.ctx.Err() != nil {
		return nil, fail(f.Forbidden)
	}
	c := r.call
	d := c.dispatcher
	if d == nil || d.executor == nil || d.executor.data() == nil || r.executor != d.executor.data() || r.executor.source == nil || r.executor.source.authority != a || !sameInstallInstance(r.executor.core.data().store, r.issuer.store) {
		return nil, fail(f.Forbidden)
	}
	d.mu.Lock()
	current := d.cold[c.binding.Identity()] == c && c.busy && c.proved && d.calls[c.binding.Identity()] == nil
	d.mu.Unlock()
	if !current || c.origin.process.Validate() != nil || c.origin.process == r.issuer.process || c.origin.attempt.Validate() != nil || c.origin.request.Validate() != nil {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}

func (r *installColdRead) matches(request sc.InstallLookupRequest) bool {
	c := r.call
	v := request.Original
	return request.Validate() == nil && v.Actor.Equal(c.binding.Actor) && v.ProjectID == c.binding.ProjectID && v.Command.Key() == c.record.Key && v.RequestID == c.origin.request && v.SkillID == c.record.SkillID && v.NormalizedName == c.record.Input.NormalizedName && v.PackageSHA256 == c.record.Input.PackageDigest && v.ManifestSHA256 == c.record.Input.ManifestDigest && v.ByteSize == c.record.Input.PackageBytes
}

type installColdLookupPlan struct {
	witness *installColdLookup
	request sc.InstallLookupRequest
	locks   []f.LockRequest
	binding sc.InstallExecutionBinding
}

func (p *installColdLookupPlan) RequiredLocks() []f.LockRequest      { return slices.Clone(p.locks) }
func (p *installColdLookupPlan) Binding() sc.InstallExecutionBinding { return p.binding }
func (*installColdLookupPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_install_cold_lookup_plan")
}
func (*installColdLookupPlan) LogValue() slog.Value {
	return slog.StringValue("tool_install_cold_lookup_plan")
}
func (*installColdLookupPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_install_cold_lookup_plan"`), nil
}

func (a *InstallAuthority) discoverColdInstallLookup(ctx context.Context, request sc.InstallLookupRequest) (sc.InstallExecutionPlan, error) {
	w, err := a.coldLookup(ctx)
	if err != nil {
		return nil, err
	}
	if !w.read.matches(request) {
		return nil, fail(f.Forbidden)
	}
	c := w.read.call
	b := sc.InstallExecutionBinding{OperationID: c.record.ID.String(), AttemptID: c.origin.attempt.String(), ToolID: c.binding.Spec.ToolID, SpecRevision: c.binding.Spec.SpecRevision, HandlerID: c.binding.Binding.HandlerID, ContractRevision: c.binding.Binding.ContractRevision, Fingerprint: c.record.Fingerprint}
	if !b.Matches(request.Original) {
		return nil, fail(f.InvalidState)
	}
	return &installColdLookupPlan{w, request, slices.Clone(w.read.locks), b}, nil
}

func (a *InstallAuthority) requireColdInstallLookupInTx(ctx context.Context, tx f.Tx, request sc.InstallLookupRequest, plan sc.InstallExecutionPlan) error {
	w, err := a.coldLookup(ctx)
	if err != nil {
		return err
	}
	p, ok := plan.(*installColdLookupPlan)
	if !ok || p == nil || p.witness != w || !p.request.Equal(request) || !w.read.matches(request) {
		return fail(f.Forbidden)
	}
	x, err := w.read.requireCurrent(ctx, tx)
	if err != nil {
		return err
	}
	return w.read.requireRunning(ctx, tx, x)
}

func (a *InstallAuthority) requireCurrentColdInstallLookup(ctx context.Context, call builtin.SkillInstallCall, request f.ID[f.Request]) error {
	w, err := a.coldLookup(ctx)
	if err != nil {
		return err
	}
	r := w.read
	want, e1 := r.input.Call.Details()
	got, e2 := call.Details()
	actor, e3 := call.Actor()
	if e1 != nil || e2 != nil || e3 != nil || want != got || !actor.Equal(r.call.binding.Actor) || request != r.call.origin.request {
		return fail(f.Forbidden)
	}
	return commitError(r.transaction(ctx, r.requireRunning))
}
