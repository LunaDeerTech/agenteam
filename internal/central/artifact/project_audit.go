package artifact

import (
	"bytes"
	"context"
	"reflect"
	"sync/atomic"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// ProjectAuditAuthority checks Artifact facts only. Project independently checks
// the current Human Session, Owner and lifecycle in this same transaction.
type ProjectAuditAuthority struct{ store Store }

func NewProjectAuditAuthority(store Store) (*ProjectAuditAuthority, error) {
	if !sameProjectAuditStore(store, store) {
		return nil, failure(f.DependencyUnbound, nil)
	}
	return &ProjectAuditAuthority{store: store}, nil
}

type projectAuditWitnessKey struct{}
type projectAuditCommand struct {
	identity f.CommandIdentity
	digest   f.Digest
	path     string
	cause    string
}
type projectAuditWitness struct {
	active   atomic.Bool
	store    Store
	tx       f.Tx
	entry    au.Entry
	key      au.AppendKey
	meta     ac.Metadata
	command  *projectAuditCommand
	download *projectAuditDownload
}
type projectAuditDownload struct {
	target oc.DownloadTarget
	event  oc.DownloadEvent
}

// Sources reaches this only after validating the original target/plan. The
// checker also consumes Object's private issuing scope and actual grant row.
func (s *Sources) appendDownloadIssuedAudit(ctx context.Context, tx f.Tx, entry au.Entry, key au.AppendKey, target oc.DownloadTarget, event oc.DownloadEvent) error {
	store := s.state().owners.state().store
	x, err := store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return failure(f.DependencyUnbound, nil)
	}
	row, found, err := loadArtifact(ctx, x, target.Details().Source.Details().Owner.Details().ID)
	if err != nil {
		return err
	}
	if !found {
		return failure(f.Forbidden, nil)
	}
	w := &projectAuditWitness{store: store, tx: tx, entry: entry, key: key, meta: row.meta, download: &projectAuditDownload{target: target, event: event}}
	w.active.Store(true)
	defer w.active.Store(false)
	_, err = s.state().audit.AppendInTx(context.WithValue(ctx, projectAuditWitnessKey{}, w), tx, entry, key)
	return portOrNil(err)
}

// Only the real publication/read tail calls this, after its complete original
// authorization, Object I/O and canonical checks. Nothing public mints a witness.
// It carries safe metadata, never the source, payload or read text.
func (s *Service) appendProjectAudit(ctx context.Context, tx f.Tx, entry au.Entry, key au.AppendKey, meta ac.Metadata, command *projectAuditCommand) error {
	w := &projectAuditWitness{store: s.state().store, tx: tx, entry: entry, key: key, meta: meta, command: command}
	w.active.Store(true)
	defer w.active.Store(false)
	_, err := s.state().audit.AppendInTx(context.WithValue(ctx, projectAuditWitnessKey{}, w), tx, entry, key)
	return portOrNil(err)
}

func sameProjectAuditStore(a, b Store) bool {
	return !nilPort(a) && !nilPort(b) && reflect.TypeOf(a) == reflect.TypeOf(b) &&
		reflect.ValueOf(a).Comparable() && reflect.ValueOf(b).Comparable() && a == b
}
func sameProjectAuditEntry(a, b au.Entry) bool {
	if a.Validate() != nil || b.Validate() != nil {
		return false
	}
	x, y := a.Fields(), b.Fields()
	return x.Scope.Equal(y.Scope) && x.Actor.Equal(y.Actor) && x.Action == y.Action &&
		x.Outcome == y.Outcome && x.Resource.Details() == y.Resource.Details() &&
		bytes.Equal(x.Metadata.JSON(), y.Metadata.JSON()) && x.Associations == y.Associations
}

