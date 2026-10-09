//go:build integration

package account_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
)

const projectModelsWebProtocol = "project-owner-models.v1"

var projectModelsWebID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var projectModelsWebRequestToken = regexp.MustCompile(`^r[0-9]{6}$`)
var projectModelsWebArmToken = regexp.MustCompile(`^a[0-9]{4}$`)

type projectModelsWebOperation struct {
	Operation string   `json:"operation"`
	Method    string   `json:"method"`
	Path      string   `json:"path"`
	Family    string   `json:"family"`
	Target    *string  `json:"target"`
	Query     string   `json:"query"`
	Effects   []string `json:"effects"`
}

// Endpoint metadata comes from the formal attachment, not a second routing
// protocol maintained inside the fixture. Callers keep this map immutable.
func projectModelsWebLoadOperations(raw []byte) ([]projectModelsWebOperation, error) {
	if _, err := projectModelsWebObject(raw, "status", "protocol", "base", "fields_exact", "operations", "private_session", "ipc_actions", "operation_notes"); err != nil {
		return nil, err
	}
	var input struct {
		Protocol   string            `json:"protocol"`
		Base       string            `json:"base"`
		Fields     []string          `json:"fields_exact"`
		Operations []json.RawMessage `json:"operations"`
	}
	if json.Unmarshal(raw, &input) != nil || input.Protocol != projectModelsWebProtocol || input.Base != "/api/v1/projects/{project_id}" || len(input.Operations) != 17 {
		return nil, errors.New("formal endpoint attachment rejected")
	}
	fields := []string{"operation", "method", "path", "family", "target", "query", "effects"}
	if len(input.Fields) != len(fields) {
		return nil, errors.New("formal endpoint attachment rejected")
	}
	for n := range fields {
		if input.Fields[n] != fields[n] {
			return nil, errors.New("formal endpoint attachment rejected")
		}
	}
	seen := map[string]bool{}
	result := make([]projectModelsWebOperation, 0, 17)
	for _, raw := range input.Operations {
		var operation projectModelsWebOperation
		if _, err := projectModelsWebObject(raw, fields...); err != nil || json.Unmarshal(raw, &operation) != nil || operation.Operation == "" || seen[operation.Operation] {
			return nil, errors.New("formal endpoint attachment rejected")
		}
		seen[operation.Operation] = true
		result = append(result, operation)
	}
	return result, nil
}

type projectModelsWebRegistry struct {
	Operations []projectModelsWebOperation
	Projects   map[string]string
	// IDs are partitioned by exact Project and endpoint target family.
	Targets  map[string]map[string]map[string]bool
	Cursors  map[string]map[string]map[string]bool
	Sessions map[string]bool
}

func projectModelsWebString(raw json.RawMessage) (string, bool) {
	var value string
	err := json.Unmarshal(raw, &value)
	return value, err == nil && string(bytes.TrimSpace(raw)) != "null"
}

