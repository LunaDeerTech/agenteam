package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// OwnerReader is not implemented by this pure block. A future implementation
// must check the current Owner/Session/Project gate on every call. ListSkills is
// only the bounded builtin directory; external installation requires pagination.
type OwnerReader interface {
	ListSkills(context.Context, id.Actor, id.ProjectID) ([]Metadata, error)
	GetSkill(context.Context, id.Actor, id.ProjectID, SkillID) (Metadata, error)
	OpenPackage(context.Context, id.Actor, id.ProjectID, SkillID, f.Revision) (*PackageReader, error)
}

type readerState struct {
	meta              RevisionMetadata
	body              *oc.ObjectReader
	mu                sync.Mutex
	readMu            sync.Mutex
	closeOnce         sync.Once
	closed, closeDone bool
	active            int
	closeErr          error
}
type PackageReader struct{ data func() *readerState }

// NewPackageReader validates the exact full Object metadata. On failure the
// caller still owns body. On success Close owns its one underlying Close call.
// Neither this constructor nor Joined certifies permission or lease COMMIT.
func NewPackageReader(meta RevisionMetadata, body *oc.ObjectReader) (*PackageReader, error) {
	if meta.Validate() != nil || body == nil || body.Meta().Validate() != nil || body.Range() != nil {
		return nil, invalid()
	}
	got := body.Meta()
	want := meta.Object
	if got.ID != want.ID || !got.Scope.Equal(want.Scope) || got.MediaType != want.MediaType || got.ByteSize != want.ByteSize || got.SHA256 != want.SHA256 || got.State != want.State || got.Version != want.Version || !got.CreatedAt.Time().Equal(want.CreatedAt.Time()) {
		return nil, invalid()
	}
	state := &readerState{meta: meta, body: body}
	return &PackageReader{func() *readerState { return state }}, nil
}
func (r *PackageReader) Metadata() (RevisionMetadata, error) {
	if r == nil || r.data == nil {
		return RevisionMetadata{}, invalid()
	}
	return r.data().meta, nil
}
func (r *PackageReader) Read(p []byte) (int, error) {
	if r == nil || r.data == nil {
		return 0, invalid()
	}
	s := r.data()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	s.active++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.active--; s.mu.Unlock() }()
	s.readMu.Lock()
	defer s.readMu.Unlock()
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return 0, io.ErrClosedPipe
	}
	return s.body.Read(p)
}
func (r *PackageReader) Close() error {
	if r == nil || r.data == nil {
		return invalid()
	}
	s := r.data()
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.closeErr = invalid()
		s.mu.Unlock()
		// Do not hold readMu: Close must be able to interrupt a blocked Read. If an
		// underlying Close panics, closeDone stays false and Joined cannot lie.
		err := s.body.Close()
		s.mu.Lock()
		s.closeErr = err
		s.closeDone = true
		s.mu.Unlock()
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeErr
}

// Joined reports only that admission is closed and actual local Read/Close
// calls ended. The Object service independently tracks releaseUnknown leases.
func (r *PackageReader) Joined() bool {
	if r == nil || r.data == nil {
		return false
	}
	s := r.data()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed && s.closeDone && s.active == 0
}
func (*PackageReader) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "skill_package_reader") }
func (*PackageReader) MarshalJSON() ([]byte, error) { return []byte(`"skill_package_reader"`), nil }
func (*PackageReader) UnmarshalJSON([]byte) error   { return invalid() }
func (*PackageReader) LogValue() slog.Value         { return slog.StringValue("skill_package_reader") }
