//go:build integration && linux

package projectvariable_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// The real cmd, system Runner, Owner Agent configuration, enrollment, current
// Mount, TLS/WSS and disk are exercised. Only the not-default-wired Operation
// producer uses the strict D15 section 7 substitute below; this is not a Model
// or ToolCall integration test. No second Runner Manager or Handler is created.
func TestRunnerWriteFileApp(t *testing.T) {
	v := newRunnerReadFileAppFixture(t, runnerReadFileAppBinary(t))
	// This table belongs solely to this fixture's isolated database. It models
	// the substitute's exact Attempt/RID record, not a production Tool table.
	_, err := v.setup.agent.p2.base.raw.Exec(ctxFor(t), `CREATE TABLE runner_write_file_cli_admissions (
 operation_id text PRIMARY KEY, request_id text NOT NULL UNIQUE,
 project_id text NOT NULL, agent_id text NOT NULL, execution_id text NOT NULL,
 mount_id text NOT NULL, path text NOT NULL, data bytea NOT NULL, overwrite boolean NOT NULL)`)
	readFileAdapterOK(t, err, "private CLI authority fixture")
	data := bytes.Repeat([]byte{0, 0xff, '\n', 'A'}, 16<<10)
	defer clear(data)
	file := filepath.Join(v.configuration.RootPath, v.intent.ProjectID.String(), v.intent.AgentID.String(), "source", "written.bin")
	check := func(t *testing.T, want []byte) {
		t.Helper()
		got, err := os.ReadFile(file)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("independent disk observation differs from complete write", err)
		}
	}
	var original [32]byte
	if !t.Run("enrollment-write-and-no-overwrite-conflict", func(t *testing.T) {
		selection := "write-file"
		process := runnerReadFileAppLaunch(t, v.binary, v.env(&selection, true), []string{"--enroll"}, &v.token)
		runnerDirectoryAppOnline(t, v, process, 1, []string{p.WriteFileOperation})
		original = v.identity(t, true)
		runnerWriteFileAppDispatch(t, v, p.WriteFileArguments{Path: "written.bin", Data: data}, true, false)
		check(t, data)
		runnerWriteFileAppDispatch(t, v, p.WriteFileArguments{Path: "written.bin", Data: []byte("must-not-overwrite")}, true, true)
		check(t, data)
		v.dispatch(t, false, 64)
		v.stop(t, process)
		t.Log("write_cli_enrollment=1 committed_write=1 explicit_conflict=1 undeclared_read_rejected=1 original_waits=1")
	}) {
		return
	}
	replacement := []byte("replacement from stored identity\n")
	if !t.Run("stored-identity-combined-read-write", func(t *testing.T) {
		selection := "write-file,read-file"
		process := runnerReadFileAppLaunch(t, v.binary, v.env(&selection, false), nil, nil)
		runnerDirectoryAppOnline(t, v, process, 2, []string{p.ReadFileOperation, p.WriteFileOperation})
		if v.identity(t, true) != original {
			t.Fatal("write capability selection changed original identity")
		}
		runnerWriteFileAppDispatch(t, v, p.WriteFileArguments{Path: "written.bin", Data: replacement, Overwrite: true}, true, false)
		check(t, replacement)
		v.dispatch(t, true, 64)
		v.stop(t, process)
		t.Log("write_cli_identity_only_restart=1 explicit_overwrite=1 combined_read=1 original_waits=1")
	}) {
		return
	}
	t.Run("omitted-write-is-not-dispatched", func(t *testing.T) {
		selection := "read-file"
		process := runnerReadFileAppLaunch(t, v.binary, v.env(&selection, false), nil, nil)
		runnerDirectoryAppOnline(t, v, process, 3, []string{p.ReadFileOperation})
		if v.identity(t, true) != original {
			t.Fatal("omitting write changed original identity")
		}
		runnerWriteFileAppDispatch(t, v, p.WriteFileArguments{Path: "written.bin", Data: []byte("must-not-dispatch"), Overwrite: true}, false, false)
		check(t, replacement)
		v.dispatch(t, true, 64)
		v.stop(t, process)
		t.Log("write_cli_undeclared_write_rejected=1 no_write_attempt=true original_waits=1")
	})
}

