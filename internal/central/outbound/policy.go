package outbound

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type Store interface {
	postgres.SQLExecutor
	InTx(foundation.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, foundation.TransactionCause, func(context.Context, foundation.Tx) error) foundation.CommitResult
	AcquireAll(context.Context, foundation.Tx, []foundation.LockRequest) error
}
type Authorizations struct {
	Sessions identity.SessionAuthority
	System   identity.SystemAuthority
}
type policyData struct {
	version foundation.Version
	mode    RuleMode
	rules   Rules
}
type Policy struct{ data func() policyData }

func policy(version foundation.Version, mode RuleMode, rules Rules) Policy {
	d := policyData{version, mode, rules}
	return Policy{data: func() policyData { return d }}
}
func (p Policy) Version() foundation.Version {
	if p.data == nil {
		return 0
	}
	return p.data().version
}
func (p Policy) Mode() RuleMode {
	if p.data == nil {
		return ""
	}
	return p.data().mode
}
func (p Policy) Rules() Rules {
	if p.data == nil {
		return Rules{}
	}
	return p.data().rules
}
func (p Policy) JSON() []byte {
	b, _ := json.Marshal(struct {
		Version foundation.Version `json:"version"`
		Mode    RuleMode           `json:"rule_mode"`
		Rules   json.RawMessage    `json:"rules"`
	}{p.Version(), p.Mode(), p.Rules().JSON()})
	return b
}
func (p Policy) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "outbound_policy") }
func (p Policy) MarshalJSON() ([]byte, error) { return []byte(`"outbound_policy"`), nil }
func (p Policy) LogValue() slog.Value         { return slog.StringValue("outbound_policy") }

type CommandMeta struct {
	Identity        foundation.CommandIdentity
	ExpectedVersion foundation.Version
	HTTPTraceID     string
}
type UpdateResult struct {
	Version   foundation.Version  `json:"version"`
	RuleCount foundation.Progress `json:"rule_count"`
	AuditID   ac.ID               `json:"audit_id"`
	CreatedAt foundation.Instant  `json:"created_at"`
}
type PolicyStatus struct {
	Available bool                `json:"available"`
	Version   *foundation.Version `json:"version,omitempty"`
}
type policyState struct {
	store             Store
	audit             ac.Appender
	auth              Authorizations
	gate              sendGate
	mu                sync.Mutex
	mirror            Policy
	available         bool
	proxy             proxySnapshot
	proxyAvailable    bool
	proxyDependencies *ProxyDependencies
	pending           map[string]foundation.CommandIdentity
}
type PolicyService struct{ data func() *policyState }

func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func NewPolicyService(store Store, appender ac.Appender, auth Authorizations) (*PolicyService, error) {
	if nilPort(store) || nilPort(appender) {
		return nil, invalid()
	}
	s := &policyState{store: store, audit: appender, auth: auth, proxy: proxySnapshot{version: 1, mode: "direct"}, proxyAvailable: true}
	return &PolicyService{data: func() *policyState { return s }}, nil
}
func (s *PolicyService) state() *policyState { return s.data() }
func (s *PolicyService) Status() PolicyStatus {
	d := s.state()
	d.mu.Lock()
	defer d.mu.Unlock()
	out := PolicyStatus{Available: d.available && d.proxyAvailable && len(d.pending) == 0}
	if d.mirror.Version() > 0 {
		v := d.mirror.Version()
		out.Version = &v
	}
	return out
}
func (s *PolicyService) snapshot() (Policy, error) {
	d := s.state()
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.available || !d.proxyAvailable || len(d.pending) != 0 {
		return Policy{}, unavailable(nil)
	}
	return d.mirror, nil
}
func (s *PolicyService) unavailable() {
	d := s.state()
	d.mu.Lock()
	d.available = false
	if d.proxyDependencies != nil {
		d.proxyAvailable = false
	}
	d.mu.Unlock()
}

