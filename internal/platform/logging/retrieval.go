package logging

import (
	"context"
	"log/slog"
	"time"
)

// RetrievalDiagnostic is a neutral projection, not a domain request. String
// fields are validated below; callers cannot attach raw query/error/content.
type RetrievalDiagnostic struct {
	RequestID, ProjectID, AgentID string
	Domain, QuerySHA256, Backend  string
	Mode, DegradationReason       string
	ErrorCode                     string
	QueryBytes                    int
	LexicalCandidates             int
	DenseCandidates               int
	FusionCandidates              int
	ResultCount                   int
	RerankUsed                    bool
	Lexical, Embedding, Dense     time.Duration
	Fusion, Total                 time.Duration
}

func (l *Logger) Retrieval(d RetrievalDiagnostic) {
	if l == nil || l.logger == nil {
		return
	}
	mode := retrievalChoice(d.Mode, "hybrid", "lexical_only", "dense_only", "hybrid_without_rerank")
	code := retrievalChoice(d.ErrorCode, "CANCELED", "DEADLINE_EXCEEDED", "INVALID_ARGUMENT", "INVALID_STATE", "FORBIDDEN", "UNAUTHENTICATED", "SESSION_REVOKED", "NOT_FOUND", "RESOURCE_DELETED", "VERSION_CONFLICT", "RESOURCE_BUSY", "PROJECT_NOT_ACTIVE", "DEPENDENCY_UNBOUND", "DEPENDENCY_UNAVAILABLE", "COMMIT_UNKNOWN", "INTERNAL_ERROR", "SHUTTING_DOWN", "OBJECT_PAYLOAD_MISSING", "OBJECT_INTEGRITY_MISMATCH")
	if d.ErrorCode != "" && code == "" {
		code = "INTERNAL_ERROR"
	}
	if code != "" {
		mode = ""
	}
	level := slog.LevelInfo
	if code != "" {
		level = slog.LevelError
	}
	l.logger.LogAttrs(context.Background(), level, "retrieval",
		slog.String("event", "retrieval"),
		slog.String("request_id", retrievalUUID(d.RequestID)),
		slog.String("domain_kind", retrievalChoice(d.Domain, "knowledge", "memory")),
		slog.String("project_id", retrievalUUID(d.ProjectID)),
		slog.String("agent_id", retrievalUUID(d.AgentID)),
		slog.String("query_sha256", retrievalDigest(d.QuerySHA256)),
		slog.Int("query_bytes", retrievalCount(d.QueryBytes, 16<<10)),
		slog.String("retrieval_mode", mode),
		slog.String("lexical_backend", retrievalChoice(d.Backend, "native_pg_fts", "pgroonga", "pg_search")),
		slog.Int("lexical_candidate_count", retrievalCount(d.LexicalCandidates, 4096)),
		slog.Int("dense_candidate_count", retrievalCount(d.DenseCandidates, 4096)),
		slog.Int("fusion_candidate_count", retrievalCount(d.FusionCandidates, 8192)),
		slog.Int("result_count", retrievalCount(d.ResultCount, 8192)),
		slog.Bool("rerank_used", d.RerankUsed),
		slog.String("degradation_reason", retrievalChoice(d.DegradationReason, "lexical_unavailable", "dense_unavailable", "reranker_unavailable")),
		slog.String("error_code", code),
		slog.Duration("lexical_latency", retrievalDuration(d.Lexical)),
		slog.Duration("embedding_latency", retrievalDuration(d.Embedding)),
		slog.Duration("dense_latency", retrievalDuration(d.Dense)),
		slog.Duration("fusion_latency", retrievalDuration(d.Fusion)),
		slog.Duration("total_latency", retrievalDuration(d.Total)),
	)
}

func retrievalChoice(value string, allowed ...string) string {
	for _, choice := range allowed {
		if value == choice {
			return value
		}
	}
	return ""
}

func retrievalUUID(value string) string {
	if len(value) != 36 {
		return ""
	}
	for n, c := range value {
		if n == 8 || n == 13 || n == 18 || n == 23 {
			if c != '-' {
				return ""
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return ""
		}
	}
	return value
}

func retrievalDigest(value string) string {
	if len(value) != 64 {
		return ""
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return ""
		}
	}
	return value
}

func retrievalCount(value, maximum int) int {
	if value < 0 || value > maximum {
		return 0
	}
	return value
}

func retrievalDuration(value time.Duration) time.Duration {
	if value < 0 || value > time.Hour {
		return 0
	}
	return value
}
