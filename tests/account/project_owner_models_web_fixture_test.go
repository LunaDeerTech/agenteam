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
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
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
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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
	modelLastCounts    json.RawMessage
	modelAux           map[string]map[string]any
	modelReference     string
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
	arm.Applied = true
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

func projectModelsWebProjectKeys(mode string) []string {
	switch mode {
	case "configuration", "credential":
		return []string{"main"}
	case "recovery":
		return []string{"main", "config_recovery", "credential_recovery"}
	case "read", "navigation":
		return []string{"main", "second"}
	case "authority":
		return []string{"main", "second", "other", "admin_owned", "archiving", "archived", "deleting", "pending", "config_recovery", "credential_recovery", "referenced"}
	}
	return nil
}

func newProjectModelsWebFixture(t *testing.T, ctx context.Context, mode string) *projectModelsWebFixture {
	t.Helper()
	keys := projectModelsWebProjectKeys(mode)
	if keys == nil {
		t.Fatal("unknown Project Models browser mode")
	}
	raw, err := os.ReadFile("../../docs/development/work-items/d27-project-owner-model-settings-ui-endpoints.json")
	if err != nil {
		t.Fatal("formal Project Models endpoint attachment unavailable")
	}
	operations, err := projectModelsWebLoadOperations(raw)
	if err != nil {
		t.Fatal("formal Project Models endpoint attachment invalid")
	}
	base := &projectOwnerWebFixture{mode: mode, ids: map[string]string{}, initial: map[string]any{}}
	f := &projectModelsWebFixture{
		projectOwnerAuditWebFixture: &projectOwnerAuditWebFixture{projectOwnerWebFixture: base},
		registry:                    projectModelsWebRegistry{Operations: operations, Projects: base.ids, Targets: map[string]map[string]map[string]bool{}, Cursors: map[string]map[string]map[string]bool{}, Sessions: map[string]bool{}},
		modelCounts:                 map[string]*projectModelsWebOperationCounts{}, modelSessionCounts: map[string]int{"setup": 0, "browser": 0, "control": 0}, modelSessions: map[string]projectModelsWebSession{}, modelAux: map[string]map[string]any{},
	}
	for _, op := range operations {
		f.modelCounts[op.Operation] = &projectModelsWebOperationCounts{Operation: op.Operation}
	}
	cfg := f.startRoot(t, ctx)
	f.setup = &personalWebFixture{authenticationWebFixture: f.authenticationWebFixture}
	f.admin = projectOwnerWebCredential{personalWebCredential: personalWebCredential{Email: f.entry.Email, Password: f.entry.Password, UserID: f.entry.ID}}
	var session map[string]any
	f.adminClient, session = f.login(ctx, f.admin.personalWebCredential)
	f.admin.Username = httpString(t, httpObject(t, session, "user"), "username")
	f.adminCSRF, f.adminActor = httpString(t, session, "csrf_token"), f.actor(session)
	f.other = projectOwnerWebCredential{personalWebCredential: f.setup.inviteMember(ctx, f.adminClient, f.adminCSRF, "models-other@example.com", "models-other"), Username: "models-other"}
	_, session = f.login(ctx, f.other.personalWebCredential)
	f.otherActor = f.actor(session)
	if mode == "navigation" {
		f.owner, f.ownerClient, f.ownerCSRF, f.ownerActor = f.admin, f.adminClient, f.adminCSRF, f.adminActor
	} else {
		f.owner = projectOwnerWebCredential{personalWebCredential: f.setup.inviteMember(ctx, f.adminClient, f.adminCSRF, "models-owner@example.com", "models-owner"), Username: "models-owner"}
		f.ownerClient, session = f.login(ctx, f.owner.personalWebCredential)
		f.ownerCSRF, f.ownerActor = httpString(t, session, "csrf_token"), f.actor(session)
		if httpObject(t, session, "user")["role"] != "user" {
			t.Fatal("ordinary Project Model Owner was not prepared")
		}
	}
	f.projectOwnerAuditWebFixture.prepareService(ctx, cfg)
	t.Cleanup(func() {
		f.stopProxy()
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.modelServer.Started != f.modelServer.Finished || f.modelControls.Held != f.modelControls.HeldJoined || f.modelFailure {
			t.Error("Project Models proxy final state incomplete")
		}
		for _, origin := range f.modelOrigins {
			origin.Original.Tap.clear()
			origin.Original.Key = ""
		}
		clear(f.modelSessions)
		f.admin.Password, f.owner.Password, f.other.Password, f.adminCSRF, f.ownerCSRF = "", "", "", "", ""
	})
	for _, key := range keys {
		actor := f.ownerActor
		if key == "other" {
			actor = f.otherActor
		}
		if key == "admin_owned" {
			actor = f.adminActor
		}
		var projectID string
		if key == "pending" {
			pending, err := f.projects.CreateProject(ctx, actor, foundation.CommandMeta{RequestID: id[foundation.Request](t), IdempotencyKey: foundation.IdempotencyKey(id[struct{}](t).String())}, pc.CreateProjectRequest{ProjectID: f.skills.pending, Name: "models-pending"})
			if err != nil || pending.State != pc.CreationPending {
				t.Fatal("formal Project Models pending preparation failed")
			}
			projectID = f.skills.pending.String()
		} else {
			projectID = f.create(ctx, actor, "models-"+strings.ReplaceAll(key, "_", "-")).ID.String()
		}
		f.mu.Lock()
		f.ids[key] = projectID
		f.mu.Unlock()
		f.modelAux[key] = map[string]any{"archive_recovery_applied": false, "reference_fact_state": nil, "rename_reuse_applied": false}
	}
	projects, seedProjects := map[string]any{}, map[string]any{}
	for _, key := range keys {
		seedProjects[key] = map[string]any{"providers": []any{}, "models": []any{}, "credentials": []any{}}
	}
	createProvider := func(key, name string, enabled bool) map[string]any {
		client, csrf := f.ownerClient, f.ownerCSRF
		if key == "admin_owned" {
			client, csrf = f.adminClient, f.adminCSRF
		}
		receipt := f.setup.setupRequest(ctx, client, http.MethodPost, "/api/v1/projects/"+f.ids[key]+"/model-providers", map[string]any{"input": map[string]any{"name": name, "protocol": "openai-chat-completions", "base_url": "https://model-ui.invalid/v1", "enabled": enabled, "credential_ref": nil, "options": map[string]any{}}}, csrf, true, 200)
		return map[string]any{"id": receipt["resource_id"], "project_id": f.ids[key], "name": name, "protocol": "openai-chat-completions", "version": receipt["version"], "credential_ref": nil}
	}
	createModel := func(key, provider, name, native string, enabled bool) map[string]any {
		input := modelsWebInput(name, "chat")
		input["provider_model_id"], input["enabled"] = native, enabled
		receipt := f.setup.setupRequest(ctx, f.ownerClient, http.MethodPost, "/api/v1/projects/"+f.ids[key]+"/models", map[string]any{"provider_id": provider, "input": input}, f.ownerCSRF, true, 200)
		return map[string]any{"id": receipt["resource_id"], "project_id": f.ids[key], "provider_id": provider, "name": name, "version": receipt["version"]}
	}
	var pagination map[string]any
	if mode == "read" {
		providers, models := []any{}, []any{}
		providerIDs, modelIDs := []string{}, []string{}
		for n := 0; n < 26; n++ {
			p := createProvider("main", fmt.Sprintf("Models Provider %02d", n), n != 25)
			providers = append(providers, p)
			providerIDs = append(providerIDs, p["id"].(string))
		}
		for n := 0; n < 26; n++ {
			m := createModel("main", providerIDs[n%2], fmt.Sprintf("Models Chat %02d", n), fmt.Sprintf("models-fixture-%02d", n), n != 25)
			models = append(models, m)
			modelIDs = append(modelIDs, m["id"].(string))
		}
		system := &modelsWebFixture{authenticationWebFixture: f.authenticationWebFixture, setup: f.setup, admin: f.admin.personalWebCredential, adminClient: f.adminClient, csrf: f.adminCSRF, ids: map[string]string{}}
		providerID := system.createProvider(ctx, "Models System Provider", "openai-chat-completions", "https://model-ui.invalid/v1", true)
		input := modelsWebInput("Models System Chat", "chat")
		input["provider_model_id"] = "models-fixture-system"
		system.createModel(ctx, providerID, input)
		directory := f.setup.setupRequest(ctx, f.ownerClient, http.MethodGet, "/api/v1/projects/"+f.ids["main"]+"/available-chat-models?limit=100", nil, "", false, 200)
		available := []any{}
		for _, item := range directory["items"].([]any) {
			row := item.(map[string]any)
			available = append(available, map[string]any{"id": row["id"], "provider_id": row["provider_id"], "scope": row["scope"], "name": row["name"], "provider_name": row["provider_name"], "version": row["version"]})
		}
		if len(available) != 26 {
			t.Fatal("real mixed Project/System directory count differs from fixture contract")
		}
		sort.Strings(providerIDs)
		sort.Strings(modelIDs)
		seedProjects["main"] = map[string]any{"providers": providers, "models": models, "credentials": []any{}}
		pagination = map[string]any{"provider_ids": providerIDs, "model_ids": modelIDs, "available": available}
	} else {
		for _, key := range []string{"main", "referenced"} {
			if f.ids[key] == "" {
				continue
			}
			p := createProvider(key, "Models Seed Provider", true)
			seedProjects[key] = map[string]any{"providers": []any{p}, "models": []any{}, "credentials": []any{}}
			if key == "referenced" {
				m := createModel(key, p["id"].(string), "Models Referenced Chat", "models-fixture-referenced", true)
				seedProjects[key].(map[string]any)["models"] = []any{m}
				f.modelReference = id[struct{}](t).String()
			}
		}
	}
	if mode == "authority" {
		f.lifecycle(ctx, "archiving", pc.Archiving)
		f.lifecycle(ctx, "archived", pc.Archived)
		f.lifecycle(ctx, "deleting", pc.Deleting)
	}
	for _, key := range keys {
		projects[key] = f.locator(ctx, key)
		seeds := seedProjects[key].(map[string]any)
		for _, family := range []string{"providers", "models", "credentials"} {
			rows := seeds[family].([]any)
			sort.Slice(rows, func(i, j int) bool {
				return rows[i].(map[string]any)["id"].(string) < rows[j].(map[string]any)["id"].(string)
			})
		}
	}
	var system any
	if mode == "navigation" {
		prepared := f.prepareSystemDrafts(ctx)
		ids, names := prepared["ids"].(map[string]string), prepared["names"].(map[string]string)
		system = map[string]any{"selection": map[string]any{"initial": prepared["selection"], "draft": map[string]any{"purpose": "memory", "model": map[string]any{"id": ids["memory_replacement"], "name": names["memory_replacement"]}}}, "summary": map[string]any{"initial": prepared["summary"], "draft": map[string]any{"model": map[string]any{"id": ids["summary_1"], "name": names["summary_1"]}}}}
	}
	expected := map[string]any{"projects": seedProjects}
	if pagination != nil {
		expected["pagination"] = pagination
	}
	f.private("project-models-material.json", map[string]any{"protocol": projectModelsWebProtocol, "input_hash": f.inputHash, "mode": mode, "actors": map[string]any{"owner": f.owner, "other_owner": f.other, "other_admin": f.admin}, "projects": projects, "expected": expected, "system": system})
	return f
}

