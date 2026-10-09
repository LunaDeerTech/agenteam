package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ev "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type commandEvent struct {
	command  *commandRecord
	document kc.DocumentID
	header   ev.Header
	payload  []byte
}

func eventProject(summary ev.Summary) (id.ProjectID, error) {
	h := summary.Header
	if summary.Producer != kc.KnowledgeProducer || h.Validate() != nil || summary.PayloadDigest.Validate() != nil || h.SchemaVersion != 1 || h.Scope.Kind != ev.ProjectScope || h.AggregateType != kc.KnowledgeAggregate || h.AggregateVersion == nil || h.AggregateSequence != nil || h.EventType != kc.ContentChangedEvent && h.EventType != kc.DeletedEvent {
		return id.ProjectID{}, fault(f.Forbidden)
	}
	project, err := f.ParseID[id.Project](h.Scope.ProjectID.String())
	if err != nil {
		return id.ProjectID{}, fault(f.Forbidden)
	}
	return project, nil
}

func loadCommandEvent(ctx context.Context, x postgres.SQLExecutor, eventID ev.EventID) (*commandEvent, error) {
	var commandID, project, document, kind string
	var header, payload []byte
	err := x.QueryRow(ctx, `SELECT command_id::text,project_id::text,document_id::text,event_type,header,payload
 FROM agenteam_knowledge.command_events WHERE id=$1`, eventID.String()).Scan(&commandID, &project, &document, &kind, &header, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fault(f.Forbidden)
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if len(header) > 4096 || len(payload) > 4096 {
		return nil, internal(nil)
	}
	id, err := f.ParseID[command](commandID)
	if err != nil {
		return nil, internal(err)
	}
	h, err := ev.DecodeHeader(header)
	if err != nil || h.EventID != eventID || string(h.EventType) != kind || h.Scope.ProjectID.String() != project || h.AggregateID.String() != document {
		return nil, internal(err)
	}
	k, err := f.ParseID[kc.Document](document)
	if err != nil {
		return nil, internal(err)
	}
	raw, err := cursor.CanonicalJSON(payload)
	if err != nil {
		return nil, internal(err)
	}
	record, err := scanCommand(x.QueryRow(ctx, `SELECT id::text,project_id::text,document_id::text,actor_user_id::text,
 command_name,command_key,semantic_digest,state,receipt,created_at,committed_at
 FROM agenteam_knowledge.commands WHERE project_id=$1 AND id=$2`, project, id.String()))
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, internal(nil)
	}
	return &commandEvent{record, k, h, raw}, nil
}

func eventBinding(actor id.Actor, summary ev.Summary, row *commandEvent) (f.Digest, []f.LockRequest, []byte, error) {
	project, err := eventProject(summary)
	if err != nil {
		return "", nil, nil, err
	}
	if actor.Validate() != nil || actor.Details().Kind != id.Human || row == nil || row.command == nil || row.command.user.String() != actor.Details().UserID || row.command.project != project || row.document.String() != summary.Header.AggregateID.String() || ob.DigestBytes(row.payload) != summary.PayloadDigest {
		return "", nil, nil, fault(f.Forbidden)
	}
	expected, err := json.Marshal(row.header)
	if err != nil {
		return "", nil, nil, internal(err)
	}
	actual, err := json.Marshal(summary.Header)
	if err != nil || !bytes.Equal(expected, actual) {
		return "", nil, nil, fault(f.Forbidden)
	}
	switch row.header.EventType {
	case kc.ContentChangedEvent:
		var payload kc.ContentChangedPayload
		if json.Unmarshal(row.payload, &payload) != nil || payload.Validate() != nil || payload.DocumentID != row.document || payload.ContentVersion != *row.header.AggregateVersion || row.command.document != row.document {
			return "", nil, nil, fault(f.Forbidden)
		}
		if row.command.name == kc.Create {
			if len(payload.Changes) != 1 || payload.Changes[0] != kc.ContentCreated {
				return "", nil, nil, fault(f.Forbidden)
			}
		} else if row.command.name != kc.Update || slices.Contains(payload.Changes, kc.ContentCreated) {
			return "", nil, nil, fault(f.Forbidden)
		}
	case kc.DeletedEvent:
		var payload kc.DeletedPayload
		if json.Unmarshal(row.payload, &payload) != nil || payload.Validate() != nil || payload.DocumentID != row.document || payload.ContentVersion != *row.header.AggregateVersion || row.command.name != kc.DeleteSubtree {
			return "", nil, nil, fault(f.Forbidden)
		}
	default:
		return "", nil, nil, fault(f.Forbidden)
	}
	identity, err := kc.CommandIdentity(project, row.command.name, row.command.key)
	if err != nil {
		return "", nil, nil, internal(err)
	}
	locks, err := scopeLocks(actor, project, true)
	if err != nil {
		return "", nil, nil, err
	}
	commandLock, err := f.CommandLock(identity)
	if err != nil {
		return "", nil, nil, internal(err)
	}
	locks, err = ob.NormalizeLocks(append(locks, f.LockRequest{Key: commandLock, Mode: f.Exclusive}))
	if err != nil {
		return "", nil, nil, portError(err)
	}
	// Include the exact Session, immutable command identity and full typed
	// summary. Opaque carries only the command UUID, never a key or content.
	raw, err := json.Marshal(struct {
		Domain   string
		Actor    id.ActorDetails
		Summary  ev.Summary
		Command  f.ID[command]
		Identity string
	}{"knowledge.event.v1", actor.Details(), summary, row.command.id, identity.Canonical()})
	if err != nil {
		return "", nil, nil, internal(err)
	}
	opaque, err := json.Marshal(row.command.id)
	if err != nil {
		return "", nil, nil, internal(err)
	}
	return ob.DigestBytes(raw), locks, opaque, nil
}

