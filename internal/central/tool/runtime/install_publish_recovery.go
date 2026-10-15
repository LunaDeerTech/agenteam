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

type installPublicationLookupKey struct{}
type installPublicationLookup struct {
	issuer  *installAuthorityState
	handoff *installHandoff
	ctx     context.Context
	unknown installCommitProvenance
	active  atomic.Bool
}

// The original dispatcher may borrow only its retained, actually returned
// backend. This context is a fresh read lifetime; h.live stays false and the
// old cancelled dispatch context is never revived.
func newInstallPublicationLookup(ctx context.Context, d *installAuthorityState, h *installHandoff) (context.Context, func(), error) {
	if err := contextError(ctx); err != nil {
		return nil, nil, err
	}
	if d == nil || h == nil || !h.boundExecutor(d) {
		return nil, nil, fail(f.Forbidden)
	}
	d.mu.Lock()
	active := d.active[h.record.ID] == h
	d.mu.Unlock()
	if !active {
		return nil, nil, fail(f.Forbidden)
	}
	select {
	case <-h.done:
	default:
		return nil, nil, fail(f.ResourceBusy)
	}
	h.mu.Lock()
	live, terminal, retirement := h.live, h.terminal, h.retirement
	h.mu.Unlock()
	unknown := installUnknown(retirement)
	details, err := h.call.Details()
	if err != nil {
		return nil, nil, err
	}
	command, err := f.NewCommandIdentity("project", []string{h.binding.ProjectID.String()}, "skill.install", details.Key)
	if err != nil || live || terminal == nil || terminal.Known || terminal.validate(h) != nil || unknown == nil || terminal.Unknown == nil || unknown.Attempt != terminal.Unknown.Attempt || unknown.Kind != f.CommandsCause || unknown.Primary != command.Canonical() || terminal.Unknown.Primary != unknown.Primary {
		return nil, nil, fail(f.Forbidden)
	}
	run, cancel := context.WithCancel(ctx)
	w := &installPublicationLookup{issuer: d, handoff: h, ctx: run, unknown: *unknown}
	w.active.Store(true)
	return context.WithValue(run, installPublicationLookupKey{}, w), func() { w.active.Store(false); cancel() }, nil
}

func (a *InstallAuthority) publicationLookup(ctx context.Context) (*installAuthorityState, *installPublicationLookup, error) {
	if err := contextError(ctx); err != nil {
		return nil, nil, err
	}
	if a == nil || a.data == nil {
		return nil, nil, fail(f.DependencyUnbound)
	}
	d := a.data()
	w, _ := ctx.Value(installPublicationLookupKey{}).(*installPublicationLookup)
	if w == nil || w.issuer != d || w.handoff == nil || !w.active.Load() || w.ctx == nil || w.ctx.Err() != nil || !w.handoff.boundExecutor(d) {
		return nil, nil, fail(f.Forbidden)
	}
	d.mu.Lock()
	active := d.active[w.handoff.record.ID] == w.handoff
	d.mu.Unlock()
	w.handoff.mu.Lock()
	live := w.handoff.live
	unknown := installUnknown(w.handoff.retirement)
	w.handoff.mu.Unlock()
	if !active || live || unknown == nil || unknown.Attempt != w.unknown.Attempt || unknown.Kind != w.unknown.Kind || unknown.Primary != w.unknown.Primary {
		return nil, nil, fail(f.Forbidden)
	}
	return d, w, nil
}

type installPublicationLookupPlan struct {
	issuer  *installAuthorityState
	witness *installPublicationLookup
	request sc.InstallLookupRequest
	locks   []f.LockRequest
	binding sc.InstallExecutionBinding
}

func (p *installPublicationLookupPlan) RequiredLocks() []f.LockRequest      { return slices.Clone(p.locks) }
func (p *installPublicationLookupPlan) Binding() sc.InstallExecutionBinding { return p.binding }

// This public plan contains a private pointer to original argument ownership.
// Recursive formatting must never walk that retained call.
func (*installPublicationLookupPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_install_lookup_plan")
}
func (*installPublicationLookupPlan) LogValue() slog.Value {
	return slog.StringValue("tool_install_lookup_plan")
}
func (*installPublicationLookupPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_install_lookup_plan"`), nil
}

func (a *InstallAuthority) DiscoverInstallLookup(ctx context.Context, request sc.InstallLookupRequest) (sc.InstallExecutionPlan, error) {
	if ctx == nil || request.Validate() != nil {
		return nil, fail(f.Forbidden)
	}
	if ctx.Value(installColdLookupKey{}) != nil {
		return a.discoverColdInstallLookup(ctx, request)
	}
	if ctx.Value(installPublicationLookupKey{}) == nil {
		// Preserve the already-supported live read; it still needs its own
		// original active handoff, never this returned-backend witness.
		return a.DiscoverInstallExecution(ctx, request.Original)
	}
	d, w, err := a.publicationLookup(ctx)
	if err != nil {
		return nil, err
	}
	h := w.handoff
	if !h.matches(request.Original) || request.Original.Command.Canonical() != w.unknown.Primary {
		return nil, fail(f.Forbidden)
	}
	b := sc.InstallExecutionBinding{OperationID: h.record.ID.String(), AttemptID: h.attempt.String(), ToolID: h.binding.Spec.ToolID, SpecRevision: h.binding.Spec.SpecRevision, HandlerID: h.binding.Binding.HandlerID, ContractRevision: h.binding.Binding.ContractRevision, Fingerprint: h.record.Fingerprint}
	if !b.Matches(request.Original) {
		return nil, fail(f.InvalidState)
	}
	return &installPublicationLookupPlan{issuer: d, witness: w, request: request, locks: slices.Clone(h.locks), binding: b}, nil
}