func (f *projectModelsWebFixture) modelSnapshot(ctx context.Context, key string) (map[string]any, error) {
	f.mu.Lock()
	project := f.registry.Projects[key]
	ids := map[string][]string{"provider": {}, "model": {}, "credential": {}}
	for family, values := range f.registry.Targets[key] {
		for id := range values {
			ids[family] = append(ids[family], id)
		}
		sort.Strings(ids[family])
	}
	origins := append([]*projectModelsWebOrigin(nil), f.modelOrigins...)
	aux := map[string]any{}
	for name, value := range f.modelAux[key] {
		aux[name] = value
	}
	f.mu.Unlock()
	if project == "" {
		return nil, errors.New("owned snapshot target missing")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	run, err := foundation.NewID[struct{}]()
	if err != nil {
		return nil, err
	}
	cause, err := foundation.NewRecoveryCause("project-model-ui-fixture", run.String(), "")
	if err != nil {
		return nil, err
	}
	var result map[string]any
	commit := f.store.WithinTxOptions(ctx, cause, postgres.TxOptions{Isolation: postgres.Serializable}, func(ctx context.Context, tx foundation.Tx) error {
		x, err := f.store.InTx(tx)
		if err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `SET TRANSACTION READ ONLY`); err != nil {
			return err
		}
		var version, lifecycle string
		var initialized bool
		if err = x.QueryRow(ctx, `SELECT version::text,lifecycle,initialized_at IS NOT NULL FROM agenteam_project.projects WHERE id=$1`, project).Scan(&version, &lifecycle, &initialized); err != nil {
			return err
		}
		providers, models, credentials := []any{}, []any{}, []any{}
		modelReferences, credentialReferences := []any{}, []any{}
		for _, id := range ids["provider"] {
			var version, credential string
			err = x.QueryRow(ctx, `SELECT coalesce((SELECT version::text FROM agenteam_model.providers WHERE scope='project' AND project_id=$1 AND id=$2),''),coalesce((SELECT credential_id::text FROM agenteam_model.providers WHERE scope='project' AND project_id=$1 AND id=$2),'')`, project, id).Scan(&version, &credential)
			if err != nil {
				return err
			}
			var v, c any
			if version != "" {
				v = version
			}
			if credential != "" {
				c = credential
			}
			providers = append(providers, map[string]any{"id": id, "present": version != "", "version": v, "credential_ref": c})
		}
		for _, id := range ids["model"] {
			var version, provider string
			var reference bool
			err = x.QueryRow(ctx, `SELECT coalesce((SELECT m.version::text FROM agenteam_model.models m JOIN agenteam_model.providers p ON p.id=m.provider_id WHERE p.scope='project' AND p.project_id=$1 AND m.id=$2),''),coalesce((SELECT m.provider_id::text FROM agenteam_model.models m JOIN agenteam_model.providers p ON p.id=m.provider_id WHERE p.scope='project' AND p.project_id=$1 AND m.id=$2),''),EXISTS(SELECT 1 FROM agenteam_model.references WHERE project_id=$1 AND model_id=$2)`, project, id).Scan(&version, &provider, &reference)
			if err != nil {
				return err
			}
			var v, p any
			if version != "" {
				v, p = version, provider
			}
			models = append(models, map[string]any{"id": id, "present": version != "", "version": v, "provider_id": p})
			modelReferences = append(modelReferences, map[string]any{"id": id, "present": reference})
		}
		for _, id := range ids["credential"] {
			var version string
			var reference bool
			err = x.QueryRow(ctx, `SELECT coalesce((SELECT version::text FROM agenteam_secret.secrets WHERE scope='project' AND project_id=$1 AND id=$2 AND purpose='model'),''),EXISTS(SELECT 1 FROM agenteam_secret.secret_references WHERE scope='project' AND project_id=$1 AND credential_id=$2)`, project, id).Scan(&version, &reference)
			if err != nil {
				return err
			}
			var metadata any
			if version != "" {
				metadata = map[string]any{"credential_id": id, "purpose": "model", "version": version}
			}
			credentials = append(credentials, map[string]any{"credential_id": id, "metadata": metadata})
			credentialReferences = append(credentialReferences, map[string]any{"credential_id": id, "present": reference})
		}
		historyConfig, historyCredential := map[string]any{}, map[string]any{}
		queries := []struct {
			output   map[string]any
			key, sql string
		}{
			{historyConfig, "committed_commands", `SELECT count(*)::text FROM agenteam_model.commands WHERE scope='project' AND project_id=$1 AND phase='committed'`},
			{historyConfig, "audit_records", `SELECT count(*)::text FROM agenteam_audit.audit_records WHERE scope='project' AND project_id=$1 AND producer='model'`},
			{historyConfig, "events", `SELECT count(*)::text FROM agenteam_outbox.events WHERE scope='project' AND project_id=$1 AND producer='model'`},
			{historyCredential, "committed_commands", `SELECT count(*)::text FROM agenteam_secret.secret_command_receipts WHERE scope='project' AND project_id=$1`},
			{historyCredential, "audit_records", `SELECT count(*)::text FROM agenteam_audit.audit_records WHERE scope='project' AND project_id=$1 AND producer='secret'`},
		}
		for _, query := range queries {
			var count string
			if err = x.QueryRow(ctx, query.sql, project).Scan(&count); err != nil {
				return err
			}
			query.output[query.key] = count
		}
		originFacts := []any{}
		for _, origin := range origins {
			original := origin.Original
			if original.Project != key {
				continue
			}
			body, complete := original.Tap.captured()
			size := len(body)
			clear(body)
			if !complete || original.User == "" {
				return errors.New("owned original request identity incomplete")
			}
			namespace, kind, family := "model.project", projectModelsWebKind(original.Operation.Operation), "configuration"
			if strings.Contains(original.Operation.Operation, "Credential") {
				namespace, family = "secret", "credential"
			}
			identity, err := foundation.NewCommandIdentity(namespace, []string{project, original.User}, kind, foundation.IdempotencyKey(original.Key))
			if err != nil {
				return err
			}
			history := map[string]any{"family": family, "committed_rows": "0"}
			if family == "configuration" {
				history["receipt"] = nil
				var raw []byte
				if err = x.QueryRow(ctx, `SELECT coalesce((SELECT safe_receipt::text FROM agenteam_model.commands WHERE scope='project' AND project_id=$1 AND phase='committed' AND command_identity=$2),'null')`, project, identity.Canonical()).Scan(&raw); err != nil {
					return err
				}
				if string(raw) != "null" {
					fields, err := projectModelsWebObject(raw, "kind", "resource_id", "version", "affected_references")
					if err != nil {
						return err
					}
					actualKind, _ := projectModelsWebString(fields["kind"])
					resource, _ := projectModelsWebString(fields["resource_id"])
					affected, _ := projectModelsWebString(fields["affected_references"])
					if actualKind != kind || !projectModelsWebID.MatchString(resource) || affected != "0" || !projectModelsWebVersion(fields["version"]) {
						return errors.New("owned configuration history invalid")
					}
					var receipt any
					if json.Unmarshal(raw, &receipt) != nil {
						return errors.New("owned configuration history invalid")
					}
					history["committed_rows"], history["receipt"] = "1", receipt
				}
			} else {
				history["result"] = nil
				digest, err := cursor.Digest([]byte(identity.Canonical()))
				if err != nil {
					return err
				}
				var raw []byte
				if err = x.QueryRow(ctx, `SELECT coalesce((SELECT jsonb_build_object('credential_id',credential_id::text,'purpose',purpose,'version',result_version::text,'deleted',deleted)::text FROM agenteam_secret.secret_command_receipts WHERE scope='project' AND project_id=$1 AND command_digest=$2),'null')`, project, string(digest)).Scan(&raw); err != nil {
					return err
				}
				if string(raw) != "null" {
					fields, err := projectModelsWebObject(raw, "credential_id", "purpose", "version", "deleted")
					if err != nil {
						return err
					}
					resource, _ := projectModelsWebString(fields["credential_id"])
					purpose, _ := projectModelsWebString(fields["purpose"])
					if !projectModelsWebID.MatchString(resource) || purpose != "model" || !projectModelsWebVersion(fields["version"]) || (string(fields["deleted"]) == "true") != (kind == "delete") {
						return errors.New("owned Credential history invalid")
					}
					var receipt any
					if json.Unmarshal(raw, &receipt) != nil {
						return errors.New("owned Credential history invalid")
					}
					history["committed_rows"], history["result"] = "1", receipt
				}
			}
			var target any
			if original.Target != "" {
				target = original.Target
			}
			f.mu.Lock()
			comparisonCount := origin.ComparisonCount
			var comparison any
			if origin.Comparison != nil {
				copy := map[string]any{}
				for k, v := range origin.Comparison {
					copy[k] = v
				}
				comparison = copy
			}
			f.mu.Unlock()
			originFacts = append(originFacts, map[string]any{"origin_token": original.Token, "original_request_token": original.Token, "operation": original.Operation.Operation, "project_id": project, "target_id": target, "original_body_bytes": size, "history": history, "comparison_count": comparisonCount, "comparison": comparison})
		}
		result = map[string]any{"project": map[string]any{"project_id": project, "version": version, "initialized": initialized, "lifecycle": lifecycle}, "current": map[string]any{"providers": providers, "models": models, "credentials": credentials}, "history": map[string]any{"configuration": historyConfig, "credential": historyCredential}, "reference_presence": map[string]any{"models": modelReferences, "credentials": credentialReferences}, "origins": originFacts, "fixture_only": aux}
		return nil
	})
	if commit.State() != foundation.Committed || ctx.Err() != nil {
		return nil, errors.New("owned snapshot transaction did not commit within its budget")
	}
	return result, nil
}

