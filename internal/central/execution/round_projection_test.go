package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// These controls exercise the real stream decoder, loan cleanup and original
// Store lifetime. They do not claim a complete persisted RoundInput or real PG;
// the integration chain must obtain that input through the original 68 writer.
func roundProjectionRowsControl(t *testing.T) (*occupancyRowsControl, c.RoundInputFields) {
	t.Helper()
	x := newReadFileAuthorityControl(t)
	_, consumed := toolResultCandidateControl(t)
	round := x.stored.round.Fields().ID
	var result mc.Message
	if err := json.Unmarshal(consumed.message, &result); err != nil {
		t.Fatal(err)
	}
	result.Parts[0].ToolResult.CallID = x.stored.toolPending.call.ID
	messages := []mc.Message{x.stored.round.Fields().Messages[1], x.stored.toolPending.response.Message, result}
	rows := &occupancyRowsControl{}
	var rawMessages []json.RawMessage
	for n, m := range messages {
		raw, err := directTextJSON(m)
		if err != nil {
			t.Fatal(err)
		}
		rows.values = append(rows.values, []any{int64(n + 1), round.String(), []string{"input", "assistant", "tool_result"}[n], raw, string(c.TriggerInputDigest(raw))})
		rawMessages = append(rawMessages, raw)
	}
	prefix, err := json.Marshal(rawMessages)
	if err != nil {
		t.Fatal(err)
	}
	return rows, c.RoundInputFields{PredecessorRoundID: round, TranscriptPrefixDigest: c.TriggerInputDigest(prefix), Source: c.ToolResultConsumptionReceipt{MessageDigest: c.TriggerInputDigest(rawMessages[2])}}
}

func TestExecutionRoundProjectionEntriesRequireCompleteCanonicalPrefix(t *testing.T) {
	rows, fields := roundProjectionRowsControl(t)
	entries, err := scanRoundProjectionEntries(context.Background(), rows, fields)
	if err != nil || len(entries) != 3 || !rows.closed {
		t.Fatal("complete original prefix", err)
	}
	for n, entry := range entries {
		if entry.Sequence != f.Sequence(n+1) || !bytes.Equal(entry.CanonicalMessage, rows.values[n][3].([]byte)) {
			t.Fatal("original bytes changed")
		}
	}
	entries[0].CanonicalMessage[0] = '!'
	if rows.values[0][3].([]byte)[0] == '!' {
		t.Fatal("SQL data alias escaped")
	}
	for name, alter := range map[string]func(*occupancyRowsControl, *c.RoundInputFields){
		"missing-final":      func(r *occupancyRowsControl, _ *c.RoundInputFields) { r.values = r.values[:2] },
		"foreign-round":      func(r *occupancyRowsControl, _ *c.RoundInputFields) { r.values[2][1] = newTestID[c.Round](t).String() },
		"duplicate-sequence": func(r *occupancyRowsControl, _ *c.RoundInputFields) { r.values[2][0] = int64(2) },
		"changed-kind":       func(r *occupancyRowsControl, _ *c.RoundInputFields) { r.values[2][2] = "input" },
		"changed-content":    func(r *occupancyRowsControl, _ *c.RoundInputFields) { r.values[2][3] = []byte(`{}`) },
		"noncanonical": func(r *occupancyRowsControl, _ *c.RoundInputFields) {
			raw := append([]byte(" "), r.values[2][3].([]byte)...)
			r.values[2][3], r.values[2][4] = raw, string(c.TriggerInputDigest(raw))
		},
		"changed-prefix": func(_ *occupancyRowsControl, f *c.RoundInputFields) {
			f.TranscriptPrefixDigest = c.TriggerInputDigest([]byte("other"))
		},
		"changed-receipt": func(_ *occupancyRowsControl, f *c.RoundInputFields) {
			f.Source.MessageDigest = c.TriggerInputDigest([]byte("other"))
		},
		"late-row-error": func(r *occupancyRowsControl, _ *c.RoundInputFields) { r.endErr = errors.New("controlled rows failed") },
		"close-error": func(r *occupancyRowsControl, _ *c.RoundInputFields) {
			r.closeErr = errors.New("controlled close failed")
		},
	} {
		t.Run(name, func(t *testing.T) {
			r, f := roundProjectionRowsControl(t)
			alter(r, &f)
			got, err := scanRoundProjectionEntries(context.Background(), r, f)
			if err == nil || got != nil || !r.closed {
				t.Fatal("incomplete or changed prefix published", err)
			}
		})
	}
}

