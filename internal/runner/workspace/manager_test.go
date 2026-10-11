package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func identity() Identity {
	return Identity{"01900000-0000-7000-8000-000000000001", "01900000-0000-7000-8000-000000000002", "source"}
}
func TestIdentityAndSafeProjection(t *testing.T) {
	for _, name := range []string{"source", "repo-1", "mac_debug", "A.b", strings.Repeat("a", 255), "COM0", "LPT0", "CONSOLE"} {
		id := identity()
		id.Name = name
		if !id.valid() {
			t.Errorf("valid name refused")
		}
	}
	for _, name := range []string{"", ".", "..", ".hidden", "a.", "a/b", "a\\b", "/tmp", "C:source", "a\x00b", "a b", "中文", strings.Repeat("a", 256), "con", "nul.txt", "COM9.log", "LpT1", " PRN"} {
		id := identity()
		id.Name = name
		if id.valid() {
			t.Errorf("invalid name accepted")
		}
	}
	for _, bad := range []string{"", "01900000-0000-4000-8000-000000000001", "01900000-0000-7000-c000-000000000001", "01900000-0000-7000-8000-00000000000A", "../project"} {
		id := identity()
		id.ProjectID = runnerprotocol.ID(bad)
		if id.valid() {
			t.Error("invalid project accepted")
		}
		id = identity()
		id.AgentID = runnerprotocol.ID(bad)
		if id.valid() {
			t.Error("invalid agent accepted")
		}
	}
	id := identity()
	id.Name = "DO_NOT_LOG_THIS"
	for _, value := range []any{id, &Manager{}, &Handle{identity: id}} {
		b, _ := json.Marshal(value)
		var log bytes.Buffer
		slog.New(slog.NewJSONHandler(&log, nil)).Info("projection", "value", value)
		output := fmt.Sprintf("%v %+v %#v", value, value, value) + string(b) + log.String()
		if strings.Contains(output, id.Name) || strings.Contains(output, string(id.ProjectID)) {
			t.Fatal("identity leaked in default projection")
		}
	}
}
func TestZeroLifecycle(t *testing.T) {
	for _, m := range []*Manager{nil, {}} {
		m.Stop()
		if !errors.Is(m.Drain(context.Background()), ErrClosed) {
			t.Fatal("zero manager drain")
		}
		if h, err := m.Ensure(context.Background(), identity()); h != nil || !errors.Is(err, ErrClosed) {
			t.Fatal("zero manager ensure")
		}
	}
	for _, h := range []*Handle{nil, {}} {
		h.Stop()
		if !errors.Is(h.Drain(context.Background()), ErrClosed) {
			t.Fatal("zero handle drain")
		}
	}
	if !errors.Is((*Manager)(nil).Drain(nil), ErrInvalid) {
		t.Fatal("nil context")
	}
}
