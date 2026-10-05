package app

import (
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/tests/testsupport/accountenv"
)

var accountTestInputs = struct {
	sync.Mutex
	values map[testing.TB]map[string]*accountenv.Environment
}{values: make(map[testing.TB]map[string]*accountenv.Environment)}

// Reusing one test database means restarting the same configured deployment:
// retain its exact key material and recovery path, not merely the same kid.
func accountTestEnvironment(t testing.TB, identity string) *accountenv.Environment {
	t.Helper()
	accountTestInputs.Lock()
	defer accountTestInputs.Unlock()
	owned := accountTestInputs.values[t]
	if owned == nil {
		owned = make(map[string]*accountenv.Environment)
		accountTestInputs.values[t] = owned
		t.Cleanup(func() {
			accountTestInputs.Lock()
			delete(accountTestInputs.values, t)
			accountTestInputs.Unlock()
		})
	}
	if owned[identity] == nil {
		owned[identity] = accountenv.New(t)
	}
	return owned[identity]
}
