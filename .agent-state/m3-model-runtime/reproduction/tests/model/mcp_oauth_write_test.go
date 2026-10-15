//go:build integration

package model_test

import (
	"context"
	"net"
	"net/url"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// This fixture supplies only MCP-owned private intent and lifecycle facts.
// Account sessions, Project gates, Secret sealing/ledger and Audit are real.
// It is not an acceptance test for MCP's OAuth exchange or refresh consumer.
type oauthWriteFixtureKey struct{}
type oauthWriteFixtureOwner struct {
	request sc.MCPOAuthWriteRequest
	actor   id.Actor
}
type oauthWriteFixtureAuthority struct {
	v      *textRuntimeFixture
	issuer sc.PlanIssuer
}
type oauthWriteFixtureFacts struct {
	Operation, Project, Connection, Owner, Kind, Binding, User, Process, State string
	Revision                                                                   f.Version
}

func (a *oauthWriteFixtureAuthority) proof(ctx context.Context, x postgres.SQLExecutor, r sc.MCPOAuthWriteRequest) (oauthWriteFixtureFacts, error) {
	deny := func() (oauthWriteFixtureFacts, error) {
		return oauthWriteFixtureFacts{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	private, ok := ctx.Value(oauthWriteFixtureKey{}).(*oauthWriteFixtureOwner)
	if !ok || private == nil || r.Validate() != nil {
		return deny()
	}
	want, _ := sc.MCPOAuthWriteBinding(private.request)
	got, _ := sc.MCPOAuthWriteBinding(r)
	if want != got || !private.actor.Equal(a.v.base.owner) {
		return deny()
	}
	v := r.Fields()
	var out oauthWriteFixtureFacts
	err := x.QueryRow(ctx, `SELECT id::text,project_id::text,connection_id::text,owner_id::text,owner_kind,binding,initiator::text,process_id::text,state,config_revision FROM model_runtime_fixture.oauth_writes WHERE id=$1`, v.Actor.Details().CauseRef).Scan(&out.Operation, &out.Project, &out.Connection, &out.Owner, &out.Kind, &out.Binding, &out.User, &out.Process, &out.State, &out.Revision)
	if err != nil {
		return out, err
	}
	process, err := a.v.guard.CurrentProcess()
	if err != nil || out.Operation != v.Actor.Details().CauseRef || out.Project != v.ProjectID.String() || out.Connection != v.ConnectionID || out.Owner != v.OwnerID || out.Kind != string(v.OwnerKind) || out.Binding != string(got) || out.User != private.actor.Details().UserID || out.Process != process.String() || out.Revision != v.ConfigRevision {
		return deny()
	}
	return out, nil
}
func oauthWriteFixtureLocks(v oauthWriteFixtureFacts) []f.LockRequest {
	user, _ := f.UserLock(v.User)
	project, _ := f.ProjectLock(v.Project)
	owner, _ := f.RecordLock(f.ReferenceRecordLock, "fixture-oauth:"+v.Operation)
	return []f.LockRequest{{Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared}, {Key: owner, Mode: f.Exclusive}}
}
func oauthWriteFixtureMapping(v oauthWriteFixtureFacts) f.Digest {
	v.State = ""
	return resolutionDigest(v)
}
func (a *oauthWriteFixtureAuthority) DiscoverMCPOAuthWrite(ctx context.Context, r sc.MCPOAuthWriteRequest) (sc.WriteDependencies, error) {
	v, e := a.proof(ctx, a.v.base.raw, r)
	if e != nil {
		return sc.WriteDependencies{}, e
	}
	binding, _ := sc.MCPOAuthWriteBinding(r)
	return sc.NewWriteDependencies(a.issuer, binding, oauthWriteFixtureMapping(v), oauthWriteFixtureLocks(v))
}
func (a *oauthWriteFixtureAuthority) ValidateMCPOAuthWriteInTx(ctx context.Context, tx f.Tx, r sc.MCPOAuthWriteRequest, p sc.WriteDependencies) error {
	return a.validate(ctx, tx, r, p, false)
}
func (a *oauthWriteFixtureAuthority) ValidateMCPOAuthWriteObservationInTx(ctx context.Context, tx f.Tx, r sc.MCPOAuthWriteRequest, p sc.WriteDependencies) error {
	return a.validate(ctx, tx, r, p, true)
}
func (a *oauthWriteFixtureAuthority) validate(ctx context.Context, tx f.Tx, r sc.MCPOAuthWriteRequest, p sc.WriteDependencies, observe bool) error {
	x, e := a.v.base.raw.InTx(tx)
	if e != nil {
		return e
	}
	v, e := a.proof(ctx, x, r)
	if e != nil {
		return e
	}
	binding, _ := sc.MCPOAuthWriteBinding(r)
	if !p.Matches(a.issuer, binding, oauthWriteFixtureMapping(v)) || !observe && v.State != "pending" {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	if e = a.v.base.raw.RequireHeldLocks(ctx, tx, p.Locks()); e != nil {
		return e
	}
	if observe {
		return nil
	} // Original private owner + durable before/after proof only.
	if e = a.v.base.accounts.RequireCurrentSession(ctx, tx, a.v.base.owner); e != nil {
		return e
	}
	_, e = a.v.base.projects.RequireOwnerInTx(ctx, tx, a.v.base.owner, r.Fields().ProjectID, id.Mutate)
	return e
}
func newOAuthWriteFixture(t *testing.T, v *textRuntimeFixture) (*oauthWriteFixtureAuthority, *secret.MCPOAuthWriter) {
	t.Helper()
	_, err := v.base.raw.Exec(testContext(t), `CREATE TABLE model_runtime_fixture.oauth_writes(id uuid PRIMARY KEY,project_id uuid NOT NULL,connection_id uuid NOT NULL,owner_id uuid NOT NULL,owner_kind text NOT NULL,binding text NOT NULL,initiator uuid NOT NULL,process_id uuid NOT NULL,state text NOT NULL,config_revision bigint NOT NULL,credential_id uuid,result_version bigint)`)
	requireTextRuntime(t, err)
	a := &oauthWriteFixtureAuthority{v, sc.NewPlanIssuer()}
	writer, err := secret.NewMCPOAuthWrites(v.base.secrets, a)
	requireTextRuntime(t, err)
	return a, writer
}
func oauthWriteRequest(t *testing.T, v *textRuntimeFixture, kind sc.MutationKind, ref sc.CredentialRef, version f.Version) (context.Context, sc.MCPOAuthWriteRequest) {
	t.Helper()
	operation := newID[struct{}](t).String()
	connection := newID[struct{}](t).String()
	role, _ := id.RegisterService(id.SecretService)
	actor, _ := role.Actor(operation, v.base.scope)
	command, _ := f.NewCommandIdentity("secret.mcp", []string{v.base.project.ID.String(), connection, connection}, string(kind), f.IdempotencyKey(operation))
	material, err := sc.NewSecretMaterial([]byte("private-oauth-runtime-fixture-token-pkce"))
	requireTextRuntime(t, err)
	t.Cleanup(material.Destroy)
	r, err := sc.NewMCPOAuthWriteRequest(sc.MCPOAuthWriteFields{Actor: actor, Identity: command, ProjectID: v.base.project.ID, ConnectionID: connection, ConfigRevision: 1, Kind: kind, Ref: ref, ExpectedVersion: version, OwnerKind: sc.MCPOAuthToken, OwnerID: connection, Value: material})
	requireTextRuntime(t, err)
	binding, _ := sc.MCPOAuthWriteBinding(r)
	process, err := v.guard.CurrentProcess()
	requireTextRuntime(t, err)
	_, err = v.base.raw.Exec(testContext(t), `INSERT INTO model_runtime_fixture.oauth_writes(id,project_id,connection_id,owner_id,owner_kind,binding,initiator,process_id,state,config_revision) VALUES($1,$2,$3,$3,'oauth_token',$4,$5,$6,'pending',1)`, operation, v.base.project.ID.String(), connection, string(binding), v.base.owner.Details().UserID, process.String())
	requireTextRuntime(t, err)
	return context.WithValue(testContext(t), oauthWriteFixtureKey{}, &oauthWriteFixtureOwner{r.WithoutMaterial(), v.base.owner}), r
}
func oauthWritePrepare(t *testing.T, w *secret.MCPOAuthWriter, ctx context.Context, r sc.MCPOAuthWriteRequest) sc.PreparedMCPOAuthWrite {
	t.Helper()
	plan, e := w.DiscoverMCPOAuthWrite(ctx, r)
	requireTextRuntime(t, e)
	p, e := w.PrepareMCPOAuthWrite(ctx, r, plan)
	requireTextRuntime(t, e)
	r.Fields().Value.Destroy()
	return p
}
func oauthWriteApply(t *testing.T, v *textRuntimeFixture, w *secret.MCPOAuthWriter, ctx context.Context, r sc.MCPOAuthWriteRequest, p sc.PreparedMCPOAuthWrite, tail func(context.Context, f.Tx) error) f.CommitResult {
	t.Helper()
	cause, e := f.NewCommandsCause(r.Fields().Identity)
	requireTextRuntime(t, e)
	return v.base.raw.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := v.base.raw.AcquireAll(ctx, tx, p.RequiredLocks()); e != nil {
			return e
		}
		out, e := w.ApplyMCPOAuthWriteInTx(ctx, tx, p)
		if e != nil {
			return e
		}
		if !out.Found {
			return f.NewFault(f.InvalidState, f.NotStarted)
		}
		x, e := v.base.raw.InTx(tx)
		if e != nil {
			return e
		}
		_, e = x.Exec(ctx, `UPDATE model_runtime_fixture.oauth_writes SET state='applied',credential_id=$2,result_version=$3 WHERE id=$1`, r.Fields().Actor.Details().CauseRef, out.Result.Metadata.CredentialRef.Details().ID.String(), int64(out.Result.Metadata.Version))
		if e != nil {
			return e
		}
		if tail != nil {
			return tail(ctx, tx)
		}
		return nil
	})
}
func oauthWriteObserve(t *testing.T, v *textRuntimeFixture, w *secret.MCPOAuthWriter, ctx context.Context, p sc.PreparedMCPOAuthWrite) sc.MCPOAuthWriteObservation {
	t.Helper()
	var out sc.MCPOAuthWriteObservation
	result := v.base.raw.WithinTx(ctx, recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if e := v.base.raw.AcquireAll(ctx, tx, p.RequiredLocks()); e != nil {
			return e
		}
		var e error
		out, e = w.ObserveMCPOAuthWriteInTx(ctx, tx, p)
		return e
	})
	if result.State() != f.Committed {
		t.Fatal("original OAuth observation", result.Fault())
	}
	return out
}

func TestModelM3MCPOAuthSealedWrite(t *testing.T) {
	v := newTextRuntimeFixture(t)
	_, writer := newOAuthWriteFixture(t, v)
	ctx, r := oauthWriteRequest(t, v, sc.Create, sc.CredentialRef{}, 0)
	p := oauthWritePrepare(t, writer, ctx, r)
	if oauthWriteObserve(t, v, writer, ctx, p).Found {
		t.Fatal("uncommitted create found")
	}
	denied := oauthWriteApply(t, v, writer, testContext(t), r, p, nil)
	if denied.State() != f.NotCommitted {
		t.Fatal("bare DTO authorized")
	}
	requireCode(t, denied.Fault(), f.Forbidden)

	weak := v.base.raw.WithinTx(ctx, recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		// Omit the complete sealed plan. The Store poisons the transaction even if
		// a caller discards the immediate failure.
		_, _ = writer.ApplyMCPOAuthWriteInTx(ctx, tx, p)
		return nil
	})
	if weak.State() != f.NotCommitted || oauthWriteObserve(t, v, writer, ctx, p).Found {
		t.Fatal("missing full lock plan committed")
	}
	rollback := oauthWriteApply(t, v, writer, ctx, r, p, func(context.Context, f.Tx) error { return f.NewFault(f.InvalidState, f.NotStarted) })
	if rollback.State() != f.NotCommitted || oauthWriteObserve(t, v, writer, ctx, p).Found {
		t.Fatal("outer rollback leaked receipt/value")
	}
	result := oauthWriteApply(t, v, writer, ctx, r, p, nil)
	if result.State() != f.Committed {
		t.Fatal("sealed OAuth create", result.Fault())
	}
	created := oauthWriteObserve(t, v, writer, ctx, p)
	if !created.Found || created.Result.Metadata.Version != 1 || created.Result.Metadata.Purpose != sc.MCP {
		t.Fatal("create observation")
	}
	ref := p.Ref()
	updateCtx, update := oauthWriteRequest(t, v, sc.Update, ref, 1)
	updateP := oauthWritePrepare(t, writer, updateCtx, update)
	result = oauthWriteApply(t, v, writer, updateCtx, update, updateP, nil)
	if result.State() != f.Committed {
		t.Fatal("OAuth CAS update", result.Fault())
	}
	staleCtx, stale := oauthWriteRequest(t, v, sc.Update, ref, 1)
	staleP := oauthWritePrepare(t, writer, staleCtx, stale)
	result = oauthWriteApply(t, v, writer, staleCtx, stale, staleP, nil)
	if result.State() != f.NotCommitted {
		t.Fatal("stale update committed")
	}
	requireCode(t, result.Fault(), f.VersionConflict)
	// Cancellation only authorizes exact original observation, never a write.
	_, err := v.base.raw.Exec(testContext(t), `UPDATE model_runtime_fixture.oauth_writes SET state='cancelled' WHERE id=$1`, stale.Fields().Actor.Details().CauseRef)
	requireTextRuntime(t, err)
	if oauthWriteObserve(t, v, writer, staleCtx, staleP).Found {
		t.Fatal("canceled write invented")
	}
	result = oauthWriteApply(t, v, writer, staleCtx, stale, staleP, nil)
	requireCode(t, result.Fault(), f.Forbidden)
	deleteCtx, del := oauthWriteRequest(t, v, sc.Delete, ref, 2)
	deleteP := oauthWritePrepare(t, writer, deleteCtx, del)
	result = oauthWriteApply(t, v, writer, deleteCtx, del, deleteP, nil)
	if result.State() != f.Committed {
		t.Fatal("OAuth delete", result.Fault())
	}
	deleted := oauthWriteObserve(t, v, writer, deleteCtx, deleteP)
	if !deleted.Found || !deleted.Result.Deleted || deleted.Result.Metadata.Version != 3 {
		t.Fatal("delete observation")
	}
	var receipts, audits, calls int
	err = v.base.raw.QueryRow(testContext(t), `SELECT (SELECT count(*) FROM agenteam_secret.secret_command_receipts WHERE credential_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE resource_id=$1),(SELECT count(*) FROM agenteam_model.calls)`, ref.Details().ID.String()).Scan(&receipts, &audits, &calls)
	requireTextRuntime(t, err)
	if receipts != 3 || audits != 3 || calls != 0 {
		t.Fatalf("receipt/audit/call counts %d/%d/%d", receipts, audits, calls)
	}
}

