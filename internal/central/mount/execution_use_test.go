package mount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These are controlled same-Tx protocol checks, not proof of real running,
// consumer join, PostgreSQL constraints or rollback. The Execution integration
// fixture supplies those facts through its real owner and original transaction.
type mountUseStore struct {
	*captureMaterialStore
	useRow, retirementRow             []any
	useWrites, retireWrites, useReads int
	authorized                        string
	nilExecutor                       bool
	queryError, writeError            error
	cancelWrite                       context.CancelFunc
}

func (*mountUseStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	panic("use opened a transaction")
}
func (*mountUseStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	panic("use acquired locks")
}
func (s *mountUseStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, fault(f.DependencyUnavailable)
	}
	if s.nilExecutor {
		return nil, nil
	}
	return s, nil
}
func (s *mountUseStore) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	if s.authorized == "" {
		panic("use read without owner witness")
	}
	s.useReads++
	if s.queryError != nil {
		return controlRow{err: s.queryError}
	}
	switch {
	case strings.HasPrefix(sql, "SELECT project_id,agent_id,agent_version,attempt_binding,capture_digest,input_digest,snapshot_digest"):
		if s.useRow == nil {
			return controlRow{err: pgx.ErrNoRows}
		}
		return controlRow{values: slices.Clone(s.useRow)}
	case strings.HasPrefix(sql, "SELECT terminal_version,terminal_event_id::text"):
		if s.retirementRow == nil {
			return controlRow{err: pgx.ErrNoRows}
		}
		return controlRow{values: slices.Clone(s.retirementRow)}
	default:
		return s.captureMaterialStore.QueryRow(ctx, sql, args...)
	}
}
func (s *mountUseStore) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if s.writeError != nil {
		return pgconn.CommandTag{}, s.writeError
	}
	switch {
	case strings.HasPrefix(sql, "INSERT INTO agenteam_mount.execution_uses("):
		if s.authorized != "acquire" || s.useRow != nil || s.retirementRow != nil {
			panic("invalid use insert")
		}
		s.useWrites++
		s.useRow = slices.Clone(args[1:])
	case strings.HasPrefix(sql, "INSERT INTO agenteam_mount.execution_use_retirements("):
		if s.authorized != "retire" || s.useRow == nil || s.retirementRow != nil {
			panic("invalid retirement insert")
		}
		s.retireWrites++
		s.retirementRow = slices.Clone(args[1:])
	default:
		panic("use rewrote capture or unexpected table")
	}
	if s.cancelWrite != nil {
		s.cancelWrite()
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type mountUseProofKey struct{}
type mountUseOwner struct {
	store      *mountUseStore
	expected   mc.ExecutionMountUse
	terminal   mc.ExecutionMountRetirement
	phase      string
	joined     bool
	ownerError error
	cancel     context.CancelFunc
	mutate     bool
	calls      int
}

func (o *mountUseOwner) check(ctx context.Context, tx f.Tx, use mc.ExecutionMountUse, action string) error {
	o.calls++
	if o.ownerError != nil {
		return o.ownerError
	}
	if ctx.Value(mountUseProofKey{}) != o || tx != o.store.tx {
		return fault(f.Forbidden)
	}
	expected, _ := json.Marshal(o.expected)
	actual, _ := json.Marshal(use)
	if !bytes.Equal(actual, expected) {
		return fault(f.Forbidden)
	}
	if err := o.store.RequireHeldLocks(ctx, tx, mountCaptureLocks(use.Mounts.Request)); err != nil {
		return err
	}
	o.store.authorized = action
	if o.cancel != nil {
		o.cancel()
	}
	if o.mutate {
		*use.Mounts.Mounts[0].Description = "owner mutated its private copy"
	}
	return nil
}
func (o *mountUseOwner) RequireMountMaterialInTx(ctx context.Context, tx f.Tx, set mc.ExecutionMountSet) error {
	use := o.expected.Clone()
	use.Mounts = set
	return o.check(ctx, tx, use, "material")
}
func (o *mountUseOwner) RequireExecutionMountUseAcquireInTx(ctx context.Context, tx f.Tx, use mc.ExecutionMountUse) error {
	if o.phase != "running" {
		return fault(f.InvalidState)
	}
	return o.check(ctx, tx, use, "acquire")
}
func (o *mountUseOwner) RequireExecutionMountUseVerifyInTx(ctx context.Context, tx f.Tx, use mc.ExecutionMountUse, retired *mc.ExecutionMountRetirement) error {
	if retired == nil && o.phase != "running" || retired != nil && (o.phase != "terminal" || *retired != o.terminal) {
		return fault(f.InvalidState)
	}
	err := o.check(ctx, tx, use, "verify")
	if retired != nil && o.mutate {
		retired.Version++
	}
	return err
}
func (o *mountUseOwner) RequireExecutionMountUseRetireInTx(ctx context.Context, tx f.Tx, use mc.ExecutionMountUse, retired mc.ExecutionMountRetirement) error {
	if o.phase != "terminal" || !o.joined || retired != o.terminal {
		return fault(f.InvalidState)
	}
	return o.check(ctx, tx, use, "retire")
}

func executionUseFixture(t *testing.T) (*ExecutionUse, *mountUseStore, *mountUseOwner, mc.ExecutionMountUse, context.Context) {
	t.Helper()
	capture, original, _, _, request, ctx := nonemptyMountCaptureFixture(t)
	plan, err := capture.DiscoverExecutionMounts(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	set, err := capture.CaptureExecutionMountsInTx(ctx, original.tx, request, plan)
	if err != nil {
		t.Fatal(err)
	}
	use := mc.ExecutionMountUse{Mounts: set, InputDigest: f.Digest("sha256:" + strings.Repeat("d", 64)), SnapshotDigest: f.Digest("sha256:" + strings.Repeat("e", 64))}
	s := &mountUseStore{captureMaterialStore: original}
	s.forbidCurrentReads = true
	o := &mountUseOwner{store: s, expected: use.Clone(), phase: "running", terminal: mc.ExecutionMountRetirement{Version: 4, EventID: testID[event.EventIdentity](t, 95)}}
	p, err := NewExecutionUse(s, o)
	if err != nil {
		t.Fatal(err)
	}
	return p, s, o, use, context.WithValue(context.Background(), mountUseProofKey{}, o)
}

func TestExecutionMountUseLiveAndRetiredIdentity(t *testing.T) {
	p, s, o, use, ctx := executionUseFixture(t)
	material := bytes.Clone(s.material.raw)
	refs := slices.Clone(s.refs)
	oldWrites := s.writes
	if err := p.VerifyExecutionMountMaterialInTx(ctx, s.tx, use.Mounts); err != nil {
		t.Fatal(err)
	}
	if err := p.VerifyExecutionMountUseInTx(ctx, s.tx, use, nil); err == nil {
		t.Fatal("missing use accepted as live")
	}
	o.mutate = true
	for range 2 {
		if err := p.AcquireExecutionMountUseInTx(ctx, s.tx, use); err != nil {
			t.Fatal(err)
		}
		if err := p.VerifyExecutionMountUseInTx(ctx, s.tx, use, nil); err != nil {
			t.Fatal(err)
		}
	}
	if s.useWrites != 1 || s.retireWrites != 0 {
		t.Fatal("live replay wrote or retired use")
	}
	// Current configuration is deliberately unavailable, and is never reread.
	s.definitions = nil
	s.mappings = nil
	s.head.ids = nil
	o.phase, o.joined = "terminal", true
	for range 2 {
		if err := p.RetireExecutionMountUseInTx(ctx, s.tx, use, o.terminal); err != nil {
			t.Fatal(err)
		}
		if err := p.VerifyExecutionMountUseInTx(ctx, s.tx, use, &o.terminal); err != nil {
			t.Fatal(err)
		}
	}
	if s.useWrites != 1 || s.retireWrites != 1 || s.writes != oldWrites || !bytes.Equal(material, s.material.raw) || !slices.Equal(refs, s.refs) {
		t.Fatal("retirement modified original capture or replay wrote twice")
	}
	// Even an owner allowing a live check cannot turn a retired row back live.
	o.phase = "running"
	if p.AcquireExecutionMountUseInTx(ctx, s.tx, use) == nil || p.VerifyExecutionMountUseInTx(ctx, s.tx, use, nil) == nil {
		t.Fatal("retired use revived")
	}
}

func TestExecutionMountUseRequiresOriginalAuthorityAndTransaction(t *testing.T) {
	for _, mode := range []string{"foreign-tx", "missing-locks", "no-witness", "nil-executor", "preparing", "cancelled", "owner-cancel", "unknown-owner"} {
		t.Run(mode, func(t *testing.T) {
			p, s, o, use, ctx := executionUseFixture(t)
			tx := s.tx
			var unknown error
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "missing-locks":
				s.missingLocks = true
			case "no-witness":
				ctx = context.Background()
			case "nil-executor":
				s.nilExecutor = true
			case "preparing":
				o.phase = "preparing"
			case "cancelled", "owner-cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				if mode == "cancelled" {
					cancel()
				} else {
					o.cancel = cancel
				}
			case "unknown-owner":
				unknown = f.NewFault(f.CommitUnknown, f.Unknown)
				o.ownerError = unknown
			}
			err := p.AcquireExecutionMountUseInTx(ctx, tx, use)
			if err == nil || s.useReads != 0 || s.useWrites != 0 || s.retireWrites != 0 {
				t.Fatal("denied use reached SQL", err)
			}
			if unknown != nil && err != unknown {
				t.Fatal("owner Unknown was replaced")
			}
			if (mode == "cancelled" || mode == "owner-cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost")
			}
		})
	}
	var nilOwner *mountUseOwner
	if _, err := NewExecutionUse(nil, nilOwner); err == nil {
		t.Fatal("unbound constructor accepted")
	}
}

