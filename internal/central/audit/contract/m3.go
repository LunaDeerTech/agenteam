package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	MemoryRetain            Action       = "memory.retain"
	MemoryDelete            Action       = "memory.delete"
	MemoryProducer          Producer     = "memory"
	MemoryResource          ResourceKind = "memory"
	MemoryNamespaceResource ResourceKind = "memory_namespace"
	MCPConfigCreate         Action       = "mcp.config.create"
	MCPConfigUpdate         Action       = "mcp.config.update"
	MCPConfigDelete         Action       = "mcp.config.delete"
	MCPConnectionConnect    Action       = "mcp.connection.connect"
	MCPConnectionEnable     Action       = "mcp.connection.enable"
	MCPConnectionDisable    Action       = "mcp.connection.disable"
	MCPConnectionDisconnect Action       = "mcp.connection.disconnect"
	MCPDiscoveryRefresh     Action       = "mcp.discovery.refresh"
	MCPProducer             Producer     = "mcp"
	MCPConfigResource       ResourceKind = "mcp_config"
	MCPConnectionResource   ResourceKind = "mcp_connection"
)

func MemoryAction(a Action) bool { return a == MemoryRetain || a == MemoryDelete }
func MCPAction(a Action) bool {
	switch a {
	case MCPConfigCreate, MCPConfigUpdate, MCPConfigDelete, MCPConnectionConnect, MCPConnectionEnable, MCPConnectionDisable, MCPConnectionDisconnect, MCPDiscoveryRefresh:
		return true
	}
	return false
}