// Unlike a configuration DTO, this private test owner supplies the missing
// Operation authority. Its full intent is fixed, its original fixture Attempt
// is persisted in the same real Store, and admission opens only AFTER that
// WithinTx has returned known committed. It does not fabricate Tool facts.
type writeFileAppAuthority struct {
	store     *hookStore
	intent    mc.WriteFileIntent
	requestID p.ID
}
type writeFileAppWitnessKey struct{}
type writeFileAppWitness struct {
	owner *writeFileAppAuthority
	tx    f.Tx
	live  atomic.Bool
}

func (o *writeFileAppAuthority) matches(in mc.WriteFileIntent) bool {
	want := o.intent
	return in.Validate() == nil && in.Actor.Equal(want.Actor) && in.ProjectID == want.ProjectID && in.AgentID == want.AgentID && in.ExecutionID == want.ExecutionID && in.OperationID == want.OperationID && in.MountID == want.MountID && in.Arguments.Path == want.Arguments.Path && in.Arguments.Overwrite == want.Arguments.Overwrite && bytes.Equal(in.Arguments.Data, want.Arguments.Data)
}
func (o *writeFileAppAuthority) configuration() ac.ExecutionConfigurationRequest {
	in := o.intent
	return ac.ExecutionConfigurationRequest{Actor: in.Actor, ProjectID: in.ProjectID, AgentID: in.AgentID, ExecutionID: in.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}
}
func (o *writeFileAppAuthority) WriteFileLocks(in mc.WriteFileIntent) ([]f.LockRequest, error) {
	if !o.matches(in) {
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	trigger, err := f.ProjectScheduleLock(in.ProjectID.String())
	if err != nil {
		return nil, err
	}
	return []f.LockRequest{{Key: trigger, Mode: f.Exclusive}}, nil
}
func (o *writeFileAppAuthority) locks() ([]f.LockRequest, error) {
	extra, err := o.WriteFileLocks(o.intent)
	if err != nil {
		return nil, err
	}
	op, err := f.AggregateLock(f.OperationAggregate, o.intent.OperationID.String())
	if err != nil {
		return nil, err
	}
	locks := append(o.configuration().RequiredLocks(), extra...)
	return oc.NormalizeLocks(append(locks, f.LockRequest{Key: op, Mode: f.Exclusive}))
}
func (o *writeFileAppAuthority) held(ctx context.Context, tx f.Tx) error {
	if _, err := o.store.InTx(tx); err != nil {
		return err
	}
	locks, err := o.locks()
	if err != nil {
		return err
	}
	return o.store.RequireHeldLocks(ctx, tx, locks)
}
func (o *writeFileAppAuthority) admit(t *testing.T, requestID p.ID) {
	t.Helper()
	if !requestID.Valid() || o.requestID != "" {
		t.Fatal("fixture admission requires one original RequestID")
	}
	locks, err := o.locks()
	readFileAdapterOK(t, err, "fixture Attempt lock plan")
	cause, err := f.NewRecoveryCause("runner-write-file-cli-attempt", o.intent.OperationID.String(), string(requestID))
	readFileAdapterOK(t, err, "fixture Attempt identity")
	result := o.store.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx f.Tx) error {
		if err := o.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		x, err := o.store.InTx(tx)
		if err != nil {
			return err
		}
		in := o.intent
		_, err = x.Exec(ctx, `INSERT INTO runner_write_file_cli_admissions(operation_id,request_id,project_id,agent_id,execution_id,mount_id,path,data,overwrite) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, in.OperationID.String(), string(requestID), in.ProjectID.String(), in.AgentID.String(), in.ExecutionID.String(), in.MountID.String(), in.Arguments.Path, in.Arguments.Data, in.Arguments.Overwrite)
		return err
	})
	if result.State() != f.Committed {
		t.Fatal("original fixture Attempt did not return known committed", result.State())
	}
	o.requestID = requestID
}
func (o *writeFileAppAuthority) WithWriteFileInTx(ctx context.Context, tx f.Tx, in mc.WriteFileIntent, requestID p.ID, fn func(context.Context) error) error {
	if ctx == nil || fn == nil || !o.matches(in) || requestID != "" && (requestID != o.requestID || !requestID.Valid()) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	if err := o.held(ctx, tx); err != nil {
		return err
	}
	if requestID != "" {
		x, err := o.store.InTx(tx)
		if err != nil {
			return err
		}
		var exists bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runner_write_file_cli_admissions WHERE operation_id=$1 AND request_id=$2 AND project_id=$3 AND agent_id=$4 AND execution_id=$5 AND mount_id=$6 AND path=$7 AND data=$8 AND overwrite=$9)`, in.OperationID.String(), string(requestID), in.ProjectID.String(), in.AgentID.String(), in.ExecutionID.String(), in.MountID.String(), in.Arguments.Path, in.Arguments.Data, in.Arguments.Overwrite).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return f.NewFault(f.Forbidden, f.NotStarted)
		}
	}
	witness := &writeFileAppWitness{owner: o, tx: tx}
	witness.live.Store(true)
	defer witness.live.Store(false)
	return fn(context.WithValue(ctx, writeFileAppWitnessKey{}, witness))
}
func (o *writeFileAppAuthority) RequireExecutionConfigurationInTx(ctx context.Context, tx f.Tx, in ac.ExecutionConfigurationRequest) error {
	if ctx == nil {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	witness, ok := ctx.Value(writeFileAppWitnessKey{}).(*writeFileAppWitness)
	want := o.configuration()
	if !ok || witness.owner != o || witness.tx != tx || !witness.live.Load() || in.Validate() != nil || in.Stage != want.Stage || !in.Actor.Equal(want.Actor) || in.ProjectID != want.ProjectID || in.AgentID != want.AgentID || in.ExecutionID != want.ExecutionID {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return o.held(ctx, tx)
}

func runnerWriteFileAppDispatch(t *testing.T, v *runnerReadFileAppFixture, args p.WriteFileArguments, enabled, conflict bool) {
	t.Helper()
	in := mc.WriteFileIntent{Actor: v.intent.Actor, ProjectID: v.intent.ProjectID, AgentID: v.intent.AgentID, ExecutionID: v.intent.ExecutionID, OperationID: id[tc.Operation](t), MountID: v.intent.MountID, Arguments: args}
	in.Arguments.Data = bytes.Clone(args.Data)
	defer clear(in.Arguments.Data)
	owner := &writeFileAppAuthority{store: v.setup.agent.p2.base.tracked, intent: in}
	current, err := agent.NewExecutionConfiguration(v.setup.agent.providers.Agents, owner)
	readFileAdapterOK(t, err, "actual current write Agent")
	resolver, err := mount.NewWriteFileResolver(owner.store, current, owner)
	readFileAdapterOK(t, err, "actual current write Mount resolver")
	dispatcher, err := runnerhttp.NewWriteFileDispatcher(v.device, resolver)
	readFileAdapterOK(t, err, "actual write dispatcher")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticket, err := resolver.Resolve(ctx, in)
	readFileAdapterOK(t, err, "original write ticket")
	call, err := dispatcher.Prepare(ticket)
	readFileAdapterOK(t, err, "original write RequestID")
	requestID := call.RequestID()
	if !requestID.Valid() || v.requests[requestID] {
		t.Fatal("write dispatcher did not allocate a distinct RequestID")
	}
	v.requests[requestID] = true
	owner.admit(t, requestID)
	result, err := call.Dispatch(ctx)
	if !enabled {
		requireCode(t, err, f.CapabilityUnsupported)
		if result.Attempted || result.Terminal != nil {
			t.Fatal("undeclared write capability reached dispatch")
		}
		return
	}
	readFileAdapterOK(t, err, "original CLI write terminal")
	terminal := result.Terminal
	if !result.Attempted || terminal == nil || terminal.RequestID != requestID || terminal.OperationID != p.ID(in.OperationID.String()) {
		t.Fatal("actual write terminal lost original RequestID/OperationID")
	}
	if conflict {
		if terminal.Outcome != p.Failure || terminal.Code == nil || *terminal.Code != p.Conflict || terminal.Payload != nil {
			t.Fatal("actual CLI changed the no-overwrite conflict")
		}
	} else {
		value, err := p.DecodeWriteFileResult(terminal.Payload)
		if terminal.Outcome != p.Success || terminal.Code != nil || terminal.SafeMessage != nil || err != nil || value.Path != args.Path || value.State != "committed" || value.BytesWritten != len(args.Data) || !value.Durable || value.CompletionCode != nil {
			t.Fatal("actual CLI changed complete publication facts", err)
		}
	}
	// A copied/reused transport call cannot send a second physical write.
	reused, err := call.Dispatch(ctx)
	requireCode(t, err, f.InvalidState)
	if reused.Attempted || reused.Terminal != nil {
		t.Fatal("original write call was dispatched twice")
	}
}