// publish is called only with the exclusive send gate. A stale result cannot
// decrease a previously observed version, including after an unavailable period.
func (s *PolicyService) publish(p Policy) error {
	d := s.state()
	d.mu.Lock()
	defer d.mu.Unlock()
	if p.Version().Validate() != nil || !p.Mode().Valid() || !p.Rules().Valid() || p.Version() < d.mirror.Version() {
		d.available = false
		return unavailable(nil)
	}
	if p.Version() == d.mirror.Version() && (p.Mode() != d.mirror.Mode() || string(p.Rules().JSON()) != string(d.mirror.Rules().JSON())) {
		d.available = false
		return unavailable(nil)
	}
	d.mirror = p
	d.available = len(d.pending) == 0
	return nil
}
func policyLock() foundation.LockKey {
	k, _ := foundation.SystemConfigLock("outbound-policy")
	return k
}
func readPolicy(ctx context.Context, e postgres.SQLExecutor) (Policy, error) {
	var version int64
	var raw []byte
	var mode string
	if err := e.QueryRow(ctx, `SELECT version,rule_mode,rules FROM agenteam_outbound.outbound_policy WHERE singleton=true`).Scan(&version, &mode, &raw); err != nil {
		return Policy{}, unavailable(err)
	}
	// PostgreSQL jsonb output adds formatting whitespace. The public byte limit
	// applies to the serialized request/canonical DTO, not server formatting.
	var compact bytes.Buffer
	if len(raw) > 2<<20 || json.Compact(&compact, raw) != nil {
		return Policy{}, unavailable(nil)
	}
	rules, err := DecodeRules(compact.Bytes())
	if err != nil || foundation.Version(version).Validate() != nil || !RuleMode(mode).Valid() {
		return Policy{}, unavailable(err)
	}
	return policy(foundation.Version(version), RuleMode(mode), rules), nil
}

// Reload is an explicit startup/recovery operation, never a TTL/polling cache.
// The authoritative current row is reloaded under the same exclusive send gate
// used by commands; a historical receipt is never a substitute for this read.
func (s *PolicyService) Reload(ctx context.Context) error {
	release, err := s.state().gate.acquire(ctx, true)
	if err != nil {
		return unavailable(err)
	}
	defer release()
	return s.recoverCurrentLocked(ctx)
}

