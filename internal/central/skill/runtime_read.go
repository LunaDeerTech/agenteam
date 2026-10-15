package skill

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sync/atomic"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

const MaxRuntimeSkillTextBytes = 256 << 10

type runtimeReadKey struct{}
type runtimeReadWitness struct {
	authority  *Authority
	invocation sc.Invocation
	plan       sc.InvocationPlan
	arguments  []byte
	bindings   []sc.SkillBinding
	locks      []f.LockRequest
	binding    sc.SkillBinding
	metadata   sc.RevisionMetadata
	active     atomic.Bool
}

type runtimeReadArgs struct {
	SkillID    sc.SkillID `json:"skill_id"`
	Path       string     `json:"path"`
	ByteOffset int64      `json:"byte_offset,omitempty"`
	MaxBytes   int        `json:"max_bytes,omitempty"`
}

func decodeRuntimeRead(inv sc.Invocation, raw []byte) (runtimeReadArgs, error) {
	var r runtimeReadArgs
	if len(raw) > 8192 {
		return r, invalid()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if inv.Intent == sc.ListSkills {
		var fields map[string]json.RawMessage
		if d.Decode(&fields) != nil || fields == nil || len(fields) != 0 {
			return r, invalid()
		}
	} else if inv.Intent == sc.ReadSkill {
		if d.Decode(&r) != nil || r.SkillID.Validate() != nil || sc.ValidatePath(r.Path) != nil || r.ByteOffset < 0 || r.ByteOffset > sc.MaxFileBytes || r.MaxBytes < 0 || r.MaxBytes > MaxRuntimeSkillTextBytes {
			return r, invalid()
		}
		if r.MaxBytes == 0 {
			r.MaxBytes = MaxRuntimeSkillTextBytes
		}
	} else {
		return r, fault(f.CapabilityUnsupported)
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return r, invalid()
	}
	return r, nil
}

func (s *Service) RequireSkillBuiltinBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if s.state() == nil || nilPort(s.state().authority.state().runtime) {
		return fault(f.DependencyUnbound)
	}
	if _, err := s.state().authority.state().store.InTx(tx); err != nil {
		return portError(err)
	}
	return s.state().authority.state().runtime.RequireSkillRuntimeBindingInTx(ctx, tx, store)
}

func (a *Authority) runtimeReadTx(ctx context.Context, w *runtimeReadWitness, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	cause, err := f.NewRecoveryCause("skill.runtime-read", w.invocation.ExecutionID.String(), w.invocation.OperationID)
	if err != nil {
		return err
	}
	var callbackErr error
	result := a.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if err = a.state().store.AcquireAll(ctx, tx, w.locks); err != nil {
			return portError(err)
		}
		x, err := a.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return fn(ctx, tx, x)
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return callbackErr
	}
	return commitError(result)
}

