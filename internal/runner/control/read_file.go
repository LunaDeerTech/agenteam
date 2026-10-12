package control

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/runner/readfile"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// NewReadFile explicitly binds only read-file revision 1 to the authenticated
// client. The caller must supply a Handler backed by a workspace.New Manager
// for this device's trusted root, and own that Manager until Client.Run joins.
// New and the default command continue to advertise no operations.
func NewReadFile(options Options, handler *readfile.Handler) (*Client, error) {
	if !handler.Accepts(p.Request{OperationName: p.ReadFileOperation, OperationRevision: p.ReadFileRevision}) {
		return nil, ErrProtocol
	}
	client, err := New(options)
	if err != nil {
		return nil, err
	}
	client.executor = readFileExecutor{handler: handler}
	return client, nil
}

type readFileExecutor struct{ handler *readfile.Handler }

func (e readFileExecutor) execute(ctx context.Context, request p.Request, _ func(string, string) error) (p.Response, error) {
	return e.handler.Execute(ctx, request)
}
func (e readFileExecutor) accepts(request p.Request) bool { return e.handler.Accepts(request) }
func (readFileExecutor) maskingReady() bool               { return false }
func (readFileExecutor) mask(string) (string, error)      { return "", ErrProtocol }

func (c *Client) connectionHello(id p.ID, version string) p.Hello {
	hello := clientHello(id, version)
	switch c.executor.(type) {
	case readFileExecutor:
		hello.Capabilities = []string{p.ReadFileOperation}
	case writeFileExecutor:
		hello.Capabilities = []string{p.WriteFileOperation}
	case runCommandExecutor:
		hello.Capabilities = []string{p.RunCommandOperation}
	case directoryExecutor:
		hello.Capabilities = []string{p.ListDirectoryOperation, p.FindFilesOperation}
	}
	return hello
}
