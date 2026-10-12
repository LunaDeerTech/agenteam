package runcommand

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/runner/command"
)

func TestRunCommandStreamsPreserveUTF8AndIndependentCarries(t *testing.T) {
	var stdout, stderr strings.Builder
	o := &commandOutput{emit: func(stream, data string) error {
		if !utf8.ValidString(data) {
			t.Fatal("partial rune emitted")
		}
		if stream == "stdout" {
			stdout.WriteString(data)
		} else {
			stderr.WriteString(data)
		}
		return nil
	}}
	defer o.clear()
	ctx := context.Background()
	for _, step := range []struct {
		stream command.Stream
		raw    []byte
	}{
		{command.Stdout, []byte{0xe4}}, {command.Stderr, []byte{0xf0, 0x9f}},
		{command.Stdout, []byte{0xb8, 0xad}}, {command.Stderr, []byte{0x8c, 0x8d}},
		{command.Stdout, []byte("�\x00\n")},
	} {
		if err := o.sink(ctx, step.stream, step.raw); err != nil {
			t.Fatal(err)
		}
	}
	o.finish(ctx, command.Result{OutputComplete: true})
	if stdout.String() != "中�\x00\n" || stderr.String() != "🌍" || o.streams[0].queued != 8 || o.streams[1].queued != 4 || o.invalid || o.dropped {
		t.Fatal("text, independent carries or queued bytes changed")
	}

	limited := &commandOutput{emit: func(string, string) error { return nil }}
	defer limited.clear()
	for range 7 {
		if limited.sink(ctx, command.Stdout, []byte(strings.Repeat("a", command.MaxChunkBytes))) != nil {
			t.Fatal("bounded chunk rejected")
		}
	}
	last := append([]byte(strings.Repeat("a", command.MaxChunkBytes-1)), 0xe4)
	if limited.sink(ctx, command.Stdout, last) != nil {
		t.Fatal("last bounded chunk rejected")
	}
	limited.finish(ctx, command.Result{OutputComplete: true, StdoutTruncated: true})
	if limited.invalid || limited.dropped || limited.streams[0].queued != command.MaxStreamBytes-1 || len(limited.streams[0].carry) != 0 {
		t.Fatal("declared truncation emitted or misclassified a half rune")
	}
}

func TestRunCommandStreamsRejectDamagedOutput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		raw   []byte
		atEOF bool
	}{
		{"invalid-byte", []byte{0xff}, false},
		{"invalid-continuation", []byte{'a', 0xe4, 0xff}, false},
		{"overlong", []byte{0xc0, 0x80}, false},
		{"incomplete-eof", []byte{0xe4, 0xb8}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			o := &commandOutput{emit: func(string, string) error { calls++; return nil }}
			defer o.clear()
			err := o.sink(context.Background(), command.Stdout, tc.raw)
			if tc.atEOF {
				if err != nil {
					t.Fatal("incomplete carry rejected early")
				}
				o.finish(context.Background(), command.Result{OutputComplete: true})
			} else if !errors.Is(err, command.ErrIO) {
				t.Fatal("invalid bytes not rejected")
			}
			if !o.invalid || calls != 0 || o.streams[0].queued != 0 {
				t.Fatal("invalid bytes were replaced, encoded or counted")
			}
		})
	}
}

func TestRunCommandStreamsKeepCancellationAdmissionDistinct(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	admitted := 0
	o := &commandOutput{emit: func(string, string) error { admitted++; cancel(); return ErrOutputCancelled }}
	defer o.clear()
	if err := o.sink(ctx, command.Stdout, []byte("tail")); err != nil {
		t.Fatal("locked cancellation sentinel became I/O failure")
	}
	if err := o.sink(ctx, command.Stderr, []byte("late")); err != nil {
		t.Fatal("cancelled local tail did not drain")
	}
	o.finish(ctx, command.Result{OutputComplete: true})
	if admitted != 1 || !o.dropped || o.invalid || o.transportFailed || o.streams[0].queued != 0 || o.streams[1].queued != 0 {
		t.Fatal("discarded frame was counted or wire error fabricated")
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	failed := &commandOutput{emit: func(string, string) error { cancel2(); return errors.New("actual send failed") }}
	defer failed.clear()
	if err := failed.sink(ctx2, command.Stdout, []byte("text")); !errors.Is(err, command.ErrIO) || !failed.transportFailed || failed.dropped {
		t.Fatal("real send error was swallowed after cancellation")
	}

	pending := &commandOutput{emit: func(string, string) error { return nil }}
	defer pending.clear()
	if pending.sink(context.Background(), command.Stdout, []byte{0xe4}) != nil {
		t.Fatal("carry rejected")
	}
	pending.finish(ctx, command.Result{OutputComplete: true})
	if !pending.dropped || pending.invalid || len(pending.streams[0].carry) != 0 {
		t.Fatal("cancelled carry was not cleared as incomplete output")
	}
}
