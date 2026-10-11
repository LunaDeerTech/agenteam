package mount

import (
	"context"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// This controlled port exercises exact directory authority/intent. The PG
// fixture separately uses the actual Agent Current reader and real SQL facts.
type directoryOwner struct {
	*readFileOwner
	intent mc.DirectoryIntent
}

func (o *directoryOwner) DirectoryLocks(in mc.DirectoryIntent) ([]f.LockRequest, error) {
	if in.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	return o.readFileOwner.ReadFileLocks(o.readFileOwner.intent)
}
func (o *directoryOwner) WithDirectoryInTx(ctx context.Context, tx f.Tx, in mc.DirectoryIntent, fn func(context.Context) error) error {
	want := o.intent
	if o.deny || tx != o.store.tx || !in.Actor.Equal(want.Actor) || in.ProjectID != want.ProjectID || in.AgentID != want.AgentID || in.ExecutionID != want.ExecutionID || in.OperationID != want.OperationID || in.MountID != want.MountID || in.Operation != want.Operation || in.Arguments != want.Arguments {
		return fault(f.Forbidden)
	}
	o.calls++
	o.active = true
	defer func() { o.active = false }()
	return fn(ctx)
}
func directoryFixture(t *testing.T, operation string) (*DirectoryResolver, *runnerMountStore, *directoryOwner, mc.DirectoryIntent) {
	t.Helper()
	_, s, base, original := readFileFixture(t)
	in := mc.DirectoryIntent{Actor: original.Actor, ProjectID: original.ProjectID, AgentID: original.AgentID, ExecutionID: original.ExecutionID, OperationID: original.OperationID, MountID: original.MountID, Operation: operation, Arguments: p.DirectoryArguments{Path: ".", Limit: 2}}
	if operation == p.FindFilesOperation {
		in.Arguments.Pattern = "*.go"
		in.Arguments.Match = "name"
	}
	owner := &directoryOwner{readFileOwner: base, intent: in}
	r, err := NewDirectoryResolver(s, owner, owner)
	if err != nil {
		t.Fatal(err)
	}
	return r, s, owner, in
}
func TestDirectoryTicketBindsOperationAndCurrent(t *testing.T) {
	for _, operation := range []string{p.ListDirectoryOperation, p.FindFilesOperation} {
		t.Run(operation, func(t *testing.T) {
			r, s, owner, in := directoryFixture(t, operation)
			original := in
			ticket, err := r.Resolve(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			in.Operation = "untrusted"
			in.Arguments.Path = "changed"
			writes := s.writes
			request, err := r.Claim(context.Background(), ticket)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := p.DecodeDirectoryPayload(operation, request.Payload)
			if err != nil || payload.Arguments != original.Arguments || payload.WorkspaceName != s.mappings[original.MountID].binding.Workspace || request.OperationName != operation || request.OperationRevision != p.DirectoryRevision || request.RequestID != "" || request.OperationID != p.ID(original.OperationID.String()) || owner.active || owner.calls != 2 || s.writes != writes {
				t.Fatal("directory claim changed original operation or mapping", err)
			}
			_, err = r.Claim(context.Background(), ticket)
			requireCode(t, err, f.InvalidState)
		})
	}
}
func TestDirectoryTicketRefusesRevocationAndChangedAuthority(t *testing.T) {
	for _, mode := range []string{"clear", "operation", "arguments", "foreign-issuer", "zero"} {
		t.Run(mode, func(t *testing.T) {
			r, s, owner, in := directoryFixture(t, p.FindFilesOperation)
			ticket, err := r.Resolve(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
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
			case "operation":
				owner.intent.Operation = p.ListDirectoryOperation
			case "arguments":
				owner.intent.Arguments.Pattern = "*.txt"
			case "foreign-issuer":
				r, _ = NewDirectoryResolver(s, owner, owner)
			case "zero":
				ticket = DirectoryTicket{}
			}
			request, err := r.Claim(context.Background(), ticket)
			if err == nil || request.OperationID != "" || request.Payload != nil {
				t.Fatal("unapproved directory operation escaped")
			}
		})
	}
}
func TestDirectoryClaimUnknownHasNoWireRequest(t *testing.T) {
	r, s, _, in := directoryFixture(t, p.ListDirectoryOperation)
	ticket, err := r.Resolve(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	attempt := testID[f.TransactionAttempt](t, 87)
	cause, err := f.NewJobCause("directory-test", in.ExecutionID.String(), attempt.String())
	if err != nil {
		t.Fatal(err)
	}
	unknown := f.UnknownResult(attempt, cause)
	s.physical = &unknown
	request, err := r.Claim(context.Background(), ticket)
	got, ok := UnknownAttempt(err)
	if !ok || got.AttemptID() != attempt || request.OperationID != "" || request.Payload != nil {
		t.Fatal("Unknown published a directory request or lost original provenance")
	}
	s.physical = nil
	_, err = r.Claim(context.Background(), ticket)
	requireCode(t, err, f.InvalidState)
}
