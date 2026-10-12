package mount

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// Controlled current-authority/SQL ports only. The real producer's Attempt
// commit gate and Execution witness require the separate integration chain.
type writeFileOwner struct {
	*readFileOwner
	writeIntent mc.WriteFileIntent
	requestID   p.ID
	borrowed    [][]byte
	seen        []p.ID
}

func (o *writeFileOwner) WriteFileLocks(in mc.WriteFileIntent) ([]f.LockRequest, error) {
	o.borrowed = append(o.borrowed, in.Arguments.Data)
	return o.ReadFileLocks(o.intent)
}
func (o *writeFileOwner) WithWriteFileInTx(ctx context.Context, tx f.Tx, in mc.WriteFileIntent, rid p.ID, fn func(context.Context) error) error {
	want := o.writeIntent
	o.calls++
	o.seen = append(o.seen, rid)
	o.borrowed = append(o.borrowed, in.Arguments.Data)
	if o.deny || tx != o.store.tx || rid != o.requestID || !in.Actor.Equal(want.Actor) || in.ProjectID != want.ProjectID || in.AgentID != want.AgentID || in.ExecutionID != want.ExecutionID || in.OperationID != want.OperationID || in.MountID != want.MountID || in.Arguments.Path != want.Arguments.Path || in.Arguments.Overwrite != want.Arguments.Overwrite || !bytes.Equal(in.Arguments.Data, want.Arguments.Data) {
		return fault(f.Forbidden)
	}
	locks, _ := o.ReadFileLocks(o.intent)
	if err := o.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return err
	}
	o.active = true
	defer func() { o.active = false }()
	return fn(ctx)
}
func writeFileFixture(t *testing.T) (*WriteFileResolver, *runnerMountStore, *writeFileOwner, mc.WriteFileIntent) {
	t.Helper()
	_, s, old, read := readFileFixture(t)
	in := mc.WriteFileIntent{Actor: read.Actor, ProjectID: read.ProjectID, AgentID: read.AgentID, ExecutionID: read.ExecutionID, OperationID: read.OperationID, MountID: read.MountID, Arguments: p.WriteFileArguments{Path: "notes.txt", Data: []byte("private write\x00bytes"), Overwrite: true}}
	want := in
	want.Arguments.Data = bytes.Clone(in.Arguments.Data)
	o := &writeFileOwner{readFileOwner: old, writeIntent: want}
	r, err := NewWriteFileResolver(s, o, o)
	if err != nil {
		t.Fatal(err)
	}
	return r, s, o, in
}
func writeFileRequestID(t *testing.T) p.ID {
	t.Helper()
	rid, err := p.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return rid
}

func TestWriteFileTicketBindsOwnedBytesAndPreparedIdentity(t *testing.T) {
	r, s, owner, in := writeFileFixture(t)
	writes := s.writes
	ticket, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	target, err := ticket.Target()
	if err != nil || target.Validate() != nil || target.MountID != in.MountID || ticket.state.used {
		t.Fatal("target changed ticket ownership", err)
	}
	raw, _ := json.Marshal(ticket)
	if strings.Contains(string(raw), in.Arguments.Path) || strings.Contains(string(raw), "private") {
		t.Fatal("ticket exposed protected input")
	}
	clear(in.Arguments.Data) // A caller cannot rewrite the ticket's owned data.
	owner.requestID = writeFileRequestID(t)
	request, err := r.Claim(context.Background(), ticket, owner.requestID)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := p.DecodeWriteFilePayload(request.Payload)
	if err != nil || !bytes.Equal(payload.Arguments.Data, owner.writeIntent.Arguments.Data) || payload.Arguments.Path != owner.writeIntent.Arguments.Path || payload.Arguments.Overwrite != owner.writeIntent.Arguments.Overwrite || payload.WorkspaceName != target.WorkspaceName || request.RequestID != owner.requestID || request.Mount.WorkspaceID != p.ID(target.WorkspaceID.String()) || request.OperationName != p.WriteFileOperation || request.OperationID != p.ID(in.OperationID.String()) || s.writes != writes || owner.active || len(owner.seen) != 2 || owner.seen[0] != "" || owner.seen[1] != owner.requestID {
		t.Fatal("claim lost protected intent, current mapping or original request identity", err)
	}
	defer clear(payload.Arguments.Data)
	for _, borrowed := range append(owner.borrowed, ticket.state.intent.Arguments.Data) {
		if !bytes.Equal(borrowed, make([]byte, len(borrowed))) {
			t.Fatal("borrowed or consumed bytes survived")
		}
	}
	copy := ticket
	_, err = r.Claim(context.Background(), copy, owner.requestID)
	requireCode(t, err, f.InvalidState)
	after, err := ticket.Target()
	if err != nil || after != target {
		t.Fatal("consumption changed safe target", err)
	}
}

