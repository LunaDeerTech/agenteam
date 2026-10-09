package knowledge

import (
	"context"
	"errors"
	"io"
	"sync"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// A returned stream keeps the service call registered until the underlying
// Object reader actually closes successfully. Cancellation alone is not join.
type trackedRead struct {
	body *oc.ObjectReader
	done func()
	once sync.Once
}

func (r *trackedRead) Read(p []byte) (int, error) { return r.body.Read(p) }
func (r *trackedRead) Close() error {
	err := r.body.Close()
	if err == nil {
		r.once.Do(r.done)
	}
	return err
}

func (s *Service) OpenCanonical(ctx context.Context, actor id.Actor, project id.ProjectID, document kc.DocumentID, byteRange *oc.ByteRange) (kc.CanonicalRead, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return kc.CanonicalRead{}, err
	}
	if document.Validate() != nil {
		return kc.CanonicalRead{}, fault(f.InvalidArgument)
	}
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return kc.CanonicalRead{}, err
	}
	owned := true
	defer func() {
		if owned {
			done()
		}
	}()
	head, err := s.GetDocument(ctx, actor, project, document)
	if err != nil {
		return kc.CanonicalRead{}, err
	}
	if head.Active == nil {
		return kc.CanonicalRead{}, fault(f.NotFound)
	}
	owner, err := oc.NewObjectOwner(oc.Knowledge, document.String(), project.String())
	if err != nil {
		return kc.CanonicalRead{}, internal(err)
	}
	reader, err := s.state().deps.Objects.ReadObject(ctx, actor, owner, head.Active.ObjectID, byteRange)
	if err != nil {
		return kc.CanonicalRead{}, portError(err)
	}
	// Once a reader exists, an unsuccessful Close must not remove its active
	// call. A subsequent process shutdown may observe the outstanding work.
	owned = false
	tracked := &trackedRead{body: reader, done: done}
	closeFailure := func(primary error) (kc.CanonicalRead, error) {
		return kc.CanonicalRead{}, errors.Join(primary, portError(tracked.Close()))
	}
	if reader == nil {
		return closeFailure(internal(nil))
	}
	err = s.read(ctx, actor, project, func(ctx context.Context, x postgres.SQLExecutor) error {
		current, err := loadDocument(ctx, x, project, document)
		if err != nil {
			return err
		}
		if current.head.Active == nil || current.head.Active.ContentVersion != head.Active.ContentVersion || current.head.Active.ObjectID != head.Active.ObjectID {
			return fault(f.VersionConflict)
		}
		return nil
	})
	if err != nil {
		return closeFailure(err)
	}
	wrapped, err := oc.NewObjectReader(reader.Meta(), reader.Range(), tracked)
	if err != nil {
		return closeFailure(internal(err))
	}
	result, err := kc.NewCanonicalRead(*head.Active, wrapped)
	if err != nil {
		return closeFailure(internal(err))
	}
	return result, nil
}

func readText(reader io.Reader, request kc.ReadRequest) (kc.TextContent, error) {
	if request.Validate() != nil {
		return kc.TextContent{}, fault(f.InvalidArgument)
	}
	if request.ByteOffset > 0 {
		if _, err := io.CopyN(io.Discard, reader, int64(request.ByteOffset)); err != nil {
			if errors.Is(err, io.EOF) {
				return kc.TextContent{}, fault(f.InvalidArgument)
			}
			return kc.TextContent{}, portError(err)
		}
	}
	// Extra bytes distinguish EOF from truncation and complete the code point
	// straddling the requested byte budget without reading an unbounded body.
	raw, err := io.ReadAll(io.LimitReader(reader, int64(request.MaxBytes+utf8.UTFMax)))
	if err != nil {
		return kc.TextContent{}, portError(err)
	}
	n := 0
	for n < len(raw) && n < request.MaxBytes {
		r, size := utf8.DecodeRune(raw[n:])
		if r == utf8.RuneError && size == 1 {
			return kc.TextContent{}, fault(f.InvalidArgument)
		}
		if n+size > request.MaxBytes {
			break
		}
		n += size
	}
	return kc.TextContent{Text: string(raw[:n]), NextByteOffset: request.ByteOffset + f.Progress(n), Truncated: n < len(raw)}, nil
}

