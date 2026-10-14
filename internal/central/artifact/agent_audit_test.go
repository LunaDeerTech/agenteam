package artifact

import (
	"context"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type agentAuditTestAuthority struct {
	ac.Authority
	tx        f.Tx
	want      ac.AccessSubject
	deny      error
	checks    int
	checkedAt f.Instant
}

func (a *agentAuditTestAuthority) AuthorizeInTx(ctx context.Context, tx f.Tx, s ac.AccessSubject, intent i.AccessIntent) (i.AccessGrant, error) {
	a.checks++
	if tx != a.tx || !s.Actor.Equal(a.want.Actor) || s.ProjectID != a.want.ProjectID || s.ExecutionID != a.want.ExecutionID || s.OperationID != a.want.OperationID || s.ToolID != a.want.ToolID || s.ToolCallID != a.want.ToolCallID {
		return i.AccessGrant{}, failure(f.Forbidden, nil)
	}
	if a.deny != nil {
		return i.AccessGrant{}, a.deny
	}
	scope, _ := i.InProject(s.ProjectID)
	return i.NewAccessGrant(s.Actor, scope, intent, a.checkedAt, 1)
}

func TestArtifactAgentAuditRequiresOriginalWitnessAndCurrentAuthority(t *testing.T) {
	for _, path := range []string{"content", "read"} {
		for _, mode := range []string{"current", "revoked", "changed-operation", "wrong-tx", "wrong-store", "missing-witness"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				v := newProjectAuditFixture(t, path)
				d := v.request.invocation.Details()
				execution := newTestID[i.Execution](t)
				d.Actor, _ = i.NewAgentRun(d.ProjectID, newTestID[i.Agent](t), execution)
				d.ExecutionID, d.OperationID, d.ToolID, d.ToolCallID = execution.String(), newTestID[struct{}](t).String(), newTestID[struct{}](t).String(), newTestID[struct{}](t).String()
				var err error
				v.request.invocation, err = ac.NewInvocation(d)
				if err != nil {
					t.Fatal(err)
				}
				if path == "content" {
					m := v.meta.Details()
					m.CreatedByUserID = ""
					m.CreatedByAgentID = d.Actor.Details().AgentID
					m.ExecutionID = d.ExecutionID
					m.OperationID = d.OperationID
					v.meta, err = ac.NewMetadata(m)
					if err != nil {
						t.Fatal(err)
					}
					v.storedMeta = v.meta
					v.command.creatorKind, v.command.creator, v.command.originalActor = i.AgentRun, d.Actor.Details().AgentID, stableActor(d.Actor)
					v.command.execution, v.command.operation, v.command.tool, v.command.toolCall = d.ExecutionID, d.OperationID, d.ToolID, d.ToolCallID
					v.storedCommand = v.command
				}
				authority := &agentAuditTestAuthority{tx: v.store.tx, want: invocationSubject(v.request.invocation), checkedAt: v.meta.Details().CreatedAt}
				if mode == "revoked" {
					authority.deny = failure(f.ConfirmationStale, nil)
				}
				v.service.state().owners, err = NewOwnerProvider(v.store, authority)
				if err != nil {
					t.Fatal(err)
				}
				v.service.state().audit = projectAuditAppender(func(ctx context.Context, tx f.Tx, entry au.Entry, key au.AppendKey) (au.AppendReceipt, error) {
					v.saved, v.entry, v.key = ctx, entry, key
					store := Store(v.store)
					switch mode {
					case "changed-operation":
						fields := entry.Fields()
						fields.Associations.OperationID = newTestID[struct{}](t).String()
						entry, _ = au.NewEntry(fields)
					case "wrong-tx":
						tx = f.NewTx()
					case "wrong-store":
						store = &projectAuditStore{}
					case "missing-witness":
						ctx = context.Background()
					}
					if err := CheckAgentAuditAuthorizationInTx(ctx, tx, store, entry, key); err != nil {
						return au.AppendReceipt{}, err
					}
					return au.AppendReceipt{}, v.authority.CheckProjectAuditInTx(ctx, tx, entry, key)
				})
				err = v.append(context.Background())
				if mode == "current" {
					if err != nil || authority.checks != 1 {
						t.Fatal("original current AgentRun audit failed", err, authority.checks)
					}
				} else if err == nil {
					t.Fatal("invalid AgentRun audit accepted")
				}
				if CheckAgentAuditAuthorizationInTx(v.saved, v.store.tx, v.store, v.entry, v.key) == nil {
					t.Fatal("expired Artifact witness reused")
				}
			})
		}
	}
}
