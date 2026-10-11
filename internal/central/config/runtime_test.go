package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const runtimeConfigJSON = `{
	"profile":"direct-text-v1",
	"project_runners":{
		"max_projects":3,"project_page_size":64,"execution_page_size":128,
		"discovery_interval":"2s","tick_interval":"250ms","relaunch_skip_count":0,
		"launch_policy":{"schema_version":"1","denied_tool_ids":["018f1234-5678-7000-8000-000000000001","018f1234-5678-7000-8000-000000000002"],"allowed_resource_constraints":[]}
	},
	"associated_executor":{"max_owned":4,"recovery_interval":"500ms"},
	"model_agent_retry":{"initial_request_timeout":"30s","max_request_timeout":"120s","timeout_multiplier":2,"initial_backoff":"200ms","max_backoff":"3s"}
}`

func runtimeConfigValues(raw string) map[string]string {
	values := retryConfigValues("2", "100ms", "1s")
	values[Prefix+executionRuntimeSetting] = raw
	return values
}

// Only changes the wire input to the public Config loader. No runtime or
// authority is constructed, and the ordinary config-only providers are reused.
func runtimeConfigEdit(t *testing.T, path string, value any, remove bool) string {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal([]byte(runtimeConfigJSON), &object); err != nil {
		t.Fatal(err)
	}
	keys := strings.Split(path, ".")
	current := object
	for _, key := range keys[:len(keys)-1] {
		var ok bool
		current, ok = current[key].(map[string]any)
		if !ok {
			t.Fatal("invalid controlled configuration path")
		}
	}
	if remove {
		delete(current, keys[len(keys)-1])
	} else {
		current[keys[len(keys)-1]] = value
	}
	raw, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestExecutionRuntimeConfigurationAbsentAndExplicit(t *testing.T) {
	for _, values := range []map[string]string{nil, retryConfigValues("2", "100ms", "1s")} {
		cfg, err := loadValues(values)
		if err != nil || cfg.Validate() != nil {
			t.Fatal("runtime absence changed existing configuration", err)
		}
		if options, present := cfg.ExecutionRuntime(); present || options.Validate() == nil {
			t.Fatal("absence manufactured enabled runtime defaults")
		}
	}
	cfg, err := loadValues(runtimeConfigValues(runtimeConfigJSON))
	if err != nil || cfg.Validate() != nil {
		t.Fatal("complete explicit runtime rejected", err)
	}
	options, present := cfg.ExecutionRuntime()
	if !present || options.Validate() != nil {
		t.Fatal("runtime not exposed as validated options")
	}
	p := options.ProjectRunners()
	if p.MaxProjects != 3 || p.ProjectPageSize != 64 || p.ExecutionPageSize != 128 || p.DiscoveryInterval != 2*time.Second || p.TickInterval != 250*time.Millisecond || p.RelaunchSkipCount != 0 {
		t.Fatal("explicit manager inputs changed")
	}
	if p.LaunchPolicy.SchemaVersion != 1 || len(p.LaunchPolicy.DeniedToolIDs) != 2 || p.LaunchPolicy.DeniedToolIDs[0].String() != "018f1234-5678-7000-8000-000000000001" || p.LaunchPolicy.AllowedResourceConstraints == nil || len(p.LaunchPolicy.AllowedResourceConstraints) != 0 {
		t.Fatal("explicit original policy changed")
	}
	e := options.AssociatedExecutor()
	if e.MaxOwned != 4 || e.RecoveryInterval != 500*time.Millisecond {
		t.Fatal("explicit executor limits changed")
	}
	m := options.AgentRetryTiming().Fields()
	if m.InitialRequestTimeout != 30*time.Second || m.MaxRequestTimeout != 120*time.Second || m.TimeoutMultiplier != 2 || m.InitialBackoff != 200*time.Millisecond || m.MaxBackoff != 3*time.Second {
		t.Fatal("explicit model timing changed")
	}
	launch, present := cfg.SchedulerLaunchRetryPolicy()
	if !present || launch.Validate() != nil {
		t.Fatal("runtime lost the separate bound Launch retry policy")
	}
	// Every public projection owns its slices, and neither receiving a copy
	// nor changing its ordinary value fields can modify the loaded options.
	p.LaunchPolicy.DeniedToolIDs[0] = p.LaunchPolicy.DeniedToolIDs[1]
	p.LaunchPolicy.AllowedResourceConstraints = append(p.LaunchPolicy.AllowedResourceConstraints, json.RawMessage(`{}`))
	p.MaxProjects, e.MaxOwned, m.TimeoutMultiplier = 99, 99, 99
	if got := options.ProjectRunners(); got.MaxProjects != 3 || got.LaunchPolicy.Validate() != nil || len(got.LaunchPolicy.AllowedResourceConstraints) != 0 {
		t.Fatal("project option getter aliased its policy")
	}
	data := options.state()
	data.projects.LaunchPolicy.DeniedToolIDs[0] = data.projects.LaunchPolicy.DeniedToolIDs[1]
	again, _ := cfg.ExecutionRuntime()
	if again.Validate() != nil || again.AssociatedExecutor().MaxOwned != 4 || again.AgentRetryTiming().Fields().TimeoutMultiplier != 2 {
		t.Fatal("Config getter aliased its stored options")
	}
	// An explicit empty deny list remains empty. Deployment parsing cannot
	// invent IDs or claim that such a policy will pass actual tool capture.
	empty, err := loadValues(runtimeConfigValues(runtimeConfigEdit(t, "project_runners.launch_policy.denied_tool_ids", []string{}, false)))
	if err != nil {
		t.Fatal(err)
	}
	noDeny, _ := empty.ExecutionRuntime()
	if ids := noDeny.ProjectRunners().LaunchPolicy.DeniedToolIDs; ids == nil || len(ids) != 0 {
		t.Fatal("empty policy was defaulted or lost its explicit array")
	}
}

func TestExecutionRuntimeConfigurationRejectsIncompleteAndUnsupported(t *testing.T) {
	reject := func(raw string) {
		t.Helper()
		cfg, err := loadValues(runtimeConfigValues(raw))
		var issue *Error
		if !errors.As(err, &issue) || issue.Field() != Prefix+executionRuntimeSetting || issue.Reason() != "invalid" || errors.Unwrap(err) != nil {
			t.Fatal("runtime rejection lost safe field classification")
		}
		if _, present := cfg.ExecutionRuntime(); present || cfg.HTTPAddr() != "" {
			t.Fatal("rejected runtime returned partial configuration")
		}
		assertConfigProjectionSafe(t, err)
	}
	for _, path := range []string{
		"profile", "project_runners", "associated_executor", "model_agent_retry",
		"project_runners.max_projects", "project_runners.project_page_size", "project_runners.execution_page_size", "project_runners.discovery_interval", "project_runners.tick_interval", "project_runners.relaunch_skip_count", "project_runners.launch_policy",
		"associated_executor.max_owned", "associated_executor.recovery_interval",
		"model_agent_retry.initial_request_timeout", "model_agent_retry.max_request_timeout", "model_agent_retry.timeout_multiplier", "model_agent_retry.initial_backoff", "model_agent_retry.max_backoff",
		"project_runners.launch_policy.schema_version", "project_runners.launch_policy.denied_tool_ids", "project_runners.launch_policy.allowed_resource_constraints",
	} {
		reject(runtimeConfigEdit(t, path, nil, true))
		reject(runtimeConfigEdit(t, path, nil, false))
	}
	for _, raw := range []string{
		"", "null", `[]`, runtimeConfigJSON + `{}`, strings.Repeat(" ", maxExecutionRuntimeBytes+1), string([]byte{0xff}),
		strings.Replace(runtimeConfigJSON, `"profile":`, `"profile":"direct-text-v1","profile":`, 1),
		strings.Replace(runtimeConfigJSON, `"max_owned":4`, `"max_owned":4,"max_owned":4`, 1),
		strings.Replace(runtimeConfigJSON, `"profile":`, `"PROFILE":`, 1),
		strings.Replace(runtimeConfigJSON, `"schema_version":`, `"SCHEMA_VERSION":`, 1),
	} {
		reject(raw)
	}
	for _, change := range []struct {
		path  string
		value any
	}{
		{"future", "raw-SENTINEL"}, {"project_runners.future", "raw-SENTINEL"}, {"project_runners.launch_policy.future", "raw-SENTINEL"},
		{"profile", "unknown-SENTINEL"}, {"profile", true}, {"associated_executor", []any{}},
		{"project_runners.max_projects", 0}, {"project_runners.max_projects", "3"}, {"project_runners.max_projects", 1.5},
		{"project_runners.project_page_size", 129}, {"project_runners.execution_page_size", 0}, {"project_runners.execution_page_size", 129}, {"project_runners.relaunch_skip_count", -1},
		{"project_runners.discovery_interval", "0s"}, {"project_runners.tick_interval", 250}, {"project_runners.tick_interval", "9223372036854775808ns"},
		{"associated_executor.max_owned", 0}, {"associated_executor.recovery_interval", "-1s"},
		{"model_agent_retry.initial_request_timeout", "121s"}, {"model_agent_retry.max_request_timeout", "121s"}, {"model_agent_retry.max_request_timeout", "29s"},
		{"model_agent_retry.timeout_multiplier", 1}, {"model_agent_retry.timeout_multiplier", uint64(1) << 32}, {"model_agent_retry.max_backoff", "199ms"},
		{"project_runners.launch_policy.schema_version", 1}, {"project_runners.launch_policy.schema_version", "2"},
		{"project_runners.launch_policy.denied_tool_ids", []string{"tool-SENTINEL"}},
		{"project_runners.launch_policy.denied_tool_ids", []string{"018f1234-5678-7000-8000-000000000002", "018f1234-5678-7000-8000-000000000001"}},
		{"project_runners.launch_policy.denied_tool_ids", []string{"018f1234-5678-7000-8000-000000000001", "018f1234-5678-7000-8000-000000000001"}},
		{"project_runners.launch_policy.allowed_resource_constraints", []any{map[string]any{}}},
	} {
		reject(runtimeConfigEdit(t, change.path, change.value, false))
	}
	for _, missing := range []string{"all", schedulerRetryMaxAttempts, schedulerRetryInitialBackoff, schedulerRetryMaxBackoff} {
		values := runtimeConfigValues(runtimeConfigJSON)
		for _, key := range []string{schedulerRetryMaxAttempts, schedulerRetryInitialBackoff, schedulerRetryMaxBackoff} {
			if missing == "all" || missing == key {
				delete(values, Prefix+key)
			}
		}
		cfg, err := loadValues(values)
		var issue *Error
		field := missing
		if missing == "all" {
			field = schedulerRetryMaxAttempts
		}
		if !errors.As(err, &issue) || issue.Field() != Prefix+field || errors.Unwrap(err) != nil {
			t.Fatal("runtime accepted missing explicit Launch retry settings")
		}
		if _, present := cfg.ExecutionRuntime(); present {
			t.Fatal("missing retry settings exposed enabled runtime")
		}
	}
}

func TestExecutionRuntimeConfigurationSnapshotAndSafeValidation(t *testing.T) {
	values := configValues(runtimeConfigValues(runtimeConfigJSON))
	var env []string
	for key := range values {
		env = append(env, key)
	}
	reads := 0
	cfg, err := Load(func(key string) (string, bool) {
		if key == Prefix+executionRuntimeSetting {
			reads++
			if reads > 1 {
				return "changed-SENTINEL", true
			}
		}
		value, present := values[key]
		return value, present
	}, env)
	if err != nil || reads != 1 || cfg.Validate() != nil {
		t.Fatal("loader did not retain its single explicit runtime snapshot", err)
	}
	values[Prefix+executionRuntimeSetting] = "later-SENTINEL"
	options, present := cfg.ExecutionRuntime()
	if !present || options.Validate() != nil || reads != 1 {
		t.Fatal("getter reread deployment input")
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		if got := fmt.Sprintf(format, options); got != "execution_runtime_options" {
			t.Fatal("runtime options exposed configuration values")
		}
	}
	encoded, err := json.Marshal(options)
	if err != nil || string(encoded) != `"execution_runtime_options"` {
		t.Fatal("runtime options did not use safe JSON projection")
	}
	data := options.state()
	data.profile = "private-SENTINEL"
	options = newExecutionRuntimeOptions(data)
	assertConfigProjectionSafe(t, options)
	if options.Validate() == nil || (ExecutionRuntimeOptions{}).Validate() == nil {
		t.Fatal("invalid opaque options passed validation")
	}
	broken := cfg
	broken.executionRuntime = &options
	if broken.Validate() == nil {
		t.Fatal("Config validation ignored invalid runtime options")
	}
	broken = cfg
	broken.launchRetry = nil
	if broken.Validate() == nil {
		t.Fatal("Config validation ignored missing explicit Launch retry")
	}
	if cfg.Validate() != nil {
		t.Fatal("validation modified the original configuration")
	}
}