func (s *Service) ReadDocument(ctx context.Context, actor id.Actor, project id.ProjectID, document kc.DocumentID, request kc.ReadRequest) (kc.DocumentContent, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return kc.DocumentContent{}, err
	}
	if document.Validate() != nil || request.Validate() != nil {
		return kc.DocumentContent{}, fault(f.InvalidArgument)
	}
	head, err := s.GetDocument(ctx, actor, project, document)
	if err != nil {
		return kc.DocumentContent{}, err
	}
	if head.Active == nil {
		return kc.DocumentContent{}, fault(f.NotFound)
	}
	if head.Active.SourceKind == kc.File {
		unbound := kc.ReadableUnbound
		return kc.DocumentContent{Document: *head.Active, Unavailable: &unbound}, nil
	}
	reader, err := s.OpenCanonical(ctx, actor, project, document, nil)
	if err != nil {
		return kc.DocumentContent{}, err
	}
	// OpenCanonical may observe a newer current version between these two
	// independently authorized operations. Its exact returned metadata wins.
	if reader.Document().SourceKind != kc.Text {
		err = reader.Close()
		if err != nil {
			return kc.DocumentContent{}, portError(err)
		}
		unbound := kc.ReadableUnbound
		return kc.DocumentContent{Document: reader.Document(), Unavailable: &unbound}, nil
	}
	content, readErr := readText(reader, request)
	closeErr := reader.Close()
	if err = errors.Join(readErr, portError(closeErr)); err != nil {
		return kc.DocumentContent{}, err
	}
	return kc.DocumentContent{Document: reader.Document(), Text: &content}, nil
}

var _ kc.CanonicalReads = (*Service)(nil)

// SourceResolver binds exact Knowledge revisions. Other business variants
// belong to their real domain providers in the outer source router.
type SourceResolver struct{ data func() *sourceState }
type sourceState struct {
	store     Store
	authority *Authority
	objects   oc.Objects
}

func NewSourceResolver(store Store, authority *Authority, objects oc.Objects) (*SourceResolver, error) {
	if nilPort(store) || authority.state() == nil || !sameStore(store, authority.state().store) || nilPort(objects) {
		return nil, fault(f.DependencyUnbound)
	}
	st := &sourceState{store: store, authority: authority, objects: objects}
	return &SourceResolver{data: func() *sourceState { return st }}, nil
}
func (r *SourceResolver) state() *sourceState {
	if r == nil || r.data == nil {
		return nil
	}
	return r.data()
}

func sourceOwner(reference oc.BusinessFileRef) (oc.ObjectOwner, error) {
	if reference.Validate() != nil {
		return oc.ObjectOwner{}, fault(f.InvalidArgument)
	}
	d := reference.Details()
	if d.Kind != oc.KnowledgeFile {
		return oc.ObjectOwner{}, fault(f.DependencyUnbound)
	}
	return oc.NewObjectOwner(oc.Knowledge, d.DocumentID, d.ProjectID.String())
}

func (r *SourceResolver) sourceInTx(ctx context.Context, tx f.Tx, actor id.Actor, reference oc.BusinessFileRef) (kc.DocumentRef, error) {
	st := r.state()
	if st == nil {
		return kc.DocumentRef{}, fault(f.DependencyUnbound)
	}
	owner, err := sourceOwner(reference)
	if err != nil {
		return kc.DocumentRef{}, err
	}
	x, project, document, err := st.authority.ownerScope(ctx, tx, actor, owner, id.Read)
	if err != nil {
		return kc.DocumentRef{}, err
	}
	row, err := loadDocument(ctx, x, project, document)
	if err != nil {
		return kc.DocumentRef{}, err
	}
	if row.head.Active == nil {
		return kc.DocumentRef{}, fault(f.NotFound)
	}
	if row.head.Active.ContentVersion != reference.Details().Revision {
		return kc.DocumentRef{}, fault(f.VersionConflict)
	}
	return *row.head.Active, nil
}

func (r *SourceResolver) sourceSnapshot(ctx context.Context, actor id.Actor, reference oc.BusinessFileRef) (kc.DocumentRef, error) {
	st := r.state()
	if st == nil {
		return kc.DocumentRef{}, fault(f.DependencyUnbound)
	}
	if _, err := sourceOwner(reference); err != nil {
		return kc.DocumentRef{}, err
	}
	project := reference.Details().ProjectID
	if err := readInput(ctx, actor, project); err != nil {
		return kc.DocumentRef{}, err
	}
	locks, err := scopeLocks(actor, project, false)
	if err != nil {
		return kc.DocumentRef{}, err
	}
	cause, err := readCause()
	if err != nil {
		return kc.DocumentRef{}, err
	}
	var out kc.DocumentRef
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		var err error
		out, err = r.sourceInTx(ctx, tx, actor, reference)
		return err
	})
	if err = txError(result); err != nil {
		return kc.DocumentRef{}, err
	}
	return out, nil
}