func TestExecutionRoundProjectionLoanEndsWithOriginalCallback(t *testing.T) {
	for _, mode := range []string{"success", "callback-error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			arguments, message, control := []byte("original arguments"), []byte("original message"), []byte("original control")
			source := c.RoundProjectionSource{Arguments: arguments, ControlCanonical: control, Transcript: []c.RoundProjectionEntry{{CanonicalMessage: message}}}
			var retained c.RoundProjectionSource
			failure := errors.New("controlled callback failed")
			err := lendRoundProjection(ctx, source, func(v c.RoundProjectionSource) error {
				retained = v
				if !bytes.Equal(v.Arguments, arguments) || !bytes.Equal(v.Transcript[0].CanonicalMessage, message) {
					t.Fatal("original loan changed")
				}
				// Changing a descriptor cannot evade cleanup of the original loan.
				v.Transcript[0].CanonicalMessage = nil
				switch mode {
				case "callback-error":
					return failure
				case "cancel":
					cancel()
				}
				return nil
			})
			if mode == "success" && err != nil || mode == "callback-error" && err != failure || mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("callback outcome changed", err)
			}
			for _, raw := range [][]byte{arguments, message, control, retained.Arguments, retained.ControlCanonical} {
				if !bytes.Equal(raw, make([]byte, len(raw))) {
					t.Fatal("loan remained available after callback")
				}
			}
			if retained.Transcript[0].CanonicalMessage != nil {
				t.Fatal("entry loan retained")
			}
		})
	}
}

func TestExecutionRoundProjectionStopWaitsForActualStoreReturn(t *testing.T) {
	s := &roundBlockedStore{launchStore: &launchStore{t: t, rows: map[string][]any{}}, entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	d := &RoundInputDeterminer{store: s, active: make(map[i.ExecutionID]context.CancelFunc), changed: make(chan struct{})}
	e, next := newTestID[i.Execution](t), newTestID[c.Round](t)
	t.Cleanup(func() {
		d.Stop()
		select {
		case <-s.release:
		default:
			close(s.release)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := d.Drain(ctx); err != nil {
			t.Error("original Store did not return", err)
		}
	})
	done := make(chan error, 1)
	go func() {
		done <- d.WithCommittedRoundProjection(context.Background(), e, next, func(c.RoundProjectionSource) error { return errors.New("unexpected callback") })
	}()
	<-s.entered
	if err := d.WithCommittedRoundProjection(context.Background(), e, next, func(c.RoundProjectionSource) error { return nil }); err == nil {
		t.Fatal("same Execution read reopened")
	}
	d.Stop()
	<-s.cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Drain(ctx); !errors.Is(err, context.Canceled) || d.Joined() {
		t.Fatal("cancellation falsely joined actual Store", err)
	}
	close(s.release)
	if err := <-done; err == nil {
		t.Fatal("cancelled original read succeeded")
	}
	if err := d.Drain(context.Background()); err != nil || !d.Joined() {
		t.Fatal("returned original read remained active", err)
	}
	if err := d.WithCommittedRoundProjection(context.Background(), e, next, func(c.RoundProjectionSource) error { return nil }); err == nil || s.calls != 1 {
		t.Fatal("stopped reader reopened Store", err)
	}
}

type roundProjectionReturnStore struct {
	*launchStore
	entered, release chan struct{}
	unknown          bool
}

func (s *roundProjectionReturnStore) WithinTx(ctx context.Context, cause f.TransactionCause, consume func(context.Context, f.Tx) error) f.CommitResult {
	result := s.launchStore.WithinTx(ctx, cause, consume)
	close(s.entered)
	<-s.release
	if s.unknown {
		return f.UnknownResult(newTestID[f.TransactionAttempt](s.t), cause)
	}
	return result
}

func TestExecutionRoundProjectionCallbackCannotSubstituteTransactionReturn(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		name := "committed"
		if unknown {
			name = "unknown"
		}
		t.Run(name, func(t *testing.T) {
			s := &roundProjectionReturnStore{launchStore: &launchStore{t: t, rows: map[string][]any{}}, entered: make(chan struct{}), release: make(chan struct{}), unknown: unknown}
			d := &RoundInputDeterminer{store: s}
			e, next := newTestID[i.Execution](t), newTestID[c.Round](t)
			done := make(chan error, 1)
			calls := 0
			go func() {
				done <- d.roundReadTx(context.Background(), "projection", e, next, []f.LockRequest{executionLock(e)}, func(context.Context, f.Tx) error { calls++; return nil })
			}()
			<-s.entered
			if calls != 1 {
				t.Fatal("original callback not called")
			}
			select {
			case <-done:
				t.Fatal("callback published before actual Store return")
			default:
			}
			close(s.release)
			err := <-done
			if unknown && err == nil || !unknown && err != nil {
				t.Fatal("original commit result changed", err)
			}
			if unknown {
				if _, ok := UnknownAttempt(err); !ok {
					t.Fatal("original Unknown identity lost")
				}
			}
		})
	}
}
