package foundation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestTaskBlockerFaultCodesPreserveSafeProjectionAndCause(t *testing.T) {
	codes := []Code{BlockerNotFound, BlockerAlreadyResolved, TaskDependencyCycle}
	wants := []string{"BLOCKER_NOT_FOUND", "BLOCKER_ALREADY_RESOLVED", "TASK_DEPENDENCY_CYCLE"}
	cause := errors.New("private-task-sql-credential")
	for n, code := range codes {
		if string(code) != wants[n] || !code.Known() || code.Safe() != code {
			t.Fatal("task code not preserved")
		}
		fault := NewFault(code, NotCommitted).WithCause(cause)
		if !errors.Is(fault, cause) || fault.Error() != wants[n] {
			t.Fatal("cause/code lost")
		}
		wire, err := json.Marshal(fault)
		if err != nil || !bytes.Contains(wire, []byte(wants[n])) || bytes.Contains(wire, []byte(cause.Error())) {
			t.Fatal("unsafe or downgraded fault JSON")
		}
		for _, format := range []string{"%v", "%+v", "%#v"} {
			for _, v := range []any{fault, *fault, struct{ Value any }{fault}, struct{ value Fault }{*fault}} {
				if strings.Contains(fmt.Sprintf(format, v), cause.Error()) {
					t.Fatal("cause leaked through fmt")
				}
			}
		}
		var buf bytes.Buffer
		slog.New(slog.NewJSONHandler(&buf, nil)).Info("safe", "fault", fault)
		if strings.Contains(buf.String(), cause.Error()) || !strings.Contains(buf.String(), wants[n]) {
			t.Fatal("slog cause/code")
		}
	}
	for _, code := range []Code{"BLOCKER_LAST_UNRESOLVED", "LAST_BLOCKER", "BLOCKER_UNKNOWN", "blocker_not_found", "private-task-sql-credential"} {
		if code.Known() || code.Safe() != InternalError {
			t.Fatal("unknown task code admitted")
		}
		fault := NewFault(code, Unknown).WithCause(cause)
		wire, err := json.Marshal(fault)
		if err != nil || bytes.Contains(wire, []byte(code)) || !bytes.Contains(wire, []byte(InternalError)) || !errors.Is(fault, cause) {
			t.Fatal("unknown code safety")
		}
	}
}