func (r *SourceResolver) Resolve(ctx context.Context, actor id.Actor, reference oc.BusinessFileRef) (oc.ResolvedSource, error) {
	doc, err := r.sourceSnapshot(ctx, actor, reference)
	if err != nil {
		return oc.ResolvedSource{}, err
	}
	owner, err := sourceOwner(reference)
	if err != nil {
		return oc.ResolvedSource{}, err
	}
	meta, err := r.state().objects.StatObject(ctx, actor, owner, doc.ObjectID)
	if err != nil {
		return oc.ResolvedSource{}, portError(err)
	}
	current, err := r.sourceSnapshot(ctx, actor, reference)
	if err != nil {
		return oc.ResolvedSource{}, err
	}
	if current.ObjectID != doc.ObjectID {
		return oc.ResolvedSource{}, fault(f.VersionConflict)
	}
	if meta.ID != current.ObjectID || meta.MediaType != current.MediaType {
		return oc.ResolvedSource{}, internal(nil)
	}
	return oc.NewResolvedSource(oc.ResolvedSourceDetails{Reference: reference, Owner: owner, Meta: meta, Revision: current.ContentVersion})
}

func (r *SourceResolver) ValidateInTx(ctx context.Context, tx f.Tx, actor id.Actor, source oc.ResolvedSource, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	st := r.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	if source.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	owner, err := sourceOwner(source.Details().Reference)
	if err != nil {
		return err
	}
	if !owner.Equal(source.Details().Owner) {
		return fault(f.InvalidArgument)
	}
	request, err := oc.NewSourceAccess(oc.AccessRequestDetails{Operation: plan.Details().Request.Details().Operation, Actor: actor, Source: source})
	if err != nil {
		return portError(err)
	}
	// The Object issuer checks the exact source request, caller Tx, token and
	// full held union. No new discovery, transaction, locks or network I/O here.
	if err = st.objects.ValidateAccessPlanInTx(ctx, tx, request, plan, locked); err != nil {
		return portError(err)
	}
	doc, err := r.sourceInTx(ctx, tx, actor, source.Details().Reference)
	if err != nil {
		return err
	}
	d := source.Details()
	if doc.ObjectID != d.Meta.ID || doc.ContentVersion != d.Revision || doc.MediaType != d.Meta.MediaType {
		return f.NewFault(f.ResourceBusy, f.NotCommitted)
	}
	return nil
}

var _ oc.SourceResolver = (*SourceResolver)(nil)

// businessPublicationSource binds one exact reference to its real resolver
// result. It is not permission to read bytes; only a confirmed SourceReads
// lease may do that. The final canonical transaction must revalidate it.
type businessPublicationSource struct {
	resolved    oc.ResolvedSource
	description publicationSource
	measurement publicationMeasurement
}

func checkBusinessResolution(expected publicationSource, resolved oc.ResolvedSource) (publicationMeasurement, error) {
	if expected.validate() != nil || expected.Kind != kc.InputBusinessFile || resolved.Validate() != nil {
		return publicationMeasurement{}, internal(nil)
	}
	d := resolved.Details()
	actual, err := kc.NewBusinessSource(d.Reference)
	if err != nil {
		return publicationMeasurement{}, internal(err)
	}
	description, err := describePublicationSource(actual)
	if err != nil || !publicationSourceEqual(expected, description) {
		return publicationMeasurement{}, fault(f.ResourceBusy)
	}
	if _, err = publicationMedia(d.Meta.MediaType); err != nil {
		return publicationMeasurement{}, err
	}
	measurement := publicationMeasurement{Media: d.Meta.MediaType, Length: d.Meta.ByteSize, SHA: d.Meta.SHA256}
	if err = measurement.validate(); err != nil {
		return publicationMeasurement{}, err
	}
	return measurement, nil
}

