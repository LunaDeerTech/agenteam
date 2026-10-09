//go:build integration

package account_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/LunaDeerTech/agenteam/internal/central/app"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	"github.com/LunaDeerTech/agenteam/tests/testsupport/accountenv"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
)

const projectModelsWebProtocol = "project-owner-models.v1"

type projectModelsWebFixture struct {
	*projectOwnerAuditWebFixture
	registry    projectModelsWebRegistry
	modelCounts map[string]*projectModelsWebOperationCounts
	modelServer struct {
		Started  int `json:"started"`
		Finished int `json:"finished"`
	}
	modelControls struct {
		Armed        int `json:"armed"`
		Claimed      int `json:"claimed"`
		Held         int `json:"held"`
		HeldJoined   int `json:"held_joined"`
		Cut          int `json:"cut"`
		Disconnected int `json:"disconnected"`
	}
	modelSessionCounts map[string]int
	requestSequence    int
	modelArms          []*projectModelsWebArm
	modelFailure       bool
	modelSessions      map[string]projectModelsWebSession
	modelOrigins       []*projectModelsWebOrigin
}

type projectModelsWebOperationCounts struct {
	Operation        string `json:"operation"`
	Setup            int    `json:"setup"`
	Browser          int    `json:"browser"`
	Control          int    `json:"control"`
	UpstreamComplete int    `json:"upstream_complete"`
	HandlerJoined    int    `json:"handler_joined"`
}

type projectModelsWebArm struct {
	ID                                              string
	Operation, Project, Target, Query, Effect       string
	Request                                         *projectModelsWebRequest
	Release                                         chan struct{}
	ReleaseOnce                                     sync.Once
	Held, Released, Complete, Safe, Applied, Joined bool
}

type projectModelsWebRequest struct {
	Token, Source, Project, Target string
	Operation                      *projectModelsWebOperation
	Arm                            *projectModelsWebArm
	Tap                            *projectModelsWebTap
	User, Key, Method, Path        string
	Origin                         *projectModelsWebOrigin
}

type projectModelsWebSession struct{ ID, User, Cookie, CSRF string }
type projectModelsWebOrigin struct {
	Original        *projectModelsWebRequest
	ComparisonCount int
	Comparison      map[string]any
}

type projectModelsWebTap struct {
	io.ReadCloser
	mu                sync.Mutex
	raw               []byte
	limit             int
	complete, invalid bool
}

func (tap *projectModelsWebTap) Read(p []byte) (int, error) {
	n, err := tap.ReadCloser.Read(p)
	tap.mu.Lock()
	defer tap.mu.Unlock()
	if !tap.invalid && len(tap.raw)+n <= tap.limit {
		tap.raw = append(tap.raw, p[:n]...)
	} else {
		tap.invalid = true
		clear(tap.raw)
		tap.raw = nil
	}
	if err == io.EOF {
		tap.complete = true
	}
	if err != nil && err != io.EOF {
		tap.invalid = true
	}
	return n, err
}
func (tap *projectModelsWebTap) captured() ([]byte, bool) {
	tap.mu.Lock()
	defer tap.mu.Unlock()
	return append([]byte(nil), tap.raw...), tap.complete && !tap.invalid
}
func (tap *projectModelsWebTap) clear() {
	tap.mu.Lock()
	defer tap.mu.Unlock()
	clear(tap.raw)
	tap.raw = nil
}

type projectModelsWebRequestKey struct{}

type projectModelsWebResponseLoss struct {
	header http.Header
	length int
	cut    bool
}

func (*projectModelsWebResponseLoss) Error() string {
	return "owned Project model response deliberately interrupted"
}

func projectModelsWebPage(raw string) bool {
	for _, suffix := range []string{"/model-providers", "/available-models"} {
		if strings.HasSuffix(raw, "/settings"+suffix) {
			return projectOwnerWebPage(strings.TrimSuffix(raw, suffix) + "/general")
		}
	}
	return projectOwnerAuditWebPage(raw)
}

func (f *projectModelsWebFixture) releaseAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, arm := range f.modelArms {
		arm.Released = true
		arm.ReleaseOnce.Do(func() { close(arm.Release) })
	}
}

