package runtime

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	"github.com/jackc/pgx/v5/pgconn"
)

// Controlled SQL rows exercise the reader's original tuple and phase gates.
// They do not stand in for actual authority, COMMIT, or physical owner Join.
type installObservedSQL struct {
	t                    *testing.T
	h                    *installHandoff
	metadata, terminal   []any
	metadataReads, reads int
}

func (s *installObservedSQL) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	b := s.h.binding
	if strings.HasPrefix(q, "SELECT operation_id,") {
		if !reflect.DeepEqual(args, []any{b.ExecutionID.String(), b.LogicalCallID.String(), b.InvocationID.String(), b.CallID}) {
			s.t.Fatal("metadata read lost original Model call tuple")
		}
		s.metadataReads++
		return controlRow{values: slices.Clone(s.metadata)}
	}
	if !strings.HasPrefix(q, "SELECT a.attempt_id,") || !reflect.DeepEqual(args, []any{s.h.record.ID.String(), b.ExecutionID.String(), b.ProjectID.String(), b.AgentID.String()}) {
		s.t.Fatal("terminal read lost original operation scope")
	}
	s.reads++
	return controlRow{values: slices.Clone(s.terminal)}
}
func (s *installObservedSQL) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	s.t.Fatal("observer queried unrelated rows")
	return nil, nil
}
func (s *installObservedSQL) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	s.t.Fatal("observer attempted a write")
	return pgconn.CommandTag{}, nil
}

func installObservedFixture(t *testing.T) *installObservedSQL {
	t.Helper()
	h, _ := installHandoffFixture(t)
	receipt := skill.InstallReceipt{InstallationID: newOperationID[skill.Installation](t), SkillID: h.record.SkillID, ProjectID: h.binding.ProjectID, RevisionID: newOperationID[sc.Revision](t), Revision: 1, Version: 1, ObjectID: newOperationID[oc.StoredObject](t), PackageSHA256: h.record.Input.PackageDigest}
	output, err := builtin.ProjectSkillInstallReceipt(context.Background(), h.call, receipt)
	if err != nil {
		t.Fatal(err)
	}
	rc := projectInstallReceipt(receipt)
	terminal := installTerminal{Format: 1, Operation: h.record.ID, Attempt: h.attempt, Status: "success", Known: true, Receipt: &rc, Output: output}
	if err = terminal.validate(h); err != nil {
		t.Fatal(err)
	}
	body, err := encode(terminal)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encode(h.record.Input)
	if err != nil {
		t.Fatal(err)
	}
	b := h.binding
	return &installObservedSQL{t: t, h: h,
		metadata: []any{h.record.ID.String(), b.ProjectID.String(), b.AgentID.String(), b.ExecutionID.String(), b.LogicalCallID.String(), b.InvocationID.String(), b.CallID, b.Spec.ToolID.String(), int64(b.Spec.SpecRevision), raw, h.record.InputDigest, h.record.SkillID.String(), h.record.Key, h.record.Fingerprint, "succeeded", f.Version(3)},
		terminal: []any{h.attempt.String(), h.request.String(), newOperationID[oc.Process](t).String(), int64(1), int64(1), "succeeded", true, body, body, "success", h.attempt.String()},
	}
}

func TestSkillInstallObservedTerminalPreservesPreparationGate(t *testing.T) {
	x := installObservedFixture(t)
	if _, err := loadOperation(context.Background(), x, x.h.binding); err == nil {
		t.Fatal("legacy preparation loader accepted terminal install")
	}
	observed, err := loadInstallObserved(context.Background(), x, x.h.binding)
	if err != nil || observed == nil || !observed.outcome().ResultAvailable || observed.outcome().Outcome != "success" || observed.binding(x.h.binding).Validate() != nil {
		t.Fatal("exact retired install terminal was not observed", err)
	}
	if x.metadataReads != 2 || x.reads != 1 {
		t.Fatal("observer repeated queries")
	}
}

func TestSkillInstallObservedRejectsChangedTerminalFacts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*installObservedSQL)
	}{
		{"terminal version", func(x *installObservedSQL) { x.metadata[15] = f.Version(4) }},
		{"unsupported phase", func(x *installObservedSQL) { x.metadata[14] = "cancelled" }},
		{"immutable digest", func(x *installObservedSQL) { x.metadata[10] = digest([]byte("different")) }},
		{"unretired attempt", func(x *installObservedSQL) { x.terminal[6] = false }},
		{"attempt fence", func(x *installObservedSQL) { x.terminal[3] = int64(2) }},
		{"active attempt", func(x *installObservedSQL) { x.terminal[10] = newOperationID[struct{}](x.t).String() }},
		{"terminal copies", func(x *installObservedSQL) { x.terminal[8] = []byte(`{}`) }},
		{"terminal outcome", func(x *installObservedSQL) { x.terminal[9] = "error" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := installObservedFixture(t)
			tc.change(x)
			if _, err := loadInstallObserved(context.Background(), x, x.h.binding); err == nil {
				t.Fatal("changed terminal facts accepted")
			}
		})
	}
	t.Run("original binding", func(t *testing.T) {
		x := installObservedFixture(t)
		changed := x.h.binding
		changed.SnapshotID = newOperationID[struct{}](t).String()
		if _, err := loadInstallObserved(context.Background(), x, changed); err == nil {
			t.Fatal("different snapshot accepted")
		}
	})
}
