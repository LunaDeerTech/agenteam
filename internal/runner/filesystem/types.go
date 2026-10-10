// Package filesystem reads a trusted, already-open workspace directory. It does
// not resolve Mount identities, authorize callers or register Runner operations.
package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	MaxReadBytes   = 64 << 10
	MaxEntries     = 128
	MaxResultBytes = 512 << 10
)

var (
	ErrInvalid     = errors.New("filesystem: invalid request")
	ErrType        = errors.New("filesystem: invalid type")
	ErrNotFound    = errors.New("filesystem: not found")
	ErrOutside     = errors.New("filesystem: outside root")
	ErrConflict    = errors.New("filesystem: conflict")
	ErrUnsupported = errors.New("filesystem: unsupported")
	ErrUnavailable = errors.New("filesystem: unavailable")
	ErrLimit       = errors.New("filesystem: result limit")
	ErrClosed      = errors.New("filesystem: closed")
)

// These are local values, not Runner wire DTOs. Default formatting, JSON and
// logging redact them; a future authorized adapter must explicitly project data.
type ReadRequest struct {
	Path     string
	Offset   int64
	Length   int
	Encoding string
}

type ReadResult struct {
	Path      string
	Offset    int64
	BytesRead int
	EOF       bool
	Encoding  string
	Data      string
}

type ListRequest struct {
	Path  string
	Limit int
}

type Entry struct {
	Name string
	Kind string
}

type ListResult struct {
	Path      string
	Entries   []Entry
	Truncated bool
}

func (ReadRequest) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "filesystem_read_request") }
func (ReadRequest) MarshalJSON() ([]byte, error) { return []byte(`"filesystem_read_request"`), nil }
func (ReadRequest) LogValue() slog.Value         { return slog.StringValue("filesystem_read_request") }
func (ReadResult) Format(w fmt.State, _ rune)    { _, _ = io.WriteString(w, "filesystem_read_result") }
func (ReadResult) MarshalJSON() ([]byte, error)  { return []byte(`"filesystem_read_result"`), nil }
func (ReadResult) LogValue() slog.Value          { return slog.StringValue("filesystem_read_result") }
func (ListRequest) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "filesystem_list_request") }
func (ListRequest) MarshalJSON() ([]byte, error) { return []byte(`"filesystem_list_request"`), nil }
func (ListRequest) LogValue() slog.Value         { return slog.StringValue("filesystem_list_request") }
func (Entry) Format(w fmt.State, _ rune)         { _, _ = io.WriteString(w, "filesystem_entry") }
func (Entry) MarshalJSON() ([]byte, error)       { return []byte(`"filesystem_entry"`), nil }
func (Entry) LogValue() slog.Value               { return slog.StringValue("filesystem_entry") }
func (ListResult) Format(w fmt.State, _ rune)    { _, _ = io.WriteString(w, "filesystem_list_result") }
func (ListResult) MarshalJSON() ([]byte, error)  { return []byte(`"filesystem_list_result"`), nil }
func (ListResult) LogValue() slog.Value          { return slog.StringValue("filesystem_list_result") }

func validComponent(value string) bool {
	return value != "" && value != "." && value != ".." && len(value) <= 255 &&
		utf8.ValidString(value) && !strings.ContainsAny(value, "/\\\x00")
}

func validPath(value string, rootAllowed bool) bool {
	if rootAllowed && value == "." {
		return true
	}
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if !validComponent(part) {
			return false
		}
	}
	return true
}

func (r ReadRequest) valid() bool {
	return validPath(r.Path, false) && r.Length > 0 && r.Length <= MaxReadBytes &&
		r.Offset >= 0 && r.Offset <= math.MaxInt64-int64(r.Length)-1 &&
		(r.Encoding == "utf8" || r.Encoding == "base64")
}

func (r ListRequest) valid() bool {
	return validPath(r.Path, true) && r.Limit > 0 && r.Limit <= MaxEntries
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		return context.Canceled
	}
	return nil
}

// Only these private projections encode actual data for the local size budget.
// The default MarshalJSON methods above remain safe even inside nested logs.
func readBudget(result ReadResult) error {
	return jsonBudget(struct {
		Path      string `json:"path"`
		Offset    int64  `json:"offset"`
		BytesRead int    `json:"bytes_read"`
		EOF       bool   `json:"eof"`
		Encoding  string `json:"encoding"`
		Data      string `json:"data"`
	}{result.Path, result.Offset, result.BytesRead, result.EOF, result.Encoding, result.Data})
}

func listBudget(result ListResult) error {
	type entryWire struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	entries := make([]entryWire, len(result.Entries))
	for i, entry := range result.Entries {
		entries[i] = entryWire{entry.Name, entry.Kind}
	}
	return jsonBudget(struct {
		Path      string      `json:"path"`
		Entries   []entryWire `json:"entries"`
		Truncated bool        `json:"truncated"`
	}{result.Path, entries, result.Truncated})
}

func jsonBudget(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ErrUnavailable
	}
	if len(encoded) > MaxResultBytes {
		return ErrLimit
	}
	return nil
}
