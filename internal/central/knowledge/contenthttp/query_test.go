package contenthttp

import (
	"net/http/httptest"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestContentQueryCanonicalBounds(t *testing.T) {
	for _, tt := range []struct {
		raw    string
		offset f.Progress
		limit  int
	}{
		{"", 0, 65536}, {"byte_offset=0", 0, 65536}, {"max_bytes=1", 0, 1},
		{"max_bytes=1048576&byte_offset=9223372036854775807", 9223372036854775807, 1048576},
		{"byte_offset=3&max_bytes=7", 3, 7},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.URL.RawQuery = tt.raw
		q, err := parseQuery(r)
		if err != nil || q.ByteOffset != tt.offset || q.MaxBytes != tt.limit {
			t.Fatal("canonical read query rejected", tt.raw, err)
		}
	}
	for _, raw := range []string{
		"byte_offset=", "byte_offset", "byte_offset=-1", "byte_offset=+1", "byte_offset=%2b1", "byte_offset=01", "byte_offset=0%0a", "byte_offset=9223372036854775808",
		"max_bytes=0", "max_bytes=1048577", "max_bytes=1.0", "max_bytes=01", "max_bytes=%ff", "max_bytes=1;byte_offset=0", "max_bytes=1&", "max_bytes=1&max_%62ytes=2", "byte_offset=0&byte_%6fffset=1", "offset=0", "version=1", "%xx=1", "max_bytes=%xx", strings.Repeat("a", 257),
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.URL.RawQuery = raw
		if _, err := parseQuery(r); err == nil {
			t.Fatal("noncanonical query accepted", raw)
		}
	}
	r := httptest.NewRequest("GET", "/?", nil)
	if _, err := parseQuery(r); err == nil {
		t.Fatal("bare query accepted")
	}
}
