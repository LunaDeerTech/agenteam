package projectvariable

import (
	"context"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

const appendPurpose = "projectvariable.append-v1"

func eventProject(summary event.Summary) (c.ProjectID, error) {
	h := summary.Header
	if summary.Producer != c.VariableProducer || h.Validate() != nil || summary.PayloadDigest.Validate() != nil || h.EventType != c.VariableChangedName || h.AggregateType != c.VariableAggregate || h.SchemaVersion != 1 || h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil || h.AggregateSequence != nil {
		return c.ProjectID{}, fault(f.Forbidden)
	}
	p, e := f.ParseID[i.Project](h.Scope.ProjectID.String())
	if e != nil {
		return c.ProjectID{}, fault(f.Forbidden)
	}
	return p, nil
}
func appendBinding(r *commandRecord, a i.Actor, summary event.Summary) (f.Digest, []f.LockRequest, []byte, error) {
	if r == nil || r.Plan == nil || r.EventID == nil {
		return "", nil, nil, fault(f.Forbidden)
	}
	if e := validateRecord(r, a); e != nil {
		return "", nil, nil, e
	}
	p, e := eventProject(summary)
	if e != nil {
		return "", nil, nil, e
	}
	raw, e := canonical(recordPayload(r))
	if e != nil {
		return "", nil, nil, e
	}
	if p != r.Project || !sameValue(recordHeader(r), summary.Header) || digest(raw) != summary.PayloadDigest {
		return "", nil, nil, fault(f.Forbidden)
	}
	locks, e := r.Input.locks(r.Key)
	if e != nil {
		return "", nil, nil, e
	}
	raw, e = canonical(struct {
		Purpose   string
		Actor     i.ActorDetails
		Summary   event.Summary
		Operation c.OperationID
		Revision  f.Version
		Stages    [2]oc.Stage
	}{appendPurpose, a.Details(), summary, r.ID, r.Revision, [2]oc.Stage{oc.CurrentAccess, oc.NewFact}})
	if e != nil {
		return "", nil, nil, e
	}
	opaque, e := canonical(struct {
		Purpose   string        `json:"purpose"`
		Operation c.OperationID `json:"operation_id"`
		Revision  f.Version     `json:"plan_revision"`
	}{appendPurpose, r.ID, r.Revision})
	if e != nil {
		return "", nil, nil, e
	}
	return digest(raw), locks, opaque, nil
}

// Project checks current Owner/Session separately; this store-only authority
// proves only its own command, exact plan and facts in the supplied transaction.
func (a *Authority) DiscoverAppend(ctx context.Context, actor i.Actor, summary event.Summary) (oc.Dependencies, error) {
	empty := oc.Dependencies{}
	st := a.state()
	if st == nil {
		return empty, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return empty, fault(f.InvalidArgument)
	}
	p, e := eventProject(summary)
	if e != nil {
		return empty, e
	}
	if e = readInput(actor, p); e != nil {
		return empty, e
	}
	cause, e := readCause("append")
	if e != nil {
		return empty, e
	}
	var out oc.Dependencies
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, readLocks(actor, p)); e != nil {
			return portError(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		r, e := loadEventCommand(ctx, x, summary.Header.EventID)
		if e != nil {
			return e
		}
		binding, locks, opaque, e := appendBinding(r, actor, summary)
		if e != nil {
			return e
		}
		out, e = oc.NewDependencies(st.issuer, binding, locks, opaque)
		return portError(e)
	})
	if e = txError(result); e != nil {
		return empty, e
	}
	return out, nil
}
func (a *Authority) ValidateAppendInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	p, e := eventProject(summary)
	if e != nil {
		return e
	}
	if e = readInput(actor, p); e != nil {
		return e
	}
	if !stage.Valid() || deps.Validate() != nil || !deps.Matches(st.issuer, deps.Binding()) {
		return fault(f.Forbidden)
	}
	x, e := st.store.InTx(tx)
	if e != nil {
		return portError(e)
	}
	if e = st.store.RequireHeldLocks(ctx, tx, deps.Locks()); e != nil {
		return portError(e)
	}
	r, e := loadEventCommand(ctx, x, summary.Header.EventID)
	if e != nil {
		return e
	}
	binding, locks, opaque, e := appendBinding(r, actor, summary)
	if e != nil {
		return e
	}
	if !deps.Matches(st.issuer, binding) || !slices.Equal(opaque, deps.Opaque()) || !slices.EqualFunc(locks, deps.Locks(), func(a, b f.LockRequest) bool { return a.Key.Canonical() == b.Key.Canonical() && a.Mode == b.Mode }) {
		return fault(f.Forbidden)
	}
	if e = st.store.RequireHeldLocks(ctx, tx, locks); e != nil {
		return portError(e)
	}
	if stage == oc.CurrentAccess {
		return nil
	}
	return verifyPostimage(ctx, x, r)
}
func (a *Authority) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	if entry.Validate() != nil || key.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	v, k := entry.Fields(), key.Details()
	if !ac.ProjectVariableAction(v.Action) || k.Producer != ac.ProjectVariableProducer || k.Ordinal != 0 || v.Actor.Details().Kind != i.Human || v.Scope.Details().Kind != i.ProjectScope {
		return fault(f.Forbidden)
	}
	p, e := f.ParseID[i.Project](v.Scope.Details().ProjectID)
	if e != nil {
		return fault(f.Forbidden)
	}
	if e = st.store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(v.Actor.Details().UserID, f.Exclusive), projectLock(p, f.Exclusive)}); e != nil {
		return portError(e)
	}
	x, e := st.store.InTx(tx)
	if e != nil {
		return portError(e)
	}
	m, e := v.Metadata.ProjectVariableFields()
	if e != nil {
		return fault(f.Forbidden)
	}
	r, e := scanCommand(x.QueryRow(ctx, `SELECT `+commandColumns+` FROM agenteam_projectvariable.commands WHERE id=(SELECT operation_id FROM agenteam_projectvariable.history WHERE project_id=$1 AND variable_id=$2 AND version=$3)`, p.String(), m.VariableID, int64(m.Version)))
	if e != nil {
		return e
	}
	if r == nil || r.Project != p {
		return fault(f.Forbidden)
	}
	if e = validateRecord(r, v.Actor); e != nil {
		return e
	}
	locks, e := r.Input.locks(r.Key)
	if e != nil {
		return e
	}
	if e = st.store.RequireHeldLocks(ctx, tx, locks); e != nil {
		return portError(e)
	}
	expected, expectedKey, e := recordAudit(r, v.Actor)
	if e != nil {
		return e
	}
	wanted := expected.Fields()
	if expectedKey.Details() != k || wanted.Action != v.Action || wanted.Outcome != v.Outcome || wanted.Resource.Details() != v.Resource.Details() || wanted.Scope.Details() != v.Scope.Details() || !wanted.Actor.Equal(v.Actor) || wanted.Associations != v.Associations || !slices.Equal(wanted.Metadata.JSON(), v.Metadata.JSON()) {
		return fault(f.Forbidden)
	}
	return verifyPostimage(ctx, x, r)
}

var _ oc.ProducerAuthority = (*Authority)(nil)
var _ ac.ProjectFactAuthority = (*Authority)(nil)
