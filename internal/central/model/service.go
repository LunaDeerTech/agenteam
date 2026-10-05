// Package model implements model configuration and optionally bound current
// resolution. It does not invoke providers or supply consumer business authority.
package model

import (
	"context"
	"reflect"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type Dependencies struct {
	Secret              sc.UsageOperations
	Audit               ac.Appender
	Events              oc.Appender
	ConfigurationEvents ModelEvents
	Cursors             cursor.Keyring
}

type Service struct{ data func() *serviceState }
type serviceState struct {
	store     Store
	authority *Authority
	deps      Dependencies
}

func New(store Store, authority *Authority, d Dependencies) (*Service, error) {
	if nilPort(store) || authority.state() == nil || !sameStore(store, authority.state().store) || nilPort(d.Secret) || nilPort(d.Audit) || nilPort(d.Events) {
		return nil, fault(f.DependencyUnbound)
	}
	if d.Cursors.Validate() != nil || !d.ConfigurationEvents.valid() {
		return nil, fault(f.InvalidArgument)
	}
	s := &serviceState{store, authority, d}
	return &Service{data: func() *serviceState { return s }}, nil
}
func (s *Service) state() *serviceState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}
func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return r.IsNil()
	}
	return false
}
func sameStore(a, b Store) bool {
	return reflect.TypeOf(a) == reflect.TypeOf(b) && reflect.TypeOf(a).Comparable() && a == b
}

// Initialize persists a technical singleton identity, not a default model.
// Constructors remain free of I/O; repeated initialization preserves identity.
func (s *Service) Initialize(ctx context.Context) error {
	if s.state() == nil {
		return fault(f.DependencyUnbound)
	}
	cause, err := readCause("initialize")
	if err != nil {
		return err
	}
	newID, err := newID()
	if err != nil {
		return err
	}
	r := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, []f.LockRequest{systemLock("model-platform-selection", f.Exclusive)}); err != nil {
			return portError(err)
		}
		x, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		var count int64
		if err = x.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_model.providers WHERE false)+(SELECT count(*) FROM agenteam_model.models WHERE false)+(SELECT count(*) FROM agenteam_model.commands WHERE false)+(SELECT count(*) FROM agenteam_model.references WHERE false)`).Scan(&count); err != nil {
			return unavailable(err)
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_model.platform_selection(id,singleton,version,configured,updated_at) VALUES($1,true,1,false,clock_timestamp()) ON CONFLICT(singleton) DO NOTHING`, newID)
		if err != nil {
			return unavailable(err)
		}
		if s.state().authority.state().auth.Resolution != nil {
			if err = x.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_model.resolution_preparations WHERE false)+(SELECT count(*) FROM agenteam_model.snapshots WHERE false)+(SELECT count(*) FROM agenteam_model.snapshot_bindings WHERE false)`).Scan(&count); err != nil {
				return unavailable(err)
			}
		}
		_, err = loadSelection(ctx, x)
		return err
	})
	return commitError(r)
}