func (f *projectModelsWebFixture) match(r *http.Request) (string, string, *projectModelsWebOperation) {
	for key, project := range f.registry.Projects {
		base := "/api/v1/projects/" + project
		if !strings.HasPrefix(r.URL.Path, base+"/") {
			continue
		}
		for n := range f.registry.Operations {
			op := &f.registry.Operations[n]
			if op.Method != r.Method {
				continue
			}
			path := base + op.Path
			if op.Target == nil {
				if r.URL.Path == path {
					return key, "", op
				}
				continue
			}
			prefix, _, found := strings.Cut(path, "{")
			if found && strings.HasPrefix(r.URL.Path, prefix) {
				target := strings.TrimPrefix(r.URL.Path, prefix)
				if projectModelsWebID.MatchString(target) && f.registry.Targets[key][*op.Target][target] {
					return key, target, op
				}
			}
		}
	}
	return "", "", nil
}

func (f *projectModelsWebFixture) serveAPI(proxy http.Handler, w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.modelServer.Started++
	f.requestSequence++
	source := "setup"
	if f.browserActive {
		source = "browser"
	}
	project, target, operation := f.match(r)
	request := &projectModelsWebRequest{Token: fmt.Sprintf("r%06d", f.requestSequence), Source: source, Project: project, Target: target, Operation: operation}
	request.Method, request.Path, request.Key = r.Method, r.URL.Path, r.Header.Get("Idempotency-Key")
	if session, ok := f.modelSessions[r.Header.Get("Cookie")]; ok {
		request.User = session.User
	}
	name := ""
	if operation != nil {
		name = operation.Operation
		entry := f.modelCounts[name]
		if source == "setup" {
			entry.Setup++
		} else {
			entry.Browser++
		}
	} else if r.Method == http.MethodGet && r.URL.Path == "/api/v1/session" {
		name = "getCurrentSession"
		f.modelSessionCounts[source]++
	}
	if source == "browser" && name != "" {
		for _, arm := range f.modelArms {
			if arm.Request == nil && !arm.Joined && arm.Operation == name && arm.Project == project && arm.Target == target && arm.Query == r.URL.RawQuery {
				arm.Request = request
				request.Arm = arm
				f.modelControls.Claimed++
				break
			}
		}
	}
	if operation != nil && operation.Family == "mutation" {
		limit := 1048576
		if strings.Contains(name, "Credential") {
			limit = 409600
			if r.Method == http.MethodDelete {
				limit = 1024
			}
		}
		request.Tap = &projectModelsWebTap{ReadCloser: r.Body, limit: limit}
		r.Body = request.Tap
		for _, origin := range f.modelOrigins {
			original := origin.Original
			if original.Project == project && original.Target == target && original.Operation.Operation == name {
				request.Origin = origin
				break
			}
		}
		if request.Arm != nil {
			if request.Origin != nil || len(f.modelOrigins) >= 4 {
				f.modelFailure = true
			} else {
				request.Origin = &projectModelsWebOrigin{Original: request}
				f.modelOrigins = append(f.modelOrigins, request.Origin)
			}
		}
	}
	f.mu.Unlock()
	defer func() {
		if request.Tap != nil {
			f.compareRequest(request)
			if request.Origin == nil || request.Origin.Original != request {
				request.Tap.clear()
				request.Key = ""
			}
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.modelServer.Finished++
		if operation != nil {
			f.modelCounts[name].HandlerJoined++
		}
		if arm := request.Arm; arm != nil {
			arm.Joined = true
			if arm.Held {
				f.modelControls.HeldJoined++
			}
		}
	}()
	r = r.WithContext(context.WithValue(r.Context(), projectModelsWebRequestKey{}, request))
	if arm := request.Arm; arm != nil && arm.Effect == "before_dispatch_hold" {
		if !f.hold(r.Context(), arm) {
			return
		}
	}
	proxy.ServeHTTP(w, r)
}

func (f *projectModelsWebFixture) hold(ctx context.Context, arm *projectModelsWebArm) bool {
	f.mu.Lock()
	arm.Held = true
	f.modelControls.Held++
	f.mu.Unlock()
	select {
	case <-arm.Release:
		return ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

func (f *projectModelsWebFixture) observeResponse(response *http.Response) error {
	request, ok := response.Request.Context().Value(projectModelsWebRequestKey{}).(*projectModelsWebRequest)
	if !ok {
		return errors.New("owned Project model request registration missing")
	}
	if request.Operation == nil {
		if response.Request.Method == http.MethodGet && response.Request.URL.Path == "/api/v1/session" && response.StatusCode == http.StatusOK {
			raw, err := io.ReadAll(io.LimitReader(response.Body, 600001))
			closed := response.Body.Close()
			if err != nil || closed != nil || len(raw) > 600000 || projectModelsWebJSON(raw) != nil {
				clear(raw)
				return errors.New("owned Session observation invalid")
			}
			var value struct {
				CSRF string `json:"csrf_token"`
				User *struct {
					ID string `json:"id"`
				} `json:"user"`
				Session *struct {
					ID string `json:"id"`
				} `json:"session"`
			}
			if json.Unmarshal(raw, &value) == nil && value.User != nil && value.Session != nil && projectModelsWebID.MatchString(value.User.ID) && projectModelsWebID.MatchString(value.Session.ID) && value.CSRF != "" {
				cookie := response.Request.Header.Get("Cookie")
				f.mu.Lock()
				f.modelSessions[cookie] = projectModelsWebSession{ID: value.Session.ID, User: value.User.ID, CSRF: value.CSRF, Cookie: cookie}
				if request.Source == "browser" {
					f.registry.Sessions[value.Session.ID] = true
				}
				f.mu.Unlock()
			}
			response.Body = &projectOwnerWebBody{Reader: bytes.NewReader(raw), raw: raw}
		}
		return nil
	}
	limit := int64(1024)
	if request.Operation.Family == "read" && request.Operation.Operation != "getProjectModelCredentialMetadata" {
		limit = 8388608
	}
	if response.StatusCode != http.StatusOK {
		limit = 600000
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	closeErr := response.Body.Close()
	if err != nil || closeErr != nil || int64(len(raw)) > limit || projectModelsWebJSON(raw) != nil {
		clear(raw)
		return errors.New("owned Project model complete response invalid")
	}
	if err := f.admitResponse(request, response, raw); err != nil {
		clear(raw)
		f.mu.Lock()
		f.modelFailure = true
		f.mu.Unlock()
		return err
	}
	if request.Operation.Family == "mutation" {
		body, complete := request.Tap.captured()
		var fields map[string]json.RawMessage
		decodeErr := json.Unmarshal(body, &fields)
		clear(body)
		if response.StatusCode == http.StatusOK {
			var receipt map[string]json.RawMessage
			if !complete || decodeErr != nil || json.Unmarshal(raw, &receipt) != nil {
				clear(raw)
				return errors.New("owned mutation request consumption incomplete")
			}
			if expected, exists := fields["expected_version"]; exists {
				old, ok := projectModelsWebString(expected)
				next, _ := projectModelsWebString(receipt["version"])
				parsed, err := strconv.ParseInt(old, 10, 64)
				if !ok || err != nil || parsed < 1 || parsed == 9223372036854775807 || next != strconv.FormatInt(parsed+1, 10) {
					clear(raw)
					return errors.New("owned mutation receipt version mismatch")
				}
			}
		}
	}
	if err := f.saveModelResponse(request, response, raw); err != nil {
		clear(raw)
		return err
	}
	response.Header.Set("X-Project-Models-Request", request.Token)
	f.mu.Lock()
	f.modelCounts[request.Operation.Operation].UpstreamComplete++
	if request.Arm != nil {
		request.Arm.Complete = true
		request.Arm.Safe = true
	}
	f.mu.Unlock()
	if arm := request.Arm; arm != nil {
		if arm.Effect == "after_complete_hold" && !f.hold(response.Request.Context(), arm) {
			clear(raw)
			return response.Request.Context().Err()
		}
		if arm.Effect == "after_complete_cut" || arm.Effect == "after_complete_disconnect" {
			if response.StatusCode != http.StatusOK || request.Operation.Family != "mutation" {
				clear(raw)
				return errors.New("owned response loss requires a complete successful mutation")
			}
			f.mu.Lock()
			arm.Applied = true
			if arm.Effect == "after_complete_cut" {
				f.modelControls.Cut++
			} else {
				f.modelControls.Disconnected++
			}
			f.mu.Unlock()
			lost := &projectModelsWebResponseLoss{header: response.Header.Clone(), length: len(raw), cut: arm.Effect == "after_complete_cut"}
			clear(raw)
			return lost
		}
	}
	response.Body = &projectOwnerWebBody{Reader: bytes.NewReader(raw), raw: raw}
	return nil
}

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

func (f *projectModelsWebFixture) startRoot(t *testing.T, ctx context.Context) config.Config {
	t.Helper()
	dist := os.Getenv("AGENTEAM_PROJECT_MODELS_WEB_DIST")
	runtime := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 {
		t.Fatal("explicit frozen Project dist and short owned browser runtime required")
	}
	if _, err := os.Stat(filepath.Join(dist, "index.html")); err != nil {
		t.Fatal("frozen production dist missing")
	}
	f.evidence = os.Getenv("AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE")
	f.inputHash = os.Getenv("AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH")
	decoded, err := hex.DecodeString(f.inputHash)
	if !filepath.IsAbs(f.evidence) || len(decoded) != sha256.Size || err != nil {
		t.Fatal("explicit safe evidence directory and frozen input hash required")
	}
	f.evidence = filepath.Join(f.evidence, t.Name())
	if err := os.Mkdir(f.evidence, 0700); err != nil {
		t.Fatal("fresh Project evidence directory required")
	}
	directory, err := os.MkdirTemp(runtime, "models-")
	if err != nil {
		t.Fatal("private browser runtime unavailable")
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error("private Project runtime cleanup failed")
		}
		if _, err := os.Lstat(directory); !errors.Is(err, os.ErrNotExist) {
			t.Error("private Project runtime remains")
		}
	})
	db := pgfixture.NewDatabase(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("owned Project listener unavailable")
	}
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	values := accountenv.New(t).Values()
	objects, err := objectfixture.Environment(ctx, db.Name)
	if err != nil {
		t.Fatal("owned full object fixture required")
	}
	for _, entry := range objects {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	for key, value := range map[string]string{
		"DATABASE_URL": db.Fixture.URL(db.Name), "DATABASE_CA_FILE": db.Fixture.CAFile,
		"DATABASE_STARTUP_TIMEOUT": "15s", "HTTP_ADDR": "127.0.0.1:0", "PUBLIC_ORIGIN": origin, "SHUTDOWN_TIMEOUT": "2s",
		"CURSOR_KEYRING": `{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`,
		"SECRET_KEYRING": `{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`,
	} {
		values[config.Prefix+key] = value
	}
	var environment []string
	for key, value := range values {
		environment = append(environment, key+"="+value)
	}
	cfg, err := config.Load(func(key string) (string, bool) { value, ok := values[key]; return value, ok }, environment)
	if err != nil {
		t.Fatal("owned default root configuration invalid")
	}
	output := &httpFixtureLog{listening: make(chan string, 1)}
	logger, err := logging.New(logging.Central, slog.LevelInfo, output)
	if err != nil {
		t.Fatal(err)
	}
	rootCtx, cancel := context.WithCancel(ctx)
	rootDone := make(chan error, 1)
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-rootDone:
			if err != nil {
				t.Error("Project default root shutdown failed", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Project default root exceeded cleanup observation budget; ownership retained until actual return")
			<-rootDone
		}
		t.Log("Project default root actual join")
	})
	go func() { rootDone <- app.Run(rootCtx, cfg, logger, nil) }()
	var address string
	select {
	case address = <-output.listening:
	case err := <-rootDone:
		rootDone <- err
		t.Fatal("Project default root initialization failed", err)
	case <-ctx.Done():
		t.Fatal("Project root startup exhausted top budget")
	}
	backend, err := url.Parse("http://" + address)
	if err != nil {
		t.Fatal(err)
	}
	old := &httpFixture{t: t, db: db, config: cfg, address: address, log: output}
	entry := old.record("bootstrap", "admin@mail.com", "")
	f.authenticationWebFixture = &authenticationWebFixture{t: t, db: db, origin: origin, directory: directory, webRoot: dist, entry: entry, log: output, record: old.record, recoveryLogPath: cfg.AccountRecoveryLog()}
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = transport
	proxy.ModifyResponse = f.observeResponse
	proxy.ErrorLog = slog.NewLogLogger(slog.NewTextHandler(io.Discard, nil), slog.LevelError)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		var lost *projectModelsWebResponseLoss
		if !errors.As(err, &lost) {
			http.Error(w, "owned Project API unavailable", http.StatusBadGateway)
			return
		}
		for key, values := range lost.header {
			w.Header()[key] = append([]string(nil), values...)
		}
		w.Header().Del("Transfer-Encoding")
		w.Header().Set("Content-Length", strconv.Itoa(lost.length))
		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusOK)
		if lost.cut {
			_, _ = w.Write([]byte("{"))
		}
		_ = http.NewResponseController(w).Flush()
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}
	assets := http.FileServer(http.Dir(dist))
	var handlers sync.WaitGroup
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		if r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/") {
			f.serveAPI(proxy, w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method unavailable", http.StatusMethodNotAllowed)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		if info, err := os.Stat(filepath.Join(dist, clean)); err == nil && !info.IsDir() {
			assets.ServeHTTP(w, r)
			return
		}
		// Legal Project names may contain dots. Classify the original request
		// path before treating an extension as a missing static asset; the
		// frontend still performs its independent raw-route and API checks.
		if projectModelsWebPage(r.RequestURI) {
			w.Header().Set("Cache-Control", "no-store")
			http.ServeFile(w, r, filepath.Join(dist, "index.html"))
			return
		}
		if strings.HasPrefix(clean, "/assets/") || filepath.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	})}
	serverDone := make(chan error, 1)
	var stopProxyOnce sync.Once
	f.stopProxy = func() {
		stopProxyOnce.Do(func() {
			f.releaseAll()
			shutdown, stop := context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
			if err := server.Shutdown(shutdown); err != nil {
				t.Error("Project proxy exceeded graceful cleanup budget")
				_ = server.Close()
			}
			if err := <-serverDone; !errors.Is(err, http.ErrServerClosed) {
				t.Error("Project proxy Serve failed", err)
			}
			handlers.Wait()
			transport.CloseIdleConnections()
			t.Log("Project proxy Serve and all body handlers actually joined")
		})
	}
	t.Cleanup(f.stopProxy)
	go func() { serverDone <- server.Serve(listener) }()
	return cfg
}

