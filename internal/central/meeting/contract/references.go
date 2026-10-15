package contract

import (
	"net/url"
	"strconv"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const MaxReferences = 256

// UpdateReferencesRequest replaces the ordered pinned identities. Inline
// message references and already-frozen Execution inputs are not changed.
type UpdateReferencesRequest struct {
	ProjectID       i.ProjectID   `json:"project_id"`
	MeetingID       MeetingID     `json:"meeting_id"`
	ExpectedVersion f.Version     `json:"expected_version"`
	References      []Reference   `json:"references"`
	Meta            f.CommandMeta `json:"-"`
}

func CanonicalReference(ref Reference) (Reference, error) {
	if !ValidText(ref.ID, 8192) || !ValidText(ref.Label, 1024) || ref.ID == "" {
		return Reference{}, invalid()
	}
	switch ref.Kind {
	case "task", "knowledge", "file":
		if _, err := f.ParseID[struct{}](ref.ID); err != nil {
			return Reference{}, invalid()
		}
	case "link":
		if strings.TrimSpace(ref.ID) != ref.ID {
			return Reference{}, invalid()
		}
		u, err := url.Parse(ref.ID)
		if err != nil || u.Opaque != "" || u.User != nil || u.Host == "" || u.Hostname() == "" {
			return Reference{}, invalid()
		}
		u.Scheme = strings.ToLower(u.Scheme)
		if u.Scheme != "http" && u.Scheme != "https" {
			return Reference{}, invalid()
		}
		host, port := strings.ToLower(u.Hostname()), u.Port()
		if port != "" {
			n, err := strconv.ParseUint(port, 10, 16)
			if err != nil || n == 0 {
				return Reference{}, invalid()
			}
			if u.Scheme == "http" && port == "80" || u.Scheme == "https" && port == "443" {
				port = ""
			}
		}
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		u.Host = host
		if port != "" {
			u.Host += ":" + port
		}
		if u.Path == "" {
			u.Path = "/"
		}
		ref.ID = u.String()
		if len(ref.ID) > 8192 {
			return Reference{}, invalid()
		}
	default:
		return Reference{}, invalid()
	}
	return ref, nil
}

func (r UpdateReferencesRequest) Canonical() (UpdateReferencesRequest, error) {
	if r.ProjectID.Validate() != nil || r.MeetingID.Validate() != nil || r.ExpectedVersion.Validate() != nil || r.Meta.Validate() != nil || r.Meta.ExpectedVersion != nil || r.References == nil || len(r.References) > MaxReferences {
		return UpdateReferencesRequest{}, invalid()
	}
	out := r
	out.References = make([]Reference, len(r.References))
	seen := map[string]bool{}
	for n, ref := range r.References {
		value, err := CanonicalReference(ref)
		if err != nil {
			return UpdateReferencesRequest{}, err
		}
		key := value.Kind + ":" + value.ID
		if seen[key] {
			return UpdateReferencesRequest{}, invalid()
		}
		seen[key] = true
		out.References[n] = value
	}
	return out, nil
}
func (r UpdateReferencesRequest) Validate() error { _, err := r.Canonical(); return err }
