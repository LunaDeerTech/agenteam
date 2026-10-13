package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The original technical ledger is exercised by the existing controlled Store.
// Only the existing command/work facts and physical return are controlled here;
// this is not a PostgreSQL or full publication-success fixture.
type ownedPublicationStore struct {
	*knowledgeProjectPureStore
	publication      *publicationCheckpointStore
	request, header  []byte
	reservation      *publicationReservationExecutor
	lease            string
	claims, pointers int
}
type ownedPublicationExecutor struct {
	postgres.SQLExecutor
	owner *ownedPublicationStore
}

func (s *ownedPublicationStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	x, err := s.knowledgeProjectPureStore.InTx(tx)
	if err != nil {
		return nil, err
	}
	return &ownedPublicationExecutor{SQLExecutor: x, owner: s}, nil
}
func (x *ownedPublicationExecutor) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if strings.HasPrefix(q, "SELECT request,plan") {
		return publicationCheckpointRow(func(dest ...any) error {
			*dest[0].(*[]byte), *dest[1].(*[]byte) = x.owner.request, x.owner.header
			return nil
		})
	}
	if strings.Contains(q, "FROM agenteam_knowledge.publications") && x.owner.reservation != nil {
		return x.owner.reservation.QueryRow(ctx, q, args...)
	}
	if strings.Contains(q, "FROM agenteam_knowledge.work_claims") {
		if x.owner.publication.work.command.Validate() != nil {
			return publicationCheckpointRow(func(...any) error { return pgx.ErrNoRows })
		}
		row := x.owner.publication.QueryRow(ctx, q, args...)
		return publicationCheckpointRow(func(dest ...any) error {
			if err := row.Scan(dest...); err != nil {
				return err
			}
			if source := x.owner.publication.work.source; source != nil {
				value := source.String()
				*dest[2].(**string) = &value
			}
			return nil
		})
	}
	if strings.Contains(q, "FROM agenteam_knowledge.commands") {
		return x.owner.publication.QueryRow(ctx, q, args...)
	}
	return x.SQLExecutor.QueryRow(ctx, q, args...)
}
func (x *ownedPublicationExecutor) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(q, "INSERT INTO agenteam_knowledge.work_claims") {
		if len(args) != 6 || args[0] != x.owner.publication.record.id.String() || args[1] != x.owner.publication.record.project.String() || args[2] != nil {
			return pgconn.CommandTag{}, errors.New("foreign candidate work")
		}
		process, err := f.ParseID[ob.Process](args[3].(string))
		if err != nil {
			return pgconn.CommandTag{}, err
		}
		attempt, err := f.ParseID[publicationAttempt](args[4].(string))
		if err != nil {
			return pgconn.CommandTag{}, err
		}
		x.owner.publication.work = publicationWork{command: x.owner.publication.record.id, project: x.owner.publication.record.project, process: process, attempt: attempt, fence: args[5].(int64), phase: "active"}
		x.owner.claims++
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	if strings.Contains(q, "UPDATE agenteam_knowledge.publications SET source_lease_id=NULL") {
		if len(args) != 2 || args[0] != x.owner.publication.record.id.String() || args[1] != x.owner.lease || x.owner.lease == "" {
			return pgconn.CommandTag{}, errors.New("foreign source pointer cleanup")
		}
		x.owner.lease = ""
		x.owner.pointers++
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	if strings.Contains(q, "UPDATE agenteam_knowledge.work_claims SET phase='joined'") {
		p := x.owner.publication
		if len(args) == 5 {
			if args[1] != p.work.project.String() {
				return pgconn.CommandTag{}, errors.New("foreign publication project")
			}
			args = []any{args[0], args[2], args[3], args[4]}
		}
		return p.Exec(ctx, q, args...)
	}
	return x.SQLExecutor.Exec(ctx, q, args...)
}
func awaitOwnedPublication(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("original publication return did not reach barrier")
	}
}

