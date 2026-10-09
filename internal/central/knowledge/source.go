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
