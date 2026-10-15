package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// Reuse the original owned-call/strict-row controls. This proves the local
// lock, projection and Audit boundary, not real Account/Project authorization.
type metadataReadStore struct{ *creationLookupStore }

func (s *metadataReadStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.artifactCallTestStore.InTx(tx); err != nil {
		return nil, err
	}
	return metadataReadExecutor{s, tx}, nil
}

type metadataReadExecutor struct {
	store *metadataReadStore
	tx    f.Tx
}

func (e metadataReadExecutor) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, errors.New("metadata point read must not open payloads or a list")
}
func (e metadataReadExecutor) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	s := e.store
	if _, err := s.InTx(e.tx); err != nil {
		return cleanupOwnerRow{err: err}
	}
	if strings.Contains(query, "agenteam_artifact.project_") || strings.HasPrefix(query, "SELECT EXISTS") && strings.Contains(query, "agenteam_artifact.cleanup") {
		return s.fixture.row(ctx, e.tx, query, args)
	}
	lock := artifactLock(s.rows.meta.Details().Reference.ArtifactID.String())
	s.mu.Lock()
	held := append([]f.LockRequest(nil), s.live[e.tx]...)
	s.mu.Unlock()
	found := false
	for _, current := range held {
		found = found || current.Key.Canonical() == lock.Key.Canonical() && current.Mode == lock.Mode
	}
	if !found {
		return cleanupOwnerRow{err: errors.New("metadata queried before original Artifact lock")}
	}
	s.businessReads++
	if strings.HasPrefix(query, "SELECT id,project_id,creation_cause") {
		s.ownerReads++
		m := s.rows.storedMeta.Details()
		if len(args) != 1 || args[0] != m.Reference.ArtifactID.String() {
			return cleanupOwnerRow{err: errors.New("owner identity changed")}
		}
		return cleanupOwnerRow{values: []any{m.Reference.ArtifactID.String(), m.Reference.ProjectID.String(), s.rows.rowCause, "", m.Object.ID.String(), "", "", "completed", "existing", int64(m.Version)}}
	}
	return s.rows.store.QueryRow(ctx, query, args...)
}
func (e metadataReadExecutor) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !strings.Contains(query, "agenteam_artifact.project_") {
		e.store.businessWrites++
		return pgconn.CommandTag{}, errors.New("metadata read attempted a business write")
	}
	return e.store.fixture.exec(ctx, e.tx, query, args)
}

func TestArtifactMetadataUsesCurrentOwnedReadWithoutObjectIO(t *testing.T) {
	for _, mode := range []string{"published", "missing", "foreign-project", "current-owner-denied", "audit-failed"} {
		t.Run(mode, func(t *testing.T) {
			v := newCreationLookupFixture(t)
			store := &metadataReadStore{v.store}
			owners, err := NewOwnerProvider(store, v.authority)
			if err != nil {
				t.Fatal(err)
			}
			facts, err := NewProjectAuditAuthority(store)
			if err != nil {
				t.Fatal(err)
			}
			v.call.state.store, v.call.state.owners = store, owners
			// A metadata read must work with every Object payload port unbound.
			v.call.state.objects, v.call.state.uploads, v.call.state.reads, v.call.state.resolver = nil, nil, nil, nil
			audits := 0
			v.call.state.audit = projectAuditAppender(func(ctx context.Context, tx f.Tx, entry au.Entry, key au.AppendKey) (au.AppendReceipt, error) {
				audits++
				var fields struct {
					Sent string `json:"sent_bytes"`
				}
				if entry.Fields().Action != au.ArtifactRead || json.Unmarshal(entry.Fields().Metadata.JSON(), &fields) != nil || fields.Sent != "0" {
					t.Fatal("metadata read claimed payload bytes or another action")
				}
				if mode == "audit-failed" {
					return au.AppendReceipt{}, failure(f.DependencyUnavailable, nil)
				}
				return au.AppendReceipt{}, facts.CheckProjectAuditInTx(ctx, tx, entry, key)
			})
			want := f.Code("")
			switch mode {
			case "missing":
				v.store.rows.missingArtifact, want = true, f.NotFound
			case "foreign-project":
				m := v.store.rows.storedMeta.Details()
				m.Reference.ProjectID = newTestID[i.Project](t)
				m.Object.Scope = identityScope(m.Reference.ProjectID)
				v.store.rows.storedMeta, err = ac.NewMetadata(m)
				if err != nil {
					t.Fatal(err)
				}
				want = f.NotFound
			case "current-owner-denied":
				v.authority.deny, want = true, f.Forbidden
			case "audit-failed":
				want = f.DependencyUnavailable
			}
			ref := v.store.rows.meta.Details().Reference
			out, err := v.call.service.ReadArtifactMetadata(context.Background(), v.invocation, ref.ArtifactID)
			if want == "" {
				if err != nil || out.Validate() != nil || out.Details().Reference != ref || ref.ArtifactID.String() == ref.FileID.String() || audits != 1 || store.ownerReads != 1 {
					t.Fatal("metadata lost its original published identity or Audit", err, audits, store.ownerReads)
				}
			} else {
				projectAuditCode(t, err, want)
				if out.Validate() == nil || mode != "audit-failed" && audits != 0 {
					t.Fatal("failed read published metadata or Audit")
				}
			}
			v.assertReadOnly(t)
		})
	}
}

func TestArtifactMetadataRejectsUnboundInvocationBeforePorts(t *testing.T) {
	actor, _ := i.NewHuman(newTestID[i.User](t), newTestID[i.Session](t))
	v, err := ac.NewInvocation(ac.InvocationDetails{Actor: actor, ProjectID: newTestID[i.Project](t), ExecutionID: newTestID[i.Execution](t).String(), AttemptID: newTestID[ac.CallAttempt](t)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&Service{}).ReadArtifactMetadata(context.Background(), v, newTestID[ac.ArtifactEntity](t))
	projectAuditCode(t, err, f.Forbidden)
}
