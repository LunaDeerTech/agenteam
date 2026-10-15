// Package document converts bounded source bytes into located structural blocks.
package document

import (
	"context"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
)

const Profile = "plain-markdown:v1"

func Parse(ctx context.Context, source parser.SourceIdentity, title, media, text string) (chunker.Document, error) {
	if ctx == nil || source.Validate() != nil || !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		return chunker.Document{}, invalid()
	}
	if len(text) > parser.MaxSourceBytes {
		return chunker.Document{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	if err := ctx.Err(); err != nil {
		return chunker.Document{}, err
	}
	switch media {
	case "text/plain":
		parsed, err := parser.ParsePlainText(ctx, source, text)
		if err != nil {
			return chunker.Document{}, err
		}
		return chunker.FromPlain(parsed, title, text)
	case "text/markdown":
		return markdown(ctx, source, title, text)
	default:
		return chunker.Document{}, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
}

type line struct {
	start, end, next int
	text             string
}

func lines(text string) []line {
	var out []line
	for at := 0; at < len(text); {
		end := at
		for end < len(text) && text[end] != '\r' && text[end] != '\n' {
			end++
		}
		next := end
		if next < len(text) {
			next++
			if text[end] == '\r' && next < len(text) && text[next] == '\n' {
				next++
			}
		}
		out = append(out, line{at, end, next, text[at:end]})
		at = next
	}
	return out
}
func heading(text string) (int, string) {
	trimmed := strings.TrimLeft(text, " ")
	if len(text)-len(trimmed) > 3 {
		return 0, ""
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level < 1 || level > 6 || level < len(trimmed) && trimmed[level] != ' ' && trimmed[level] != '\t' {
		return 0, ""
	}
	value := strings.TrimSpace(trimmed[level:])
	value = strings.TrimSpace(strings.TrimRight(value, "#"))
	if value == "" {
		return 0, ""
	}
	return level, value
}
func fence(text string) (byte, int) {
	text = strings.TrimLeft(text, " ")
	if len(text) < 3 || (text[0] != '`' && text[0] != '~') {
		return 0, 0
	}
	n := 1
	for n < len(text) && text[n] == text[0] {
		n++
	}
	if n < 3 {
		return 0, 0
	}
	return text[0], n
}
func listItem(text string) bool {
	text = strings.TrimLeft(text, " \t")
	if len(text) > 1 && strings.ContainsRune("-*+", rune(text[0])) && (text[1] == ' ' || text[1] == '\t') {
		return true
	}
	n := 0
	for n < len(text) && text[n] >= '0' && text[n] <= '9' {
		n++
	}
	return n > 0 && n < 10 && n+1 < len(text) && (text[n] == '.' || text[n] == ')') && (text[n+1] == ' ' || text[n+1] == '\t')
}
func tableRule(text string) bool {
	text = strings.TrimSpace(text)
	if !strings.Contains(text, "|") {
		return false
	}
	found := false
	for _, r := range text {
		switch r {
		case '-':
			found = true
		case '|', ':', ' ', '\t':
		default:
			return false
		}
	}
	return found
}
func markdown(ctx context.Context, source parser.SourceIdentity, title, text string) (chunker.Document, error) {
	out := chunker.Document{Source: source, Title: title, ParserProfile: Profile, Text: text, Elements: []chunker.Element{}}
	ls := lines(text)
	var path []string
	var levels []int
	appendBlock := func(kind string, start, end int) error {
		if len(out.Elements) >= parser.MaxParagraphs {
			return f.NewFault(f.PayloadTooLarge, f.NotStarted)
		}
		out.Elements = append(out.Elements, chunker.Element{Kind: kind, Ordinal: len(out.Elements) + 1, Bytes: parser.ByteRange{Start: start, End: end}, HeadingPath: append([]string(nil), path...)})
		return nil
	}
	for i := 0; i < len(ls); {
		if err := ctx.Err(); err != nil {
			return chunker.Document{}, err
		}
		if strings.TrimSpace(ls[i].text) == "" {
			i++
			continue
		}
		if level, value := heading(ls[i].text); level != 0 {
			for len(levels) > 0 && levels[len(levels)-1] >= level {
				levels = levels[:len(levels)-1]
				path = path[:len(path)-1]
			}
			levels = append(levels, level)
			path = append(path, value)
			if err := appendBlock("heading", ls[i].start, ls[i].end); err != nil {
				return chunker.Document{}, err
			}
			i++
			continue
		}
		start, end, kind := i, i, "paragraph"
		if marker, count := fence(ls[i].text); count > 0 {
			kind = "code"
			end = i + 1
			for end < len(ls) {
				m, n := fence(ls[end].text)
				if m == marker && n >= count {
					break
				}
				end++
			}
			if end == len(ls) {
				end--
			}
		} else if i+1 < len(ls) && strings.Contains(ls[i].text, "|") && tableRule(ls[i+1].text) {
			kind = "table"
			end = i + 1
			for end+1 < len(ls) && strings.Contains(ls[end+1].text, "|") && strings.TrimSpace(ls[end+1].text) != "" {
				end++
			}
		} else {
			if listItem(ls[i].text) {
				kind = "list"
			}
			for end+1 < len(ls) && strings.TrimSpace(ls[end+1].text) != "" {
				level, _ := heading(ls[end+1].text)
				_, n := fence(ls[end+1].text)
				if level != 0 || n > 0 || listItem(ls[end+1].text) != (kind == "list") || end+2 < len(ls) && tableRule(ls[end+2].text) {
					break
				}
				end++
			}
		}
		if err := appendBlock(kind, ls[start].start, ls[end].end); err != nil {
			return chunker.Document{}, err
		}
		i = end + 1
	}
	return out, nil
}
func invalid() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }
