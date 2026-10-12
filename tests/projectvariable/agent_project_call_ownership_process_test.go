//go:build integration

package projectvariable_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	assembly "github.com/LunaDeerTech/agenteam/tests/testsupport/agentconfiguration"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

const agentOwnershipChildEnv = "AGENTEAM_AGENT_CALL_OWNERSHIP_CHILD"
const agentOwnershipChildReady = "AGENT_CALL_ORIGINAL_UNKNOWN_GUARD_HELD"
const agentOwnershipChildPID = "AGENT_CALL_COMMIT_PID:"

type agentOwnershipInput struct {
	Database, Proxy, Bucket, Spool, Process, Project, User, Session string
	Target                                                          i.AgentID
	Request                                                         ac.AgentUpdate
	Meta                                                            f.CommandMeta
}
type agentOwnershipChild struct {
	*phaseGuardChild
	pid chan int32
}

// Only the selector/input/closed protocol differ from the existing real guard
// child. Its single original cmd.Wait and pipe reader are retained by join.
func startAgentOwnershipChild(t *testing.T, input agentOwnershipInput) *agentOwnershipChild {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "agent-call-child.json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write(raw)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("private Agent child input write")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAgentProjectCallOwnership$", "-test.timeout=40s")
	cmd.Env = append(os.Environ(), agentOwnershipChildEnv+"="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		cancel()
		_ = stdin.Close()
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = writer, io.Discard
	if err = cmd.Start(); err != nil {
		cancel()
		_ = stdin.Close()
		_ = reader.Close()
		_ = writer.Close()
		t.Fatal(err)
	}
	_ = writer.Close()
	child := &agentOwnershipChild{phaseGuardChild: &phaseGuardChild{cmd: cmd, cancel: cancel, stdin: stdin, done: make(chan struct{}), readDone: make(chan struct{}), ready: make(chan struct{})}, pid: make(chan int32, 1)}
	go func() {
		defer close(child.readDone)
		defer reader.Close()
		scanner := bufio.NewScanner(reader)
		var readyOnce, pidOnce sync.Once
		for scanner.Scan() {
			line := scanner.Text()
			if line == agentOwnershipChildReady {
				readyOnce.Do(func() { close(child.ready) })
			}
			if strings.HasPrefix(line, agentOwnershipChildPID) {
				n, err := strconv.ParseInt(strings.TrimPrefix(line, agentOwnershipChildPID), 10, 32)
				if err == nil && n > 0 {
					pidOnce.Do(func() { child.pid <- int32(n) })
				}
			}
		}
	}()
	go func() { child.waitErr = cmd.Wait(); close(child.done) }()
	return child
}