func (a *ProjectAuditAuthority) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, entry au.Entry, key au.AppendKey) error {
	if a == nil || nilPort(a.store) {
		return failure(f.DependencyUnbound, nil)
	}
	if ctx == nil || !tx.Valid() || entry.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	e, k := entry.Fields(), key.Details()
	if e.Actor.Details().Kind != identity.Human || e.Scope.Details().Kind != identity.ProjectScope ||
		e.Resource.Details().Kind != au.ArtifactResource || e.Outcome != au.Success ||
		e.Associations != (au.Associations{}) || k.Producer != au.ArtifactProducer || k.Ordinal != 0 ||
		(e.Action != au.ArtifactCreate && e.Action != au.ArtifactRead && e.Action != au.ArtifactDownload) {
		return failure(f.Forbidden, nil)
	}
	w, ok := ctx.Value(projectAuditWitnessKey{}).(*projectAuditWitness)
	if !ok || w == nil || !w.active.Load() || w.tx != tx || !sameProjectAuditStore(w.store, a.store) ||
		!sameProjectAuditEntry(w.entry, entry) || w.key.Validate() != nil || w.key.Details() != k || w.meta.Validate() != nil {
		return failure(f.Forbidden, nil)
	}
	m := w.meta.Details()
	if m.Reference.ProjectID.String() != e.Scope.Details().ProjectID || m.Reference.ArtifactID.String() != e.Resource.Details().ID {
		return failure(f.Forbidden, nil)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return failure(f.DependencyUnbound, nil)
	}
	row, found, err := loadArtifact(ctx, x, m.Reference.ArtifactID.String())
	if err != nil {
		return err
	}
	actual, aerr := jsonDigest(row.meta.Details())
	expected, eerr := jsonDigest(m)
	if !found || aerr != nil || eerr != nil || actual != expected {
		return failure(f.Forbidden, nil)
	}
	if e.Action == au.ArtifactCreate {
		command := w.command
		if command == nil || command.identity.Validate() != nil || (command.path != "content" && command.path != "upload") {
			return failure(f.Forbidden, nil)
		}
		expectedKey, err := audit.CommandAppendKey(au.ArtifactProducer, command.identity, 0)
		if err != nil || expectedKey.Details() != k {
			return failure(f.Forbidden, nil)
		}
		current, exists, err := loadCommand(ctx, x, command.identity)
		if err != nil {
			return err
		}
		if !exists || current.state != "pending" || current.ref != m.Reference || current.object != m.Object.ID ||
			current.originalActor != stableActor(e.Actor) || current.creatorKind != identity.Human || current.creator != e.Actor.Details().UserID ||
			current.digest != command.digest || current.path != command.path || current.cause != command.cause || row.cause != command.cause ||
			current.execution != "" || current.operation != "" || current.tool != "" || current.toolCall != "" {
			return failure(f.Forbidden, nil)
		}
	} else if w.command != nil {
		return failure(f.Forbidden, nil)
	}
	if e.Action == au.ArtifactDownload {
		download := w.download
		if download == nil || download.target.Validate() != nil || download.event.Validate() != nil || download.event.Phase != oc.DownloadIssued {
			return failure(f.Forbidden, nil)
		}
		target, event := download.target.Details(), download.event
		current, err := artifactSource(row, target.Source.Details().Reference)
		if err != nil {
			return err
		}
		if !sameSource(current, target.Source) || target.Filename != m.Name {
			return failure(f.Forbidden, nil)
		}
		cause, err := jsonDigest([]string{"download", event.GrantID.String(), event.AttemptID.String(), string(event.Phase)})
		if err != nil {
			return err
		}
		expected, err := au.ArtifactMetadata(au.ArtifactDownload, au.ArtifactMetadataFields{ArtifactID: m.Reference.ArtifactID.String(), ObjectID: m.Object.ID.String(), MediaType: auditMedia(m.Object.MediaType), ByteSize: event.Length, Phase: au.IssuedPhase})
		if err != nil || k.CauseRef != cause.String() || !bytes.Equal(expected.JSON(), e.Metadata.JSON()) {
			return failure(f.Forbidden, nil)
		}
		if err = object.CheckDownloadIssuedInTx(ctx, tx, a.store, e.Actor, download.target, event); err != nil {
			return portError(err)
		}
	} else if w.download != nil {
		return failure(f.Forbidden, nil)
	}
	if !w.active.Load() {
		return failure(f.Forbidden, nil)
	}
	return portOrNil(ctx.Err())
}

var _ au.ProjectFactAuthority = (*ProjectAuditAuthority)(nil)
