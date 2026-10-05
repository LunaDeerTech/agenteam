package model

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sort"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Durable DTOs contain scalar facts, not an issuer, an Actor capability or a
// serialized public plan. In-memory plans separately bind the entire Actor.
type resolutionRequestDTO struct {
	Format          int `json:"format_version"`
	Actor           id.ActorDetails
	Consumer        mc.Consumer
	Purpose         mc.Purpose
	Source          mc.ResolutionSource
	ModelRef        *mc.ModelID
	Selection       *mc.SelectionRef
	ReasoningEffort string
	Owner           sc.OwnerDetails
}

func resolutionRequest(r mc.ResolveRequest) resolutionRequestDTO {
	a := r.Actor.Details()
	if a.Kind == id.Human {
		a.SessionID = ""
	}
	return resolutionRequestDTO{1, a, r.Consumer.Clone(), r.Purpose, r.Source, r.Clone().ModelRef, r.Clone().Selection, r.ReasoningEffort, r.LeaseOwner.Details()}
}
func resolutionSemantic(r mc.ResolveRequest) f.Digest {
	raw, _ := encoded(resolutionRequest(r))
	return hash(raw)
}
func resolutionIdentity(r mc.ResolveRequest) (f.CommandIdentity, error) {
	return resolutionUnitIdentity(r.Consumer, r.Purpose, r.LeaseOwner.Details())
}
func resolutionUnitIdentity(consumer mc.Consumer, purpose mc.Purpose, o sc.OwnerDetails) (f.CommandIdentity, error) {
	key := "current-v1"
	if o.Kind == sc.ExecutionOwner {
		resource := consumer.OperationID
		if consumer.ExecutionID != nil && resource == "" {
			resource = consumer.ExecutionID.String()
		}
		v, _ := encoded(struct {
			Kind     mc.ConsumerKind
			Purpose  mc.Purpose
			Resource string
		}{consumer.Kind, purpose, resource})
		key = hash(v).String()
	}
	return f.NewCommandIdentity("model.resolve", []string{consumer.ProjectID.String(), o.ID}, "current."+string(o.Kind), f.IdempotencyKey(key))
}

// Validate durable scalar identity without reconstructing an Actor or minting a
// ServiceRegistration. Current authority always comes from the original caller.
func (d resolutionRequestDTO) validate() error {
	if d.Format != 1 || d.Consumer.Validate() != nil || d.Purpose != d.Consumer.Purpose || d.Source != mc.CurrentSelectionSource || d.Selection == nil || (mc.SelectionRequest{Consumer: d.Consumer, ModelRef: d.ModelRef, Selection: *d.Selection}).Validate() != nil || d.ReasoningEffort != "" {
		return unavailable(nil)
	}
	owner, e := sc.NewCredentialLeaseOwner(d.Owner.Kind, d.Owner.ID)
	if e != nil || owner.Details().Kind == sc.ExecutionOwner && (d.Consumer.ExecutionID == nil || d.Consumer.ExecutionID.String() != d.Owner.ID) {
		return unavailable(e)
	}
	uuid := func(v string) bool { _, e := f.ParseID[struct{}](v); return e == nil }
	a := d.Actor
	switch a.Kind {
	case id.Human:
		if !uuid(a.UserID) || a.SessionID != "" || a.ProjectID != "" || a.AgentID != "" || a.ExecutionID != "" || a.ServiceName != "" || a.CauseRef != "" {
			return unavailable(nil)
		}
	case id.AgentRun:
		if a.UserID != "" || a.SessionID != "" || a.ServiceName != "" || a.CauseRef != "" || d.Consumer.AgentID == nil || d.Consumer.ExecutionID == nil || a.ProjectID != d.Consumer.ProjectID.String() || a.AgentID != d.Consumer.AgentID.String() || a.ExecutionID != d.Consumer.ExecutionID.String() {
			return unavailable(nil)
		}
	case id.Service:
		if a.UserID != "" || a.SessionID != "" || a.AgentID != "" || a.ExecutionID != "" || a.ProjectID != "" && a.ProjectID != d.Consumer.ProjectID.String() || !id.ValidCauseRef(a.CauseRef) {
			return unavailable(nil)
		}
		switch a.ServiceName {
		case id.SecretService, id.SecretMaintenance, id.OutboundService, id.ObjectService, id.ObjectMaintenance, id.ProjectLifecycle, id.ProjectInitialization, id.OutboxDelivery, id.AccountBootstrap, id.AccountAuth, id.AccountMaintenance, id.AccountMail:
		default:
			return unavailable(nil)
		}
	default:
		return unavailable(nil)
	}
	return nil
}

