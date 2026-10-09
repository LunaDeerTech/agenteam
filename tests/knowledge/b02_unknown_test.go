//go:build integration

package knowledge_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// This wrapper observes a successful callback and the original Store result;
// it never substitutes CommitResult, retries a callback or changes SQL results.
type knowledgeCommitStore struct {
	knowledge.Store
	identity f.CommandIdentity
	project  string
	key      f.IdempotencyKey
	phase    string
	proxy    *commitproxy.Proxy
	armed    atomic.Bool
	original chan f.CommitResult
}

func (s *knowledgeCommitStore) WithinTx(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
	matched := cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == s.identity.Canonical()
	result := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := callback(ctx, tx); err != nil {
			return err
		}
		if !matched || s.armed.Load() {
			return nil
		}
		x, err := s.Store.InTx(tx)
		if err != nil {
			return err
		}
		var command, publication string
		var work bool
		err = x.QueryRow(ctx, `SELECT c.state,p.phase,
 EXISTS(SELECT 1 FROM agenteam_knowledge.work_claims w WHERE w.command_id=c.id)
 FROM agenteam_knowledge.commands c JOIN agenteam_knowledge.publications p ON p.command_id=c.id
 WHERE c.project_id=$1 AND c.command_name='create' AND c.command_key=$2`, s.project, string(s.key)).Scan(&command, &publication, &work)
		if err != nil {
			return err
		}
		reached := s.phase == "plan" && command == "planned" && publication == "planned" && !work ||
			s.phase == "reserve" && command == "planned" && publication == "reserved" ||
			s.phase == "final" && command == "completed" && publication == "published"
		if !reached || !s.armed.CompareAndSwap(false, true) {
			return nil
		}
		var pid int32
		if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		return s.proxy.Arm(pid)
	})
	if matched && result.State() == f.Unknown {
		select {
		case s.original <- result:
		default:
		}
	}
	return result
}

func unknownPublicationFixture(t *testing.T) (*ownerTreeFixture, *postgres.Store, *commitproxy.Proxy) {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	knowledgeMigrate(t, db, knowledgeMigrationSource(t, knowledgeMigrationFiles(t, "00025")))
	observer, err := postgres.Open(knowledgeContext(t), db.Config(t, nil))
	if err != nil {
		t.Fatal("owned observer Store unavailable")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := observer.ForceClose(ctx); err != nil {
			t.Error("observer Store did not close", err)
		}
	})
	proxy, err := commitproxy.New(knowledgeContext(t), net.JoinHostPort("127.0.0.1", db.Fixture.Port))
	if err != nil {
		t.Fatal("owned COMMIT proxy unavailable")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := proxy.Close(ctx); err != nil {
			t.Error("owned COMMIT proxy did not actually join", err)
		}
	})
	u, err := url.Parse(db.Fixture.URL(db.Name))
	if err != nil {
		t.Fatal("owned fixture URL invalid")
	}
	u.Host = proxy.Address()
	raw, err := postgres.Open(knowledgeContext(t), db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	if err != nil {
		t.Fatal("owned proxy Store unavailable")
	}
	return publicationFixtureFromTree(t, newOwnerTreeFixtureOnStore(t, raw)), observer, proxy
}

