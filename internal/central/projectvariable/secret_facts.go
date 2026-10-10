package projectvariable

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const secretAppendPurpose = "projectvariable.secret.append-v1"

type secretDiscoveryKey struct{}
type secretMutationKey struct{}
type secretDiscovery struct {
	issuer      *authorityState
	plan        *secretMutationPlan
	preparation sc.ProjectVariablePreparationFields
	locks       []f.LockRequest
	value       bool
}
type secretMutationWitness struct {
	discovery   *secretDiscovery
	tx          f.Tx
	observation sc.ProjectVariableWriteObservation
	entry       ac.Entry
	key         ac.AppendKey
	audit       ac.AppendReceipt
}

func (secretDiscovery) Format(w fmt.State, _ rune)       { secretRecordSafe(w) }
func (secretMutationWitness) Format(w fmt.State, _ rune) { secretRecordSafe(w) }

// Only the Owner's private prepare path calls this with a projection returned
// by the actual D04 preparation and an already-checked safe metadata plan.
func secretDiscoveryContext(ctx context.Context, authority *Authority, p *secretMutationPlan, preparation sc.ProjectVariablePreparationFields, locks []f.LockRequest, value bool) (context.Context, error) {
	if ctx == nil || authority.state() == nil || p == nil || len(p.Fields) == 0 || p.Event.Validate() != nil || preparation.Request.Validate() != nil || preparation.ReceiptID.Validate() != nil || preparation.Ref.Validate() != nil {
		return nil, internal(nil)
	}
	r, pr := p.Request.Fields(), preparation.Request.Fields()
	if !r.Actor.Equal(pr.Actor) || r.Identity.Canonical() != pr.Identity.Canonical() || r.ProjectID != pr.ProjectID || r.VariableID != pr.VariableID || r.Kind != pr.Kind || !sameSecretExpected(r.ExpectedVersion, pr.ExpectedVersion) {
		return nil, internal(nil)
	}
	normalized, err := oc.NormalizeLocks(locks)
	if err != nil {
		return nil, portError(err)
	}
	required, err := sc.ProjectVariableWriteLocks(p.Request, preparation.Ref)
	if err != nil {
		return nil, internal(err)
	}
	for _, want := range required {
		found := false
		for _, held := range normalized {
			if held.Key.Canonical() == want.Key.Canonical() && (held.Mode == f.Exclusive || held.Mode == want.Mode) {
				found = true
			}
		}
		if !found {
			return nil, internal(nil)
		}
	}
	copy := *p
	copy.Fields = slices.Clone(p.Fields)
	if p.Before != nil {
		before := *p.Before
		copy.Before = &before
	}
	w := &secretDiscovery{issuer: authority.state(), plan: &copy, preparation: preparation, locks: normalized, value: value}
	return context.WithValue(ctx, secretDiscoveryKey{}, w), nil
}
func secretEventProject(summary event.Summary) (c.ProjectID, error) {
	h := summary.Header
	if summary.Producer != c.SecretVariableProducer || h.Validate() != nil || summary.PayloadDigest.Validate() != nil || h.EventType != c.SecretVariableChangedName || h.AggregateType != c.SecretVariableAggregate || h.SchemaVersion != 1 || h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil || h.AggregateSequence != nil {
		return c.ProjectID{}, fault(f.Forbidden)
	}
	project, err := f.ParseID[i.Project](h.Scope.ProjectID.String())
	if err != nil {
		return c.ProjectID{}, fault(f.Forbidden)
	}
	return project, nil
}
func (a *Authority) secretDiscovery(ctx context.Context, actor i.Actor, summary event.Summary) (*secretDiscovery, f.Digest, []byte, error) {
	if a.state() == nil {
		return nil, "", nil, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return nil, "", nil, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, "", nil, canceled(err)
	}
	project, err := secretEventProject(summary)
	if err != nil {
		return nil, "", nil, err
	}
	if err = readInput(actor, project); err != nil {
		return nil, "", nil, err
	}
	w, ok := ctx.Value(secretDiscoveryKey{}).(*secretDiscovery)
	if !ok || w == nil || w.issuer != a.state() || w.plan == nil {
		return nil, "", nil, fault(f.Forbidden)
	}
	p := w.plan
	r := p.Request.Fields()
	payload, err := canonical(secretPlanPayload(p))
	if err != nil {
		return nil, "", nil, err
	}
	if project != r.ProjectID || !actor.Equal(r.Actor) || !sameValue(summary.Header, secretPlanHeader(p)) || digest(payload) != summary.PayloadDigest {
		return nil, "", nil, fault(f.Forbidden)
	}
	// Opaque contract values have fixed safe JSON. Bind their actual fields,
	// including the private preparation's receipt/ref and complete original actor.
	var before *c.SecretVariable
	var priorCredential string
	var priorVersion f.Version
	if p.Before != nil {
		v := p.Before.Variable
		before = &v
		priorCredential = p.Before.Ref.Details().ID.String()
		priorVersion = p.Before.CredentialVersion
	}
	raw, err := canonical(struct {
		Purpose            string
		Actor              i.ActorDetails
		Summary            event.Summary
		Identity           string
		Kind               sc.MutationKind
		Target             c.VariableID
		Expected           *f.Version
		Operation, History c.OperationID
		Before             *c.SecretVariable
		After              c.SecretVariable
		Deleted            bool
		Fields             []string
		At                 f.Instant
		PriorCredential    string
		PriorVersion       int64
		Receipt            sc.ProjectVariableReceiptID
		Credential         string
		Value              bool
		Stages             [2]oc.Stage
	}{secretAppendPurpose, actor.Details(), summary, r.Identity.Canonical(), r.Kind, r.VariableID, r.ExpectedVersion, p.Operation, p.History, before, p.After, p.Deleted, p.Fields, p.At, priorCredential, int64(priorVersion), w.preparation.ReceiptID, w.preparation.Ref.Details().ID.String(), w.value, [2]oc.Stage{oc.CurrentAccess, oc.NewFact}})
	if err != nil {
		return nil, "", nil, err
	}
	opaque, err := canonical(struct {
		Purpose   string
		Operation c.OperationID
	}{secretAppendPurpose, p.Operation})
	if err != nil {
		return nil, "", nil, err
	}
	return w, digest(raw), opaque, nil
}
func (a *Authority) discoverSecretAppend(ctx context.Context, actor i.Actor, summary event.Summary) (oc.Dependencies, error) {
	w, binding, opaque, err := a.secretDiscovery(ctx, actor, summary)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.state().issuer, binding, w.locks, opaque)
}
func (a *Authority) validateSecretAppend(ctx context.Context, tx f.Tx, actor i.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	w, binding, opaque, err := a.secretDiscovery(ctx, actor, summary)
	if err != nil {
		return err
	}
	if !stage.Valid() || deps.Validate() != nil || !deps.Matches(a.state().issuer, binding) || !bytes.Equal(opaque, deps.Opaque()) || !slices.EqualFunc(w.locks, deps.Locks(), func(x, y f.LockRequest) bool { return x.Key.Canonical() == y.Key.Canonical() && x.Mode == y.Mode }) {
		return fault(f.Forbidden)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return portError(err)
	}
	mutation, ok := ctx.Value(secretMutationKey{}).(*secretMutationWitness)
	if !ok || mutation == nil || mutation.discovery != w || mutation.tx != tx {
		return fault(f.Forbidden)
	}
	if err = validateSecretApplied(w.plan, w.preparation, mutation.observation, w.value); err != nil {
		return err
	}
	if stage == oc.CurrentAccess {
		return nil
	}
	if mutation.audit.AuditID.Validate() != nil || mutation.audit.CreatedAt.Validate() != nil {
		return fault(f.Forbidden)
	}
	r := w.plan.Request.Fields()
	saved, err := loadSecretCommand(ctx, x, r.ProjectID, secretRequestCommand(w.plan.Request), r.Identity.Key())
	if err != nil {
		return err
	}
	if saved == nil || saved.ID != w.plan.Operation || !secretRecordMatchesRequest(saved, w.plan.Request) || !sameSecretObservation(saved.Observation, mutation.observation) {
		return fault(f.Forbidden)
	}
	m := saved.Receipt.Fields()
	if !m.Changed || m.AuditID == nil || *m.AuditID != mutation.audit.AuditID || m.EventID == nil || *m.EventID != w.plan.Event {
		return fault(f.Forbidden)
	}
	if w.plan.Deleted {
		v := w.plan.After.Fields()
		if m.Deleted == nil || m.Deleted.ID != v.ID || m.Deleted.ProjectID != v.ProjectID || m.Deleted.Version != v.Version || m.Deleted.DeletedAt != v.UpdatedAt {
			return fault(f.Forbidden)
		}
	} else if !sameValue(m.Variable, w.plan.After) {
		return fault(f.Forbidden)
	}
	return verifySecretPostimage(ctx, x, w.plan, mutation.observation)
}
func sameSecretAuditEntry(x, y ac.Entry) bool {
	if x.Validate() != nil || y.Validate() != nil {
		return false
	}
	a, b := x.Fields(), y.Fields()
	return a.Scope.Equal(b.Scope) && a.Actor.Equal(b.Actor) && a.Action == b.Action && a.Outcome == b.Outcome && a.Resource.Details() == b.Resource.Details() && a.Associations == b.Associations && bytes.Equal(a.Metadata.JSON(), b.Metadata.JSON())
}

