package outbound

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type proxyRecord struct {
	settings ProxySettings
	ref      string
}

func proxyLock() foundation.LockKey { k, _ := foundation.SystemConfigLock("outbound-proxy"); return k }
func readProxy(ctx context.Context, x postgres.SQLExecutor) (proxyRecord, error) {
	var p proxyRecord
	var version int64
	var raw []byte
	if e := x.QueryRow(ctx, `SELECT version,mode,gateway,coalesce(credential_id::text,'') FROM agenteam_outbound.proxy_settings WHERE singleton=true`).Scan(&version, &p.settings.Mode, &raw, &p.ref); e != nil {
		return p, unavailable(e)
	}
	p.settings.Version = foundation.Version(version)
	if p.settings.Version.Validate() != nil || json.Unmarshal(raw, &p.settings.Gateway) != nil {
		return p, unavailable(nil)
	}
	if p.settings.Gateway != nil {
		p.settings.Gateway.CredentialPresent = p.ref != ""
	}
	action := "keep"
	if p.settings.Gateway == nil {
		action = "remove"
	}
	normalized, e := normalizeProxyUpdate(ProxyUpdate{Mode: p.settings.Mode, Gateway: p.settings.Gateway, CredentialAction: action})
	if e != nil || !sameProxyGateway(normalized.Gateway, p.settings.Gateway) || p.ref != "" && (p.settings.Gateway == nil || p.settings.Gateway.Username == "") || p.ref == "" && p.settings.Gateway != nil && p.settings.Gateway.Username != "" {
		return p, unavailable(e)
	}
	return p, nil
}
func (s *PolicyService) BindProxy(deps ProxyDependencies) error {
	if nilPort(deps.Processes) || deps.Secrets == nil || deps.Authority == nil || deps.CurrentMACKey == nil || deps.MAC == nil {
		return invalid()
	}
	st := s.state()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.available || st.proxyDependencies != nil {
		return invalid()
	}
	copy := deps
	st.proxyDependencies = &copy
	st.proxyAvailable = false
	return nil
}
func (s *PolicyService) publishProxy(p proxyRecord) error {
	st := s.state()
	st.mu.Lock()
	defer st.mu.Unlock()
	if p.settings.Version.Validate() != nil || p.settings.Version < st.proxy.version {
		st.proxyAvailable = false
		return unavailable(nil)
	}
	if p.settings.Version == st.proxy.version && (p.settings.Mode != st.proxy.mode || p.ref != st.proxy.ref || !sameProxyGateway(p.settings.Gateway, st.proxy.gateway)) {
		st.proxyAvailable = false
		return unavailable(nil)
	}
	var gateway *ProxyGateway
	if p.settings.Gateway != nil {
		g := *p.settings.Gateway
		gateway = &g
	}
	snapshot := proxySnapshot{version: p.settings.Version, ref: p.ref, mode: p.settings.Mode, gateway: gateway}
	if p.ref != "" {
		snapshot.credential = func(ctx context.Context, use func([]byte) error) error { return s.borrowProxyCredential(ctx, p, use) }
	}
	st.proxy = snapshot
	st.proxyAvailable = len(st.pending) == 0
	return nil
}
func (s *PolicyService) GetProxy(ctx context.Context, actor identity.Actor) (ProxySettings, error) {
	var out proxyRecord
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return out.settings, invalid()
	}
	user, _ := foundation.UserLock(actor.Details().UserID)
	cause, e := recoveryCause()
	if e != nil {
		return out.settings, e
	}
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: proxyLock(), Mode: foundation.Shared}, {Key: user, Mode: foundation.Shared}}); e != nil {
			return e
		}
		if e := s.authorize(ctx, tx, actor, identity.Read); e != nil {
			return e
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return e
		}
		out, e = readProxy(ctx, x)
		return e
	})
	if e = resultError(result); e != nil {
		return ProxySettings{}, e
	}
	return out.settings, nil
}
func proxySemantic(meta CommandMeta, input ProxyUpdate) []byte {
	b, _ := json.Marshal(struct {
		Version foundation.Version
		Mode    string
		Gateway *ProxyGateway
		Action  string
		Replace bool
	}{meta.ExpectedVersion, input.Mode, input.Gateway, input.CredentialAction, input.Password != nil})
	return b
}