func unknownRollbackWriter(t *testing.T, observer *postgres.Store, proxy *commitproxy.Proxy) {
	t.Helper()
	pid := proxy.WriterPID()
	if pid <= 0 {
		t.Fatal("COMMIT was not bound to an exact writer")
	}
	// The held frame has not reached this owned database. End only its exact
	// verified backend, observe actual disappearance, then unblock the proxy.
	var stopped bool
	err := observer.QueryRow(knowledgeContext(t), `SELECT pg_terminate_backend(pid)
 FROM pg_stat_activity WHERE pid=$1 AND datname=current_database() AND application_name='agenteam'`, pid).Scan(&stopped)
	if err != nil || !stopped {
		t.Fatal("owned held writer termination not confirmed", err)
	}
	ctx, cancel := context.WithTimeout(knowledgeContext(t), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var present bool
		if err = observer.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND datname=current_database())`, pid).Scan(&present); err != nil {
			t.Fatal("owned writer retirement observation failed", err)
		}
		if !present {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("owned held writer did not actually exit")
		case <-ticker.C:
		}
	}
	proxy.Release()
	runtimeAwait(t, proxy.HeldJoined())
	select {
	case <-proxy.Committed():
		t.Fatal("not-forwarded control unexpectedly observed COMMIT completion")
	default:
	}
}

// Native protocol loss at three distinct Knowledge transactions. The observed
// f.Unknown and attempt come from real postgres.Store, not a returned fake.
// Cleanup checkpoints and cross-process recovery require their own groups.
func TestKnowledgeB02CommitUnknown(t *testing.T) {
	for _, phase := range []string{"plan", "reserve", "final"} {
		for _, commit := range []bool{false, true} {
			outcome := "not_forwarded"
			if commit {
				outcome = "committed_ack_lost"
			}
			t.Run(phase+"/"+outcome, func(t *testing.T) {
				x, observer, proxy := unknownPublicationFixture(t)
				actor := x.human(t)
				project := x.project(t, actor, true)
				request := kc.CreateRequest{ProjectID: project, DocumentID: treeID[kc.Document](t), Title: "unknown target"}
				meta := treeMeta(t)
				identity, err := kc.CommandIdentity(project, kc.Create, meta.IdempotencyKey)
				if err != nil {
					t.Fatal(err)
				}
				store := &knowledgeCommitStore{Store: x.raw, identity: identity, project: project.String(), key: meta.IdempotencyKey, phase: phase, proxy: proxy, original: make(chan f.CommitResult, 1)}
				s, err := knowledge.New(store, x.deps)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					s.Stop()
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					if err := s.Drain(ctx); err != nil {
						t.Error("Knowledge Unknown caller did not drain", err)
					}
				})
				type result struct {
					document kc.DocumentRef
					err      error
				}
				returned := make(chan result, 1)
				joined := make(chan struct{})
				ctx := knowledgeContext(t)
				input := publicationText(t, "native unknown bytes")
				digest, err := kc.CreateDigest(actor, meta, request, input)
				if err != nil {
					t.Fatal(err)
				}
				go func() {
					defer close(joined)
					d, err := s.CreateDocument(ctx, actor, meta, request, input)
					returned <- result{d, err}
				}()
				t.Cleanup(func() { proxy.Release(); runtimeAwait(t, joined) })
				runtimeAwait(t, proxy.Reached())
				var original f.CommitResult
				select {
				case original = <-store.original:
				case <-ctx.Done():
					t.Fatal("real Store Unknown was not observed")
				}
				if original.State() != f.Unknown || original.AttemptID().Validate() != nil || proxy.WriterPID() <= 0 {
					t.Fatal("missing original physical Unknown identity")
				}
				if commit {
					proxy.Release()
					runtimeAwait(t, proxy.Committed())
					runtimeAwait(t, proxy.HeldJoined())
				} else {
					unknownRollbackWriter(t, observer, proxy)
				}
				runtimeAwait(t, joined)
				got := <-returned
				var fault *f.Fault
				if got.document.Validate() == nil || !errors.As(got.err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.CauseID != original.AttemptID().String() {
					t.Fatal("public result did not preserve native Unknown/cause", got.err)
				}
				lookup, err := s.LookupCommand(knowledgeContext(t), actor, kc.LookupRequest{ProjectID: project, Command: kc.Create, Key: meta.IdempotencyKey, SemanticDigest: digest})
				want := kc.InProgress
				if phase == "plan" && !commit {
					want = kc.NotObserved
				} else if phase == "final" && commit {
					want = kc.Committed
				}
				if err != nil || lookup.State != want || (lookup.Receipt != nil) != (want == kc.Committed) {
					t.Fatal("native outcome and public Lookup disagree", err)
				}
				if want != kc.Committed {
					runtimeNoCanonical(t, x, project)
				}
				actual, err := s.CreateDocument(knowledgeContext(t), actor, meta, request, publicationText(t, "native unknown bytes"))
				if err != nil || actual.ID != request.DocumentID || actual.ContentVersion != 1 {
					t.Fatal("original command did not converge after actual writer outcome", err)
				}
				publicationRead(t, x, actor, actual, []byte("native unknown bytes"))
				if audits, events := publicationCount(t, x, project); audits != 1 || events != 1 {
					t.Fatal("Unknown recovery duplicated Object Audit or Outbox")
				}
			})
		}
	}
}

// Only the final Knowledge checkpoint is intercepted. Object deletion, its
// Audit and reference/lease checks have already happened through real D05.
type knowledgeCleanupCommitStore struct {
	knowledge.Store
	cleanup  string
	proxy    *commitproxy.Proxy
	armed    atomic.Bool
	original chan f.CommitResult
}

func (s *knowledgeCleanupCommitStore) WithinTx(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
	d := cause.Details()
	matched := cause.Kind() == f.RecoveryCause && d.Owner == "knowledge.cleanup" && d.RecoveryRunID == s.cleanup && d.CheckpointRef == ""
	result := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := callback(ctx, tx); err != nil {
			return err
		}
		if !matched || s.armed.Load() {
			return nil
		}
		x, err := s.Store.InTx(tx)
		if err != nil {
			return err
		}
		var completed, objectDeleted bool
		err = x.QueryRow(ctx, `SELECT c.phase='completed',o.state='deleted'
 FROM agenteam_knowledge.object_cleanup c JOIN agenteam_object.objects o ON o.id=c.object_id
 WHERE c.id=$1`, s.cleanup).Scan(&completed, &objectDeleted)
		if err != nil || !completed {
			return err
		}
		if !objectDeleted {
			return errors.New("cleanup checkpoint reached before real Object deletion")
		}
		if !s.armed.CompareAndSwap(false, true) {
			return nil
		}
		var pid int32
		if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		return s.proxy.Arm(pid)
	})
	if matched && result.State() == f.Unknown {
		select {
		case s.original <- result:
		default:
		}
	}
	return result
}

func TestKnowledgeB02CleanupCommitUnknown(t *testing.T) {
	for _, commit := range []bool{false, true} {
		outcome := "not_forwarded"
		if commit {
			outcome = "committed_ack_lost"
		}
		t.Run(outcome, func(t *testing.T) {
			x, observer, proxy := unknownPublicationFixture(t)
			actor := x.human(t)
			project := x.project(t, actor, true)
			document := publicationSeedContent(t, x, actor, project, "cleanup checkpoint bytes")
			preview, err := x.service.PrepareDeleteSubtree(knowledgeContext(t), actor, project, document.ID)
			if err != nil {
				t.Fatal(err)
			}
			meta := treeMeta(t)
			deleted, err := x.service.DeleteSubtree(knowledgeContext(t), actor, meta, project, document.ID, preview.Confirmation)
			if err != nil || !deleted.CleanupPending || len(deleted.DeletedIDs) != 1 {
				t.Fatal("real deletion did not establish exact cleanup work", err)
			}
			var cleanup string
			if err = observer.QueryRow(knowledgeContext(t), `SELECT id::text FROM agenteam_knowledge.object_cleanup
 WHERE project_id=$1 AND document_id=$2 AND object_id=$3 AND phase<>'completed'`, project.String(), document.ID.String(), document.ObjectID.String()).Scan(&cleanup); err != nil {
				t.Fatal("missing exact durable cleanup identity", err)
			}
			store := &knowledgeCleanupCommitStore{Store: x.raw, cleanup: cleanup, proxy: proxy, original: make(chan f.CommitResult, 1)}
			s, err := knowledge.New(store, x.deps)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				s.Stop()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := s.Drain(ctx); err != nil {
					t.Error("cleanup Unknown caller did not drain", err)
				}
			})
			ctx := knowledgeContext(t)
			returned, joined := make(chan error, 1), make(chan struct{})
			go func() { defer close(joined); returned <- s.RecoverCleanup(ctx) }()
			t.Cleanup(func() { proxy.Release(); runtimeAwait(t, joined) })
			runtimeAwait(t, proxy.Reached())
			var original f.CommitResult
			select {
			case original = <-store.original:
			case <-ctx.Done():
				t.Fatal("original Store cleanup Unknown was not observed")
			}
			d := original.Cause().Details()
			if original.State() != f.Unknown || original.AttemptID().Validate() != nil || original.Cause().Kind() != f.RecoveryCause || d.Owner != "knowledge.cleanup" || d.RecoveryRunID != cleanup || d.CheckpointRef != "" {
				t.Fatal("cleanup Unknown lost its original exact transaction cause")
			}
			if commit {
				proxy.Release()
				runtimeAwait(t, proxy.Committed())
				runtimeAwait(t, proxy.HeldJoined())
			} else {
				unknownRollbackWriter(t, observer, proxy)
			}
			runtimeAwait(t, joined)
			callErr := <-returned
			var fault *f.Fault
			if !errors.As(callErr, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.CauseID != original.AttemptID().String() {
				t.Fatal("cleanup public result replaced the native Unknown", callErr)
			}
			wantPhase := "object"
			if commit {
				wantPhase = "completed"
			}
			var phase, objectState string
			if err = observer.QueryRow(knowledgeContext(t), `SELECT c.phase,o.state FROM agenteam_knowledge.object_cleanup c
 JOIN agenteam_object.objects o ON o.id=c.object_id WHERE c.id=$1 AND c.object_id=$2`, cleanup, document.ObjectID.String()).Scan(&phase, &objectState); err != nil || phase != wantPhase || objectState != "deleted" {
				t.Fatal("actual checkpoint outcome was confused with physical deletion", phase, objectState, err)
			}
			if deletes, objectDeletes, _ := recoveryDeleteFacts(t, x, project); deletes != 1 || objectDeletes != 1 {
				t.Fatal("real physical deletion was not completed exactly once")
			}
			beforeEvents, beforeActivity := titleEventCount(t, x, project), x.activity(t, actor)
			if err = s.RecoverCleanup(knowledgeContext(t)); err != nil {
				t.Fatal("cleanup did not converge from original checkpoint outcome", err)
			}
			if err = s.RecoverCleanup(knowledgeContext(t)); err != nil {
				t.Fatal("completed cleanup did not replay safely", err)
			}
			if deletes, objectDeletes, pending := recoveryDeleteFacts(t, x, project); deletes != 1 || objectDeletes != 1 || pending != 0 || titleEventCount(t, x, project) != beforeEvents || !x.activity(t, actor).Equal(beforeActivity) {
				t.Fatal("checkpoint recovery repeated Object Audit or user facts")
			}
			digest, err := kc.DeleteDigest(actor, meta, project, document.ID, preview.Confirmation)
			if err != nil {
				t.Fatal(err)
			}
			lookup, err := s.LookupCommand(knowledgeContext(t), actor, kc.LookupRequest{ProjectID: project, Command: kc.DeleteSubtree, Key: meta.IdempotencyKey, SemanticDigest: digest})
			if err != nil || lookup.State != kc.Committed || lookup.Receipt == nil || len(lookup.Receipt.DeletedIDs) != 1 || lookup.Receipt.DeletedIDs[0] != document.ID {
				t.Fatal("cleanup checkpoint recovery changed the original delete receipt", err)
			}
		})
	}
}
