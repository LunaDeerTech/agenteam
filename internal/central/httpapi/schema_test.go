package httpapi

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestSharedSchemaMatchesImplementedBoundary(t *testing.T) {
	body, err := os.ReadFile("../../../api/openapi/common.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		OpenAPI    string                     `json:"openapi"`
		Paths      map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	if document.OpenAPI != "3.1.0" || document.Paths == nil || len(document.Paths) != 0 {
		t.Fatal("schema contains unimplemented routes or wrong version")
	}
	schemas := document.Components.Schemas
	var problem struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Examples   []Problem                  `json:"examples"`
	}
	if err := json.Unmarshal(schemas["Problem"], &problem); err != nil {
		t.Fatal(err)
	}
	var codeSchema struct {
		Enum []string `json:"enum"`
	}
	if err := json.Unmarshal(problem.Properties["code"], &codeSchema); err != nil {
		t.Fatal(err)
	}
	codes := codeSchema.Enum
	if len(codes) != len(problemKinds) {
		t.Fatal("schema code list drift")
	}
	seen := make(map[foundation.Code]bool)
	for _, code := range codes {
		c := foundation.Code(code)
		if !c.Known() || seen[c] {
			t.Fatalf("unknown/duplicate code %s", code)
		}
		if _, ok := problemKinds[c]; !ok {
			t.Fatalf("code has no HTTP mapping: %s", code)
		}
		seen[c] = true
	}
	for _, example := range problem.Examples {
		if example.Status != problemKinds[example.Code].status || example.RequestID.Validate() != nil || example.CommitState.Safe() != example.CommitState {
			t.Fatal("Problem example disagrees with runtime")
		}
	}
	for _, name := range []string{"ID", "Instant", "InstantInput", "PositiveInt64String", "NonnegativeInt64String", "Digest", "IdempotencyKey"} {
		var schema struct {
			Pattern  string   `json:"pattern"`
			Examples []string `json:"examples"`
		}
		if err := json.Unmarshal(schemas[name], &schema); err != nil {
			t.Fatal(err)
		}
		pattern, err := regexp.Compile(schema.Pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, example := range schema.Examples {
			if !pattern.MatchString(example) {
				t.Fatalf("schema %s rejects its example", name)
			}
			var err error
			switch name {
			case "ID":
				_, err = foundation.ParseID[foundation.Request](example)
			case "Instant", "InstantInput":
				_, err = foundation.ParseInstant(example)
			case "PositiveInt64String":
				_, err = foundation.ParseVersion(example)
			case "NonnegativeInt64String":
				_, err = foundation.ParseProgress(example)
			case "Digest":
				_, err = foundation.ParseDigest(example)
			case "IdempotencyKey":
				_, err = foundation.ParseIdempotencyKey(example)
			}
			if err != nil {
				t.Fatalf("Go rejects schema example for %s: %v", name, err)
			}
		}
		if strings.Contains(name, "Int64String") {
			for _, bad := range []string{"", "-1", "01", "+1", "9223372036854775808", "9999999999999999999", "18446744073709551615", "1.0"} {
				if pattern.MatchString(bad) {
					t.Fatalf("schema %s accepts out-of-range/noncanonical %s", name, bad)
				}
			}
			for _, valid := range []string{"1", "9", "99", "9007199254740993", "9223372036854775806", "9223372036854775807"} {
				if !pattern.MatchString(valid) {
					t.Fatalf("schema %s rejects legal integer %s", name, valid)
				}
			}
			if pattern.MatchString("0") != (name == "NonnegativeInt64String") {
				t.Fatal("schema zero boundary drift")
			}
		}
	}
	var page struct {
		Examples []json.RawMessage `json:"examples"`
	}
	if err := json.Unmarshal(schemas["PageRequest"], &page); err != nil {
		t.Fatal(err)
	}
	for _, example := range page.Examples {
		var request foundation.PageRequest
		if err := json.Unmarshal(example, &request); err != nil || request.Validate() != nil {
			t.Fatalf("Go rejects page example: %v", err)
		}
	}
}
