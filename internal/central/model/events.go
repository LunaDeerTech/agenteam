package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"reflect"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

const (
	ModelProducer                  ec.StableName = "model"
	ConfigurationChangedEvent      ec.StableName = "model.configuration_changed"
	EmbeddingSelectionChangedEvent ec.StableName = "model.embedding_selection_changed"
	ConfigurationAggregate         ec.StableName = "model.configuration"
	SelectionAggregate             ec.StableName = "model.selection"
)

type modelEvents struct {
	catalog       *ec.Catalog
	configuration ec.EventType[mc.ConfigurationChanged]
	embedding     ec.EventType[mc.EmbeddingSelectionChanged]
}

// ModelEvents can only be obtained by registering both definitions in one
// caller-owned catalog. It cannot be assembled from unrelated EventTypes.
type ModelEvents struct{ data func() modelEvents }

func DefineEvents(catalog *ec.Catalog) (ModelEvents, error) {
	if catalog == nil || !catalog.Valid() {
		return ModelEvents{}, fault(f.InvalidArgument)
	}
	configuration, e := ec.DefineEvent(catalog, ec.Definition[mc.ConfigurationChanged]{Schema: ec.Schema{Producer: ModelProducer, EventType: ConfigurationChangedEvent, AggregateType: ConfigurationAggregate, Version: 1}, Codec: ec.JSONCodec[mc.ConfigurationChanged]{}, Validate: func(v mc.ConfigurationChanged) error { return v.Validate() }})
	if e != nil {
		return ModelEvents{}, e
	}
	embedding, e := ec.DefineEvent(catalog, ec.Definition[mc.EmbeddingSelectionChanged]{Schema: ec.Schema{Producer: ModelProducer, EventType: EmbeddingSelectionChangedEvent, AggregateType: SelectionAggregate, Version: 1}, Codec: ec.JSONCodec[mc.EmbeddingSelectionChanged]{}, Validate: func(v mc.EmbeddingSelectionChanged) error { return v.Validate() }})
	if e != nil {
		return ModelEvents{}, e
	}
	v := modelEvents{catalog, configuration, embedding}
	return ModelEvents{data: func() modelEvents { return v }}, nil
}
func (e ModelEvents) valid() bool {
	if e.data == nil {
		return false
	}
	v := e.data()
	return v.catalog != nil && v.catalog.Valid() && v.configuration.Schema() == (ec.Schema{Producer: ModelProducer, EventType: ConfigurationChangedEvent, AggregateType: ConfigurationAggregate, Version: 1}) && v.embedding.Schema() == (ec.Schema{Producer: ModelProducer, EventType: EmbeddingSelectionChangedEvent, AggregateType: SelectionAggregate, Version: 1})
}
func (e ModelEvents) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "model_events") }
func (e ModelEvents) MarshalJSON() ([]byte, error) { return []byte(`"model_events"`), nil }
func (e ModelEvents) LogValue() slog.Value         { return slog.StringValue("model_events") }
func newHeader(kind, aggregate ec.StableName, resource string, version f.Version, at f.Instant) (ec.Header, error) {
	id, e := f.NewID[ec.EventIdentity]()
	if e != nil {
		return ec.Header{}, unavailable(e)
	}
	agg, e := f.ParseID[ec.Aggregate](resource)
	if e != nil {
		return ec.Header{}, unavailable(e)
	}
	return ec.Header{EventID: id, EventType: kind, SchemaVersion: 1, OccurredAt: at, Scope: ec.Scope{Kind: ec.SystemScope}, AggregateType: aggregate, AggregateID: agg, AggregateVersion: &version}, nil
}
func (s *Service) prepareEvents(p *preparedCommand) error {
	add := func(resource string, version f.Version, changed []string) error {
		h, e := newHeader(ConfigurationChangedEvent, ConfigurationAggregate, resource, version, p.plan.At)
		if e != nil {
			return e
		}
		if p.plan.Project != "" {
			project, err := f.ParseID[ec.Project](p.plan.Project)
			if err != nil {
				return unavailable(err)
			}
			h.Scope = ec.Scope{Kind: ec.ProjectScope, ProjectID: project}
		}
		event, e := ec.NewEvent(s.state().deps.ConfigurationEvents.data().configuration, h, mc.ConfigurationChanged{AggregateID: resource, Version: version, Scope: string(configurationScope(p.plan.Project).Details().Kind), ProjectID: p.plan.Project, ChangedFields: append([]string(nil), changed...)})
		if e != nil {
			return e
		}
		p.events = append(p.events, event)
		return nil
	}
	if e := add(p.plan.Resource, p.plan.Receipt.Version, p.plan.Changed); e != nil {
		return e
	}
	if before, after := p.plan.BeforeSelection, p.plan.AfterSelection; before != nil && after != nil {
		if p.plan.Kind != "model.selection.update" {
			if e := add(after.ID, after.Version, []string{"selection"}); e != nil {
				return e
			}
		}
		if before.Embedding != after.Embedding {
			h, e := newHeader(EmbeddingSelectionChangedEvent, SelectionAggregate, after.ID, after.Version, p.plan.At)
			if e != nil {
				return e
			}
			newModel, e := f.ParseID[mc.Model](after.Embedding)
			if e != nil {
				return unavailable(e)
			}
			payload := mc.EmbeddingSelectionChanged{AggregateID: after.ID, Version: after.Version, NewModelID: newModel}
			if before.Embedding != "" {
				old, e := f.ParseID[mc.Model](before.Embedding)
				if e != nil {
					return unavailable(e)
				}
				payload.OldModelID = &old
			}
			event, e := ec.NewEvent(s.state().deps.ConfigurationEvents.data().embedding, h, payload)
			if e != nil {
				return e
			}
			p.events = append(p.events, event)
		}
	}
	if after := p.plan.AfterMeetingSummary; after != nil && p.plan.Kind == "model.delete" {
		if e := add(after.ID, after.Version, []string{"selection"}); e != nil {
			return e
		}
	}
	for _, event := range p.events {
		p.plan.Events = append(p.plan.Events, persistedEvent{Header: event.Header(), Payload: event.PayloadBytes()})
	}
	return nil
}
func appendBinding(actor id.Actor, summary ec.Summary) (f.Digest, error) {
	if actor.Validate() != nil || summary.Producer != ModelProducer || summary.Header.Validate() != nil || summary.PayloadDigest.Validate() != nil {
		return "", fault(f.InvalidArgument)
	}
	raw, e := encoded(struct {
		Actor id.ActorDetails
		Event ec.Summary
	}{actor.Details(), summary})
	if e != nil {
		return "", e
	}
	return hash(raw), nil
}
func matchingEvent(p *preparedCommand, summary ec.Summary) bool {
	if summary.Producer != ModelProducer {
		return false
	}
	for _, event := range p.plan.Events {
		if validPersistedEvent(event) && reflect.DeepEqual(event.Header, summary.Header) {
			canonical, e := cursor.CanonicalJSON(event.Payload)
			return e == nil && hash(canonical) == summary.PayloadDigest
		}
	}
	return false
}
func (a *Authority) DiscoverAppend(ctx context.Context, actor id.Actor, summary ec.Summary) (oc.Dependencies, error) {
	p, e := a.contextPlan(ctx, actor)
	if e != nil {
		return oc.Dependencies{}, e
	}
	if !matchingEvent(p, summary) {
		return oc.Dependencies{}, fault(f.Forbidden)
	}
	binding, e := appendBinding(actor, summary)
	if e != nil {
		return oc.Dependencies{}, e
	}
	return oc.NewDependencies(a.state().eventIssuer, binding, p.locks, []byte(p.plan.CommandID))
}
func (a *Authority) ValidateAppendInTx(ctx context.Context, tx f.Tx, actor id.Actor, summary ec.Summary, d oc.Dependencies, stage oc.Stage) error {
	if !stage.Valid() {
		return fault(f.InvalidArgument)
	}
	p, e := a.contextPlan(ctx, actor)
	if e != nil {
		return e
	}
	binding, e := appendBinding(actor, summary)
	if e != nil || !d.Matches(a.state().eventIssuer, binding) || string(d.Opaque()) != p.plan.CommandID || !matchingEvent(p, summary) {
		return fault(f.Forbidden)
	}
	return a.validatePreparedFact(ctx, tx, p)
}
func (s *Service) prepareEffects(ctx context.Context, p *preparedCommand) error {
	if e := s.prepareSecret(ctx, p); e != nil {
		return e
	}
	for _, event := range p.events {
		// This is the existing Appender's catalog ownership check, before the
		// business transaction or any persistent command/configuration write.
		plan, e := s.state().deps.Events.PrepareAppend(ctx, p.actor, event)
		if e != nil {
			return portError(e)
		}
		if !reflect.DeepEqual(plan.Details().Event.Summary(), event.Summary()) {
			return unavailable(nil)
		}
		p.appendPlans = append(p.appendPlans, plan)
		p.locks = append(p.locks, plan.Locks()...)
	}
	locks, e := oc.NormalizeLocks(p.locks)
	if e != nil {
		return e
	}
	p.locks = locks
	return nil
}

// Keep persisted events typed and canonical when checking stored command facts.
func validPersistedEvent(v persistedEvent) bool {
	if v.Header.Validate() != nil {
		return false
	}
	switch v.Header.EventType {
	case ConfigurationChangedEvent:
		var p mc.ConfigurationChanged
		return json.Unmarshal(v.Payload, &p) == nil && p.Validate() == nil && v.Header.SchemaVersion == 1 && v.Header.AggregateType == ConfigurationAggregate && v.Header.AggregateSequence == nil && v.Header.AggregateVersion != nil && *v.Header.AggregateVersion == p.Version && v.Header.AggregateID.String() == p.AggregateID && (v.Header.Scope.Kind == ec.SystemScope && p.Scope == "system" && p.ProjectID == "" || v.Header.Scope.Kind == ec.ProjectScope && p.Scope == "project" && v.Header.Scope.ProjectID.String() == p.ProjectID)
	case EmbeddingSelectionChangedEvent:
		var p mc.EmbeddingSelectionChanged
		return json.Unmarshal(v.Payload, &p) == nil && p.Validate() == nil
	}
	return false
}

var _ oc.ProducerAuthority = (*Authority)(nil)