func TestExecutionMountUseRejectsIncompleteOrConflictingHistory(t *testing.T) {
	for _, mode := range []string{"missing-material", "missing-ref", "changed-material", "project", "agent", "owner-version", "attempt", "capture-digest", "input-digest", "snapshot-digest", "retired-bad-version", "retired-bad-event", "retired-other-version", "retired-other-event", "missing-retirement", "missing-use"} {
		t.Run(mode, func(t *testing.T) {
			p, s, o, use, ctx := executionUseFixture(t)
			if err := p.AcquireExecutionMountUseInTx(ctx, s.tx, use); err != nil {
				t.Fatal(err)
			}
			var expected *mc.ExecutionMountRetirement
			switch mode {
			case "missing-material":
				s.material = nil
			case "missing-ref":
				s.refs = s.refs[:1]
			case "changed-material":
				s.material.raw = []byte(`{}`)
			case "project":
				s.useRow[0] = "wrong-project"
			case "agent":
				s.useRow[1] = "wrong-agent"
			case "owner-version":
				s.useRow[2] = int64(0)
			case "attempt":
				s.useRow[3] = "wrong-attempt"
			case "capture-digest":
				s.useRow[4] = "wrong-capture"
			case "input-digest":
				use.InputDigest = f.Digest("sha256:" + strings.Repeat("a", 64))
				o.expected = use.Clone()
			case "snapshot-digest":
				use.SnapshotDigest = f.Digest("sha256:" + strings.Repeat("b", 64))
				o.expected = use.Clone()
			case "missing-use":
				s.useRow = nil
			default:
				o.phase, o.joined = "terminal", true
				expected = &o.terminal
				if mode != "missing-retirement" {
					if err := p.RetireExecutionMountUseInTx(ctx, s.tx, use, o.terminal); err != nil {
						t.Fatal(err)
					}
				}
				switch mode {
				case "retired-bad-version":
					s.retirementRow[0] = int64(0)
				case "retired-bad-event":
					s.retirementRow[1] = "invalid"
				case "retired-other-version":
					s.retirementRow[0] = int64(5)
				case "retired-other-event":
					s.retirementRow[1] = testID[event.EventIdentity](t, 96).String()
				}
			}
			writes := s.useWrites + s.retireWrites
			if err := p.VerifyExecutionMountUseInTx(ctx, s.tx, use, expected); err == nil {
				t.Fatal("corrupt or conflicting history accepted")
			}
			if s.useWrites+s.retireWrites != writes {
				t.Fatal("verification repaired corrupt history")
			}
		})
	}
}

