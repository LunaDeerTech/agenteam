package object

import (
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// These controls exercise the pure identity read only. They do not claim a
// flock, a database process registration, or Runtime retirement has occurred.
func TestProcessGuardCurrentIdentityPure(t *testing.T) {
	id, err := f.NewID[oc.Process]()
	if err != nil {
		t.Fatal(err)
	}
	for _, guard := range []*ProcessGuard{nil, {}, {data: func() *processState { return nil }}} {
		if got, err := guard.CurrentProcess(); err == nil || got != (oc.ProcessID{}) {
			t.Fatal("unconstructed guard exposed identity")
		}
	}
	state := &processState{process: id}
	guard := &ProcessGuard{data: func() *processState { return state }}
	if got, err := guard.CurrentProcess(); err == nil || got != (oc.ProcessID{}) {
		t.Fatal("unbound guard exposed identity")
	}
	state.bound, state.service = true, &Service{}
	if got, err := guard.CurrentProcess(); err != nil || got != id {
		t.Fatal("bound guard lost original identity")
	}
	state.closed = true
	if got, err := guard.CurrentProcess(); err == nil || got != (oc.ProcessID{}) {
		t.Fatal("closed guard exposed identity")
	}
}

func TestProcessGuardCurrentIdentityUsesOriginalMutex(t *testing.T) {
	id, err := f.NewID[oc.Process]()
	if err != nil {
		t.Fatal(err)
	}
	state := &processState{process: id, bound: true, service: &Service{}}
	guard := &ProcessGuard{data: func() *processState { return state }}
	var joined sync.WaitGroup
	joined.Add(1)
	go func() {
		defer joined.Done()
		for range 256 {
			got, err := guard.CurrentProcess()
			if err == nil && got != id || err != nil && got != (oc.ProcessID{}) {
				t.Error("identity read returned a partial result")
				return
			}
		}
	}()
	state.mu.Lock()
	state.closed = true
	state.mu.Unlock()
	joined.Wait()
	if got, err := guard.CurrentProcess(); err == nil || got != (oc.ProcessID{}) {
		t.Fatal("close publication was not observed")
	}
}
