//go:build integration

package account_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"strconv"
	"testing"
)

func TestAccountHTTPAvatarOriginalDigestRangesAndLeaseClosure(t *testing.T) {
	f := newHTTPFixture(t)
	admin := f.admin()
	user := httpObject(t, admin.profile(), "user")
	version := httpString(t, user, "version")
	im := image.NewNRGBA(image.Rect(0, 0, 40, 24))
	for y := range 24 {
		for x := range 40 {
			im.SetNRGBA(x, y, color.NRGBA{uint8(5 * x), uint8(9 * y), 100, 128})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, im); err != nil {
		t.Fatal(err)
	}
	upload := func(raw []byte, key, match, contentType string) httpResult {
		return admin.raw("PUT", "/api/v1/me/avatar", raw, func(r *http.Request) {
			r.Header.Set("Content-Type", contentType)
			r.Header.Set("If-Match", match)
			r.Header.Set("X-CSRF-Token", admin.csrf)
			r.Header.Set("Idempotency-Key", key)
		})
	}
	key := id[struct{}](t).String()
	match := `"` + version + `"`
	upload(encoded.Bytes(), id[struct{}](t).String(), "W/"+match, "image/png").problem(t, 400, "INVALID_ARGUMENT")
	upload(encoded.Bytes(), id[struct{}](t).String(), match, "application/octet-stream").problem(t, 415, "UNSUPPORTED_MEDIA_TYPE")
	applied := upload(encoded.Bytes(), key, match, "image/png").want(t, 200)
	replay := upload(encoded.Bytes(), key, match, "image/png").want(t, 200)
	if !bytes.Equal(applied.data, replay.data) {
		t.Fatal("avatar replay changed its original upload receipt")
	}
	profile := applied.object(t)
	meta := httpObject(t, profile, "avatar")
	if meta["media_type"] != "image/jpeg" {
		t.Fatal("avatar was not safely re-encoded")
	}
	full := admin.request("GET", "/api/v1/me/avatar", nil, "", "").want(t, 200)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(full.data))
	if meta["sha256"] != digest || meta["byte_size"] != strconv.Itoa(len(full.data)) || full.headers.Get("Content-Type") != "image/jpeg" {
		t.Fatal("avatar wire body and stored metadata disagree")
	}
	decoded, err := jpeg.Decode(bytes.NewReader(full.data))
	if err != nil || decoded.Bounds().Dx() != 40 || decoded.Bounds().Dy() != 24 {
		t.Fatal("avatar conversion lost valid image dimensions")
	}
	head := admin.request("HEAD", "/api/v1/me/avatar", nil, "", "").want(t, 200)
	if len(head.data) != 0 || head.headers.Get("Content-Length") != strconv.Itoa(len(full.data)) {
		t.Fatal("HEAD did not return exact full-object metadata without bytes")
	}
	for _, r := range []struct {
		header     string
		start, end int
	}{
		{"bytes=0-9", 0, 10}, {"bytes=5-", 5, len(full.data)}, {"bytes=-7", len(full.data) - 7, len(full.data)},
	} {
		partial := admin.raw("GET", "/api/v1/me/avatar", nil, func(q *http.Request) { q.Header.Set("Range", r.header) }).want(t, 206)
		if !bytes.Equal(partial.data, full.data[r.start:r.end]) || partial.headers.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", r.start, r.end-1, len(full.data)) {
			t.Fatal("single range was not resolved against the opened avatar")
		}
	}
	for _, value := range []string{"bytes=0-1,3-4", "bytes=" + strconv.Itoa(len(full.data)) + "-"} {
		admin.raw("GET", "/api/v1/me/avatar", nil, func(r *http.Request) { r.Header.Set("Range", value) }).problem(t, 416, "RANGE_NOT_SATISFIABLE")
	}
	conn := f.db.Connect(t)
	var liveReaders int
	if err := conn.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='reader' AND state='active'`).Scan(&liveReaders); err != nil || liveReaders != 0 {
		t.Fatal("HTTP completion left a real avatar reader lease active", err)
	}
	// Different valid PNG bytes under the same key must not replay a JPEG digest.
	im.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	encoded.Reset()
	if err := png.Encode(&encoded, im); err != nil {
		t.Fatal(err)
	}
	upload(encoded.Bytes(), key, match, "image/png").problem(t, 409, "IDEMPOTENCY_KEY_REUSED")
	version = httpString(t, httpObject(t, profile, "user"), "version")
	admin.request("DELETE", "/api/v1/me/avatar", map[string]any{"version": version}, id[struct{}](t).String(), admin.csrf).want(t, 204)
	if admin.profile()["avatar"] != nil {
		t.Fatal("avatar deletion kept the user reference")
	}
	admin.request("GET", "/api/v1/me/avatar", nil, "", "").problem(t, 404, "NOT_FOUND")
}
