//go:build integration

package account_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func TestProjectModelsWebPrivateInput(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "input.json")
	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if raw, err := projectModelsWebReadPrivate(path, 64); err != nil || !bytes.Equal(raw, []byte(`{"ok":true}`)) {
		t.Fatal("regular private input rejected")
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(directory, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{link, fifo, directory} {
		if raw, err := projectModelsWebReadPrivate(entry, 64); err == nil || len(raw) != 0 {
			t.Fatal("non-regular private input admitted")
		}
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := projectModelsWebReadPrivate(path, 64); err == nil {
		t.Fatal("publicly readable private input admitted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := projectModelsWebReadPrivate(path, 2); err == nil {
		t.Fatal("oversized private input admitted")
	}
}

func TestProjectModelsWebPrivateAtomicReplacement(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "input.json")
	a, b := []byte(`{"phase":"first"}`), []byte(`{"phase":"other"}`)
	if err := os.WriteFile(path, a, 0600); err != nil {
		t.Fatal(err)
	}
	var worker sync.WaitGroup
	worker.Add(1)
	errors := make(chan error, 1)
	go func() {
		defer worker.Done()
		for n := 0; n < 300; n++ {
			value := a
			if n%2 == 0 {
				value = b
			}
			temp := filepath.Join(directory, "next")
			if err := os.WriteFile(temp, value, 0600); err != nil {
				errors <- err
				return
			}
			if err := os.Rename(temp, path); err != nil {
				errors <- err
				return
			}
		}
	}()
	for n := 0; n < 300; n++ {
		raw, err := projectModelsWebReadPrivate(path, 64)
		if err != nil || !bytes.Equal(raw, a) && !bytes.Equal(raw, b) {
			t.Error("atomic publication produced missing, partial, or rejected input")
			break
		}
	}
	worker.Wait()
	select {
	case err := <-errors:
		t.Fatal(err)
	default:
	}
}

func TestProjectModelsWebStrictJSON(t *testing.T) {
	for _, raw := range []string{`{"x":null}`, `{"x":"\ud83d\ude00"}`, `{"x":"\\ud800"}`} {
		if _, err := projectModelsWebObject([]byte(raw), "x"); err != nil {
			t.Fatal("legal closed JSON rejected")
		}
	}
	for _, raw := range []string{
		`{"x":1,"x":2}`, `{"x":{"nested":1,"nested":2}}`,
		`{"x":1} {}`, `{"x":1,"extra":2}`, `null`, `[]`,
		`{"x":"\ud800"}`, `{"x":"\udc00"}`, `{"x":"\ud800\u0041"}`,
		"{\"x\":\"\xff\"}",
	} {
		if _, err := projectModelsWebObject([]byte(raw), "x"); err == nil {
			t.Fatal("malformed or open JSON admitted")
		}
	}
}

func TestProjectModelsWebIPCEnvelope(t *testing.T) {
	input := strings.Repeat("a", 64)
	makeRaw := func(action string, args any, sequence int) []byte {
		raw, err := json.Marshal(map[string]any{"protocol": projectModelsWebProtocol, "input_hash": input, "sequence": sequence, "action": action, "args": args})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if _, code := decodeProjectModelsWebIPC(makeRaw("counts", map[string]any{}, 1), input, 1); code != "" {
		t.Fatal("valid counts request rejected")
	}
	for _, test := range []struct {
		action   string
		args     any
		sequence int
		code     string
	}{
		{"counts", map[string]any{}, 2, "invalid_sequence"},
		{"unknown", map[string]any{}, 1, "invalid_action"},
		{"counts", nil, 1, "invalid_arguments"},
		{"counts", map[string]any{"sql": "forbidden"}, 1, "invalid_arguments"},
		{"release", map[string]any{"arm_id": "a0001"}, 1, "invalid_arguments"},
		{"snapshot", map[string]any{"project": nil}, 1, "invalid_arguments"},
		{"snapshot", map[string]any{"project": 3}, 1, "invalid_arguments"},
		{"snapshot", map[string]any{"project": "unregistered"}, 1, "invalid_arguments"},
		{"reference-fact", map[string]any{"project": "referenced", "state": "invalid"}, 1, "invalid_arguments"},
		{"archive-recovery-project", map[string]any{"project": "main", "expected_version": "1"}, 1, "invalid_arguments"},
		{"archive-recovery-project", map[string]any{"project": "config_recovery", "expected_version": "9223372036854775808"}, 1, "invalid_arguments"},
		{"release", map[string]any{"arm_id": "a0000", "request_token": "r000001"}, 1, "invalid_arguments"},
	} {
		if _, code := decodeProjectModelsWebIPC(makeRaw(test.action, test.args, test.sequence), input, 1); code != test.code {
			t.Fatalf("invalid request classification: got %s want %s", code, test.code)
		}
	}
}

func TestProjectModelsWebRegistryAdmission(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../docs/development/work-items/d27-project-owner-model-settings-ui-endpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	operations, err := projectModelsWebLoadOperations(raw)
	if err != nil {
		t.Fatal(err)
	}
	provider := "01900000-0000-7000-8000-000000000001"
	registry := projectModelsWebRegistry{
		Operations: operations,
		Projects:   map[string]string{"main": "01900000-0000-7000-8000-000000000002"},
		Targets:    map[string]map[string]map[string]bool{"main": {"provider": {provider: true}}},
		Cursors:    map[string]map[string]map[string]bool{"main": {"listProjectModelProviders": {"registered_cursor": true}}},
	}
	arm := func(operation string, target, query any, effect string) projectModelsWebIPC {
		raw, err := json.Marshal(map[string]any{"operation": operation, "project": "main", "target_id": target, "query": query, "effect": effect})
		if err != nil {
			t.Fatal(err)
		}
		return projectModelsWebIPC{Action: "arm", Args: raw}
	}
	for _, request := range []projectModelsWebIPC{
		arm("listProjectModelProviders", nil, "limit=25", "before_dispatch_hold"),
		arm("listProjectModelProviders", nil, "cursor=registered_cursor&limit=25", "after_complete_hold"),
		arm("deleteProjectModelProvider", provider, nil, "after_complete_disconnect"),
	} {
		if code := registry.admit(request); code != "" {
			t.Fatalf("registered endpoint rejected: %s", code)
		}
	}
	for _, request := range []projectModelsWebIPC{
		arm("listProjectModels", nil, "provider_id="+provider, "before_dispatch_hold"),
		arm("listProjectModels", nil, "limit=025", "before_dispatch_hold"),
		arm("listProjectModels", nil, "limit=25&limit=50", "before_dispatch_hold"),
		arm("listProjectModels", nil, "cursor=unregistered", "before_dispatch_hold"),
		arm("listProjectModelProviders", nil, "limit=25&cursor=registered_cursor", "before_dispatch_hold"),
		arm("getProjectModel", provider, nil, "after_complete_hold"),
		arm("deleteProjectModelProvider", provider, nil, "before_dispatch_hold"),
		arm("lookupProjectModelConfiguration", nil, nil, "after_complete_disconnect"),
		arm("getCurrentSession", nil, nil, "before_dispatch_hold"),
	} {
		if code := registry.admit(request); code == "" {
			t.Fatal("unregistered target, query, or effect admitted")
		}
	}
}