func projectModelsWebKind(operation string) string {
	for _, action := range []string{"create", "update", "delete"} {
		if operation == action+"ProjectModelProvider" {
			return "provider." + action
		}
		if operation == action+"ProjectModel" {
			return "model." + action
		}
		if operation == action+"ProjectModelCredential" {
			return action
		}
	}
	return ""
}

func (f *projectModelsWebFixture) registerTarget(project, family, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.registry.Targets[project] == nil {
		f.registry.Targets[project] = map[string]map[string]bool{}
	}
	if f.registry.Targets[project][family] == nil {
		f.registry.Targets[project][family] = map[string]bool{}
	}
	f.registry.Targets[project][family][id] = true
}

func (f *projectModelsWebFixture) admitResponse(request *projectModelsWebRequest, response *http.Response, raw []byte) error {
	bad := errors.New("owned Project model response safe admission failed")
	if response.Header.Get("Cache-Control") != "no-store" || !projectModelsWebID.MatchString(response.Header.Get("X-Request-ID")) {
		return bad
	}
	media, _, _ := strings.Cut(response.Header.Get("Content-Type"), ";")
	if response.StatusCode != http.StatusOK {
		if media != "application/problem+json" {
			return bad
		}
		var actual map[string]json.RawMessage
		if json.Unmarshal(raw, &actual) != nil {
			return bad
		}
		var problem httpapi.Problem
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&problem) != nil || !problem.Code.Known() || problem.RequestID.String() != response.Header.Get("X-Request-ID") || problem.Instance != response.Request.URL.Path || len(problem.FieldErrors) != 0 {
			return bad
		}
		// This formatter is an in-memory admission control. Evidence remains the
		// original upstream bytes, never the control's re-encoded response.
		control := httptest.NewRecorder()
		httpapi.WriteProblem(control, response.Request, foundation.NewFault(problem.Code, problem.CommitState))
		var expected httpapi.Problem
		if json.Unmarshal(control.Body.Bytes(), &expected) != nil || problem.Title != expected.Title || problem.Type != expected.Type || problem.Detail != expected.Detail || problem.Status != response.StatusCode || problem.Status != expected.Status || problem.CommitState != expected.CommitState || problem.RetryHint != expected.RetryHint {
			return bad
		}
		for key := range actual {
			if key != "type" && key != "title" && key != "status" && key != "detail" && key != "instance" && key != "code" && key != "request_id" && key != "commit_state" && key != "retry_hint" {
				return bad
			}
		}
		return nil
	}
	if media != "application/json" {
		return bad
	}
	op := request.Operation
	var result map[string]json.RawMessage
	if json.Unmarshal(raw, &result) != nil {
		return bad
	}
	if op.Family == "lookup" {
		flag, value := "found", "receipt"
		if op.Operation == "lookupProjectModelCredential" {
			flag, value = "observed", "result"
		}
		fields, err := projectModelsWebObject(raw, flag, value)
		if err != nil {
			return bad
		}
		if string(fields[flag]) == "false" && string(fields[value]) == "null" {
			return nil
		}
		if string(fields[flag]) != "true" || string(fields[value]) == "null" {
			return bad
		}
		if json.Unmarshal(fields[value], &result) != nil {
			return bad
		}
	}
	if op.Family == "mutation" || op.Family == "lookup" || op.Operation == "getProjectModelCredentialMetadata" {
		credential := strings.Contains(op.Operation, "Credential")
		keys := []string{"kind", "resource_id", "version", "affected_references"}
		if credential {
			keys = []string{"credential_id", "purpose", "version", "deleted"}
		}
		if op.Operation == "getProjectModelCredentialMetadata" {
			keys = []string{"credential_id", "purpose", "version"}
		}
		if len(result) != len(keys) {
			return bad
		}
		for _, key := range keys {
			if result[key] == nil {
				return bad
			}
		}
		if !projectModelsWebVersion(result["version"]) {
			return bad
		}
		kind := projectModelsWebKind(op.Operation)
		idKey, family := "resource_id", "model"
		if credential {
			idKey, family = "credential_id", "credential"
		} else if strings.Contains(op.Operation, "Provider") {
			family = "provider"
		}
		resource, _ := projectModelsWebString(result[idKey])
		if !projectModelsWebID.MatchString(resource) || request.Target != "" && resource != request.Target {
			return bad
		}
		if credential {
			purpose, _ := projectModelsWebString(result["purpose"])
			if purpose != "model" {
				return bad
			}
			if result["deleted"] != nil && string(result["deleted"]) != "true" && string(result["deleted"]) != "false" {
				return bad
			}
			if op.Family == "mutation" && (string(result["deleted"]) == "true") != (kind == "delete") {
				return bad
			}
		} else {
			gotKind, _ := projectModelsWebString(result["kind"])
			affected, _ := projectModelsWebString(result["affected_references"])
			if affected != "0" || !strings.Contains("|provider.create|provider.update|provider.delete|model.create|model.update|model.delete|", "|"+gotKind+"|") || op.Family == "mutation" && gotKind != kind {
				return bad
			}
		}
		if strings.HasSuffix(kind, "create") {
			version, _ := projectModelsWebString(result["version"])
			if version != "1" {
				return bad
			}
			f.registerTarget(request.Project, family, resource)
		}
		return nil
	}
	if op.Query == "page" {
		fields, err := projectModelsWebObject(raw, "items", "next_cursor")
		if err != nil {
			return bad
		}
		var items []json.RawMessage
		if json.Unmarshal(fields["items"], &items) != nil || items == nil || len(items) > 100 {
			return bad
		}
		seen := map[string]bool{}
		for _, item := range items {
			id, err := f.admitResource(request, item)
			if err != nil || seen[id] {
				return bad
			}
			seen[id] = true
		}
		if string(fields["next_cursor"]) != "null" {
			cursor, ok := projectModelsWebString(fields["next_cursor"])
			if !ok || len(cursor) == 0 || len(cursor) > 8192 || strings.ContainsAny(cursor, "\r\n\x00") {
				return bad
			}
			f.mu.Lock()
			if f.registry.Cursors[request.Project] == nil {
				f.registry.Cursors[request.Project] = map[string]map[string]bool{}
			}
			if f.registry.Cursors[request.Project][op.Operation] == nil {
				f.registry.Cursors[request.Project][op.Operation] = map[string]bool{}
			}
			f.registry.Cursors[request.Project][op.Operation][cursor] = true
			f.mu.Unlock()
		}
		return nil
	}
	id, err := f.admitResource(request, raw)
	if err != nil || id != request.Target {
		return bad
	}
	return nil
}

