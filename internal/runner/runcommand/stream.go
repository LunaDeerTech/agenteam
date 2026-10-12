package runcommand

import (
	"context"
	"errors"
	"sync"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/runner/command"
)

// ErrOutputCancelled is a narrow Control admission result, not an I/O error.
// The bound command executor may return it only when this cancelled frame was
// not enqueued. A transport, protocol or backpressure error must never use it.
var ErrOutputCancelled = errors.New("command output was not admitted after cancellation")

type outputStream struct {
	carry  []byte
	seen   int
	queued int64
}

type commandOutput struct {
	mu                                sync.Mutex
	emit                              func(string, string) error
	streams                           [2]outputStream
	dropped, invalid, transportFailed bool
}

func (o *commandOutput) sink(ctx context.Context, stream command.Stream, raw []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	index := 0
	switch stream {
	case command.Stdout:
	case command.Stderr:
		index = 1
	default:
		o.invalid = true
		return command.ErrIO
	}
	s := &o.streams[index]
	if ctx == nil || len(raw) > command.MaxChunkBytes || len(raw) > command.MaxStreamBytes-s.seen {
		o.invalid = true
		return command.ErrIO
	}
	s.seen += len(raw)
	if o.transportFailed || o.invalid {
		return command.ErrIO
	}
	if ctx.Err() != nil {
		o.discard(s, len(raw) != 0)
		return nil
	}
	joined := make([]byte, 0, len(s.carry)+len(raw))
	joined = append(joined, s.carry...)
	joined = append(joined, raw...)
	clear(s.carry)
	s.carry = nil
	defer clear(joined)
	end := 0
	for end < len(joined) {
		if !utf8.FullRune(joined[end:]) {
			break
		}
		r, n := utf8.DecodeRune(joined[end:])
		if r == utf8.RuneError && n == 1 {
			o.invalid = true
			return command.ErrIO
		}
		end += n
	}
	// FullRune distinguishes incomplete text from invalid text; valid carry is
	// at most three bytes, independent for stdout and stderr.
	if len(joined)-end > 3 {
		o.invalid = true
		return command.ErrIO
	}
	if end != 0 {
		err := o.emit(string(stream), string(joined[:end]))
		if err == ErrOutputCancelled {
			// The original locked admission established non-enqueue. Consume
			// this local tail without fabricating an I/O failure or a send.
			o.dropped = true
			return nil
		}
		if err != nil {
			o.transportFailed = true
			return command.ErrIO
		}
		s.queued += int64(end)
	}
	s.carry = append(s.carry, joined[end:]...)
	return nil
}

func (o *commandOutput) discard(s *outputStream, hasBytes bool) {
	if hasBytes || len(s.carry) != 0 {
		o.dropped = true
	}
	clear(s.carry)
	s.carry = nil
}

// finish runs only after the original RunCommand (including its pipe readers)
// returned. Truncation is a declared byte limit, not an invalid UTF-8 claim.
func (o *commandOutput) finish(ctx context.Context, result command.Result) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := range o.streams {
		s := &o.streams[i]
		if len(s.carry) == 0 {
			continue
		}
		truncated := result.StdoutTruncated
		if i == 1 {
			truncated = result.StderrTruncated
		}
		switch {
		case ctx.Err() != nil:
			o.dropped = true
		case !truncated:
			o.invalid = true
		}
		clear(s.carry)
		s.carry = nil
	}
}

func (o *commandOutput) clear() {
	for i := range o.streams {
		clear(o.streams[i].carry)
		o.streams[i].carry = nil
	}
}
