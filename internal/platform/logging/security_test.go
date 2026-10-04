package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestSecurityLoggingOnlyFixedProjection(t *testing.T) {
	var b bytes.Buffer
	l, err := New(Central, slog.LevelInfo, &b)
	if err != nil {
		t.Fatal(err)
	}
	l.Security(CursorInitializing)
	l.Security(AuditInitializing)
	l.Security(SecurityInitialized)
	l.Security(OutboxInitializing)
	l.Security(OutboxAvailable)
	l.Security(OutboxUnavailable)
	l.Security(SecurityPhase("private-key-canary"))
	l.InvalidConfig("AGENTEAM_CENTRAL_CURSOR_KEYRING", "private-key-canary")
	for _, phase := range []string{"outbox_initializing", "outbox_available", "outbox_unavailable"} {
		if !strings.Contains(b.String(), `"phase":"`+phase+`"`) {
			t.Fatal("missing fixed Outbox security phase")
		}
	}
	if strings.Contains(b.String(), "private-key-canary") || !strings.Contains(b.String(), `"phase":"failed"`) || !strings.Contains(b.String(), `"field":"AGENTEAM_CENTRAL_CURSOR_KEYRING"`) {
		t.Fatal("unsafe security diagnostics")
	}
}
