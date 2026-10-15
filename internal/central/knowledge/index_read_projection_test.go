package knowledge

import (
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"testing"
)

func TestParsedFileReadUTF8Coordinates(t *testing.T) {
	first, err := parsedTextPage("甲。\nA", "docx_xml:v1", kc.ReadRequest{MaxBytes: 4})
	if err != nil || first.Text != "甲" || first.NextByteOffset != 3 || !first.Truncated {
		t.Fatal("wrong extracted page", err)
	}
	next, err := parsedTextPage("甲。\nA", "docx_xml:v1", kc.ReadRequest{ByteOffset: first.NextByteOffset, MaxBytes: 8})
	if err != nil || next.Text != "。\nA" || next.NextByteOffset != 8 || next.Truncated {
		t.Fatal("wrong extracted continuation", err)
	}
	if _, err := parsedTextPage("甲。", "docx_xml:v1", kc.ReadRequest{ByteOffset: 1, MaxBytes: 8}); err == nil {
		t.Fatal("split UTF8 offset admitted")
	}
}
