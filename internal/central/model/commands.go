package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type commandRequest struct {
	Meta                     mc.CommandMeta
	Kind, Resource, Provider string
	Expected                 f.Version
	ProviderInput            *mc.ProviderInput
	ModelInput               *mc.ModelInput
	Selection                *mc.PlatformSelection
	MeetingSummary           *mc.ModelID
	Replacement              string
}
type persistedEvent struct {
	Header  ec.Header
	Payload json.RawMessage
}
type mutationPlan struct {
	Project                                   string `json:",omitempty"`
	CommandID, Identity, User, Kind, Resource string
	Semantic                                  f.Digest
	BeforeProvider, AfterProvider             *providerRecord
	BeforeModel, AfterModel                   *modelRecord
	BeforeSelection, AfterSelection           *selectionRecord
	BeforeMeetingSummary                      *meetingSummaryRecord `json:",omitempty"`
	AfterMeetingSummary                       *meetingSummaryRecord `json:",omitempty"`
	Providers                                 []providerRecord
	Models                                    []modelRecord
	References                                []referenceRecord
	Changed                                   []string
	Receipt                                   mc.CommandReceipt
	Metadata                                  ac.ModelMetadataFields
	Events                                    []persistedEvent
	At                                        f.Instant
}
type commandRecord struct {
	ID, Identity, User, Kind, Resource, Phase string
	Semantic                                  f.Digest
	Plan                                      mutationPlan
	Receipt                                   *mc.CommandReceipt
}
type preparedCommand struct {
	authority   *Authority
	actor       id.Actor
	identity    f.CommandIdentity
	plan        mutationPlan
	locks       []f.LockRequest
	events      []ec.Event
	appendPlans []oc.AppendPlan
	usages      []sc.UsageRequest
	usagePlans  []sc.UsageDependencies
}
type commandContextKey struct{}

func commandContext(ctx context.Context, p *preparedCommand) context.Context {
	return context.WithValue(ctx, commandContextKey{}, p)
}
func (a *Authority) contextPlan(ctx context.Context, actor id.Actor) (*preparedCommand, error) {
	p, ok := ctx.Value(commandContextKey{}).(*preparedCommand)
	if !ok || p == nil || p.authority != a || !reflect.DeepEqual(p.actor.Details(), actor.Details()) {
		return nil, fault(f.Forbidden)
	}
	return p, nil
}

