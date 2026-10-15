package mcphttp

import (
	"encoding/json"
	"net/http"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type configInput struct {
	Name           string                  `json:"name"`
	Description    string                  `json:"description"`
	Endpoint       string                  `json:"endpoint"`
	Authentication protocol.Authentication `json:"authentication"`
}
type configWrite struct {
	ExpectedVersion f.Progress  `json:"expected_version"`
	Input           configInput `json:"input"`
}
type versionInput struct {
	ExpectedVersion f.Version `json:"expected_version"`
}
type connectInput struct {
	ConfigID        mcp.ConfigID `json:"config_id"`
	CredentialID    string       `json:"credential_id,omitempty"`
	ExpectedVersion f.Progress   `json:"expected_version"`
}
type lookupInput struct {
	Command string `json:"command"`
}

func (v configInput) domain() (mcp.ConfigInput, error) {
	if !implementedAuthentication(v.Authentication) {
		return mcp.ConfigInput{}, fault(f.CapabilityUnsupported)
	}
	return mcp.ConfigInput{Name: v.Name, Description: v.Description, Endpoint: v.Endpoint, Authentication: v.Authentication}, nil
}
func implementedAuthentication(a protocol.Authentication) bool {
	if a.Validate() != nil {
		return false
	}
	switch a.Scheme {
	case "none", "bearer", "basic", "custom_header", "oauth":
		return true
	}
	return false
}

// Runtime binding and lease identities remain internal. Browser requests never
// carry a remote invocation, a Secret material value or an execution grant.
type connectionWire struct {
	ID                mcp.ConnectionID `json:"id"`
	ProjectID         id.ProjectID     `json:"project_id"`
	ConfigID          mcp.ConfigID     `json:"config_id"`
	Version           f.Version        `json:"version"`
	ConfigRevision    f.Version        `json:"config_revision"`
	State             string           `json:"state"`
	Enabled           bool             `json:"enabled"`
	CredentialID      string           `json:"credential_id,omitempty"`
	ProtocolRevision  string           `json:"protocol_revision,omitempty"`
	ToolsState        string           `json:"tools_state"`
	ResourcesState    string           `json:"resources_state"`
	ToolsRevision     f.Progress       `json:"tools_revision"`
	ResourcesRevision f.Progress       `json:"resources_revision"`
	LastErrorCode     string           `json:"last_error_code,omitempty"`
}

func projectConnection(v mcp.Connection) connectionWire {
	return connectionWire{v.ID, v.ProjectID, v.ConfigID, v.Version, v.ConfigRevision, v.State, v.Enabled, v.CredentialID, v.ProtocolRevision, v.ToolsState, v.ResourcesState, v.ToolsRevision, v.ResourcesRevision, v.LastErrorCode}
}

type resourceWire struct {
	ID           string           `json:"id"`
	ConnectionID mcp.ConnectionID `json:"connection_id"`
	Kind         string           `json:"kind"`
	URI          string           `json:"uri"`
	Name         string           `json:"name"`
	Title        string           `json:"title"`
	Description  string           `json:"description"`
	MIMEType     string           `json:"mime_type"`
	ByteSize     *f.Progress      `json:"byte_size,omitempty"`
	Revision     f.Version        `json:"revision"`
}

type toolWire struct {
	ToolID         id.ToolID     `json:"tool_id"`
	SpecRevision   f.Version     `json:"spec_revision"`
	Definition     tc.Definition `json:"definition"`
	ConfigID       string        `json:"config_id"`
	ConnectionID   string        `json:"connection_id"`
	RemoteName     string        `json:"remote_name"`
	DefaultEnabled bool          `json:"default_enabled"`
	Active         bool          `json:"active"`
}

func projectTool(v tc.MCPRegistrationView) toolWire {
	return toolWire{v.SpecRef.ToolID, v.SpecRef.SpecRevision, v.Definition, v.Binding.ConfigID, v.Binding.ConnectionID, v.Binding.RemoteToolName, v.DefaultEnabled, v.Active}
}

func projectResource(v mcp.ResourceEntry) resourceWire {
	out := resourceWire{ID: v.ID, ConnectionID: v.ConnectionID, Kind: v.Kind, URI: v.URI, Name: v.Name, Title: v.Title, Description: v.Description, MIMEType: v.MIMEType, Revision: v.Revision}
	if v.ByteSize != nil {
		size := f.Progress(*v.ByteSize)
		out.ByteSize = &size
	}
	return out
}

type commandWire struct {
	Config     *mcp.Config             `json:"config,omitempty"`
	Connection *connectionWire         `json:"connection,omitempty"`
	Deleted    bool                    `json:"deleted,omitempty"`
	OAuth      *mcp.OAuthAuthorization `json:"oauth,omitempty"`
}

func projectCommand(v mcp.CommandResult) commandWire {
	out := commandWire{Config: v.Config, Deleted: v.Deleted, OAuth: v.OAuth}
	if v.Connection != nil {
		c := projectConnection(*v.Connection)
		out.Connection = &c
	}
	return out
}
func encode(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fault(f.InvalidState)
	}
	if len(raw) > 8<<20 {
		return nil, fault(f.PayloadTooLarge)
	}
	return raw, nil
}
func decode(w http.ResponseWriter, r *http.Request, out any) error {
	return httpapi.DecodeJSON(w, r, out, 32<<10)
}
func commandKey(r *http.Request) (f.IdempotencyKey, error) {
	if len(r.Header.Values("Idempotency-Key")) != 1 {
		return "", fault(f.InvalidArgument)
	}
	key, err := f.ParseIdempotencyKey(r.Header.Get("Idempotency-Key"))
	if err != nil {
		return "", fault(f.InvalidArgument)
	}
	return key, nil
}
func command(actor id.Actor, scope id.Scope, name string, key f.IdempotencyKey, version f.Version) (mcp.Command, error) {
	identity, err := mcp.CommandIdentity(actor, scope, name, key)
	if err != nil {
		return mcp.Command{}, err
	}
	return mcp.Command{Actor: actor, Scope: scope, Identity: identity, ExpectedVersion: version}, nil
}
func fault(code f.Code) error { return f.NewFault(code, f.NotStarted) }
