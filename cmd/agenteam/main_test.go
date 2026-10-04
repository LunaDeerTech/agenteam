package main

import (
	"bytes"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func TestInformationAndRepairArgumentsDoNotReadUnselectedInputs(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--version"}, {"--repair-migration", "private-sentinel", "--expected-checksum", "private-sentinel"}, {"--repair-migration", "1", "--repair-migration", "2"}} {
		var out, logs bytes.Buffer
		code := execute(args, func(string) (string, bool) { t.Fatal("argument-only command read configuration"); return "", false }, nil, &out, &logs, nil)
		if args[0] == "--help" || args[0] == "--version" {
			if code != 0 {
				t.Fatal("informational command failed")
			}
		} else if code != 2 {
			t.Fatal("bad repair arguments not rejected")
		}
		if strings.Contains(out.String()+logs.String(), "private-sentinel") {
			t.Fatal("CLI argument leaked")
		}
	}
}
func TestCheckConfigAndUnsupportedCompiledRepair(t *testing.T) {
	lookup := func(key string) (string, bool) {
		if value, ok := objectfixture.ConfigOnlyValues()[key]; ok {
			return value, true
		}
		switch key {
		case config.Prefix + "SECRET_KEYRING":
			return `{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`, true
		case config.Prefix + "CURSOR_KEYRING":
			return `{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`, true
		case config.Prefix + "DATABASE_URL":
			return "postgresql://check:private-sentinel@127.0.0.1:1/config_only", true
		case config.Prefix + "DATABASE_TLS_MODE":
			return "disable", true
		}
		return "", false
	}
	var out, logs bytes.Buffer
	if execute([]string{"--check-config"}, lookup, nil, &out, &logs, nil) != 0 || !strings.Contains(out.String(), `"scope":"d05"`) {
		t.Fatal("pure D05 configuration check failed")
	}
	source, err := postgres.EmbeddedSource()
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	logs.Reset()
	if execute([]string{"--repair-migration", "1", "--expected-checksum", string(source.Manifest()[0].Checksum)}, lookup, nil, &out, &logs, nil) != 1 {
		t.Fatal("transactional migration repair did not reject")
	}
	if !strings.Contains(logs.String(), "MIGRATION_REPAIR_UNSUPPORTED") || out.Len() != 0 || strings.Contains(logs.String(), "private-sentinel") {
		t.Fatal("repair refusal was unsafe or claimed success")
	}
}
