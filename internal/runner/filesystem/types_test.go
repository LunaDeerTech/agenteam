package filesystem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"testing"
)

func TestRequestBounds(t *testing.T) {
	good := ReadRequest{"a/b", 0, 1, "utf8"}
	for _, path := range []string{"", ".", "..", "a/../b", "a/./b", "/a", "a//b", "a/", "a\\b", "a\x00b", "\xff", strings.Repeat("a", 256), strings.Repeat("a/", 2048) + "b"} {
		r := good
		r.Path = path
		if r.valid() {
			t.Fatalf("invalid path accepted, length %d", len(path))
		}
	}
	for _, vector := range []ReadRequest{{"a", -1, 1, "utf8"}, {"a", 0, 0, "utf8"}, {"a", 0, MaxReadBytes + 1, "utf8"}, {"a", math.MaxInt64 - 1, 1, "utf8"}, {"a", 0, 1, "UTF8"}, {"a", 0, 1, ""}} {
		if vector.valid() {
			t.Fatal("invalid read accepted")
		}
	}
	for _, vector := range []ReadRequest{good, {"é/中", math.MaxInt64 - 2, 1, "base64"}, {strings.Repeat("a", 255), 0, MaxReadBytes, "utf8"}} {
		if !vector.valid() {
			t.Fatal("valid read rejected")
		}
	}
	for _, vector := range []ListRequest{{".", 1}, {"dir", MaxEntries}} {
		if !vector.valid() {
			t.Fatal("valid list rejected")
		}
	}
	for _, vector := range []ListRequest{{"", 1}, {"..", 1}, {"a", 0}, {"a", MaxEntries + 1}} {
		if vector.valid() {
			t.Fatal("invalid list accepted")
		}
	}
	var root Root
	if _, err := root.Read(context.Background(), good); !errors.Is(err, ErrClosed) {
		t.Fatal("zero root read", err)
	}
	root.Stop()
	if err := root.Drain(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatal("zero root drain", err)
	}
	if _, err := (*Root)(nil).List(context.Background(), ListRequest{".", 1}); !errors.Is(err, ErrClosed) {
		t.Fatal("nil root", err)
	}
}

func TestResultBudgetAndSafeOutput(t *testing.T) {
	// A valid maximum request with the largest ordinary JSON character expansion.
	result := ReadResult{"a", 0, MaxReadBytes, true, "utf8", strings.Repeat("\x00", MaxReadBytes)}
	if err := readBudget(result); err != nil {
		t.Fatal("valid escaped read", err)
	}
	result.Data = strings.Repeat("\x00", MaxResultBytes/6+1)
	if err := readBudget(result); !errors.Is(err, ErrLimit) {
		t.Fatal("actual escaped byte budget", err)
	}
	list := ListResult{Path: "."}
	for i := 0; i < MaxEntries; i++ {
		list.Entries = append(list.Entries, Entry{strings.Repeat("\x01", 255), "file"})
	}
	if err := listBudget(list); err != nil {
		t.Fatal("maximum list", err)
	}
	list.Entries = append(list.Entries, Entry{strings.Repeat("\x01", MaxResultBytes/6), "file"})
	if err := listBudget(list); !errors.Is(err, ErrLimit) {
		t.Fatal("actual list budget", err)
	}
	const canary = "D16_PRIVATE_PATH_CONTENT_CANARY"
	values := []any{ReadRequest{canary, 0, 1, "utf8"}, ReadResult{Path: canary, Data: canary}, ListRequest{canary, 1}, ListResult{Path: canary, Entries: []Entry{{canary, "file"}}}, Entry{canary, "file"}, &Root{}}
	var log bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&log, nil))
	for _, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(format, value), canary) {
				t.Fatal("formatted material")
			}
		}
		raw, err := json.Marshal(map[string]any{"nested": []any{value}})
		if err != nil || bytes.Contains(raw, []byte(canary)) {
			t.Fatal("nested JSON material")
		}
		logger.Info("safe", "nested", map[string]any{"value": value})
	}
	if bytes.Contains(log.Bytes(), []byte(canary)) {
		t.Fatal("nested log material")
	}
}
