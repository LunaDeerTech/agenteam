package object

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// Controlled live Store/Scan and planner facts exercise the real access-plan
// acquisition and issuing callback. A persisted grant INSERT and the complete
// Object -> Artifact -> Project Audit chain remain integration requirements.
type issuedAuditStore struct {
	*auditTestStore
	locks []f.LockRequest
}

func (s *issuedAuditStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.locks = append([]f.LockRequest(nil), locks...)
	return ctx.Err()
}

type issuedAuditProvider struct {
	oc.DownloadProvider
	append func(context.Context, f.Tx, i.Actor, oc.DownloadTarget, oc.DownloadEvent) error
}

func (p *issuedAuditProvider) AppendDownloadInTx(ctx context.Context, tx f.Tx, actor i.Actor, target oc.DownloadTarget, event oc.DownloadEvent, _ oc.AccessLockPlan, _ oc.LockedAccess) error {
	return p.append(ctx, tx, actor, target, event)
}

type issuedAuditFixture struct {
	store                 *issuedAuditStore
	objects               *Service
	downloads             *Downloads
	provider              *issuedAuditProvider
	actor                 i.Actor
	claims                downloadClaims
	plan                  oc.AccessLockPlan
	locked                oc.LockedAccess
	deps                  oc.AccessDependencies
	request               oc.AccessRequest
	raw, hash             []byte
	revoked, missing      bool
	rowErr, plannerErr    error
	grantReads, validates int
	beforeScan            func()
	saved                 context.Context
	event                 oc.DownloadEvent
}

