//go:build integration && linux

package projectvariable_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	runnerservice "github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/runner/control"
	"github.com/LunaDeerTech/agenteam/internal/runner/directory"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// This is the strict D15 section 7 substitute for the unbound ToolCall producer,
// not evidence of a real running Execution or a production Tool authorization.
// Its immutable, explicit intents bind the complete operation and arguments;
// ReadFile authority and Snapshot are never accepted as directory permission.
type directoryAdapterAuthority struct {
	store   *hookStore
	intents map[tc.OperationID]mc.DirectoryIntent
}

type directoryAdapterWitnessKey struct{}
type directoryAdapterWitness struct {
	owner  *directoryAdapterAuthority
	tx     f.Tx
	intent mc.DirectoryIntent
	live   atomic.Bool
}

func (o *directoryAdapterAuthority) matches(in mc.DirectoryIntent) bool {
	want, ok := o.intents[in.OperationID]
	return ok && in.Validate() == nil && in.Actor.Equal(want.Actor) && in.ProjectID == want.ProjectID && in.AgentID == want.AgentID && in.ExecutionID == want.ExecutionID && in.OperationID == want.OperationID && in.MountID == want.MountID && in.Operation == want.Operation && in.Arguments == want.Arguments
}

func (o *directoryAdapterAuthority) DirectoryLocks(in mc.DirectoryIntent) ([]f.LockRequest, error) {
	if !o.matches(in) {
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	trigger, err := f.ProjectScheduleLock(in.ProjectID.String())
	if err != nil {
		return nil, err
	}
	return []f.LockRequest{{Key: trigger, Mode: f.Exclusive}}, nil
}

func directoryAdapterConfigurationRequest(in mc.DirectoryIntent) ac.ExecutionConfigurationRequest {
	return ac.ExecutionConfigurationRequest{Actor: in.Actor, ProjectID: in.ProjectID, AgentID: in.AgentID, ExecutionID: in.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}
}

func (o *directoryAdapterAuthority) held(ctx context.Context, tx f.Tx, in mc.DirectoryIntent) error {
	if _, err := o.store.InTx(tx); err != nil {
		return err
	}
	extra, err := o.DirectoryLocks(in)
	if err != nil {
		return err
	}
	operation, err := f.AggregateLock(f.OperationAggregate, in.OperationID.String())
	if err != nil {
		return err
	}
	locks := append(directoryAdapterConfigurationRequest(in).RequiredLocks(), extra...)
	locks = append(locks, f.LockRequest{Key: operation, Mode: f.Exclusive})
	locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return err
	}
	return o.store.RequireHeldLocks(ctx, tx, locks)
}

func (o *directoryAdapterAuthority) WithDirectoryInTx(ctx context.Context, tx f.Tx, in mc.DirectoryIntent, fn func(context.Context) error) error {
	if ctx == nil || fn == nil || !o.matches(in) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	if err := o.held(ctx, tx, in); err != nil {
		return err
	}
	witness := &directoryAdapterWitness{owner: o, tx: tx, intent: in}
	witness.live.Store(true)
	defer witness.live.Store(false)
	return fn(context.WithValue(ctx, directoryAdapterWitnessKey{}, witness))
}

