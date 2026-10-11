package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
)

const executionRuntimeSetting = "EXECUTION_RUNTIME"
const directTextRuntimeProfile = "direct-text-v1"
const maxExecutionRuntimeBytes = 64 * 1024

// ExecutionRuntimeOptions contains explicit deployment inputs, not domain
// authority. Loading it does not start workers, initialize Projects, register
// tools or alter any Agent capability. This profile supports the existing
// direct-text path; the actual captured facts still determine eligibility.
type ExecutionRuntimeOptions struct{ data func() executionRuntimeData }
type executionRuntimeData struct {
	profile  string
	projects scheduler.ProjectRunnersOptions
	executor execution.AssociatedExecutorOptions
	retry    mc.AgentRetryTiming
}

func newExecutionRuntimeOptions(data executionRuntimeData) ExecutionRuntimeOptions {
	data.projects.LaunchPolicy = data.projects.LaunchPolicy.Clone()
	return ExecutionRuntimeOptions{data: func() executionRuntimeData { return data }}
}
func (v ExecutionRuntimeOptions) state() executionRuntimeData {
	if v.data == nil {
		return executionRuntimeData{}
	}
	return v.data()
}

// ExecutionRuntime is absent unless the complete explicit configuration was
// supplied. It never supplies a default policy or derives denied tool IDs.
func (c Config) ExecutionRuntime() (ExecutionRuntimeOptions, bool) {
	if c.executionRuntime == nil {
		return ExecutionRuntimeOptions{}, false
	}
	return newExecutionRuntimeOptions(c.executionRuntime.state()), true
}

func (v ExecutionRuntimeOptions) ProjectRunners() scheduler.ProjectRunnersOptions {
	result := v.state().projects
	result.LaunchPolicy = result.LaunchPolicy.Clone()
	return result
}
func (v ExecutionRuntimeOptions) AssociatedExecutor() execution.AssociatedExecutorOptions {
	return v.state().executor
}
func (v ExecutionRuntimeOptions) AgentRetryTiming() mc.AgentRetryTiming {
	return v.state().retry.Clone()
}

func (v ExecutionRuntimeOptions) Validate() error {
	d := v.state()
	p, e := d.projects, d.executor
	if d.profile != directTextRuntimeProfile || p.MaxProjects < 1 || p.ProjectPageSize < 1 || p.ProjectPageSize > pc.MaxSchedulerProjectPageSize || p.ExecutionPageSize < 1 || p.ExecutionPageSize > scheduler.MaxExecutionHandoffPage || p.DiscoveryInterval <= 0 || p.TickInterval <= 0 || p.RelaunchSkipCount < 0 || p.LaunchPolicy.Validate() != nil || len(p.LaunchPolicy.AllowedResourceConstraints) != 0 || e.MaxOwned < 1 || e.RecoveryInterval <= 0 || d.retry.Validate() != nil || d.retry.Fields().MaxRequestTimeout > 120*time.Second {
		return invalid(executionRuntimeSetting)
	}
	return nil
}

func (ExecutionRuntimeOptions) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_runtime_options")
}
func (ExecutionRuntimeOptions) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_runtime_options"`), nil
}
func (ExecutionRuntimeOptions) LogValue() slog.Value {
	return slog.StringValue("execution_runtime_options")
}

type executionRuntimeWire struct {
	Profile            string          `json:"profile"`
	ProjectRunners     json.RawMessage `json:"project_runners"`
	AssociatedExecutor json.RawMessage `json:"associated_executor"`
	ModelAgentRetry    json.RawMessage `json:"model_agent_retry"`
}
type runtimeProjectsWire struct {
	MaxProjects       int             `json:"max_projects"`
	ProjectPageSize   int             `json:"project_page_size"`
	ExecutionPageSize int             `json:"execution_page_size"`
	DiscoveryInterval string          `json:"discovery_interval"`
	TickInterval      string          `json:"tick_interval"`
	RelaunchSkipCount int64           `json:"relaunch_skip_count"`
	LaunchPolicy      json.RawMessage `json:"launch_policy"`
}
type runtimeExecutorWire struct {
	MaxOwned         int    `json:"max_owned"`
	RecoveryInterval string `json:"recovery_interval"`
}
type runtimeRetryWire struct {
	InitialRequestTimeout string `json:"initial_request_timeout"`
	MaxRequestTimeout     string `json:"max_request_timeout"`
	TimeoutMultiplier     uint32 `json:"timeout_multiplier"`
	InitialBackoff        string `json:"initial_backoff"`
	MaxBackoff            string `json:"max_backoff"`
}

