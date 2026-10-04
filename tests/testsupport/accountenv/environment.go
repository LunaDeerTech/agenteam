// Package accountenv provides explicit, test-owned Account configuration. It
// neither reads deployment environment variables nor contacts infrastructure.
package accountenv

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// Environment retains one key and recovery log across restarts of the same
// test-owned process. New allocates different inputs for each independent test.
// The closure keeps key material and the private path out of ordinary formatting.
type Environment struct{ values func() map[string]string }

func New(t testing.TB) *Environment {
	t.Helper()
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatal("account environment key generation failed")
	}
	defer clear(key[:])
	ring, err := json.Marshal(struct {
		Format int                 `json:"format"`
		Kid    string              `json:"current_kid"`
		Keys   []map[string]string `json:"keys"`
	}{1, "account_test", []map[string]string{{"kid": "account_test", "key_b64": base64.StdEncoding.EncodeToString(key[:])}}})
	if err != nil {
		t.Fatal("account environment key encoding failed")
	}
	directory := t.TempDir()
	if err = os.Chmod(directory, 0700); err != nil {
		t.Fatal("account environment directory setup failed")
	}
	path := filepath.Join(directory, "account-recovery.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("account environment recovery log creation failed")
	}
	if err = file.Close(); err != nil {
		t.Fatal("account environment recovery log close failed")
	}
	encoded := string(ring)
	return &Environment{values: func() map[string]string {
		return map[string]string{
			"AGENTEAM_CENTRAL_ACCOUNT_KEYRING":      encoded,
			"AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG": path,
		}
	}}
}

// Values returns a new map so callers can alter inputs without changing the
// environment retained for a later child-process restart.
func (e *Environment) Values() map[string]string { return e.values() }

func (e *Environment) Environ() []string {
	var result []string
	for key, value := range e.Values() {
		result = append(result, key+"="+value)
	}
	sort.Strings(result)
	return result
}