func (f *projectModelsWebFixture) modelControlState(arm *projectModelsWebArm) map[string]any {
	state := "armed"
	var token, origin any
	if arm.Request != nil {
		token = arm.Request.Token
		state = "claimed"
		if arm.Request.Origin != nil {
			origin = arm.Request.Origin.Original.Token
		}
	}
	if arm.Complete {
		state = "upstream_complete"
	}
	if arm.Released {
		state = "released"
	}
	if arm.Joined {
		state = "joined"
	}
	return map[string]any{"arm_id": arm.ID, "request_token": token, "origin_token": origin, "state": state, "held": arm.Held, "release_requested": arm.Released, "upstream_complete": arm.Complete, "safe_admitted": arm.Safe, "effect_applied": arm.Applied, "joined": arm.Joined}
}

func (f *projectModelsWebFixture) modelIPC(ctx context.Context, request projectModelsWebIPC) map[string]any {
	reply := map[string]any{"protocol": projectModelsWebProtocol, "input_hash": f.inputHash, "sequence": request.Sequence, "action": request.Action, "ok": false, "result": nil, "error": nil}
	fail := func(code string) map[string]any {
		f.mu.Lock()
		f.modelFailure = true
		f.mu.Unlock()
		reply["error"] = code
		return reply
	}
	f.mu.Lock()
	code := f.registry.admit(request)
	f.mu.Unlock()
	if code != "" {
		return fail(code)
	}
	if ctx.Err() != nil {
		return fail("budget_exhausted")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(request.Args, &fields) != nil {
		return fail("invalid_arguments")
	}
	get := func(name string) string { value, _ := projectModelsWebString(fields[name]); return value }
	var result any
	switch request.Action {
	case "counts":
		f.mu.Lock()
		operations := []projectModelsWebOperationCounts{}
		for _, op := range f.registry.Operations {
			operations = append(operations, *f.modelCounts[op.Operation])
		}
		sessions := map[string]int{}
		for key, value := range f.modelSessionCounts {
			sessions[key] = value
		}
		result = map[string]any{"operations": operations, "session": sessions, "server": f.modelServer, "controls": f.modelControls, "browser_eof": nil, "schema_bodies": nil, "client_bodies": nil}
		f.modelLastCounts, _ = json.Marshal(result)
		f.mu.Unlock()
	case "snapshot":
		snapshot, err := f.modelSnapshot(ctx, get("project"))
		if err != nil {
			return fail("fixture_failed")
		}
		result = snapshot
	case "arm":
		f.mu.Lock()
		for _, arm := range f.modelArms {
			if !arm.Joined {
				f.mu.Unlock()
				return fail("arm_busy")
			}
		}
		for _, origin := range f.modelOrigins {
			if origin.Original.Project == get("project") && origin.Original.Target == get("target_id") && origin.Original.Operation.Operation == get("operation") {
				f.mu.Unlock()
				return fail("invalid_arguments")
			}
		}
		mutation := false
		for _, op := range f.registry.Operations {
			if op.Operation == get("operation") {
				mutation = op.Family == "mutation"
			}
		}
		if mutation && len(f.modelOrigins) >= 4 {
			f.mu.Unlock()
			return fail("invalid_arguments")
		}
		arm := &projectModelsWebArm{ID: fmt.Sprintf("a%04d", len(f.modelArms)+1), Operation: get("operation"), Project: get("project"), Target: get("target_id"), Query: get("query"), Effect: get("effect"), Release: make(chan struct{})}
		f.modelArms = append(f.modelArms, arm)
		f.modelControls.Armed++
		result = map[string]any{"arm_id": arm.ID, "state": "armed"}
		f.mu.Unlock()
	case "control-state", "release":
		f.mu.Lock()
		var selected *projectModelsWebArm
		for _, arm := range f.modelArms {
			if arm.ID == get("arm_id") {
				selected = arm
				break
			}
		}
		if selected == nil {
			f.mu.Unlock()
			return fail("token_mismatch")
		}
		if request.Action == "control-state" {
			result = f.modelControlState(selected)
		} else {
			if selected.Request == nil || !selected.Held {
				f.mu.Unlock()
				return fail("not_ready")
			}
			if selected.Request.Token != get("request_token") {
				f.mu.Unlock()
				return fail("token_mismatch")
			}
			selected.Released = true
			selected.ReleaseOnce.Do(func() { close(selected.Release) })
			result = map[string]any{"arm_id": selected.ID, "request_token": selected.Request.Token, "release_requested": true}
		}
		f.mu.Unlock()
	case "logout":
		var session projectModelsWebSession
		f.mu.Lock()
		for _, candidate := range f.modelSessions {
			if candidate.ID == get("session_id") {
				session = candidate
				break
			}
		}
		f.mu.Unlock()
		if session.Cookie == "" {
			return fail("unknown_target")
		}
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, f.origin+"/api/v1/sessions/logout", strings.NewReader(`{}`))
		if err != nil {
			return fail("fixture_failed")
		}
		r.Header.Set("Cookie", session.Cookie)
		r.Header.Set("Origin", f.origin)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", session.CSRF)
		r.Header.Set("Idempotency-Key", id[struct{}](f.t).String())
		transport := &http.Transport{Proxy: nil}
		defer transport.CloseIdleConnections()
		response, err := (&http.Client{Transport: transport, Timeout: 8 * time.Second}).Do(r)
		if err != nil {
			return fail("fixture_failed")
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 1))
		closed := response.Body.Close()
		clear(raw)
		if response.StatusCode != 204 || readErr != nil || closed != nil || len(raw) != 0 {
			return fail("fixture_failed")
		}
		var revoked bool
		if f.store.QueryRow(ctx, `SELECT revoked_at IS NOT NULL AND revoked_reason='logout' FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`, session.ID, session.User).Scan(&revoked) != nil || !revoked {
			return fail("fixture_failed")
		}
		result = map[string]any{"session_id": session.ID, "revoked": true}
	case "archive-recovery-project":
		projectKey := get("project")
		target, err := foundation.ParseID[identity.Project](f.ids[projectKey])
		if err != nil {
			return fail("unknown_target")
		}
		parsed, err := strconv.ParseInt(get("expected_version"), 10, 64)
		if err != nil {
			return fail("invalid_arguments")
		}
		expected := foundation.Version(parsed)
		op, err := f.projects.BeginArchive(ctx, f.ownerActor, foundation.CommandMeta{RequestID: id[foundation.Request](f.t), IdempotencyKey: foundation.IdempotencyKey(id[struct{}](f.t).String()), ExpectedVersion: &expected}, target)
		if err != nil {
			return fail("fixture_failed")
		}
		committed := f.store.WithinTx(ctx, cause(f.t), func(ctx context.Context, tx foundation.Tx) error {
			lock, _ := foundation.ProjectLock(target.String())
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: lock, Mode: foundation.Exclusive}}); err != nil {
				return err
			}
			x, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			var at time.Time
			if err = x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
				return err
			}
			if _, err = x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET stop_state='stopped' WHERE operation_id=$1`, op.ID.String()); err != nil {
				return err
			}
			if _, err = x.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET state='completed',completed_project_version=project_version+1,completed_at=$2,updated_at=$2,version=version+1 WHERE id=$1 AND project_id=$3`, op.ID.String(), at, target.String()); err != nil {
				return err
			}
			tag, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=$2,updated_at=$2,version=version+1 WHERE id=$1 AND lifecycle='archiving' AND current_lifecycle_operation_id=$3 AND initialized_at IS NOT NULL`, target.String(), at, op.ID.String())
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return errors.New("owned archive auxiliary fact mismatch")
			}
			return nil
		})
		if committed.State() != foundation.Committed || ctx.Err() != nil {
			return fail("fixture_failed")
		}
		f.modelAux[projectKey]["archive_recovery_applied"] = true
		result = map[string]any{"project_id": target.String(), "initialized": true, "lifecycle": "archived", "fixture_only": true}
	case "reference-fact":
		project := f.ids["referenced"]
		f.mu.Lock()
		var model string
		for id := range f.registry.Targets["referenced"]["model"] {
			if model != "" {
				f.mu.Unlock()
				return fail("unknown_target")
			}
			model = id
		}
		f.mu.Unlock()
		if model == "" || f.modelReference == "" {
			return fail("unknown_target")
		}
		committed := f.store.WithinTx(ctx, cause(f.t), func(ctx context.Context, tx foundation.Tx) error {
			x, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			if get("state") == "present" {
				_, err = x.Exec(ctx, `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,project_id,model_id,owner_version) SELECT 'agent',$1,'agent_model',$2,m.id,1 FROM agenteam_model.models m JOIN agenteam_model.providers p ON p.id=m.provider_id WHERE m.id=$3 AND p.scope='project' AND p.project_id=$2 ON CONFLICT DO NOTHING`, f.modelReference, project, model)
			} else {
				_, err = x.Exec(ctx, `DELETE FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id=$1 AND role='agent_model' AND project_id=$2 AND model_id=$3`, f.modelReference, project, model)
			}
			if err != nil {
				return err
			}
			var present bool
			if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id=$1 AND role='agent_model' AND project_id=$2 AND model_id=$3)`, f.modelReference, project, model).Scan(&present); err != nil {
				return err
			}
			if present != (get("state") == "present") {
				return errors.New("owned reference auxiliary fact mismatch")
			}
			return nil
		})
		if committed.State() != foundation.Committed || ctx.Err() != nil {
			return fail("fixture_failed")
		}
		f.modelAux["referenced"]["reference_fact_state"] = get("state")
		result = map[string]any{"project_id": project, "model_id": model, "reference_present": get("state") == "present", "fixture_only": true}
	case "rename-reuse":
		current := f.setup.setupRequest(ctx, f.ownerClient, http.MethodGet, "/api/v1/projects/"+f.ids["main"], nil, "", false, 200)
		oldName := httpString(f.t, current, "name")
		f.setup.setupRequest(ctx, f.ownerClient, http.MethodPatch, "/api/v1/projects/"+f.ids["main"], map[string]any{"expected_version": current["version"], "name": "models-renamed-main"}, f.ownerCSRF, true, 200)
		replacement := f.create(ctx, f.ownerActor, oldName)
		f.mu.Lock()
		f.ids["reused"] = replacement.ID.String()
		f.mu.Unlock()
		f.modelAux["main"]["rename_reuse_applied"] = true
		result = map[string]any{"renamed": f.locator(ctx, "main"), "replacement": f.locator(ctx, "reused")}
	default:
		return fail("invalid_action")
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 64000 {
		return fail("fixture_failed")
	}
	reply["ok"], reply["result"] = true, result
	return reply
}