type proxyMAC func(string) ([]byte, error)

func readProxyReceipt(ctx context.Context, x postgres.SQLExecutor, key string, mac proxyMAC) (ProxyUpdateResult, bool, error) {
	var out ProxyUpdateResult
	var kid, id string
	var got []byte
	var version int64
	var at time.Time
	e := x.QueryRow(ctx, `SELECT semantic_kid,semantic_mac,version,audit_id::text,created_at FROM agenteam_outbound.proxy_receipts WHERE command_digest=$1`, key).Scan(&kid, &got, &version, &id, &at)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, false, nil
	}
	if e != nil {
		return out, false, unavailable(e)
	}
	want, e := mac(kid)
	defer clear(want)
	if e != nil {
		return out, true, unavailable(e)
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return out, true, foundation.NewFault(foundation.IdempotencyKeyReused, foundation.NotStarted)
	}
	out.Version = foundation.Version(version)
	out.AuditID, e = foundation.ParseID[ac.Record](id)
	if e != nil {
		return out, true, unavailable(e)
	}
	out.CreatedAt, e = foundation.NewInstant(at)
	if e != nil || out.Version.Validate() != nil {
		return out, true, unavailable(e)
	}
	return out, true, nil
}
func (s *PolicyService) UpdateProxy(ctx context.Context, actor identity.Actor, meta CommandMeta, input ProxyUpdate) (ProxyUpdateResult, error) {
	var zero ProxyUpdateResult
	if actor.Validate() != nil || actor.Details().Kind != identity.Human || meta.Identity.Validate() != nil || meta.ExpectedVersion.Validate() != nil || meta.Identity.Namespace() != "outbound-proxy" || meta.Identity.Command() != "update" || !slices.Equal(meta.Identity.OwnerIDs(), []string{actor.Details().UserID}) {
		return zero, invalid()
	}
	if meta.HTTPTraceID != "" {
		if _, e := foundation.ParseID[struct{}](meta.HTTPTraceID); e != nil {
			return zero, invalid()
		}
	}
	input, e := normalizeProxyUpdate(input)
	if e != nil {
		return zero, e
	}
	st := s.state()
	deps := st.proxyDependencies
	if deps == nil {
		return zero, unavailable(nil)
	}
	semantic := proxySemantic(meta, input)
	mac := func(kid string) (out []byte, e error) {
		if input.Password != nil {
			e = input.Password.Use(func(b []byte) error { out, e = deps.MAC(kid, []byte(meta.Identity.Canonical()), semantic, b); return e })
			return
		}
		return deps.MAC(kid, []byte(meta.Identity.Canonical()), semantic, nil)
	}
	release, e := st.gate.acquire(ctx, true)
	if e != nil {
		return zero, unavailable(e)
	}
	defer release()
	key := digestBytes([]byte(meta.Identity.Canonical()))
	ck, _ := foundation.CommandLock(meta.Identity)
	user, _ := foundation.UserLock(actor.Details().UserID)
	locks := []foundation.LockRequest{{Key: ck, Mode: foundation.Exclusive}, {Key: proxyLock(), Mode: foundation.Exclusive}, {Key: user, Mode: foundation.Shared}}
	cause, _ := foundation.NewCommandsCause(meta.Identity)
	var before proxyRecord
	var out ProxyUpdateResult
	var found bool
	preflight := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return e
		}
		if e := s.authorize(ctx, tx, actor, identity.Mutate); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return e
		}
		out, found, e = readProxyReceipt(ctx, x, key, mac)
		if e != nil || found {
			return e
		}
		before, e = readProxy(ctx, x)
		return e
	})
	if e = resultError(preflight); e != nil {
		return zero, e
	}
	if e = s.reconcileProxyLeasesLocked(ctx); e != nil {
		s.unavailable()
		if found {
			return zero, committedRecoveryError(e)
		}
		return zero, e
	}
	if found {
		if s.recoveryRequired() {
			if e = s.recoverCurrentLocked(ctx); e != nil {
				return zero, committedRecoveryError(e)
			}
		}
		return out, nil
	}
	if before.settings.Version != meta.ExpectedVersion {
		return zero, foundation.NewFault(foundation.VersionConflict, foundation.NotStarted)
	}
	if before.settings.Version == foundation.Version(math.MaxInt64) {
		return zero, foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
	}
	if before.ref != "" && !sameProxyGateway(before.settings.Gateway, input.Gateway) && input.CredentialAction == "keep" && input.Password == nil {
		return zero, invalid()
	}
	next := proxyRecord{settings: ProxySettings{Version: before.settings.Version + 1, Mode: input.Mode, Gateway: input.Gateway}, ref: before.ref}
	if input.CredentialAction == "remove" {
		next.ref = ""
	}
	var prepared secret.PreparedWrite
	hasPrepared := false
	if input.Password != nil || before.ref != "" && next.ref == "" {
		kind := sc.Create
		var ref sc.CredentialRef
		var expected foundation.Version
		if before.ref != "" {
			cid, e := foundation.ParseID[sc.Credential](before.ref)
			if e != nil {
				return zero, unavailable(e)
			}
			ref, e = sc.NewCredentialRef(cid, identity.SystemScope())
			if e != nil {
				return zero, e
			}
			metadata, e := deps.Secrets.Metadata(ctx, actor, ref)
			if e != nil {
				return zero, e
			}
			expected = metadata.Version
			kind = sc.Update
			if next.ref == "" {
				kind = sc.Delete
			}
		}
		secretKey := foundation.IdempotencyKey(key[len("sha256:"):])
		identityKey, e := foundation.NewCommandIdentity("secret", []string{actor.Details().UserID}, string(kind), secretKey)
		if e != nil {
			return zero, e
		}
		request := sc.WriteRequest{Actor: actor, Scope: identity.SystemScope(), Identity: identityKey, Kind: kind, Ref: ref, ExpectedVersion: expected, Purpose: sc.System}
		if input.Password != nil {
			request.Value = *input.Password
		}
		prepared, e = deps.Secrets.PrepareWrite(ctx, request)
		if e != nil {
			return zero, e
		}
		hasPrepared = true
		locks = append(locks, prepared.RequiredLocks()...)
		cause, e = foundation.NewCommandsCause(meta.Identity, identityKey)
		if e != nil {
			return zero, e
		}
		if input.Password != nil {
			next.ref = prepared.Ref().Details().ID.String()
		}
	}
	if next.settings.Gateway != nil {
		next.settings.Gateway.CredentialPresent = next.ref != ""
		if (next.settings.Gateway.Username != "") != (next.ref != "") {
			return zero, invalid()
		}
	}
	witness := &proxyUse{authority: deps.Authority, version: next.settings.Version, previous: before.ref, next: next.ref, actor: actor}
	ctx = context.WithValue(ctx, proxyUseKey{}, witness)
	var requests []sc.UsageRequest
	var plans []sc.UsageDependencies
	if before.ref != next.ref {
		for _, item := range []struct {
			ref    string
			retain bool
		}{{next.ref, true}, {before.ref, false}} {
			if item.ref == "" {
				continue
			}
			id, e := foundation.ParseID[sc.Credential](item.ref)
			if e != nil {
				return zero, e
			}
			ref, e := sc.NewCredentialRef(id, identity.SystemScope())
			if e != nil {
				return zero, e
			}
			action := sc.ReleaseReferenceUsage
			if item.retain {
				action = sc.RetainReferenceUsage
			}
			r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.System, ReferenceOwner: proxySettingsID, Action: action, Retain: item.retain}
			p, e := deps.Secrets.DiscoverUsage(ctx, r)
			if e != nil {
				return zero, e
			}
			requests = append(requests, r)
			plans = append(plans, p)
			locks = append(locks, p.RequiredLocks()...)
		}
	}
	kid := deps.CurrentMACKey()
	fingerprint, e := mac(kid)
	if e != nil {
		return zero, e
	}
	defer clear(fingerprint)
	changed := false
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return e
		}
		if e := s.authorize(ctx, tx, actor, identity.Mutate); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return e
		}
		out, found, e = readProxyReceipt(ctx, x, key, mac)
		if e != nil || found {
			return e
		}
		current, e := readProxy(ctx, x)
		if e != nil {
			return e
		}
		if current.settings.Version != meta.ExpectedVersion || current.ref != before.ref {
			return foundation.NewFault(foundation.VersionConflict, foundation.NotStarted)
		}
		raw, e := json.Marshal(next.settings.Gateway)
		if e != nil {
			return e
		}
		// A new Secret is inserted before its FK mapping. Deleted references are
		// released after the new mapping, before the prepared Secret delete.
		deleting := hasPrepared && next.ref == ""
		if hasPrepared && !deleting {
			if _, e = deps.Secrets.ApplyPreparedWriteInTx(ctx, tx, prepared); e != nil {
				return e
			}
		}
		var ref any
		if next.ref != "" {
			ref = next.ref
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_outbound.proxy_settings SET version=$1,mode=$2,gateway=$3,credential_id=$4 WHERE singleton=true`, int64(next.settings.Version), next.settings.Mode, raw, ref); e != nil {
			return e
		}
		for i, r := range requests {
			if _, e = deps.Secrets.ApplyUsageInTx(ctx, tx, r, plans[i]); e != nil {
				return e
			}
		}
		if deleting {
			if _, e = deps.Secrets.ApplyPreparedWriteInTx(ctx, tx, prepared); e != nil {
				return e
			}
		}
		metadata, e := ac.ProxyMetadata(next.settings.Version)
		if e != nil {
			return e
		}
		resource, _ := ac.NewResource(ac.PolicyResource, "")
		entry, e := ac.NewEntry(ac.EntryFields{Scope: identity.SystemScope(), Actor: actor, Action: ac.ProxyUpdate, Outcome: ac.Success, Resource: resource, Metadata: metadata, Associations: ac.Associations{HTTPTraceID: meta.HTTPTraceID}})
		if e != nil {
			return e
		}
		appendKey, e := audit.CommandAppendKey(ac.PolicyProducer, meta.Identity, 0)
		if e != nil {
			return e
		}
		receipt, e := st.audit.AppendInTx(ctx, tx, entry, appendKey)
		if e != nil {
			return e
		}
		out = ProxyUpdateResult{next.settings.Version, receipt.AuditID, receipt.CreatedAt}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_outbound.proxy_receipts(command_digest,semantic_kid,semantic_mac,user_id,version,audit_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, key, kid, fingerprint, actor.Details().UserID, int64(out.Version), out.AuditID.String(), out.CreatedAt.Time()); e != nil {
			return e
		}
		changed = true
		return nil
	})
	if result.State() == foundation.Committed {
		if changed {
			if e = s.publishProxy(next); e != nil {
				return zero, committedRecoveryError(e)
			}
		}
		if s.commandPending(meta.Identity) {
			if e = s.recoverCurrentLocked(ctx); e != nil {
				return zero, committedRecoveryError(e)
			}
		}
		return out, nil
	}
	if result.State() == foundation.Unknown {
		s.recordPending(meta.Identity)
		f := foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
		f.RetryHint = "replay"
		return zero, f
	}
	return zero, resultError(result)
}
