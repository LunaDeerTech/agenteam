package process_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/tests/testsupport/accountenv"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
)

var accountInputs = struct {
	sync.Mutex
	values map[*testing.T]map[string]*accountenv.Environment
}{values: make(map[*testing.T]map[string]*accountenv.Environment)}

func accountEnvironment(t *testing.T, identity string) []string {
	t.Helper()
	accountInputs.Lock()
	defer accountInputs.Unlock()
	owned := accountInputs.values[t]
	if owned == nil {
		owned = make(map[string]*accountenv.Environment)
		accountInputs.values[t] = owned
		t.Cleanup(func() { accountInputs.Lock(); delete(accountInputs.values, t); accountInputs.Unlock() })
	}
	if owned[identity] == nil {
		owned[identity] = accountenv.New(t)
	}
	return owned[identity].Environ()
}

func TestAccountProcessConfigCheckDoesNotOpenRecoveryLog(t *testing.T) {
	inputs := accountEnvironment(t, "check")
	parent := filepath.Join(t.TempDir(), "not-created")
	path := filepath.Join(parent, "account-private-SENTINEL.log")
	for i, entry := range inputs {
		if strings.HasPrefix(entry, "AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG=") {
			inputs[i] = "AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG=" + path
		}
	}
	inputs = append(inputs, objectfixture.ConfigOnlyEnvironment()...)
	inputs = append(inputs,
		`AGENTEAM_CENTRAL_KNOWLEDGE_CONFIRMATION_KEYRING={"format":1,"current_kid":"knowledge","keys":[{"kid":"knowledge","key_b64":"gIGCg4SFhoeIiYqLjI2Oj5CRkpOUlZaXmJmam5ydnp8="}]}`,
		`AGENTEAM_CENTRAL_SECRET_KEYRING={"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`,
		`AGENTEAM_CENTRAL_CURSOR_KEYRING={"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`,
		"AGENTEAM_CENTRAL_DATABASE_URL=postgresql://config:config-only@127.0.0.1:1/config_only", "AGENTEAM_CENTRAL_DATABASE_TLS_MODE=disable")
	p := launch(t, "agenteam", []string{"--check-config"}, inputs)
	p.wait(t, 0)
	var result map[string]any
	if json.Unmarshal(p.stdout.Bytes(), &result) != nil || result["valid"] != true || result["ready"] != false {
		t.Fatal("account configuration check failed or claimed ready")
	}
	if _, err := os.Lstat(parent); !os.IsNotExist(err) {
		t.Fatal("pure CLI opened/created the recovery path")
	}
	if strings.Contains(p.stdout.String()+p.stderr.String(), "SENTINEL") {
		t.Fatal("CLI leaked private recovery path")
	}
}