func agentOwnershipChildMain(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 65536 {
		t.Fatal("invalid private Agent child input")
	}
	rawInput, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("private Agent child input unavailable")
	}
	var input agentOwnershipInput
	decoder := json.NewDecoder(bytes.NewReader(rawInput))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !filepath.IsAbs(input.Spool) || input.Request.Validate() != nil || input.Request.Fields().Name != nil || input.Target.Validate() != nil || input.Meta.Validate() != nil {
		t.Fatal("invalid Agent child input shape")
	}
	host, _, err := net.SplitHostPort(input.Proxy)
	if err != nil || host != "127.0.0.1" {
		t.Fatal("child proxy is not the owned loopback address")
	}
	remote, err := pgfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	db := &pgfixture.Database{Fixture: remote, Name: input.Database}
	u, err := url.Parse(remote.URL(db.Name))
	if err != nil {
		t.Fatal("owned database URL invalid")
	}
	u.Host = input.Proxy
	raw := openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "CA_FILE": ""}))
	store := &hookStore{fixtureStore: raw}
	ak, keys := keys(t)
	accounts, err := account.NewAuthority(store, ak)
	if err != nil {
		t.Fatal(err)
	}
	if err = accounts.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	projects, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts})
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := audit.New(store, keys, audit.Authorizations{Accounts: accounts, Sessions: accounts, System: accounts, Projects: projects})
	if err != nil {
		t.Fatal(err)
	}
	process, err := f.ParseID[objc.Process](input.Process)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := f.ParseID[i.Project](input.Project)
	if err != nil {
		t.Fatal(err)
	}
	user, err := f.ParseID[i.User](input.User)
	if err != nil {
		t.Fatal(err)
	}
	session, err := f.ParseID[i.Session](input.Session)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := i.NewHuman(user, session)
	if err != nil {
		t.Fatal(err)
	}
	storage, _ := phaseGuardStorage(t, input.Bucket)
	runtime, objects, guard := phaseGuardRuntime(t, store, auditor, storage, input.Spool, process)
	if err = runtime.Initialize(ctxFor(t)); err != nil {
		phaseGuardRetireRuntime(t, runtime)
		t.Fatal(err)
	}
	installer, err := toolruntime.NewInstallAuthority(store, guard, process)
	if err != nil {
		phaseGuardRetireRuntime(t, runtime)
		t.Fatal(err)
	}
	authority, err := skill.NewAuthorityWithInstallExecution(store, projects, installer)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := skill.AddSkills(ctxFor(t))
	if err != nil {
		t.Fatal(err)
	}
	skills, err := skill.New(skill.Dependencies{Authority: authority, Objects: objects, Processes: guard, ProcessID: process, Bundle: bundle})
	if err != nil {
		t.Fatal(err)
	}
	source, err := toolruntime.NewInstallSource(installer, skills)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := assembly.New(store, assembly.Options{Accounts: accounts, BuiltinSources: map[string]registry.BuiltinSource{builtin.SkillInstallStableKey: source}, InstallSourceKey: builtin.SkillInstallStableKey})
	if err != nil {
		t.Fatal(err)
	}
	pair, err := graph.NewProjectCallOwnership(keys, &phaseGuardProcesses{guard: guard, process: process}, guard, process)
	if err != nil {
		t.Fatal(err)
	}
	release, entered := newPhaseRelease(), make(chan struct{})
	done := make(chan struct{})
	started := false
	defer func() {
		release.release()
		if started {
			phaseAwait(t, done)
		}
		pair.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := pair.Drain(ctx); err != nil || !pair.Joined() {
			t.Error("child original calls did not join", err)
			return
		}
		if err := skills.Drain(ctx); err != nil {
			t.Error(err)
			return
		}
		installer.Stop()
		if err := installer.Drain(ctx); err != nil {
			t.Error(err)
			return
		}
		phaseGuardRetireRuntime(t, runtime)
	}()
	command, err := ac.AgentCommandIdentity(projectID, ac.UpdateAgentCommand, input.Meta.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	inputReader := bufio.NewReader(os.Stdin)
	var once sync.Once
	var physical f.CommitResult
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if cause.Details().Kind != f.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		key, _ := f.ProjectLock(projectID.String())
		if err = store.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Shared}}); err != nil {
			return err
		}
		var state string
		if err = x.QueryRow(ctx, `SELECT state FROM agenteam_agent.commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, projectID.String(), string(ac.UpdateAgentCommand), string(input.Meta.IdempotencyKey)).Scan(&state); err != nil {
			return err
		}
		if state != "completed" {
			return errors.New("child did not take the existing completed replay")
		}
		var pid int32
		if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		fmt.Printf("%s%d\n", agentOwnershipChildPID, pid)
		if _, err = inputReader.ReadByte(); err != nil {
			return errors.New("original COMMIT arm acknowledgement missing")
		}
		return nil
	})
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if cause.Details().Kind == f.CommandsCause && cause.Details().Primary.Canonical() == command.Canonical() {
			once.Do(func() { physical = result; close(entered); <-release.done })
		}
	}
	store.mu.Unlock()
	var callErr error
	started = true
	go func() {
		defer close(done)
		_, callErr = pair.Service().UpdateAgent(ctxFor(t), actor, input.Meta, projectID, input.Target, input.Request)
	}()
	select {
	case <-entered:
	case <-done:
		t.Fatal("child replay returned before original result barrier", callErr)
	case <-time.After(15 * time.Second):
		t.Fatal("child original result barrier missing")
	}
	if physical.State() != f.Unknown || physical.AttemptID().Validate() != nil {
		t.Fatal("original cut caller did not produce real Unknown")
	}
	// This is the underlying raw Store's actual Unknown. afterResult still
	// holds the original wrapper return, so Agent has not entered confirmation
	// or recorded local returned. Neither fact proves the upstream Tx ended.
	// Only the spool directory lock is released. The bound process guard stays
	// held until the parent kills and actually waits for this exact child.
	objects.StopAdmission()
	if err = objects.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	fmt.Println(agentOwnershipChildReady)
	_, _ = inputReader.ReadByte()
}
