package filesystem

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
)

// MaxWriteBytes bounds this local whole-file operation, not a wire protocol.
const MaxWriteBytes = 1 << 20

// WriteRequest replaces a complete file. Parent directories must already exist.
// Data may be empty. The caller must not mutate Data until Write returns.
// Overwrite is explicit; false never replaces an existing directory entry.
type WriteRequest struct {
	Path      string
	Data      []byte
	Overwrite bool
}

// WriteState describes only atomic file publication, not a database transaction.
// An error or cancellation after publication cannot undo the write.
type WriteState string

const (
	WriteNotCommitted WriteState = "not_committed"
	WriteCommitted    WriteState = "committed"
	WriteUnknown      WriteState = "unknown"
)

// WriteResult must be inspected even when Write returns an error. Durable means
// both the complete temporary file and its containing directory were fsynced.
// BytesWritten is the complete replacement size only after known publication.
// No automatic retry is safe for an unknown result.
type WriteResult struct {
	State        WriteState
	BytesWritten int
	Durable      bool
}

func (WriteRequest) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "filesystem_write_request") }
func (WriteRequest) MarshalJSON() ([]byte, error) { return []byte(`"filesystem_write_request"`), nil }
func (WriteRequest) LogValue() slog.Value         { return slog.StringValue("filesystem_write_request") }
func (WriteResult) Format(w fmt.State, _ rune)    { _, _ = io.WriteString(w, "filesystem_write_result") }
func (WriteResult) MarshalJSON() ([]byte, error)  { return []byte(`"filesystem_write_result"`), nil }
func (WriteResult) LogValue() slog.Value          { return slog.StringValue("filesystem_write_result") }

type nativeWriter interface {
	write(context.Context, WriteRequest) (WriteResult, error)
}

func (r *Root) Write(ctx context.Context, request WriteRequest) (WriteResult, error) {
	result := WriteResult{State: WriteNotCommitted}
	if !validPath(request.Path, false) || len(request.Data) > MaxWriteBytes {
		return result, ErrInvalid
	}
	op, err := r.begin(ctx)
	if err != nil {
		return result, err
	}
	writer, ok := r.fs.(nativeWriter)
	if !ok {
		err = ErrUnsupported
	} else {
		request.Data = bytes.Clone(request.Data)
		defer clear(request.Data)
		result, err = writer.write(op.ctx, request)
	}
	// Keep actual publication/durability facts when Stop wins the return race.
	// Like Read, this retires only after native I/O and cleanup actually return.
	r.mu.Lock()
	if cancelled := contextError(op.ctx); cancelled != nil {
		err = errors.Join(err, cancelled)
	}
	delete(r.active, op)
	if r.stopped && len(r.active) == 0 {
		close(r.quiet)
	}
	r.mu.Unlock()
	op.cancel()
	return result, err
}