func TestExecutionMountUseRetirementRequiresJoinAndOriginalTerminal(t *testing.T) {
	for _, mode := range []string{"still-running", "not-joined", "wrong-event", "wrong-version", "missing-use", "conflicting-retirement"} {
		t.Run(mode, func(t *testing.T) {
			p, s, o, use, ctx := executionUseFixture(t)
			if mode != "missing-use" {
				if err := p.AcquireExecutionMountUseInTx(ctx, s.tx, use); err != nil {
					t.Fatal(err)
				}
			}
			o.phase, o.joined = "terminal", true
			expected := o.terminal
			switch mode {
			case "still-running":
				o.phase = "running"
			case "not-joined":
				o.joined = false
			case "wrong-event":
				expected.EventID = testID[event.EventIdentity](t, 96)
			case "wrong-version":
				expected.Version++
			case "conflicting-retirement":
				s.retirementRow = []any{int64(expected.Version + 1), expected.EventID.String()}
			}
			if err := p.RetireExecutionMountUseInTx(ctx, s.tx, use, expected); err == nil || s.retireWrites != 0 {
				t.Fatal("premature/conflicting retirement accepted", err)
			}
		})
	}
}

func TestExecutionMountUseStorageFailureIsNotSuccess(t *testing.T) {
	for _, mode := range []string{"read", "acquire-write", "retire-write", "cancel-after-write"} {
		t.Run(mode, func(t *testing.T) {
			p, s, o, use, ctx := executionUseFixture(t)
			canary := errors.New("PRIVATE_USE_SQL_DIAGNOSTIC")
			var err error
			switch mode {
			case "read":
				s.queryError = canary
			case "acquire-write":
				s.writeError = canary
			case "retire-write":
				if err := p.AcquireExecutionMountUseInTx(ctx, s.tx, use); err != nil {
					t.Fatal(err)
				}
				o.phase, o.joined = "terminal", true
				s.writeError = canary
			case "cancel-after-write":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				s.cancelWrite = cancel
			}
			if mode == "retire-write" {
				err = p.RetireExecutionMountUseInTx(ctx, s.tx, use, o.terminal)
			} else {
				err = p.AcquireExecutionMountUseInTx(ctx, s.tx, use)
			}
			if err == nil || strings.Contains(err.Error(), canary.Error()) {
				t.Fatal("storage failure lost or exposed diagnostics")
			}
			if mode == "cancel-after-write" && !errors.Is(err, context.Canceled) {
				t.Fatal("post-write cancellation was reported as success")
			}
			// The controlled store has no rollback engine. Only the real PG
			// integration may claim these uncommitted appends were rolled back.
		})
	}
}