type projectModelsWebResult struct {
	Protocol     string          `json:"protocol"`
	InputHash    string          `json:"input_hash"`
	Completed    bool            `json:"completed"`
	Mode         string          `json:"mode"`
	Checks       map[string]bool `json:"checks"`
	Counts       json.RawMessage `json:"counts"`
	SchemaBodies int             `json:"schema_bodies"`
	ClientBodies int             `json:"client_bodies"`
	Layouts      int             `json:"layouts"`
}

func projectModelsWebChecks(mode string) ([]string, error) {
	raw, err := os.ReadFile("../../docs/development/work-items/d27-project-owner-model-settings-ui.md")
	if err != nil {
		return nil, err
	}
	_, body, ok := strings.Cut(string(raw), "```json\n{\n  \"files\":")
	if !ok {
		return nil, errors.New("formal browser result contract unavailable")
	}
	body, _, ok = strings.Cut("{\n  \"files\":"+body, "\n```")
	if !ok {
		return nil, errors.New("formal browser result contract invalid")
	}
	var contract struct {
		Checks map[string][]string `json:"checks_by_mode"`
	}
	if json.Unmarshal([]byte(body), &contract) != nil || len(contract.Checks[mode]) == 0 {
		return nil, errors.New("formal browser mode contract invalid")
	}
	return contract.Checks[mode], nil
}

