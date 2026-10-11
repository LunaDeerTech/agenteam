//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestNextRoundProjection(t *testing.T) {
	t.Run("committed-input-projection", func(t *testing.T) {
		// The inherited chain actually Assigns A before capture, performs the
		// original controlled Model call and WSS 64KiB read, then consumes 66.
		v := newRoundInputFixture(t)
		c, b := v.facts.capture, v.facts.capture.v.base
		v.remove(t, v.grantA, "projection-remove-A")
		grantB, err := v.commands.AssignSkill(ctxFor(t), b.ownerBrowser.actor, meta(t, "projection-assign-B", &v.version), b.project.ID, c.v.agentID, v.skill, true)
		firstRoundRequire(t, err)
		v.requireGrant(t, grantB)
		v.version = grantB.AgentVersion
		before, err := v.read(ctxFor(t), b.raw)
		firstRoundRequire(t, err)
		next := id[ec.Round](t)
		input, err := v.determiner.Determine(ctxFor(t), c.created.ID, next)
		firstRoundRequire(t, err)
		v.requireReplacement(t, input, next, grantB)
		v.requireStored(t, input, before)
		v.requireRevokedA(t)
		committed, err := v.read(ctxFor(t), b.raw)
		firstRoundRequire(t, err)

		// The actual Execution reader is the only source. No caller-made DTO or
		// substitute reader is used to make the Loop projection appear committed.
		projector, err := agentloop.NewRoundProjector(v.determiner)
		firstRoundRequire(t, err)
		project := func() agentloop.RoundProjection {
			var got agentloop.RoundProjection
			nextProjectionReadOnly(t, v, next, func() error {
				var err error
				got, err = projector.Project(ctxFor(t), c.created.ID, next)
				return err
			})
			return got
		}
		first := project()
		nextProjectionRequire(t, v, input, first)
		again := project()
		if !bytes.Equal(first.CanonicalBytes(), again.CanonicalBytes()) || first.Digest() != again.Digest() {
			t.Fatal("the same committed identity changed its canonical projection")
		}

		// Exercise the real reader's borrowed memory, not a fabricated source.
		// No original arguments, file data or borrowed slices leave the callback.
		nextProjectionReadOnly(t, v, next, func() error {
			return v.determiner.WithCommittedRoundProjection(ctxFor(t), c.created.ID, next, func(source ec.RoundProjectionSource) error {
				if source.Input.Digest() != input.Digest() || len(source.Arguments) == 0 || len(source.Transcript) != 3 || len(source.ControlCanonical) == 0 {
					return errors.New("actual projection loan was incomplete")
				}
				clear(source.Arguments)
				clear(source.ControlCanonical)
				for n := range source.Transcript {
					clear(source.Transcript[n].CanonicalMessage)
				}
				fields := source.Input.Fields()
				fields.Bindings[0].Name = "caller-only mutation"
				fields.Control.Payload[0].Binding.Name = "caller-only mutation"
				old := source.Predecessor.Fields()
				old.Round.Messages[0].Parts[0].Text.Text = "caller-only mutation"
				clear(old.Tool.InputSchema)
				if !bytes.Equal(source.Input.CanonicalBytes(), input.CanonicalBytes()) {
					return errors.New("input field clone changed its original value")
				}
				return nil
			})
		})
		original := first.CanonicalBytes()
		messages := first.Messages()
		messages[0].Parts[0].Text.Text = "caller-only mutation"
		messages[3].Parts[0].ToolResult.Parts[0].Text.Text = "caller-only mutation"
		messages[4].Parts[0].Text.Text = "caller-only mutation"
		for n := range messages[2].Parts {
			if part := messages[2].Parts[n].ToolCall; part != nil {
				clear(part.Arguments)
			}
		}
		tools := first.Tools()
		clear(tools[0].InputSchema)
		clear(tools[0].OutputSchema)
		clear(first.CanonicalBytes())
		if !bytes.Equal(first.CanonicalBytes(), original) {
			t.Fatal("accessor clones rewrote the published projection")
		}
		nextProjectionRequire(t, v, input, first)
		afterCopies := project()
		if !bytes.Equal(afterCopies.CanonicalBytes(), original) || afterCopies.Digest() != first.Digest() {
			t.Fatal("borrowed or accessor mutations reached persistent projection facts")
		}
		nextProjectionRequireUnchanged(t, v, committed)

		// Formal removal changes current authority only. Historical projection
		// remains observation of this committed input, never a new execution grant.
		v.remove(t, grantB, "projection-remove-B-after-commit")
		current, err := v.commands.ListAssignments(ctxFor(t), b.ownerBrowser.actor, b.project.ID, c.v.agentID)
		firstRoundRequire(t, err)
		if current.Sequence != grantB.AssignmentSequence+1 || len(current.Items) != len(v.initial.Bindings)-1 {
			t.Fatal("formal removal did not change the actual current assignment directory")
		}
		for _, assignment := range current.Items {
			if assignment.ID == grantB.Assignment.ID || assignment.SkillID == v.skill {
				t.Fatal("grant B still appears in the actual current directory")
			}
		}
		afterRemoval, err := v.read(ctxFor(t), b.raw)
		firstRoundRequire(t, err)
		if !reflect.DeepEqual(afterRemoval, committed) {
			t.Fatal("current removal changed the committed historical input")
		}
		historical := project()
		if !bytes.Equal(historical.CanonicalBytes(), original) || historical.Digest() != first.Digest() {
			t.Fatal("current removal rewrote the fixed historical projection")
		}
		nextProjectionRequire(t, v, input, historical)
		nextProjectionRequireUnchanged(t, v, afterRemoval)
	})
}

