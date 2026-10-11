package execution

import (
	"bytes"
	"context"
	"reflect"
	"sync/atomic"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mt "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
)

type directTextMountKey struct{}

// Only the original Execution reader/writer installs this synchronous proof,
// after reading canonical input/Snapshot/terminal in its current transaction.
// It is neither a public receipt nor permission to run a Runner operation.
type directTextMountWitness struct {
	state      *directTextState
	run        *directTextCall
	tx         f.Tx
	input      c.PreparationInput
	use        mt.ExecutionMountUse
	retirement *mt.ExecutionMountRetirement
	mode       string
	active     atomic.Bool
}

func directTextMountUse(snapshot c.DirectTextSnapshot) mt.ExecutionMountUse {
	input := snapshot.Fields().Context.Input()
	return mt.ExecutionMountUse{Mounts: input.Fields().Mounts.Clone(), InputDigest: input.Digest(), SnapshotDigest: snapshot.Digest()}
}

func directTextMountRetirement(terminal *directTextTerminal) *mt.ExecutionMountRetirement {
	if terminal == nil {
		return nil
	}
	return &mt.ExecutionMountRetirement{Version: terminal.version, EventID: terminal.eventID}
}

func (s *directTextState) withMountProof(ctx context.Context, tx f.Tx, run *directTextCall, input c.PreparationInput, use mt.ExecutionMountUse, retirement *mt.ExecutionMountRetirement, mode string, call func(context.Context) error) error {
	if input.Validate() != nil || !input.Fields().Request.Equal(run.request) {
		return fault(f.InvalidState)
	}
	if len(input.Fields().Mounts.Mounts) == 0 {
		return ctx.Err() // The historical empty profile has no new rows or port.
	}
	if nilPort(s.deps.Mounts) {
		return fault(f.DependencyUnbound)
	}
	w := &directTextMountWitness{state: s, run: run, tx: tx, input: input, use: use.Clone(), mode: mode}
	if retirement != nil {
		v := *retirement
		w.retirement = &v
	}
	w.active.Store(true)
	defer w.active.Store(false)
	if err := call(context.WithValue(ctx, directTextMountKey{}, w)); err != nil {
		return portError(err)
	}
	return ctx.Err()
}

func (s *directTextState) verifyDirectTextMountMaterial(ctx context.Context, tx f.Tx, run *directTextCall, input c.PreparationInput) error {
	return s.withMountProof(ctx, tx, run, input, mt.ExecutionMountUse{}, nil, "material", func(ctx context.Context) error {
		return s.deps.Mounts.VerifyExecutionMountMaterialInTx(ctx, tx, input.Fields().Mounts.Clone())
	})
}

// Historical reads distinguish preparing material, running use and a sealed
// terminal retirement. None of them resolve current Agent/Mount/Runner facts.
func (s *directTextState) verifyDirectTextMountUse(ctx context.Context, tx f.Tx, run *directTextCall, row *executionRecord, stored *directTextRecord, input *preparationInputRecord) error {
	if row == nil || stored == nil || input == nil || !bytes.Equal(stored.snapshot.Fields().Context.Input().CanonicalBytes(), input.input.CanonicalBytes()) || !((c.PreparationRequest{ExecutionID: row.summary.ID, Launch: row.launch}).Equal(run.request)) || row.summary.SnapshotID == nil || *row.summary.SnapshotID != stored.snapshot.Fields().ID {
		return fault(f.InvalidState)
	}
	var retired *mt.ExecutionMountRetirement
	if row.summary.Status.Terminal() {
		if stored.terminal == nil || !directTextTerminalMatches(row, stored, stored.terminal) {
			return fault(f.InvalidState)
		}
		retired = directTextMountRetirement(stored.terminal)
	} else if row.summary.Status != c.Running || stored.terminal != nil {
		return fault(f.InvalidState)
	}
	use := directTextMountUse(stored.snapshot)
	return s.withMountProof(ctx, tx, run, input.input, use, retired, "verify", func(ctx context.Context) error {
		return s.deps.Mounts.VerifyExecutionMountUseInTx(ctx, tx, use.Clone(), retired)
	})
}

