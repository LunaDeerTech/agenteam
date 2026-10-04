//go:build integration

package account_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type releaseFaultAuthority struct {
	*account.Authority
	f       *b02Fixture
	mode    string
	enabled atomic.Bool
	deleted atomic.Int64
}

func (p *releaseFaultAuthority) DiscoverUsage(ctx context.Context, q sc.UsageRequest) (sc.UsageDependencies, error) {
	if p.enabled.Load() && q.Action == sc.ReleaseLeaseUsage {
		switch p.mode {
		case "nested_not_found":
			// This is a genuine Secret missing-lease error, from another exact
			// ID. The outer Secret service must wrap the provider's rejection;
			// it is not evidence that the requested original lease is absent.
			missing := q
			var e error
			missing.LeaseID, e = foundation.NewID[sc.Lease]()
			if e != nil {
				return sc.UsageDependencies{}, e
			}
			_, e = p.f.secrets.DiscoverUsage(ctx, missing)
			if se, ok := e.(*secret.Error); !ok || se.Code() != secret.NotFound {
				return sc.UsageDependencies{}, foundation.NewFault(foundation.InternalError, foundation.NotStarted)
			}
			return sc.UsageDependencies{}, e
		case "dependency":
			return sc.UsageDependencies{}, foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
		}
	}
	return p.Authority.DiscoverUsage(ctx, q)
}
func (p *releaseFaultAuthority) ValidateUsageInTx(ctx context.Context, tx foundation.Tx, q sc.UsageRequest, d sc.UsageDependencies) error {
	if e := p.Authority.ValidateUsageInTx(ctx, tx, q, d); e != nil {
		return e
	}
	if p.enabled.Load() && p.mode == "apply_missing" && q.Action == sc.ReleaseLeaseUsage {
		x, e := p.f.store.InTx(tx)
		if e != nil {
			return e
		}
		// An owned transaction fault after formal current authorization. The
		// real Apply must fail, rolling this deletion back with the final fact.
		result, e := x.Exec(ctx, `DELETE FROM agenteam_secret.secret_leases WHERE id=$1`, q.LeaseID.String())
		if e == nil {
			p.deleted.Add(result.RowsAffected())
		}
		return e
	}
	return nil
}
func deliveryServiceWithUsage(t *testing.T, f *b02Fixture, usage *releaseFaultAuthority) *b02Fixture {
	t.Helper()
	_, cursor, master := keys(t)
	aud, e := audit.New(f.store, cursor, audit.Authorizations{Sessions: f.authority, System: f.authority, Accounts: f.authority})
	if e != nil {
		t.Fatal(e)
	}
	s, e := secret.New(f.store, master, aud, secret.Authorizations{Sessions: f.authority, System: f.authority, Usage: usage, AccountWrites: f.authority})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Initialize(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	if e = os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	sink, e := recoverylog.Open(filepath.Join(dir, "unused-mail-release.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	service, e := account.New(account.Dependencies{Authority: f.authority, Audit: aud, Secrets: s, Processes: f.process, RecoveryLog: sink})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = service.Force(ctx)
	})
	copy := *f
	base := *f.fixture
	copy.fixture = &base
	copy.service, copy.secrets = service, s
	return &copy
}
func TestAccountMailReleaseDoesNotTreatProviderOrApplyFailureAsAbsence(t *testing.T) {
	for _, mode := range []string{"nested_not_found", "dependency", "apply_missing"} {
		t.Run(mode, func(t *testing.T) {
			f := newB02Account(t)
			job := prepareMailJob(t, f)
			provider := &releaseFaultAuthority{Authority: f.authority, f: f, mode: mode}
			consumer := deliveryServiceWithUsage(t, f, provider)
			port, runtime := newManualMail(t, consumer)
			a, e := port.ClaimDelivery(ctxFor(t), job)
			if e != nil {
				t.Fatal(e)
			}
			runtime.accept(a)
			material, e := port.PrepareDelivery(ctxFor(t), a)
			if e != nil {
				t.Fatal(e)
			}
			completion := runtime.finish(t, a, material)
			provider.enabled.Store(true)
			e = port.FinishDelivery(ctxFor(t), a, completion)
			if e == nil {
				t.Fatal("failed release was reported terminal")
			}
			if mode == "nested_not_found" {
				var found bool
				for err := e; err != nil; err = errors.Unwrap(err) {
					if se, ok := err.(*secret.Error); ok && se.Code() == secret.NotFound {
						found = true
					}
				}
				if !found {
					t.Fatal("real missing-lease cause was not exercised", e)
				}
			}
			if mode == "apply_missing" {
				var fault *foundation.Fault
				if provider.deleted.Load() != 1 || !errors.As(e, &fault) || fault.Code != foundation.NotFound || fault.CommitState != foundation.NotCommitted {
					t.Fatal("actual Apply absence did not roll back safely", provider.deleted.Load(), e)
				}
			}
			var joined, terminal bool
			var live, audits int
			e = f.store.QueryRow(ctxFor(t), `SELECT io_joined,terminal,(SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_id=$1 AND NOT released),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='smtp.delivery') FROM agenteam_account.mail_attempts WHERE id=$1`, a.Details().AttemptID.String()).Scan(&joined, &terminal, &live, &audits)
			if e != nil || !joined || terminal || live != 1 || audits != 0 {
				t.Fatal("release failure lost protected checkpoint", joined, terminal, live, audits, e)
			}
			provider.enabled.Store(false)
			if _, e = port.RecoverDeliveries(ctxFor(t)); e != nil {
				t.Fatal("retained completion did not recover", e)
			}
			e = f.store.QueryRow(ctxFor(t), `SELECT terminal,(SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_id=$1 AND NOT released),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='smtp.delivery') FROM agenteam_account.mail_attempts WHERE id=$1`, a.Details().AttemptID.String()).Scan(&terminal, &live, &audits)
			if e != nil || !terminal || live != 0 || audits != 1 {
				t.Fatal("exact retry did not close atomically", terminal, live, audits, e)
			}
		})
	}
}
