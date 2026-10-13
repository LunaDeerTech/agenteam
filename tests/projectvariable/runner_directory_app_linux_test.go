//go:build integration && linux

package projectvariable_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// The independently prebuilt original cmd is mandatory, as in ReadFileApp.
// The shared fixture supplies real system Runner registration, Owner Agent
// runner_mounts, authenticated TLS/WSS, and process/handler/identity cleanup.
// Only the still-unbound Execution/Operation producer is the strict D15 §7
// authority substitute. Agent configuration itself is not execution authority.
func TestRunnerDirectoryApp(t *testing.T) {
	v := newRunnerReadFileAppFixture(t, runnerReadFileAppBinary(t))
	root := v.configuration.RootPath
	data := filepath.Join(root, v.intent.ProjectID.String(), v.intent.AgentID.String(), "source", "src")
	readFileAdapterOK(t, os.MkdirAll(filepath.Join(data, "nested"), 0700), "private directory test data")
	for _, name := range []string{"a.go", "info.txt", "nested/b.go"} {
		readFileAdapterOK(t, os.WriteFile(filepath.Join(data, name), []byte("owned directory bytes"), 0600), "private directory file")
	}
	if !t.Run("selected-directory-reads-and-identity-restart", func(t *testing.T) {
		selection := "find-files,list-directory"
		first := runnerReadFileAppLaunch(t, v.binary, v.env(&selection, true), []string{"--enroll"}, &v.token)
		runnerDirectoryAppOnline(t, v, first, 1, []string{p.ListDirectoryOperation, p.FindFilesOperation})
		original := v.identity(t, true)
		runnerDirectoryAppDispatch(t, v, p.ListDirectoryOperation, true)
		runnerDirectoryAppDispatch(t, v, p.FindFilesOperation, true)
		v.dispatch(t, false, 64)
		v.stop(t, first)

		// Changing only the selected reads reuses the locked stored identity. No
		// token, device/root override, test factory or second Manager is supplied.
		selection = "find-files,read-file,list-directory"
		second := runnerReadFileAppLaunch(t, v.binary, v.env(&selection, false), nil, nil)
		runnerDirectoryAppOnline(t, v, second, 2, []string{p.ReadFileOperation, p.ListDirectoryOperation, p.FindFilesOperation})
		if v.identity(t, true) != original {
			t.Fatal("read capability selection changed the persisted Runner identity")
		}
		runnerDirectoryAppDispatch(t, v, p.ListDirectoryOperation, true)
		runnerDirectoryAppDispatch(t, v, p.FindFilesOperation, true)
		v.dispatch(t, true, 64)
		v.stop(t, second)
		t.Log("directory_cli_enrollment=1 identity_only_restart=1 list_reads=2 find_reads=2 combined_read_file=1 omitted_read_file_unsupported=1 original_waits=2")
	}) {
		return
	}
	t.Run("selection-isolation-and-before-device-validation", func(t *testing.T) {
		selection := "list-directory"
		one := runnerReadFileAppLaunch(t, v.binary, v.env(&selection, false), nil, nil)
		runnerDirectoryAppOnline(t, v, one, 3, []string{p.ListDirectoryOperation})
		runnerDirectoryAppDispatch(t, v, p.ListDirectoryOperation, true)
		runnerDirectoryAppDispatch(t, v, p.FindFilesOperation, false)
		v.dispatch(t, false, 64)
		v.stop(t, one)

		readFileAdapterOK(t, os.Rename(root, root+".held"), "temporarily absent private deployment root")
		t.Cleanup(func() {
			if os.Rename(root+".held", root) != nil {
				t.Error("original deployment root restoration failed")
			}
		})
		// Default startup still has no filesystem owner, even with a missing root.
		empty := runnerReadFileAppLaunch(t, v.binary, v.env(nil, false), nil, nil)
		runnerDirectoryAppOnline(t, v, empty, 4, []string{})
		runnerDirectoryAppDispatch(t, v, p.ListDirectoryOperation, false)
		runnerDirectoryAppDispatch(t, v, p.FindFilesOperation, false)
		v.stop(t, empty)

		original, facts, counts := v.identity(t, false), v.facts(t), v.countsNow()
		check := runnerReadFileAppLaunch(t, v.binary, v.env(&selection, false), []string{"--check-config"}, nil)
		check.wait(t, 0, true)
		check.noLeaks(t, v.token, v.seed, v.path, root)
		badRoot := runnerReadFileAppLaunch(t, v.binary, v.env(&selection, false), nil, nil)
		badRoot.wait(t, 1, false)
		badRoot.noLeaks(t, v.token, v.seed, v.path, root)
		mismatch := v.env(&selection, true)
		mismatch[len(mismatch)-1] = "AGENTEAM_RUNNER_ROOT_PATH=" + root + ".different"
		conflict := runnerReadFileAppLaunch(t, v.binary, mismatch, nil, nil)
		conflict.wait(t, 2, false)
		conflict.noLeaks(t, v.token, v.seed, v.path, root)
		for _, invalid := range []string{"list-directory,run-command", "find-files,find-files"} {
			process := runnerReadFileAppLaunch(t, v.binary, v.env(&invalid, false), nil, nil)
			process.wait(t, 2, false)
			process.noLeaks(t, v.token, v.seed, v.path, root)
		}
		v.idle(t)
		if v.identity(t, false) != original || v.facts(t) != facts || v.countsNow() != counts {
			t.Fatal("offline or rejected selection changed identity or entered the device protocol")
		}
		if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("default/offline/rejected operation constructed the absent root")
		}
		// Zero protocol/announcement delta is not a measurement of TCP SYNs.
		t.Log("list_only=1 undeclared_find_unsupported=1 default_empty=1 default_directory_unsupported=2 offline_check=1 invalid_before_device=4 device_protocol_delta=0 identity_unchanged=true original_waits=7")
	})
}

