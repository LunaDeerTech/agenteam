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

func TestTaskFaultCodesPreserveSafeProjectionAndCause(t *testing.T) {
	codes := []Code{TaskNotFound, TaskVersionConflict, TaskStateInvalid, TaskAssigneeRequired, TaskSprintInvalid, TaskTerminalImmutable}
	wants := []string{"TASK_NOT_FOUND", "TASK_VERSION_CONFLICT", "TASK_STATE_INVALID", "TASK_ASSIGNEE_REQUIRED", "TASK_SPRINT_INVALID", "TASK_TERMINAL_IMMUTABLE"}
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
	for _, code := range []Code{"TASK_ASSIGNEE_INVALID", "TASK_DELETE_DENIED", "TASK_UNKNOWN", "task_not_found", "private-task-sql-credential"} {
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