func TestKnowledgeOwnedPublicationCheckpointUnknownReturn(t *testing.T) {
	for _, committed := range []bool{true, false} {
		name := "committed"
		if !committed {
			name = "rolled-back"
		}
		t.Run(name, func(t *testing.T) {
			v := newKnowledgeProjectPureFixture(t)
			s := v.pair.Service()
			state := v.pair.state
			user, err := f.ParseID[id.User](v.actor.Details().UserID)
			if err != nil {
				t.Fatal(err)
			}
			record := &commandRecord{id: newID[command](t), project: v.a, document: v.document, user: user, name: kc.Create, key: "owned-checkpoint", digest: ob.DigestBytes([]byte("original publication")), state: kc.InProgress}
			record.created, _ = f.NewInstant(time.Now())
			process, err := f.ParseID[ob.Process](state.process.String())
			if err != nil {
				t.Fatal(err)
			}
			work, err := nextPublicationWork(record, nil, process, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			originalStore := v.store
			store := &ownedPublicationStore{knowledgeProjectPureStore: originalStore, publication: &publicationCheckpointStore{record: record, work: work}}
			s.state().store = store
			state.store = store
			state.resolver.state().store = store
			s.state().deps.Processes = &publicationProcesses{current: process}
			identity := projectCallIdentity{Format: 1, Method: "create", User: user.String(), Document: record.document.String(), Command: "create", Key: string(record.key), Digest: record.digest}
			run, entry, err := s.beginProjectCall(withProjectCallIdentity(context.Background(), identity), v.actor, v.a, nil, projectCallCommand)
			if err != nil {
				t.Fatal(err)
			}
			r, err := s.publicationRetirement(work, func() {})
			if err != nil {
				t.Fatal(err)
			}
			r.originalCall = run
			if err = r.join(run); err != nil {
				t.Fatal(err)
			}
			first := make(chan struct{})
			release := make(chan struct{})
			var openOnce sync.Once
			open := func() { openOnce.Do(func() { close(release) }) }
			var original f.CommitResult
			attempt := newID[f.TransactionAttempt](t)
			masked := false
			originalStore.result = func(x *knowledgeProjectPureTx, result f.CommitResult) (f.CommitResult, bool) {
				if x.cause.Kind() == f.CommandsCause && !masked && result.State() == f.Committed {
					masked = true
					original = f.UnknownResult(attempt, x.cause)
					if !committed {
						store.publication.work.phase = "active"
					}
					return original, committed
				}
				return result, result.State() == f.Committed
			}
			originalStore.tail = func(x *knowledgeProjectPureTx) {
				if x.cause.Kind() == f.CommandsCause {
					close(first)
					<-release
				}
			}
			returned := make(chan error, 1)
			finished := false
			t.Cleanup(func() {
				open()
				if !finished {
					select {
					case <-returned:
					case <-time.After(time.Second):
						t.Error("first original Store did not return")
					}
				}
			})
			go func() {
				err := s.checkpointOwnedPublicationJoin(run, v.actor, record, work, r)
				s.finishProjectCall(entry, &err)
				if err != nil {
					transfer := s.deferPublicationRetirement(r, func() { _ = s.projectCallReturned(entry) }, err)
					if transfer != nil {
						err = errors.Join(err, transfer)
					}
				} else {
					err = s.projectCallReturned(entry)
				}
				returned <- err
			}()
			awaitOwnedPublication(t, first)
			actor, cause, scope := v.stopIdentity(t, v.a)
			report, err := v.pair.InspectProjectStop(context.Background(), actor, cause, scope)
			if err != nil || report.Details().State != pc.StopPending {
				t.Fatal("Store callback was treated as original return", err)
			}
			r.mu.Lock()
			early := r.joinCheckpoint
			r.mu.Unlock()
			state.mu.Lock()
			stillOwned := state.owners[entry.owner.record.ID] == entry.owner && !entry.owner.returned
			state.mu.Unlock()
			if early != nil || !stillOwned {
				t.Fatal("unreturned checkpoint lost original owner")
			}
			open()
			err = <-returned
			finished = true
			var raw commitFailure
			if !errors.As(err, &raw) || raw.result.AttemptID() != original.AttemptID() || raw.result.Cause().Details().Primary.Canonical() != original.Cause().Details().Primary.Canonical() {
				t.Fatal("normal wrapper lost first raw Unknown", err)
			}
			originalStore.result = nil
			originalStore.tail = nil
			r.mu.Lock()
			retained := r.joinCheckpoint
			r.mu.Unlock()
			if retained == nil || retained.AttemptID() != original.AttemptID() {
				t.Fatal("normal wrapper did not retain physical result")
			}
			before := store.publication.updates
			_, _, writes := originalStore.counts()
			report, err = v.pair.InspectProjectStop(context.Background(), actor, cause, scope)
			_, _, afterWrites := originalStore.counts()
			if err != nil || report.Details().State != pc.StopPending || writes != afterWrites || store.publication.updates != before {
				t.Fatal("Inspect wrote or retired original checkpoint", err)
			}
			confirm := make(chan struct{})
			confirmRelease := make(chan struct{})
			var confirmOnce sync.Once
			unblock := func() { confirmOnce.Do(func() { close(confirmRelease) }) }
			confirmed := make(chan error, 1)
			joined := false
			originalStore.tail = func(x *knowledgeProjectPureTx) {
				if x.cause.Kind() == f.CommandsCause {
					close(confirm)
					<-confirmRelease
				}
			}
			t.Cleanup(func() {
				unblock()
				if !joined {
					select {
					case <-confirmed:
					case <-time.After(time.Second):
						t.Error("confirmation original return remained held")
					}
				}
			})
			go func() { confirmed <- s.retryPublicationRetirements(context.Background(), work.command) }()
			awaitOwnedPublication(t, confirm)
			// The projector holds r.mu through the actual Store return; inspect the
			// original owner's independent lifetime rather than blocking on that mutex.
			state.mu.Lock()
			stillOwned = state.owners[entry.owner.record.ID] == entry.owner && !entry.owner.returned
			state.mu.Unlock()
			if !stillOwned || store.publication.updates != before {
				t.Fatal("confirmation callback claimed actual return")
			}
			unblock()
			err = <-confirmed
			joined = true
			knowledgeProjectAssertFault(t, err, f.ResourceBusy)
			originalStore.tail = nil
			if store.publication.updates != before {
				t.Fatal("read confirmation wrote work")
			}
			if err = s.retryPublicationRetirements(context.Background(), work.command); err != nil {
				t.Fatal("next explicit original recovery did not close", err)
			}
			state.mu.Lock()
			_, present := state.owners[entry.owner.record.ID]
			state.mu.Unlock()
			if present || !entry.owner.returned || store.publication.work.phase != "joined" {
				t.Fatal("actual projection/technical return did not retire original owner")
			}
		})
	}
}

func newOwnedPublicationGraph(t *testing.T, v *knowledgeProjectPureFixture, source kc.SourceInput) (*ownedPublicationStore, contentInput, contentIntent, context.Context, *projectCallEntry) {
	t.Helper()
	meta := f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: "owned-publication"}
	input, err := createContentInput(v.actor, meta, kc.CreateRequest{ProjectID: v.a, DocumentID: v.document, Title: "Original"}, source)
	if err != nil {
		t.Fatal(err)
	}
	user, err := f.ParseID[id.User](v.actor.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	created, _ := f.NewInstant(time.Now())
	record := &commandRecord{id: newID[command](t), project: v.a, document: v.document, user: user, name: kc.Create, key: meta.IdempotencyKey, digest: input.digest, state: kc.InProgress, created: created}
	header, err := newContentHeader(v.a, v.document, 1, created)
	if err != nil {
		t.Fatal(err)
	}
	requestRaw, err := json.Marshal(input.request)
	if err != nil {
		t.Fatal(err)
	}
	headerRaw, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	store := &ownedPublicationStore{knowledgeProjectPureStore: v.store, publication: &publicationCheckpointStore{record: record}, request: requestRaw, header: headerRaw}
	s := v.pair.Service()
	s.state().store, v.pair.state.store, v.pair.state.resolver.state().store = store, store, store
	process, err := f.ParseID[ob.Process](v.pair.state.process.String())
	if err != nil {
		t.Fatal(err)
	}
	s.state().deps.Processes = &publicationProcesses{current: process}
	identity := projectCallIdentity{Format: 1, Method: "create", User: user.String(), Document: v.document.String(), Command: "create", Key: string(meta.IdempotencyKey), Digest: input.digest}
	origin, err := input.request.Source.sourceProject()
	if err != nil {
		t.Fatal(err)
	}
	run, entry, err := s.beginProjectCall(withProjectCallIdentity(context.Background(), identity), v.actor, v.a, origin, projectCallCommand)
	if err != nil {
		t.Fatal(err)
	}
	return store, input, contentIntent{record: record, request: input.request, header: header}, run, entry
}

func TestKnowledgeOwnedClaimRollbackRequiresOriginalBeforeConfirmation(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		name := "known-not-committed"
		if uncertain {
			name = "unknown"
		}
		t.Run(name, func(t *testing.T) {
			v := newKnowledgeProjectPureFixture(t)
			source, err := kc.NewTextSource(kc.PlainText, "original")
			if err != nil {
				t.Fatal(err)
			}
			store, input, intent, run, entry := newOwnedPublicationGraph(t, v, source)
			s := v.pair.Service()
			before, err := nextPublicationWork(intent.record, nil, s.state().deps.Processes.CurrentProcess(), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			before.phase = "joined"
			store.publication.work = before
			refusal := errors.New("original claim transaction rolled back")
			attempt := newID[f.TransactionAttempt](t)
			var original f.CommitResult
			v.store.result = func(tx *knowledgeProjectPureTx, normal f.CommitResult) (f.CommitResult, bool) {
				if tx.cause.Kind() == f.CommandsCause && store.claims == 1 {
					store.publication.work = before.clone()
					original = f.NotCommittedResult(fault(f.DependencyUnavailable).WithCause(refusal))
					if uncertain {
						original = f.UnknownResult(attempt, tx.cause)
					}
					return original, false
				}
				return normal, normal.State() == f.Committed
			}
			work, replay, claimErr := s.claimContentPublication(run, input, intent)
			if replay != nil || claimErr == nil || work.command != intent.record.id || work.attempt == before.attempt || work.fence != before.fence+1 || store.claims != 1 || !store.publication.work.equal(before) {
				t.Fatal("actual failed claim did not preserve original candidate and preimage", claimErr)
			}
			if uncertain {
				knowledgeProjectAssertUnknown(t, claimErr, original)
				actual, _ := projectCallRawError(claimErr)
				if actual.Cause().Details().Primary.Canonical() != original.Cause().Details().Primary.Canonical() {
					t.Fatal("claim Unknown changed original command")
				}
			} else if !errors.Is(claimErr, refusal) {
				t.Fatal("known failed claim lost original cause", claimErr)
			}
			v.store.result = nil
			r, err := s.publicationRetirement(work, func() {})
			if err != nil {
				t.Fatal(err)
			}
			r.originalCall = run
			if r.claimProof == nil || r.claimProof.before == nil || !r.claimProof.before.equal(before) || !r.claimProof.work.equal(work) {
				t.Fatal("actual claim return did not transfer the complete original before image")
			}
			if err = r.join(run); err != nil {
				t.Fatal(err)
			}
			transactions, _, writes := v.store.counts()
			checkpointErr := s.checkpointOwnedPublicationJoin(run, v.actor, intent.record, work, r)
			if uncertain {
				knowledgeProjectAssertUnknown(t, checkpointErr, original)
			} else if !errors.Is(checkpointErr, refusal) {
				t.Fatal("checkpoint replaced original failed claim", checkpointErr)
			}
			afterTx, _, afterWrites := v.store.counts()
			if afterTx != transactions || afterWrites != writes || store.publication.updates != 0 {
				t.Fatal("failed candidate started a join projection before confirming its claim")
			}
			s.finishProjectCall(entry, &claimErr)
			if err = s.deferPublicationRetirement(r, func() { _ = s.projectCallReturned(entry) }, claimErr); err != nil {
				t.Fatal(err)
			}
			// Same command/fence shape is insufficient: the complete original
			// prior work must still match, including its exact attempt identity.
			store.publication.work.attempt = newID[publicationAttempt](t)
			err = s.retryPublicationRetirements(context.Background(), work.command)
			if err == nil || r.claimProof.confirmed {
				t.Fatal("changed pre-claim work was treated as confirmed rollback", err)
			}
			if uncertain {
				knowledgeProjectAssertUnknown(t, err, original)
			} else if !errors.Is(err, refusal) {
				t.Fatal("failed confirmation replaced first claim refusal", err)
			}
			store.publication.work = before.clone()
			entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
			v.store.tail = func(tx *knowledgeProjectPureTx) {
				if tx.cause.Kind() == f.CommandsCause {
					close(entered)
					<-release
				}
			}
			result := make(chan error, 1)
			go func() {
				defer close(joined)
				result <- s.retryPublicationRetirements(context.Background(), work.command)
			}()
			defer func() {
				contentReaderRelease(release)
				contentReaderAwait(t, joined, "claim confirmation original Store did not return")
			}()
			awaitOwnedPublication(t, entered)
			v.pair.state.mu.Lock()
			present := v.pair.state.owners[entry.owner.record.ID] == entry.owner && !entry.owner.returned
			v.pair.state.mu.Unlock()
			if !present || store.claims != 1 || store.publication.updates != 0 {
				t.Fatal("claim confirmation callback retired the parent or wrote")
			}
			contentReaderRelease(release)
			contentReaderAwait(t, joined, "claim confirmation was not returned")
			knowledgeProjectAssertFault(t, <-result, f.ResourceBusy)
			v.store.tail = nil
			if !r.claimProof.confirmed || !r.claimProof.rolledBack || !store.publication.work.equal(before) {
				t.Fatal("actual confirmation lost exact rolled-back preimage")
			}
			if err = s.retryPublicationRetirements(context.Background(), work.command); err != nil {
				t.Fatal("later explicit recovery did not retire the absent candidate", err)
			}
			v.pair.state.mu.Lock()
			_, present = v.pair.state.owners[entry.owner.record.ID]
			v.pair.state.mu.Unlock()
			if present || !entry.owner.returned || store.claims != 1 || store.publication.updates != 0 || !store.publication.work.equal(before) {
				t.Fatal("rollback recovery replaced original prior work or retained its returned parent")
			}
		})
	}
}

type ownedSourceCancelPort struct {
	oc.SourceReads
	store   *knowledgeProjectPureStore
	lease   oc.SourceLease
	cancels int
}

func (p *ownedSourceCancelPort) CancelSourceLease(_ context.Context, lease oc.SourceLease) error {
	p.store.mu.Lock()
	active := len(p.store.live)
	p.store.mu.Unlock()
	if active != 0 || lease.ID() != p.lease.ID() {
		return errors.New("source cancellation changed original lease or ran inside Tx")
	}
	p.cancels++
	return nil
}

func TestKnowledgeOwnedSourcePointerUnknownRetainsPublicationParent(t *testing.T) {
	v := newKnowledgeProjectPureFixture(t)
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: v.source, DocumentID: newID[kc.Document](t).String(), Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	source, err := kc.NewBusinessSource(ref)
	if err != nil {
		t.Fatal(err)
	}
	store, input, intent, run, entry := newOwnedPublicationGraph(t, v, source)
	s := v.pair.Service()
	work, err := nextPublicationWork(intent.record, &v.source, s.state().deps.Processes.CurrentProcess(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	store.publication.work = work
	owner, err := oc.NewObjectOwner(oc.Knowledge, ref.Details().DocumentID, v.source.String())
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := id.InProject(v.source)
	now, _ := f.NewInstant(time.Now())
	resolved, err := oc.NewResolvedSource(oc.ResolvedSourceDetails{Reference: ref, Owner: owner, Revision: 1, Meta: oc.ObjectMeta{ID: newID[oc.StoredObject](t), Scope: scope, MediaType: kc.PlainText, ByteSize: 0, SHA256: ob.DigestBytes(nil), State: oc.Available, Version: 1, CreatedAt: now}})
	if err != nil {
		t.Fatal(err)
	}
	measurement, err := checkBusinessResolution(*input.request.Source, resolved)
	if err != nil {
		t.Fatal(err)
	}
	bound := businessPublicationSource{resolved: resolved, description: *input.request.Source, measurement: measurement}
	rawSource, err := json.Marshal(input.request.Source)
	if err != nil {
		t.Fatal(err)
	}
	store.reservation = &publicationReservationExecutor{record: intent.record, source: rawSource, project: v.a.String(), document: v.document.String(), phase: "planned"}
	lease, err := oc.NewSourceLease(oc.NewSourceIssuer(), newID[oc.Lease](t))
	if err != nil {
		t.Fatal(err)
	}
	store.lease = lease.ID().String()
	port := &ownedSourceCancelPort{store: v.store, lease: lease}
	s.state().deps.SourceReads = port
	command, err := kc.CommandIdentity(v.a, input.name, input.meta.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	// The already-issued local lease below is a controlled port boundary.
	// This does not claim actual Object acquisition or its Project stop proof.
	item := &knowledgeProjectSource{originalCall: run, origin: f.NewTx(), command: command, record: *intent.record, actor: v.actor, work: work.clone(), source: bound, lease: lease, acquisitionReturned: true}
	sourceState := s.knowledgeSourceState()
	sourceState.sources = map[oc.LeaseID]*knowledgeProjectSource{lease.ID(): item}
	r, err := s.publicationRetirement(work, func() {})
	if err != nil {
		t.Fatal(err)
	}
	r.originalCall = run
	if err = r.ownContext(func(ctx context.Context) error { return s.cancelKnowledgeSource(ctx, lease) }); err != nil {
		t.Fatal(err)
	}
	prepared, err := oc.NewPreparedPayload(oc.PreparedDetails{ID: newID[oc.Payload](t), MediaType: measurement.Media, Length: int64(measurement.Length), SHA256: measurement.SHA})
	if err != nil {
		t.Fatal(err)
	}
	attempt := newID[f.TransactionAttempt](t)
	var original f.CommitResult
	v.store.result = func(tx *knowledgeProjectPureTx, normal f.CommitResult) (f.CommitResult, bool) {
		if tx.cause.Kind() == f.CommandsCause && normal.State() == f.Committed {
			original = f.UnknownResult(attempt, tx.cause)
			return original, true
		}
		return normal, normal.State() == f.Committed
	}
	entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
	v.store.tail = func(tx *knowledgeProjectPureTx) {
		if tx.cause.Kind() == f.CommandsCause {
			close(entered)
			<-release
		}
	}
	result := make(chan error, 1)
	go func() {
		defer close(joined)
		replay, err := s.closeBusinessPublicationLease(run, input, intent, work, bound, lease, prepared)
		if replay != nil {
			err = errors.New("source pointer cleanup fabricated receipt")
		}
		result <- err
	}()
	defer func() {
		contentReaderRelease(release)
		contentReaderAwait(t, joined, "source pointer original Store did not return")
	}()
	awaitOwnedPublication(t, entered)
	sourceState.mu.Lock()
	retained := item.retained
	sourceState.mu.Unlock()
	if retained || store.pointers != 1 || store.lease != "" || port.cancels != 1 {
		t.Fatal("pointer fixture did not distinguish physical commit and original return")
	}
	contentReaderRelease(release)
	contentReaderAwait(t, joined, "pointer acknowledgment not returned")
	closeErr := <-result
	knowledgeProjectAssertUnknown(t, closeErr, original)
	actual, _ := projectCallRawError(closeErr)
	if actual.Cause().Details().Primary.Canonical() != command.Canonical() {
		t.Fatal("pointer Unknown changed original publication command")
	}
	v.store.result = nil
	v.store.tail = nil
	joinErr := r.join(context.Background())
	knowledgeProjectAssertUnknown(t, joinErr, original)
	if r.joined || port.cancels != 1 || store.pointers != 1 {
		t.Fatal("normal retirement discarded pointer Unknown or repeated cancellation")
	}
	s.finishProjectCall(entry, &closeErr)
	if err = s.deferPublicationRetirement(r, func() { _ = s.projectCallReturned(entry) }, closeErr); err != nil {
		t.Fatal(err)
	}
	err = s.retryPublicationRetirements(context.Background(), work.command)
	knowledgeProjectAssertUnknown(t, err, original)
	v.pair.state.mu.Lock()
	present := v.pair.state.owners[entry.owner.record.ID] == entry.owner && !entry.owner.returned
	v.pair.state.mu.Unlock()
	if !present || r.joined || port.cancels != 1 || store.pointers != 1 {
		t.Fatal("source pointer Unknown released original publication parent")
	}
	v.pair.Stop()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err = v.pair.Drain(cancelled); !errors.Is(err, context.Canceled) || v.pair.Joined() {
		t.Fatal("cancellation became source publication join", err)
	}
}
