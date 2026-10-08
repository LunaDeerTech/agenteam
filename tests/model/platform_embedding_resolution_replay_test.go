//go:build integration

package model_test

import (
	"bytes"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelPlatformEmbeddingResolutionReplay(t *testing.T) {
	v := newPlatformEmbeddingResolutionFixture(t)
	for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
		t.Run(string(purpose), func(t *testing.T) {
			_, first := v.embeddingConfig(t, true)
			provider, second := v.embeddingConfig(t, true)
			v.choose(t, first.ID)
			r := v.request(t, purpose)
			old := v.discover(t, r)
			v.choose(t, second.ID)
			prepared := v.discover(t, r)
			if old.Details().SnapshotID != prepared.Details().SnapshotID || prepared.Details().ModelID != second.ID {
				t.Fatal("prepared target did not replan on the same snapshot")
			}
			var version int64
			if e := v.raw.QueryRow(testContext(t), `SELECT plan_version FROM agenteam_model.resolution_preparations WHERE snapshot_id=$1`, prepared.Details().SnapshotID.String()).Scan(&version); e != nil || version != 2 {
				t.Fatal("changed draft plan version", version, e)
			}
			_, result := v.final(t, r, old, true, false)
			if result.State() != f.NotCommitted {
				t.Fatal("old draft accepted")
			}
			v.atomicFacts(t, r, prepared, 0)
			accepted, result := v.final(t, r, prepared, true, false)
			if result.State() != f.Committed {
				t.Fatal(result.Fault())
			}
			v.atomicFacts(t, r, prepared, 1)
			frozen := resolutionJSON(accepted.Snapshot)
			lease := accepted.CredentialLease.LeaseID
			same := func(out mc.ResolvedModel) {
				t.Helper()
				if !bytes.Equal(frozen, resolutionJSON(out.Snapshot)) || out.CredentialLease == nil || out.CredentialLease.LeaseID != lease {
					t.Fatal("committed snapshot or canonical lease changed")
				}
			}
			t.Run("live-switch-disable-endpoint-and-delete-do-not-rebind-history", func(t *testing.T) {
				v.choose(t, first.ID)
				same(v.resolve(t, r))
				provider.Input.Enabled = false
				provider.Input.BaseURL = "https://changed-embedding.example/v2"
				receipt, e := v.service.UpdateProvider(testContext(t), mc.UpdateProviderRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: provider.ID, ExpectedVersion: provider.Version, Input: provider.Input})
				if e != nil {
					t.Fatal(e)
				}
				provider.Version = receipt.Version
				same(v.resolve(t, r))
				second.Input.Enabled = false
				receipt, e = v.service.UpdateModel(testContext(t), mc.UpdateModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: second.ID, ExpectedVersion: second.Version, Input: second.Input})
				if e != nil {
					t.Fatal(e)
				}
				second.Version = receipt.Version
				same(v.resolve(t, r))
				if _, e = v.service.DeleteModel(testContext(t), mc.DeleteModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: second.ID, ExpectedVersion: second.Version, Replacement: &first.ID}); e != nil {
					t.Fatal(e)
				}
				if _, e = v.service.DeleteProvider(testContext(t), mc.DeleteProviderRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: provider.ID, ExpectedVersion: provider.Version}); e != nil {
					t.Fatal(e)
				}
				same(v.resolve(t, r))
				next := v.resolve(t, v.request(t, purpose))
				if next.Snapshot.Identity.ModelID != first.ID || next.Snapshot.ID == accepted.Snapshot.ID || next.CredentialLease == nil || next.CredentialLease.LeaseID == lease {
					t.Fatal("new Call did not select current model and independent lease")
				}
			})
			t.Run("new-authority-and-formal-new-session-use-durable-result", func(t *testing.T) {
				restarted := bindPlatformEmbeddingResolution(t, v.projectConfigurationFixture, v.identity)
				current := r.Clone()
				current.Actor = v.identity.login(t, v.identity.ownerBrowser.email).actor
				_, result := restarted.final(t, current, prepared, false, false)
				requireCode(t, result.Fault(), f.Forbidden)
				same(restarted.resolve(t, current))
			})
			t.Run("accepted-input-cannot-be-consistently-rewritten-to-borrow-snapshot", func(t *testing.T) {
				d, e := v.strict.facts(testContext(t), v.raw, r.Consumer.OperationID)
				if e != nil {
					t.Fatal(e)
				}
				next := resolutionDigest("changed current input").String()
				v.sql(t, `UPDATE platform_embedding_fixture.inputs SET digest=$2,version=version+1 WHERE id=$1`, d.Input, next)
				v.sql(t, `UPDATE platform_embedding_fixture.operations SET input_digest=$2,input_version=input_version+1,version=version+1 WHERE id=$1`, d.Operation, next)
				defer func() {
					v.sql(t, `UPDATE platform_embedding_fixture.inputs SET digest=$2,version=$3 WHERE id=$1`, d.Input, d.InputDigest, d.InputVersion)
					v.sql(t, `UPDATE platform_embedding_fixture.operations SET input_digest=$2,input_version=$3,version=$4 WHERE id=$1`, d.Operation, d.ExpectedDigest, d.ExpectedVersion, d.OperationVersion)
				}()
				before, calls := v.strict.factsChecked.Load(), v.tap.calls.Load()
				out, e := v.resolving.ResolveModel(testContext(t), r)
				resolutionNoResult(t, out, e)
				requireCode(t, e, f.Forbidden)
				if v.strict.factsChecked.Load() != before+1 || v.tap.calls.Load() != calls {
					t.Fatal("accepted-input guard not reached or reached Acquire")
				}
			})
			t.Run("committed-subject-and-operation-still-require-current-permission", func(t *testing.T) {
				d, e := v.strict.facts(testContext(t), v.raw, r.Consumer.OperationID)
				if e != nil {
					t.Fatal(e)
				}
				for _, subject := range []bool{true, false} {
					if subject {
						v.sql(t, `UPDATE platform_embedding_fixture.subjects SET phase='closed' WHERE id=$1`, d.Subject)
					} else {
						v.sql(t, `UPDATE platform_embedding_fixture.operations SET phase='cancelled' WHERE id=$1`, d.Operation)
					}
					out, e := v.resolving.ResolveModel(testContext(t), r)
					if subject {
						v.sql(t, `UPDATE platform_embedding_fixture.subjects SET phase=$2 WHERE id=$1`, d.Subject, d.SubjectPhase)
					} else {
						v.sql(t, `UPDATE platform_embedding_fixture.operations SET phase=$2 WHERE id=$1`, d.Operation, d.OperationPhase)
					}
					resolutionNoResult(t, out, e)
					requireCode(t, e, f.Forbidden)
				}
				same(v.resolve(t, r))
			})
			t.Run("consumer-remapping-does-not-promise-draft-version-increment", func(t *testing.T) {
				request := v.request(t, purpose)
				before := v.discover(t, request)
				v.sql(t, `UPDATE platform_embedding_fixture.operations SET version=version+1 WHERE id=$1`, request.Consumer.OperationID)
				after := v.discover(t, request)
				if before.Details().SnapshotID != after.Details().SnapshotID || before.Details().Mapping == after.Details().Mapping {
					t.Fatal("consumer mapping did not invalidate opaque plan")
				}
				_, result := v.final(t, request, before, true, false)
				requireCode(t, result.Fault(), f.Forbidden)
				v.atomicFacts(t, request, after, 0)
				_, result = v.final(t, request, after, true, false)
				if result.State() != f.Committed {
					t.Fatal(result.Fault())
				}
				v.atomicFacts(t, request, after, 1)
			})
			t.Run("explicit-version-is-not-silently-upgraded", func(t *testing.T) {
				state, e := v.service.GetPlatformSelection(testContext(t), v.admin)
				if e != nil {
					t.Fatal(e)
				}
				request := v.request(t, purpose)
				version := state.Version
				request.Selection.Version = &version
				p := v.discover(t, request)
				_, next := v.embeddingConfig(t, false)
				v.choose(t, next.ID)
				got, e := v.resolving.DiscoverResolve(testContext(t), request)
				resolutionZeroPlan(t, got, e)
				requireCode(t, e, f.ResourceBusy)
				if *request.Selection.Version != version {
					t.Fatal("caller version mutated")
				}
				v.atomicFacts(t, request, p, 0)
			})
			t.Run("released-canonical-lease-never-resurrected", func(t *testing.T) {
				v.sql(t, `UPDATE agenteam_secret.secret_leases SET released=true WHERE id=$1`, lease.String())
				out, e := v.resolving.ResolveModel(testContext(t), r)
				resolutionNoResult(t, out, e)
				requireCode(t, e, f.InvalidState)
				var released bool
				if e = v.raw.QueryRow(testContext(t), `SELECT released FROM agenteam_secret.secret_leases WHERE id=$1`, lease.String()).Scan(&released); e != nil || !released {
					t.Fatal("released lease revived", e)
				}
				t.Log("controlled irreversible released metadata in this disposable fixture; no production release/material operation")
			})
		})
	}
	// Rev3 preserves the existing canonical authorization error ahead of the
	// semantic comparison. Exercise both refs and both preparation phases; the
	// committed request's original ref is used even after a live selector switch.
	for _, withRef := range []bool{false, true} {
		_, target := v.embeddingConfig(t, withRef)
		_, opposite := v.embeddingConfig(t, !withRef)
		for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
			for _, committed := range []bool{false, true} {
				modes := []string{"operation", "explicit-version", "cross-purpose"}
				if purpose == mc.MemoryEmbedding {
					modes = append(modes, "agent")
				}
				for _, mode := range modes {
					t.Run(platformEmbeddingSemanticName(purpose, withRef, committed, mode), func(t *testing.T) {
						v.choose(t, target.ID)
						r := v.request(t, purpose)
						p := v.discover(t, r)
						if committed {
							_, result := v.final(t, r, p, true, false)
							if result.State() != f.Committed {
								t.Fatal(result.Fault())
							}
							v.choose(t, opposite.ID)
						}
						changed := r.Clone()
						d, e := v.strict.facts(testContext(t), v.raw, r.Consumer.OperationID)
						if e != nil {
							t.Fatal(e)
						}
						var oldAgent any
						if r.Consumer.AgentID != nil {
							oldAgent = r.Consumer.AgentID.String()
						}
						switch mode {
						case "operation":
							changed.Consumer.OperationID = newID[struct{}](t).String()
							v.bindFacts(t, changed)
						case "explicit-version":
							version := *p.Details().SelectionVersion
							changed.Selection.Version = &version
						case "agent", "cross-purpose":
							var agent any
							if mode == "agent" || purpose == mc.KnowledgeEmbedding {
								key := newID[id.Agent](t)
								changed.Consumer.AgentID = &key
								agent = key.String()
								v.sql(t, `INSERT INTO platform_embedding_fixture.agents(id,project_id,phase,version)VALUES($1,$2,'active',1)`, agent, d.Project)
							} else {
								changed.Consumer.AgentID = nil
							}
							if mode == "cross-purpose" {
								if purpose == mc.KnowledgeEmbedding {
									changed.Consumer.Kind, changed.Consumer.Purpose = mc.MemoryConsumer, mc.MemoryEmbedding
								} else {
									changed.Consumer.Kind, changed.Consumer.Purpose = mc.KnowledgeConsumer, mc.KnowledgeEmbedding
								}
								changed.Purpose = changed.Consumer.Purpose
							}
							v.sql(t, `UPDATE platform_embedding_fixture.subjects SET kind=$2,agent_id=$3 WHERE id=$1`, d.Subject, string(changed.Consumer.Kind), agent)
							v.sql(t, `UPDATE platform_embedding_fixture.operations SET purpose=$2 WHERE id=$1`, d.Operation, string(changed.Purpose))
							defer func() {
								v.sql(t, `UPDATE platform_embedding_fixture.subjects SET kind=$2,agent_id=$3 WHERE id=$1`, d.Subject, d.Kind, oldAgent)
								v.sql(t, `UPDATE platform_embedding_fixture.operations SET purpose=$2 WHERE id=$1`, d.Operation, d.Purpose)
							}()
						}
						if changed.Validate() != nil {
							t.Fatal("semantic counterexample did not reach valid structure")
						}
						before, calls := v.strict.validated.Load(), v.tap.calls.Load()
						snapshots := v.count(t, "snapshots")
						got, e := v.resolving.DiscoverResolve(testContext(t), changed)
						resolutionZeroPlan(t, got, e)
						want := f.IdempotencyKeyReused
						if withRef && committed && (mode == "agent" || mode == "cross-purpose") {
							want = f.Forbidden
						}
						requireCode(t, e, want)
						if v.strict.validated.Load() != before+1 || v.tap.calls.Load() != calls || v.count(t, "snapshots") != snapshots {
							t.Fatal("semantic/canonical error bypassed current authorization or created side effects")
						}
					})
				}
			}
		}
	}
	v.noSecretRead(t)
}

func platformEmbeddingSemanticName(purpose mc.Purpose, ref, committed bool, mode string) string {
	name := string(purpose) + "/same-call/"
	if ref {
		name += "ref/"
	} else {
		name += "no-ref/"
	}
	if committed {
		name += "committed/"
	} else {
		name += "prepared/"
	}
	return name + mode
}
