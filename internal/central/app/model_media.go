package app

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// Fixed during single-threaded App construction, before runtime admission.
// No live caller may replace an authority, SourceResolver or Object service.
type modelMediaAssembly struct {
	store       object.Store
	objects     *objectAssembly
	authorities *executionRuntimeAuthorities
	artifacts   *artifact.AttachmentSources
	mcp         oc.SourceResolver
	bound       bool
}

func (a *modelMediaAssembly) construct(owners *artifact.OwnerProvider, mcp oc.SourceResolver) error {
	if a == nil || a.bound || a.objects == nil || a.objects.service == nil || a.authorities == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	var err error
	a.artifacts, err = artifact.NewAttachmentSources(owners, a.objects.service)
	if err != nil {
		return err
	}
	a.mcp = mcp
	a.bound = true
	return nil
}
func (a *modelMediaAssembly) RequireBound() error {
	if a == nil || !a.bound || a.artifacts == nil || a.objects == nil || a.objects.service == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return nil
}
func (a *modelMediaAssembly) resolver(ref oc.BusinessFileRef) (oc.SourceResolver, error) {
	if a == nil || !a.bound || ref.Validate() != nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	switch ref.Details().Kind {
	case oc.ArtifactFile:
		return a.artifacts, nil
	case oc.MCPFile:
		if !runtimeInformationNil(a.mcp) {
			return a.mcp, nil
		}
	}
	return nil, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
}
func (a *modelMediaAssembly) Resolve(ctx context.Context, actor id.Actor, ref oc.BusinessFileRef) (oc.ResolvedSource, error) {
	r, e := a.resolver(ref)
	if e != nil {
		return oc.ResolvedSource{}, e
	}
	return r.Resolve(ctx, actor, ref)
}
func (a *modelMediaAssembly) ValidateInTx(ctx context.Context, tx f.Tx, actor id.Actor, source oc.ResolvedSource, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	r, e := a.resolver(source.Details().Reference)
	if e != nil {
		return e
	}
	return r.ValidateInTx(ctx, tx, actor, source, plan, locked)
}
func (a *modelMediaAssembly) OpenModelMedia(ctx context.Context, request mc.ModelRequest) (mc.MediaSession, error) {
	if a == nil || !a.bound || ctx == nil || request.Validate() != nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	reads, err := object.NewSourceReads(a.objects.service, a)
	if err != nil {
		return nil, err
	}
	return &modelMediaSession{assembly: a, reads: reads, request: request.Clone()}, nil
}

type modelMediaRead struct {
	*oc.ObjectReader
	name string
}

func (r *modelMediaRead) Filename() string { return r.name }

type modelMediaEntry struct {
	ctx    context.Context
	source oc.ResolvedSource
	lease  oc.SourceLease
	reader *modelMediaRead
}
type modelMediaSession struct {
	assembly *modelMediaAssembly
	reads    *object.SourceReads
	request  mc.ModelRequest
	mu       sync.Mutex
	closed   bool
	entries  []*modelMediaEntry
}

