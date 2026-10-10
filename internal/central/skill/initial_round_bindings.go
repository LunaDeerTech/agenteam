package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

// InitialRoundBindings reads Skill-owned facts. It neither grants Execution
// authority nor writes capture, assignment, Round or Model facts.
type InitialRoundBindings struct{ store Store }

func NewInitialRoundBindings(authority *Authority) (*InitialRoundBindings, error) {
	if authority.state() == nil || nilPort(authority.state().store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &InitialRoundBindings{store: authority.state().store}, nil
}

type initialRoundBindingsPlan struct {
	owner   *InitialRoundBindings
	request sc.InitialRoundBindingsRequest
	source  skillCaptureSource
	digest  f.Digest
	locks   []f.LockRequest
}

func (p *initialRoundBindingsPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (*initialRoundBindingsPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_initial_round_bindings_plan")
}
func (*initialRoundBindingsPlan) LogValue() slog.Value {
	return slog.StringValue("skill_initial_round_bindings_plan")
}
func (*initialRoundBindingsPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_initial_round_bindings_plan"`), nil
}
func (InitialRoundBindings) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_initial_round_bindings")
}
func (InitialRoundBindings) LogValue() slog.Value {
	return slog.StringValue("skill_initial_round_bindings")
}

func sameInitialRoundRequest(a, b sc.InitialRoundBindingsRequest) bool {
	return a.CaptureAttemptBinding == b.CaptureAttemptBinding &&
		a.Initial.Request == b.Initial.Request && a.Initial.AssignmentSequence == b.Initial.AssignmentSequence &&
		slices.Equal(a.Initial.Bindings, b.Initial.Bindings)
}

func (p *InitialRoundBindings) valid(ctx context.Context, r sc.InitialRoundBindingsRequest) error {
	if ctx == nil || r.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || nilPort(p.store) {
		return fault(f.DependencyUnbound)
	}
	return nil
}

func (p *InitialRoundBindings) inTx(ctx context.Context, tx f.Tx, locks []f.LockRequest) (postgres.SQLExecutor, error) {
	x, err := p.store.InTx(tx)
	if err != nil {
		return nil, captureError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	if err = p.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, captureError(err)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return x, nil
}

// The head is immutable; its record also identifies the protected default
// Skill when the captured directory is empty. Never infer empty from no row.
func loadInitialRoundCapture(ctx context.Context, x postgres.SQLExecutor, r sc.InitialRoundBindingsRequest) (skillCaptureRecord, error) {
	var empty skillCaptureRecord
	var project, agent, attempt, source string
	var sequence, count int64
	var raw []byte
	err := x.QueryRow(ctx, `SELECT project_id::text,agent_id::text,assignment_sequence,attempt_binding,source_digest,binding_count,record FROM agenteam_skill.execution_binding_heads WHERE execution_id=$1`, r.Initial.Request.ExecutionID.String()).Scan(&project, &agent, &sequence, &attempt, &source, &count, &raw)
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, fault(f.DependencyUnbound)
	}
	if err != nil {
		return empty, captureError(err)
	}
	if project != r.Initial.Request.ProjectID.String() || agent != r.Initial.Request.AgentID.String() || sequence != int64(r.Initial.AssignmentSequence) || attempt != string(r.CaptureAttemptBinding) || count != int64(len(r.Initial.Bindings)) {
		return empty, fault(f.ConfirmationStale)
	}
	if len(raw) == 0 || len(raw) > 256<<10 || count < 0 || count > 1 {
		return empty, unavailable(nil)
	}
	var record skillCaptureRecord
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&record) != nil || d.Decode(new(any)) != io.EOF || record.Format != 1 ||
		record.Attempt != r.CaptureAttemptBinding || record.SourceDigest != f.Digest(source) ||
		record.SourceDigest.Validate() != nil || record.Source.InitialSkillID.Validate() != nil ||
		record.Source.Result.Validate() != nil || !sameInitialRoundRequest(r, sc.InitialRoundBindingsRequest{Initial: record.Source.Result, CaptureAttemptBinding: record.Attempt}) {
		return empty, unavailable(nil)
	}
	digest, _, err := captureDigest(record.Source)
	if err != nil {
		return empty, err
	}
	if digest != record.SourceDigest {
		return empty, unavailable(nil)
	}
	return record, nil
}