func decodeProjectModelsWebResult(raw []byte, mode, inputHash string) (projectModelsWebResult, error) {
	var result projectModelsWebResult
	bad := errors.New("owned Project Models final evidence invalid")
	if _, err := projectModelsWebObject(raw, "protocol", "input_hash", "completed", "mode", "checks", "counts", "schema_bodies", "client_bodies", "layouts"); err != nil || json.Unmarshal(raw, &result) != nil || result.Protocol != projectModelsWebProtocol || result.InputHash != inputHash || !result.Completed || result.Mode != mode {
		return result, bad
	}
	checks, err := projectModelsWebChecks(mode)
	if err != nil || len(result.Checks) != len(checks) {
		return result, bad
	}
	for _, key := range checks {
		if !result.Checks[key] {
			return result, bad
		}
	}
	if result.SchemaBodies <= 0 || result.SchemaBodies > 512 || result.ClientBodies != result.SchemaBodies || mode == "navigation" && result.Layouts != 8 || mode != "navigation" && result.Layouts != 0 {
		return result, bad
	}
	counts, err := projectModelsWebObject(result.Counts, "server", "browser")
	if err != nil {
		return result, bad
	}
	browser, err := projectModelsWebObject(counts["browser"], "attempts", "complete_eof", "typed_client_ok", "schema_ok", "incomplete")
	if err != nil {
		return result, bad
	}
	observed := map[string]int{}
	for key, raw := range browser {
		var n int
		if json.Unmarshal(raw, &n) != nil || string(raw) == "null" || n < 0 || n > 512 {
			return result, bad
		}
		observed[key] = n
	}
	if observed["typed_client_ok"] != result.ClientBodies || observed["schema_ok"] != result.SchemaBodies || observed["complete_eof"] != observed["typed_client_ok"] || observed["complete_eof"] != observed["schema_ok"] || observed["complete_eof"] > observed["attempts"] || observed["incomplete"]+observed["complete_eof"] != observed["attempts"] {
		return result, bad
	}
	server, err := projectModelsWebObject(counts["server"], "operations", "session", "server", "controls", "browser_eof", "schema_bodies", "client_bodies")
	if err != nil {
		return result, bad
	}
	for _, key := range []string{"browser_eof", "schema_bodies", "client_bodies"} {
		if string(server[key]) != "null" {
			return result, bad
		}
	}
	var rows []json.RawMessage
	if json.Unmarshal(server["operations"], &rows) != nil || len(rows) != 17 {
		return result, bad
	}
	attachment, err := os.ReadFile("../../docs/development/work-items/d27-project-owner-model-settings-ui-endpoints.json")
	if err != nil {
		return result, bad
	}
	operations, err := projectModelsWebLoadOperations(attachment)
	if err != nil {
		return result, bad
	}
	for index, raw := range rows {
		fields, err := projectModelsWebObject(raw, "operation", "setup", "browser", "control", "upstream_complete", "handler_joined")
		if err != nil {
			return result, bad
		}
		operation, ok := projectModelsWebString(fields["operation"])
		if !ok || operation != operations[index].Operation {
			return result, bad
		}
		for key, raw := range fields {
			if key == "operation" {
				continue
			}
			var n int
			if json.Unmarshal(raw, &n) != nil || string(raw) == "null" || n < 0 {
				return result, bad
			}
		}
	}
	for _, entry := range []struct {
		key    string
		fields []string
	}{{"session", []string{"setup", "browser", "control"}}, {"server", []string{"started", "finished"}}, {"controls", []string{"armed", "claimed", "held", "held_joined", "cut", "disconnected"}}} {
		fields, err := projectModelsWebObject(server[entry.key], entry.fields...)
		if err != nil {
			return result, bad
		}
		for _, raw := range fields {
			var n int
			if json.Unmarshal(raw, &n) != nil || string(raw) == "null" || n < 0 {
				return result, bad
			}
		}
	}
	return result, nil
}