func projectModelsWebVersion(raw json.RawMessage) bool {
	value, ok := projectModelsWebString(raw)
	if !ok || value == "" || value[0] < '1' || value[0] > '9' {
		return false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return err == nil && parsed > 0 && strconv.FormatInt(parsed, 10) == value
}

func (registry projectModelsWebRegistry) admit(request projectModelsWebIPC) string {
	var args map[string]json.RawMessage
	if json.Unmarshal(request.Args, &args) != nil {
		return "invalid_arguments"
	}
	get := func(key string) (string, bool) { return projectModelsWebString(args[key]) }
	project, projectString := get("project")
	if raw, exists := args["project"]; exists && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if !projectString {
			return "invalid_arguments"
		}
		if registry.Projects[project] == "" {
			return "unknown_target"
		}
	}
	switch request.Action {
	case "counts":
		return ""
	case "snapshot":
		if !projectString {
			return "invalid_arguments"
		}
	case "rename-reuse":
		if project != "main" {
			return "invalid_arguments"
		}
	case "archive-recovery-project":
		if project != "config_recovery" && project != "credential_recovery" || !projectModelsWebVersion(args["expected_version"]) {
			return "invalid_arguments"
		}
	case "reference-fact":
		state, ok := get("state")
		if project != "referenced" || !ok || state != "present" && state != "absent" {
			return "invalid_arguments"
		}
	case "logout":
		session, ok := get("session_id")
		if !ok || !projectModelsWebID.MatchString(session) {
			return "invalid_arguments"
		}
		if !registry.Sessions[session] {
			return "unknown_target"
		}
	case "release", "control-state":
		arm, ok := get("arm_id")
		if !ok || !projectModelsWebArmToken.MatchString(arm) || arm == "a0000" {
			return "invalid_arguments"
		}
		if request.Action == "release" {
			token, ok := get("request_token")
			if !ok || !projectModelsWebRequestToken.MatchString(token) || token == "r000000" {
				return "invalid_arguments"
			}
		}
	case "arm":
		operation, ok := get("operation")
		effect, effectOK := get("effect")
		if !ok || !effectOK {
			return "invalid_arguments"
		}
		null := func(key string) bool { return bytes.Equal(bytes.TrimSpace(args[key]), []byte("null")) }
		if operation == "getCurrentSession" {
			if !null("project") || !null("target_id") || !null("query") || effect != "before_dispatch_hold" {
				return "invalid_arguments"
			}
			return ""
		}
		var selected *projectModelsWebOperation
		for n := range registry.Operations {
			if registry.Operations[n].Operation == operation {
				selected = &registry.Operations[n]
				break
			}
		}
		if selected == nil || !projectString {
			return "invalid_arguments"
		}
		allowed := false
		for _, candidate := range selected.Effects {
			allowed = allowed || candidate == effect
		}
		if !allowed {
			return "invalid_arguments"
		}
		if selected.Target == nil {
			if !null("target_id") {
				return "invalid_arguments"
			}
		} else {
			target, ok := get("target_id")
			if !ok || !projectModelsWebID.MatchString(target) {
				return "invalid_arguments"
			}
			if !registry.Targets[project][*selected.Target][target] {
				return "unknown_target"
			}
		}
		if selected.Query == "none" {
			if !null("query") {
				return "invalid_arguments"
			}
		} else {
			query, ok := get("query")
			if !ok || len(query) > 32768 {
				return "invalid_arguments"
			}
			values, err := url.ParseQuery(query)
			if err != nil || values.Encode() != query {
				return "invalid_arguments"
			}
			for key, values := range values {
				if len(values) != 1 || values[0] == "" {
					return "invalid_arguments"
				}
				switch key {
				case "limit":
					limit, err := strconv.Atoi(values[0])
					if err != nil || limit < 1 || limit > 100 || strconv.Itoa(limit) != values[0] {
						return "invalid_arguments"
					}
				case "cursor":
					if len(values[0]) > 8192 || strings.ContainsAny(values[0], "\r\n\x00") {
						return "invalid_arguments"
					}
					if !registry.Cursors[project][operation][values[0]] {
						return "unknown_target"
					}
				default:
					return "invalid_arguments"
				}
			}
		}
	default:
		return "invalid_action"
	}
	return ""
}

// Open first, then validate and read that same descriptor. A concurrent atomic
// publication may replace the pathname; it cannot change the descriptor's
// identity. NONBLOCK prevents an unexpected FIFO from stalling before fstat.
func projectModelsWebReadPrivate(path string, limit int64) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() < 1 || info.Size() > limit {
		return nil, errors.New("owned private input rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(raw)) > limit || !utf8.Valid(raw) {
		clear(raw)
		return nil, errors.New("owned private input rejected")
	}
	if err := file.Close(); err != nil {
		clear(raw)
		return nil, errors.New("owned private input close failed")
	}
	return raw, nil
}

// encoding/json alone accepts duplicate keys and replaces malformed strings.
// Walk the complete value before decoding into typed, closed envelopes.
func projectModelsWebJSON(raw []byte) error {
	if !utf8.Valid(raw) {
		return errors.New("owned JSON invalid")
	}
	// JSON permits escapes but not unpaired UTF-16 surrogates in our protocol.
	// Validate escape pairs before encoding/json can replace them with U+FFFD.
	quoted := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) || raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return errors.New("owned JSON escape invalid")
		}
		code, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil || code >= 0xdc00 && code <= 0xdfff {
			return errors.New("owned JSON escape invalid")
		}
		i += 4
		if code >= 0xd800 && code <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return errors.New("owned JSON escape invalid")
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return errors.New("owned JSON escape invalid")
			}
			i += 6
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return errors.New("owned JSON depth exceeded")
		}
		token, err := d.Token()
		if err != nil {
			return errors.New("owned JSON invalid")
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return errors.New("owned JSON duplicate member")
				}
				seen[name] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			if end, err := d.Token(); err != nil || end != json.Delim('}') {
				return errors.New("owned JSON invalid")
			}
		case json.Delim('['):
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			if end, err := d.Token(); err != nil || end != json.Delim(']') {
				return errors.New("owned JSON invalid")
			}
		case json.Delim('}'), json.Delim(']'):
			return errors.New("owned JSON invalid")
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("owned JSON trailing value")
	}
	return nil
}