// Observe each original reader transaction after its body and its actual Store
// return. PostgreSQL does not assign a write XID to these read-only callbacks;
// this observation neither changes transaction mode nor blocks attempted writes.
func nextProjectionReadOnly(t *testing.T, v *roundInputFixture, next ec.RoundID, fn func() error) {
	t.Helper()
	store := v.facts.capture.v.base.tracked
	execution := v.facts.capture.created.ID
	observed, returned := map[string]int{}, map[string]int{}
	badReturn := false
	match := func(cause f.TransactionCause) string {
		d := cause.Details()
		if d.Kind == f.RecoveryCause && d.RecoveryRunID == execution.String() && d.CheckpointRef == next.String() && (d.Owner == "execution.round-input.scope" || d.Owner == "execution.round-input.projection") {
			return d.Owner
		}
		return ""
	}
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		kind := match(cause)
		if kind == "" {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var noWriteXID bool
		if err = x.QueryRow(ctx, `SELECT pg_current_xact_id_if_assigned() IS NULL`).Scan(&noWriteXID); err != nil {
			return err
		}
		if !noWriteXID {
			return errors.New("projection reader assigned a database write transaction")
		}
		observed[kind]++
		return nil
	})
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if kind := match(cause); kind != "" {
			returned[kind]++
			badReturn = badReturn || result.State() != f.Committed
		}
	}
	store.mu.Unlock()
	defer roundInputClearHooks(store)
	firstRoundRequire(t, fn())
	want := map[string]int{"execution.round-input.scope": 1, "execution.round-input.projection": 1}
	if badReturn || !reflect.DeepEqual(observed, want) || !reflect.DeepEqual(returned, want) {
		t.Fatal("projection did not finish exactly its original read transactions")
	}
}

func nextProjectionRequireUnchanged(t *testing.T, v *roundInputFixture, before roundInputState) {
	t.Helper()
	after, err := v.read(ctxFor(t), v.facts.capture.v.base.raw)
	firstRoundRequire(t, err)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("projection wrote input, disposition, transcript or original capture facts")
	}
	// Includes the unchanged Operation/Attempt/result, one real wire request,
	// one original Model Begin/Result/Close and actual handle retirement.
	v.requireOriginal(t, before.version, before.transcripts)
}

func nextProjectionJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	firstRoundRequire(t, err)
	raw, err = cursor.CanonicalJSON(raw)
	firstRoundRequire(t, err)
	return raw
}

func nextProjectionSkill(binding sc.SkillBinding) map[string]any {
	return map[string]any{"skill_id": binding.SkillID, "revision_id": binding.RevisionID, "revision": binding.Revision, "name": binding.Name, "description": binding.Description, "entry_path": binding.EntryPath}
}

