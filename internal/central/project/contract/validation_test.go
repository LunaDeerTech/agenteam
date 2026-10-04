package contract

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestNameASCIIAndBoundaries(t *testing.T) {
	for _, name := range []string{"A", "admin", "api", "settings", "_", "-", "...", "UPPER.lower_09", strings.Repeat("A", 64)} {
		normalized, err := NormalizeName(name)
		if err != nil || normalized != strings.ToLower(name) {
			t.Fatalf("valid name %q: %v", name, err)
		}
	}
	for _, name := range []string{"", ".", "..", strings.Repeat("a", 65), " a", "a ", "a/b", "a\\b", "a%2Fb", "café", "Ａ", "a\n", "\xff"} {
		_, err := NormalizeName(name)
		requireCode(t, err, foundation.InvalidArgument)
	}
	for value := 0; value < 256; value++ {
		c := byte(value)
		_, err := NormalizeName("a" + string([]byte{c}))
		allowed := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if (err == nil) != allowed {
			t.Fatalf("ASCII boundary %d", value)
		}
	}
}

func TestDescriptionByteLengthAndControlPreservation(t *testing.T) {
	for _, value := range []string{"", " \t\n body ", strings.Repeat("é", 4096), "\u0085"} {
		if err := ValidateDescription(value); err != nil {
			t.Fatalf("valid description: %v", err)
		}
	}
	for _, value := range []string{strings.Repeat("é", 4096) + "a", "\xff", "\r", "\x00", "\x7f"} {
		requireCode(t, ValidateDescription(value), foundation.InvalidArgument)
	}
	for c := byte(0); c < 32; c++ {
		err := ValidateDescription(string([]byte{c}))
		if (err == nil) != (c == '\n' || c == '\t') {
			t.Fatalf("control %d", c)
		}
	}
}

func TestRouteSingleDecodeAndConfirmation(t *testing.T) {
	path, err := NormalizeProjectPath("ADMIN", "Api_Project")
	if err != nil || path != "admin/api_project" {
		t.Fatalf("bootstrap route: %q %v", path, err)
	}
	for _, path := range []string{"admin/x", "User-1/X.Y"} {
		if _, err := NormalizeConfirmationPath(path); err != nil {
			t.Fatalf("valid route %q", path)
		}
	}
	for _, path := range []string{"/admin/x", "admin/x/", "admin/x/y", " admin/x", "admin/x ", "admin/%2f", "admin/a\\b", "admin/.", "admin/..", "admin/", "/x", "ad/x", "-admin/x", "admin-/x", "ad_min/x", "admİn/x", "admin%2f/x"} {
		if _, err := NormalizeConfirmationPath(path); err == nil {
			t.Fatalf("accepted path %q", path)
		}
	}
}

func TestStrictBodyDecodingRejectsAliasesDuplicatesNullAndUnknown(t *testing.T) {
	valid := `{"project_id":"01960000-0000-7000-8000-000000000001","name":"X"}`
	var request CreateProjectRequest
	if err := json.Unmarshal([]byte(valid), &request); err != nil || request.Description != "" {
		t.Fatalf("omitted description: %v", err)
	}
	for _, raw := range []string{
		`null`, `[]`, `{}`, strings.Replace(valid, `"name":"X"`, `"Name":"X"`, 1), strings.Replace(valid, `"name":"X"`, `"name":"X","name":"Y"`, 1), strings.Replace(valid, `"name":"X"`, `"name":"X","na\u006de":"Y"`, 1), strings.Replace(valid, `"name":"X"`, `"name":null`, 1), strings.Replace(valid, `"name":"X"`, `"name":"X","description":null`, 1), strings.Replace(valid, `"name":"X"`, `"name":"X","owner_user_id":"01960000-0000-7000-8000-000000000002"`, 1), valid + ` {}`, strings.Replace(valid, `"X"`, `"`+string([]byte{255})+`"`, 1),
	} {
		before := request
		if err := json.Unmarshal([]byte(raw), &request); err == nil {
			t.Fatalf("accepted invalid body: %q", raw)
		}
		if request != before {
			t.Fatal("failed decode partially mutated request")
		}
	}
	for _, raw := range []string{`{}`, `{"name":null}`, `{"description":null}`, `{"name":"a","NAME":"b"}`, `{"description":"ok","version":"1"}`, `{"description":"x","description":"y"}`} {
		var patch UpdateProjectRequest
		if json.Unmarshal([]byte(raw), &patch) == nil {
			t.Fatalf("accepted invalid patch: %s", raw)
		}
	}
	for _, raw := range []string{`{"description":""}`, `{"name":"X"}`, `{"description":" \n","name":"X"}`} {
		var patch UpdateProjectRequest
		if err := json.Unmarshal([]byte(raw), &patch); err != nil {
			t.Fatalf("valid patch: %v", err)
		}
	}
}

func TestSafeFaultNeverEchoesUnknownKeys(t *testing.T) {
	var patch UpdateProjectRequest
	err := json.Unmarshal([]byte(`{"private-password":"value"}`), &patch)
	requireCode(t, err, foundation.InvalidArgument)
	raw, _ := json.Marshal(err)
	if strings.Contains(string(raw), "password") || strings.Contains(string(raw), "value") {
		t.Fatalf("unsafe validation diagnostic: %s", raw)
	}
}

func TestEveryClosedEnumStrictEncoding(t *testing.T) {
	cases := []struct {
		name   string
		target any
		good   string
	}{
		{"lifecycle", new(Lifecycle), `"active"`},
		{"safe_reason", new(SafeReason), `"outcome_unknown"`},
		{"creation_state", new(CreationState), `"initializing"`},
		{"creation_result", new(CreationResultState), `"ready"`},
		{"command", new(CommandName), `"retry-lifecycle"`},
		{"lookup", new(LookupState), `"not_observed"`},
		{"gate", new(InitializationGate), `"initialized"`},
		{"action", new(LifecycleAction), `"restore"`},
		{"operation_state", new(OperationState), `"cleaning"`},
		{"phase", new(OperationPhase), `"cleanup"`},
		{"scope", new(ScopeKind), `"meeting"`},
		{"stop", new(StopState), `"stopped"`},
		{"cleanup", new(CleanupState), `"completed"`},
		{"initialization", new(InitializationState), `"pending"`},
		{"changed_field", new(ChangedField), `"description"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc.good), tc.target); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(tc.target)
			if err != nil || string(raw) != tc.good {
				t.Fatal("enum round trip")
			}
			for _, bad := range []string{`null`, `1`, `""`, `"NOT_A_VARIANT"`, `[]`, tc.good + ` {}`} {
				if json.Unmarshal([]byte(bad), tc.target) == nil {
					t.Fatalf("accepted %s", bad)
				}
			}
			raw, err = json.Marshal(tc.target)
			if err != nil || string(raw) != tc.good {
				t.Fatal("failed enum decode mutated target")
			}
		})
	}
}
