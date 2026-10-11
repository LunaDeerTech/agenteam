//go:build linux

package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
	"github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestHandleWriteReadAndIsolation(t *testing.T) {
	base, borrowed, manager := fixture(t)
	first := identity()
	second, third := first, first
	second.AgentID = runnerprotocol.ID("01900000-0000-7000-8000-000000000003")
	third.ProjectID = runnerprotocol.ID("01900000-0000-7000-8000-000000000004")
	identities := []Identity{first, second, third}
	handles := make([]*Handle, len(identities))
	for index, id := range identities {
		handles[index] = ensure(t, manager, id)
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	for index, h := range handles {
		got, err := h.Write(context.Background(), filesystem.WriteRequest{Path: "proof.txt", Data: []byte{byte('a' + index)}, Overwrite: false})
		if err != nil || got != (filesystem.WriteResult{State: filesystem.WriteCommitted, BytesWritten: 1, Durable: true}) {
			t.Fatal("managed Write", got, err)
		}
	}
	for index, h := range handles {
		got, err := read(h)
		if err != nil || got.Data != string([]byte{byte('a' + index)}) {
			t.Fatal("managed read/isolation", err)
		}
		info, err := os.Stat(filepath.Join(physical(base, identities[index]), "proof.txt"))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("managed mode", err)
		}
	}
	if got, err := handles[0].Write(context.Background(), filesystem.WriteRequest{Path: "../escape", Data: []byte("bad")}); !errors.Is(err, filesystem.ErrInvalid) || got.State != filesystem.WriteNotCommitted {
		t.Fatal("managed traversal", err)
	}
	handles[0].Stop()
	if _, err := handles[0].Write(context.Background(), filesystem.WriteRequest{Path: "new"}); !errors.Is(err, ErrClosed) {
		t.Fatal("stopped Handle accepted write", err)
	}
	if _, err := handles[1].Write(context.Background(), filesystem.WriteRequest{Path: "new"}); err != nil {
		t.Fatal("independent Handle stopped", err)
	}
}

type controlledHandleWriteRoot struct {
	readerRoot
	call func(context.Context, filesystem.WriteRequest) (filesystem.WriteResult, error)
}

func (r controlledHandleWriteRoot) Write(ctx context.Context, q filesystem.WriteRequest) (filesystem.WriteResult, error) {
	return r.call(ctx, q)
}

func TestHandleWriteStopJoinsOriginalRootAndKeepsResult(t *testing.T) {
	for _, phase := range []string{"before-write", "after-write"} {
		t.Run(phase, func(t *testing.T) {
			_, _, manager := fixture(t)
			h := ensure(t, manager, identity())
			actual := h.root.(*filesystem.Root)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unpark := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unpark)
			h.root = controlledHandleWriteRoot{actual, func(ctx context.Context, q filesystem.WriteRequest) (filesystem.WriteResult, error) {
				if phase == "before-write" {
					close(entered)
					<-release
				}
				got, err := actual.Write(ctx, q)
				if phase == "after-write" {
					close(entered)
					<-release
				}
				return got, err
			}}
			done := make(chan error, 1)
			go func() {
				got, err := h.Write(context.Background(), filesystem.WriteRequest{Path: "proof.txt", Data: []byte("complete")})
				if phase == "after-write" && got != (filesystem.WriteResult{State: filesystem.WriteCommitted, BytesWritten: 8, Durable: true}) {
					err = errors.New("actual committed result lost")
				}
				if phase == "before-write" && got.State != filesystem.WriteNotCommitted {
					err = errors.New("stopped write reported publication")
				}
				done <- err
			}()
			await(t, entered)
			manager.Stop()
			deadline(t, manager.Drain)
			manager.mu.Lock()
			active, registered := len(manager.active), len(manager.handles)
			manager.mu.Unlock()
			if active != 1 || registered != 1 {
				t.Error("managed original call retired early")
			}
			unpark()
			if err := receive(t, done); !errors.Is(err, context.Canceled) {
				t.Fatal("original return", err)
			}
			if err := manager.Drain(context.Background()); err != nil {
				t.Fatal("actual manager Drain", err)
			}
		})
	}
}
