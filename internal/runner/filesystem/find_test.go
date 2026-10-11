package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

func TestFindRequestValidation(t *testing.T) {
	valid := FindRequest{".", "*.go", "name", 1}
	for _, request := range []FindRequest{
		valid,
		{"src", "src/[a-z]?.go", "path", MaxEntries},
		{".", `literal\*.go`, "name", 1},
		{".", "中?", "name", 1},
	} {
		if !request.valid() {
			t.Fatal("valid request rejected")
		}
	}
	for _, field := range []string{"path", "pattern", "match", "limit"} {
		t.Run(field, func(t *testing.T) {
			values := []any{}
			switch field {
			case "path":
				values = []any{"", "/root", "../a", "a/../b", "a/", "a//b", "a\\b", "\xff", strings.Repeat("a", 256)}
			case "pattern":
				values = []any{"", "**", "a/**/b", "[", "[z-a", "trailing\\", "\x00", "\xff", strings.Repeat("a", 257), "a/b"}
			case "match":
				values = []any{"", "NAME", "other"}
			case "limit":
				values = []any{-1, 0, MaxEntries + 1}
			}
			for _, value := range values {
				r := valid
				switch field {
				case "path":
					r.Path = value.(string)
				case "pattern":
					r.Pattern = value.(string)
				case "match":
					r.Match = value.(string)
				case "limit":
					r.Limit = value.(int)
				}
				var root *Root
				result, err := root.Find(context.Background(), r)
				if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(result, FindResult{}) {
					t.Fatal("invalid request reached root", err)
				}
			}
		})
	}
	if (FindRequest{".", "/src/*", "path", 1}).valid() {
		t.Fatal("absolute pattern accepted")
	}
	var root *Root
	if _, err := root.Find(context.Background(), valid); !errors.Is(err, ErrClosed) {
		t.Fatal("nil root", err)
	}
	if _, err := (&Root{}).Find(context.Background(), valid); !errors.Is(err, ErrClosed) {
		t.Fatal("zero root", err)
	}
	if _, err := root.Find(nil, valid); !errors.Is(err, ErrInvalid) {
		t.Fatal("nil context", err)
	}
}

func TestFindSafeRepresentationsAndEncodedBudget(t *testing.T) {
	for _, value := range []any{
		FindRequest{"private-dir", "secret-pattern", "name", 1},
		FindResult{"private-dir", []string{"private-result"}, true},
	} {
		for _, format := range []string{"%v", "%+v", "%#v", "%q"} {
			if output := fmt.Sprintf(format, value); strings.Contains(output, "private") || strings.Contains(output, "secret") {
				t.Fatal("unsafe formatting")
			}
		}
		encoded, err := json.Marshal(struct{ Value any }{value})
		if err != nil || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "secret") {
			t.Fatal("unsafe JSON", err)
		}
		var log strings.Builder
		slog.New(slog.NewJSONHandler(&log, nil)).Info("find", "value", value)
		if strings.Contains(log.String(), "private") || strings.Contains(log.String(), "secret") {
			t.Fatal("unsafe structured logging")
		}
	}
	// The real projection, not the redacting MarshalJSON method or the raw path
	// byte count, decides the cap. '<' occupies six bytes after JSON escaping.
	empty := FindResult{Path: ".", Paths: []string{""}}
	projection := struct {
		Path      string   `json:"path"`
		Paths     []string `json:"paths"`
		Truncated bool     `json:"truncated"`
	}{empty.Path, empty.Paths, false}
	encoded, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	empty.Paths[0] = strings.Repeat("a", MaxResultBytes-len(encoded))
	if err := findBudget(empty); err != nil {
		t.Fatal("exact encoded limit rejected", err)
	}
	empty.Paths[0] += "a"
	if !errors.Is(findBudget(empty), ErrLimit) {
		t.Fatal("encoded limit + 1 accepted")
	}
	empty.Paths[0] = strings.Repeat("<", MaxResultBytes/6)
	if !errors.Is(findBudget(empty), ErrLimit) {
		t.Fatal("JSON escaping not counted")
	}
}