func TestModelM3MCPOAuthRealCommitUnknown(t *testing.T) {
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
				u, e := url.Parse(db.Fixture.URL(db.Name))
				requireTextRuntime(t, e)
				u.Host = address
				return openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
			})
			_, writer := newOAuthWriteFixture(t, v)
			ctx, r := oauthWriteRequest(t, v, sc.Create, sc.CredentialRef{}, 0)
			p := oauthWritePrepare(t, writer, ctx, r)
			result := oauthWriteApply(t, v, writer, ctx, r, p, func(ctx context.Context, tx f.Tx) error { // All canonical participants are present before the real final COMMIT.
				x, e := v.base.raw.InTx(tx)
				if e != nil {
					return e
				}
				var ready bool
				e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM model_runtime_fixture.oauth_writes o JOIN agenteam_secret.secrets s ON s.id=o.credential_id JOIN agenteam_secret.secret_command_receipts r ON r.credential_id=s.id WHERE o.id=$1 AND o.state='applied' AND s.version=1 AND r.result_version=1)`, r.Fields().Actor.Details().CauseRef).Scan(&ready)
				if e != nil {
					return e
				}
				if !ready {
					return f.NewFault(f.InvalidState, f.NotStarted)
				}
				arm()
				return nil
			})
			if result.State() != f.Unknown || result.AttemptID().Validate() != nil {
				t.Fatal("lost COMMIT did not remain Unknown")
			}
			if committed {
				waitSignal(t, trace.ackDropped)
			} else {
				waitSignal(t, held.reached)
				close(held.release)
				waitSignal(t, held.completed)
			}
			direct := openStore(t, v.base.db.Config(t, nil))
			settled := direct.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error { return direct.AcquireAll(ctx, tx, p.RequiredLocks()) })
			if settled.State() != f.Committed {
				t.Fatal("original OAuth writer not settled", settled.Fault())
			}
			facts := func() string {
				var out string
				e := direct.QueryRow(testContext(t), `SELECT jsonb_build_array(o.xmin::text,o.state,o.credential_id,o.result_version,(SELECT count(*) FROM agenteam_secret.secret_command_receipts WHERE credential_id=$2),(SELECT count(*) FROM agenteam_secret.secrets WHERE id=$2),(SELECT count(*) FROM agenteam_model.calls))::text FROM model_runtime_fixture.oauth_writes o WHERE id=$1`, r.Fields().Actor.Details().CauseRef, p.Ref().Details().ID.String()).Scan(&out)
				requireTextRuntime(t, e)
				return out
			}
			before := facts()
			if out := oauthWriteObserve(t, v, writer, ctx, p); out.Found != committed {
				t.Fatalf("original observation found=%t committed=%t", out.Found, committed)
			}
			if before != facts() {
				t.Fatal("observation changed OAuth/Secret ledger or dispatched")
			}
		})
	}
}
