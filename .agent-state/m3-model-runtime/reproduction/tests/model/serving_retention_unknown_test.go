//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func TestModelM3ServingRetentionRealCommitUnknown(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "commit-not-forwarded"
		if committed {
			name = "commit-ack-dropped"
		}
		t.Run(name, func(t *testing.T) {
			var trace *projectModelPGTrace
			var held *modelCommitProxy
			var arm func()
			v := newTextRuntimeFixtureWithStore(t, nil, nil, nil, func(t *testing.T, db *pgfixture.Database) *postgres.Store {
				upstream := net.JoinHostPort("127.0.0.1", db.Fixture.Port)
				var address string
				if committed {
					trace = newProjectModelPGTrace(t, upstream)
					address = trace.listener.Addr().String()
					arm = func() { trace.dropCommitACK.Store(true) }
				} else {
					held = newModelCommitProxy(t, upstream, false)
					address = held.listener.Addr().String()
					arm = func() { held.armed.Store(true) }
				}
				u, err := url.Parse(db.Fixture.URL(db.Name))
				requireTextRuntime(t, err)
				u.Host = address
				return openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
			})
			_, err := v.base.raw.Exec(testContext(t), `CREATE TABLE model_runtime_fixture.generations(id uuid PRIMARY KEY,project_id uuid NOT NULL,kind text NOT NULL,agent_id uuid,snapshot_id uuid NOT NULL,initiator uuid NOT NULL,process_id uuid NOT NULL,state text NOT NULL,version bigint NOT NULL,enabled boolean NOT NULL)`)
			requireTextRuntime(t, err)
			a := &servingFixtureAuthority{textRuntimeConsumer: v.consumer, issuer: mc.NewPlanIssuer()}
			retention, err := model.NewServingSnapshotRetention(v.base.authority, a, v.base.secrets)
			requireTextRuntime(t, err)
			v.wire.allow(t, true)
			key := embeddingWireScenario(t, v.wire, wireJSON(embeddingWireReply(t, []int{0}, [][]float64{{.25, -.5}}, json.RawMessage(`{"prompt_tokens":3,"total_tokens":3}`))))
			em := m3RuntimeModel(t, v, key, true)
			chat := m3RuntimeModel(t, v, key, false)
			m3RuntimeSelection(t, v, em, chat)
			_, call := m3RuntimeRequest(t, v, mc.KnowledgeEmbedding, mc.ResponseFormat{Kind: "text"}, nil)
			ctx, request := servingFixtureRequest(t, v, call.Model.Snapshot.ID, mc.MemoryConsumer)
			plan, err := retention.DiscoverServingSnapshotRetention(ctx, request)
			requireTextRuntime(t, err)
			if result := servingFixtureTx(t, v, retention, ctx, request, plan, "active", false); result.State() != f.Committed {
				t.Fatal("initial retention", result.Fault())
			}
			if _, err = v.core.Embed(testContext(t), call); err != nil {
				t.Fatal("initial call", err)
			}
			m3RuntimeTerminal(t, v, call.CallID, "succeeded")
			request.Action = mc.ReleaseServingSnapshot
			request.OwnerVersion = 2
			ctx = context.WithValue(testContext(t), servingFixtureKey{}, &servingFixtureOwner{request.Clone()})
			plan, err = retention.DiscoverServingSnapshotRetention(ctx, request)
			requireTextRuntime(t, err)
			// Before the transition, lookup is authorized and false, while the
			// identical request/plan still cannot release an active generation.
			if servingFixtureObserve(t, v, retention, ctx, request, plan) {
				t.Fatal("active generation reported released")
			}
			denied := v.base.raw.WithinTx(ctx, recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
				if err := v.base.raw.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				return retention.ApplyServingSnapshotRetentionInTx(ctx, tx, request, plan)
			})
			if denied.State() != f.NotCommitted {
				t.Fatal("observation authority released active generation")
			}
			requireCode(t, denied.Fault(), f.Forbidden)
			cause := recoveryCause(t)
			result := v.base.raw.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
				if err := v.base.raw.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				x, err := v.base.raw.InTx(tx)
				if err != nil {
					return err
				}
				if _, err = x.Exec(ctx, `UPDATE model_runtime_fixture.generations SET state='retired',version=2 WHERE id=$1`, request.Owner.GenerationID); err != nil {
					return err
				}
				if err = retention.ApplyServingSnapshotRetentionInTx(ctx, tx, request, plan); err != nil {
					return err
				}
				var final bool
				err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.serving_snapshot_retentions r JOIN model_runtime_fixture.generations g ON g.id=r.generation_id::uuid WHERE r.generation_id=$1 AND r.state='released' AND r.released_version=2 AND g.state='retired' AND g.version=2 AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_references s WHERE s.owner_id=r.reference_id::uuid AND s.consumer='model'))`, request.Owner.GenerationID).Scan(&final)
				if err != nil {
					return err
				}
				if !final {
					return f.NewFault(f.InvalidState, f.NotStarted)
				}
				// Arm only the actual final COMMIT, after all three durable
				// participants have their exact final facts in this transaction.
				arm()
				return nil
			})
			if result.State() != f.Unknown || result.AttemptID().Validate() != nil {
				t.Fatal("real wire loss did not preserve Unknown")
			}
			if committed {
				waitSignal(t, trace.ackDropped)
			} else {
				waitSignal(t, held.reached)
				close(held.release)
				waitSignal(t, held.completed)
			}
			// An independent connection confirms the original writer has
			// released the generation lock before inspecting its durable result.
			direct := openStore(t, v.base.db.Config(t, nil))
			settled := direct.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error { return direct.AcquireAll(ctx, tx, plan.RequiredLocks()) })
			if settled.State() != f.Committed {
				t.Fatal("original writer not settled", settled.Fault())
			}
			facts := func() string {
				var out string
				err := direct.QueryRow(testContext(t), `SELECT jsonb_build_array(r.xmin::text,r.state,r.released_version,g.xmin::text,g.state,g.version,(SELECT count(*) FROM agenteam_secret.secret_references s WHERE s.owner_id=r.reference_id::uuid AND s.consumer='model'),(SELECT count(*) FROM agenteam_model.resolution_preparations),(SELECT count(*) FROM agenteam_model.calls),(SELECT count(*) FROM agenteam_secret.secret_leases))::text FROM agenteam_model.serving_snapshot_retentions r JOIN model_runtime_fixture.generations g ON g.id=r.generation_id::uuid WHERE r.generation_id=$1`, request.Owner.GenerationID).Scan(&out)
				requireTextRuntime(t, err)
				return out
			}
			before := facts()
			if observed := servingFixtureObserve(t, v, retention, ctx, request, plan); observed != committed {
				t.Fatalf("lookup=%t committed=%t", observed, committed)
			}
			if before != facts() {
				t.Fatal("original-plan lookup changed durable facts")
			}
			if len(v.wire.state(t, key).Requests) != 1 {
				t.Fatal("Unknown confirmation dispatched a new call")
			}
			v.wire.settled(t, key)
		})
	}
}
