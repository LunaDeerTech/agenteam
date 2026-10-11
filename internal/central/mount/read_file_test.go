package mount

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// Controlled current-operation and Agent ports test the adapter protocol. They
// are not a production execution grant or evidence of a real PG transaction.
type readFileOwner struct {
	store  *runnerMountStore
	intent mc.ReadFileIntent
	config ac.AgentConfig
	active bool
	deny   bool
	extra  bool
	calls  int
}

func (o *readFileOwner) ReadFileLocks(mc.ReadFileIntent) ([]f.LockRequest, error) {
	key, _ := f.ProjectScheduleLock(o.intent.ProjectID.String())
	locks := []f.LockRequest{{Key: key, Mode: f.Exclusive}}
	if o.extra {
		k, _ := f.SystemConfigLock("read-file-test")
		locks = append(locks, f.LockRequest{Key: k, Mode: f.Shared})
	}
	return locks, nil
}
func (o *readFileOwner) WithReadFileInTx(ctx context.Context, tx f.Tx, in mc.ReadFileIntent, fn func(context.Context) error) error {
	o.calls++
	if o.deny || tx != o.store.tx || !in.Actor.Equal(o.intent.Actor) || in.ProjectID != o.intent.ProjectID || in.AgentID != o.intent.AgentID || in.ExecutionID != o.intent.ExecutionID || in.OperationID != o.intent.OperationID || in.MountID != o.intent.MountID || in.Arguments != o.intent.Arguments {
		return fault(f.Forbidden)
	}
	locks, _ := o.ReadFileLocks(in)
	if err := o.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return err
	}
	o.active = true
	defer func() { o.active = false }()
	return fn(ctx)
}
func (o *readFileOwner) ReadExecutionConfigurationInTx(ctx context.Context, tx f.Tx, in ac.ExecutionConfigurationRequest) (ac.AgentConfig, error) {
	if !o.active || tx != o.store.tx || in.Validate() != nil || in.ProjectID != o.intent.ProjectID || in.AgentID != o.intent.AgentID || in.ExecutionID != o.intent.ExecutionID {
		return ac.AgentConfig{}, fault(f.Forbidden)
	}
	if err := o.store.RequireHeldLocks(ctx, tx, in.RequiredLocks()); err != nil {
		return ac.AgentConfig{}, err
	}
	return o.config.Clone(), nil
}
func readFileFixture(t *testing.T) (*ReadFileResolver, *runnerMountStore, *readFileOwner, mc.ReadFileIntent) {
	t.Helper()
	c, s, _, owner, _, change, plan := runnerMountFixture(t)
	bindings := runnerMountPlan(t, c, s, plan)
	change = runnerMountChange(t, change, bindings)
	runnerMountApply(t, c, s, owner, change)
	execution := testID[i.Execution](t, 80)
	actor, err := i.NewAgentRun(change.ProjectID, change.AgentID, execution)
	if err != nil {
		t.Fatal(err)
	}
	in := mc.ReadFileIntent{Actor: actor, ProjectID: change.ProjectID, AgentID: change.AgentID, ExecutionID: execution, OperationID: testID[tc.Operation](t, 81), MountID: bindings[0].MountID, Arguments: p.ReadFileArguments{Path: "notes.txt", Offset: 0, Length: 64, Encoding: "utf8"}}
	o := &readFileOwner{store: s, intent: in, config: runnerMountBefore(t, change)}
	r, err := NewReadFileResolver(s, o, o)
	if err != nil {
		t.Fatal(err)
	}
	return r, s, o, in
}

func TestReadFileTicketRechecksCurrentAndIsSingleUse(t *testing.T) {
	r, s, o, in := readFileFixture(t)
	writes := s.writes
	ticket, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(ticket)
	if strings.Contains(string(raw), in.Arguments.Path) {
		t.Fatal("ticket leaked arguments")
	}
	request, err := r.Claim(context.Background(), ticket)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := p.DecodeReadFilePayload(request.Payload)
	if err != nil || payload.Arguments != in.Arguments || payload.WorkspaceName != s.mappings[in.MountID].binding.Workspace || request.Mount.WorkspaceID != p.ID(s.mappings[in.MountID].binding.WorkspaceID.String()) || request.OperationID != p.ID(in.OperationID.String()) || request.RequestID != "" || o.calls != 2 || o.active || s.writes != writes {
		t.Fatal("claim did not preserve exact intent/mapping or made writes", err)
	}
	_, err = r.Claim(context.Background(), ticket)
	requireCode(t, err, f.InvalidState)
	// A caller-held copy shares consumption and is not a fresh capability.
	copy := ticket
	_, err = r.Claim(context.Background(), copy)
	requireCode(t, err, f.InvalidState)
}

func TestReadFileTicketDeniesCurrentMutationAndForeignAuthority(t *testing.T) {
	for _, mode := range []string{"clear", "head", "mapping", "definition", "deny", "plan", "foreign-issuer", "zero"} {
		t.Run(mode, func(t *testing.T) {
			r, s, o, in := readFileFixture(t)
			ticket, err := r.Resolve(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "clear":
				fields := o.config.Fields()
				fields.Core.Version++
				fields.AllowedMountIDs = []i.MountID{}
				o.config, err = ac.NewAgentConfig(fields)
				if err != nil {
					t.Fatal(err)
				}
				s.head = referenceState{exists: true, version: fields.Core.Version, ids: []i.MountID{}}
			case "head":
				s.head.version++
			case "mapping":
				v := s.mappings[in.MountID]
				v.binding.WorkspaceID = testID[mc.Workspace](t, 82)
				s.mappings[in.MountID] = v
			case "definition":
				v := s.definitions[in.MountID]
				v.Lifecycle = "disabled"
				s.definitions[in.MountID] = v
			case "deny":
				o.deny = true
			case "plan":
				o.extra = true
			case "foreign-issuer":
				r, _ = NewReadFileResolver(s, o, o)
			case "zero":
				ticket = ReadFileTicket{}
			}
			request, err := r.Claim(context.Background(), ticket)
			if err == nil || request.OperationID != "" || len(request.Payload) != 0 {
				t.Fatal("invalid claim produced wire intent", mode)
			}
		})
	}
}

func TestReadFileClaimUnknownNeverPublishesRequest(t *testing.T) {
	r, s, _, in := readFileFixture(t)
	ticket, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	attempt := testID[f.TransactionAttempt](t, 83)
	cause, err := f.NewJobCause("read-file-test", in.ExecutionID.String(), attempt.String())
	if err != nil {
		t.Fatal(err)
	}
	unknown := f.UnknownResult(attempt, cause)
	s.physical = &unknown
	request, err := r.Claim(context.Background(), ticket)
	got, ok := UnknownAttempt(err)
	if !ok || got.AttemptID() != attempt || request.OperationID != "" || request.Payload != nil {
		t.Fatal("Unknown yielded an attempt or lost provenance")
	}
	s.physical = nil
	_, err = r.Claim(context.Background(), ticket)
	requireCode(t, err, f.InvalidState)
}