func validateInitialRoundCapture(ctx context.Context, x postgres.SQLExecutor, r sc.InitialRoundBindingsRequest, record skillCaptureRecord) error {
	current, err := loadSkillCaptureSource(ctx, x, r.Initial.Request, record.Source.InitialSkillID)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return captureError(err)
	}
	digest, _, err := captureDigest(current)
	if err != nil {
		return err
	}
	if digest != record.SourceDigest {
		return fault(f.ConfirmationStale)
	}
	// Reuse the capture writer's complete canonical/row-count/assignment/
	// revision/Object verification without calling its INSERT path or issuer.
	expected := &initialBindingsPlan{request: r.Initial.Request, attempt: r.CaptureAttemptBinding, source: record.Source, digest: record.SourceDigest}
	found, err := verifySkillCapture(ctx, x, expected)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return captureError(err)
	}
	if !found {
		return fault(f.ConfirmationStale)
	}
	return nil
}

func (p *InitialRoundBindings) DiscoverInitialRoundBindings(ctx context.Context, r sc.InitialRoundBindingsRequest) (sc.InitialRoundBindingsPlan, error) {
	if err := p.valid(ctx, r); err != nil {
		return nil, err
	}
	r = r.Clone()
	locks, err := captureLocks(r.Initial.Request, nil)
	if err != nil {
		return nil, captureError(err)
	}
	var record skillCaptureRecord
	// The first short transaction discovers the real Skill key. The second
	// takes a new complete union; neither transaction adds a late lock.
	for pass := 0; pass < 2; pass++ {
		id, err := f.NewID[f.TransactionAttempt]()
		if err != nil {
			return nil, unavailable(err)
		}
		cause, err := f.NewJobCause("skill-initial-round", r.Initial.Request.ExecutionID.String(), id.String())
		if err != nil {
			return nil, err
		}
		result := p.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			if err := p.store.AcquireAll(ctx, tx, locks); err != nil {
				return captureError(err)
			}
			x, err := p.inTx(ctx, tx, locks)
			if err != nil {
				return err
			}
			stored, err := loadInitialRoundCapture(ctx, x, r)
			if err != nil {
				return err
			}
			if pass == 1 {
				if stored.SourceDigest != record.SourceDigest {
					return fault(f.ConfirmationStale)
				}
				if err = validateInitialRoundCapture(ctx, x, r, stored); err != nil {
					return err
				}
			}
			record = stored
			return ctx.Err()
		})
		if err = commitError(result); err != nil {
			return nil, err
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if pass == 0 {
			locks, err = captureLocks(r.Initial.Request, &record.Source.InitialSkillID)
			if err != nil {
				return nil, captureError(err)
			}
		}
	}
	return &initialRoundBindingsPlan{owner: p, request: r, source: record.Source, digest: record.SourceDigest, locks: slices.Clone(locks)}, nil
}

func (p *InitialRoundBindings) ReadInitialRoundBindingsInTx(ctx context.Context, tx f.Tx, r sc.InitialRoundBindingsRequest, plan sc.InitialRoundBindingsPlan) (sc.InitialSkillBindings, error) {
	var empty sc.InitialSkillBindings
	if err := p.valid(ctx, r); err != nil {
		return empty, err
	}
	selected, ok := plan.(*initialRoundBindingsPlan)
	if !ok || selected == nil || selected.owner != p || !sameInitialRoundRequest(selected.request, r) {
		return empty, fault(f.Forbidden)
	}
	x, err := p.inTx(ctx, tx, selected.locks)
	if err != nil {
		return empty, err
	}
	stored, err := loadInitialRoundCapture(ctx, x, r)
	if err != nil {
		return empty, err
	}
	if stored.SourceDigest != selected.digest {
		return empty, fault(f.ConfirmationStale)
	}
	if err = validateInitialRoundCapture(ctx, x, r, stored); err != nil {
		return empty, err
	}
	return stored.Source.Result.Clone(), nil
}

var _ sc.InitialRoundBindingsReader = (*InitialRoundBindings)(nil)