func (s *Service) resolveBusinessPublication(ctx context.Context, input contentInput, intent contentIntent, source kc.SourceInput, work publicationWork, retirement *publicationRetirement) (businessPublicationSource, error) {
	if ctx == nil || s.state() == nil || retirement == nil || retirement.service != s || !retirement.work.equal(work) || intent.record == nil || intent.record.id != work.command || intent.record.project != input.project || intent.record.document != input.document || intent.record.digest != input.digest || intent.record.key != input.meta.IdempotencyKey || intent.record.user.String() != input.actor.Details().UserID || work.project != input.project || work.process != s.state().deps.Processes.CurrentProcess() || work.phase != "active" || input.request.Source == nil || input.request.Source.Kind != kc.InputBusinessFile {
		return businessPublicationSource{}, internal(nil)
	}
	retirement.mu.Lock()
	joined := retirement.joined
	retirement.mu.Unlock()
	if joined {
		return businessPublicationSource{}, fault(f.ResourceBusy)
	}
	if err := ctx.Err(); err != nil {
		return businessPublicationSource{}, unavailable(err)
	}
	actual, err := describePublicationSource(source)
	if err != nil || !publicationSourceEqual(actual, *input.request.Source) {
		return businessPublicationSource{}, fault(f.IdempotencyKeyReused)
	}
	origin, err := actual.sourceProject()
	if err != nil || !equalProjectPointer(origin, work.source) {
		return businessPublicationSource{}, fault(f.ResourceBusy)
	}
	ref, err := actual.Business.reference()
	if err != nil {
		return businessPublicationSource{}, err
	}
	resolved, err := s.state().deps.Sources.Resolve(ctx, input.actor, ref)
	if err != nil {
		return businessPublicationSource{}, portError(err)
	}
	measurement, err := checkBusinessResolution(actual, resolved)
	if err != nil {
		return businessPublicationSource{}, err
	}
	return businessPublicationSource{resolved: resolved, description: actual, measurement: measurement}, nil
}

func (source businessPublicationSource) validate(input contentInput) error {
	if input.request.Source == nil || !publicationSourceEqual(source.description, *input.request.Source) {
		return fault(f.ResourceBusy)
	}
	measurement, err := checkBusinessResolution(source.description, source.resolved)
	if err != nil || measurement != source.measurement {
		return fault(f.ResourceBusy)
	}
	return nil
}

