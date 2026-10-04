package secret

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func preparedLocks(p preparedWrite) ([]foundation.LockRequest, error) {
	locks, e := mutationLocks(p.command, p.ref, p.actor)
	if e != nil {
		return nil, e
	}
	if p.serviceRequest.Validate() == nil {
		locks = append(locks, p.dependencies.Locks()...)
	}
	return locks, nil
}
func (p PreparedWrite) Ref() sc.CredentialRef {
	if p.data == nil {
		return sc.CredentialRef{}
	}
	return p.data().ref
}
func (p PreparedWrite) RequiredLocks() []foundation.LockRequest {
	if p.data == nil {
		return nil
	}
	l, _ := preparedLocks(p.data())
	return l
}

type PreparedServiceWrite struct{ data func() preparedWrite }

func (p PreparedServiceWrite) Ref() sc.CredentialRef {
	if p.data == nil {
		return sc.CredentialRef{}
	}
	return p.data().ref
}
func (p PreparedServiceWrite) RequiredLocks() []foundation.LockRequest {
	if p.data == nil {
		return nil
	}
	l, _ := preparedLocks(p.data())
	return l
}
func (p PreparedServiceWrite) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "prepared_account_secret_write")
}
func (p PreparedServiceWrite) MarshalJSON() ([]byte, error) {
	return []byte(`"prepared_account_secret_write"`), nil
}
func (*PreparedServiceWrite) UnmarshalJSON([]byte) error { return invalid() }
func (p PreparedServiceWrite) LogValue() slog.Value {
	return slog.StringValue("prepared_account_secret_write")
}
func (s *Service) PrepareServiceWrite(ctx context.Context, r sc.ServiceWriteRequest) (PreparedServiceWrite, error) {
	if r.Validate() != nil {
		return PreparedServiceWrite{}, invalid()
	}
	port := s.state().auth.AccountWrites
	if nilPort(port) {
		return PreparedServiceWrite{}, failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	deps, err := port.DiscoverServiceWrite(ctx, r.WithoutMaterial())
	if err != nil {
		return PreparedServiceWrite{}, plannedAuthorization(err)
	}
	binding, err := sc.ServiceWriteBinding(r)
	if err != nil || deps.Validate() != nil || deps.Binding() != binding {
		return PreparedServiceWrite{}, invalid()
	}
	f := r.Fields()
	p, err := s.prepareWrite(ctx, sc.WriteRequest{Actor: f.Actor, Scope: f.Scope, Identity: f.Identity, Kind: f.Kind, Ref: f.Ref, ExpectedVersion: f.ExpectedVersion, Purpose: f.Purpose, Value: f.Value}, r, deps)
	if err != nil {
		return PreparedServiceWrite{}, err
	}
	return PreparedServiceWrite{p.data}, nil
}
func (s *Service) ApplyServiceWriteInTx(ctx context.Context, tx foundation.Tx, p PreparedServiceWrite) (sc.MutationResult, error) {
	if p.data == nil || p.data().issuer != s.state() || p.data().serviceRequest.Validate() != nil {
		return sc.MutationResult{}, failure(PreparationRequired, foundation.ResourceBusy, nil)
	}
	return s.applyWriteInTx(ctx, tx, p.data())
}
func (s *Service) LookupServiceWrite(ctx context.Context, p PreparedServiceWrite) (MutationLookup, error) {
	if p.data == nil || p.data().issuer != s.state() || p.data().serviceRequest.Validate() != nil {
		return MutationLookup{}, invalid()
	}
	d := p.data()
	cause, err := foundation.NewCommandsCause(d.command)
	if err != nil {
		return MutationLookup{}, invalid()
	}
	var out MutationLookup
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, p.RequiredLocks()); err != nil {
			return unavailable(err)
		}
		port := s.state().auth.AccountWrites
		if nilPort(port) {
			return failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
		}
		if err := port.ValidateServiceWriteInTx(ctx, tx, d.serviceRequest, d.dependencies); err != nil {
			return plannedAuthorization(err)
		}
		e, err := s.state().store.InTx(tx)
		if err != nil {
			return unavailable(err)
		}
		saved, found, err := s.findReceipt(ctx, e, d)
		if err != nil {
			return err
		}
		if found {
			if !saved.Metadata.CredentialRef.Equal(d.ref) {
				return failure(Busy, foundation.ResourceBusy, nil)
			}
			out = MutationLookup{Observed: true, Result: &saved}
		}
		return nil
	})
	if err := commitError(result); err != nil {
		return MutationLookup{}, err
	}
	return out, nil
}

func plannedAuthorization(e error) error {
	var f *foundation.Fault
	if errors.As(e, &f) && f != nil && f.Code == foundation.ResourceBusy {
		return failure(Busy, foundation.ResourceBusy, e)
	}
	return authorization(e)
}
