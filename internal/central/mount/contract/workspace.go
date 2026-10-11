package contract

import (
	"regexp"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// Workspace identifies a stable logical mapping. It is neither a host path nor
// evidence that a physical directory has been created on a Runner.
type Workspace struct{}
type WorkspaceID = f.ID[Workspace]

var workspaceSegment = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,254}$`)

// ValidWorkspaceName accepts the portable, single-segment logical directory
// name. It does not normalize or truncate the supplied bytes.
func ValidWorkspaceName(s string) bool {
	if !workspaceSegment.MatchString(s) || strings.HasSuffix(s, ".") {
		return false
	}
	base, _, _ := strings.Cut(strings.ToUpper(s), ".")
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return false
	}
	return !(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9')
}
