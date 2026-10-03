package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestMaterialOwnershipConcurrentUseAndDestroy(t *testing.T) {
	source := []byte("secret-material-private-canary")
	m, err := NewSecretMaterial(source)
	if err != nil {
		t.Fatal(err)
	}
	clear(source)
	for _, outer := range []any{m, struct{ private SecretMaterial }{m}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(format, outer), "private-canary") {
				t.Fatal("fmt leaked")
			}
		}
		b, _ := json.Marshal(outer)
		if strings.Contains(string(b), "private-canary") {
			t.Fatal("JSON leaked")
		}
		for _, text := range []bool{true, false} {
			var b bytes.Buffer
			var h slog.Handler = slog.NewJSONHandler(&b, nil)
			if text {
				h = slog.NewTextHandler(&b, nil)
			}
			slog.New(h).LogAttrs(context.Background(), slog.LevelInfo, "test", slog.Any("value", outer))
			if strings.Contains(b.String(), "private-canary") {
				t.Fatal("slog leaked")
			}
		}
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if err := m.Use(func(value []byte) error {
				if string(value) != "secret-material-private-canary" {
					t.Error("material was not an owned copy")
				}
				value[0] = 0
				return nil
			}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var borrowed []byte
	if err := m.Use(func(value []byte) error { borrowed = value; return nil }); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(borrowed, make([]byte, len(borrowed))) {
		t.Fatal("temporary use copy not wiped")
	}
	copy := m
	m.Destroy()
	if copy.Use(func([]byte) error { return nil }) == nil {
		t.Fatal("copied handle bypassed destruction")
	}
	m.Destroy()
	for _, size := range []int{0, MaxValueBytes + 1} {
		if _, err := NewSecretMaterial(make([]byte, size)); err == nil {
			t.Fatal("material size invalid")
		}
	}
}