func (f *projectModelsWebFixture) admitResource(request *projectModelsWebRequest, raw []byte) (string, error) {
	bad := errors.New("owned Project model resource safe admission failed")
	available := request.Operation.Operation == "listProjectAvailableChatModels"
	provider := strings.Contains(request.Operation.Operation, "Provider")
	keys := []string{"id", "scope", "input", "version", "created_at", "updated_at"}
	if !provider {
		keys = append(keys, "provider_id")
	}
	if available {
		keys = []string{"id", "provider_id", "scope", "name", "provider_name", "version", "capabilities"}
	}
	v, err := projectModelsWebObject(raw, keys...)
	if err != nil {
		return "", bad
	}
	id, _ := projectModelsWebString(v["id"])
	if !projectModelsWebID.MatchString(id) || !projectModelsWebVersion(v["version"]) {
		return "", bad
	}
	var scope map[string]json.RawMessage
	if json.Unmarshal(v["scope"], &scope) != nil {
		return "", bad
	}
	kind, _ := projectModelsWebString(scope["kind"])
	if kind == "project" {
		project, _ := projectModelsWebString(scope["project_id"])
		if len(scope) != 2 || project != f.registry.Projects[request.Project] {
			return "", bad
		}
	} else if !available || kind != "system" || len(scope) != 1 {
		return "", bad
	}
	if !provider {
		value, _ := projectModelsWebString(v["provider_id"])
		if !projectModelsWebID.MatchString(value) {
			return "", bad
		}
	}
	name := func(raw json.RawMessage) bool {
		value, ok := projectModelsWebString(raw)
		return ok && len(value) <= 512 && strings.HasPrefix(value, "Models ")
	}
	if available {
		if !name(v["name"]) || !name(v["provider_name"]) || !projectModelsWebSafeCapabilities(v["capabilities"]) {
			return "", bad
		}
		return id, nil
	}
	for _, key := range []string{"created_at", "updated_at"} {
		value, ok := projectModelsWebString(v[key])
		if !ok {
			return "", bad
		}
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			return "", bad
		}
	}
	inputKeys := []string{"name", "protocol", "base_url", "enabled", "credential_ref", "options"}
	if !provider {
		inputKeys = []string{"name", "provider_model_id", "type", "enabled", "parameters", "request_overwrite", "header_overwrite", "capabilities"}
	}
	input, err := projectModelsWebObject(v["input"], inputKeys...)
	if err != nil || !name(input["name"]) || string(input["enabled"]) != "true" && string(input["enabled"]) != "false" {
		return "", bad
	}
	if provider {
		protocol, _ := projectModelsWebString(input["protocol"])
		baseURL, _ := projectModelsWebString(input["base_url"])
		if protocol != "openai-chat-completions" && protocol != "anthropic-messages" || baseURL != "https://model-ui.invalid/v1" {
			return "", bad
		}
		if _, err := projectModelsWebObject(input["options"]); err != nil {
			return "", bad
		}
		if string(input["credential_ref"]) != "null" {
			ref, _ := projectModelsWebString(input["credential_ref"])
			if !projectModelsWebID.MatchString(ref) {
				return "", bad
			}
		}
	} else {
		native, _ := projectModelsWebString(input["provider_model_id"])
		modelType, _ := projectModelsWebString(input["type"])
		if !strings.HasPrefix(native, "models-fixture-") || len(native) > 256 || modelType != "chat" || !projectModelsWebSafeCapabilities(input["capabilities"]) {
			return "", bad
		}
		for _, key := range []string{"parameters", "request_overwrite", "header_overwrite"} {
			if _, err := projectModelsWebObject(input[key]); err != nil {
				return "", bad
			}
		}
	}
	return id, nil
}