func (o *directoryAdapterAuthority) RequireExecutionConfigurationInTx(ctx context.Context, tx f.Tx, in ac.ExecutionConfigurationRequest) error {
	if ctx == nil {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	witness, ok := ctx.Value(directoryAdapterWitnessKey{}).(*directoryAdapterWitness)
	if !ok || witness.owner != o || witness.tx != tx || !witness.live.Load() || !o.matches(witness.intent) || in.Validate() != nil || in.Stage != ac.ExecutionConfigurationCurrent {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	want := directoryAdapterConfigurationRequest(witness.intent)
	if !in.Actor.Equal(want.Actor) || in.ProjectID != want.ProjectID || in.AgentID != want.AgentID || in.ExecutionID != want.ExecutionID {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return o.held(ctx, tx, witness.intent)
}

// One real connection exercises the bounded directory slice. Existing helpers
// own Account/ordinary Owner, PG, enrollment, verified TLS/WSS and every actual
// HTTP/socket close. The original Object ProcessGuard remains held through the
// later client, Handle, Manager, service and database cleanup callbacks.
func TestRunnerDirectoryAdapter(t *testing.T) {
	v := newRunnerMountPGFixture(t)
	base := v.agent.p2.base
	private := readFileAdapterPrivateDirectory(t)
	rootPath := filepath.Join(private, "root")
	readFileAdapterOK(t, os.Mkdir(rootPath, 0700), "owned directory device root")
	authority, err := runnerservice.NewAuthority(base.tracked, base.accounts)
	readFileAdapterOK(t, err, "real directory Runner authority")
	auditor, err := audit.New(base.tracked, base.keys, audit.Authorizations{Sessions: base.accounts, System: base.accounts, Accounts: base.accounts, Runners: authority})
	readFileAdapterOK(t, err, "real directory Runner Audit")
	runners, err := runnerservice.New(authority, auditor)
	readFileAdapterOK(t, err, "real directory Runner service")
	t.Cleanup(func() {
		runners.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if runners.Drain(ctx) != nil {
			t.Error("directory Runner service fallback cleanup did not join")
		}
	})
	create, err := rc.NewCreate(rc.CreateRequest{RunnerID: id[rc.Runner](t), Name: "directory-runner", Description: "Owned directory adapter fixture", Tags: []string{}, RootPath: rootPath})
	readFileAdapterOK(t, err, "formal directory Runner creation intent")
	created, err := runners.Execute(ctxFor(t), base.adminBrowser.actor, "directory-runner-create", create)
	material := created.Material
	defer func() {
		if material != nil {
			material.Token = p.EnrollmentToken{}
		}
	}()
	readFileAdapterOK(t, err, "formal directory Runner creation")
	if created.Receipt.Validate() != nil || created.Receipt.Runner.Status != rc.Offline || material == nil || !material.Token.Valid() || material.ExpiresAt.Validate() != nil {
		t.Fatal("first known directory Runner creation omitted valid one-time material")
	}
	declarations := []ac.RunnerMountDeclaration{{RunnerID: created.Receipt.Runner.ID, Workspace: "source"}}
	createAgent, commandMeta, _ := v.createRequest(t, declarations)
	mutation := v.mutation(t, v.request(t, base.ownerBrowser, http.MethodPost, v.collection(), commandMeta.IdempotencyKey, jsonBytes(t, map[string]any{"request": createAgent})), 1, true, declarations)
	binding := (*mutation.Fields().RunnerMounts)[0]
	projectID, agentID := v.agent.p2.project.ID, mutation.Fields().Agent.Fields().Core.ID
	device, err := runnerhttp.NewDeviceHandler(runners)
	readFileAdapterOK(t, err, "real directory device HTTP handler")
	server := newReadFileAdapterServer(t, device, runners)
	root, err := os.Open(rootPath)
	readFileAdapterOK(t, err, "trusted directory root descriptor")
	manager, managerErr := workspace.New(root)
	closeErr := root.Close()
	readFileAdapterOK(t, managerErr, "real directory Workspace manager")
	readFileAdapterOK(t, closeErr, "borrowed directory root descriptor close")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if manager.Drain(ctx) != nil {
			t.Error("directory Workspace manager did not actually join")
		}
	})
	handle, err := manager.Ensure(ctxFor(t), workspace.Identity{ProjectID: p.ID(projectID.String()), AgentID: p.ID(agentID.String()), Name: binding.Workspace})
	readFileAdapterOK(t, err, "real directory Workspace Ensure")
	readFileAdapterOK(t, handle.Drain(context.Background()), "initial directory Workspace Handle close")
	workspaceRoot := filepath.Join(rootPath, projectID.String(), agentID.String(), binding.Workspace)
	for _, name := range []string{"src/empty", "src/nested"} {
		readFileAdapterOK(t, os.MkdirAll(filepath.Join(workspaceRoot, name), 0700), "owned directory tree")
	}
	for _, name := range []string{"src/a.go", "src/b.txt", "src/nested/c.go", "src/nested/文.go"} {
		readFileAdapterOK(t, os.WriteFile(filepath.Join(workspaceRoot, name), []byte("owned fixture"), 0600), "owned directory file")
	}
	readFileAdapterOK(t, os.Symlink("nested", filepath.Join(workspaceRoot, "src/linked")), "owned discovered directory symlink")
	readFileAdapterOK(t, os.Symlink("a.go", filepath.Join(workspaceRoot, "src/alias.go")), "owned discovered file symlink")
	handler, err := directory.New(manager)
	readFileAdapterOK(t, err, "real directory handler")
	configuration := identity.Configuration{CentralURL: server.origin, RunnerID: p.ID(created.Receipt.Runner.ID.String()), RootPath: rootPath}
	states := make(chan control.ConnectionState, 16)
	client, err := control.NewDirectoryReads(control.Options{IdentityFile: filepath.Join(private, "identity.json"), Configuration: &configuration, EnrollmentToken: material.Token, RunnerVersion: "directory-fixture", Roots: server.roots, Observe: func(ctx context.Context, state control.ConnectionState) {
		select {
		case states <- state:
		case <-ctx.Done():
		}
	}}, handler)
	readFileAdapterOK(t, err, "real explicitly bound directory Runner client")
	material.Token = p.EnrollmentToken{}
	created.Material = nil
	runDone := make(chan struct{})
	var runErr error
	go func() { runErr = client.Run(context.Background()); close(runDone) }()
	t.Cleanup(func() {
		client.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if client.Drain(ctx) != nil {
			t.Error("real directory client Drain did not join")
		}
		select {
		case <-runDone:
			if !errors.Is(runErr, context.Canceled) {
				t.Error("original directory Client.Run cancellation outcome differs")
			}
		case <-ctx.Done():
			t.Error("original directory Client.Run return missing")
		}
	})
	connected, cancelConnect := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancelConnect()
connect:
	for {
		select {
		case state := <-states:
			if state == control.Connected {
				break connect
			}
		case <-runDone:
			t.Fatal("real directory Runner returned before authenticated hello")
		case <-connected.Done():
			t.Fatal("real directory Runner did not complete authenticated hello")
		}
	}
	executionID := id[i.Execution](t)
	actor, err := i.NewAgentRun(projectID, agentID, executionID)
	readFileAdapterOK(t, err, "directory fixture AgentRun identity")
	cases := []struct {
		operation string
		arguments p.DirectoryArguments
		entries   []p.DirectoryEntry
		paths     []string
		truncated bool
	}{
		{operation: p.ListDirectoryOperation, arguments: p.DirectoryArguments{Path: "src", Limit: 8}, entries: []p.DirectoryEntry{{Name: "a.go", Kind: "file"}, {Name: "alias.go", Kind: "symlink"}, {Name: "b.txt", Kind: "file"}, {Name: "empty", Kind: "directory"}, {Name: "linked", Kind: "symlink"}, {Name: "nested", Kind: "directory"}}},
		{operation: p.FindFilesOperation, arguments: p.DirectoryArguments{Path: "src", Limit: 8, Pattern: "*.go", Match: "name"}, paths: []string{"src/a.go", "src/nested/c.go", "src/nested/文.go"}},
		{operation: p.FindFilesOperation, arguments: p.DirectoryArguments{Path: "src", Limit: 8, Pattern: "src/*.go", Match: "path"}, paths: []string{"src/a.go"}},
		{operation: p.ListDirectoryOperation, arguments: p.DirectoryArguments{Path: "src/empty", Limit: 1}, entries: []p.DirectoryEntry{}},
		{operation: p.FindFilesOperation, arguments: p.DirectoryArguments{Path: "src/empty", Limit: 1, Pattern: "*", Match: "name"}, paths: []string{}},
		{operation: p.ListDirectoryOperation, arguments: p.DirectoryArguments{Path: "src", Limit: 1}, truncated: true},
		{operation: p.FindFilesOperation, arguments: p.DirectoryArguments{Path: "src", Limit: 1, Pattern: "*.go", Match: "name"}, truncated: true},
		{operation: p.FindFilesOperation, arguments: p.DirectoryArguments{Path: "src", Limit: 3, Pattern: "*.go", Match: "name"}, paths: []string{"src/a.go", "src/nested/c.go", "src/nested/文.go"}},
	}
	intents := make([]mc.DirectoryIntent, len(cases))
	owner := &directoryAdapterAuthority{store: base.tracked, intents: make(map[tc.OperationID]mc.DirectoryIntent, len(cases))}
	for n, item := range cases {
		in := mc.DirectoryIntent{Actor: actor, ProjectID: projectID, AgentID: agentID, ExecutionID: executionID, OperationID: id[tc.Operation](t), MountID: binding.MountID, Operation: item.operation, Arguments: item.arguments}
		intents[n], owner.intents[in.OperationID] = in, in
	}
	current, err := agent.NewExecutionConfiguration(v.agent.providers.Agents, owner)
	readFileAdapterOK(t, err, "real directory current Agent configuration adapter")
	resolver, err := mount.NewDirectoryResolver(base.tracked, current, owner)
	readFileAdapterOK(t, err, "real directory current Mount resolver")
	dispatcher, err := runnerhttp.NewDirectoryDispatcher(device, resolver)
	readFileAdapterOK(t, err, "real directory ticket-only dispatcher")
	seen := make(map[p.ID]bool)
	check := func(n int, result runnerhttp.DirectoryDispatchResult, dispatchErr error) {
		t.Helper()
		readFileAdapterOK(t, dispatchErr, "real WSS directory dispatch")
		in, want := intents[n], cases[n]
		if !result.Attempted || result.Terminal == nil || result.Terminal.Outcome != p.Success || result.Terminal.OperationID != p.ID(in.OperationID.String()) || !result.Terminal.RequestID.Valid() || seen[result.Terminal.RequestID] {
			t.Fatal("directory dispatch did not return its unique correlated terminal")
		}
		seen[result.Terminal.RequestID] = true
		if in.Operation == p.ListDirectoryOperation {
			value, err := p.DecodeListDirectoryResult(result.Terminal.Payload)
			if err != nil || value.Path != in.Arguments.Path || value.Truncated != want.truncated || len(value.Entries) > in.Arguments.Limit {
				t.Fatal("real list result changed path, limit or truncation")
			}
			if want.truncated {
				if len(value.Entries) != in.Arguments.Limit {
					t.Fatal("real list truncated without observing a full bounded selection")
				}
				for _, got := range value.Entries {
					found := false
					for _, entry := range cases[0].entries {
						if got == entry {
							found = true
							break
						}
					}
					if !found {
						t.Fatal("real limited list returned an entry outside the owned tree")
					}
				}
			} else if !reflect.DeepEqual(value.Entries, want.entries) {
				t.Fatal("real list lost explicit empty entries, sorted names or local kinds")
			}
		} else {
			value, err := p.DecodeFindFilesResult(result.Terminal.Payload)
			if err != nil || value.Path != in.Arguments.Path || value.Truncated != want.truncated || len(value.Paths) > in.Arguments.Limit {
				t.Fatal("real find result changed path, limit or truncation")
			}
			if want.truncated {
				if len(value.Paths) != in.Arguments.Limit {
					t.Fatal("real find truncated without observing a full bounded selection")
				}
				for _, got := range value.Paths {
					found := false
					for _, path := range cases[1].paths {
						if got == path {
							found = true
							break
						}
					}
					if !found {
						t.Fatal("real limited find followed a symlink or left the owned tree")
					}
				}
			} else if !reflect.DeepEqual(value.Paths, want.paths) {
				t.Fatal("real find changed its glob, explicit empty result or symlink boundary")
			}
		}
	}
	for n, in := range intents {
		ticket, err := resolver.Resolve(ctxFor(t), in)
		readFileAdapterOK(t, err, "initial verified directory ticket")
		result, err := dispatcher.Dispatch(ctxFor(t), ticket)
		check(n, result, err)
	}
	// These tickets are verified but unclaimed. Formal clear must reject both
	// before any queue admission; it does not rewrite an already admitted call.
	stale := make([]mount.DirectoryTicket, 2)
	for n := range stale {
		stale[n], err = resolver.Resolve(ctxFor(t), intents[n])
		readFileAdapterOK(t, err, "pre-clear unclaimed directory ticket")
	}
	v.update(t, agentID, "directory-formal-clear", 1, []ac.RunnerMountDeclaration{}, 2, true)
	for n, ticket := range stale {
		rejected, err := dispatcher.Dispatch(ctxFor(t), ticket)
		requireCode(t, err, f.Forbidden)
		if rejected.Attempted || rejected.Terminal != nil {
			t.Fatal("clear allowed an old unclaimed directory ticket to reach the wire")
		}
		_, err = resolver.Resolve(ctxFor(t), intents[n])
		requireCode(t, err, f.Forbidden)
	}
	readded, _ := v.update(t, agentID, "directory-formal-readd", 2, declarations, 3, true)
	bindings := readded.Fields().RunnerMounts
	if bindings == nil || len(*bindings) != 1 || (*bindings)[0] != binding {
		t.Fatal("formal re-add changed the stable directory Mount/Workspace mapping")
	}
	for n := range stale {
		ticket, err := resolver.Resolve(ctxFor(t), intents[n])
		readFileAdapterOK(t, err, "current re-added directory Mount ticket")
		result, err := dispatcher.Dispatch(ctxFor(t), ticket)
		check(n, result, err)
	}
}