type resolutionSnapshotDTO struct {
	Format                          int `json:"format_version"`
	ID                              mc.SnapshotID
	ProjectID                       id.ProjectID
	Identity                        mc.ModelIdentity
	Endpoint                        string
	Parameters, RequestOverwrite    json.RawMessage
	HeaderOverwrite                 map[string]string
	Capabilities                    mc.Capabilities
	CredentialID, CredentialProject string
	SelectionVersion                *f.Version
}

func (d resolutionSnapshotDTO) snapshot() (mc.ConfigSnapshot, error) {
	s := mc.ConfigSnapshot{ID: d.ID, Identity: d.Identity, Endpoint: d.Endpoint, Parameters: bytes.Clone(d.Parameters), RequestOverwrite: bytes.Clone(d.RequestOverwrite), HeaderOverwrite: d.HeaderOverwrite, Capabilities: d.Capabilities.Clone(), SelectionVersion: d.SelectionVersion}
	if d.Format != 1 || d.ProjectID.Validate() != nil || d.CredentialID == "" && d.CredentialProject != "" {
		return s, unavailable(nil)
	}
	if d.CredentialID != "" {
		cid, e := f.ParseID[sc.Credential](d.CredentialID)
		if e != nil {
			return s, unavailable(e)
		}
		ref, e := sc.NewCredentialRef(cid, configurationScope(d.CredentialProject))
		if e != nil {
			return s, unavailable(e)
		}
		if d.CredentialProject != "" && d.CredentialProject != d.ProjectID.String() {
			return s, unavailable(nil)
		}
		s.CredentialRef = &ref
	}
	if s.Validate() != nil {
		return s, unavailable(nil)
	}
	return s.Clone(), nil
}

type resolutionDraft struct {
	Format                        int `json:"format_version"`
	Snapshot                      resolutionSnapshotDTO
	ProviderVersion, ModelVersion f.Version
	LeaseID                       string
}