func projectModelsWebSafeCapabilities(raw []byte) bool {
	v, err := projectModelsWebObject(raw, "tool_calls", "parallel_tool_calls", "streaming", "reasoning", "input_modalities", "output_modalities", "reasoning_efforts", "structured_output_modes", "context_length", "max_output")
	if err != nil {
		return false
	}
	for _, key := range []string{"tool_calls", "parallel_tool_calls", "streaming", "reasoning"} {
		if string(v[key]) != "true" && string(v[key]) != "false" {
			return false
		}
	}
	for _, key := range []string{"context_length", "max_output"} {
		if string(v[key]) != "null" && !projectModelsWebVersion(v[key]) {
			return false
		}
	}
	for _, key := range []string{"input_modalities", "output_modalities", "reasoning_efforts", "structured_output_modes"} {
		var values []string
		if json.Unmarshal(v[key], &values) != nil || values == nil {
			return false
		}
		for _, value := range values {
			if key == "reasoning_efforts" {
				return false
			}
			if key == "structured_output_modes" {
				if value != "text" && value != "json_schema" {
					return false
				}
			} else if value != "text" && value != "image" && value != "file" && value != "vector" {
				return false
			}
		}
	}
	return true
}

func (f *projectModelsWebFixture) compareRequest(request *projectModelsWebRequest) {
	if request.Origin == nil || request.Origin.Original == request {
		return
	}
	original := request.Origin.Original
	before, beforeOK := original.Tap.captured()
	defer clear(before)
	after, afterOK := request.Tap.captured()
	defer clear(after)
	if !beforeOK || !afterOK || original.User == "" || request.User == "" || original.Key == "" || request.Key == "" {
		return
	}
	comparison := map[string]any{
		"request_token": request.Token, "body_equal": bytes.Equal(before, after), "key_equal": original.Key == request.Key,
		"target_equal": original.Path == request.Path, "identity_equal": original.User == request.User && original.Project == request.Project && original.Operation.Operation == request.Operation.Operation,
		"method_equal": original.Method == request.Method, "original_body_bytes": len(before), "replay_body_bytes": len(after),
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	request.Origin.Comparison = comparison
	request.Origin.ComparisonCount++
}

func (f *projectModelsWebFixture) saveModelResponse(request *projectModelsWebRequest, response *http.Response, raw []byte) error {
	sum := fmt.Sprintf("%x", sha256.Sum256(raw))
	name := "body-" + sum + ".json"
	transfer := "forwarded"
	if arm := request.Arm; arm != nil {
		switch arm.Effect {
		case "before_dispatch_hold", "after_complete_hold":
			transfer = "hold"
		case "after_complete_cut":
			transfer = "cut"
		case "after_complete_disconnect":
			transfer = "failure"
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.responseSequence >= 512 {
		return errors.New("owned model evidence count exceeded")
	}
	path := filepath.Join(f.evidence, name)
	if existing, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(existing, raw) {
			return errors.New("owned model response digest collision")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if err := os.WriteFile(path, raw, 0600); err != nil {
		return err
	}
	f.responseSequence++
	var target any
	if request.Target != "" {
		target = request.Target
	}
	metadata := map[string]any{
		"protocol": projectModelsWebProtocol, "sequence": f.responseSequence, "source_run": f.t.Name(), "input_hash": f.inputHash, "source": request.Source,
		"operation": request.Operation.Operation, "method": response.Request.Method, "endpoint": response.Request.URL.Path, "query": response.Request.URL.RawQuery,
		"status": response.StatusCode, "content_type": response.Header.Get("Content-Type"), "content_length": response.Header.Get("Content-Length"), "request_id": response.Header.Get("X-Request-ID"),
		"project_id": f.registry.Projects[request.Project], "resource_id": target, "request_token": request.Token,
		"body_file": name, "body_sha256": sum, "body_bytes": len(raw), "transfer_kind": transfer, "body_stage": "complete_formal_upstream",
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(f.evidence, fmt.Sprintf("response-%03d.json", f.responseSequence)), encoded, 0600)
}
