package artifact

import (
	"context"
	"io"
	"strings"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func (s *Service) createFromContent(ctx context.Context, v ac.Invocation, meta foundation.CommandMeta, display ac.Display, media, content string) (ac.Metadata, error) {
	req, err := makeContentRequest(v, meta, display, media, content)
	if err != nil {
		return ac.Metadata{}, err
	}
	c, completed, err := s.lookup(ctx, req)
	if err != nil {
		return ac.Metadata{}, err
	}
	if completed != nil {
		return *completed, nil
	}
	if c.identity.Validate() != nil {
		c, err = s.beginCommand(ctx, req, oc.ResolvedSource{}, oc.SourceLease{})
		if err != nil {
			return ac.Metadata{}, err
		}
	}
	p, err := s.state().uploads.PreparePayload(ctx, v.Details().Actor, artifactOwner(c.ref), c.media, c.size, &c.sha, io.NopCloser(strings.NewReader(content)))
	if err != nil {
		return ac.Metadata{}, err
	}
	defer s.state().uploads.DiscardPrepared(p)
	return s.publishPrepared(ctx, req, c, p)
}

// Both creation and its explicit lookup bind the same normalized media type,
// display, byte length, content digest and original command metadata.
func makeContentRequest(v ac.Invocation, meta foundation.CommandMeta, display ac.Display, media, content string) (createRequest, error) {
	if err := ac.ValidateInline(content); err != nil {
		return createRequest{}, err
	}
	normalized, err := oc.NormalizeMediaType(media)
	if err != nil {
		return createRequest{}, err
	}
	return makeRequest(v, meta, display, "content", normalized, int64(len(content)), digestBytes([]byte(content)), oc.BusinessFileRef{}, oc.UploadReceipt{})
}
func (s *Service) createFromSource(ctx context.Context, v ac.Invocation, meta foundation.CommandMeta, display ac.Display, ref oc.BusinessFileRef) (ac.Metadata, error) {
	req, err := makeRequest(v, meta, display, "source", "", 0, "", ref, oc.UploadReceipt{})
	if err != nil {
		return ac.Metadata{}, err
	}
	c, completed, err := s.lookup(ctx, req)
	if err != nil {
		return ac.Metadata{}, err
	}
	if completed != nil {
		return *completed, nil
	}
	r := s.state()
	if nilPort(r.reads) || nilPort(r.resolver) {
		return ac.Metadata{}, failure(foundation.DependencyUnbound, nil)
	}
	source, err := r.resolver.Resolve(ctx, v.Details().Actor, ref)
	if err != nil {
		return ac.Metadata{}, portError(err)
	}
	if source.Validate() != nil || referenceStorage(source.Details().Reference) != referenceStorage(ref) {
		return ac.Metadata{}, unavailable(nil)
	}
	if c.identity.Validate() == nil {
		if !sameSource(source, c.source) {
			return ac.Metadata{}, failure(foundation.ResourceBusy, nil)
		}
		if err = s.retireOldSource(ctx, c); err != nil {
			return ac.Metadata{}, err
		}
	}
	// Capture the original source facts and acquire its Process/source lease in
	// the same short Tx as the command. No GET is permitted on unknown commit.
	var lease oc.SourceLease
	c, lease, err = s.acquireCommandSource(ctx, req, c, source)
	if err != nil {
		return ac.Metadata{}, err
	}
	defer s.cancelSource(lease)
	reader, err := r.reads.OpenLeasedSource(ctx, v.Details().Actor, lease)
	if err != nil {
		return ac.Metadata{}, err
	}
	defer reader.Close()
	p, err := r.uploads.PreparePayload(ctx, v.Details().Actor, artifactOwner(c.ref), c.media, c.size, &c.sha, reader)
	if err != nil {
		return ac.Metadata{}, err
	}
	defer r.uploads.DiscardPrepared(p)
	if err = reader.Close(); err != nil {
		return ac.Metadata{}, err
	}
	return s.publishPrepared(ctx, req, c, p)
}
func newRef(project identity.ProjectID) (ac.ArtifactRef, error) {
	a, err := foundation.NewID[ac.ArtifactEntity]()
	if err != nil {
		return ac.ArtifactRef{}, unavailable(err)
	}
	f, err := foundation.NewID[ac.File]()
	if err != nil {
		return ac.ArtifactRef{}, unavailable(err)
	}
	return ac.ArtifactRef{ProjectID: project, ArtifactID: a, FileID: f}, nil
}
func (s *Service) beginCommand(ctx context.Context, req createRequest, source oc.ResolvedSource, lease oc.SourceLease) (commandRow, error) {
	ref, err := newRef(req.invocation.Details().ProjectID)
	if err != nil {
		return commandRow{}, err
	}
	c, err := newCommand(req, ref, source)
	if err != nil {
		return commandRow{}, err
	}
	c.sourceLease = lease.ID()
	cause, _ := foundation.NewCommandsCause(req.identity)
	result := s.within(ctx, invocationSubject(req.invocation), identity.Mutate, cause, nil, createLocks(req.identity, ref.ArtifactID.String()), func(ctx context.Context, tx foundation.Tx, _ oc.LockedAccess) error {
		e, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		_, found, err := loadCommand(ctx, e, req.identity)
		if err != nil {
			return err
		}
		if found {
			return failure(foundation.ResourceBusy, nil)
		}
		if req.command.ExpectedVersion != nil && *req.command.ExpectedVersion != 1 {
			return failure(foundation.VersionConflict, nil)
		}
		return insertCommand(ctx, e, c)
	})
	if err = commitError(result); err != nil {
		return commandRow{}, err
	}
	return c, nil
}
func (s *Service) retireOldSource(ctx context.Context, c commandRow) error {
	if c.sourceLease.Validate() != nil {
		return nil
	}
	r := s.state()
	hash := digestBytes([]byte(c.identity.Canonical()))
	r.mu.Lock()
	local := r.leases[hash]
	r.mu.Unlock()
	if local.Validate() == nil && local.ID() == c.sourceLease {
		if err := r.reads.CancelSourceLease(ctx, local); err != nil {
			return err
		}
	}
	inspection, err := r.cleaner.InspectReferences(ctx, c.source.Details().Meta.ID)
	if err != nil {
		return err
	}
	for _, lease := range inspection.ActiveLeases {
		if lease.ID == c.sourceLease {
			return failure(foundation.ResourceBusy, nil)
		}
	}
	return nil
}
func (s *Service) acquireCommandSource(ctx context.Context, req createRequest, c commandRow, source oc.ResolvedSource) (commandRow, oc.SourceLease, error) {
	r := s.state()
	isNew := c.identity.Validate() != nil
	if isNew {
		ref, err := newRef(req.invocation.Details().ProjectID)
		if err != nil {
			return commandRow{}, oc.SourceLease{}, err
		}
		c, err = newCommand(req, ref, source)
		if err != nil {
			return commandRow{}, oc.SourceLease{}, err
		}
	}
	plan, err := s.sourcePlan(ctx, req.invocation.Details().Actor, source, oc.AcquireSourceAccess)
	if err != nil {
		return commandRow{}, oc.SourceLease{}, err
	}
	cause, _ := foundation.NewCommandsCause(req.identity)
	var lease oc.SourceLease
	if st := r.ownership; st != nil && !nilPort(st.sourceAuthority) {
		reads, ok := r.reads.(*object.SourceReads)
		if !ok {
			return commandRow{}, oc.SourceLease{}, failure(foundation.DependencyUnbound, nil)
		}
		ctx = context.WithValue(ctx, artifactSourceAcquisitionKey{}, reads)
	}
	result := s.within(ctx, invocationSubject(req.invocation), identity.Mutate, cause, []oc.AccessLockPlan{plan}, createLocks(req.identity, c.ref.ArtifactID.String()), func(ctx context.Context, tx foundation.Tx, locked oc.LockedAccess) error {
		e, err := r.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, found, err := loadCommand(ctx, e, req.identity)
		if err != nil {
			return err
		}
		if isNew && found || !isNew && (!found || current.ref != c.ref || current.state != "pending" || current.sourceLease != c.sourceLease) {
			return failure(foundation.ResourceBusy, nil)
		}
		if found {
			if err = matchesCommand(current, req); err != nil {
				return err
			}
			if !sameSource(current.source, source) {
				return failure(foundation.ResourceBusy, nil)
			}
		}
		if req.command.ExpectedVersion != nil && *req.command.ExpectedVersion != 1 {
			return failure(foundation.VersionConflict, nil)
		}
		lease, err = r.reads.AcquireSourceInTx(ctx, tx, req.invocation.Details().Actor, source, plan, locked)
		// Keep any issued handle on both sides before the original Store
		// returns, including the original error path. Its source Project cannot
		// report Stopped merely because the command targets another Project.
		s.rememberProjectSource(req, source, lease)
		if err != nil {
			return err
		}
		if err = s.bindProjectSource(ctx, tx, req, source, lease); err != nil {
			return err
		}
		if isNew {
			next := c
			next.sourceLease = lease.ID()
			return insertCommand(ctx, e, next)
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_artifact.commands SET source_lease_id=$2 WHERE command_hash=$1`, digestRaw(digestBytes([]byte(req.identity.Canonical()))), lease.ID().String())
		return portOrNil(err)
	})

	if err = commitError(result); err != nil {
		if result.State() == foundation.NotCommitted && lease.Validate() == nil {
			s.discardProjectSourceAcquisition(lease)
		}
		return commandRow{}, oc.SourceLease{}, err
	}
	c.sourceLease = lease.ID()
	return c, lease, nil
}
func (s *Service) cancelSource(lease oc.SourceLease) {
	if lease.Validate() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s.state().reads.CancelSourceLease(ctx, lease) == nil {
		if st := s.state().ownership; st != nil && !nilPort(st.sourceAuthority) {
			_ = s.returnedProjectSource(ctx, lease)
			return
		}
		r := s.state()
		r.mu.Lock()
		for key, handle := range r.leases {
			if handle.ID() == lease.ID() {
				delete(r.leases, key)
				delete(r.leaseProjects, key)
				delete(r.leaseSources, key)
			}
		}
		r.mu.Unlock()
	}
}

func (s *Service) rememberProjectSource(req createRequest, source oc.ResolvedSource, lease oc.SourceLease) {
	if lease.Validate() != nil {
		return
	}
	r := s.state()
	key := digestBytes([]byte(req.identity.Canonical()))
	r.mu.Lock()
	defer r.mu.Unlock()
	r.leases[key] = lease
	if r.leaseProjects == nil {
		r.leaseProjects = make(map[foundation.Digest]identity.ProjectID)
	}
	if r.leaseSources == nil {
		r.leaseSources = make(map[foundation.Digest]string)
	}
	r.leaseProjects[key] = req.invocation.Details().ProjectID
	r.leaseSources[key] = source.Details().Owner.Details().ProjectID
}