func validCommand(kind string) bool { return ac.ModelAction(ac.Action(kind)) }
func commandIdentity(meta mc.CommandMeta, kind string) (f.CommandIdentity, error) {
	if meta.Scope.Details().Kind == id.ProjectScope {
		return f.NewCommandIdentity("model.project", []string{meta.Scope.Details().ProjectID, meta.Actor.Details().UserID}, kind, meta.Key)
	}
	return f.NewCommandIdentity("model.system", []string{meta.Actor.Details().UserID}, kind, meta.Key)
}
func commandSemantic(r commandRequest) (f.Digest, error) {
	if r.MeetingSummary != nil {
		return meetingSummarySemantic(r)
	}
	var provider *providerInput
	if r.ProviderInput != nil {
		v := providerFromInput(*r.ProviderInput)
		provider = &v
	}
	var model *mc.ModelInput
	if r.ModelInput != nil {
		v := r.ModelInput.Clone()
		v.Parameters = normalizedObject(v.Parameters)
		v.RequestOverwrite = normalizedObject(v.RequestOverwrite)
		if v.HeaderOverwrite == nil {
			v.HeaderOverwrite = map[string]string{}
		}
		model = &v
	}
	raw, e := json.Marshal(struct {
		Format, User, Kind, Resource, Provider string
		Expected                               string
		ProviderInput                          *providerInput
		ModelInput                             *mc.ModelInput
		Selection                              *mc.PlatformSelection
		Replacement                            string
	}{"model-command-v1", r.Meta.Actor.Details().UserID, r.Kind, r.Resource, r.Provider, strconv.FormatInt(int64(r.Expected), 10), provider, model, r.Selection, r.Replacement})
	if e != nil {
		return "", unavailable(e)
	}
	if r.Meta.Scope.Details().Kind == id.ProjectScope {
		raw, e = json.Marshal(struct {
			Format, Project, User string
			Request               json.RawMessage
		}{"model-project-command-v1", r.Meta.Scope.Details().ProjectID, r.Meta.Actor.Details().UserID, raw})
		if e != nil {
			return "", unavailable(e)
		}
	}
	return cursor.Digest(raw)
}
func loadCommand(ctx context.Context, x postgres.SQLExecutor, identity f.CommandIdentity) (*commandRecord, error) {
	return loadCommandScope(ctx, x, identity, id.SystemScope())
}
func loadCommandScope(ctx context.Context, x postgres.SQLExecutor, identity f.CommandIdentity, scope id.Scope) (*commandRecord, error) {
	if scope.Validate() != nil || identity.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	owners := identity.OwnerIDs()
	if scope.Details().Kind == id.System {
		if identity.Namespace() != "model.system" || len(owners) != 1 {
			return nil, fault(f.Forbidden)
		}
	} else if identity.Namespace() != "model.project" || len(owners) != 2 || owners[0] != scope.Details().ProjectID {
		return nil, fault(f.Forbidden)
	}
	var r commandRecord
	var plan, receipt []byte
	query := `SELECT id::text,command_identity,user_id::text,command_name,resource_id::text,phase,semantic_digest,mutation_plan,safe_receipt FROM agenteam_model.commands WHERE command_identity=$1 AND scope='system' AND project_id IS NULL`
	args := []any{identity.Canonical()}
	if scope.Details().Kind == id.ProjectScope {
		query = `SELECT id::text,command_identity,user_id::text,command_name,resource_id::text,phase,semantic_digest,mutation_plan,safe_receipt FROM agenteam_model.commands WHERE command_identity=$1 AND scope='project' AND project_id=$2`
		args = append(args, scope.Details().ProjectID)
	}
	e := x.QueryRow(ctx, query, args...).Scan(&r.ID, &r.Identity, &r.User, &r.Kind, &r.Resource, &r.Phase, &r.Semantic, &plan, &receipt)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, unavailable(e)
	}
	// Only the actual scope-filtered row and exact original identity can
	// interpret a historical System plan without the Project discriminator.
	if r.Identity != identity.Canonical() || r.User != owners[len(owners)-1] || r.Kind != identity.Command() {
		return nil, unavailable(nil)
	}
	if json.Unmarshal(plan, &r.Plan) != nil || r.Plan.Project != scope.Details().ProjectID {
		return nil, unavailable(nil)
	}
	if e := decodeMeetingSummaryPlan(plan, &r.Plan); e != nil {
		return nil, e
	}
	if r.Plan.BeforeMeetingSummary != nil && (r.Resource != r.Plan.Resource || r.Kind != r.Plan.Kind || r.ID != r.Plan.CommandID || r.Identity != r.Plan.Identity || r.User != r.Plan.User || r.Semantic != r.Plan.Semantic) {
		return nil, unavailable(nil)
	}
	if r.Phase == "committed" {
		var v mc.CommandReceipt
		if json.Unmarshal(receipt, &v) != nil || v.Validate() != nil {
			return nil, unavailable(nil)
		}
		if r.Plan.BeforeMeetingSummary != nil && (v != r.Plan.Receipt || v.Kind != r.Kind || v.ResourceID != r.Resource) {
			return nil, unavailable(nil)
		}
		r.Receipt = &v
	}
	return &r, nil
}
func (s *Service) runCommand(ctx context.Context, r commandRequest) (mc.CommandReceipt, error) {
	var empty mc.CommandReceipt
	if s.state() == nil {
		return empty, fault(f.DependencyUnbound)
	}
	if e := r.Meta.Validate(); e != nil {
		return empty, e
	}
	if r.Meta.Scope.Details().Kind == id.ProjectScope && r.Kind == "model.selection.update" {
		return empty, fault(f.DependencyUnbound)
	}
	if e := s.state().authority.currentScope(ctx, f.Tx{}, r.Meta.Actor, r.Meta.Scope, receiptIntent(r.Meta.Scope)); e != nil {
		return empty, e
	}
	identity, e := commandIdentity(r.Meta, r.Kind)
	if e != nil {
		return empty, portError(e)
	}
	semantic, e := commandSemantic(r)
	if e != nil {
		return empty, e
	}
	prior, e := loadCommandScope(ctx, s.state().store, identity, r.Meta.Scope)
	if e != nil {
		return empty, e
	}
	if prior != nil {
		return s.replayScope(ctx, r.Meta.Actor, r.Meta.Scope, identity, semantic, nil)
	}
	if e := s.state().authority.currentScope(ctx, f.Tx{}, r.Meta.Actor, r.Meta.Scope, id.Mutate); e != nil {
		return s.recheckPreparationScope(ctx, r.Meta.Actor, r.Meta.Scope, identity, semantic, e)
	}
	p, e := s.prepare(ctx, r, identity, semantic)
	if e != nil {
		return s.recheckPreparationScope(ctx, r.Meta.Actor, r.Meta.Scope, identity, semantic, e)
	}
	ctx = commandContext(ctx, p)
	if e = s.prepareEffects(ctx, p); e != nil {
		return s.recheckPreparationScope(ctx, r.Meta.Actor, r.Meta.Scope, identity, semantic, e)
	}
	var receipt mc.CommandReceipt
	cause, e := f.NewCommandsCause(identity)
	if e != nil {
		return empty, portError(e)
	}
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, p.locks); e != nil {
			return portError(e)
		}
		if e := s.state().authority.currentScope(ctx, tx, r.Meta.Actor, r.Meta.Scope, receiptIntent(r.Meta.Scope)); e != nil {
			return e
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		old, e := loadCommandScope(ctx, x, identity, r.Meta.Scope)
		if e != nil {
			return e
		}
		if old != nil {
			if old.Semantic != semantic {
				return fault(f.IdempotencyKeyReused)
			}
			if old.Phase != "committed" || old.Receipt == nil {
				return fault(f.ResourceBusy)
			}
			receipt = *old.Receipt
			return nil
		}
		if e = s.state().authority.currentScope(ctx, tx, r.Meta.Actor, r.Meta.Scope, id.Mutate); e != nil {
			return e
		}
		if e = s.validateMapping(ctx, x, p); e != nil {
			return e
		}
		if e = s.insertCommand(ctx, x, p); e != nil {
			return e
		}
		if e = s.applyConfiguration(ctx, x, p); e != nil {
			return e
		}
		for i, request := range p.usages {
			result, e := s.state().deps.Secret.ApplyUsageInTx(ctx, tx, request, p.usagePlans[i])
			if e != nil {
				return portError(e)
			}
			if result.Action() != request.Action {
				return unavailable(nil)
			}
		}
		entry, key, e := auditEntry(p.actor, &p.plan)
		if e != nil {
			return e
		}
		if _, e = s.state().deps.Audit.AppendInTx(ctx, tx, entry, key); e != nil {
			return portError(e)
		}
		for i, event := range p.events {
			if _, e = s.state().deps.Events.AppendEventInTx(ctx, tx, p.actor, event, p.appendPlans[i]); e != nil {
				return portError(e)
			}
		}
		raw, e := encoded(p.plan.Receipt)
		if e != nil {
			return e
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_model.commands SET phase='committed',safe_receipt=$2 WHERE id=$1 AND phase='prepared'`, p.plan.CommandID, raw); e != nil {
			return unavailable(e)
		}
		receipt = p.plan.Receipt
		return nil
	})
	if result.State() == f.Unknown {
		return s.replayScope(ctx, r.Meta.Actor, r.Meta.Scope, identity, semantic, &result)
	}
	if e = commitError(result); e != nil {
		return empty, e
	}
	return receipt, nil
}

// A concurrent same-key writer may commit between the optimistic absence read
// and preparation. Its receipt must precede obsolete version/dependency errors.
// This confirmation does no discovery, adds no locks after AcquireAll, and
// preserves the original preparation error (including Unknown) on true absence.
func (s *Service) recheckPreparation(ctx context.Context, actor id.Actor, identity f.CommandIdentity, expected f.Digest, preparationError error) (mc.CommandReceipt, error) {
	return s.recheckPreparationScope(ctx, actor, id.SystemScope(), identity, expected, preparationError)
}
func (s *Service) recheckPreparationScope(ctx context.Context, actor id.Actor, scope id.Scope, identity f.CommandIdentity, expected f.Digest, preparationError error) (mc.CommandReceipt, error) {
	var receipt *mc.CommandReceipt
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return mc.CommandReceipt{}, portError(err)
	}
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, append([]f.LockRequest{commandLock(identity)}, scopeLocks(actor, scope)...)); err != nil {
			return portError(err)
		}
		if err := s.state().authority.currentScope(ctx, tx, actor, scope, receiptIntent(scope)); err != nil {
			return err
		}
		x, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		r, err := loadCommandScope(ctx, x, identity, scope)
		if err != nil {
			return err
		}
		if r == nil {
			return nil
		}
		if r.Semantic != expected {
			return fault(f.IdempotencyKeyReused)
		}
		if r.Phase != "committed" || r.Receipt == nil {
			return fault(f.ResourceBusy)
		}
		value := *r.Receipt
		receipt = &value
		return nil
	})
	if err = commitError(result); err != nil {
		var originalFault, confirmationFault *f.Fault
		if errors.As(preparationError, &originalFault) && originalFault.Code == f.CommitUnknown &&
			(!errors.As(err, &confirmationFault) || confirmationFault.Code != f.IdempotencyKeyReused) {
			return mc.CommandReceipt{}, preparationError
		}
		return mc.CommandReceipt{}, err
	}
	if receipt != nil {
		return *receipt, nil
	}
	return mc.CommandReceipt{}, preparationError
}

// replay also performs private Unknown confirmation. It waits on the original
// canonical writer lock and compares the original expected digest; public
// LookupCommand deliberately has no authority to confirm a different request.
func (s *Service) replay(ctx context.Context, actor id.Actor, identity f.CommandIdentity, expected f.Digest, unknown *f.CommitResult) (mc.CommandReceipt, error) {
	return s.replayScope(ctx, actor, id.SystemScope(), identity, expected, unknown)
}
func (s *Service) replayScope(ctx context.Context, actor id.Actor, scope id.Scope, identity f.CommandIdentity, expected f.Digest, unknown *f.CommitResult) (mc.CommandReceipt, error) {
	var receipt mc.CommandReceipt
	found := false
	cause, _ := f.NewCommandsCause(identity)
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, append([]f.LockRequest{commandLock(identity)}, scopeLocks(actor, scope)...)); e != nil {
			return portError(e)
		}
		if e := s.state().authority.currentScope(ctx, tx, actor, scope, receiptIntent(scope)); e != nil {
			return e
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		r, e := loadCommandScope(ctx, x, identity, scope)
		if e != nil {
			return e
		}
		if r == nil {
			return nil
		}
		if r.Semantic != expected {
			return fault(f.IdempotencyKeyReused)
		}
		if r.Phase == "committed" && r.Receipt != nil {
			receipt = *r.Receipt
			found = true
		}
		return nil
	})
	if result.State() == f.Committed && found {
		return receipt, nil
	}
	err := commitError(result)
	if unknown != nil {
		var ff *f.Fault
		if errors.As(err, &ff) && ff.Code == f.IdempotencyKeyReused {
			return mc.CommandReceipt{}, err
		}
		return mc.CommandReceipt{}, &UnknownCommandError{cause: unknown.Cause(), attempt: unknown.AttemptID()}
	}
	if err != nil {
		return mc.CommandReceipt{}, err
	}
	return mc.CommandReceipt{}, fault(f.ResourceBusy)
}
func (s *Service) insertCommand(ctx context.Context, x postgres.SQLExecutor, p *preparedCommand) error {
	plan, e := encoded(p.plan)
	if e != nil {
		return e
	}
	if len(plan) > 262144 {
		return fault(f.PayloadTooLarge)
	}
	header, e := encoded(p.plan.Events[0].Header)
	if e != nil {
		return e
	}
	_, e = x.Exec(ctx, `INSERT INTO agenteam_model.commands(id,scope,project_id,user_id,command_name,command_identity,key_digest,semantic_digest,resource_id,phase,mutation_plan,event_id,event_header,event_payload,created_at) VALUES($1,$13,$14,$2,$3,$4,$5,$6,$7,'prepared',$8,$9,$10,$11,$12)`, p.plan.CommandID, p.plan.User, p.plan.Kind, p.plan.Identity, hash([]byte(p.identity.Key())).String(), p.plan.Semantic.String(), p.plan.Resource, plan, p.plan.Events[0].Header.EventID.String(), header, []byte(p.plan.Events[0].Payload), p.plan.At.Time(), string(configurationScope(p.plan.Project).Details().Kind), null(p.plan.Project))
	return portError(e)
}
func nextVersion(v f.Version) (f.Version, error) {
	if v < 1 || v == f.Version(math.MaxInt64) {
		return 0, fault(f.InvalidState)
	}
	return v + 1, nil
}
func sameValue(a, b any) bool {
	x, e := json.Marshal(a)
	if e != nil {
		return false
	}
	y, e := json.Marshal(b)
	if e != nil {
		return false
	}
	cx, e := cursor.CanonicalJSON(x)
	if e != nil {
		return false
	}
	cy, e := cursor.CanonicalJSON(y)
	return e == nil && bytes.Equal(cx, cy)
}

func (s *Service) CreateProvider(ctx context.Context, r mc.CreateProviderRequest) (mc.CommandReceipt, error) {
	if r.Scope.Details().Kind == id.ProjectScope && r.CommandMeta.Validate() == nil && (s.state() == nil || nilPort(s.state().authority.state().auth.Projects)) {
		return mc.CommandReceipt{}, fault(f.DependencyUnbound)
	}
	if e := r.Validate(); e != nil {
		return mc.CommandReceipt{}, e
	}
	v := r.Input.Clone()
	return s.runCommand(ctx, commandRequest{Meta: r.CommandMeta, Kind: "provider.create", ProviderInput: &v})
}
func (s *Service) UpdateProvider(ctx context.Context, r mc.UpdateProviderRequest) (mc.CommandReceipt, error) {
	if r.Scope.Details().Kind == id.ProjectScope && r.CommandMeta.Validate() == nil && (s.state() == nil || nilPort(s.state().authority.state().auth.Projects)) {
		return mc.CommandReceipt{}, fault(f.DependencyUnbound)
	}
	if e := r.Validate(); e != nil {
		return mc.CommandReceipt{}, e
	}
	v := r.Input.Clone()
	return s.runCommand(ctx, commandRequest{Meta: r.CommandMeta, Kind: "provider.update", Resource: r.ID.String(), Expected: r.ExpectedVersion, ProviderInput: &v})
}
func (s *Service) DeleteProvider(ctx context.Context, r mc.DeleteProviderRequest) (mc.CommandReceipt, error) {
	if r.Scope.Details().Kind == id.ProjectScope && r.CommandMeta.Validate() == nil && (s.state() == nil || nilPort(s.state().authority.state().auth.Projects)) {
		return mc.CommandReceipt{}, fault(f.DependencyUnbound)
	}
	if e := r.Validate(); e != nil {
		return mc.CommandReceipt{}, e
	}
	return s.runCommand(ctx, commandRequest{Meta: r.CommandMeta, Kind: "provider.delete", Resource: r.ID.String(), Expected: r.ExpectedVersion})
}
func (s *Service) CreateModel(ctx context.Context, r mc.CreateModelRequest) (mc.CommandReceipt, error) {
	if r.Scope.Details().Kind == id.ProjectScope && r.CommandMeta.Validate() == nil && (s.state() == nil || nilPort(s.state().authority.state().auth.Projects)) {
		return mc.CommandReceipt{}, fault(f.DependencyUnbound)
	}
	if e := r.Validate(); e != nil {
		return mc.CommandReceipt{}, e
	}
	v := r.Input.Clone()
	return s.runCommand(ctx, commandRequest{Meta: r.CommandMeta, Kind: "model.create", Provider: r.ProviderID.String(), ModelInput: &v})
}
func (s *Service) UpdateModel(ctx context.Context, r mc.UpdateModelRequest) (mc.CommandReceipt, error) {
	if r.Scope.Details().Kind == id.ProjectScope && r.CommandMeta.Validate() == nil && (s.state() == nil || nilPort(s.state().authority.state().auth.Projects)) {
		return mc.CommandReceipt{}, fault(f.DependencyUnbound)
	}
	if e := r.Validate(); e != nil {
		return mc.CommandReceipt{}, e
	}
	v := r.Input.Clone()
	return s.runCommand(ctx, commandRequest{Meta: r.CommandMeta, Kind: "model.update", Resource: r.ID.String(), Expected: r.ExpectedVersion, ModelInput: &v})
}
func (s *Service) DeleteModel(ctx context.Context, r mc.DeleteModelRequest) (mc.CommandReceipt, error) {
	if r.Scope.Details().Kind == id.ProjectScope && r.CommandMeta.Validate() == nil && (s.state() == nil || nilPort(s.state().authority.state().auth.Projects)) {
		return mc.CommandReceipt{}, fault(f.DependencyUnbound)
	}
	if e := r.Validate(); e != nil {
		return mc.CommandReceipt{}, e
	}
	v := ""
	if r.Replacement != nil {
		v = r.Replacement.String()
	}
	return s.runCommand(ctx, commandRequest{Meta: r.CommandMeta, Kind: "model.delete", Resource: r.ID.String(), Expected: r.ExpectedVersion, Replacement: v})
}
func (s *Service) UpdatePlatformSelection(ctx context.Context, r mc.UpdatePlatformSelectionRequest) (mc.CommandReceipt, error) {
	if r.Scope.Details().Kind == id.ProjectScope && r.CommandMeta.Validate() == nil {
		return mc.CommandReceipt{}, fault(f.DependencyUnbound)
	}
	if e := r.Validate(); e != nil {
		return mc.CommandReceipt{}, e
	}
	v := r.Selection.Clone()
	return s.runCommand(ctx, commandRequest{Meta: r.CommandMeta, Kind: "model.selection.update", Resource: v.ID, Expected: r.ExpectedVersion, Selection: &v})
}

func receiptIntent(scope id.Scope) id.AccessIntent {
	if scope.Details().Kind == id.ProjectScope {
		return id.Read
	}
	return id.Mutate
}