func (d resolutionDraft) validate() error {
	s, e := d.Snapshot.snapshot()
	if e != nil {
		return e
	}
	if d.Format != 1 || d.ProviderVersion.Validate() != nil || d.ModelVersion.Validate() != nil || (d.LeaseID == "") != (s.CredentialRef == nil) {
		return unavailable(nil)
	}
	if d.LeaseID != "" {
		if _, e = f.ParseID[sc.Lease](d.LeaseID); e != nil {
			return unavailable(e)
		}
	}
	return nil
}
func resolutionEqual(a, b any) bool { x, _ := encoded(a); y, _ := encoded(b); return bytes.Equal(x, y) }
func resolutionDecode(raw []byte, max int, target any) error {
	if len(raw) == 0 || len(raw) > max {
		return unavailable(nil)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(target); e != nil {
		return unavailable(e)
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return unavailable(e)
	}
	return nil
}
func resolutionUnion(groups ...[]f.LockRequest) ([]f.LockRequest, error) {
	m := map[string]f.LockRequest{}
	for _, g := range groups {
		for _, r := range g {
			if r.Key.Validate() != nil || r.Mode != f.Shared && r.Mode != f.Exclusive {
				return nil, fault(f.InvalidArgument)
			}
			k := r.Key.Canonical()
			if old, ok := m[k]; !ok || old.Mode == f.Shared {
				m[k] = r
			}
		}
	}
	out := make([]f.LockRequest, 0, len(m))
	for _, r := range m {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return f.CompareLockKeys(out[i].Key, out[j].Key) < 0 })
	return out, nil
}
func resolutionBaseLocks(r mc.ResolveRequest, identity f.CommandIdentity, snapshot mc.SnapshotID, draft resolutionDraft) []f.LockRequest {
	locks := []f.LockRequest{commandLock(identity), systemLock("model-references", f.Shared), projectLock(r.Consumer.ProjectID.String()), recordLock(f.ReferenceRecordLock, "model-resolution:"+hash([]byte(identity.Canonical())).String()), recordLock(f.ReferenceRecordLock, "model-snapshot:"+snapshot.String())}
	if r.Actor.Details().Kind == id.Human {
		locks = append(locks, userLock(r.Actor.Details().UserID))
	}
	if r.Selection != nil && r.Selection.Kind == "platform" {
		locks = append(locks, systemLock("model-platform-selection", f.Shared))
	}
	if r.ModelRef != nil {
		locks = append(locks, aggregateLock(f.ModelConfigAggregate, r.ModelRef.String(), f.Shared))
	}
	if draft.Snapshot.Identity.ProviderID.Validate() == nil {
		locks = append(locks, aggregateLock(f.ProviderAggregate, draft.Snapshot.Identity.ProviderID.String(), f.Shared))
	}
	if draft.Snapshot.Identity.ModelID.Validate() == nil {
		locks = append(locks, aggregateLock(f.ModelConfigAggregate, draft.Snapshot.Identity.ModelID.String(), f.Shared))
	}
	if draft.Snapshot.CredentialID != "" {
		raw, _ := encoded(struct {
			Ref, Project string
			Owner        sc.OwnerDetails
		}{draft.Snapshot.CredentialID, draft.Snapshot.CredentialProject, r.LeaseOwner.Details()})
		locks = append(locks, recordLock(f.ReferenceRecordLock, "model-resolution-owner:"+hash(raw).String()))
	}
	return locks
}
func resolutionConsumerRequest(r mc.ResolveRequest, snapshot mc.SnapshotID, lease string) (mc.ConsumerRequest, error) {
	copy := r.Clone()
	q := mc.ConsumerRequest{Action: mc.ResolveConsumer, Actor: r.Actor, Consumer: r.Consumer.Clone(), Resolve: &copy, SnapshotID: snapshot, LeaseOwner: r.LeaseOwner}
	if r.LeaseOwner.Details().Kind == sc.ModelCallOwner {
		call, e := f.ParseID[mc.Call](r.LeaseOwner.Details().ID)
		if e != nil {
			return q, e
		}
		q.CallID = &call
	}
	if lease != "" {
		value, e := f.ParseID[sc.Lease](lease)
		if e != nil {
			return q, e
		}
		q.LeaseID = &value
	}
	return q, q.Validate()
}

type resolutionCandidate struct {
	authority       *Authority
	request         mc.ResolveRequest
	identity        f.CommandIdentity
	row             *resolutionPreparation
	draft           resolutionDraft
	version         f.Version
	consumerRequest mc.ConsumerRequest
	consumer        mc.ConsumerDependencies
	locks           []f.LockRequest
	usage           sc.UsageRequest
	secret          *sc.UsageDependencies
}

func (c *resolutionCandidate) mapping() f.Digest {
	raw, _ := encoded(struct {
		Format   int
		Identity string
		Semantic f.Digest
		Version  f.Version
		Draft    resolutionDraft
		Consumer f.Digest
	}{1, c.identity.Canonical(), resolutionSemantic(c.request), c.version, c.draft, c.consumer.Details().Mapping})
	return hash(raw)
}
func resolutionContextError(ctx context.Context) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	return ctx.Err()
}