// runtimeObject checks exact names and required, non-null members before typed
// decoding. encoding/json alone also accepts case-insensitive aliases. The
// whole document has already passed CanonicalJSON's duplicate/integer checks.
func runtimeObject(raw []byte, out any, keys ...string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != len(keys) {
		return false
	}
	for _, key := range keys {
		value, exists := fields[key]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(out) == nil
}

func loadExecutionRuntime(lookup LookupEnv) (*ExecutionRuntimeOptions, error) {
	// Snapshot this explicit opt-in once. Empty is not absence.
	raw, present := lookup(Prefix + executionRuntimeSetting)
	if !present {
		return nil, nil
	}
	if len(raw) == 0 || len(raw) > maxExecutionRuntimeBytes {
		return nil, invalid(executionRuntimeSetting)
	}
	canonical, err := cursor.CanonicalJSON([]byte(raw))
	if err != nil {
		return nil, invalid(executionRuntimeSetting)
	}
	var wire executionRuntimeWire
	var projects runtimeProjectsWire
	var executor runtimeExecutorWire
	var retry runtimeRetryWire
	var policy ec.Policy
	if !runtimeObject(canonical, &wire, "profile", "project_runners", "associated_executor", "model_agent_retry") ||
		!runtimeObject(wire.ProjectRunners, &projects, "max_projects", "project_page_size", "execution_page_size", "discovery_interval", "tick_interval", "relaunch_skip_count", "launch_policy") ||
		!runtimeObject(wire.AssociatedExecutor, &executor, "max_owned", "recovery_interval") ||
		!runtimeObject(wire.ModelAgentRetry, &retry, "initial_request_timeout", "max_request_timeout", "timeout_multiplier", "initial_backoff", "max_backoff") ||
		!runtimeObject(projects.LaunchPolicy, &policy, "schema_version", "denied_tool_ids", "allowed_resource_constraints") {
		return nil, invalid(executionRuntimeSetting)
	}
	var values [7]time.Duration
	for n, raw := range []string{projects.DiscoveryInterval, projects.TickInterval, executor.RecoveryInterval, retry.InitialRequestTimeout, retry.MaxRequestTimeout, retry.InitialBackoff, retry.MaxBackoff} {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			return nil, invalid(executionRuntimeSetting)
		}
		values[n] = value
	}
	timing, err := mc.NewAgentRetryTiming(mc.AgentRetryTimingFields{InitialRequestTimeout: values[3], MaxRequestTimeout: values[4], TimeoutMultiplier: retry.TimeoutMultiplier, InitialBackoff: values[5], MaxBackoff: values[6]})
	if err != nil {
		return nil, invalid(executionRuntimeSetting)
	}
	result := newExecutionRuntimeOptions(executionRuntimeData{
		profile:  wire.Profile,
		projects: scheduler.ProjectRunnersOptions{MaxProjects: projects.MaxProjects, ProjectPageSize: projects.ProjectPageSize, ExecutionPageSize: projects.ExecutionPageSize, DiscoveryInterval: values[0], TickInterval: values[1], RelaunchSkipCount: projects.RelaunchSkipCount, LaunchPolicy: policy.Clone()},
		executor: execution.AssociatedExecutorOptions{MaxOwned: executor.MaxOwned, RecoveryInterval: values[2]},
		retry:    timing,
	})
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return &result, nil
}
