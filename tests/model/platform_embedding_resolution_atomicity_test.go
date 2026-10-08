//go:build integration

package model_test

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestModelPlatformEmbeddingResolutionAtomicity(t *testing.T) {
	v := newPlatformEmbeddingResolutionFixture(t)
	_, withRef := v.embeddingConfig(t, true)
	_, withoutRef := v.embeddingConfig(t, false)
	for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
		t.Run(string(purpose), func(t *testing.T) {
			t.Run("external-input-snapshot-binding-lease-same-commit", func(t *testing.T) {
				for _, target := range []mc.ModelView{withRef, withoutRef} {
					v.choose(t, target.ID)
					r := v.request(t, purpose)
					p := v.discover(t, r)
					before := v.tap.calls.Load()
					provisional, result := v.final(t, r, p, true, true)
					if result.State() != f.NotCommitted || provisional.Validate() != nil {
						t.Fatal("rollback/provisional boundary", result.Fault())
					}
					v.atomicFacts(t, r, p, 0)
					out, result := v.final(t, r, p, true, false)
					if result.State() != f.Committed || out.Validate() != nil {
						t.Fatal("atomic acceptance", result.Fault())
					}
					v.atomicFacts(t, r, p, 1)
					if target.ID == withoutRef.ID && (out.Snapshot.CredentialRef != nil || out.CredentialLease != nil || before != v.tap.calls.Load()) {
						t.Fatal("no-ref resolution called Secret")
					}
					if target.ID == withRef.ID && (out.CredentialLease == nil || v.tap.calls.Load() != before+2) {
						t.Fatal("both outer attempts did not use planned Acquire")
					}
				}
			})
			t.Run("secret-error-rolls-back-external-input-too", func(t *testing.T) {
				v.choose(t, withRef.ID)
				r := v.request(t, purpose)
				p := v.discover(t, r)
				v.tap.mu.Lock()
				v.tap.fail = true
				v.tap.mu.Unlock()
				out, result := v.final(t, r, p, true, false)
				v.tap.mu.Lock()
				v.tap.fail = false
				v.tap.mu.Unlock()
				if result.State() != f.NotCommitted || out.Snapshot.ID.Validate() == nil {
					t.Fatal("Secret failure published/committed")
				}
				v.atomicFacts(t, r, p, 0)
			})
			t.Run("selector-ex-serializes-both-selection-reads", func(t *testing.T) {
				v.choose(t, withRef.ID)
				key, e := f.SystemConfigLock("model-platform-selection")
				if e != nil {
					t.Fatal(e)
				}
				for _, discover := range []bool{false, true} {
					r := v.request(t, purpose)
					holder, release := v.hold(t, f.LockRequest{Key: key, Mode: f.Exclusive})
					pending := platformEmbeddingStart(t, func(ctx context.Context) error {
						if discover {
							p, e := v.resolving.DiscoverResolve(ctx, r)
							if e == nil && p.Details().ModelID != withRef.ID {
								return errors.New("wrong discovered target")
							}
							return e
						}
						out, e := v.resolving.SelectModel(ctx, v.owner, mc.SelectionRequest{Consumer: r.Consumer, Selection: *r.Selection})
						if e == nil && (out.Selected == nil || *out.Selected != withRef.ID) {
							return errors.New("wrong selected target")
						}
						return e
					})
					v.blocked(t, holder, pending)
					release()
					if e = pending.wait(); e != nil {
						t.Fatal("serialized selector read", e)
					}
				}
			})
			t.Run("real-selector-switch-invalidates-prepared-under-sh-ex", func(t *testing.T) {
				v.choose(t, withRef.ID)
				r := v.request(t, purpose)
				old := v.discover(t, r)
				state, e := v.service.GetPlatformSelection(testContext(t), v.admin)
				if e != nil {
					t.Fatal(e)
				}
				choice := state.Configured.Clone()
				choice.Embedding = withoutRef.ID
				request := mc.UpdatePlatformSelectionRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ExpectedVersion: state.Version, Selection: choice}
				key, e := f.SystemConfigLock("model-platform-selection")
				if e != nil {
					t.Fatal(e)
				}
				holder, release := v.hold(t, f.LockRequest{Key: key, Mode: f.Shared})
				var receipt mc.CommandReceipt
				pending := platformEmbeddingStart(t, func(ctx context.Context) error {
					var e error
					receipt, e = v.service.UpdatePlatformSelection(ctx, request)
					return e
				})
				v.blocked(t, holder, pending)
				release()
				if e = pending.wait(); e != nil || receipt.Version != state.Version+1 {
					t.Fatal("selector update after holder", e)
				}
				_, result := v.final(t, r, old, true, false)
				if result.State() != f.NotCommitted {
					t.Fatal("stale prepared choice accepted")
				}
				requireCode(t, result.Fault(), f.ResourceBusy)
				v.atomicFacts(t, r, old, 0)
				fresh := v.discover(t, r)
				if fresh.Details().SnapshotID != old.Details().SnapshotID || fresh.Details().ModelID != withoutRef.ID {
					t.Fatal("replan changed logical unit")
				}
				out, result := v.final(t, r, fresh, true, false)
				if result.State() != f.Committed || out.Snapshot.Identity.ModelID != withoutRef.ID {
					t.Fatal("replanned final", result.Fault())
				}
				v.atomicFacts(t, r, fresh, 1)
			})
			t.Run("cross-provider-delete-replacement-holds-original-selector", func(t *testing.T) {
				_, oldModel := v.embeddingConfig(t, true)
				_, replacement := v.embeddingConfig(t, true)
				if oldModel.ProviderID == replacement.ProviderID {
					t.Fatal("not a cross-provider fixture")
				}
				v.choose(t, oldModel.ID)
				r := v.request(t, purpose)
				old := v.discover(t, r)
				request := mc.DeleteModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: oldModel.ID, ExpectedVersion: oldModel.Version, Replacement: &replacement.ID}
				key, e := f.SystemConfigLock("model-platform-selection")
				if e != nil {
					t.Fatal(e)
				}
				holder, release := v.hold(t, f.LockRequest{Key: key, Mode: f.Shared})
				var receipt mc.CommandReceipt
				pending := platformEmbeddingStart(t, func(ctx context.Context) error {
					var e error
					receipt, e = v.service.DeleteModel(ctx, request)
					return e
				})
				v.blocked(t, holder, pending)
				release()
				if e = pending.wait(); e != nil || receipt.AffectedReferences != 1 {
					t.Fatal("cross-provider selector replacement", e, receipt.AffectedReferences)
				}
				_, result := v.final(t, r, old, true, false)
				if result.State() != f.NotCommitted {
					t.Fatal("deleted prepared target accepted")
				}
				v.atomicFacts(t, r, old, 0)
				fresh := v.discover(t, r)
				out, result := v.final(t, r, fresh, true, false)
				if result.State() != f.Committed || out.Snapshot.Identity.ModelID != replacement.ID {
					t.Fatal("replacement final", result.Fault())
				}
				v.atomicFacts(t, r, fresh, 1)
			})
			t.Run("canonical-call-ref-reentry-and-independent-call", func(t *testing.T) {
				v.choose(t, withRef.ID)
				var leases []sc.LeaseID
				for range 2 {
					r := v.request(t, purpose)
					a := v.resolve(t, r)
					b := v.resolve(t, r)
					if a.CredentialLease == nil || b.CredentialLease == nil || a.Snapshot.ID != b.Snapshot.ID || a.CredentialLease.LeaseID != b.CredentialLease.LeaseID {
						t.Fatal("canonical lease drift")
					}
					leases = append(leases, a.CredentialLease.LeaseID)
				}
				if leases[0] == leases[1] {
					t.Fatal("different calls shared lease")
				}
			})
		})
	}
	v.noSecretRead(t)
}
