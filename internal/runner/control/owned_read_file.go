package control

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	"github.com/LunaDeerTech/agenteam/internal/runner/readfile"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
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
	if err := configuration.Validate(); err != nil {
		return nil, nil, identity.ErrInvalid
	}
	directory, err := openReadFileRoot(configuration.RootPath)
	if err != nil {
		return nil, nil, err
	}
	manager, makeErr := workspace.New(directory)
	// workspace.New duplicates the original fd before returning. Closing our
	// original is mandatory on both success and failure, before any connection.
	closeErr := directory.Close()
	if makeErr != nil || closeErr != nil {
		if closeErr != nil {
			makeErr = workspace.ErrUnavailable
		}
		if manager != nil {
			return nil, manager, makeErr
		}
		return nil, nil, makeErr
	}
	if manager == nil {
		return nil, nil, workspace.ErrUnavailable
	}
	handler, err := readfile.New(manager)
	if err != nil {
		return nil, manager, err
	}
	return readFileExecutor{handler: handler}, manager, nil
}
