package skill

import (
	"context"
	"io"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

var _ sc.OwnerReader = (*Service)(nil)

func (s *Service) OpenPackage(ctx context.Context, actor id.Actor, project id.ProjectID, skill sc.SkillID, revision f.Revision) (_ *sc.PackageReader, err error) {
	if skill.Validate() != nil || revision.Validate() != nil {
		return nil, invalid()
	}
	call, err := s.beginProjectWork(ctx, project, packageReaderWork)
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			s.end(call)
		}
	}()
	var work *ownedWork
	var metadata sc.RevisionMetadata
	err = s.ownerReadTx(call.ctx, actor, project, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, row initializationRow) error {
		if row.skill != skill || row.bundle.revision != revision {
			return fault(f.NotFound)
		}
		_, m, e := loadPublished(ctx, x, row)
		if e != nil {
			return e
		}
		metadata = m
		work, e = s.newOwnedWork(row, packageReaderWork, call)
		if e != nil {
			return e
		}
		return insertWork(ctx, x, work.fact)
	})
	if err != nil {
		if work != nil {
			s.registrationFailed(work, err)
		}
		return nil, err
	}
	// A known registration commit precedes ReadObject, which performs its own
	// current access gate and durable lease accounting. No cached read grant is
	// passed through from the metadata transaction.
	owner, err := work.row.owner()
	if err != nil {
		_ = s.finishOwnedWork(call.ctx, work)
		return nil, err
	}
	body, err := s.state().objects.ReadObject(call.ctx, actor, owner, metadata.Object.ID, nil)
	if err != nil {
		if body != nil {
			_ = body.Close()
		}
		_ = s.finishOwnedWork(call.ctx, work)
		return nil, err
	}
	if body == nil {
		_ = s.finishOwnedWork(call.ctx, work)
		return nil, unavailable(nil)
	}
	tracked := &packageBody{service: s, call: call, work: work, body: body}
	wrapped, err := oc.NewObjectReader(body.Meta(), body.Range(), tracked)
	if err == nil {
		var out *sc.PackageReader
		out, err = sc.NewPackageReader(metadata, wrapped)
		if err == nil {
			tracked.installCancellation()
			transferred = true
			tracked.openReturned()
			if e := call.ctx.Err(); e != nil {
				_ = tracked.Close()
				return nil, portError(e)
			}
			return out, nil
		}
	}
	// Constructors leave ownership with us on any malformed/mismatched metadata.
	// The actual Close and its accounting tail still precede call retirement.
	_ = body.Close()
	_ = s.finishOwnedWork(call.ctx, work)
	return nil, portError(err)
}

// packageBody owns the Object reader, its concurrent local calls, and the one
// cancellation callback. No EOF or cancellation signal is a proof of Close.
// D05 lease-release Unknown remains D05-owned even after local I/O returns.
type packageBody struct {
	service                                             *Service
	call                                                *serviceCall
	work                                                *ownedWork
	body                                                *oc.ObjectReader
	mu                                                  sync.Mutex
	reads                                               int
	closed, closeDone, openDone, watcherDone, finishing bool
	closeErr, tailErr                                   error
	closeOnce                                           sync.Once
	stopCancel                                          func() bool
}

func (r *packageBody) installCancellation() {
	stop := context.AfterFunc(r.call.ctx, func() {
		r.closeBody()
		r.mu.Lock()
		r.watcherDone = true
		r.mu.Unlock()
		r.settle()
	})
	r.mu.Lock()
	r.stopCancel = stop
	r.mu.Unlock()
}

func (r *packageBody) openReturned() {
	r.mu.Lock()
	r.openDone = true
	r.mu.Unlock()
	r.settle()
}

func (r *packageBody) Read(p []byte) (int, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	r.reads++
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.reads--
		r.mu.Unlock()
		r.settle()
	}()
	return r.body.Read(p)
}

func (r *packageBody) closeBody() {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.mu.Unlock()
		err := r.body.Close()
		r.mu.Lock()
		r.closeErr = err
		r.closeDone = true
		r.mu.Unlock()
	})
}

func (r *packageBody) Close() error {
	r.closeBody()
	r.mu.Lock()
	stop := r.stopCancel
	r.mu.Unlock()
	if stop != nil && stop() {
		r.mu.Lock()
		r.watcherDone = true
		r.mu.Unlock()
	}
	r.settle()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closeErr != nil {
		return r.closeErr
	}
	return r.tailErr
}

func (r *packageBody) settle() {
	r.mu.Lock()
	ready := r.openDone && r.closed && r.closeDone && r.reads == 0 && r.watcherDone && !r.finishing
	if ready {
		r.finishing = true
	}
	r.mu.Unlock()
	if !ready {
		return
	}
	err := r.service.finishOwnedWork(r.call.ctx, r.work)
	r.mu.Lock()
	r.tailErr = err
	r.mu.Unlock()
	r.service.end(r.call)
}