func (f *projectModelsWebFixture) browserModels(ctx context.Context) projectModelsWebResult {
	root := filepath.Clean(filepath.Join(f.webRoot, "../../../../tests/account-captcha-web"))
	if _, err := os.Stat(filepath.Join(root, "project-owner-models.config.js")); err != nil {
		f.t.Fatal("owned Project Models browser source unavailable beside private build")
	}
	browserCtx, stop := context.WithCancel(ctx)
	defer stop()
	cmd := exec.CommandContext(browserCtx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "project-owner-models.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+f.directory, "AGENTEAM_AUTH_WEB_ORIGIN="+f.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+f.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_PROJECT_MODELS_WEB_CASE="+f.mode, "AGENTEAM_PROJECT_MODELS_WEB_DIST="+f.webRoot, "AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE="+f.evidence, "AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH="+f.inputHash, "PLAYWRIGHT_NO_COPY_PROMPT=1")
	if f.mode == "navigation" {
		images := os.Getenv("AGENTEAM_AUTH_WEB_IMAGES")
		if !filepath.IsAbs(images) {
			f.t.Fatal("owned navigation images directory required")
		}
		cmd.Env = append(cmd.Env, "AGENTEAM_AUTH_WEB_IMAGES="+images)
	}
	// Playwright diagnostics may include user input on future failure paths.
	// Keep them private in memory; only closed result/exit facts are published.
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	f.mu.Lock()
	f.browserActive = true
	f.mu.Unlock()
	if err := cmd.Start(); err != nil {
		f.t.Fatal("owned Project Models browser runner could not start")
	}
	done := make(chan error, 1)
	joined := false
	defer func() {
		f.releaseAll()
		if !joined {
			_ = cmd.Cancel()
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		clear(output.Bytes())
		cmd.Env = nil
		f.t.Log("Project Models Node direct child actually waited; outer driver owns adopted descendants")
	}()
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()
	sequence := 0
	var previous []byte
	var runErr error
wait:
	for {
		select {
		case runErr = <-done:
			joined = true
			break wait
		case <-ticker.C:
			raw, err := projectModelsWebReadPrivate(filepath.Join(f.directory, "project-models-ipc.json"), 8192)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				f.t.Fatal("owned Project Models IPC private read rejected")
			}
			if bytes.Equal(previous, raw) {
				clear(raw)
				continue
			}
			request, code := decodeProjectModelsWebIPC(raw, f.inputHash, sequence+1)
			if code != "" {
				clear(raw)
				f.t.Fatal("owned Project Models IPC envelope rejected")
			}
			clear(previous)
			previous = raw
			sequence = request.Sequence
			ack := f.modelIPC(ctx, request)
			encoded, err := json.Marshal(ack)
			if err != nil || len(encoded) > 65536 {
				f.t.Fatal("owned Project Models IPC acknowledgement rejected")
			}
			f.private("project-models-ack-"+strconv.Itoa(sequence)+".json", ack)
			if ack["ok"] != true {
				stop()
			}
		}
	}
	clear(previous)
	if runErr != nil {
		f.t.Fatal("actual Project Models browser did not complete; private diagnostics suppressed")
	}
	raw, err := projectModelsWebReadPrivate(filepath.Join(f.directory, "project-models-result.json"), 65536)
	if err != nil {
		f.t.Fatal("actual Project Models browser final evidence unavailable")
	}
	defer clear(raw)
	result, err := decodeProjectModelsWebResult(raw, f.mode, f.inputHash)
	if err != nil {
		f.t.Fatal("actual Project Models browser final evidence rejected")
	}
	if err := f.verifyModelBrowserEvidence(result); err != nil {
		f.t.Fatal("actual Project Models browser body/counter evidence rejected")
	}
	f.safeEvidence("browser-result.json", result)
	return result
}

