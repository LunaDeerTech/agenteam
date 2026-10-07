package secret

import (
	"context"
	"errors"
	"reflect"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

var _ sc.HumanWriteCommands = (*Service)(nil)

func lookupNil(v any) bool {
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

// LookupWriteCommand reads only the receipt's safe historical columns. A
// successful absence is an observation, never a negative commit decision.
func (s *Service) LookupWriteCommand(ctx context.Context, r sc.WriteCommandLookupRequest) (sc.WriteCommandObservation, error) {
	var zero sc.WriteCommandObservation
	if s == nil || s.data == nil {
		return zero, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	state := s.state()
	if state == nil || lookupNil(state.store) || lookupNil(state.auth.Sessions) || r.Scope.Details().Kind != id.ProjectScope && lookupNil(state.auth.System) {
		return zero, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	if ctx == nil || r.Validate() != nil {
		return zero, invalid()
	}
	cause, err := recoveryCause("secret-write-lookup")
	if err != nil {
		return zero, err
	}
	command, _ := f.CommandLock(r.Identity)
	user, _ := f.UserLock(r.Actor.Details().UserID)
	locks := []f.LockRequest{{Key: command, Mode: f.Shared}, {Key: user, Mode: f.Shared}}
	if r.Scope.Details().Kind == id.ProjectScope {
		project, _ := f.ProjectLock(r.Scope.Details().ProjectID)
		locks = append(locks, f.LockRequest{Key: project, Mode: f.Shared})
	}
	digest, err := cursor.Digest([]byte(r.Identity.Canonical()))
	if err != nil {
		return zero, invalid()
	}
	var observed sc.WriteCommandObservation
	committed := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := state.store.AcquireAll(ctx, tx, locks); err != nil {
			return lookupError(err)
		}
		x, err := state.store.InTx(tx)
		if err != nil {
			return lookupError(err)
		}
		if lookupNil(x) {
			return unavailable(nil)
		}
		if err = state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return lookupError(err)
		}
		// A Project lookup has no System authority dependency. As in Metadata,
		// preserve current Session rejection before reporting an unbound scope
		// port, including named nil channels that implement the interface.
		if r.Scope.Details().Kind == id.ProjectScope && lookupNil(state.auth.Projects) {
			if err = state.auth.Sessions.RequireCurrentSession(ctx, tx, r.Actor); err != nil {
				return authorization(err)
			}
			return failure(AuthorizationUnbound, f.DependencyUnbound, nil)
		}
		if err = s.authorize(ctx, tx, r.Actor, r.Scope, id.Read); err != nil {
			return err
		}
		var kind, credential, purpose string
		var version int64
		var deleted bool
		err = x.QueryRow(ctx, `SELECT mutation_kind,credential_id::text,purpose,result_version,deleted FROM agenteam_secret.secret_command_receipts WHERE scope=$1 AND scope_key=$2 AND command_digest=$3`, string(r.Scope.Details().Kind), scopeKey(r.Scope), string(digest)).Scan(&kind, &credential, &purpose, &version, &deleted)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return lookupError(err)
		}
		refID, err := f.ParseID[sc.Credential](credential)
		v, k, p := f.Version(version), sc.MutationKind(kind), sc.Purpose(purpose)
		if err != nil || v.Validate() != nil || !p.Valid() || (k != sc.Create && k != sc.Update && k != sc.Delete) || deleted != (k == sc.Delete) || k == sc.Create && v != 1 || k != sc.Create && v < 2 {
			return unavailable(nil)
		}
		if k != r.Kind || p != r.Purpose || k != sc.Create && (r.Ref.Details().ID != refID || v != r.ExpectedVersion+1) {
			return failure(KeyReused, f.IdempotencyKeyReused, nil)
		}
		ref, err := sc.NewCredentialRef(refID, r.Scope)
		if err != nil {
			return unavailable(err)
		}
		observed = sc.WriteCommandObservation{Observed: true, Result: &sc.MutationResult{Metadata: sc.Metadata{CredentialRef: ref, Purpose: p, Version: v}, Deleted: deleted}}
		return nil
	})
	if err = commitError(committed); err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, f.NewFault(f.DependencyUnavailable, f.Committed).WithCause(err)
	}
	return observed, nil
}

func lookupError(err error) error {
	var fault *f.Fault
	if errors.As(err, &fault) {
		return err
	}
	return unavailable(err)
}