// This projection excludes input, extracted text, embedding values and prompts.
type MemoryMetadataFields struct {
	AgentID     string `json:"agent_id"`
	MemoryID    string `json:"memory_id,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
	Gate        string `json:"gate,omitempty"`
	Added       int    `json:"added"`
	Updated     int    `json:"updated"`
	Deleted     int    `json:"deleted"`
	Noop        int    `json:"noop"`
}

func MemoryMetadata(a Action, v MemoryMetadataFields) (Metadata, error) {
	if !MemoryAction(a) || !validID(v.AgentID) || v.Added < 0 || v.Updated < 0 || v.Deleted < 0 || v.Noop < 0 || v.Added > 10000 || v.Updated > 10000 || v.Deleted > 10000 || v.Noop > 10000 {
		return Metadata{}, invalid("metadata")
	}
	if a == MemoryRetain {
		if !validID(v.OperationID) || v.MemoryID != "" || (v.Gate != "allow" && v.Gate != "mask") {
			return Metadata{}, invalid("metadata")
		}
	} else if !validID(v.MemoryID) || v.OperationID != "" || v.Gate != "" || v.Added != 0 || v.Updated != 0 || v.Deleted != 1 || v.Noop != 0 {
		return Metadata{}, invalid("metadata")
	}
	return m3Metadata(a, v)
}
func (m Metadata) MemoryFields() (MemoryMetadataFields, error) {
	var v MemoryMetadataFields
	if m.data == nil || !MemoryAction(m.data().action) || json.Unmarshal([]byte(m.data().raw), &v) != nil {
		return v, invalid("metadata")
	}
	return v, nil
}
func validateMemoryEntry(v EntryFields) error {
	m, e := v.Metadata.MemoryFields()
	if e != nil {
		return e
	}
	a := v.Actor.Details()
	r := v.Resource.Details()
	if v.Scope.Details().Kind != id.ProjectScope || v.Outcome != Success {
		return invalid("entry")
	}
	if v.Action == MemoryRetain {
		if a.Kind != id.AgentRun || a.AgentID != m.AgentID || r.Kind != MemoryNamespaceResource || r.ID != m.AgentID || v.Associations != (Associations{ExecutionID: a.ExecutionID, OperationID: m.OperationID}) {
			return invalid("entry")
		}
	} else if a.Kind != id.Human || r.Kind != MemoryResource || r.ID != m.MemoryID || v.Associations != (Associations{}) {
		return invalid("entry")
	}
	return nil
}

// MCP audit stores identities and counts; remote definitions and endpoints are
// deliberately absent. Failed discovery has one bounded safe code.
type MCPMetadataFields struct {
	ConfigID      string    `json:"config_id"`
	ConnectionID  string    `json:"connection_id,omitempty"`
	Version       f.Version `json:"version"`
	ToolCount     int       `json:"tool_count"`
	ResourceCount int       `json:"resource_count"`
	InvalidCount  int       `json:"invalid_count"`
	ErrorCode     string    `json:"error_code,omitempty"`
}

func MCPMetadata(a Action, v MCPMetadataFields) (Metadata, error) {
	if !MCPAction(a) || !validID(v.ConfigID) || v.Version.Validate() != nil || v.ToolCount < 0 || v.ResourceCount < 0 || v.InvalidCount < 0 || v.ToolCount > 10000 || v.ResourceCount > 10000 || v.InvalidCount > 10000 {
		return Metadata{}, invalid("metadata")
	}
	config := a == MCPConfigCreate || a == MCPConfigUpdate || a == MCPConfigDelete
	if config && v.ConnectionID != "" || !config && !validID(v.ConnectionID) {
		return Metadata{}, invalid("metadata")
	}
	if a != MCPDiscoveryRefresh && v.ErrorCode != "" {
		return Metadata{}, invalid("metadata")
	}
	if v.ErrorCode != "" {
		switch Reason(v.ErrorCode) {
		case InvalidTarget, AddressForbidden, PrivateNotAllowed, PortDenied, HTTPDenied, TLSFailed, DNSFailed, RedirectDenied, PolicyUnavailable, BindingInvalid, ConsumerDenied, CredentialDenied, OriginDenied, ResponseLimit, Timeout, Cancelled, InternalError, StorageUnavailable, IntegrityMismatch:
		default:
			return Metadata{}, invalid("metadata")
		}
	}
	return m3Metadata(a, v)
}
func (m Metadata) MCPFields() (MCPMetadataFields, error) {
	var v MCPMetadataFields
	if m.data == nil || !MCPAction(m.data().action) || json.Unmarshal([]byte(m.data().raw), &v) != nil {
		return v, invalid("metadata")
	}
	return v, nil
}
func validateMCPEntry(v EntryFields) error {
	m, e := v.Metadata.MCPFields()
	if e != nil {
		return e
	}
	r := v.Resource.Details()
	config := v.Action == MCPConfigCreate || v.Action == MCPConfigUpdate || v.Action == MCPConfigDelete
	actor := v.Actor.Details()
	technical := actor.Kind == id.Service && actor.ServiceName == id.MCPRuntime && v.Action == MCPDiscoveryRefresh && v.Scope.Details().Kind == id.ProjectScope && actor.ProjectID == v.Scope.Details().ProjectID
	if actor.Kind != id.Human && !technical || v.Associations != (Associations{}) || v.Outcome != Success && v.Outcome != Failed || v.Outcome == Failed && (v.Action != MCPDiscoveryRefresh || m.ErrorCode == "") || v.Outcome == Success && m.ErrorCode != "" {
		return invalid("entry")
	}
	if config {
		if r.Kind != MCPConfigResource || r.ID != m.ConfigID {
			return invalid("entry")
		}
	} else if v.Scope.Details().Kind != id.ProjectScope || r.Kind != MCPConnectionResource || r.ID != m.ConnectionID {
		return invalid("entry")
	}
	return nil
}
func m3Metadata(a Action, v any) (Metadata, error) {
	raw, e := json.Marshal(v)
	if e != nil || len(raw) > 4096 {
		return Metadata{}, invalid("metadata")
	}
	d := metadataData{action: a, raw: string(raw)}
	return Metadata{data: func() metadataData { return d }}, nil
}
func decodeM3Metadata(a Action, raw []byte) (Metadata, error) {
	allowed := map[string]bool{}
	if MemoryAction(a) {
		for _, k := range []string{"agent_id", "memory_id", "operation_id", "gate", "added", "updated", "deleted", "noop"} {
			allowed[k] = true
		}
	} else {
		for _, k := range []string{"config_id", "connection_id", "version", "tool_count", "resource_count", "invalid_count", "error_code"} {
			allowed[k] = true
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return Metadata{}, invalid("metadata")
	}
	seen := map[string]bool{}
	for d.More() {
		key, e := d.Token()
		k, ok := key.(string)
		if e != nil || !ok || !allowed[k] || seen[k] {
			return Metadata{}, invalid("metadata")
		}
		seen[k] = true
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return Metadata{}, invalid("metadata")
		}
	}
	last, e := d.Token()
	if e != nil || last != json.Delim('}') || d.Decode(new(any)) != io.EOF {
		return Metadata{}, invalid("metadata")
	}
	if MemoryAction(a) {
		for _, k := range []string{"agent_id", "added", "updated", "deleted", "noop"} {
			if !seen[k] {
				return Metadata{}, invalid("metadata")
			}
		}
		var v MemoryMetadataFields
		if json.Unmarshal(raw, &v) != nil {
			return Metadata{}, invalid("metadata")
		}
		return MemoryMetadata(a, v)
	}
	for _, k := range []string{"config_id", "version", "tool_count", "resource_count", "invalid_count"} {
		if !seen[k] {
			return Metadata{}, invalid("metadata")
		}
	}
	var v MCPMetadataFields
	if json.Unmarshal(raw, &v) != nil {
		return Metadata{}, invalid("metadata")
	}
	return MCPMetadata(a, v)
}

// System MCP facts must be proven in the original controller transaction in
// addition to the current administrator gate checked by Audit.
type MCPAuthority interface {
	RequireMCPAuditInTx(context.Context, f.Tx, Entry, AppendKey) error
}
