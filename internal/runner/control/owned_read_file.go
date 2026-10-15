package control

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/runner/directory"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	"github.com/LunaDeerTech/agenteam/internal/runner/operations"
	"github.com/LunaDeerTech/agenteam/internal/runner/readfile"
	"github.com/LunaDeerTech/agenteam/internal/runner/runcommand"
	"github.com/LunaDeerTech/agenteam/internal/runner/skillprepare"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	"github.com/LunaDeerTech/agenteam/internal/runner/writefile"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// This private owner stays on the original Client.Run stack. Tests can wrap an
// instance's actual manager return without exposing a public resource factory.
type ownedReadFileManager interface {
	Stop()
	Drain(context.Context) error
}

// newOwnedReadFile consumes only the configuration read under the identity lock.
// A returned manager, including alongside an error, transfers to Client.Run for
// synchronous Stop/Drain before identity.Close and Client.done.
func newOwnedReadFile(configuration identity.Configuration) (operationExecutor, ownedReadFileManager, error) {
	manager, err := newOwnedWorkspace(configuration)
	if manager == nil {
		return nil, nil, err
	}
	if err != nil {
		return nil, manager, err
	}
	handler, err := readfile.New(manager)
	if err != nil {
		return nil, manager, err
	}
	return readFileExecutor{handler: handler}, manager, nil
}

// All selected handlers borrow exactly one Manager from the locked identity.
func newOwnedOperations(configuration identity.Configuration, selected operations.Selection) (operationExecutor, ownedReadFileManager, error) {
	manager, err := newOwnedWorkspace(configuration)
	if manager == nil {
		return nil, nil, err
	}
	if err != nil {
		return nil, manager, err
	}
	executor := ownedOperationExecutor{selected: selected}
	if selected.Contains(p.ReadFileOperation) {
		executor.read, err = readfile.New(manager)
		if err != nil {
			return nil, manager, err
		}
	}
	if selected.Contains(p.ListDirectoryOperation) || selected.Contains(p.FindFilesOperation) {
		executor.directory, err = directory.New(manager)
		if err != nil {
			return nil, manager, err
		}
	}
	if selected.Contains(p.WriteFileOperation) {
		executor.write, err = writefile.New(manager)
		if err != nil {
			return nil, manager, err
		}
	}
	if selected.Contains(p.SkillPrepareOperation) {
		executor.skill, err = skillprepare.New(manager)
		if err != nil {
			return nil, manager, err
		}
	}
	if selected.Contains(p.RunCommandOperation) {
		handler, err := runcommand.New(manager)
		if err != nil {
			return nil, manager, err
		}
		return ownedCommandOperationExecutor{ownedOperationExecutor: executor, command: runCommandExecutor{handler: handler}}, manager, nil
	}
	return executor, manager, nil
}

// Only a successfully constructed command profile acquires its streaming and
// cancellation policy. Filesystem-only selections retain their original gates.
// Every handler still borrows the same Manager owned by Client.Run.
type ownedCommandOperationExecutor struct {
	ownedOperationExecutor
	command runCommandExecutor
}

func (ownedCommandOperationExecutor) commandCancellationTail() {}
func (e ownedCommandOperationExecutor) accepts(request p.Request) bool {
	if request.OperationName == p.RunCommandOperation {
		return e.command.accepts(request)
	}
	return e.ownedOperationExecutor.accepts(request)
}
func (e ownedCommandOperationExecutor) execute(ctx context.Context, request p.Request, sink func(string, string) error) (p.Response, error) {
	if request.OperationName == p.RunCommandOperation {
		return e.command.execute(ctx, request, sink)
	}
	return e.ownedOperationExecutor.execute(ctx, request, sink)
}
func (e ownedCommandOperationExecutor) maskingReady() bool { return e.command.maskingReady() }
func (e ownedCommandOperationExecutor) mask(data string) (string, error) {
	return e.command.mask(data)
}

type ownedOperationExecutor struct {
	selected  operations.Selection
	read      *readfile.Handler
	directory *directory.Handler
	write     *writefile.Handler
	skill     *skillprepare.Handler
}

func (e ownedOperationExecutor) accepts(request p.Request) bool {
	if !e.selected.Contains(request.OperationName) {
		return false
	}
	switch request.OperationName {
	case p.ReadFileOperation:
		return e.read.Accepts(request)
	case p.ListDirectoryOperation, p.FindFilesOperation:
		return e.directory.Accepts(request)
	case p.WriteFileOperation:
		return e.write.Accepts(request)
	case p.SkillPrepareOperation:
		return e.skill.Accepts(request)
	default:
		return false
	}
}
func (e ownedOperationExecutor) execute(ctx context.Context, request p.Request, _ func(string, string) error) (p.Response, error) {
	if !e.accepts(request) {
		return p.Response{}, ErrProtocol
	}
	switch request.OperationName {
	case p.ReadFileOperation:
		return e.read.Execute(ctx, request)
	case p.ListDirectoryOperation, p.FindFilesOperation:
		return e.directory.Execute(ctx, request)
	case p.WriteFileOperation:
		return e.write.Execute(ctx, request)
	case p.SkillPrepareOperation:
		return e.skill.Execute(ctx, request)
	default:
		return p.Response{}, ErrProtocol
	}
}
func (ownedOperationExecutor) maskingReady() bool          { return false }
func (ownedOperationExecutor) mask(string) (string, error) { return "", ErrProtocol }

// Transfer one Manager to the original Client.Run cleanup stack, including
// partial construction errors. No new root is inferred from a request.
func newOwnedWorkspace(configuration identity.Configuration) (*workspace.Manager, error) {
	if err := configuration.Validate(); err != nil {
		return nil, identity.ErrInvalid
	}
	directory, err := openReadFileRoot(configuration.RootPath)
	if err != nil {
		return nil, err
	}
	manager, makeErr := workspace.New(directory)
	// workspace.New duplicates the original fd before returning. Closing our
	// original is mandatory on both success and failure, before any connection.
	closeErr := directory.Close()
	if makeErr != nil || closeErr != nil {
		if closeErr != nil {
			makeErr = workspace.ErrUnavailable
		}
		return manager, makeErr
	}
	if manager == nil {
		return nil, workspace.ErrUnavailable
	}
	return manager, nil
}