// A returned handle is kept even if the transaction reports Unknown or the
// provider also reports an error. Retirement must cancel it outside its origin
// transaction. No OpenLeasedSource or external I/O is performed by this stage.
func (s *Service) acquireBusinessPublicationLease(ctx context.Context, input contentInput, intent contentIntent, work publicationWork, source businessPublicationSource, retirement *publicationRetirement) (lease oc.SourceLease, replay *kc.DocumentRef, err error) {
	if ctx == nil || s.state() == nil || intent.record == nil || work.command != intent.record.id || retirement == nil || retirement.service != s || !retirement.work.equal(work) {
		return lease, nil, internal(nil)
	}
	if err = source.validate(input); err != nil {
		return lease, nil, err
	}
	st := s.state()
	request, err := oc.NewSourceAccess(oc.AccessRequestDetails{Operation: oc.AcquireSourceAccess, Actor: input.actor, Source: source.resolved})
	if err != nil {
		return lease, nil, portError(err)
	}
	plan, err := st.deps.Objects.DiscoverAccess(ctx, request)
	if err != nil {
		return lease, nil, portError(err)
	}
	locks, err := publicationLocks(input.actor, intent.record, work.source)
	if err != nil {
		return lease, nil, err
	}
	identity, err := kc.CommandIdentity(input.project, input.name, input.meta.IdempotencyKey)
	if err != nil {
		return lease, nil, portError(err)
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return lease, nil, portError(err)
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locked, err := st.deps.Objects.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, locks)
		if err != nil {
			return portError(err)
		}
		x, record, _, err := s.currentContentIntent(ctx, tx, input, intent)
		if err != nil {
			return err
		}
		if record.state == kc.Committed {
			replay = record.receipt.Document
			return nil
		}
		if err = requirePublicationWork(ctx, x, work); err != nil {
			return err
		}
		publication, err := loadPublicationReservation(ctx, x, record)
		if err != nil {
			return err
		}
		if !publicationSourceEqual(publication.source, source.description) || publication.measurement != nil && *publication.measurement != source.measurement {
			return fault(f.ResourceBusy)
		}
		if err = st.deps.Sources.ValidateInTx(ctx, tx, input.actor, source.resolved, plan, locked); err != nil {
			return portError(err)
		}
		lease, err = st.deps.SourceReads.AcquireSourceInTx(ctx, tx, input.actor, source.resolved, plan, locked)
		if err != nil {
			return portError(err)
		}
		if lease.Validate() != nil {
			return internal(nil)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.publications SET source_lease_id=$2
 WHERE command_id=$1 AND phase IN ('planned','reserved','uploaded')`, record.id.String(), lease.ID().String())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ResourceBusy)
		}
		return nil
	})
	if lease.Validate() == nil {
		// The handle cannot be reconstructed from a persisted UUID. Its issuer
		// and actual originating Tx stay owned by the real SourceReads port.
		owned := lease
		if ownErr := retirement.ownContext(func(cleanup context.Context) error {
			return portError(st.deps.SourceReads.CancelSourceLease(cleanup, owned))
		}); ownErr != nil {
			return lease, replay, ownErr
		}
	}
	return lease, replay, txError(result)
}

// closeBusinessPublicationLease releases the source through its real adapter
// before clearing only this command's exact persisted lease. A failure/Unknown
// is not permission to continue to the final canonical transaction.
func (s *Service) closeBusinessPublicationLease(ctx context.Context, input contentInput, intent contentIntent, work publicationWork, source businessPublicationSource, lease oc.SourceLease, prepared oc.PreparedPayload) (*kc.DocumentRef, error) {
	if ctx == nil || s.state() == nil || intent.record == nil || work.command != intent.record.id || lease.Validate() != nil {
		return nil, internal(nil)
	}
	if err := source.validate(input); err != nil {
		return nil, err
	}
	measured, err := measurementOf(prepared)
	if err != nil || measured != source.measurement {
		return nil, fault(f.ResourceBusy)
	}
	st := s.state()
	if err = st.deps.SourceReads.CancelSourceLease(ctx, lease); err != nil {
		return nil, portError(err)
	}
	locks, err := publicationLocks(input.actor, intent.record, work.source)
	if err != nil {
		return nil, err
	}
	identity, err := kc.CommandIdentity(input.project, input.name, input.meta.IdempotencyKey)
	if err != nil {
		return nil, portError(err)
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return nil, portError(err)
	}
	var replay *kc.DocumentRef
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, record, _, err := s.currentContentIntent(ctx, tx, input, intent)
		if err != nil {
			return err
		}
		if record.state == kc.Committed {
			replay = record.receipt.Document
			return nil
		}
		if err = requirePublicationWork(ctx, x, work); err != nil {
			return err
		}
		publication, err := loadPublicationReservation(ctx, x, record)
		if err != nil {
			return err
		}
		if !publicationSourceEqual(publication.source, source.description) || publication.measurement != nil && *publication.measurement != measured {
			return fault(f.ResourceBusy)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.publications SET source_lease_id=NULL
 WHERE command_id=$1 AND source_lease_id=$2 AND phase IN ('planned','reserved','uploaded')`, record.id.String(), lease.ID().String())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ResourceBusy)
		}
		return nil
	})
	return replay, txError(result)
}

// Direct sources have no resolver gate. Business sources must carry the exact
// prior resolution, and acquire their final validation plan in the same union
// as every target mutation; this never obtains a newer reference implicitly.
func (s *Service) planPublicationSource(ctx context.Context, input contentInput, source *businessPublicationSource) (oc.AccessLockPlan, error) {
	business := input.request.Source != nil && input.request.Source.Kind == kc.InputBusinessFile
	if business != (source != nil) {
		return oc.AccessLockPlan{}, fault(f.DependencyUnbound)
	}
	if source == nil {
		return oc.AccessLockPlan{}, nil
	}
	if err := source.validate(input); err != nil {
		return oc.AccessLockPlan{}, err
	}
	request, err := oc.NewSourceAccess(oc.AccessRequestDetails{Operation: oc.AcquireSourceAccess, Actor: input.actor, Source: source.resolved})
	if err != nil {
		return oc.AccessLockPlan{}, portError(err)
	}
	plan, err := s.state().deps.Objects.DiscoverAccess(ctx, request)
	return plan, portError(err)
}

func (s *Service) validatePublicationSource(ctx context.Context, tx f.Tx, input contentInput, source *businessPublicationSource, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	business := input.request.Source != nil && input.request.Source.Kind == kc.InputBusinessFile
	if business != (source != nil) {
		return fault(f.DependencyUnbound)
	}
	if source == nil {
		return nil
	}
	if err := source.validate(input); err != nil {
		return err
	}
	return portError(s.state().deps.Sources.ValidateInTx(ctx, tx, input.actor, source.resolved, plan, locked))
}
