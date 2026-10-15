package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type invocationContextKey struct{}
type invocationRead struct {
	service      *Service
	invocation   kc.Invocation
	dependencies oc.AccessDependencies
	document     kc.DocumentID
	version      f.Version
	read         kc.ReadRequest
	query        *kc.QueryRequest
}

func currentInvocation(ctx context.Context, actor id.Actor, project id.ProjectID) (*invocationRead, error) {
	if ctx == nil {
		return nil, fault(f.InvalidArgument)
	}
	v, ok := ctx.Value(invocationContextKey{}).(*invocationRead)
	if !ok || v == nil || v.service.state() == nil || v.invocation.Validate() != nil || v.dependencies.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	d := v.invocation.Details()
	if !d.Actor.Equal(actor) || d.ProjectID != project {
		return nil, fault(f.Forbidden)
	}
	return v, nil
}

func (s *Service) invocationContext(ctx context.Context, invocation kc.Invocation, document kc.DocumentID, version f.Version, read kc.ReadRequest, query *kc.QueryRequest) (context.Context, error) {
	if ctx == nil || invocation.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	st := s.state()
	if st == nil || nilPort(st.deps.InvocationAuthority) {
		return nil, fault(f.DependencyUnbound)
	}
	if _, ok := st.deps.InvocationAuthority.(kc.KnowledgeInvocationInputAuthority); !ok {
		return nil, fault(f.DependencyUnbound)
	}
	dependencies, err := st.deps.InvocationAuthority.Discover(ctx, invocation)
	if err != nil {
		return nil, portError(err)
	}
	if dependencies.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	return context.WithValue(ctx, invocationContextKey{}, &invocationRead{s, invocation, dependencies, document, version, read, query}), nil
}

func accessLocks(ctx context.Context, actor id.Actor, project id.ProjectID, write bool) ([]f.LockRequest, error) {
	if actor.Details().Kind == id.Service {
		if _, err := currentIndexWork(ctx, actor, project); err != nil {
			if _, err := currentIndexBuild(ctx, actor, project); err != nil {
				return nil, err
			}
		}
		return indexLocks(project, write)
	}
	if actor.Details().Kind != id.AgentRun {
		return scopeLocks(actor, project, write)
	}
	if write {
		return nil, fault(f.Forbidden)
	}
	v, err := currentInvocation(ctx, actor, project)
	if err != nil {
		return nil, err
	}
	p, err := f.ProjectLock(project.String())
	if err != nil {
		return nil, err
	}
	t, err := f.KnowledgeTreeLock(project.String())
	if err != nil {
		return nil, err
	}
	locks := append([]f.LockRequest{}, v.dependencies.Locks()...)
	return ob.NormalizeLocks(append(locks, f.LockRequest{Key: p, Mode: f.Shared}, f.LockRequest{Key: t, Mode: f.Shared}))
}

func authorizeInvocationInTx(ctx context.Context, tx f.Tx, store Store, actor id.Actor, project id.ProjectID, intent id.AccessIntent) error {
	v, err := currentInvocation(ctx, actor, project)
	if err != nil {
		return err
	}
	if intent != id.Read || !sameStore(store, v.service.state().store) {
		return fault(f.Forbidden)
	}
	authority := v.service.state().deps.InvocationAuthority
	current, err := authority.DiscoverInTx(ctx, tx, v.invocation)
	if err != nil {
		return portError(err)
	}
	if current.Validate() != nil || !current.Equal(v.dependencies) {
		return fault(f.ResourceBusy)
	}
	if err = store.RequireHeldLocks(ctx, tx, current.Locks()); err != nil {
		return portError(err)
	}
	if err = authority.AuthorizeInTx(ctx, tx, v.invocation, intent); err != nil {
		return portError(err)
	}
	input, ok := authority.(kc.KnowledgeInvocationInputAuthority)
	if !ok || nilPort(input) {
		return fault(f.DependencyUnbound)
	}
	return input.WithKnowledgeInputInTx(ctx, tx, v.invocation, func(ctx context.Context, raw []byte) error {
		return v.matchArguments(ctx, raw)
	})
}

func (s *Service) authorizeReadInTx(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID) error {
	if actor.Details().Kind == id.Service {
		w, err := currentIndexWork(ctx, actor, project)
		if err != nil {
			b, buildErr := currentIndexBuild(ctx, actor, project)
			if buildErr != nil || b.service.documents != s {
				return fault(f.Forbidden)
			}
			return b.authorizeInTx(ctx, tx)
		}
		return w.authorizeInTx(ctx, tx, s.state().store)
	}
	if actor.Details().Kind == id.AgentRun {
		return authorizeInvocationInTx(ctx, tx, s.state().store, actor, project, id.Read)
	}
	grant, err := s.state().deps.Projects.RequireOwnerInTx(ctx, tx, actor, project, id.Read)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(actor, project) {
		return fault(f.Forbidden)
	}
	return nil
}

