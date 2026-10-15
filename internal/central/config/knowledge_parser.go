package config

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/document"
)

const (
	knowledgePDFToTextPath   = "KNOWLEDGE_PDFTOTEXT_PATH"
	knowledgePDFToTextSHA256 = "KNOWLEDGE_PDFTOTEXT_SHA256"
	knowledgePrlimitPath     = "KNOWLEDGE_PRLIMIT_PATH"
	knowledgePrlimitSHA256   = "KNOWLEDGE_PRLIMIT_SHA256"
)

// KnowledgeParserOptions is an explicit deployment binding. Loading it never
// opens an executable, reads its bytes or launches a version probe.
type KnowledgeParserOptions struct{ poppler document.PopplerConfig }

func (c Config) KnowledgeParser() (KnowledgeParserOptions, bool) {
	if c.knowledgeParser == nil {
		return KnowledgeParserOptions{}, false
	}
	return *c.knowledgeParser, true
}
func (p KnowledgeParserOptions) Poppler() document.PopplerConfig { return p.poppler }
func (p KnowledgeParserOptions) Validate() error {
	if p.poppler.Validate() != nil {
		return invalid("KNOWLEDGE_PARSER")
	}
	return nil
}
func (KnowledgeParserOptions) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "knowledge_parser_options")
}
func (KnowledgeParserOptions) MarshalJSON() ([]byte, error) {
	return []byte(`"knowledge_parser_options"`), nil
}
func (KnowledgeParserOptions) LogValue() slog.Value {
	return slog.StringValue("knowledge_parser_options")
}

func loadKnowledgeParser(lookup LookupEnv) (*KnowledgeParserOptions, error) {
	keys := []string{knowledgePDFToTextPath, knowledgePDFToTextSHA256, knowledgePrlimitPath, knowledgePrlimitSHA256}
	values, count := make([]string, len(keys)), 0
	for i, key := range keys {
		value, present := lookup(Prefix + key)
		if present {
			if value == "" {
				return nil, invalid(key)
			}
			count++
			values[i] = value
		}
	}
	if count == 0 {
		return nil, nil
	}
	if count != len(keys) {
		return nil, invalid("KNOWLEDGE_PARSER")
	}
	p := &KnowledgeParserOptions{document.PopplerConfig{PDFToTextPath: values[0], PDFToTextSHA256: values[1], PrlimitPath: values[2], PrlimitSHA256: values[3]}}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}
