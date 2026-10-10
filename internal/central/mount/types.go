package mount

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

// These are owned storage facts, not a public MountCreate request or an
// authorization to choose a system Runner. That command/provider is unbound.
type definition struct {
	ID          i.MountID
	Project     i.ProjectID
	Agent       i.AgentID
	Runner      rc.RunnerID
	Name        string
	Description *string
	Workspace   string
	Lifecycle   string
	Version     f.Version
}

var workspaceSegment = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,254}$`)

func safeWorkspace(s string) bool {
	if !workspaceSegment.MatchString(s) || strings.HasSuffix(s, ".") {
		return false
	}
	base, _, _ := strings.Cut(strings.ToUpper(s), ".")
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return false
	}
	return !(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9')
}
func validText(s string, max int) bool {
	return len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func (d definition) valid() bool {
	return d.ID.Validate() == nil && d.Project.Validate() == nil && d.Agent.Validate() == nil && d.Runner.Validate() == nil && d.Version.Validate() == nil &&
		len(d.Name) > 0 && validText(d.Name, 128) && strings.IndexFunc(d.Name, unicode.IsControl) < 0 &&
		(d.Description == nil || validText(*d.Description, 4096)) && safeWorkspace(d.Workspace) &&
		(d.Lifecycle == "active" || d.Lifecycle == "disabled" || d.Lifecycle == "removed")
}
func sameDefinition(a, b definition) bool {
	return a.ID == b.ID && a.Project == b.Project && a.Agent == b.Agent && a.Runner == b.Runner && a.Name == b.Name && a.Workspace == b.Workspace && a.Lifecycle == b.Lifecycle && a.Version == b.Version &&
		(a.Description == nil && b.Description == nil || a.Description != nil && b.Description != nil && *a.Description == *b.Description)
}
