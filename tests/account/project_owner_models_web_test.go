//go:build integration

package account_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
	} {
		if _, code := decodeProjectModelsWebIPC(makeRaw(test.action, test.args, test.sequence), input, 1); code != test.code {
			t.Fatalf("invalid request classification: got %s want %s", code, test.code)
		}
	}
}
