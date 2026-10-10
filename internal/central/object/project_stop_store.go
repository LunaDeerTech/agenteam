package object

import (
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

const stopBatchLimit = 32

type projectStopRow struct {
	action, state, after string
	version              int64
	lane                 int
}

type projectStopFacts struct {
	ids, objects                        []string
	attempts, leases, grants, transfers []string
	processes                           []oc.ProcessID
	works                               []projectWork
	locks                               []foundation.LockRequest
	binding                             foundation.Digest
	overflow                            bool
}

// A plan is private to one Service and carries both the authority's mapping
// and our exact bounded native mapping. Neither input is an authorization.
type projectStopPlan struct {
	service      *Service
	request      oc.ProjectStopRequest
	dependencies oc.AccessDependencies
	lane         int
	after        string
	facts        projectStopFacts
	locks        []foundation.LockRequest
}

func readProjectStop(ctx context.Context, e postgres.SQLExecutor, cause oc.ProjectStopCause) (*projectStopRow, error) {
	d := cause.Details()
	var r projectStopRow
	err := e.QueryRow(ctx, `SELECT action,project_version,state,scan_kind,COALESCE(scan_after::text,'') FROM agenteam_object.project_stops WHERE project_id=$1 AND operation_id=$2`, d.ProjectID.String(), d.OperationID.String()).Scan(&r.action, &r.version, &r.state, &r.lane, &r.after)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if r.action != string(d.Action) || r.version != int64(d.ProjectVersion) {
		return nil, failure(foundation.InvalidState, nil)
	}
	return &r, nil
}

func stopWorkRelevant(kind string, action oc.ProjectStopAction) bool {
	return action == oc.ProjectStopDelete || kind == "preparation" || kind == "transfer_put" || kind == "verification" || kind == "cleanup"
}

func queryStopIDs(ctx context.Context, e postgres.SQLExecutor, query string, args ...any) ([]string, error) {
	rows, err := e.Query(ctx, query, args...)
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, unavailable(err)
		}
		ids = append(ids, id)
	}
	return ids, unavailableIf(rows.Err())
}

func (s *Service) discoverProjectStop(ctx context.Context, request oc.ProjectStopRequest, lane int, after string) (projectStopPlan, error) {
	p := projectStopPlan{service: s, request: request, lane: lane, after: after}
	var err error
	p.dependencies, err = s.state().auth.ProjectStop.DiscoverProjectStop(ctx, request)
	if err != nil {
		return p, portError(err)
	}
	if p.dependencies.Validate() != nil {
		return p, unavailable(nil)
	}
	p.facts, err = s.projectStopFacts(ctx, s.state().store, request.Details().Cause, lane, after)
	if err != nil {
		return p, err
	}
	p.locks, err = oc.NormalizeAccessLocks(append(p.dependencies.Locks(), p.facts.locks...))
	return p, err
}

func workProjection(works []projectWork) [][]any {
	out := make([][]any, 0, len(works))
	for _, w := range works {
		out = append(out, []any{w.id, w.project.String(), w.process.String(), w.kind, w.resource, w.object.String(), w.epoch, w.epochOperation, w.revoked, w.fence, w.joined})
	}
	return out
}

func (s *Service) projectStopTransaction(ctx context.Context, p projectStopPlan, fn func(context.Context, foundation.Tx, postgres.SQLExecutor, oc.ProjectStopAuthorization, projectStopFacts) error) foundation.CommitResult {
	if p.service != s || p.request.Validate() != nil {
		return rejectedAccess(invalid())
	}
	return s.state().store.WithinTx(ctx, recoveryCause(), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, p.locks); err != nil {
			return unavailable(err)
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		a, err := s.state().auth.ProjectStop.ValidateProjectStopInTx(ctx, tx, p.request, p.dependencies)
		if err != nil {
			return portError(err)
		}
		if !a.Matches(tx, p.request, p.dependencies) {
			return failure(foundation.Forbidden, nil)
		}
		f, err := s.projectStopFacts(ctx, e, p.request.Details().Cause, p.lane, p.after)
		if err != nil {
			return err
		}
		if f.binding != p.facts.binding {
			return accessChanged()
		}
		return fn(ctx, tx, e, a, f)
	})
}

// Full existence predicates, rather than bounded diagnostics or cursor state,
// establish completion under the current Project EX gate.
// Keep terminal metadata and live native rows in separate existence checks:
// an OR across joined tables otherwise prevents their pending indexes from
// excluding unrelated terminal history. Every arm retains its original join
// and Project/action binding, including inside a deferred-constraint Tx.
func projectStopPending(ctx context.Context, e postgres.SQLExecutor, cause oc.ProjectStopCause) (bool, error) {
	d := cause.Details()
	var pending bool
	err := e.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM agenteam_object.project_work w WHERE w.project_id=$1 AND w.joined_at IS NULL AND ($2 OR w.kind IN ('preparation','transfer_put','verification','cleanup')))
 OR EXISTS(SELECT 1 FROM agenteam_object.uploads u WHERE u.project_id=$1 AND u.disposition='reserved')
 OR EXISTS(SELECT 1 FROM agenteam_object.upload_attempts a JOIN agenteam_object.objects o ON o.id=a.object_id WHERE o.project_id=$1 AND a.kind='private_candidate' AND NOT a.io_closed)
 OR EXISTS(SELECT 1 FROM agenteam_object.cleanup_operations c JOIN agenteam_object.objects o ON o.id=c.object_id WHERE o.project_id=$1 AND c.phase='applying' AND NOT EXISTS(SELECT 1 FROM agenteam_object.project_work w WHERE w.id=c.worker_id AND w.kind='cleanup' AND w.resource_id=c.id AND w.cleanup_claim_fence=c.fence AND w.joined_at IS NOT NULL))
 OR EXISTS(SELECT 1 FROM agenteam_object.object_leases l JOIN agenteam_object.objects o ON o.id=l.object_id LEFT JOIN agenteam_object.object_transfers t ON t.lease_id=l.id WHERE o.project_id=$1 AND l.state='active' AND ($2 OR l.owner_kind='writer' OR l.owner_kind='transfer' AND t.direction='put'))
 OR ($2 AND EXISTS(SELECT 1 FROM agenteam_download.grants g WHERE g.project_id=$1 AND NOT g.revoked))
 OR ($2 AND EXISTS(SELECT 1 FROM agenteam_download.attempts a JOIN agenteam_download.grants g ON g.id=a.grant_id WHERE g.project_id=$1 AND a.phase='started' AND (a.pending_phase IS NULL OR NOT EXISTS(SELECT 1 FROM agenteam_object.project_work w WHERE w.kind='download' AND w.resource_id=a.id AND w.joined_at IS NOT NULL))))
 OR EXISTS(SELECT 1 FROM agenteam_object.object_transfers t JOIN agenteam_object.object_leases l ON l.id=t.lease_id WHERE t.project_id=$1 AND ($2 OR t.direction='put') AND (t.revoked_at IS NULL OR t.retirement_evidence IS NULL))
 OR EXISTS(SELECT 1 FROM agenteam_object.object_leases l JOIN agenteam_object.object_transfers t ON t.lease_id=l.id WHERE t.project_id=$1 AND ($2 OR t.direction='put') AND l.state='active')`, d.ProjectID.String(), d.Action == oc.ProjectStopDelete).Scan(&pending)
	return pending, unavailableIf(err)
}
