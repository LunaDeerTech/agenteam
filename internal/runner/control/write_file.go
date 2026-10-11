package control

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/runner/writefile"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// NewWriteFile explicitly binds only write-file revision 1. The caller owns
// the trusted-root Manager behind handler until Client.Run actually joins.
// New and the production command remain unbound; this is not a Central grant.
func NewWriteFile(options Options, handler *writefile.Handler) (*Client, error) {
	if !handler.Accepts(p.Request{OperationName: p.WriteFileOperation, OperationRevision: p.WriteFileRevision}) {
		return nil, ErrProtocol
	}
	client, err := New(options)
	if err != nil {
		return nil, err
	}
	client.executor = writeFileExecutor{handler: handler}
	return client, nil
}

type writeFileExecutor struct{ handler *writefile.Handler }

func (e writeFileExecutor) execute(ctx context.Context, request p.Request, _ func(string, string) error) (p.Response, error) {
	return e.handler.Execute(ctx, request)
}
func (e writeFileExecutor) accepts(request p.Request) bool { return e.handler.Accepts(request) }
func (writeFileExecutor) maskingReady() bool               { return false }
func (writeFileExecutor) mask(string) (string, error)      { return "", ErrProtocol }