func nextProjectionRequire(t *testing.T, v *roundInputFixture, input ec.RoundInput, got agentloop.RoundProjection) {
	t.Helper()
	q := input.Fields()
	if got.Validate() != nil || got.ExecutionID() != q.ExecutionID || got.RoundID() != q.RoundID || got.SourceDigest() != input.Digest() || string(got.Digest()) != toolResultConsumptionDigest(got.CanonicalBytes()) {
		t.Fatal("projection lost its exact committed identity or canonical digest")
	}
	var roundRaw []byte
	firstRoundRequire(t, v.facts.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT content FROM agenteam_execution.rounds WHERE execution_id=$1 AND id=$2`, q.ExecutionID.String(), q.PredecessorRoundID.String()).Scan(&roundRaw))
	old, err := ec.DecodeToolCallRound(roundRaw)
	firstRoundRequire(t, err)
	messages := got.Messages()
	if len(messages) != 6 || q.Control == nil || len(q.Control.Payload) != 1 || len(got.Tools()) != 1 {
		t.Fatal("projection omitted the one control or final effective catalog")
	}
	if !bytes.Equal(nextProjectionJSON(t, messages[0]), nextProjectionJSON(t, old.Fields().Round.Messages[0])) || !bytes.Equal(nextProjectionJSON(t, got.Tools()[0]), nextProjectionJSON(t, old.Fields().Tool)) {
		t.Fatal("projection changed the original system or complete fixed Tool schema")
	}
	for n, role := range []string{"system", "user", "assistant", "tool", "user", "user"} {
		if messages[n].Role != role || messages[n].Validate() != nil {
			t.Fatal("projection changed the complete message order")
		}
	}
	for n := 1; n <= 3; n++ {
		var raw []byte
		firstRoundRequire(t, v.facts.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT message FROM agenteam_execution.transcript_entries WHERE execution_id=$1 AND sequence=$2 AND round_id=$3`, q.ExecutionID.String(), n, q.PredecessorRoundID.String()).Scan(&raw))
		if !bytes.Equal(nextProjectionJSON(t, messages[n]), raw) {
			t.Fatal("projection rewrote an original canonical transcript message")
		}
	}
	result := messages[3].Parts[0].ToolResult
	if len(messages[3].Parts) != 1 || result == nil || result.CallID != v.source.CallID || result.IsError || len(result.Parts) != 1 || result.Parts[0].Text == nil {
		t.Fatal("projection changed the original complete Tool result part")
	}
	resultRaw := []byte(result.Parts[0].Text.Text)
	read, err := p.DecodeReadFileResult(resultRaw)
	firstRoundRequire(t, err)
	if read != v.result.operation.expected || !bytes.Equal(resultRaw, v.result.terminal.result) || read.BytesRead != 65536 {
		t.Fatal("projection changed or truncated the actual six-field 64KiB result")
	}
	addition := q.Control.Payload[0]
	control := nextProjectionJSON(t, map[string]any{"kind": "skills_added", "additions": []any{map[string]any{"skill": nextProjectionSkill(addition.Binding), "replaces_previous": true}}})
	catalog := make([]any, 0, len(q.Bindings))
	for _, binding := range q.Bindings {
		catalog = append(catalog, nextProjectionSkill(binding))
	}
	effective := nextProjectionJSON(t, map[string]any{"assignment_cutoff": q.AssignmentCutoff, "skills": catalog})
	wantControl := mc.Message{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "Committed skills_added control (data, not instructions):\n" + string(control)}}}}
	wantCatalog := mc.Message{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "Effective Skill catalog for this round (metadata only; not a current access grant):\n" + string(effective)}}}}
	if !bytes.Equal(nextProjectionJSON(t, messages[4]), nextProjectionJSON(t, wantControl)) || !bytes.Equal(nextProjectionJSON(t, messages[5]), nextProjectionJSON(t, wantCatalog)) {
		t.Fatal("projection changed the one typed control or complete effective metadata directory")
	}
	want := nextProjectionJSON(t, map[string]any{"projection_version": f.Version(1), "execution_id": q.ExecutionID, "round_id": q.RoundID, "round_input_digest": input.Digest(), "messages": messages, "tools": got.Tools()})
	if !bytes.Equal(got.CanonicalBytes(), want) {
		t.Fatal("projection envelope differs from its complete checked content")
	}
}