func TestWriteFileTicketRejectsStaleOrUncommittedClaim(t *testing.T) {
	for _, mode := range []string{"clear", "head", "mapping", "definition", "denied", "plan", "foreign-issuer", "zero", "missing-rid", "uncommitted-attempt", "different-rid", "unknown", "skipped-callback"} {
		t.Run(mode, func(t *testing.T) {
			r, s, owner, in := writeFileFixture(t)
			ticket, err := r.Resolve(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			rid := writeFileRequestID(t)
			owner.requestID = rid
			switch mode {
			case "clear":
				fields := owner.config.Fields()
				fields.Core.Version++
				fields.AllowedMountIDs = []i.MountID{}
				owner.config, err = ac.NewAgentConfig(fields)
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
			case "denied":
				owner.deny = true
			case "plan":
				owner.extra = true
			case "foreign-issuer":
				r, _ = NewWriteFileResolver(s, owner, owner)
			case "zero":
				ticket = WriteFileTicket{}
			case "missing-rid":
				rid = ""
			case "uncommitted-attempt":
				owner.requestID = ""
			case "different-rid":
				owner.requestID = writeFileRequestID(t)
			case "unknown":
				attempt := testID[f.TransactionAttempt](t, 83)
				cause, e := f.NewJobCause("write-file-test", in.ExecutionID.String(), attempt.String())
				if e != nil {
					t.Fatal(e)
				}
				unknown := f.UnknownResult(attempt, cause)
				s.physical = &unknown
			case "skipped-callback":
				committed := f.CommittedResult()
				s.physical = &committed
			}
			request, err := r.Claim(context.Background(), ticket, rid)
			if err == nil || request.RequestID != "" || request.Payload != nil {
				t.Fatal("rejected current admission produced a request", mode)
			}
			if mode == "unknown" {
				got, ok := UnknownAttempt(err)
				if !ok || got.AttemptID() != testID[f.TransactionAttempt](t, 83) {
					t.Fatal("unknown provenance lost")
				}
			}
		})
	}
}

type writeFileReturnStore struct {
	*runnerMountStore
	entered, release chan struct{}
}

func (s *writeFileReturnStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	out := s.runnerMountStore.WithinTx(ctx, cause, fn)
	close(s.entered)
	<-s.release
	return out
}
func TestWriteFileClaimWaitsForOriginalCommitReturn(t *testing.T) {
	r, s, owner, in := writeFileFixture(t)
	ticket, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	owner.requestID = writeFileRequestID(t)
	blocked := &writeFileReturnStore{s, make(chan struct{}), make(chan struct{})}
	r.store = blocked
	type answer struct {
		request p.Request
		err     error
	}
	done := make(chan answer, 1)
	returned := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() {
		once.Do(func() { close(blocked.release) })
		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Error("original claim did not retire")
		}
	})
	go func() {
		defer close(returned)
		q, e := r.Claim(context.Background(), ticket, owner.requestID)
		done <- answer{q, e}
	}()
	select {
	case <-blocked.entered:
	case <-time.After(time.Second):
		t.Fatal("original transaction did not reach return barrier")
	}
	select {
	case <-done:
		t.Fatal("claim escaped before original transaction returned")
	default:
	}
	once.Do(func() { close(blocked.release) })
	select {
	case out := <-done:
		if out.err != nil || out.request.RequestID != owner.requestID {
			t.Fatal("original committed claim lost", out.err)
		}
	case <-time.After(time.Second):
		t.Fatal("claim failed to join original transaction")
	}
}

func TestWriteFileCurrentTargetRequiresLiveSameTx(t *testing.T) {
	for _, mode := range []string{"current", "foreign-tx", "missing-locks", "missing-witness", "wrong-scope", "changed-head"} {
		t.Run(mode, func(t *testing.T) {
			r, s, owner, in := writeFileFixture(t)
			s.locks = writeFileConfigurationRequest(in).RequiredLocks()
			r.store = &readFileTargetStore{s} // Panics on any new Tx/Acquire.
			owner.active = true
			owner.deny = true // Metadata may not invoke an Operation owner.
			request := mc.WriteFileTargetRequest{Actor: in.Actor, ProjectID: in.ProjectID, AgentID: in.AgentID, ExecutionID: in.ExecutionID, MountID: in.MountID}
			tx := s.tx
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "missing-locks":
				s.missingLocks = true
			case "missing-witness":
				owner.active = false
			case "wrong-scope":
				request.AgentID = testID[i.Agent](t, 93)
			case "changed-head":
				s.head.version++
			}
			writes := s.writes
			got, err := r.CurrentTargetInTx(context.Background(), tx, request)
			if mode == "current" {
				if err != nil || got.Validate() != nil || got.WorkspaceID != s.mappings[in.MountID].binding.WorkspaceID {
					t.Fatal("safe current target missing", err)
				}
			} else if err == nil || got != (mc.WriteFileTarget{}) {
				t.Fatal("invalid current scope exposed metadata")
			}
			if owner.calls != 0 || s.writes != writes {
				t.Fatal("metadata invoked Operation owner or wrote facts")
			}
		})
	}
}
