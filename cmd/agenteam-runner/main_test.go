package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/runner/config"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func cliEnvironment(t *testing.T) (config.LookupEnv, []string, string) {
	t.Helper()
	directory, err := os.MkdirTemp(".", ".cli-identity-")
	if err != nil {
		t.Fatal(err)
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(directory, "identity.json")
	id, _ := p.NewID()
	pending, err := identity.NewPending(identity.Configuration{CentralURL: "https://central.example", RunnerID: id, RootPath: "/runner"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := identity.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Save(pending); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path + ".lock"); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{config.Prefix + "IDENTITY_FILE": path}
	return func(k string) (string, bool) { v, ok := values[k]; return v, ok }, []string{config.Prefix + "IDENTITY_FILE=" + path}, path
}
func TestRunnerD15OfflineCLI(t *testing.T) {
	lookup, env, path := cliEnvironment(t)
	for _, arg := range []string{"--help", "--version", "--check-config"} {
		var out, log bytes.Buffer
		code := executeInput([]string{arg}, lookup, env, io.NopCloser(strings.NewReader("TOKEN_CANARY")), &out, &log, nil)
		if code != 0 || log.Len() != 0 || strings.Contains(out.String(), "CANARY") {
			t.Fatal("offline CLI failed or read token", arg, code)
		}
		if arg == "--check-config" {
			var receipt map[string]any
			if json.Unmarshal(out.Bytes(), &receipt) != nil || receipt["scope"] != "d15" || receipt["valid"] != true || receipt["connected"] != false || receipt["authenticated"] != false || receipt["ready"] != false {
				t.Fatal("offline scope advertised authentication")
			}
		}
		if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
			t.Fatal("offline CLI created identity ownership")
		}
	}
}
func TestRunnerCLIRejectsPlaintextTokenAndMissingIdentity(t *testing.T) {
	for _, args := range [][]string{{"--enroll", "TOKEN_CANARY"}, {"--enroll=TOKEN_CANARY"}, {"--enroll", "--check-config"}, nil, {"--check-config"}} {
		var out, log bytes.Buffer
		code := executeInput(args, func(string) (string, bool) { return "", false }, nil, io.NopCloser(strings.NewReader("TOKEN_CANARY")), &out, &log, nil)
		if code != 2 || strings.Contains(out.String()+log.String(), "TOKEN_CANARY") || strings.Contains(log.String(), `"event":"runner_connection"`) {
			t.Fatal("unconfigured/plaintext argument started Runner", code)
		}
	}
}
