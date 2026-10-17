package model

import (
	"context"
	"encoding/json"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model/managementwire"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func managementCurrentLocks(o managementOperation) []f.LockRequest {
	locks := []f.LockRequest{userLock(o.User), aggregateLock(f.ProviderAggregate, o.Provider, f.Shared)}
	if o.Model != "" {
		locks = append(locks, aggregateLock(f.ModelConfigAggregate, o.Model, f.Shared))
	}
	if o.Credential != "" {
		locks = append(locks, aggregateLock(f.CredentialRefAggregate, o.Credential, f.Shared))
	}
	return locks
}
func managementCurrent(ctx context.Context, x postgres.SQLExecutor, o managementOperation) (*providerRecord, *modelRecord, error) {
	p, e := loadProvider(ctx, x, o.Provider)
	if e != nil {
		return nil, nil, e
	}
	if p == nil {
		return nil, nil, fault(f.NotFound)
	}
	if p.Version != o.ProviderVersion || p.Input.CredentialID != o.Credential {
		return nil, nil, fault(f.VersionConflict)
	}
	if !p.Input.Enabled {
		return nil, nil, fault(f.InvalidState)
	}
	var model *modelRecord
	if o.Model != "" {
		model, e = loadModel(ctx, x, o.Model)
		if e != nil {
			return nil, nil, e
		}
		if model == nil {
			return nil, nil, fault(f.NotFound)
		}
		if model.Version != o.ModelVersion || model.ProviderID != p.ID {
			return nil, nil, fault(f.VersionConflict)
		}
		if !model.Input.Enabled {
			return nil, nil, fault(f.InvalidState)
		}
	}
	return p, model, nil
}

