package control

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/runner/directory"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// NewDirectoryReads explicitly binds only list-directory and find-files revision
// 1. The caller supplies a Handler backed by workspace.New for this device's
// trusted root and retains the Manager until Client.Run has actually joined.
// It does not enable read-file, streams, environment injection or default cmd use.
func NewDirectoryReads(options Options, handler *directory.Handler) (*Client, error) {
	for _, operation := range []string{p.ListDirectoryOperation, p.FindFilesOperation} {
		if !handler.Accepts(p.Request{OperationName: operation, OperationRevision: p.DirectoryRevision}) {
			return nil, ErrProtocol
		}
	}
	client, err := New(options)
	if err != nil {
		return nil, err
	}
	client.executor = directoryExecutor{handler: handler}
	return client, nil
}

type directoryExecutor struct{ handler *directory.Handler }

func (e directoryExecutor) execute(ctx context.Context, request p.Request, _ func(string, string) error) (p.Response, error) {
	return e.handler.Execute(ctx, request)
}
func (e directoryExecutor) accepts(request p.Request) bool { return e.handler.Accepts(request) }
func (directoryExecutor) maskingReady() bool               { return false }
func (directoryExecutor) mask(string) (string, error)      { return "", ErrProtocol }
