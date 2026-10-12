package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	"github.com/LunaDeerTech/agenteam/internal/runner/config"
	"github.com/LunaDeerTech/agenteam/internal/runner/control"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// This uses the existing process-owner seam only to inspect the real config
// handoff. Native TLS, enrollment and filesystem success belong to the CLI
// fixture; heldClient does not simulate those successes.
func TestRunnerRootPassesClosedOperations(t *testing.T) {
	for _, tc := range []struct {
		name, operations string
		enroll           bool
	}{
		{"default", "", false},
		{"stored-read-file", p.ReadFileOperation, false},
		{"enrollment-read-file", p.ReadFileOperation, true},
		{"stored-list", p.ListDirectoryOperation, false},
		{"stored-find", p.FindFilesOperation, false},
		{"stored-all", p.FindFilesOperation + "," + p.ReadFileOperation + "," + p.ListDirectoryOperation, false},
		{"enrollment-directory", p.ListDirectoryOperation + "," + p.FindFilesOperation, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := processConfig(t)
			values := map[string]string{
				config.Prefix + "IDENTITY_FILE":    original.IdentityFile(),
				config.Prefix + "SHUTDOWN_TIMEOUT": "100ms",
				config.Prefix + "OPERATIONS":       tc.operations,
			}
			var env []string
			for key, value := range values {
				env = append(env, key+"="+value)
			}
			cfg, err := config.Load(func(key string) (string, bool) { v, ok := values[key]; return v, ok }, env)
			if err != nil {
				t.Fatal(err)
			}
			output := &eventWriter{}
			logger, err := logging.New(logging.Runner, slog.LevelInfo, output)
			if err != nil {
				t.Fatal(err)
			}
			owner := &heldClient{entered: make(chan struct{}), returned: make(chan struct{}), stop: make(chan struct{}), forced: make(chan context.Context, 1), drained: make(chan context.Context, 1)}
			captured := make(chan control.Options, 1)
			construct := func(options control.Options) (clientOwner, error) {
				owner.options = options
				captured <- options
				return owner, nil
			}
			var input io.ReadCloser
			if tc.enroll {
				input = io.NopCloser(strings.NewReader("unconsumed-by-control"))
				defer input.Close()
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				if !joined {
					_ = appResult(t, done)
				}
			})
			go func() { done <- run(ctx, cfg, logger, nil, input, construct) }()
			appWait(t, owner.entered)
			options := <-captured
			if options.Operations != tc.operations || options.IdentityFile != original.IdentityFile() || options.Configuration != nil || (options.EnrollmentSource != nil) != tc.enroll {
				t.Error("real App changed operation selection or stored-only ownership at its factory boundary")
			}
			cancel()
			err = appResult(t, done)
			joined = true
			if err != nil {
				t.Fatal(err)
			}
			appWait(t, owner.returned)
			if !strings.Contains(output.String(), `"outcome":"drained"`) {
				t.Fatal("root did not observe its original client return")
			}
		})
	}
}