func mediaCause() (f.TransactionCause, error) {
	key, e := f.NewID[f.Request]()
	if e != nil {
		return f.TransactionCause{}, e
	}
	return f.NewRecoveryCause("model-media", key.String(), "")
}
func mediaCommit(result f.CommitResult) error {
	if result.State() == f.Committed {
		return nil
	}
	if err := result.Fault(); err != nil {
		return err
	}
	return f.NewFault(f.CommitUnknown, result.State())
}
func (s *modelMediaSession) sourceContext(ctx context.Context, part mc.MediaPart) (context.Context, string, error) {
	a := s.assembly
	if part.Ref.Validate() != nil {
		return nil, "", f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	if part.Ref.Details().Kind == oc.MCPFile {
		actor, err := a.authorities.execution.MCPContentReadActor(ctx)
		if err != nil {
			return nil, "", err
		}
		if !actor.Equal(s.request.Actor) {
			return nil, "", f.NewFault(f.Forbidden, f.NotStarted)
		}
		suffix := ".txt"
		switch part.MediaType {
		case "application/pdf":
			suffix = ".pdf"
		case "image/png":
			suffix = ".png"
		case "image/jpeg":
			suffix = ".jpg"
		case "image/webp":
			suffix = ".webp"
		case "image/gif":
			suffix = ".gif"
		case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
			suffix = ".docx"
		}
		return ctx, "mcp-" + part.Ref.Details().FileID + suffix, nil
	}
	var authority ec.AttachmentContentReadAuthority
	switch s.request.Actor.Details().Kind {
	case id.AgentRun:
		authority = a.authorities.execution
	case id.Service:
		if s.request.Actor.Details().ServiceName == id.MeetingRuntime && a.authorities.m3 != nil {
			authority = a.authorities.m3.summary
		}
	}
	if runtimeInformationNil(authority) {
		return nil, "", f.NewFault(f.Forbidden, f.NotStarted)
	}
	scoped, binding, err := artifact.BindModelAttachmentRead(ctx, authority, s.request.Actor, part.Ref)
	return scoped, binding.Filename, err
}
func (s *modelMediaSession) Read(ctx context.Context, part mc.MediaPart) (mc.MediaRead, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, f.NewFault(f.InvalidState, f.NotStarted)
	}
	scoped, name, err := s.sourceContext(ctx, part)
	if err != nil {
		return nil, err
	}
	source, err := s.assembly.Resolve(scoped, s.request.Actor, part.Ref)
	if err != nil {
		return nil, err
	}
	if source.Details().Meta.MediaType != part.MediaType {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	request, err := oc.NewSourceAccess(oc.AccessRequestDetails{Operation: oc.AcquireSourceAccess, Actor: s.request.Actor, Source: source})
	if err != nil {
		return nil, err
	}
	plan, err := s.assembly.objects.service.DiscoverAccess(scoped, request)
	if err != nil {
		return nil, err
	}
	cause, err := mediaCause()
	if err != nil {
		return nil, err
	}
	entry := &modelMediaEntry{ctx: scoped, source: source}
	s.entries = append(s.entries, entry)
	result := s.assembly.store.WithinTx(scoped, cause, func(ctx context.Context, tx f.Tx) error {
		locked, err := s.assembly.objects.service.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
		if err != nil {
			return err
		}
		entry.lease, err = s.reads.AcquireSourceInTx(ctx, tx, s.request.Actor, source, plan, locked)
		return err
	})
	if err = mediaCommit(result); err != nil {
		return nil, err
	}
	reader, err := s.reads.OpenLeasedSource(scoped, s.request.Actor, entry.lease)
	if err != nil {
		return nil, err
	}
	entry.reader = &modelMediaRead{ObjectReader: reader, name: name}
	return entry.reader, nil
}

// WithSend holds all current source/owner locks through the actual application
// write. Discovery is outside the Tx and grants no permission; each plan is
// revalidated inside using its exact source context, never a new source lookup.
func (s *modelMediaSession) WithSend(ctx context.Context, write func() (int, error)) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || write == nil {
		return 0, f.NewFault(f.InvalidState, f.NotStarted)
	}
	plans := make([]oc.AccessLockPlan, 0, len(s.entries))
	contexts := make([]context.Context, 0, len(s.entries))
	for _, entry := range s.entries {
		part := mc.MediaPart{Ref: entry.source.Details().Reference, MediaType: entry.source.Details().Meta.MediaType}
		scoped, _, err := s.sourceContext(ctx, part)
		if err != nil {
			return 0, err
		}
		req, err := oc.NewSourceAccess(oc.AccessRequestDetails{Operation: oc.ValidateSourceAccess, Actor: s.request.Actor, Source: entry.source})
		if err != nil {
			return 0, err
		}
		plan, err := s.assembly.objects.service.DiscoverAccess(scoped, req)
		if err != nil {
			return 0, err
		}
		plans = append(plans, plan)
		contexts = append(contexts, scoped)
	}
	if len(plans) == 0 {
		return 0, f.NewFault(f.InvalidState, f.NotStarted)
	}
	cause, err := mediaCause()
	if err != nil {
		return 0, err
	}
	n := 0
	result := s.assembly.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locked, err := s.assembly.objects.service.AcquireAccessPlansInTx(ctx, tx, plans, nil)
		if err != nil {
			return err
		}
		for index, entry := range s.entries {
			if err = s.assembly.ValidateInTx(contexts[index], tx, s.request.Actor, entry.source, plans[index], locked); err != nil {
				return err
			}
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		n, err = write()
		return err
	})
	return n, mediaCommit(result)
}
func (s *modelMediaSession) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	var result error
	for _, entry := range s.entries {
		if entry.reader != nil {
			result = errors.Join(result, entry.reader.Close())
		}
		if entry.lease.Validate() == nil {
			result = errors.Join(result, s.reads.CancelSourceLease(ctx, entry.lease))
		}
	}
	if result == nil {
		s.closed = true
		s.entries = nil
		s.reads = nil
		s.request = mc.ModelRequest{}
	}
	return result
}

var _ mc.MediaSource = (*modelMediaAssembly)(nil)
var _ io.ReadCloser = (*modelMediaRead)(nil)
