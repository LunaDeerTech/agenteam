package mcphttp

import (
	"net/http"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp"
)

func (h *handler) write(w http.ResponseWriter, r *http.Request, actor id.Actor, scope id.Scope, path route, key f.IdempotencyKey) ([]byte, error) {
	if path.kind == "commands" {
		if r.Method != http.MethodPost {
			return nil, fault(f.MethodNotAllowed)
		}
		var input lookupInput
		if err := decode(w, r, &input); err != nil {
			return nil, err
		}
		value, err := h.service.LookupCommand(r.Context(), actor, scope, input.Command, key)
		if err != nil {
			return nil, err
		}
		out := struct {
			State  string       `json:"state"`
			Result *commandWire `json:"result,omitempty"`
		}{State: value.State}
		if value.Result != nil {
			result := projectCommand(*value.Result)
			out.Result = &result
		}
		return encode(out)
	}
	if path.kind == "configs" {
		return h.writeConfig(w, r, actor, scope, path, key)
	}
	if path.kind != "connections" || r.Method != http.MethodPost {
		return nil, fault(f.MethodNotAllowed)
	}
	if path.id == "connect" && path.action == "" {
		var input connectInput
		if err := decode(w, r, &input); err != nil {
			return nil, err
		}
		config, err := h.service.GetConfig(r.Context(), actor, scope, input.ConfigID)
		if err != nil {
			return nil, err
		}
		if !implementedAuthentication(config.Authentication) {
			return nil, fault(f.CapabilityUnsupported)
		}
		c, err := command(actor, scope, "connection.connect", key, f.Version(input.ExpectedVersion))
		if err != nil {
			return nil, err
		}
		value, err := h.service.Connect(r.Context(), c, mcp.ConnectInput{ConfigID: input.ConfigID, CredentialID: input.CredentialID})
		if err != nil {
			return nil, err
		}
		return encode(projectCommand(value))
	}
	connection, err := f.ParseID[mcp.ConnectionIdentity](path.id)
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	switch path.action {
	case "refresh", "enable", "disable", "disconnect":
	default:
		return nil, fault(f.MethodNotAllowed)
	}
	var input versionInput
	if err := decode(w, r, &input); err != nil {
		return nil, err
	}
	c, err := command(actor, scope, "connection."+path.action, key, input.ExpectedVersion)
	if err != nil {
		return nil, err
	}
	var value mcp.CommandResult
	switch path.action {
	case "disable":
		value, err = h.service.Disable(r.Context(), c, connection)
	case "disconnect":
		value, err = h.service.Disconnect(r.Context(), c, connection)
	default:
		project, _ := f.ParseID[id.Project](path.project)
		current, e := h.service.GetConnection(r.Context(), actor, project, connection)
		if e != nil {
			return nil, e
		}
		config, e := h.service.GetConfig(r.Context(), actor, scope, current.ConfigID)
		if e != nil {
			return nil, e
		}
		if !implementedAuthentication(config.Authentication) {
			return nil, fault(f.CapabilityUnsupported)
		}
		connect := mcp.ConnectInput{ConfigID: current.ConfigID, CredentialID: current.CredentialID}
		if path.action == "refresh" {
			value, err = h.service.Refresh(r.Context(), c, connect)
		} else {
			value, err = h.service.Enable(r.Context(), c, connect)
		}
	}
	if err != nil {
		return nil, err
	}
	return encode(projectCommand(value))
}

func (h *handler) writeConfig(w http.ResponseWriter, r *http.Request, actor id.Actor, scope id.Scope, path route, key f.IdempotencyKey) ([]byte, error) {
	var config mcp.ConfigID
	if path.id != "" {
		var err error
		config, err = f.ParseID[mcp.ConfigIdentity](path.id)
		if err != nil {
			return nil, fault(f.InvalidArgument)
		}
	}
	if r.Method == http.MethodDelete && path.id != "" {
		var input versionInput
		if err := decode(w, r, &input); err != nil {
			return nil, err
		}
		c, err := command(actor, scope, "config.delete", key, input.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		value, err := h.service.DeleteConfig(r.Context(), c, config)
		if err != nil {
			return nil, err
		}
		return encode(projectCommand(value))
	}
	create := r.Method == http.MethodPost && path.id == ""
	if !create && (r.Method != http.MethodPut || path.id == "") {
		return nil, fault(f.MethodNotAllowed)
	}
	var input configWrite
	if err := decode(w, r, &input); err != nil {
		return nil, err
	}
	domain, err := input.Input.domain()
	if err != nil {
		return nil, err
	}
	name := "config.update"
	if create {
		name = "config.create"
	}
	c, err := command(actor, scope, name, key, f.Version(input.ExpectedVersion))
	if err != nil {
		return nil, err
	}
	var value mcp.CommandResult
	if create {
		value, err = h.service.CreateConfig(r.Context(), c, domain)
	} else {
		value, err = h.service.UpdateConfig(r.Context(), c, config, domain)
	}
	if err != nil {
		return nil, err
	}
	return encode(projectCommand(value))
}
