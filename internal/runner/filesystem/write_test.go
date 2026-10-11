package filesystem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestWriteValidationAndSafeProjection(t *testing.T) {
	for _, path := range []string{"", ".", "..", "../escape", "/absolute", "a//b", "a/../b", "a\\b", "bad\x00", "bad\xff", strings.Repeat("a", 256)} {
		got, err := (*Root)(nil).Write(context.Background(), WriteRequest{Path: path})
		if !errors.Is(err, ErrInvalid) || got != (WriteResult{State: WriteNotCommitted}) {
			t.Fatal("invalid write path", err)
		}
	}
	if _, err := (*Root)(nil).Write(context.Background(), WriteRequest{Path: "ok", Data: make([]byte, MaxWriteBytes+1)}); !errors.Is(err, ErrInvalid) {
		t.Fatal("oversized write", err)
	}
	if _, err := (*Root)(nil).Write(context.Background(), WriteRequest{Path: "ok"}); !errors.Is(err, ErrClosed) {
		t.Fatal("nil root", err)
	}
	if _, err := (&Root{}).Write(context.Background(), WriteRequest{Path: "ok"}); !errors.Is(err, ErrClosed) {
		t.Fatal("zero root", err)
	}
	for _, value := range []any{WriteRequest{Path: "PRIVATE_PATH", Data: []byte("PRIVATE_CONTENT")}, WriteResult{State: WriteCommitted, BytesWritten: 123}} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var log bytes.Buffer
		slog.New(slog.NewJSONHandler(&log, nil)).Info("write", "value", value)
		all := fmt.Sprintf("%v %+v %#v", value, value, value) + string(encoded) + log.String()
		if strings.Contains(all, "PRIVATE") || strings.Contains(all, "123") {
			t.Fatal("write default projection leaked")
		}
	}
}