func (v *invocationRead) matchArguments(ctx context.Context, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.query != nil {
		parsed, err := parseQueryArguments(raw)
		if err != nil || parsed.Query != v.query.Query || parsed.Limit != v.query.Limit || parsed.Filters.DocumentID != v.query.Filters.DocumentID || parsed.Filters.MediaType != v.query.Filters.MediaType || parsed.Filters.DocumentSourceKind != v.query.Filters.DocumentSourceKind {
			return fault(f.Forbidden)
		}
		return nil
	}
	parsed, err := parseReadArguments(raw)
	if err != nil || parsed.Document != v.document || parsed.Version != v.version || parsed.Request != v.read {
		return fault(f.Forbidden)
	}
	return nil
}

type readArguments struct {
	Document kc.DocumentID
	Version  f.Version
	Request  kc.ReadRequest
}

// Tool arguments are closed and duplicate keys are rejected before decoding.
func decodeArguments(raw []byte, target any) error {
	if len(raw) == 0 || len(raw) > 32<<10 || !utf8.Valid(raw) {
		return fault(f.InvalidArgument)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return fault(f.InvalidArgument)
	}
	seen := map[string]bool{}
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return fault(f.InvalidArgument)
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return fault(f.InvalidArgument)
		}
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return fault(f.InvalidArgument)
	}
	if d.Decode(new(any)) != io.EOF {
		return fault(f.InvalidArgument)
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		return fault(f.InvalidArgument)
	}
	return nil
}

func parseReadArguments(raw []byte) (readArguments, error) {
	var wire struct {
		Document kc.DocumentID `json:"document_id"`
		Version  f.Version     `json:"document_version"`
		Offset   *f.Progress   `json:"byte_offset,omitempty"`
		MaxBytes *int          `json:"max_bytes,omitempty"`
	}
	if err := decodeArguments(raw, &wire); err != nil {
		return readArguments{}, err
	}
	out := readArguments{Document: wire.Document, Version: wire.Version, Request: kc.ReadRequest{MaxBytes: 8192}}
	if wire.Offset != nil {
		out.Request.ByteOffset = *wire.Offset
	}
	if wire.MaxBytes != nil {
		out.Request.MaxBytes = *wire.MaxBytes
	}
	if out.Document.Validate() != nil || out.Version.Validate() != nil || out.Request.Validate() != nil {
		return readArguments{}, fault(f.InvalidArgument)
	}
	return out, nil
}

func parseQueryArguments(raw []byte) (kc.QueryRequest, error) {
	var wire struct {
		Query      string `json:"query"`
		Limit      *int   `json:"limit,omitempty"`
		Document   string `json:"document_id,omitempty"`
		MediaType  string `json:"media_type,omitempty"`
		SourceKind string `json:"source_kind,omitempty"`
	}
	if err := decodeArguments(raw, &wire); err != nil {
		return kc.QueryRequest{}, err
	}
	out := kc.QueryRequest{Query: wire.Query, Limit: 5}
	if wire.Limit != nil {
		out.Limit = *wire.Limit
	}
	out.Filters.DocumentID, out.Filters.MediaType, out.Filters.DocumentSourceKind = wire.Document, wire.MediaType, wire.SourceKind
	if err := out.Validate(); err != nil {
		return kc.QueryRequest{}, err
	}
	return out, nil
}

func (s *Service) ReadForInvocation(ctx context.Context, invocation kc.Invocation, document kc.DocumentID, version f.Version, request kc.ReadRequest) (kc.DocumentContent, error) {
	if document.Validate() != nil || version.Validate() != nil || request.Validate() != nil {
		return kc.DocumentContent{}, fault(f.InvalidArgument)
	}
	ctx, err := s.invocationContext(ctx, invocation, document, version, request, nil)
	if err != nil {
		return kc.DocumentContent{}, err
	}
	d := invocation.Details()
	out, err := s.ReadDocument(ctx, d.Actor, d.ProjectID, document, request)
	if err != nil {
		return kc.DocumentContent{}, err
	}
	if out.Document.ID != document || out.Document.ContentVersion != version || out.Document.ProjectID != d.ProjectID {
		return kc.DocumentContent{}, fault(f.VersionConflict)
	}
	return out, nil
}