func (a *InstallAuthority) RequireInstallLookupInTx(ctx context.Context, tx f.Tx, request sc.InstallLookupRequest, plan sc.InstallExecutionPlan) error {
	if ctx == nil || request.Validate() != nil {
		return fail(f.Forbidden)
	}
	if ctx.Value(installColdLookupKey{}) != nil {
		return a.requireColdInstallLookupInTx(ctx, tx, request, plan)
	}
	if ctx.Value(installPublicationLookupKey{}) == nil {
		return a.RequireInstallExecutionInTx(ctx, tx, request.Original, plan)
	}
	d, w, err := a.publicationLookup(ctx)
	if err != nil {
		return err
	}
	p, ok := plan.(*installPublicationLookupPlan)
	if !ok || p == nil || p.issuer != d || p.witness != w || !p.request.Equal(request) || !w.handoff.matches(request.Original) {
		return fail(f.Forbidden)
	}
	return requireInstallPublicationLookup(ctx, tx, d, w.handoff)
}

func requireInstallPublicationLookup(ctx context.Context, tx f.Tx, d *installAuthorityState, h *installHandoff) error {
	x, err := d.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if _, err = h.executor.core.data().store.InTx(tx); err != nil {
		return portError(err)
	}
	if err = d.store.RequireHeldLocks(ctx, tx, h.locks); err != nil {
		return portError(err)
	}
	if d.guard == nil {
		return fail(f.DependencyUnbound)
	}
	if err = d.guard.RequireCurrentProcessInTx(ctx, tx, d.process); err != nil {
		return portError(err)
	}
	if err = h.executor.permission.AuthorizeInstallInTx(ctx, tx, h.input, h.permission); err != nil {
		return portError(err)
	}
	return requireRunningInstall(ctx, x, d, h)
}

func (a *InstallAuthority) RequireCurrentSkillInstallLookup(ctx context.Context, call builtin.SkillInstallCall, request f.ID[f.Request]) error {
	if ctx != nil && ctx.Value(installColdLookupKey{}) != nil {
		return a.requireCurrentColdInstallLookup(ctx, call, request)
	}
	d, w, err := a.publicationLookup(ctx)
	if err != nil {
		return err
	}
	h := w.handoff
	want, err := h.call.Details()
	got, actualErr := call.Details()
	actor, actorErr := call.Actor()
	if err != nil || actualErr != nil || actorErr != nil || want != got || !actor.Equal(h.binding.Actor) || request != h.request {
		return fail(f.Forbidden)
	}
	result := d.store.WithinTx(ctx, h.cause, func(ctx context.Context, tx f.Tx) error {
		if err := d.store.AcquireAll(ctx, tx, h.locks); err != nil {
			return portError(err)
		}
		return requireInstallPublicationLookup(ctx, tx, d, h)
	})
	return commitError(result)
}

// This helper issues only the scoped read. Its caller must retain the original
// Unknown for every error/missing outcome, and may finalize only a returned
// exact canonical receipt.
func lookupReturnedInstallPublication(ctx context.Context, d *installAuthorityState, h *installHandoff) (builtin.SkillInstallResult, error) {
	run, close, err := newInstallPublicationLookup(ctx, d, h)
	if err != nil {
		return builtin.SkillInstallResult{}, err
	}
	defer close()
	return h.executor.source.adapter.Lookup(run, h.call, h.request)
}

// The caller holds recoveryMu for this original returned owner. A successful
// read permits the first Tool finalization of that canonical receipt; it never
// reruns installation or upgrades an already-persisted unknown Tool terminal.
func recoverInstallPublicationLocked(ctx context.Context, d *installAuthorityState, h *installHandoff, original error) error {
	result, err := lookupReturnedInstallPublication(ctx, d, h)
	if err != nil {
		// Missing/reserved rows, revocation, cancellation and read-only Commit
		// Unknown do not settle the original publication transaction.
		return original
	}
	terminal := terminalInstall(h, result, nil)
	if !terminal.Known || terminal.validate(h) != nil {
		return original
	}
	commit := finishInstallAttempt(ctx, d, h, h.executor.schemas, &terminal)
	if err = commitError(commit); err != nil {
		h.mu.Lock()
		if commit.State() == f.Unknown {
			// This first finalization is a different physical write. Preserve
			// its own identity and the exact validated bytes for its observer.
			h.terminal = &terminal
			h.retirement = err
		} else {
			// A definite rollback leaves the original publication observation
			// pending; a later read may retry only this original finalization.
			h.finalRaw = nil
		}
		h.mu.Unlock()
		if commit.State() != f.Unknown {
			return original
		}
		return err
	}
	h.mu.Lock()
	h.terminal = &terminal
	h.retirement = nil
	h.mu.Unlock()
	d.mu.Lock()
	if d.active[h.record.ID] == h {
		delete(d.active, h.record.ID)
	}
	d.mu.Unlock()
	return nil
}

var _ sc.InstallLookupAuthority = (*InstallAuthority)(nil)
var _ builtin.SkillInstallLookupOperationAuthority = (*InstallAuthority)(nil)
