package object

import oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"

// RequireProjectGuardBinding checks the same static composition as NewRuntime.
// It performs no I/O and is not a current-process or transaction authorization.
func (s *Service) RequireProjectGuardBinding(guard *ProcessGuard, expected oc.ProcessID) error {
	if s == nil || s.data == nil || guard == nil || guard.data == nil || expected.Validate() != nil {
		return invalid()
	}
	r, g := s.state(), guard.state()
	if r == nil || g == nil {
		return invalid()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.auth.Processes != guard || r.spool != g.spool || r.process != expected || g.process != expected || g.service != nil && g.service != s {
		return invalid()
	}
	return nil
}
