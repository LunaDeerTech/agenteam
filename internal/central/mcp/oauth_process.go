package mcp

import (
	"context"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/jackc/pgx/v5"
)

type oauthProcessKey struct{}
type oauthProcessCall struct {
	service                  *Service
	call                     *processCall
	requestID, commandDigest string
}

func (s *Service) beginOAuthProcess(ctx context.Context) (context.Context, *oauthProcessCall, error) {
	if s == nil || s.owner == nil {
		return nil, nil, fail(f.DependencyUnbound)
	}
	child, call, err := s.owner.begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	w := &oauthProcessCall{service: s, call: call}
	return context.WithValue(child, oauthProcessKey{}, w), w, nil
}

func (s *Service) oauthProcessInTx(ctx context.Context, tx f.Tx, request, command string) error {
	w, ok := ctx.Value(oauthProcessKey{}).(*oauthProcessCall)
	if !ok || w == nil || w.service != s || w.call == nil || request == "" || command == "" {
		return fail(f.Forbidden)
	}
	if w.requestID != "" && (w.requestID != request || w.commandDigest != command) {
		return fail(f.Forbidden)
	}
	if err := s.owner.requireInTx(ctx, tx, w.call); err != nil {
		return err
	}
	w.requestID, w.commandDigest = request, command
	return nil
}

func (s *Service) requireOAuthProcessInTx(ctx context.Context, tx f.Tx) error {
	if s == nil || s.owner == nil || ctx == nil {
		return fail(f.Forbidden)
	}
	if ctx.Value(oauthRecoveryKey{}) != nil {
		w, err := s.oauthRecoveryUse(ctx)
		if err != nil {
			return err
		}
		_, err = w.require(ctx, tx)
		return err
	}
	if refresh, ok := ctx.Value(oauthRefreshKey{}).(*oauthRefresh); ok {
		if refresh == nil || refresh.service != s {
			return fail(f.Forbidden)
		}
		return refresh.requireProcess(ctx, tx)
	}
	w, ok := ctx.Value(oauthProcessKey{}).(*oauthProcessCall)
	if !ok || w == nil || w.service != s || w.requestID == "" || w.commandDigest == "" {
		return fail(f.Forbidden)
	}
	return s.owner.requireInTx(ctx, tx, w.call)
}

func (w *oauthProcessCall) returned(ctx context.Context) {
	if w.requestID == "" {
		w.call.returnedWith(nil)
		return
	}
	observe := func(ctx context.Context) (bool, error) { return w.observe(ctx) }
	tail, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	confirmed, err := observe(tail)
	cancel()
	if err == nil && confirmed {
		w.call.returnedWith(nil)
	} else {
		w.call.returnedWith(observe)
	}
}

// Observation is made only after the original call has returned. It keeps a
// nonterminal original request owned; neither absence of a local map nor an
// elapsed budget closes its lease or allows another exchange.
func (w *oauthProcessCall) observe(ctx context.Context) (bool, error) {
	s := w.service
	cause, _ := f.NewRecoveryCause("mcp.oauth.process", w.requestID, "")
	confirmed := false
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, []f.LockRequest{catalogLock(f.Shared)}); err != nil {
			return err
		}
		if err := s.owner.requireInTx(ctx, tx, w.call); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		var phase, operation string
		var retired, released bool
		err = x.QueryRow(ctx, `SELECT phase,operation,io_retired,lease_released FROM agenteam_mcp.oauth_requests WHERE id=$1 AND command_digest=$2 AND process_id=$3 AND process_required`, w.requestID, w.commandDigest, s.owner.process.String()).Scan(&phase, &operation, &retired, &released)
		if errors.Is(err, pgx.ErrNoRows) {
			// The catalog lock waits out the original transaction. Before the
			// original prepare commits, no protocol or Secret I/O can begin.
			var exists bool
			if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests WHERE id=$1)`, w.requestID).Scan(&exists); err != nil {
				return err
			}
			confirmed = !exists
			return nil
		}
		if err != nil {
			return err
		}
		confirmed = (phase == "complete" || phase == "failed") && retired && released
		if confirmed && operation == "exchange" && phase == "complete" {
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.connection_requests WHERE command_digest=$1 AND io_retired AND lease_released)`, w.commandDigest).Scan(&confirmed)
		}
		return err
	})
	if err := commit(result); err != nil {
		return false, err
	}
	return confirmed, nil
}