func (a *Authority) DiscoverAppend(ctx context.Context, actor id.Actor, summary ev.Summary) (ob.Dependencies, error) {
	st := a.state()
	if st == nil {
		return ob.Dependencies{}, fault(f.DependencyUnbound)
	}
	project, err := eventProject(summary)
	if err != nil {
		return ob.Dependencies{}, err
	}
	if err = readInput(ctx, actor, project); err != nil {
		return ob.Dependencies{}, err
	}
	row, err := loadCommandEvent(ctx, st.store, summary.Header.EventID)
	if err != nil {
		return ob.Dependencies{}, err
	}
	binding, locks, opaque, err := eventBinding(actor, summary, row)
	if err != nil {
		return ob.Dependencies{}, err
	}
	return ob.NewDependencies(st.eventIssuer, binding, locks, opaque)
}

func (a *Authority) ValidateAppendInTx(ctx context.Context, tx f.Tx, actor id.Actor, summary ev.Summary, deps ob.Dependencies, stage ob.Stage) error {
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	project, err := eventProject(summary)
	if err != nil {
		return err
	}
	if err = readInput(ctx, actor, project); err != nil {
		return err
	}
	if !stage.Valid() || deps.Validate() != nil || !deps.Matches(st.eventIssuer, deps.Binding()) {
		return fault(f.Forbidden)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, deps.Locks()); err != nil {
		return portError(err)
	}
	scope, err := scopeLocks(actor, project, true)
	if err != nil {
		return err
	}
	if err = st.store.RequireHeldLocks(ctx, tx, scope); err != nil {
		return portError(err)
	}
	grant, err := st.projects.RequireOwnerInTx(ctx, tx, actor, project, id.Read)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(actor, project) {
		return internal(nil)
	}
	row, err := loadCommandEvent(ctx, x, summary.Header.EventID)
	if err != nil {
		return err
	}
	binding, locks, opaque, err := eventBinding(actor, summary, row)
	if err != nil {
		return err
	}
	if !deps.Matches(st.eventIssuer, binding) || !bytes.Equal(deps.Opaque(), opaque) || !slices.EqualFunc(deps.Locks(), locks, func(a, b f.LockRequest) bool { return a.Key.Canonical() == b.Key.Canonical() && a.Mode == b.Mode }) {
		return fault(f.Forbidden)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if stage == ob.CurrentAccess {
		return nil
	}
	if row.command.state != kc.InProgress {
		return fault(f.Forbidden)
	}
	grant, err = st.projects.RequireOwnerInTx(ctx, tx, actor, project, id.Mutate)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(actor, project) {
		return internal(nil)
	}
	current, err := loadDocument(ctx, x, project, row.document)
	if err != nil {
		return err
	}
	if summary.Header.EventType == kc.DeletedEvent {
		if current.head.Deleted == nil || current.head.Deleted.ContentVersion != *summary.Header.AggregateVersion {
			return fault(f.Forbidden)
		}
		return nil
	}
	var payload kc.ContentChangedPayload
	if json.Unmarshal(row.payload, &payload) != nil || current.head.Active == nil || current.head.Active.ContentVersion != payload.ContentVersion || current.head.Active.ObjectID != payload.ObjectID || current.head.Active.IndexingStatus != kc.IndexPending {
		return fault(f.Forbidden)
	}
	return nil
}

var _ ob.ProducerAuthority = (*Authority)(nil)