// Execute binds one administrative intent to one durable operation. No recovery
// path repeats external I/O; a stopped unfinished operation becomes interrupted.
func (m *Management) Execute(ctx context.Context, actor id.Actor, key f.IdempotencyKey, action, resource string, expected, providerVersion f.Version) (ManagementResult, error) {
	var empty ManagementResult
	if m == nil || ctx == nil {
		return empty, fault(f.DependencyUnbound)
	}
	if human(actor) != nil || expected.Validate() != nil || (action != "discovery" && action != "test") {
		return empty, fault(f.InvalidArgument)
	}
	if _, e := f.ParseID[struct{}](resource); e != nil {
		return empty, fault(f.InvalidArgument)
	}
	if action == "discovery" {
		providerVersion = expected
	} else if providerVersion.Validate() != nil {
		return empty, fault(f.InvalidArgument)
	}
	ci, e := f.NewCommandIdentity("model.management", []string{actor.Details().UserID}, action, key)
	if e != nil {
		return empty, fault(f.InvalidArgument)
	}
	sem, _ := json.Marshal(struct {
		Action, Resource   string
		Expected, Provider f.Version
	}{action, resource, expected, providerVersion})
	digest, semantic := hash([]byte(ci.Canonical())).String(), hash(sem).String()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	st := m.service.state()
	cause, e := readCause("management")
	if e != nil {
		return empty, e
	}
	var prior *managementOperation
	observed := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, []f.LockRequest{commandLock(ci), userLock(actor.Details().UserID)}); e != nil {
			return e
		}
		if e := st.authority.current(ctx, tx, actor, id.Mutate); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return e
		}
		prior, e = readManagement(ctx, x, "command_digest", digest)
		return e
	})
	if e = managementCommitError(observed); e != nil {
		return empty, e
	}
	if prior != nil {
		if prior.Semantic != semantic {
			return empty, fault(f.IdempotencyKeyReused)
		}
		if len(prior.Result) == 0 {
			if e = m.recoverOne(ctx, *prior); e != nil {
				return empty, managementUnknown()
			}
			prior, e = readManagement(ctx, st.store, "id", prior.ID)
			if e != nil {
				return empty, e
			}
		}
		if prior == nil || len(prior.Result) == 0 {
			return empty, managementUnknown()
		}
		if json.Unmarshal(prior.Result, &empty) != nil {
			return ManagementResult{}, unavailable(nil)
		}
		return empty, nil
	}
	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	default:
		return empty, fault(f.ResourceBusy)
	}
	opID, e := newID()
	if e != nil {
		return empty, e
	}
	process, e := m.process.CurrentManagementProcess()
	if e != nil {
		return empty, e
	}
	o := managementOperation{ID: opID, Command: digest, Semantic: semantic, User: actor.Details().UserID, Session: actor.Details().SessionID, Process: process, Action: action, ProviderVersion: providerVersion}
	if action == "discovery" {
		o.Provider = resource
	} else {
		mr, e := loadModel(ctx, st.store, resource)
		if e != nil {
			return empty, e
		}
		if mr == nil {
			return empty, fault(f.NotFound)
		}
		o.Model = resource
		o.ModelVersion = expected
		o.Provider = mr.ProviderID
	}
	p, e := loadProvider(ctx, st.store, o.Provider)
	if e != nil {
		return empty, e
	}
	if p == nil {
		return empty, fault(f.NotFound)
	}
	o.Credential = p.Input.CredentialID
	var req sc.UsageRequest
	var plan sc.UsageDependencies
	if o.Credential != "" {
		cid, _ := f.ParseID[sc.Credential](o.Credential)
		ref, _ := sc.NewCredentialRef(cid, id.SystemScope())
		meta, e := m.secrets.Metadata(ctx, actor, ref)
		if e != nil {
			return empty, e
		}
		if meta.Purpose != sc.Model {
			return empty, fault(f.Forbidden)
		}
		o.CredentialVersion = meta.Version
		o.Lease, e = newID()
		if e != nil {
			return empty, e
		}
	}
	o.CreatedAt, e = dbNow(ctx, st.store)
	if e != nil {
		return empty, e
	}
	if o.Credential != "" {
		ctx, req, e = m.authority.context(ctx, o)
		if e != nil {
			return empty, e
		}
		plan, e = m.secrets.DiscoverUsage(ctx, req)
		if e != nil {
			return empty, e
		}
	}
	locks := append(managementCurrentLocks(o), commandLock(ci), managementLock(o.ID, f.Exclusive))
	if o.Credential != "" {
		locks = append(locks, plan.RequiredLocks()...)
	}
	m.mu.Lock()
	m.active[o.ID] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.active, o.ID); m.mu.Unlock() }()
	claimed := false
	accepted := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return e
		}
		if e := st.authority.current(ctx, tx, actor, id.Mutate); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return e
		}
		existing, e := readManagement(ctx, x, "command_digest", digest)
		if e != nil {
			return e
		}
		if existing != nil {
			return managementUnknown()
		}
		if _, _, e = managementCurrent(ctx, x, o); e != nil {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_model.management_operations(id,command_digest,semantic_digest,user_id,session_id,process_id,action,provider_id,provider_version,model_id,model_version,credential_id,credential_version,lease_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, o.ID, o.Command, o.Semantic, o.User, o.Session, o.Process, o.Action, o.Provider, int64(o.ProviderVersion), null(o.Model), nullableVersion(o.ModelVersion), null(o.Credential), nullableVersion(o.CredentialVersion), null(o.Lease), o.CreatedAt.Time())
		if e != nil {
			return unavailable(e)
		}
		if o.Credential != "" {
			if _, e = m.secrets.ApplyUsageInTx(ctx, tx, req, plan); e != nil {
				return e
			}
		}
		claimed = true
		return nil
	})
	if e = managementCommitError(accepted); e != nil {
		return empty, e
	}
	if !claimed {
		return empty, managementUnknown()
	}
	result := o.result("credential_unavailable", 0)
	started := time.Now()
	var material *sc.SecretMaterial
	if o.Credential != "" {
		mat, readErr := m.secrets.ReadCredentialForRequest(ctx, req.Actor, req.LeaseID)
		if readErr != nil {
			return m.finish(ctx, o, result)
		}
		material = &mat
		defer mat.Destroy()
		req.Action = sc.ReadLeaseUsage
		plan, e = m.secrets.DiscoverUsage(ctx, req)
		if e != nil {
			return m.finish(ctx, o, result)
		}
	}
	netLocks := append(managementCurrentLocks(o), managementLock(o.ID, f.Exclusive))
	if material != nil {
		netLocks = append(netLocks, plan.RequiredLocks()...)
	}
	// Shared current-authority/configuration/credential locks remain held until
	// transport cancellation, socket close and Drain have actually returned.
	networkContext := ctx // retain cancellation but never pass a database transaction to outbound
	wireFinished := false
	network := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, netLocks); e != nil {
			return e
		}
		if e := st.authority.current(ctx, tx, actor, id.Mutate); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return e
		}
		provider, model, e := managementCurrent(ctx, x, o)
		if e != nil {
			return e
		}
		if material != nil {
			if e = m.secrets.ValidateManagementCredentialInTx(ctx, tx, req, plan, o.CredentialVersion); e != nil {
				return e
			}
		}
		key, e := ac.NewAppendKey(ac.AccessProducer, o.ID, 0)
		if e != nil {
			return e
		}
		call, e := outbound.NewCallContext(actor, id.SystemScope(), key, ac.Associations{})
		if e != nil {
			return e
		}
		protocol := provider.Input.Protocol
		if model != nil {
			protocol = protocol.EffectiveProtocol(model.Input.Type)
		} else if protocol == mc.OpenAICompatible {
			protocol = mc.OpenAIChat
		}
		input := managementwire.Request{Action: o.Action, Protocol: string(protocol), Endpoint: provider.Input.BaseURL}
		if model != nil {
			input.Model = model.Input.ProviderModelID
		}
		wire, e := m.transport.Run(networkContext, input, material, call)
		if e != nil {
			return e
		}
		wireFinished = true
		result = o.result(wire.Reason, time.Since(started))
		if result.Items != nil {
			for _, v := range wire.Items {
				*result.Items = append(*result.Items, ManagementItem{v})
			}
		}
		return nil
	})
	if e = managementCommitError(network); e != nil && !wireFinished {
		reason := "configuration_changed"
		if ctx.Err() != nil {
			reason = "cancelled"
			if ctx.Err() == context.DeadlineExceeded {
				reason = "timeout"
			}
		}
		result = o.result(reason, time.Since(started))
	}
	if material != nil {
		material.Destroy()
	}
	return m.finish(ctx, o, result)
}
func nullableVersion(v f.Version) any {
	if v == 0 {
		return nil
	}
	return int64(v)
}