func newIssuedAuditFixture(t *testing.T) *issuedAuditFixture {
	t.Helper()
	v := &issuedAuditFixture{claims: downloadVectorClaims(t)}
	var err error
	v.claims.expires, err = f.NewInstant(time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	v.actor, err = i.NewHuman(v.claims.user, auditID[i.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	v.raw, v.hash, err = grantWire(v.claims)
	if err != nil {
		t.Fatal(err)
	}
	v.store = &issuedAuditStore{auditTestStore: &auditTestStore{tx: f.NewTx()}}
	v.store.row = func(query string, args ...any) postgres.Row {
		if strings.Contains(query, "FROM agenteam_object.uploads WHERE object_id=$1") {
			if len(args) != 1 || args[0] != v.claims.target.Details().Source.Details().Meta.ID.String() {
				return auditValues() // Strict arity failure, never a successful row.
			}
			return auditTestRow(func(...any) error { return pgx.ErrNoRows })
		}
		return auditTestRow(func(out ...any) error {
			if !strings.Contains(query, "FROM agenteam_download.grants WHERE id=$1") || len(args) != 1 || args[0] != v.claims.grant.String() {
				return errors.New("unexpected issued Audit query")
			}
			v.grantReads++
			if v.beforeScan != nil {
				v.beforeScan()
			}
			if v.rowErr != nil {
				return v.rowErr
			}
			if v.missing {
				return pgx.ErrNoRows
			}
			return auditValues(v.raw, v.hash, v.claims.user.String(), v.claims.target.Details().Source.Details().Meta.ID.String(), v.claims.expires.Time(), v.revoked).Scan(out...)
		})
	}
	state := &serviceState{store: v.store, accessIssuer: oc.NewAccessIssuer(), accessTransactions: map[f.Tx]bool{}, metadataTransactions: map[f.Tx]bool{}, auth: Authorizations{Planner: v}}
	v.objects = &Service{data: func() *serviceState { return state }}
	v.request, err = oc.NewSourceAccess(oc.AccessRequestDetails{Operation: oc.ValidateSourceAccess, Actor: v.actor, Source: v.claims.target.Details().Source})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := v.objects.accessFacts(context.Background(), v.store, v.request)
	if err != nil {
		t.Fatal(err)
	}
	v.deps, err = oc.NewAccessDependencies(newDigest(make([]byte, 32)), facts.locks)
	if err != nil {
		t.Fatal(err)
	}
	v.plan, err = oc.NewAccessLockPlan(state.accessIssuer, oc.AccessPlanDetails{Request: v.request, DependencyRequest: v.request, Dependencies: v.deps, DomainBinding: facts.binding, Objects: facts.objects, Locks: facts.locks})
	if err != nil {
		t.Fatal(err)
	}
	v.locked, err = v.objects.AcquireAccessPlansInTx(context.Background(), v.store.tx, []oc.AccessLockPlan{v.plan}, []f.LockRequest{downloadGrantLock(v.claims.grant)})
	if err != nil {
		t.Fatal(err)
	}
	v.provider = &issuedAuditProvider{append: func(ctx context.Context, tx f.Tx, actor i.Actor, target oc.DownloadTarget, event oc.DownloadEvent) error {
		v.saved, v.event = ctx, event
		return CheckDownloadIssuedInTx(ctx, tx, v.store, actor, target, event)
	}}
	ds := downloadService{objects: v.objects, provider: v.provider}
	v.downloads = &Downloads{data: func() downloadService { return ds }}
	t.Cleanup(func() { v.store.tx = f.Tx{} })
	return v
}
func (v *issuedAuditFixture) Discover(context.Context, oc.AccessRequest) (oc.AccessDependencies, error) {
	return v.deps, nil
}
func (v *issuedAuditFixture) ValidateInTx(ctx context.Context, tx f.Tx, request oc.AccessRequest, deps oc.AccessDependencies) error {
	v.validates++
	if tx != v.store.tx || !request.Equal(v.request) || !deps.Equal(v.deps) || len(v.store.locks) == 0 {
		return failure(f.Forbidden, nil)
	}
	if v.plannerErr != nil {
		return v.plannerErr
	}
	return ctx.Err()
}
func (v *issuedAuditFixture) append(ctx context.Context) error {
	return v.downloads.appendIssuedAudit(ctx, v.store.tx, v.actor, v.claims, v.plan, v.locked)
}

func TestDownloadIssuedAuditBindings(t *testing.T) {
	for _, mode := range []string{"exact", "no-witness", "foreign-store", "foreign-tx", "foreign-actor", "changed-target", "changed-grant", "changed-length", "started", "missing-grant-lock", "not-acquired", "planner-denied", "missing-row", "changed-hash", "revoked", "row-error"} {
		t.Run(mode, func(t *testing.T) {
			v := newIssuedAuditFixture(t)
			originalErr := errors.New("original issued grant Scan failed")
			switch mode {
			case "missing-grant-lock":
				var err error
				v.locked, err = oc.NewLockedAccess(v.objects.state().accessIssuer, v.store.tx, []oc.AccessLockPlan{v.plan}, nil)
				if err != nil {
					t.Fatal(err)
				}
			case "not-acquired":
				delete(v.objects.state().accessTransactions, v.store.tx)
			case "planner-denied":
				v.plannerErr = failure(f.Forbidden, nil)
			case "missing-row":
				v.missing = true
			case "changed-hash":
				v.hash = append([]byte(nil), v.hash...)
				v.hash[0] ^= 1
			case "revoked":
				v.revoked = true
			case "row-error":
				v.rowErr = originalErr
			}
			v.provider.append = func(ctx context.Context, tx f.Tx, actor i.Actor, target oc.DownloadTarget, event oc.DownloadEvent) error {
				v.saved, v.event = ctx, event
				var store Store = v.store
				switch mode {
				case "no-witness":
					ctx = context.Background()
				case "foreign-store":
					store = &issuedAuditStore{auditTestStore: &auditTestStore{tx: tx}}
				case "foreign-tx":
					tx = f.NewTx()
				case "foreign-actor":
					actor, _ = i.NewHuman(auditID[i.User](t), auditID[i.Session](t))
				case "changed-target":
					d := target.Details()
					d.Filename = "different.txt"
					var err error
					target, err = oc.NewDownloadTarget(d)
					if err != nil {
						t.Fatal(err)
					}
				case "changed-grant":
					event.GrantID = auditID[oc.DownloadGrant](t)
				case "changed-length":
					event.Length++
				case "started":
					event.Phase, event.AttemptID = oc.DownloadStarted, auditID[oc.DownloadAttempt](t)
				}
				return CheckDownloadIssuedInTx(ctx, tx, store, actor, target, event)
			}
			err := v.append(context.Background())
			if mode == "exact" {
				if err != nil || v.validates != 1 || v.grantReads != 1 {
					t.Fatal("original plan and grant not consumed", err)
				}
			} else if err == nil {
				t.Fatal("changed issuing facts accepted")
			}
			if mode == "row-error" && !errors.Is(err, originalErr) {
				t.Fatal("original Scan error lost", err)
			}
			if (mode == "missing-grant-lock" || mode == "not-acquired" || mode == "planner-denied") && v.grantReads != 0 {
				t.Fatal("grant read before actual lock/plan permission")
			}
			w := v.saved.Value(downloadIssuedAuditKey{}).(*downloadIssuedAudit)
			if w.active.Load() {
				t.Fatal("issuing scope survived the original provider return")
			}
			before := v.store.queries
			if err := CheckDownloadIssuedInTx(v.saved, v.store.tx, v.store, v.actor, v.claims.target, v.event); err == nil || v.store.queries != before {
				t.Fatal("expired issuing scope read grants")
			}
		})
	}
}

func TestDownloadIssuedAuditOriginalReturns(t *testing.T) {
	for _, mode := range []string{"append", "grant-scan"} {
		t.Run(mode, func(t *testing.T) {
			v := newIssuedAuditFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			originalErr := errors.New("original provider Append error")
			if mode == "append" {
				v.provider.append = func(ctx context.Context, tx f.Tx, actor i.Actor, target oc.DownloadTarget, event oc.DownloadEvent) error {
					v.saved, v.event = ctx, event
					if err := CheckDownloadIssuedInTx(ctx, tx, v.store, actor, target, event); err != nil {
						return err
					}
					close(entered)
					<-release
					return originalErr
				}
			} else {
				v.beforeScan = func() { close(entered); <-release }
			}
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				unblock()
				if !joined {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("original issued callback did not return")
					}
				}
			})
			go func() { done <- v.append(ctx) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("original callback barrier not reached")
			}
			cancel()
			if !v.saved.Value(downloadIssuedAuditKey{}).(*downloadIssuedAudit).active.Load() {
				t.Fatal("cancellation retired the original issuing callback")
			}
			select {
			case err := <-done:
				joined = true
				t.Fatal("returned before original callback", err)
			default:
			}
			unblock()
			var err error
			select {
			case err = <-done:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("original callback did not return")
			}
			if mode == "append" && !errors.Is(err, originalErr) || mode == "grant-scan" && !errors.Is(err, context.Canceled) {
				t.Fatal("original outcome lost", err)
			}
			if v.saved.Value(downloadIssuedAuditKey{}).(*downloadIssuedAudit).active.Load() {
				t.Fatal("original returned callback still active")
			}
		})
	}
}
