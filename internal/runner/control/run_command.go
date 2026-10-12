package control

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/runner/runcommand"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// NewRunCommand explicitly binds the env-free Linux command profile. It does
// not change New, the production initializer or any Central authorization.
func NewRunCommand(options Options, handler *runcommand.Handler) (*Client, error) {
	if !handler.Accepts(p.Request{OperationName: p.RunCommandOperation, OperationRevision: p.RunCommandRevision}) {
		return nil, ErrProtocol
	}
	client, err := New(options)
	if err != nil {
		return nil, err
	}
	client.executor = runCommandExecutor{handler: handler}
	return client, nil
}

// Only an explicitly bound executor (or its explicit observing wrapper) has
// this private policy. An operation name in an untrusted DTO cannot enable it.
type commandTailAdmission interface{ commandCancellationTail() }

var errCommandTailCancelled = errors.New("command stream cancelled before admission")

type runCommandExecutor struct{ handler *runcommand.Handler }

func (runCommandExecutor) commandCancellationTail()   {}
func (e runCommandExecutor) accepts(r p.Request) bool { return e.handler.Accepts(r) }
func (e runCommandExecutor) execute(ctx context.Context, r p.Request, sink func(string, string) error) (p.Response, error) {
	return e.handler.Execute(ctx, r, func(stream, data string) error {
		err := sink(stream, data)
		if err == errCommandTailCancelled {
			return runcommand.ErrOutputCancelled
		}
		return err
	})
}

// This profile knows its injected Secret set is empty: Handler rejects even an
// empty non-nil Environment, and command.Owner builds a fixed child env. This
// is not a general masking provider or a guarantee about arbitrary host data.
func (runCommandExecutor) maskingReady() bool { return true }
func (runCommandExecutor) mask(data string) (string, error) {
	if !utf8.ValidString(data) {
		return "", ErrProtocol
	}
	return data, nil
}
