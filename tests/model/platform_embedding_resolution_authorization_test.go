//go:build integration

package model_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestModelPlatformEmbeddingResolutionAuthorization(t *testing.T) {
	v := newPlatformEmbeddingResolutionFixture(t)
	_, target := v.embeddingConfig(t, true)
	v.choose(t, target.ID)
	for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
		t.Run(string(purpose), func(t *testing.T) {
			t.Run("independent-subject-operation-and-input-columns", func(t *testing.T) {
				modes := []string{"subject-project", "subject-kind", "subject-phase", "subject-version", "operation-subject", "call", "purpose", "initiator", "build-target", "operation-phase", "operation-version", "input-identity", "input-subject", "input-digest", "input-schema", "input-version", "input-phase"}
				if purpose == mc.MemoryEmbedding {
					modes = append(modes, "agent-id", "agent-project", "agent-phase", "agent-version")
				}
				for _, mode := range modes {
					t.Run(mode, func(t *testing.T) {
						r := v.request(t, purpose)
						p := v.discover(t, r)
						before, valid := v.strict.factsChecked.Load(), v.strict.validated.Load()
						calls := v.tap.calls.Load()
						d, e := v.strict.facts(testContext(t), v.raw, r.Consumer.OperationID)
						if e != nil {
							t.Fatal(e)
						}
						other := newID[struct{}](t).String()
						result := v.observed.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
							if e := v.observed.AcquireAll(ctx, tx, p.RequiredLocks()); e != nil {
								return e
							}
							x, e := v.observed.InTx(tx)
							if e != nil {
								return e
							}
							e = platformEmbeddingCorruptFact(ctx, x, d, mode, other)
							if e != nil {
								return e
							}
							_, e = v.resolving.ResolveModelInTx(ctx, tx, r, p)
							return e
						})
						if result.State() != f.NotCommitted {
							t.Fatal("changed canonical fact accepted", mode)
						}
						requireCode(t, result.Fault(), f.Forbidden)
						if v.strict.factsChecked.Load() != before+1 || v.strict.validated.Load() != valid || v.tap.calls.Load() != calls {
							t.Fatal("bad fact missed target check or reached Secret", mode)
						}
						v.atomicFacts(t, r, p, 0)
						after, e := v.strict.facts(testContext(t), v.raw, r.Consumer.OperationID)
						if e != nil || after != d {
							t.Fatal("negative mutation did not roll back", mode, e)
						}
						// A fresh plan must also reject bad current facts. Otherwise an
						// old-plan mapping mismatch could hide an allow-all authority.
						result = v.observed.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
							if e := v.observed.AcquireAll(ctx, tx, p.RequiredLocks()); e != nil {
								return e
							}
							x, e := v.observed.InTx(tx)
							if e != nil {
								return e
							}
							return platformEmbeddingCorruptFact(ctx, x, d, mode, other)
						})
						if result.State() != f.Committed {
							t.Fatal("bad current facts not installed", result.Fault())
						}
						defer v.restoreCanonicalFacts(t, d, other)
						before = v.strict.factsChecked.Load()
						got, e := v.resolving.DiscoverResolve(testContext(t), r)
						resolutionZeroPlan(t, got, e)
						requireCode(t, e, f.Forbidden)
						if v.strict.factsChecked.Load() != before+1 || v.strict.validated.Load() != valid || v.tap.calls.Load() != calls {
							t.Fatal("fresh bad facts bypassed the canonical check", mode)
						}
						v.atomicFacts(t, r, p, 0)
					})
				}
			})
			t.Run("formal-logout-and-new-session", func(t *testing.T) {
				browser := v.identity.login(t, v.identity.ownerBrowser.email)
				r := v.request(t, purpose)
				r.Actor = browser.actor
				p := v.discover(t, r)
				if e := v.identity.core.Logout(testContext(t), account.LogoutRequest{Actor: browser.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())}); e != nil {
					t.Fatal(e)
				}
				before := v.tap.calls.Load()
				_, result := v.final(t, r, p, true, false)
				if result.State() != f.NotCommitted || v.tap.calls.Load() != before {
					t.Fatal("revoked Session reached Secret")
				}
				v.atomicFacts(t, r, p, 0)
				r.Actor = v.identity.login(t, browser.email).actor
				_, result = v.final(t, r, p, false, false)
				requireCode(t, result.Fault(), f.Forbidden)
				fresh := v.discover(t, r)
				out, result := v.final(t, r, fresh, true, false)
				if result.State() != f.Committed || out.Validate() != nil {
					t.Fatal("fresh current Session", result.Fault())
				}
				v.atomicFacts(t, r, fresh, 1)
			})
			t.Run("owner-is-not-domain-authority-and-admin-is-not-project-owner", func(t *testing.T) {
				for _, actor := range []id.Actor{v.admin, v.identity.otherBrowser.actor} {
					r := v.request(t, purpose)
					r.Actor = actor
					v.bindFacts(t, r)
					p, e := v.resolving.DiscoverResolve(testContext(t), r)
					resolutionZeroPlan(t, p, e)
					requireCode(t, e, f.NotFound)
				}
				r := v.request(t, purpose)
				r.Consumer.OperationID = newID[struct{}](t).String()
				before := v.strict.factsChecked.Load()
				p, e := v.resolving.DiscoverResolve(testContext(t), r)
				resolutionZeroPlan(t, p, e)
				requireCode(t, e, f.Forbidden)
				if v.strict.factsChecked.Load() != before+1 {
					t.Fatal("Owner missing-operation rejection did not reach domain authority")
				}
				r = v.request(t, purpose)
				r.Consumer.ProjectID = v.createProject(t, v.identity.otherBrowser.actor).ID
				if purpose == mc.MemoryEmbedding {
					agent := newID[id.Agent](t)
					r.Consumer.AgentID = &agent
				}
				v.bindFacts(t, r)
				p, e = v.resolving.DiscoverResolve(testContext(t), r)
				resolutionZeroPlan(t, p, e)
				requireCode(t, e, f.NotFound)
			})
			t.Run("current-project-gate-precedes-secret", func(t *testing.T) {
				r := v.request(t, purpose)
				p := v.discover(t, r)
				v.sql(t, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=clock_timestamp(),updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, v.project.ID.String())
				defer v.sql(t, `UPDATE agenteam_project.projects SET lifecycle='active',archived_at=NULL,updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, v.project.ID.String())
				before := v.tap.calls.Load()
				_, result := v.final(t, r, p, true, false)
				requireCode(t, result.Fault(), f.ProjectNotActive)
				v.atomicFacts(t, r, p, 0)
				if v.tap.calls.Load() != before {
					t.Fatal("closed gate reached Acquire")
				}
			})
			t.Run("opaque-plan-full-request-foreign-store-and-expired-tx", func(t *testing.T) {
				r := v.request(t, purpose)
				p := v.discover(t, r)
				for _, mode := range []string{"actor", "call", "operation", "selector-version"} {
					changed := r.Clone()
					switch mode {
					case "actor":
						changed.Actor = v.admin
					case "call":
						changed.LeaseOwner = resolutionCallOwner(t)
					case "operation":
						changed.Consumer.OperationID = newID[struct{}](t).String()
					case "selector-version":
						version := f.Version(99)
						changed.Selection.Version = &version
					}
					before := v.strict.factsChecked.Load()
					_, result := v.final(t, changed, p, false, false)
					requireCode(t, result.Fault(), f.Forbidden)
					if v.strict.factsChecked.Load() != before {
						t.Fatal("wrong request bypassed plan binding", mode)
					}
				}
				forged, e := mc.NewResolutionPlan(mc.NewPlanIssuer(), p.Details())
				if e != nil {
					t.Fatal(e)
				}
				_, result := v.final(t, r, forged, false, false)
				requireCode(t, result.Fault(), f.Forbidden)
				other := openStore(t, v.db.Config(t, nil))
				reached := false
				result = other.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
					if e := other.AcquireAll(ctx, tx, p.RequiredLocks()); e != nil {
						return e
					}
					reached = true
					_, e := v.resolving.ResolveModelInTx(ctx, tx, r, p)
					return e
				})
				if !reached || result.State() != f.NotCommitted {
					t.Fatal("same DB foreign Store accepted")
				}
				out, e := v.resolving.ResolveModelInTx(testContext(t), f.Tx{}, r, p)
				resolutionNoResult(t, out, e)
				var expired f.Tx
				result = v.observed.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
					expired = tx
					return v.observed.AcquireAll(ctx, tx, p.RequiredLocks())
				})
				if result.State() != f.Committed {
					t.Fatal(result.Fault())
				}
				out, e = v.resolving.ResolveModelInTx(testContext(t), expired, r, p)
				resolutionNoResult(t, out, e)
				v.atomicFacts(t, r, p, 0)
			})
			t.Run("missing-low-selector-domain-lock-and-weak-mode-poison", func(t *testing.T) {
				r := v.request(t, purpose)
				p := v.discover(t, r)
				targets := []string{"user:", "model-platform-selection", "operation:", "embedding-input:", "embedding-operation:", "weak-operation"}
				if purpose == mc.MemoryEmbedding {
					targets = append(targets, "agent:", "aggregate:memory:")
				} else {
					targets = append(targets, "knowledge-tree:")
				}
				for _, target := range targets {
					var locks []f.LockRequest
					changed := false
					for _, l := range p.RequiredLocks() {
						if target == "weak-operation" && strings.Contains(l.Key.Canonical(), "embedding-operation:") {
							if l.Mode != f.Exclusive {
								t.Fatal("fixture lost required EX")
							}
							l.Mode = f.Shared
							changed = true
							locks = append(locks, l)
							continue
						}
						if target != "weak-operation" && strings.Contains(l.Key.Canonical(), target) {
							changed = true
							continue
						}
						locks = append(locks, l)
					}
					if !changed {
						t.Fatal("missing required test lock", target)
					}
					var inner error
					reached := false
					result := v.observed.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
						if e := v.observed.AcquireAll(ctx, tx, locks); e != nil {
							return e
						}
						reached = true
						_, inner = v.resolving.ResolveModelInTx(ctx, tx, r, p)
						return nil
					})
					if !reached || inner == nil || result.State() != f.NotCommitted {
						t.Fatal("swallowed missing/weak lock did not poison", target, inner)
					}
					v.atomicFacts(t, r, p, 0)
				}
				locks := p.RequiredLocks()
				var inner error
				result := v.observed.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
					if e := v.observed.AcquireAll(ctx, tx, locks[len(locks)-1:]); e != nil {
						return e
					}
					inner = v.observed.AcquireAll(ctx, tx, locks[:1])
					return nil
				})
				if inner == nil || result.State() != f.NotCommitted {
					t.Fatal("lower-order acquisition failure did not poison")
				}
				v.atomicFacts(t, r, p, 0)
			})
			t.Run("actual-secret-witness-exact-tx-and-signed-lease", func(t *testing.T) {
				r := v.request(t, purpose)
				p := v.discover(t, r)
				_, result := v.final(t, r, p, true, false)
				if result.State() != f.Committed {
					t.Fatal(result.Fault())
				}
				v.atomicFacts(t, r, p, 1)
				v.tap.mu.Lock()
				savedCtx, savedRequest, savedPlan := v.tap.ctx, v.tap.request, v.tap.plan
				v.tap.mu.Unlock()
				for _, alter := range []bool{false, true} {
					request := savedRequest
					if alter {
						request.LeaseID = newID[sc.Lease](t)
					}
					txCtx, cancel := context.WithTimeout(testContext(t), time.Second)
					deadline, _ := txCtx.Deadline()
					probeCtx, probeCancel := context.WithDeadline(context.WithoutCancel(savedCtx), deadline)
					reached := false
					var inner error
					result = v.observed.WithinTx(txCtx, recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
						if e := v.observed.AcquireAll(ctx, tx, p.RequiredLocks()); e != nil {
							return e
						}
						if e := v.observed.RequireHeldLocks(ctx, tx, p.RequiredLocks()); e != nil {
							return e
						}
						reached = true
						_, inner = v.resolutionSecrets.ApplyUsageInTx(probeCtx, tx, request, savedPlan)
						return inner
					})
					probeCancel()
					cancel()
					if !reached || inner == nil || result.State() != f.NotCommitted {
						t.Fatal("stale witness accepted", inner)
					}
					if alter {
						requireCode(t, inner, f.InvalidArgument)
					} else {
						requireCode(t, inner, f.Forbidden)
					}
					v.atomicFacts(t, r, p, 1)
				}
			})
		})
	}
	v.noSecretRead(t)
}

func platformEmbeddingCorruptFact(ctx context.Context, x postgres.SQLExecutor, d platformEmbeddingResolutionFacts, mode, other string) error {
	var e error
	exec := func(q string, args ...any) error { _, e := x.Exec(ctx, q, args...); return e }
	switch mode {
	case "subject-project":
		e = exec(`UPDATE platform_embedding_fixture.subjects SET project_id=$2 WHERE id=$1`, d.Subject, other)
	case "subject-kind":
		e = exec(`UPDATE platform_embedding_fixture.subjects SET kind='wrong-kind' WHERE id=$1`, d.Subject)
	case "subject-phase":
		e = exec(`UPDATE platform_embedding_fixture.subjects SET phase='closed' WHERE id=$1`, d.Subject)
	case "subject-version":
		e = exec(`UPDATE platform_embedding_fixture.subjects SET version=0 WHERE id=$1`, d.Subject)
	case "operation-subject", "input-subject":
		e = exec(`INSERT INTO platform_embedding_fixture.subjects(id,project_id,kind,phase,version)VALUES($1,$2,'knowledge','active',1)`, other, d.Project)
		if e == nil {
			if mode == "operation-subject" {
				e = exec(`UPDATE platform_embedding_fixture.operations SET subject_id=$2 WHERE id=$1`, d.Operation, other)
			} else {
				e = exec(`UPDATE platform_embedding_fixture.inputs SET subject_id=$2 WHERE id=$1`, d.Input, other)
			}
		}
	case "call":
		e = exec(`UPDATE platform_embedding_fixture.operations SET call_id=$2 WHERE id=$1`, d.Operation, other)
	case "purpose":
		e = exec(`UPDATE platform_embedding_fixture.operations SET purpose='rerank' WHERE id=$1`, d.Operation)
	case "initiator":
		e = exec(`UPDATE platform_embedding_fixture.operations SET initiator=$2 WHERE id=$1`, d.Operation, other)
	case "build-target":
		e = exec(`UPDATE platform_embedding_fixture.operations SET build_target='serving_query' WHERE id=$1`, d.Operation)
	case "operation-phase":
		e = exec(`UPDATE platform_embedding_fixture.operations SET phase='cancelled' WHERE id=$1`, d.Operation)
	case "operation-version":
		e = exec(`UPDATE platform_embedding_fixture.operations SET version=0 WHERE id=$1`, d.Operation)
	case "input-identity":
		e = exec(`INSERT INTO platform_embedding_fixture.inputs(id,subject_id,digest,schema_version,phase,version)VALUES($1,$2,$3,1,'active',1)`, other, d.Subject, resolutionDigest(other).String())
		if e == nil {
			e = exec(`UPDATE platform_embedding_fixture.operations SET input_id=$2 WHERE id=$1`, d.Operation, other)
		}
	case "input-digest":
		e = exec(`UPDATE platform_embedding_fixture.inputs SET digest=$2 WHERE id=$1`, d.Input, resolutionDigest(other).String())
	case "input-schema":
		e = exec(`UPDATE platform_embedding_fixture.inputs SET schema_version=schema_version+1 WHERE id=$1`, d.Input)
	case "input-version":
		e = exec(`UPDATE platform_embedding_fixture.inputs SET version=0 WHERE id=$1`, d.Input)
	case "input-phase":
		e = exec(`UPDATE platform_embedding_fixture.inputs SET phase='revoked' WHERE id=$1`, d.Input)
	case "agent-id":
		e = exec(`INSERT INTO platform_embedding_fixture.agents(id,project_id,phase,version)VALUES($1,$2,'active',1)`, other, d.Project)
		if e == nil {
			e = exec(`UPDATE platform_embedding_fixture.subjects SET agent_id=$2 WHERE id=$1`, d.Subject, other)
		}
	case "agent-project":
		e = exec(`UPDATE platform_embedding_fixture.agents SET project_id=$2 WHERE id=$1`, d.Agent, other)
	case "agent-phase":
		e = exec(`UPDATE platform_embedding_fixture.agents SET phase='disabled' WHERE id=$1`, d.Agent)
	case "agent-version":
		e = exec(`UPDATE platform_embedding_fixture.agents SET version=0 WHERE id=$1`, d.Agent)
	}
	return e
}

func (v *platformEmbeddingResolutionFixture) restoreCanonicalFacts(t *testing.T, d platformEmbeddingResolutionFacts, other string) {
	t.Helper()
	var agent any
	if d.SubjectAgent != "" {
		agent = d.SubjectAgent
	}
	v.sql(t, `UPDATE platform_embedding_fixture.subjects SET project_id=$2,kind=$3,agent_id=$4,phase=$5,version=$6 WHERE id=$1`, d.Subject, d.Project, d.Kind, agent, d.SubjectPhase, d.SubjectVersion)
	if d.Agent != "" {
		v.sql(t, `UPDATE platform_embedding_fixture.agents SET project_id=$2,phase=$3,version=$4 WHERE id=$1`, d.Agent, d.AgentProject, d.AgentPhase, d.AgentVersion)
	}
	v.sql(t, `UPDATE platform_embedding_fixture.inputs SET subject_id=$2,digest=$3,schema_version=$4,phase=$5,version=$6 WHERE id=$1`, d.Input, d.InputSubject, d.InputDigest, d.InputSchema, d.InputPhase, d.InputVersion)
	v.sql(t, `UPDATE platform_embedding_fixture.operations SET subject_id=$2,call_id=$3,purpose=$4,initiator=$5,build_target=$6,input_id=$7,input_digest=$8,input_schema=$9,input_version=$10,phase=$11,version=$12 WHERE id=$1`, d.Operation, d.Subject, d.Call, d.Purpose, d.Initiator, d.BuildTarget, d.Input, d.ExpectedDigest, d.ExpectedSchema, d.ExpectedVersion, d.OperationPhase, d.OperationVersion)
	v.sql(t, `DELETE FROM platform_embedding_fixture.inputs WHERE id=$1`, other)
	v.sql(t, `DELETE FROM platform_embedding_fixture.subjects WHERE id=$1`, other)
	v.sql(t, `DELETE FROM platform_embedding_fixture.agents WHERE id=$1`, other)
	after, e := v.strict.facts(testContext(t), v.raw, d.Operation)
	if e != nil || after != d {
		t.Error("current negative facts did not restore", e)
	}
}