// ExecuteSkillReadTool reads only the caller's effective directory. The supplied
// bytes must equal the original protected Tool input in every metadata gate.
func (s *Service) ExecuteSkillReadTool(ctx context.Context, inv sc.Invocation, raw []byte) ([]byte, error) {
	if inv.Validate() != nil {
		return nil, invalid()
	}
	args, err := decodeRuntimeRead(inv, raw)
	if err != nil {
		return nil, err
	}
	if s.state() == nil || nilPort(s.state().authority.state().runtime) {
		return nil, fault(f.DependencyUnbound)
	}
	call, err := s.begin(ctx, false)
	if err != nil {
		return nil, err
	}
	defer s.end(call)
	a := s.state().authority
	plan, err := a.state().runtime.DiscoverSkillInvocation(call.ctx, inv)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(plan) {
		return nil, fault(f.DependencyUnbound)
	}
	w := &runtimeReadWitness{authority: a, invocation: inv, plan: plan, arguments: bytes.Clone(raw), locks: plan.RequiredLocks()}
	w.active.Store(true)
	defer w.active.Store(false)
	err = a.runtimeReadTx(call.ctx, w, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		return a.state().runtime.WithSkillInvocationInTx(ctx, tx, inv, plan, func(_ context.Context, original []byte, bindings []sc.SkillBinding) error {
			if !bytes.Equal(original, w.arguments) {
				return fault(f.Forbidden)
			}
			w.bindings = slices.Clone(bindings)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	for _, b := range w.bindings {
		w.locks = append(w.locks, skillLock(b.SkillID, f.Shared))
	}
	initial, err := loadInitialization(call.ctx, a.state().store, inv.ProjectID)
	if err != nil {
		return nil, err
	}
	if initial == nil {
		return nil, fault(f.InvalidState)
	}
	w.locks = append(w.locks, skillLock(initial.skill, f.Shared))
	w.locks, err = oc.NormalizeAccessLocks(w.locks)
	if err != nil {
		return nil, err
	}
	visible := make([]sc.SkillBinding, 0, len(w.bindings))
	err = a.runtimeReadTx(call.ctx, w, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return a.withRuntimeReadInTx(ctx, tx, w, func(current context.Context) error {
			validation := &AssignmentValidation{store: a.state().store}
			for _, b := range w.bindings {
				validity, err := validation.ValidateAssignmentInTx(current, tx, inv.ProjectID, inv.AgentID, b.AssignmentID, b.SkillID, b.RevisionID, b.Revision)
				if err != nil {
					return err
				}
				if validity != sc.AssignmentValid {
					continue
				}
				visible = append(visible, b)
				if inv.Intent == sc.ReadSkill && args.SkillID == b.SkillID {
					w.binding = b
					_, w.metadata, err = loadAssignmentRevision(current, x, inv.ProjectID, b.SkillID)
					if err != nil {
						return err
					}
					if !runtimeMetadataMatches(w.metadata, b) {
						return fault(f.ConfirmationStale)
					}
				}
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if inv.Intent == sc.ListSkills {
		type item struct {
			SkillID     sc.SkillID `json:"skill_id"`
			Revision    f.Revision `json:"revision"`
			Name        string     `json:"name"`
			Description string     `json:"description"`
		}
		items := make([]item, 0, len(visible))
		for _, b := range visible {
			items = append(items, item{b.SkillID, b.Revision, b.Name, b.Description})
		}
		out, err := json.Marshal(struct {
			Skills []item `json:"skills"`
		}{items})
		if len(out) > 1<<20 {
			return nil, fault(f.CapabilityUnsupported)
		}
		return out, err
	}
	if w.binding.SkillID != args.SkillID {
		return nil, fault(f.Forbidden)
	}
	w.locks, err = oc.NormalizeAccessLocks(append(w.locks, objectLock(w.metadata.Object.ID, f.Shared)))
	if err != nil {
		return nil, err
	}
	reader, err := s.OpenPackage(context.WithValue(call.ctx, runtimeReadKey{}, w), inv.Actor, inv.ProjectID, w.binding.SkillID, w.binding.Revision)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(reader, sc.MaxArchiveBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || !reader.Joined() {
		return nil, errors.Join(readErr, closeErr, fault(f.DependencyUnavailable))
	}
	if len(body) > sc.MaxArchiveBytes || sum(body) != w.binding.PackageSHA256 {
		return nil, fault(f.InvalidState)
	}
	if _, err = ParseCanonicalPackage(call.ctx, body); err != nil {
		return nil, err
	}
	text, next, truncated, err := readRuntimePackageText(body, args)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		SkillID   sc.SkillID `json:"skill_id"`
		Revision  f.Revision `json:"revision"`
		Path      string     `json:"path"`
		Text      string     `json:"text"`
		Next      int64      `json:"next_byte_offset"`
		Truncated bool       `json:"truncated"`
	}{w.binding.SkillID, w.binding.Revision, args.Path, text, next, truncated})
}

func runtimeMetadataMatches(m sc.RevisionMetadata, b sc.SkillBinding) bool {
	return m.Validate() == nil && m.ID == b.RevisionID && m.SkillID == b.SkillID && m.Revision == b.Revision && m.PackageSHA256 == b.PackageSHA256 && m.Name == b.Name && m.Description == b.Description && m.EntryPath == b.EntryPath
}

func (a *Authority) withRuntimeReadInTx(ctx context.Context, tx f.Tx, w *runtimeReadWitness, fn func(context.Context) error) error {
	if w == nil || !w.active.Load() || w.authority != a || nilPort(a.state().runtime) {
		return fault(f.Forbidden)
	}
	if err := a.state().store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return portError(err)
	}
	return a.state().runtime.WithSkillInvocationInTx(ctx, tx, w.invocation, w.plan, func(current context.Context, raw []byte, bindings []sc.SkillBinding) error {
		if !bytes.Equal(raw, w.arguments) || !slices.Equal(bindings, w.bindings) || !w.active.Load() {
			return fault(f.Forbidden)
		}
		return fn(current)
	})
}

func (s *Service) runtimePackageReadTx(ctx context.Context, actor id.Actor, project id.ProjectID, work func(context.Context, f.Tx, postgres.SQLExecutor, initializationRow) error, targets []sc.SkillID) error {
	w, _ := ctx.Value(runtimeReadKey{}).(*runtimeReadWitness)
	if w == nil || !w.active.Load() || w.authority != s.state().authority || !w.invocation.Actor.Equal(actor) || w.invocation.ProjectID != project || w.invocation.Intent != sc.ReadSkill || len(targets) != 1 || targets[0] != w.binding.SkillID {
		return fault(f.Forbidden)
	}
	return w.authority.runtimeReadTx(ctx, w, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return w.authority.withRuntimeReadInTx(ctx, tx, w, func(current context.Context) error {
			if err := w.authority.requireRuntimeAssignmentInTx(current, tx, w); err != nil {
				return err
			}
			row, err := loadInitialization(current, x, project)
			if err != nil {
				return err
			}
			if row == nil || row.phase != initializationPublished {
				return fault(f.InvalidState)
			}
			return work(current, tx, x, *row)
		})
	})
}

func (a *Authority) requireRuntimeAssignmentInTx(ctx context.Context, tx f.Tx, w *runtimeReadWitness) error {
	v := &AssignmentValidation{store: a.state().store}
	validity, err := v.ValidateAssignmentInTx(ctx, tx, w.invocation.ProjectID, w.invocation.AgentID, w.binding.AssignmentID, w.binding.SkillID, w.binding.RevisionID, w.binding.Revision)
	if err != nil {
		return err
	}
	if validity != sc.AssignmentValid {
		return fault(f.Forbidden)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	_, metadata, err := loadAssignmentRevision(ctx, x, w.invocation.ProjectID, w.binding.SkillID)
	if err != nil {
		return err
	}
	if !runtimeMetadataMatches(metadata, w.binding) || metadata.Object.ID != w.metadata.Object.ID {
		return fault(f.ConfirmationStale)
	}
	return nil
}

func readRuntimePackageText(raw []byte, args runtimeReadArgs) (string, int64, bool, error) {
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", 0, false, invalid()
	}
	for _, file := range z.File {
		if file.Name != args.Path {
			continue
		}
		r, err := file.Open()
		if err != nil {
			return "", 0, false, invalid()
		}
		b, err := io.ReadAll(io.LimitReader(r, sc.MaxFileBytes+1))
		closeErr := r.Close()
		if err != nil || closeErr != nil || len(b) > sc.MaxFileBytes || !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
			return "", 0, false, fault(f.CapabilityUnsupported)
		}
		start := int(args.ByteOffset)
		if start > len(b) || start < len(b) && !utf8.RuneStart(b[start]) {
			return "", 0, false, invalid()
		}
		end := min(start+args.MaxBytes, len(b))
		for end > start && end < len(b) && !utf8.RuneStart(b[end]) {
			end--
		}
		if end == start && start < len(b) {
			return "", 0, false, invalid()
		}
		return string(b[start:end]), int64(end), end < len(b), nil
	}
	return "", 0, false, fault(f.NotFound)
}

// JoinSkillReadTool retires only the returned original package accounting.
// It neither reopens bytes nor repeats list/read after a failed or unknown Tx.
func (s *Service) JoinSkillReadTool(ctx context.Context, inv sc.Invocation) error {
	if s.state() == nil || inv.Validate() != nil {
		return fault(f.DependencyUnbound)
	}
	authority, ok := s.state().authority.state().runtime.(sc.InvocationRetirementAuthority)
	if !ok {
		return fault(f.DependencyUnbound)
	}
	if err := authority.RequireSkillInvocationRetirement(ctx, inv); err != nil {
		return portError(err)
	}
	state := s.state()
	for {
		state.mu.Lock()
		ready := []*ownedWork{}
		pending := false
		for _, work := range state.work {
			w, _ := work.call.ctx.Value(runtimeReadKey{}).(*runtimeReadWitness)
			if w == nil || w.authority != state.authority || !sameRuntimeInvocation(w.invocation, inv) {
				continue
			}
			if !work.returned {
				pending = true
			} else {
				ready = append(ready, work)
			}
		}
		changed := state.changed
		state.mu.Unlock()
		for _, work := range ready {
			if err := s.retireOwnedWork(ctx, work); err != nil {
				return err
			}
		}
		if !pending {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func sameRuntimeInvocation(a, b sc.Invocation) bool {
	return a.Actor.Equal(b.Actor) && a.ProjectID == b.ProjectID && a.AgentID == b.AgentID && a.ExecutionID == b.ExecutionID && a.OperationID == b.OperationID && a.AttemptID == b.AttemptID && a.ToolCallID == b.ToolCallID && a.ToolID == b.ToolID && a.SpecRevision == b.SpecRevision && a.Intent == b.Intent
}
