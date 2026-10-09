// Package knowledge owns current Knowledge documents, their tree, and the
// durable work that publishes and retires their canonical objects.
package knowledge

import (
	"context"
	"sync"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type ActivityAuthority interface {
	TouchActivityInTx(context.Context, f.Tx, id.Actor) error
}

type Dependencies struct {
	Projects         pc.ProjectAuthority
	Activity         ActivityAuthority
	Objects          oc.Objects
	Uploads          oc.Uploads
	Sources          oc.SourceResolver
	SourceReads      oc.SourceReads
	ReferenceCleanup oc.ReferenceCleanup
	ObjectCleanup    oc.Cleaner
	Audit            audit.Appender
	Outbox           ob.Appender
	Events           kc.KnowledgeEvents
	Processes        ob.ProcessAuthority
	Cursors          cursor.Keyring
	Confirmations    kc.ConfirmationKeys
}

type Service struct{ data func() *serviceState }
type serviceState struct {
	store   Store
	deps    Dependencies
	issuer  kc.MutationIssuer
	mu      sync.Mutex
	stopped bool
	calls   map[*call]struct{}
	changed chan struct{}
	// Only actual publication resource retirement installs these proofs. An
	// empty call registry or a cancelled context is never equivalent to join.
	joinedPublications map[f.ID[command]]publicationWork
}
type call struct{ cancel context.CancelFunc }

func New(store Store, deps Dependencies) (*Service, error) {
	ports := []any{store, deps.Projects, deps.Activity, deps.Objects, deps.Uploads,
		deps.Sources, deps.SourceReads, deps.ReferenceCleanup, deps.ObjectCleanup,
		deps.Audit, deps.Outbox, deps.Processes}
	for _, port := range ports {
		if nilPort(port) {
			return nil, fault(f.DependencyUnbound)
		}
	}
	if !deps.Events.Valid() || deps.Processes.CurrentProcess().Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	if deps.Cursors.Validate() != nil || deps.Confirmations.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	st := &serviceState{store: store, deps: deps, issuer: kc.NewMutationIssuer(), calls: make(map[*call]struct{}), changed: make(chan struct{})}
	return &Service{data: func() *serviceState { return st }}, nil
}

func (s *Service) state() *serviceState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}

func (s *Service) CreateDocument(ctx context.Context, actor id.Actor, meta f.CommandMeta, request kc.CreateRequest, source kc.SourceInput) (kc.DocumentRef, error) {
	input, err := createContentInput(actor, meta, request, source)
	if err != nil {
		// SourceInput owns its input even when command validation rejects it.
		_ = source.Close()
		return kc.DocumentRef{}, err
	}
	return s.runContentCommand(ctx, input, &source)
}

func (s *Service) UpdateDocument(ctx context.Context, actor id.Actor, meta f.CommandMeta, project id.ProjectID, document kc.DocumentID, request kc.UpdateRequest, source *kc.SourceInput) (kc.DocumentRef, error) {
	input, err := updateContentInput(actor, meta, project, document, request, source)
	if err != nil {
		if source != nil {
			_ = source.Close()
		}
		return kc.DocumentRef{}, err
	}
	return s.runContentCommand(ctx, input, source)
}

func (s *Service) runContentCommand(ctx context.Context, input contentInput, source *kc.SourceInput) (out kc.DocumentRef, err error) {
	run, done, err := s.begin(ctx)
	if err != nil {
		if source != nil {
			_ = source.Close()
		}
		return out, err
	}
	var retirement *publicationRetirement
	var intent contentIntent
	var work publicationWork
	defer func() {
		var closeErr error
		if retirement != nil {
			closeErr = retirement.join()
		} else if source != nil {
			closeErr = source.Close()
		}
		if closeErr != nil {
			// No done(): failed Close/Discard is not actual retirement. The
			// registered operation remains visible to Stop/Drain until process
			// termination; this does not repair the stopped D05 runtime path.
			if err == nil {
				err = contentCompletionError(out, closeErr)
			}
			return
		}
		// Keep this call registered while projecting the already-real local
		// join. If SQL is cancelled/Unknown, its local proof remains intact for
		// exact same-process recovery; do not turn it into a false rollback.
		if retirement != nil {
			if checkpointErr := s.checkpointPublicationJoin(run, input.actor, intent.record, work); checkpointErr != nil && err == nil {
				err = contentCompletionError(out, checkpointErr)
			}
		}
		done()
	}()
	intent, err = s.prepareContentIntent(run, input)
	if err != nil {
		return out, err
	}
	if intent.record.state == kc.Committed {
		return *intent.record.receipt.Document, nil
	}
	if source == nil {
		return s.finishTitleContent(run, input, intent)
	}
	if input.request.Source.Kind == kc.InputBusinessFile {
		// Lease-backed source preparation/final validation is a distinct
		// composition; direct preparation must never open it without a lease.
		return out, fault(f.DependencyUnbound)
	}
	var replay *kc.DocumentRef
	work, replay, err = s.claimContentPublication(run, input, intent)
	if work.command.Validate() == nil {
		var retirementErr error
		retirement, retirementErr = s.publicationRetirement(work, func() {})
		if retirementErr != nil {
			return out, retirementErr
		}
		if retirementErr = retirement.own(source.Close); retirementErr != nil {
			return out, retirementErr
		}
	}
	if err != nil {
		return out, err
	}
	if replay != nil {
		return *replay, nil
	}
	prepared, err := s.prepareDirectPublication(run, input, intent, *source, work, retirement)
	if err != nil {
		return out, err
	}
	reuse, replay, err := s.inspectContentReuse(run, input, intent, work, prepared)
	if err != nil {
		return out, err
	}
	if replay != nil {
		return *replay, nil
	}
	if reuse != nil {
		return s.finishContentReuse(run, input, intent, reuse)
	}
	attempt, replay, err := s.reserveContentPublication(run, input, intent, work, prepared)
	if err != nil {
		return out, err
	}
	if replay != nil {
		return *replay, nil
	}
	if err = s.sendContentPublication(run, input, intent, work, prepared, attempt); err != nil {
		return out, err
	}
	return s.finishContentPublication(run, input, intent, work, prepared, attempt)
}

func contentCompletionError(result kc.DocumentRef, cause error) error {
	if result.Validate() == nil {
		// The canonical transaction is already confirmed. Cleanup failure may
		// require retry/lookup but cannot relabel that mutation not committed.
		return f.NewFault(f.DependencyUnavailable, f.Committed).WithCause(cause)
	}
	return portError(cause)
}

var _ kc.Documents = (*Service)(nil)