func runnerDirectoryAppOnline(t *testing.T, v *runnerReadFileAppFixture, process *runnerReadFileAppProcess, generation int64, want []string) {
	t.Helper()
	process.connected(t)
	v.identity(t, true)
	target, err := f.ParseID[rc.Runner](string(v.configuration.RunnerID))
	readFileAdapterOK(t, err, "original directory Runner identity")
	snapshot, err := v.runners.Get(ctxFor(t), v.setup.agent.p2.base.adminBrowser.actor, target)
	readFileAdapterOK(t, err, "real selected Hello")
	stored, err := identity.ReadOnly(v.path)
	readFileAdapterOK(t, err, "original public identity")
	public, err := stored.PublicKey()
	readFileAdapterOK(t, err, "original public key")
	if snapshot.Status != rc.Online || snapshot.Version != 2 || snapshot.CredentialGeneration != 1 || snapshot.PublicKeyFingerprint == nil || string(*snapshot.PublicKeyFingerprint) != p.PublicKeyFingerprint(public) || snapshot.LastHello == nil || !slices.Equal(snapshot.LastHello.Capabilities, want) || len(snapshot.LastHello.FeatureFlags) != 0 || v.controls.Load() != 1 {
		t.Fatal("selected capabilities or original authenticated WSS owner differ")
	}
	var current string
	var actual int64
	err = v.setup.agent.p2.base.raw.QueryRow(ctxFor(t), `SELECT id::text,generation FROM agenteam_runner.connections WHERE runner_id=$1 AND hello_at IS NOT NULL AND lease_expires_at>clock_timestamp()`, string(v.configuration.RunnerID)).Scan(&current, &actual)
	readFileAdapterOK(t, err, "current original WSS identity")
	if actual != generation || current == "" || current == v.connection {
		t.Fatal("directory restart reused a prior connection identity")
	}
	v.connection = current
	facts := v.facts(t)
	if facts[0] != generation || facts[1] != 1 || facts[2] != 1 || facts[3] != 1 || facts[4] != 2 || facts[5] != 2 || v.countsNow() != [3]int64{1, generation, generation} {
		t.Fatal("directory restart changed enrollment or generation ownership")
	}
}

func runnerDirectoryAppDispatch(t *testing.T, v *runnerReadFileAppFixture, operation string, enabled bool) {
	t.Helper()
	in := mc.DirectoryIntent{Actor: v.intent.Actor, ProjectID: v.intent.ProjectID, AgentID: v.intent.AgentID, ExecutionID: v.intent.ExecutionID, OperationID: id[tc.Operation](t), MountID: v.intent.MountID, Operation: operation, Arguments: p.DirectoryArguments{Path: "src", Limit: 8}}
	if operation == p.FindFilesOperation {
		in.Arguments.Pattern, in.Arguments.Match = "*.go", "name"
	}
	// Preserve the existing strict substitute at its precise unbound layer;
	// production current Agent + Mount resolver + committed claim are exercised.
	owner := &directoryAdapterAuthority{store: v.setup.agent.p2.base.tracked, intents: map[tc.OperationID]mc.DirectoryIntent{in.OperationID: in}}
	current, err := agent.NewExecutionConfiguration(v.setup.agent.providers.Agents, owner)
	readFileAdapterOK(t, err, "actual current Agent reader")
	resolver, err := mount.NewDirectoryResolver(v.setup.agent.p2.base.tracked, current, owner)
	readFileAdapterOK(t, err, "actual current directory resolver")
	dispatcher, err := runnerhttp.NewDirectoryDispatcher(v.device, resolver)
	readFileAdapterOK(t, err, "actual directory dispatcher")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticket, err := resolver.Resolve(ctx, in)
	readFileAdapterOK(t, err, "original directory ticket")
	result, err := dispatcher.Dispatch(ctx, ticket)
	if !enabled {
		requireCode(t, err, f.CapabilityUnsupported)
		if result.Attempted || result.Terminal != nil {
			t.Fatal("undeclared directory capability reached dispatch")
		}
		return
	}
	readFileAdapterOK(t, err, "original CLI directory read")
	if !result.Attempted || result.Terminal == nil || result.Terminal.Outcome != p.Success || result.Terminal.OperationID != p.ID(in.OperationID.String()) || !result.Terminal.RequestID.Valid() || v.requests[result.Terminal.RequestID] {
		t.Fatal("actual directory terminal lost its unique request/operation identity")
	}
	v.requests[result.Terminal.RequestID] = true
	if operation == p.ListDirectoryOperation {
		value, err := p.DecodeListDirectoryResult(result.Terminal.Payload)
		if err != nil || value.Path != "src" || value.Truncated || !slices.Equal(value.Entries, []p.DirectoryEntry{{Name: "a.go", Kind: "file"}, {Name: "info.txt", Kind: "file"}, {Name: "nested", Kind: "directory"}}) {
			t.Fatal("actual CLI List differs from independent private directory entries")
		}
	} else {
		value, err := p.DecodeFindFilesResult(result.Terminal.Payload)
		if err != nil || value.Path != "src" || value.Truncated || !slices.Equal(value.Paths, []string{"src/a.go", "src/nested/b.go"}) {
			t.Fatal("actual CLI Find differs from independent matching file paths")
		}
	}
}