func recoveryCause() (foundation.TransactionCause, error) {
	id, err := foundation.NewID[struct{}]()
	if err != nil {
		return foundation.TransactionCause{}, unavailable(err)
	}
	return foundation.NewRecoveryCause("outbound-policy", id.String(), "")
}
func (s *PolicyService) authorize(ctx context.Context, tx foundation.Tx, actor identity.Actor, intent identity.AccessIntent) error {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return failure(foundation.Forbidden, ac.PermissionDenied, nil)
	}
	auth := s.state().auth
	if nilPort(auth.Sessions) || nilPort(auth.System) {
		return failure(foundation.DependencyUnbound, ac.PermissionDenied, nil)
	}
	if err := auth.Sessions.RequireCurrentSession(ctx, tx, actor); err != nil {
		return authorizationError(err)
	}
	g, err := auth.System.AuthorizeSystem(ctx, tx, actor, intent)
	if err != nil {
		return authorizationError(err)
	}
	if !g.Matches(actor, identity.SystemScope(), intent) {
		return failure(foundation.Forbidden, ac.PermissionDenied, nil)
	}
	return nil
}
func authorizationError(err error) error {
	var f *foundation.Fault
	if errors.As(err, &f) {
		switch f.Code {
		case foundation.Unauthenticated, foundation.SessionRevoked, foundation.Forbidden, foundation.NotFound, foundation.DependencyUnbound:
			return failure(f.Code, ac.PermissionDenied, err)
		}
	}
	return unavailable(err)
}
func resultError(r foundation.CommitResult) error {
	if r.State() == foundation.Committed {
		return nil
	}
	if r.State() == foundation.Unknown {
		return failure(foundation.CommitUnknown, ac.PolicyUnavailable, nil)
	}
	if f := r.Fault(); f != nil {
		return f
	}
	return unavailable(nil)
}
func (s *PolicyService) GetPolicy(ctx context.Context, actor identity.Actor) (Policy, error) {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return Policy{}, invalid()
	}
	userKey, _ := foundation.UserLock(actor.Details().UserID)
	cause, err := recoveryCause()
	if err != nil {
		return Policy{}, err
	}
	var out Policy
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: policyLock(), Mode: foundation.Shared}, {Key: userKey, Mode: foundation.Shared}}); err != nil {
			return err
		}
		if err := s.authorize(ctx, tx, actor, identity.Read); err != nil {
			return err
		}
		e, err := s.state().store.InTx(tx)
		if err != nil {
			return err
		}
		out, err = readPolicy(ctx, e)
		if err != nil {
			s.unavailable()
		}
		return err
	})
	if result.State() != foundation.Committed {
		return Policy{}, resultError(result)
	}
	return out, nil
}
func validateCommand(actor identity.Actor, meta CommandMeta, mode RuleMode, rules Rules) error {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human || meta.Identity.Validate() != nil || meta.ExpectedVersion.Validate() != nil || !mode.Valid() || !rules.Valid() {
		return invalid()
	}
	if meta.Identity.Namespace() != "outbound-policy-v2" || meta.Identity.Command() != "update" || !slices.Equal(meta.Identity.OwnerIDs(), []string{actor.Details().UserID}) {
		return invalid()
	}
	if meta.HTTPTraceID != "" {
		if _, err := foundation.ParseID[struct{}](meta.HTTPTraceID); err != nil {
			return invalid()
		}
	}
	return nil
}
func digestBytes(raw []byte) string {
	h := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(h[:])
}
func commandDigest(actor identity.Actor, meta CommandMeta, mode RuleMode, rules Rules) string {
	raw, _ := json.Marshal(struct {
		Format   int                `json:"format"`
		User     string             `json:"user_id"`
		Expected foundation.Version `json:"expected_version"`
		Mode     RuleMode           `json:"rule_mode"`
		Rules    json.RawMessage    `json:"rules"`
	}{2, actor.Details().UserID, meta.ExpectedVersion, mode, rules.JSON()})
	return digestBytes(raw)
}
func readReceipt(ctx context.Context, e postgres.SQLExecutor, key, digest string) (UpdateResult, bool, error) {
	var found string
	var version, count int64
	var id string
	var at time.Time
	err := e.QueryRow(ctx, `SELECT semantic_digest,version,rule_count,audit_id::text,created_at FROM agenteam_outbound.outbound_policy_receipts WHERE command_digest=$1`, key).Scan(&found, &version, &count, &id, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateResult{}, false, nil
	}
	if err != nil {
		return UpdateResult{}, false, unavailable(err)
	}
	if subtle.ConstantTimeCompare([]byte(found), []byte(digest)) != 1 {
		return UpdateResult{}, true, failure(foundation.IdempotencyKeyReused, ac.InvalidTarget, nil)
	}
	auditID, err := foundation.ParseID[ac.Record](id)
	if err != nil || version <= 0 || count < 0 || count > 512 {
		return UpdateResult{}, true, unavailable(err)
	}
	created, err := foundation.NewInstant(at)
	if err != nil {
		return UpdateResult{}, true, unavailable(err)
	}
	return UpdateResult{foundation.Version(version), foundation.Progress(count), auditID, created}, true, nil
}
func (s *PolicyService) UpdatePolicy(ctx context.Context, actor identity.Actor, meta CommandMeta, mode RuleMode, rules Rules) (UpdateResult, error) {
	if err := validateCommand(actor, meta, mode, rules); err != nil {
		return UpdateResult{}, err
	}
	release, err := s.state().gate.acquire(ctx, true)
	if err != nil {
		return UpdateResult{}, unavailable(err)
	}
	defer release()
	cause, _ := foundation.NewCommandsCause(meta.Identity)
	ck, _ := foundation.CommandLock(meta.Identity)
	userKey, _ := foundation.UserLock(actor.Details().UserID)
	key := digestBytes([]byte(meta.Identity.Canonical()))
	digest := commandDigest(actor, meta, mode, rules)
	var out UpdateResult
	var next Policy
	var changed bool
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: ck, Mode: foundation.Exclusive}, {Key: policyLock(), Mode: foundation.Exclusive}, {Key: userKey, Mode: foundation.Shared}}); err != nil {
			return err
		}
		if err := s.authorize(ctx, tx, actor, identity.Mutate); err != nil {
			return err
		}
		e, err := s.state().store.InTx(tx)
		if err != nil {
			return err
		}
		var found bool
		out, found, err = readReceipt(ctx, e, key, digest)
		if err != nil {
			if !found {
				s.unavailable()
			}
			return err
		}
		if found {
			return nil
		}
		current, err := readPolicy(ctx, e)
		if err != nil {
			s.unavailable()
			return err
		}
		if current.Version() != meta.ExpectedVersion {
			return failure(foundation.VersionConflict, ac.InvalidTarget, nil)
		}
		if current.Version() == foundation.Version(math.MaxInt64) {
			return failure(foundation.InvalidState, ac.PolicyUnavailable, nil)
		}
		next = policy(current.Version()+1, mode, rules)
		if _, err = e.Exec(ctx, `UPDATE agenteam_outbound.outbound_policy SET version=$1,rule_mode=$2,rules=$3 WHERE singleton=true`, int64(next.Version()), string(mode), rules.JSON()); err != nil {
			return err
		}
		metadata, _ := ac.PolicyMetadata(next.Version(), rules.Count())
		resource, _ := ac.NewResource(ac.PolicyResource, "")
		entry, err := ac.NewEntry(ac.EntryFields{Scope: identity.SystemScope(), Actor: actor, Action: ac.PolicyUpdate, Outcome: ac.Success, Resource: resource, Metadata: metadata, Associations: ac.Associations{HTTPTraceID: meta.HTTPTraceID}})
		if err != nil {
			return err
		}
		appendKey, err := audit.CommandAppendKey(ac.PolicyProducer, meta.Identity, 0)
		if err != nil {
			return err
		}
		receipt, err := s.state().audit.AppendInTx(ctx, tx, entry, appendKey)
		if err != nil {
			return err
		}
		out = UpdateResult{next.Version(), foundation.Progress(rules.Count()), receipt.AuditID, receipt.CreatedAt}
		if _, err = e.Exec(ctx, `INSERT INTO agenteam_outbound.outbound_policy_receipts(command_digest,semantic_digest,user_id,version,rule_count,audit_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, key, digest, actor.Details().UserID, int64(out.Version), int64(out.RuleCount), out.AuditID.String(), out.CreatedAt.Time()); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if result.State() == foundation.Committed {
		pending := s.commandPending(meta.Identity)
		if changed {
			if err := s.publish(next); err != nil {
				return UpdateResult{}, committedRecoveryError(err)
			}
		}
		// A normal save cannot resolve another command's Unknown. A replay,
		// or this pending command's fresh committed retry, can reconcile the
		// complete current state while retaining its historical receipt.
		if (!changed || pending) && s.recoveryRequired() {
			if err := s.recoverAfterPolicyCommandLocked(ctx); err != nil {
				return UpdateResult{}, committedRecoveryError(err)
			}
		}
		return out, nil
	}
	if result.State() != foundation.Unknown {
		return UpdateResult{}, resultError(result)
	}
	s.recordPending(meta.Identity)
	// COMMIT has already been attempted. Never turn a missing receipt or a
	// failed verification into "not committed"; retain the original cause.
	original := result.Cause()
	var verifiedFound bool
	verified := s.state().store.WithinTx(ctx, original, func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: ck, Mode: foundation.Shared}, {Key: policyLock(), Mode: foundation.Shared}, {Key: userKey, Mode: foundation.Shared}}); err != nil {
			return err
		}
		if err := s.authorize(ctx, tx, actor, identity.Mutate); err != nil {
			return err
		}
		e, err := s.state().store.InTx(tx)
		if err != nil {
			return err
		}
		out, verifiedFound, err = readReceipt(ctx, e, key, digest)
		return err
	})
	if verified.State() == foundation.Committed {
		if err := s.recoverAfterPolicyCommandLocked(ctx); err == nil {
			if verifiedFound {
				return out, nil
			}
		} else if verifiedFound {
			return UpdateResult{}, committedRecoveryError(err)
		}
	}
	return UpdateResult{}, failure(foundation.CommitUnknown, ac.PolicyUnavailable, unknownCause{cause: func() foundation.TransactionCause { return original }})
}

// The explicit accessor permits an owner to reconcile with the original cause
// without making the command identity or key part of any implicit formatting.
type unknownCause struct {
	cause func() foundation.TransactionCause
}

func (e unknownCause) Error() string                                 { return string(foundation.CommitUnknown) }
func (e unknownCause) TransactionCause() foundation.TransactionCause { return e.cause() }
