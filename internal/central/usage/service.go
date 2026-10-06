// Package usage persists trusted Runtime observations and provides current
// Owner reads. It creates neither calls nor permission to invoke a Provider.
package usage

import (
	"context"
	"reflect"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

type Dependencies struct{ Cursors cursor.Keyring }
type Service struct{ data func() *serviceState }
type serviceState struct {
	store     Store
	authority *Authority
	deps      Dependencies
	issuer    uc.PlanIssuer
}

func New(store Store, a *Authority, d Dependencies) (*Service, error) {
	if nilPort(store) || a.state() == nil || !sameStore(store, a.state().store) {
		return nil, fault(f.DependencyUnbound)
	}
	if d.Cursors.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	v := &serviceState{store, a, d, uc.NewPlanIssuer()}
	return &Service{func() *serviceState { return v }}, nil
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
	return !nilPort(a) && !nilPort(b) && reflect.TypeOf(a) == reflect.TypeOf(b) && reflect.TypeOf(a).Comparable() && a == b
}
func contextError(ctx context.Context) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	return ctx.Err()
}

// Initialization only checks this library's schema. No default projections or
// background work are created by either construction or initialization.
func (s *Service) Initialize(ctx context.Context) error {
	if e := contextError(ctx); e != nil {
		return e
	}
	if s.state() == nil {
		return fault(f.DependencyUnbound)
	}
	for _, q := range []string{
		`SELECT ` + invocationColumns + ` FROM agenteam_model.invocations WHERE false`,
		`SELECT invocation_id,project_id,sequence,action,fact_digest,receipt_data,recorded_at FROM agenteam_model.invocation_observations WHERE false`,
		`SELECT project_id,execution_id,version,updated_at,` + summaryColumns + ` FROM agenteam_model.execution_usage_summaries WHERE false`,
	} {
		rows, e := s.state().store.Query(ctx, q)
		if e != nil {
			return dbError(e)
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			return dbError(e)
		}
	}
	return nil
}

var _ uc.Writer = (*Service)(nil)
var _ uc.Reader = (*Service)(nil)
