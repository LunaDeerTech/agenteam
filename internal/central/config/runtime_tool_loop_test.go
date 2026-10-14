package config

import (
	"strings"
	"testing"
)

func TestExecutionRuntimeToolLoopSelectionPreservesPolicy(t *testing.T) {
	for _, profile := range []string{"direct-text-v1", "tool-loop-v1"} {
		t.Run(profile, func(t *testing.T) {
			raw := strings.Replace(runtimeConfigJSON, "direct-text-v1", profile, 1)
			cfg, err := loadValues(runtimeConfigValues(raw))
			if err != nil {
				t.Fatal("explicit execution profile rejected", err)
			}
			options, present := cfg.ExecutionRuntime()
			if !present || options.Validate() != nil || options.ToolLoop() != (profile == "tool-loop-v1") {
				t.Fatal("execution profile selection changed")
			}
			policy := options.ProjectRunners().LaunchPolicy
			if len(policy.DeniedToolIDs) != 2 || len(policy.AllowedResourceConstraints) != 0 {
				t.Fatal("profile changed the explicit tool policy")
			}
		})
	}
	if (ExecutionRuntimeOptions{}).ToolLoop() {
		t.Fatal("absent configuration enabled the tool loop")
	}
}