func (s *directTextState) acquireDirectTextMountUse(ctx context.Context, tx f.Tx, run *directTextCall) error {
	use := directTextMountUse(run.snapshot)
	return s.withMountProof(ctx, tx, run, run.input.input, use, nil, "acquire", func(ctx context.Context) error {
		return s.deps.Mounts.AcquireExecutionMountUseInTx(ctx, tx, use.Clone())
	})
}

func (s *directTextState) retireDirectTextMountUse(ctx context.Context, tx f.Tx, run *directTextCall) error {
	use, retired := directTextMountUse(run.snapshot), directTextMountRetirement(run.terminal)
	if retired == nil {
		return fault(f.InvalidState)
	}
	return s.withMountProof(ctx, tx, run, run.input.input, use, retired, "retire", func(ctx context.Context) error {
		return s.deps.Mounts.RetireExecutionMountUseInTx(ctx, tx, use.Clone(), *retired)
	})
}

func (a *Authority) directTextMountProof(ctx context.Context, tx f.Tx, expected mt.ExecutionMountSet) (*directTextMountWitness, error) {
	if ctx == nil || expected.Validate() != nil || len(expected.Mounts) == 0 {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return nil, err
	}
	w, ok := ctx.Value(directTextMountKey{}).(*directTextMountWitness)
	if !ok || w == nil || !w.active.Load() || w.state != run.owner || w.run != run || w.tx != tx || w.input.Validate() != nil || !w.input.Fields().Request.Equal(run.request) || !reflect.DeepEqual(w.input.Fields().Mounts, expected) {
		return nil, fault(f.Forbidden)
	}
	if _, err = w.state.store.InTx(tx); err != nil {
		return nil, portError(err)
	}
	locks, err := preparationLocks(run.request)
	if err != nil {
		return nil, err
	}
	if err = w.state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, portError(err)
	}
	if !w.active.Load() {
		return nil, fault(f.Forbidden)
	}
	return w, ctx.Err()
}

func (a *Authority) RequireExecutionMountUseAcquireInTx(ctx context.Context, tx f.Tx, use mt.ExecutionMountUse) error {
	if use.Validate() != nil {
		return invalid()
	}
	w, err := a.directTextMountProof(ctx, tx, use.Mounts)
	if err != nil {
		return err
	}
	if w.mode != "acquire" || !reflect.DeepEqual(w.use, use) || w.retirement != nil {
		return fault(f.Forbidden)
	}
	facts, err := a.directTextConfigurationScope(ctx, tx)
	if err != nil {
		return err
	}
	if facts.Snapshot.Digest() != use.SnapshotDigest || facts.Snapshot.Fields().Context.InputDigest() != use.InputDigest {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

func (a *Authority) RequireExecutionMountUseVerifyInTx(ctx context.Context, tx f.Tx, use mt.ExecutionMountUse, retired *mt.ExecutionMountRetirement) error {
	if use.Validate() != nil || retired != nil && retired.Validate() != nil {
		return invalid()
	}
	w, err := a.directTextMountProof(ctx, tx, use.Mounts)
	if err != nil {
		return err
	}
	if w.mode != "verify" || !reflect.DeepEqual(w.use, use) || !reflect.DeepEqual(w.retirement, retired) {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

func (a *Authority) RequireExecutionMountUseRetireInTx(ctx context.Context, tx f.Tx, use mt.ExecutionMountUse, retired mt.ExecutionMountRetirement) error {
	if use.Validate() != nil || retired.Validate() != nil {
		return invalid()
	}
	w, err := a.directTextMountProof(ctx, tx, use.Mounts)
	if err != nil {
		return err
	}
	if w.mode != "retire" || !reflect.DeepEqual(w.use, use) || w.retirement == nil || *w.retirement != retired {
		return fault(f.Forbidden)
	}
	facts, err := a.directTextRetirementScope(ctx, tx)
	if err != nil {
		return err
	}
	if facts.Snapshot.Digest() != use.SnapshotDigest || facts.Snapshot.Fields().Context.InputDigest() != use.InputDigest || facts.Summary.Version != retired.Version || w.run.terminal.eventID != retired.EventID {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

var _ mt.ExecutionMountUseAuthority = (*Authority)(nil)