// This function is used only after real D04 Apply and D10 canonical/history
// writes have succeeded in the caller's one live final transaction.
func secretMutationContext(ctx context.Context, authority *Authority, tx f.Tx, observation sc.ProjectVariableWriteObservation, entry ac.Entry, key ac.AppendKey) (context.Context, error) {
	w, ok := ctx.Value(secretDiscoveryKey{}).(*secretDiscovery)
	if !ok || w == nil || w.issuer != authority.state() || !tx.Valid() {
		return nil, internal(nil)
	}
	if _, err := authority.state().store.InTx(tx); err != nil {
		return nil, portError(err)
	}
	if err := validateSecretApplied(w.plan, w.preparation, observation, w.value); err != nil {
		return nil, err
	}
	want, wantKey, err := secretPlanAudit(w.plan)
	if err != nil || !sameSecretAuditEntry(want, entry) || wantKey.Details() != key.Details() {
		return nil, internal(err)
	}
	return context.WithValue(ctx, secretMutationKey{}, &secretMutationWitness{discovery: w, tx: tx, observation: observation, entry: entry, key: key}), nil
}
func secretAuditCompletedContext(ctx context.Context, receipt ac.AppendReceipt) (context.Context, error) {
	w, ok := ctx.Value(secretMutationKey{}).(*secretMutationWitness)
	if !ok || w == nil || receipt.AuditID.Validate() != nil || receipt.CreatedAt.Validate() != nil || w.audit.AuditID.Validate() == nil {
		return nil, internal(nil)
	}
	copy := *w
	copy.audit = receipt
	return context.WithValue(ctx, secretMutationKey{}, &copy), nil
}
func (a *Authority) checkSecretProjectAudit(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || entry.Validate() != nil || key.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	v, k := entry.Fields(), key.Details()
	if !ac.ProjectSecretVariableAction(v.Action) || k.Producer != ac.ProjectVariableProducer || k.Ordinal != 0 || v.Actor.Details().Kind != i.Human || v.Scope.Details().Kind != i.ProjectScope {
		return fault(f.Forbidden)
	}
	w, ok := ctx.Value(secretMutationKey{}).(*secretMutationWitness)
	if !ok || w == nil || w.discovery == nil || w.discovery.issuer != st || w.tx != tx || !sameSecretAuditEntry(w.entry, entry) || w.key.Details() != k {
		return fault(f.Forbidden)
	}
	if w.audit.AuditID.Validate() == nil {
		return fault(f.Forbidden)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, w.discovery.locks); err != nil {
		return portError(err)
	}
	if err = validateSecretApplied(w.discovery.plan, w.discovery.preparation, w.observation, w.discovery.value); err != nil {
		return err
	}
	want, wantKey, err := secretPlanAudit(w.discovery.plan)
	if err != nil || !sameSecretAuditEntry(want, entry) || wantKey.Details() != k {
		return fault(f.Forbidden)
	}
	return verifySecretPostimage(ctx, x, w.discovery.plan, w.observation)
}
