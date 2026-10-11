// Package workspace manages local directories below a trusted Runner root.
// It does not authorize Mounts or register Runner protocol operations.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

var (
	ErrInvalid     = errors.New("workspace: invalid request")
	ErrType        = errors.New("workspace: invalid type")
	ErrPermission  = errors.New("workspace: unsafe directory permissions")
	ErrOutside     = errors.New("workspace: outside root")
	ErrConflict    = errors.New("workspace: conflict")
	ErrUnsupported = errors.New("workspace: unsupported")
	ErrUnavailable = errors.New("workspace: unavailable")
	ErrClosed      = errors.New("workspace: closed")
)

// Identity is a local logical mapping, not the protocol workspace_id.
// The caller is responsible for resolving and authorizing the current Mount.
type Identity struct {
	ProjectID runnerprotocol.ID
	AgentID   runnerprotocol.ID
	Name      string
}

var workspaceSegment = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,254}$`)

// This matches the existing Central mount safeWorkspace contract without a
// forbidden Runner -> Central import or a new public wire contract.
func (i Identity) valid() bool {
	if !i.ProjectID.Valid() || !i.AgentID.Valid() || !workspaceSegment.MatchString(i.Name) || strings.HasSuffix(i.Name, ".") {
		return false
	}
	base, _, _ := strings.Cut(strings.ToUpper(i.Name), ".")
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return false
	}
	return !(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9')
}

func (Identity) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "workspace_identity") }
func (Identity) MarshalJSON() ([]byte, error) { return []byte(`"workspace_identity"`), nil }
func (Identity) LogValue() slog.Value         { return slog.StringValue("workspace_identity") }
func (*Manager) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "workspace_manager") }
func (*Manager) MarshalJSON() ([]byte, error) { return []byte(`"workspace_manager"`), nil }
func (*Manager) LogValue() slog.Value         { return slog.StringValue("workspace_manager") }
func (*Handle) Format(w fmt.State, _ rune)    { _, _ = io.WriteString(w, "workspace_handle") }
func (*Handle) MarshalJSON() ([]byte, error)  { return []byte(`"workspace_handle"`), nil }
func (*Handle) LogValue() slog.Value          { return slog.StringValue("workspace_handle") }

func contextError(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		return context.Canceled
	}
	return nil
}
