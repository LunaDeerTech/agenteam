package object

import (
	"strings"
	"testing"

	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestDownloadSingleRangeBounds(t *testing.T) {
	for _, tc := range []struct {
		raw                   string
		total, offset, length int64
		partial               bool
	}{{"", 0, 0, 0, false}, {"", 12, 0, 12, false}, {"bytes=0-0", 12, 0, 1, true}, {"bytes=11-", 12, 11, 1, true}, {"bytes=3-999", 12, 3, 9, true}, {"bytes=-5", 12, 7, 5, true}, {"bytes=-99", 12, 0, 12, true}, {"bytes=0001-0003", 12, 1, 3, true}} {
		a, b, c, e := parseDownloadRange(tc.raw, tc.total)
		if e != nil || a != tc.offset || b != tc.length || c != tc.partial {
			t.Fatal("valid range", tc.raw, a, b, c, e)
		}
	}
	for _, raw := range []string{"bytes=", "Bytes=0-1", "bytes=0-1,3-4", "bytes=12-", "bytes=2-1", "bytes=-0", "bytes=0--1", "bytes=+1-2", "bytes=0-9223372036854775808", "bytes= 1-2", strings.Repeat("x", 129)} {
		if _, _, _, e := parseDownloadRange(raw, 12); e == nil {
			t.Fatal("invalid range accepted", raw)
		}
	}
	if _, _, _, e := parseDownloadRange("bytes=0-0", 0); e == nil {
		t.Fatal("empty body has a range")
	}
}
func TestDownloadPreviewUsesActualContentAndClosedAllowedTypes(t *testing.T) {
	for _, tc := range []struct{ body, media, disposition string }{{"safe Markdown\n# title", "text/plain; charset=utf-8", "inline"}, {`{"x":1}`, "text/plain; charset=utf-8", "inline"}, {"<svg onload='secret' />", "application/octet-stream", "attachment"}, {" \xef\xbb\xbf<html>", "application/octet-stream", "attachment"}, {"<?xml version='1.0'?>", "application/octet-stream", "attachment"}, {"%PDF-1.5", "application/octet-stream", "attachment"}, {"PK\x03\x04docx", "application/octet-stream", "attachment"}, {"\x89PNG\r\n\x1a\nheader", "image/png", "inline"}, {"GIF89a", "image/gif", "inline"}, {"\x00\x01\x02binary", "application/octet-stream", "attachment"}} {
		media, disposition := downloadPresentation(oc.DownloadPreview, []byte(tc.body), 0)
		if media != tc.media || disposition != tc.disposition {
			t.Fatalf("projection media=%s disposition=%s", media, disposition)
		}
	}
	for _, mode := range []oc.DownloadMode{oc.DownloadAttachment, oc.DownloadPreview} {
		media, disp := downloadPresentation(mode, []byte("x"), 3)
		if media != "application/octet-stream" || disp != "attachment" {
			t.Fatal("offset prefix was treated as original type")
		}
	}
}
