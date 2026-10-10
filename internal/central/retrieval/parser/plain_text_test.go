package parser_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	p "github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
)

func TestPlainTextHandCountedByteRanges(t *testing.T) {
	// Hand-counted bytes: 甲。=6, CRLF=2, 乙！=6, CRLF CRLF=4,
	// A. B?=5. Position oracles do not call the parser or a segmentation helper.
	raw := "甲。\r\n乙！\r\n\r\nA. B?"
	got := must(p.ParsePlainText(context.Background(), source(), raw))
	want := []p.StructuredElement{
		{Kind: p.Paragraph, Ordinal: 1, Text: "甲。\r\n乙！", Bytes: p.ByteRange{Start: 0, End: 14}, Sentences: []p.ByteRange{{Start: 0, End: 6}, {Start: 6, End: 14}}},
		{Kind: p.Paragraph, Ordinal: 2, Text: "A. B?", Bytes: p.ByteRange{Start: 18, End: 23}, Sentences: []p.ByteRange{{Start: 18, End: 20}, {Start: 20, End: 23}}},
	}
	if got.SourceBytes != 23 || !reflect.DeepEqual(got.Elements, want) {
		t.Fatalf("wrong coordinates: first=%v second=%v length=%d", got.Elements[0].Bytes, got.Elements[len(got.Elements)-1].Bytes, got.SourceBytes)
	}
	checkRanges(t, raw, got)
}

func TestPlainTextPreservesBOMNULIndentationAndMixedLines(t *testing.T) {
	raw := " \t\r\n\ufeffA\x00.\rB\n \t\nC\r\n"
	got := must(p.ParsePlainText(context.Background(), source(), raw))
	want := []p.StructuredElement{
		{Kind: p.Paragraph, Ordinal: 1, Text: "\ufeffA\x00.\rB", Bytes: p.ByteRange{Start: 4, End: 12}, Sentences: []p.ByteRange{{Start: 4, End: 10}, {Start: 10, End: 12}}},
		{Kind: p.Paragraph, Ordinal: 2, Text: "C", Bytes: p.ByteRange{Start: 16, End: 17}, Sentences: []p.ByteRange{{Start: 16, End: 17}}},
	}
	if got.SourceBytes != 19 || !reflect.DeepEqual(got.Elements, want) {
		t.Fatal("changed original BOM/NUL/line-ending bytes or positions")
	}
	checkRanges(t, raw, got)
	for _, raw := range []string{"", "\r\n", "\t \n \r\t\r\n", " \t"} {
		got := must(p.ParsePlainText(context.Background(), source(), raw))
		if len(got.Elements) != 0 || got.SourceBytes != len(raw) || got.ParserProfile != p.PlainTextProfile {
			t.Fatal("empty or ASCII-blank document lost its successful identity")
		}
	}
	for _, raw := range []string{"\u00a0", "\u3000", "\u2028", "\v", "\f", "  x\t "} {
		got := must(p.ParsePlainText(context.Background(), source(), raw))
		if len(got.Elements) != 1 || got.Elements[0].Text != raw {
			t.Fatal("non-ASCII whitespace or indentation was normalized away")
		}
	}
}

func TestPlainTextSentenceProfile(t *testing.T) {
	cases := []struct {
		name, raw string
		spans     []p.ByteRange
	}{
		{"decimal and abbreviation", "3.14 Dr. X", []p.ByteRange{{Start: 0, End: 8}, {Start: 8, End: 10}}},
		{"ASCII must have boundary", "A!B?C.D", []p.ByteRange{{Start: 0, End: 7}}},
		{"Chinese needs no space", "甲。乙！丙？", []p.ByteRange{{Start: 0, End: 6}, {Start: 6, End: 12}, {Start: 12, End: 18}}},
		{"mixed terminal run", "A?!。B", []p.ByteRange{{Start: 0, End: 6}, {Start: 6, End: 7}}},
		{"closing delimiters", "A?!\"')] } B", []p.ByteRange{{Start: 0, End: 7}, {Start: 7, End: 11}}},
		{"closer no space", "A.)B", []p.ByteRange{{Start: 0, End: 4}}},
		{"Chinese closer", "甲。”乙", []p.ByteRange{{Start: 0, End: 9}, {Start: 9, End: 12}}},
		{"non-ASCII space is content", "A.\u00a0B", []p.ByteRange{{Start: 0, End: 5}}},
		{"trailing ASCII space", "A. \t", []p.ByteRange{{Start: 0, End: 4}}},
		{"emoji and combining mark", "😀e\u0301! Z", []p.ByteRange{{Start: 0, End: 8}, {Start: 8, End: 10}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := must(p.ParsePlainText(context.Background(), source(), tt.raw))
			if len(got.Elements) != 1 || !reflect.DeepEqual(got.Elements[0].Sentences, tt.spans) {
				t.Fatalf("ranges=%v, want=%v", got.Elements[0].Sentences, tt.spans)
			}
			checkRanges(t, tt.raw, got)
		})
	}
	// Every explicitly frozen closer, with an independently counted rune width.
	for _, closer := range []string{"\"", "'", ")", "]", "}", "”", "’", "」", "』", "）", "】", "》", "〉"} {
		raw := "A!" + closer + " Z"
		got := must(p.ParsePlainText(context.Background(), source(), raw))
		width := 1
		if closer[0] >= utf8.RuneSelf {
			width = 3
		}
		want := []p.ByteRange{{Start: 0, End: 2 + width}, {Start: 2 + width, End: 4 + width}}
		if !reflect.DeepEqual(got.Elements[0].Sentences, want) {
			t.Fatal("frozen closer not joined to preceding sentence")
		}
	}
}

