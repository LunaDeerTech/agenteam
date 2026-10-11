package mount

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
)

// These controlled ports check the metadata-only adapter boundary. The real
// Execution witness, complete lock union and Agent canonical reader remain
// obligations of the original same-Store integration chain.
type readFileTargetStore struct{ *runnerMountStore }

func (*readFileTargetStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	panic("target metadata opened a transaction")
}
func (*readFileTargetStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	panic("target metadata acquired locks")
}

func readFileTargetRequest(in mc.ReadFileIntent) mc.ReadFileTargetRequest {
	return mc.ReadFileTargetRequest{Actor: in.Actor, ProjectID: in.ProjectID, AgentID: in.AgentID, ExecutionID: in.ExecutionID, MountID: in.MountID}
}

func readFileTargetFixture(t *testing.T) (*ReadFileResolver, *runnerMountStore, *readFileOwner, mc.ReadFileTargetRequest) {
	t.Helper()
	r, s, owner, in := readFileFixture(t)
	request := readFileConfigurationRequest(in)
	s.locks = request.RequiredLocks()
	r.store = &readFileTargetStore{s}
	owner.active = true // Controlled live Agent witness; not an Operation grant.
	owner.deny = true   // Any recursion into the Operation owner must fail.
	return r, s, owner, readFileTargetRequest(in)
}

func TestReadFileCurrentTargetUsesLiveSameTxMetadataOnly(t *testing.T) {
	r, s, owner, in := readFileTargetFixture(t)
	reads, writes, acquires := s.reads, s.writes, s.acquires
	got, err := r.CurrentTargetInTx(context.Background(), s.tx, in)
	d, mapping := s.definitions[in.MountID], s.mappings[in.MountID]
	want := mc.ReadFileTarget{MountID: in.MountID, WorkspaceID: mapping.binding.WorkspaceID, RunnerID: d.Runner, WorkspaceName: d.Workspace, AgentVersion: owner.config.Fields().Core.Version, MountVersion: d.Version}
	if err != nil || got != want || got.Validate() != nil {
		t.Fatal("current metadata projection differs", err)
	}
	if owner.calls != 0 || s.reads <= reads || s.writes != writes || s.acquires != acquires {
		t.Fatal("metadata called Operation authority, skipped current facts or performed work")
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 6 {
		t.Fatal("target is not the exact safe metadata projection")
	}
	for _, name := range []string{"mount_id", "workspace_id", "runner_id", "workspace_name", "agent_version", "mount_version"} {
		if _, ok := fields[name]; !ok {
			t.Fatal("safe target field missing", name)
		}
	}
}

func TestReadFileCurrentTargetRejectsMissingCurrentScope(t *testing.T) {
	for _, mode := range []string{"foreign-tx", "missing-locks", "missing-witness", "actor-scope", "changed-head"} {
		t.Run(mode, func(t *testing.T) {
			r, s, owner, in := readFileTargetFixture(t)
			tx := s.tx
			code := f.Forbidden
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
				code = f.DependencyUnavailable
			case "missing-locks":
				s.missingLocks = true
			case "missing-witness":
				owner.active = false
			case "actor-scope":
				in.AgentID = testID[i.Agent](t, 93)
				code = f.InvalidArgument
			case "changed-head":
				s.head.version++
				code = f.InvalidState
			}
			reads, writes, acquires := s.reads, s.writes, s.acquires
			got, err := r.CurrentTargetInTx(context.Background(), tx, in)
			requireCode(t, err, code)
			if got != (mc.ReadFileTarget{}) || owner.calls != 0 || s.writes != writes || s.acquires != acquires {
				t.Fatal("rejected scope published target or performed work")
			}
			if mode != "changed-head" && s.reads != reads {
				t.Fatal("current Mount SQL preceded original scope checks")
			}
		})
	}
}

type readFileTargetCancelledAgent struct {
	owner  *readFileOwner
	cancel context.CancelFunc
}

func (a readFileTargetCancelledAgent) ReadExecutionConfigurationInTx(ctx context.Context, tx f.Tx, in ac.ExecutionConfigurationRequest) (ac.AgentConfig, error) {
	value, err := a.owner.ReadExecutionConfigurationInTx(ctx, tx, in)
	a.cancel()
	return value, err
}

func TestReadFileCurrentTargetCancellationDoesNotPublishMetadata(t *testing.T) {
	r, s, owner, in := readFileTargetFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.agents = readFileTargetCancelledAgent{owner, cancel}
	reads, writes := s.reads, s.writes
	got, err := r.CurrentTargetInTx(ctx, s.tx, in)
	if !errors.Is(err, context.Canceled) || got != (mc.ReadFileTarget{}) || s.reads != reads || s.writes != writes || owner.calls != 0 {
		t.Fatal("cancelled original reader published metadata or started Mount SQL", err)
	}
}

func TestReadFileTicketTargetIsReadOnlyProjection(t *testing.T) {
	if got, err := (ReadFileTicket{}).Target(); err == nil || got != (mc.ReadFileTarget{}) {
		t.Fatal("empty ticket exposed a target")
	}
	r, s, owner, in := readFileFixture(t)
	ticket, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	writes, acquires := s.writes, s.acquires
	target, err := ticket.Target()
	if err != nil || target.Validate() != nil || target.MountID != in.MountID || ticket.state.used || owner.calls != 1 || s.writes != writes || s.acquires != acquires {
		t.Fatal("target getter consumed the ticket or called an owner", err)
	}
	copy := target
	copy.WorkspaceName = "other"
	again, err := ticket.Target()
	if err != nil || again != target {
		t.Fatal("caller mutation changed the fixed target", err)
	}
	if _, err = r.Claim(context.Background(), ticket); err != nil {
		t.Fatal(err)
	}
	after, err := ticket.Target()
	if err != nil || after != target || !ticket.state.used || owner.calls != 2 {
		t.Fatal("metadata getter changed original Claim or retained identity", err)
	}
}
