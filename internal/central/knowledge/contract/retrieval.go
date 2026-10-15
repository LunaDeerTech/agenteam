package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
)

type QueryRequest struct {
	Query   string
	Limit   int
	Filters rc.Filters
}

func (q QueryRequest) Validate() error {
	if len(q.Query) == 0 || len(q.Query) > 16<<10 || !utf8.ValidString(q.Query) || strings.TrimSpace(q.Query) == "" || strings.ContainsRune(q.Query, 0) || q.Limit < 1 || q.Limit > 100 || q.Filters.ValidateFor(rc.Knowledge) != nil {
		return invalid()
	}
	return nil
}

type QueryHit struct {
	DocumentID      DocumentID      `json:"document_id"`
	DocumentVersion f.Version       `json:"document_version"`
	Title           string          `json:"title"`
	MediaType       string          `json:"media_type"`
	Snippet         string          `json:"snippet"`
	Locator         json.RawMessage `json:"locator"`
}

type QueryResult struct {
	Hits      []QueryHit `json:"hits"`
	Degraded  bool       `json:"degraded"`
	Truncated bool       `json:"truncated"`
}

// InvocationReads never accepts the project, Agent or execution from model
// JSON. Query hits are locations; reads require the exact cited version.
type InvocationReads interface {
	QueryDocuments(context.Context, Invocation, QueryRequest) (QueryResult, error)
	ReadForInvocation(context.Context, Invocation, DocumentID, f.Version, ReadRequest) (DocumentContent, error)
}

func (QueryRequest) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "knowledge_query") }
func (QueryRequest) LogValue() slog.Value         { return slog.StringValue("knowledge_query") }
func (QueryRequest) MarshalJSON() ([]byte, error) { return []byte(`"knowledge_query"`), nil }
func (QueryResult) Format(w fmt.State, _ rune)    { _, _ = io.WriteString(w, "knowledge_query_result") }
func (QueryResult) LogValue() slog.Value          { return slog.StringValue("knowledge_query_result") }
