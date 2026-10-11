package filesystem

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	MaxFindVisited = 4096
	MaxFindDepth   = 32
)

// FindRequest is local to an already-authorized Root, not a Runner wire DTO.
// Match is "name" (basename) or "path" (workspace-relative path). Pattern uses
// path.Match syntax without globstar. Find never follows discovered symlinks.
type FindRequest struct {
	Path    string
	Pattern string
	Match   string
	Limit   int
}

// FindResult contains only regular files. Paths are sorted after selection in
// directory traversal order; they are not a stable page or a filesystem snapshot.
// Truncated means an additional matching file was actually observed. Traversal
// budget exhaustion instead returns ErrLimit and no result.
type FindResult struct {
	Path      string
	Paths     []string
	Truncated bool
}

func (FindRequest) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "filesystem_find_request") }
func (FindRequest) MarshalJSON() ([]byte, error) { return []byte(`"filesystem_find_request"`), nil }
func (FindRequest) LogValue() slog.Value         { return slog.StringValue("filesystem_find_request") }
func (FindResult) Format(w fmt.State, _ rune)    { _, _ = io.WriteString(w, "filesystem_find_result") }
func (FindResult) MarshalJSON() ([]byte, error)  { return []byte(`"filesystem_find_result"`), nil }
func (FindResult) LogValue() slog.Value          { return slog.StringValue("filesystem_find_result") }

func (r FindRequest) valid() bool {
	if !validPath(r.Path, true) || r.Limit < 1 || r.Limit > MaxEntries ||
		len(r.Pattern) == 0 || len(r.Pattern) > 256 || !utf8.ValidString(r.Pattern) ||
		strings.ContainsRune(r.Pattern, 0) || strings.Contains(r.Pattern, "**") {
		return false
	}
	switch r.Match {
	case "name":
		if strings.Contains(r.Pattern, "/") {
			return false
		}
	case "path":
		if strings.HasPrefix(r.Pattern, "/") {
			return false
		}
	default:
		return false
	}
	_, err := path.Match(r.Pattern, "")
	return err == nil
}

func findBudget(result FindResult) error {
	return jsonBudget(struct {
		Path      string   `json:"path"`
		Paths     []string `json:"paths"`
		Truncated bool     `json:"truncated"`
	}{result.Path, result.Paths, result.Truncated})
}

// Find recursively searches for regular files beneath Path. One admission owns
// the full traversal and all actual target closes, sharing Read/List's Stop and
// Drain barrier. No partial result is published on cancellation or failure.
func (r *Root) Find(ctx context.Context, request FindRequest) (FindResult, error) {
	if !request.valid() {
		return FindResult{}, ErrInvalid
	}
	op, err := r.begin(ctx)
	if err != nil {
		return FindResult{}, err
	}
	result, err := r.fs.find(op.ctx, request)
	if err == nil {
		err = findBudget(result)
	}
	if err = r.finish(op, err); err != nil {
		return FindResult{}, err
	}
	return result, nil
}
