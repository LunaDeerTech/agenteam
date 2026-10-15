package memory

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
)

type GateResult struct {
	Text    string
	Outcome string
}
type SensitiveGate interface {
	Inspect(context.Context, string) (GateResult, error)
}

// SensitiveDetector is a local preflight, invoked before any model or index
// request. It never logs matching values. Unknown arbitrary secrets cannot be
// inferred from prose; callers must still avoid submitting credentials.
type SensitiveDetector struct{}

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\b(?:password|passwd|api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|secret[_-]?key|authorization)\s*[:=]\s*["']?[^\s"']{4,}`),
	regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[A-Z0-9]{16})\b`),
	regexp.MustCompile(`(?i)\b(?:https?|postgres(?:ql)?|mysql|redis)://[^\s/@:]+:[^\s/@]+@`),
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/-]{12,}`),
}

func (SensitiveDetector) Inspect(ctx context.Context, text string) (GateResult, error) {
	if err := checkContext(ctx); err != nil {
		return GateResult{}, err
	}
	if len(text) == 0 || len(text) > c.MaxContentBytes+5120 || !utf8.ValidString(text) || strings.ContainsRune(text, '\x00') {
		return GateResult{}, fault(f.InvalidArgument)
	}
	for _, pattern := range sensitivePatterns {
		if pattern.MatchString(text) {
			return GateResult{}, fault(f.Forbidden)
		}
	}
	return GateResult{Text: text, Outcome: "allow"}, nil
}
func inspect(ctx context.Context, gate SensitiveGate, text string) (GateResult, error) {
	if nilPort(gate) {
		return GateResult{}, fault(f.DependencyUnbound)
	}
	result, err := gate.Inspect(ctx, text)
	if err != nil {
		return GateResult{}, portError(err)
	}
	if result.Outcome != "allow" && result.Outcome != "mask" || result.Text == "" || len(result.Text) > c.MaxContentBytes+5120 || !utf8.ValidString(result.Text) {
		return GateResult{}, fault(f.InvalidState)
	}
	return result, nil
}
