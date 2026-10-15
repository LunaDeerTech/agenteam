package humaninbox

import (
	"context"
	"encoding/json"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/humaninbox/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"time"
)

const proposalHandler event.StableName = "humaninbox.meeting_proposal"

type proposalProjector struct{ service *Service }

func (s *Service) Handler() (oc.HandlerDefinition, error) {
	if s == nil || s.authority == nil || nilPort(s.sources) {
		return oc.HandlerDefinition{}, fault(f.DependencyUnbound)
	}
	subscriptions := []oc.Subscription{}
	for _, n := range []event.StableName{mc.ProposedName, mc.ActivatedName, mc.RestoredName, mc.ArchivedName, mc.DeletedName} {
		subscriptions = append(subscriptions, oc.Subscription{EventType: n, Versions: []uint32{1}})
	}
	return oc.HandlerDefinition{ID: proposalHandler, Subscriptions: subscriptions, Effect: oc.CanonicalConverge, Ordering: oc.CanonicalReconcile, Handler: &proposalProjector{s}}, nil
}
func (h *proposalProjector) source(e event.Event) (i.ProjectID, string, error) {
	payload, err := h.service.events.Decode(e)
	if err != nil {
		return i.ProjectID{}, "", err
	}
	if e.Header().Scope.Kind != event.ProjectScope {
		return i.ProjectID{}, "", fault(f.Forbidden)
	}
	p, err := f.ParseID[i.Project](e.Header().Scope.ProjectID.String())
	return p, payload.MeetingID.String(), err
}
func (h *proposalProjector) Prepare(ctx context.Context, e event.Event) (oc.HandlerPlan, error) {
	p, id, err := h.source(e)
	if err != nil {
		return oc.HandlerPlan{}, err
	}
	binding, err := oc.EventDigest(e)
	if err != nil {
		return oc.HandlerPlan{}, err
	}
	deps, err := oc.NewDependencies(h.service.authority.issuer, binding, []f.LockRequest{projectLock(p, f.Shared), meetingLock(id)}, nil)
	if err != nil {
		return oc.HandlerPlan{}, err
	}
	return oc.NewHandlerPlan(proposalHandler, e, deps)
}
func (h *proposalProjector) ValidateInTx(ctx context.Context, tx f.Tx, e event.Event, plan oc.HandlerPlan) error {
	binding, err := oc.EventDigest(e)
	if err != nil || !plan.Matches(proposalHandler, e) || !plan.Dependencies().Matches(h.service.authority.issuer, binding) {
		return fault(f.Forbidden)
	}
	if _, _, err = h.source(e); err != nil {
		return err
	}
	if _, err = h.service.authority.store.InTx(tx); err != nil {
		return portError(err)
	}
	return portError(h.service.authority.store.RequireHeldLocks(ctx, tx, plan.Locks()))
}
func (h *proposalProjector) HandleInTx(ctx context.Context, tx f.Tx, e event.Event, plan oc.HandlerPlan) oc.Result {
	if err := h.ValidateInTx(ctx, tx, e, plan); err != nil {
		return oc.Retry(oc.AuthorizationChanged)
	}
	p, id, err := h.source(e)
	if err != nil {
		return oc.Reject(oc.AuthorizationChanged)
	}
	var source c.Source
	err = h.service.authority.withSource(ctx, tx, p, id, false, func(ctx context.Context) error {
		var err error
		source, err = h.service.sources.ReadSourceInTx(ctx, tx, p, id)
		return err
	})
	if err != nil {
		return oc.Retry(oc.Unavailable)
	}
	if source.ProjectID != p || source.ID != id || source.Version.Validate() != nil || !proposalProjection.validContext(source.Context) {
		return oc.Retry(oc.AuthorizationChanged)
	}
	x, err := h.service.authority.store.InTx(tx)
	if err != nil {
		return oc.Retry(oc.Unavailable)
	}
	if source.Relevant {
		if err = upsertSource(ctx, x, source); err != nil {
			return oc.Retry(oc.Unavailable)
		}
	}
	raw, _ := json.Marshal(struct {
		ID      string
		Version f.Version
		Open    bool
	}{source.ID, source.Version, source.Open})
	return oc.Ack(oc.DigestBytes(raw))
}
func upsertSource(ctx context.Context, x postgres.SQLExecutor, v c.Source) error {
	return upsertProjection(ctx, x, v, proposalProjection)
}
func upsertProjection(ctx context.Context, x postgres.SQLExecutor, v c.Source, kind projectionKind) error {
	if v.ProjectID.Validate() != nil || v.Version.Validate() != nil || v.OccurredAt.IsZero() || !kind.validContext(v.Context) {
		return fault(f.InvalidArgument)
	}
	if _, err := f.ParseID[struct{}](v.ID); err != nil {
		return fault(f.InvalidArgument)
	}
	id, err := f.NewID[c.ItemIdentity]()
	if err != nil {
		return portError(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	status := "resolved"
	var resolved any = at
	if v.Open {
		status = "open"
		resolved = nil
	}
	sourceType, actionType := kind.pair()
	contextJSON, err := json.Marshal(v.Context)
	if err != nil {
		return fault(f.InvalidArgument)
	}
	blocking, priority := kind == decisionProjection, 0
	if blocking {
		priority = 1
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_humaninbox.items(id,project_id,source_type,source_id,action_type,source_context,status,blocking,priority,dismissible,title_snapshot,summary_snapshot,source_version,source_occurred_at,created_at,updated_at,resolved_at) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,false,$10,$11,$12,$13,$14,$14,$15) ON CONFLICT(source_type,source_id,action_type) DO UPDATE SET status=EXCLUDED.status,source_context=EXCLUDED.source_context,blocking=EXCLUDED.blocking,priority=EXCLUDED.priority,dismissible=EXCLUDED.dismissible,title_snapshot=EXCLUDED.title_snapshot,summary_snapshot=EXCLUDED.summary_snapshot,source_version=EXCLUDED.source_version,source_occurred_at=EXCLUDED.source_occurred_at,updated_at=EXCLUDED.updated_at,resolved_at=CASE WHEN EXCLUDED.status='resolved' THEN COALESCE(agenteam_humaninbox.items.resolved_at,EXCLUDED.resolved_at) ELSE NULL END WHERE agenteam_humaninbox.items.project_id=EXCLUDED.project_id AND (agenteam_humaninbox.items.source_version<EXCLUDED.source_version OR agenteam_humaninbox.items.source_version=EXCLUDED.source_version AND agenteam_humaninbox.items.status<>EXCLUDED.status)`, id.String(), v.ProjectID.String(), sourceType, v.ID, actionType, contextJSON, status, blocking, priority, v.Title, v.Summary, int64(v.Version), v.OccurredAt, at, resolved)
	return portError(err)
}
