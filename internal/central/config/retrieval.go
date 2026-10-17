package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/lexical"
)

const retrievalRuntimeSetting = "RETRIEVAL_RUNTIME"

// RetrievalRuntimeOptions selects the deployment retrieval backend. Embedding
// dimensions belong to the selected Model and are frozen on each serving space.
type RetrievalRuntimeOptions struct {
	backend lexical.Backend
}

func (c Config) RetrievalRuntime() (RetrievalRuntimeOptions, bool) {
	if c.retrievalRuntime == nil {
		return RetrievalRuntimeOptions{}, false
	}
	return *c.retrievalRuntime, true
}
func (v RetrievalRuntimeOptions) Backend() lexical.Backend { return v.backend }
func (v RetrievalRuntimeOptions) Validate() error {
	if _, err := lexical.Expressions(v.backend, lexical.KnowledgeChunks, 1); err != nil {
		return invalid(retrievalRuntimeSetting)
	}
	return nil
}
func (RetrievalRuntimeOptions) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "retrieval_runtime_options")
}
func (RetrievalRuntimeOptions) MarshalJSON() ([]byte, error) {
	return []byte(`"retrieval_runtime_options"`), nil
}
func (RetrievalRuntimeOptions) LogValue() slog.Value {
	return slog.StringValue("retrieval_runtime_options")
}

func loadRetrievalRuntime(lookup LookupEnv) (*RetrievalRuntimeOptions, error) {
	raw, present := lookup(Prefix + retrievalRuntimeSetting)
	if !present {
		return nil, nil
	}
	if len(raw) == 0 || len(raw) > 1024 {
		return nil, invalid(retrievalRuntimeSetting)
	}
	if _, err := cursor.CanonicalJSON([]byte(raw)); err != nil {
		return nil, invalid(retrievalRuntimeSetting)
	}
	var wire struct {
		Backend lexical.Backend `json:"backend"`
	}
	if !runtimeObject(json.RawMessage(raw), &wire, "backend") {
		return nil, invalid(retrievalRuntimeSetting)
	}
	out := &RetrievalRuntimeOptions{backend: wire.Backend}
	if err := out.Validate(); err != nil {
		return nil, err
	}
	return out, nil
}