func TestPlainTextUTF8AndHardLimits(t *testing.T) {
	for _, raw := range []string{"\xff", "a\xc0\x80", "\xed\xa0\x80", "ok\xe2\x82", "\xf4\x90\x80\x80"} {
		got, err := p.ParsePlainText(context.Background(), source(), raw)
		wantFault(t, got, err, f.InvalidArgument)
	}
	valid := strings.Repeat("a", p.MaxSourceBytes-3) + "�"
	got := must(p.ParsePlainText(context.Background(), source(), valid))
	if got.SourceBytes != p.MaxSourceBytes || got.Elements[0].Text != valid {
		t.Fatal("exact byte cap or valid replacement character rejected")
	}
	bad, err := p.ParsePlainText(context.Background(), source(), valid+"x")
	wantFault(t, bad, err, f.PayloadTooLarge)
	paragraphs := strings.Repeat("x\n\n", p.MaxParagraphs)
	got = must(p.ParsePlainText(context.Background(), source(), paragraphs))
	if len(got.Elements) != p.MaxParagraphs {
		t.Fatal("exact paragraph limit rejected")
	}
	bad, err = p.ParsePlainText(context.Background(), source(), paragraphs+"x")
	wantFault(t, bad, err, f.PayloadTooLarge)
	sentences := strings.Repeat("x. ", p.MaxSentences-1) + "x."
	got = must(p.ParsePlainText(context.Background(), source(), sentences))
	if len(got.Elements[0].Sentences) != p.MaxSentences {
		t.Fatal("exact sentence limit rejected")
	}
	bad, err = p.ParsePlainText(context.Background(), source(), sentences+" x.")
	wantFault(t, bad, err, f.PayloadTooLarge)
	// The cap applies across paragraphs, not separately to each one.
	bad, err = p.ParsePlainText(context.Background(), source(), strings.Repeat("a. ", 32769)+"\n\n"+strings.Repeat("b. ", 32768))
	wantFault(t, bad, err, f.PayloadTooLarge)
}

type cancelOnCheck struct {
	context.Context
	calls, cancelAt int
	err             error
}

func (c *cancelOnCheck) Err() error {
	c.calls++
	if c.calls >= c.cancelAt {
		return c.err
	}
	return nil
}

func TestPlainTextCooperativeCancellationReturnsNoPrefix(t *testing.T) {
	for _, cutoff := range []int{1, 4, 300, 600} {
		for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
			ctx := &cancelOnCheck{Context: context.Background(), cancelAt: cutoff, err: cause}
			got, err := p.ParsePlainText(ctx, source(), strings.Repeat("a", p.MaxSourceBytes))
			if !errors.Is(err, cause) || !reflect.DeepEqual(got, p.StructuredDocument{}) || ctx.calls != cutoff {
				t.Fatalf("cancel at check %d ignored or returned a prefix", cutoff)
			}
		}
	}
	got, err := p.ParsePlainText(nil, source(), "x")
	wantFault(t, got, err, f.InvalidArgument)
}

func TestPlainTextDeterministicIndependentResults(t *testing.T) {
	raw := "\ufeff甲？!』\r\n  next. e\u0301\r\n\r\n😀 end!\t"
	first := must(p.ParsePlainText(context.Background(), source(), raw))
	for range 8 {
		next := must(p.ParsePlainText(context.Background(), source(), raw))
		if !reflect.DeepEqual(first, next) {
			t.Fatal("same source/profile produced different result")
		}
		checkRanges(t, raw, next)
		next.Elements[0].Sentences[0].End = 0
		if first.Elements[0].Sentences[0].End == 0 {
			t.Fatal("results share mutable sentence storage")
		}
	}
}

func checkRanges(t *testing.T, raw string, got p.StructuredDocument) {
	t.Helper()
	previous := 0
	for i, e := range got.Elements {
		if e.Ordinal != i+1 || e.Kind != p.Paragraph || e.Bytes.Start < previous || e.Bytes.Start >= e.Bytes.End || e.Bytes.End > len(raw) || raw[e.Bytes.Start:e.Bytes.End] != e.Text || !utf8.ValidString(e.Text) {
			t.Fatal("paragraph did not preserve its original byte range")
		}
		at := e.Bytes.Start
		for _, r := range e.Sentences {
			if r.Start != at || r.End <= r.Start || r.End > e.Bytes.End || !utf8.ValidString(raw[r.Start:r.End]) {
				t.Fatal("sentence ranges overlap, omit bytes or split a code point")
			}
			at = r.End
		}
		if at != e.Bytes.End {
			t.Fatal("sentences do not cover paragraph")
		}
		previous = e.Bytes.End
	}
}
