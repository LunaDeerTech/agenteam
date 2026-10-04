package artifact

import (
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
)

func TestPreviewUsesUTF8ByteBoundariesWithoutReplacement(t *testing.T) {
	body := []byte("a界é🙂z")
	for _, tc := range []struct {
		offset   int64
		limit    int
		text     string
		next     int64
		boundary bool
	}{{0, 4, "a界", 4, true}, {1, 2, "", 1, true}, {1, 3, "界", 4, true}, {4, 5, "é", 6, true}, {6, 4, "🙂", 10, true}, {10, 1, "z", 11, true}, {11, 1, "", 11, true}, {2, 8, "", 0, false}, {5, 4, "", 0, false}, {9, 4, "", 0, false}} {
		text, next, valid, boundary := previewText(body, 0, int64(len(body)), ac.PreviewRequest{Offset: tc.offset, Limit: tc.limit})
		if !valid || boundary != tc.boundary || boundary && (text != tc.text || next != tc.next) {
			t.Fatalf("byte offset %d limit %d => %q/%d/%v/%v", tc.offset, tc.limit, text, next, valid, boundary)
		}
	}
	for _, bad := range [][]byte{{0xff}, {0xe7, 0x95}, {'a', 0, 0xf0, 0x80, 0x80, 0x80}} {
		_, _, valid, _ := previewText(bad, 0, int64(len(bad)), ac.PreviewRequest{Limit: 8})
		if valid {
			t.Fatal("malformed UTF8 became text")
		}
	}
	long := strings.Repeat("界", 30000)
	offset := int64(60000)
	start := offset - 3
	window := []byte(long[start : start+71])
	text, next, valid, boundary := previewText(window, start, int64(len(long)), ac.PreviewRequest{Offset: offset, Limit: 64})
	if !valid || !boundary || len(text) != 63 || next != offset+63 {
		t.Fatal("large bounded window split rune")
	}
}
