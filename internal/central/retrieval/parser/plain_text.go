package parser

import (
	"context"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// ParsePlainText implements the frozen plain_text:v1 profile:
//   - It retains original UTF-8 bytes, including BOM, NUL and CR/LF.
//   - CRLF, LF and CR terminate lines. A line containing only ASCII space/TAB
//     is blank. Consecutive nonblank lines form a paragraph, including internal
//     line endings but excluding its final line ending and separating blanks.
//   - A maximal run of .?!。！？ consumes following closing characters from
//     " ' ) ] } ” ’ 」 』 ） 】 》 〉. A run containing Chinese punctuation always
//     ends a sentence. An ASCII-only run does so only before ASCII space/TAB/
//     CR/LF or paragraph end. No abbreviation or semantic inference is made.
//   - Inter-sentence whitespace belongs to the following sentence; an all-ASCII
//     whitespace remainder belongs to the last sentence. Ranges cover the
//     paragraph continuously, with no empty sentences.
//
// Changing these rules requires a new profile. No timer, goroutine or stream is
// created. Errors, including cancellation, return a zero result, never a prefix.
func ParsePlainText(ctx context.Context, source SourceIdentity, text string) (StructuredDocument, error) {
	if ctx == nil {
		return StructuredDocument{}, parseFault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return StructuredDocument{}, err
	}
	if err := source.Validate(); err != nil {
		return StructuredDocument{}, err
	}
	if len(text) > MaxSourceBytes {
		return StructuredDocument{}, parseFault(f.PayloadTooLarge)
	}
	// Validate incrementally so even malformed input observes cancellation.
	check := scanCheck{ctx: ctx}
	for at := 0; at < len(text); {
		if err := check.at(at); err != nil {
			return StructuredDocument{}, err
		}
		r, size := utf8.DecodeRuneInString(text[at:])
		if r == utf8.RuneError && size == 1 {
			return StructuredDocument{}, parseFault(f.InvalidArgument)
		}
		at += size
	}
	out := StructuredDocument{Source: source, ParserProfile: PlainTextProfile, SourceBytes: len(text), Elements: []StructuredElement{}}
	paragraphStart, paragraphEnd, sentences := -1, 0, 0
	appendParagraph := func() error {
		if paragraphStart < 0 {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(out.Elements) == MaxParagraphs {
			return parseFault(f.PayloadTooLarge)
		}
		ranges, err := sentenceRanges(ctx, text, paragraphStart, paragraphEnd, MaxSentences-sentences)
		if err != nil {
			return err
		}
		out.Elements = append(out.Elements, StructuredElement{
			Kind: Paragraph, Ordinal: len(out.Elements) + 1,
			Text: text[paragraphStart:paragraphEnd], Bytes: ByteRange{paragraphStart, paragraphEnd}, Sentences: ranges,
		})
		sentences += len(ranges)
		paragraphStart = -1
		return nil
	}
	check = scanCheck{ctx: ctx}
	for lineStart := 0; lineStart < len(text); {
		lineEnd, blank := lineStart, true
		for lineEnd < len(text) && text[lineEnd] != '\r' && text[lineEnd] != '\n' {
			if err := check.at(lineEnd); err != nil {
				return StructuredDocument{}, err
			}
			blank = blank && (text[lineEnd] == ' ' || text[lineEnd] == '\t')
			lineEnd++
		}
		if err := check.at(lineEnd); err != nil {
			return StructuredDocument{}, err
		}
		if blank {
			if err := appendParagraph(); err != nil {
				return StructuredDocument{}, err
			}
		} else {
			if paragraphStart < 0 {
				paragraphStart = lineStart
			}
			paragraphEnd = lineEnd
		}
		lineStart = lineEnd
		if lineStart < len(text) {
			lineStart++
			if text[lineEnd] == '\r' && lineStart < len(text) && text[lineStart] == '\n' {
				lineStart++
			}
		}
	}
	if err := appendParagraph(); err != nil {
		return StructuredDocument{}, err
	}
	if err := ctx.Err(); err != nil {
		return StructuredDocument{}, err
	}
	return out, nil
}

// The checkpoint gap leaves room for one maximum-width UTF-8 decode, so no
// scanning loop processes more than 4096 bytes between context observations.
type scanCheck struct {
	ctx  context.Context
	next int
}

func (c *scanCheck) at(offset int) error {
	if offset < c.next {
		return nil
	}
	c.next = offset + 4096 - utf8.UTFMax + 1
	return c.ctx.Err()
}

func asciiSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }

func terminator(r rune) (terminal, chinese bool) {
	switch r {
	case '.', '?', '!':
		return true, false
	case '。', '！', '？':
		return true, true
	}
	return false, false
}

func closing(r rune) bool {
	switch r {
	case '"', '\'', ')', ']', '}', '”', '’', '」', '』', '）', '】', '》', '〉':
		return true
	}
	return false
}

func sentenceRanges(ctx context.Context, text string, start, end, remaining int) ([]ByteRange, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	check := scanCheck{ctx: ctx}
	result := make([]ByteRange, 0, 1)
	appendRange := func(a, b int) error {
		if len(result) == remaining {
			return parseFault(f.PayloadTooLarge)
		}
		result = append(result, ByteRange{a, b})
		return nil
	}
	sentenceStart, whitespace := start, true
	for at := start; at < end; {
		if err := check.at(at); err != nil {
			return nil, err
		}
		r, size := utf8.DecodeRuneInString(text[at:end])
		terminal, chinese := terminator(r)
		whitespace = whitespace && size == 1 && asciiSpace(text[at])
		if !terminal {
			at += size
			continue
		}
		next := at + size
		for next < end {
			if err := check.at(next); err != nil {
				return nil, err
			}
			r, size = utf8.DecodeRuneInString(text[next:end])
			t, c := terminator(r)
			if !t {
				break
			}
			chinese = chinese || c
			next += size
		}
		for next < end {
			if err := check.at(next); err != nil {
				return nil, err
			}
			r, size = utf8.DecodeRuneInString(text[next:end])
			if !closing(r) {
				break
			}
			next += size
		}
		if chinese || next == end || asciiSpace(text[next]) {
			if err := appendRange(sentenceStart, next); err != nil {
				return nil, err
			}
			sentenceStart, whitespace = next, true
		}
		at = next
	}
	if sentenceStart < end {
		if whitespace && len(result) > 0 {
			result[len(result)-1].End = end
		} else if err := appendRange(sentenceStart, end); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
