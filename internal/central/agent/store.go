// Package agent owns canonical Agent configuration. It does not infer execution
// readiness, activity or deletion safety from a configuration row.
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type Store interface {
	postgres.SQLExecutor
	InTx(f.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	AcquireAll(context.Context, f.Tx, []f.LockRequest) error
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}

func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func fault(code f.Code) *f.Fault { return f.NewFault(code, f.NotStarted) }
func invalid() error             { return fault(f.InvalidArgument) }

// Raw SQL errors can contain configuration text. Retain no public unwrap path.
type privateStorageError struct{ err error }

func (privateStorageError) Error() string              { return "agent_storage" }
func (privateStorageError) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "agent_storage") }
func (privateStorageError) LogValue() slog.Value       { return slog.StringValue("agent_storage") }
func unavailable(err error) error {
	if err == nil {
		return fault(f.DependencyUnavailable)
	}
	return fault(f.DependencyUnavailable).WithCause(privateStorageError{err})
}
func portError(err error) error {
	var known *f.Fault
	if err == nil || errors.As(err, &known) {
		return err
	}
	return unavailable(err)
}
func canonical(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, unavailable(err)
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return nil, unavailable(err)
	}
	return raw, nil
}
func hash(value any) (f.Digest, error) {
	raw, err := canonical(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}
func sameValue(a, b any) bool {
	x, err := hash(a)
	if err != nil {
		return false
	}
	y, err := hash(b)
	return err == nil && x == y
}
func userLock(actor i.Actor, mode f.LockMode) f.LockRequest {
	k, _ := f.UserLock(actor.Details().UserID)
	return f.LockRequest{Key: k, Mode: mode}
}
func projectLock(project i.ProjectID, mode f.LockMode) f.LockRequest {
	k, _ := f.ProjectLock(project.String())
	return f.LockRequest{Key: k, Mode: mode}
}
func agentLock(agent i.AgentID, mode f.LockMode) f.LockRequest {
	k, _ := f.AgentLock(agent.String())
	return f.LockRequest{Key: k, Mode: mode}
}
func commandLock(command f.CommandIdentity) f.LockRequest {
	k, _ := f.CommandLock(command)
	return f.LockRequest{Key: k, Mode: f.Exclusive}
}
func currentActor(actor i.Actor) error {
	if actor.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	switch actor.Details().Kind {
	case i.Human:
		return nil
	case i.AgentRun:
		return fault(f.DependencyUnbound)
	default:
		return fault(f.Forbidden)
	}
}
