package mcp

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func (s *Service) recoverStartupRequests(ctx context.Context) error {
	after := ""
	for {
		rows, err := s.store.Query(ctx, `SELECT id FROM agenteam_mcp.startup_requests WHERE process_id<>$1 AND (phase='pending' OR NOT lease_released) AND id>$2 ORDER BY id LIMIT 64`, s.owner.process.String(), after)
		if err != nil {
			return safeError(err)
		}
		var batch []string
		for rows.Next() {
			var request string
			if err = rows.Scan(&request); err != nil {
				break
			}
			if _, err = f.ParseID[f.Request](request); err != nil || len(batch) == 64 {
				err = fail(f.InvalidState)
				break
			}
			batch = append(batch, request)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return safeError(err)
		}
		if len(batch) == 0 {
			return ctx.Err()
		}
		for _, request := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			_ = s.recoverStartupRequest(ctx, request)
			after = request
		}
	}
}

func (s *Service) recoverStartupRequest(parent context.Context, request string) error {
	ctx, call, err := s.owner.begin(parent)
	if err != nil {
		return err
	}
	retained := false
	defer func() {
		if !retained {
			call.returnedWith(nil)
		}
	}()
	fact, err := loadStartupFact(ctx, s.store, request)
	if err != nil {
		return err
	}
	process, err := f.ParseID[oc.Process](fact.process)
	if err != nil || process == s.owner.process {
		return fail(f.Forbidden)
	}
	// This checks the exact original guard file/lock, host, deployment and
	// persisted process claim. Neither time nor a terminal business row proves
	// that its network/body has physically returned.
	if err = s.owner.guard.ConfirmStopped(ctx, process); err != nil {
		return safeError(err)
	}
	if fact.retirement != "" && fact.retirement != s.owner.process.String() && fact.retirement != fact.process {
		previous, err := f.ParseID[oc.Process](fact.retirement)
		if err != nil {
			return fail(f.InvalidState)
		}
		if err = s.owner.guard.ConfirmStopped(ctx, previous); err != nil {
			return safeError(err)
		}
	}
	w, err := s.recoveredStartupUse(call, request, fact)
	if err != nil {
		return err
	}
	w.stopped = true
	w.active.Store(true)
	ctx = context.WithValue(ctx, startupUseKey{}, w)
	confirmed, err := s.retireStartup(ctx, w)
	if err == nil && confirmed {
		w.active.Store(false)
		return nil
	}
	retained = true
	call.returnedWith(func(observe context.Context) (bool, error) {
		confirmed, err := s.retireStartup(context.WithValue(observe, startupUseKey{}, w), w)
		if err == nil && confirmed {
			w.active.Store(false)
		}
		return confirmed, err
	})
	return err
}

// Parsing a row does not establish stopped=true or activate this witness.
func (s *Service) recoveredStartupUse(call *processCall, request string, fact startupFact) (*startupUse, error) {
	w := &startupUse{service: s, call: call, request: request, previousRetirement: fact.retirement}
	var err error
	if _, err = f.ParseID[f.Request](request); err != nil {
		return nil, fail(f.InvalidState)
	}
	if w.process, err = f.ParseID[oc.Process](fact.process); err != nil {
		return nil, fail(f.InvalidState)
	}
	if w.connection.ID, err = f.ParseID[ConnectionIdentity](fact.connection); err != nil {
		return nil, fail(f.InvalidState)
	}
	if w.connection.ProjectID, err = f.ParseID[id.Project](fact.project); err != nil {
		return nil, fail(f.InvalidState)
	}
	if w.config.ID, err = f.ParseID[ConfigIdentity](fact.config); err != nil {
		return nil, fail(f.InvalidState)
	}
	w.connection.ConfigID, w.connection.ConfigRevision, w.connection.Version = w.config.ID, fact.revision, fact.version
	w.connection.RuntimeBindingID, w.connection.CredentialID = fact.runtime, fact.credential
	w.config.RuntimeRevision = fact.revision
	if fact.revision.Validate() != nil || fact.version.Validate() != nil {
		return nil, fail(f.InvalidState)
	}
	if fact.credential != "" {
		if _, err = f.ParseID[sc.Credential](fact.credential); err != nil {
			return nil, fail(f.InvalidState)
		}
		if w.lease, err = f.ParseID[sc.Lease](fact.lease); err != nil {
			return nil, fail(f.InvalidState)
		}
	} else if fact.lease != "" {
		return nil, fail(f.InvalidState)
	}
	if fact.runtime != "" {
		if _, err = f.ParseID[struct{}](fact.runtime); err != nil {
			return nil, fail(f.InvalidState)
		}
	}
	scope, _ := id.InProject(w.connection.ProjectID)
	registration, _ := id.RegisterService(id.MCPRuntime)
	w.actor, err = registration.Actor(request, scope)
	if err != nil {
		return nil, err
	}
	w.locks, err = startupLocks(w.connection.ProjectID, w.connection.ID)
	return w, err
}