func projectModelsWebObject(raw []byte, members ...string) (map[string]json.RawMessage, error) {
	if err := projectModelsWebJSON(raw); err != nil {
		return nil, err
	}
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil || out == nil || len(out) != len(members) {
		return nil, errors.New("owned JSON object rejected")
	}
	for _, member := range members {
		if _, ok := out[member]; !ok {
			return nil, errors.New("owned JSON object rejected")
		}
	}
	return out, nil
}

type projectModelsWebIPC struct {
	Protocol  string          `json:"protocol"`
	InputHash string          `json:"input_hash"`
	Sequence  int             `json:"sequence"`
	Action    string          `json:"action"`
	Args      json.RawMessage `json:"args"`
}

func decodeProjectModelsWebIPC(raw []byte, inputHash string, next int) (projectModelsWebIPC, string) {
	var out projectModelsWebIPC
	if len(raw) > 8192 {
		return out, "invalid_envelope"
	}
	if _, err := projectModelsWebObject(raw, "protocol", "input_hash", "sequence", "action", "args"); err != nil || json.Unmarshal(raw, &out) != nil || out.Protocol != projectModelsWebProtocol || out.InputHash != inputHash {
		return out, "invalid_envelope"
	}
	if out.Sequence != next || next < 1 || next > 128 {
		return out, "invalid_sequence"
	}
	var members []string
	switch out.Action {
	case "snapshot":
		members = []string{"project"}
	case "arm":
		members = []string{"operation", "project", "target_id", "query", "effect"}
	case "control-state":
		members = []string{"arm_id"}
	case "release":
		members = []string{"arm_id", "request_token"}
	case "counts":
		members = []string{}
	case "logout":
		members = []string{"session_id"}
	case "archive-recovery-project":
		members = []string{"project", "expected_version"}
	case "reference-fact":
		members = []string{"project", "state"}
	case "rename-reuse":
		members = []string{"project"}
	default:
		return out, "invalid_action"
	}
	fields, err := projectModelsWebObject(out.Args, members...)
	if err != nil {
		return out, "invalid_arguments"
	}
	for key, raw := range fields {
		if out.Action == "arm" && (key == "project" || key == "target_id" || key == "query") && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		value, ok := projectModelsWebString(raw)
		if !ok || value == "" && !(out.Action == "arm" && key == "query") {
			return out, "invalid_arguments"
		}
		switch key {
		case "arm_id":
			if !projectModelsWebArmToken.MatchString(value) || value == "a0000" {
				return out, "invalid_arguments"
			}
		case "request_token":
			if !projectModelsWebRequestToken.MatchString(value) || value == "r000000" {
				return out, "invalid_arguments"
			}
		case "session_id", "target_id":
			if !projectModelsWebID.MatchString(value) {
				return out, "invalid_arguments"
			}
		case "expected_version":
			if !projectModelsWebVersion(raw) {
				return out, "invalid_arguments"
			}
		case "state":
			if value != "present" && value != "absent" {
				return out, "invalid_arguments"
			}
		case "effect":
			if value != "before_dispatch_hold" && value != "after_complete_hold" && value != "after_complete_cut" && value != "after_complete_disconnect" {
				return out, "invalid_arguments"
			}
		case "project":
			switch value {
			case "main", "second", "other", "admin_owned", "archiving", "archived", "deleting", "pending", "config_recovery", "credential_recovery", "referenced":
			default:
				return out, "invalid_arguments"
			}
			if out.Action == "rename-reuse" && value != "main" || out.Action == "reference-fact" && value != "referenced" || out.Action == "archive-recovery-project" && value != "config_recovery" && value != "credential_recovery" {
				return out, "invalid_arguments"
			}
		}
	}
	return out, ""
}