func (f *projectModelsWebFixture) verifyModelBrowserEvidence(result projectModelsWebResult) error {
	bad := errors.New("owned browser evidence mismatch")
	var counts struct {
		Server  json.RawMessage `json:"server"`
		Browser struct {
			Attempts   int `json:"attempts"`
			EOF        int `json:"complete_eof"`
			Incomplete int `json:"incomplete"`
		} `json:"browser"`
	}
	var actual, expected any
	if json.Unmarshal(result.Counts, &counts) != nil || json.Unmarshal(counts.Server, &actual) != nil || json.Unmarshal(f.modelLastCounts, &expected) != nil {
		return bad
	}
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(expected)
	if !bytes.Equal(a, b) {
		return bad
	}
	native, err := projectModelsWebReadPrivate(filepath.Join(f.evidence, "native-browser-observations.json"), 1048576)
	if err != nil || projectModelsWebJSON(native) != nil {
		return bad
	}
	var observations []json.RawMessage
	if json.Unmarshal(native, &observations) != nil || len(observations) != counts.Browser.Attempts {
		return bad
	}
	eof := map[string]map[string]json.RawMessage{}
	tokens := map[string]bool{}
	incomplete := 0
	for _, raw := range observations {
		fields, err := projectModelsWebObject(raw, "token", "method", "path", "query", "status", "eof", "ended", "cancelled", "released", "bytes", "chunks", "has_body", "mutation_headers")
		if err != nil || string(fields["ended"]) != "true" {
			return bad
		}
		for _, key := range []string{"eof", "cancelled", "released", "has_body", "mutation_headers"} {
			if string(fields[key]) != "true" && string(fields[key]) != "false" {
				return bad
			}
		}
		method, ok := projectModelsWebString(fields["method"])
		path, pathOK := projectModelsWebString(fields["path"])
		query, queryOK := projectModelsWebString(fields["query"])
		if !ok || !pathOK || !queryOK {
			return bad
		}
		_, _, operation := f.match(&http.Request{Method: method, URL: &url.URL{Path: path, RawQuery: query}})
		if operation == nil {
			return bad
		}
		var size, status int
		var chunks []int
		if string(fields["bytes"]) == "null" || string(fields["status"]) == "null" || string(fields["chunks"]) == "null" || json.Unmarshal(fields["bytes"], &size) != nil || size < 0 || size > 8388608 || json.Unmarshal(fields["status"], &status) != nil || status < 0 || status > 599 || json.Unmarshal(fields["chunks"], &chunks) != nil {
			return bad
		}
		total := 0
		for _, chunk := range chunks {
			if chunk < 0 || chunk > 8388608 {
				return bad
			}
			total += chunk
		}
		if total != size {
			return bad
		}
		token, _ := projectModelsWebString(fields["token"])
		if string(fields["token"]) != "null" {
			if !projectModelsWebRequestToken.MatchString(token) || tokens[token] {
				return bad
			}
			tokens[token] = true
		}
		if string(fields["eof"]) == "true" {
			if token == "" || string(fields["released"]) != "true" || size == 0 {
				return bad
			}
			eof[token] = fields
		} else {
			incomplete++
		}
	}
	if len(eof) != counts.Browser.EOF || len(eof) != result.SchemaBodies || incomplete != counts.Browser.Incomplete {
		return bad
	}
	raw, err := projectModelsWebReadPrivate(filepath.Join(f.evidence, "same-body-input.json"), 65536)
	if err != nil || projectModelsWebJSON(raw) != nil {
		return bad
	}
	var rows []json.RawMessage
	if json.Unmarshal(raw, &rows) != nil || len(rows) != result.SchemaBodies {
		return bad
	}
	seen := map[string]bool{}
	for _, raw := range rows {
		fields, err := projectModelsWebObject(raw, "sidecar", "request_token", "browser_eof", "bytes", "sha256")
		if err != nil {
			return bad
		}
		name, _ := projectModelsWebString(fields["sidecar"])
		token, _ := projectModelsWebString(fields["request_token"])
		digest, _ := projectModelsWebString(fields["sha256"])
		var n int
		if !regexp.MustCompile(`^response-[0-9]{3}\.json$`).MatchString(name) || !projectModelsWebRequestToken.MatchString(token) || seen[token] || string(fields["browser_eof"]) != "true" || json.Unmarshal(fields["bytes"], &n) != nil || n < 1 || n > 8388608 {
			return bad
		}
		seen[token] = true
		fact := eof[token]
		if fact == nil || !bytes.Equal(fields["bytes"], fact["bytes"]) {
			return bad
		}
		metadata, err := projectModelsWebReadPrivate(filepath.Join(f.evidence, name), 65536)
		if err != nil {
			return bad
		}
		var sidecar struct {
			Protocol, InputHash, Source, Token, SHA, BodyFile, Transfer, Stage string
			Bytes                                                              int
		}
		var value map[string]json.RawMessage
		if json.Unmarshal(metadata, &value) != nil {
			return bad
		}
		for sidecarKey, factKey := range map[string]string{"method": "method", "endpoint": "path", "query": "query", "status": "status"} {
			if sidecarKey == "status" {
				var left, right int
				if json.Unmarshal(value[sidecarKey], &left) != nil || json.Unmarshal(fact[factKey], &right) != nil || left != right {
					return bad
				}
			} else {
				left, leftOK := projectModelsWebString(value[sidecarKey])
				right, rightOK := projectModelsWebString(fact[factKey])
				if !leftOK || !rightOK || left != right {
					return bad
				}
			}
		}
		sidecar.Protocol, _ = projectModelsWebString(value["protocol"])
		sidecar.InputHash, _ = projectModelsWebString(value["input_hash"])
		sidecar.Source, _ = projectModelsWebString(value["source"])
		sidecar.Token, _ = projectModelsWebString(value["request_token"])
		sidecar.SHA, _ = projectModelsWebString(value["body_sha256"])
		sidecar.BodyFile, _ = projectModelsWebString(value["body_file"])
		sidecar.Transfer, _ = projectModelsWebString(value["transfer_kind"])
		sidecar.Stage, _ = projectModelsWebString(value["body_stage"])
		if json.Unmarshal(value["body_bytes"], &sidecar.Bytes) != nil || sidecar.Protocol != projectModelsWebProtocol || sidecar.InputHash != f.inputHash || sidecar.Source != "browser" || sidecar.Token != token || sidecar.SHA != digest || sidecar.BodyFile != "body-"+digest+".json" || sidecar.Bytes != n || sidecar.Transfer != "forwarded" && sidecar.Transfer != "hold" || sidecar.Stage != "complete_formal_upstream" {
			return bad
		}
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size {
			return bad
		}
		body, err := projectModelsWebReadPrivate(filepath.Join(f.evidence, sidecar.BodyFile), 8388608)
		if err != nil || len(body) != n || fmt.Sprintf("%x", sha256.Sum256(body)) != digest {
			return bad
		}
	}
	return nil
}
