package outbox

import (
	"context"
	"errors"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// RegisterHandler owns only the registry transaction. It never invokes the
// handler or reads a domain table. Bootstrap belongs after this returns.
func (s *Service) RegisterHandler(ctx context.Context, input oc.HandlerDefinition) (oc.Registration, error) {
	def, digest, err := normalizeDefinition(input)
	if err != nil {
		return oc.Registration{}, err
	}
	r := s.state()
	// Serialize local publication as well as DB registration: a slower caller
	// must not replace a newer in-process declaration after its commit.
	r.mu.Lock()
	defer r.mu.Unlock()
	registrationID, err := foundation.NewID[foundation.TransactionAttempt]()
	if err != nil {
		return oc.Registration{}, unavailable(err)
	}
	locks := []foundation.LockRequest{{Key: registryLock(), Mode: foundation.Exclusive}, {Key: handlerLock(def.ID), Mode: foundation.Exclusive}}
	var result oc.Registration
	commit := r.store.WithinTx(ctx, recoveryCause("outbox.registration"), func(ctx context.Context, tx foundation.Tx) error {
		if err := r.store.AcquireAll(ctx, tx, locks); err != nil {
			return unavailable(err)
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		result, err = registerInTx(ctx, x, def, digest, registrationID.String())
		return err
	})
	if commit.State() == foundation.Unknown {
		// A lookup without these locks could see absence while the original COMMIT
		// is still pending. Even this confirmation must itself finish committed.
		confirmed := r.store.WithinTx(ctx, recoveryCause("outbox.registration-confirm"), func(ctx context.Context, tx foundation.Tx) error {
			if err := r.store.AcquireAll(ctx, tx, locks); err != nil {
				return unavailable(err)
			}
			x, err := executor(s, tx)
			if err != nil {
				return err
			}
			saved, subs, err := readRegistration(ctx, x, def, registrationID.String())
			if err != nil {
				return err
			}
			if saved != digest.String() || !sameSubscriptions(def.Subscriptions, subs) {
				return failure(foundation.CommitUnknown, nil)
			}
			result = oc.Registration{HandlerID: def.ID, Subscriptions: subs}
			return nil
		})
		if confirmed.State() != foundation.Committed {
			return oc.Registration{}, foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
		}
	} else if err = commitError(commit); err != nil {
		return oc.Registration{}, err
	}
	r.handlers[def.ID] = def
	return result, nil
}

func registerInTx(ctx context.Context, x postgres.SQLExecutor, def oc.HandlerDefinition, digest foundation.Digest, registrationID string) (oc.Registration, error) {
	saved, old, err := readRegistration(ctx, x, def, registrationID)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return oc.Registration{}, err
	}
	var watermark int64
	var called bool
	if err = x.QueryRow(ctx, `SELECT last_value,is_called FROM agenteam_outbox.outbox_sequence`).Scan(&watermark, &called); err != nil {
		return oc.Registration{}, unavailable(err)
	}
	if !called {
		watermark = 0
	}
	if watermark < 0 {
		return oc.Registration{}, unavailable(nil)
	}
	if !exists {
		var n int
		if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_outbox.handlers`).Scan(&n); err != nil {
			return oc.Registration{}, unavailable(err)
		}
		if n >= 128 {
			return oc.Registration{}, failure(foundation.InvalidState, nil)
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_outbox.handlers(id,effect,ordering_policy,declaration_digest) VALUES($1,$2,$3,$4)`, string(def.ID), string(def.Effect), string(def.Ordering), digest.String()); err != nil {
			return oc.Registration{}, unavailable(err)
		}
	} else {
		prior := def
		prior.Subscriptions = nil
		for _, sub := range old {
			prior.Subscriptions = append(prior.Subscriptions, oc.Subscription{EventType: sub.EventType, Versions: sub.AcceptedVersions})
		}
		_, oldDigest, err := normalizeDefinition(prior)
		if err != nil || oldDigest.String() != saved {
			return oc.Registration{}, unavailable(err)
		}
	}
	wanted := make(map[event.StableName]oc.Subscription, len(def.Subscriptions))
	for _, sub := range def.Subscriptions {
		wanted[sub.EventType] = sub
	}
	previous := make(map[event.StableName]oc.SubscriptionBoundary, len(old))
	for _, sub := range old {
		next, ok := wanted[sub.EventType]
		if !ok {
			return oc.Registration{}, failure(foundation.InvalidState, nil)
		}
		for _, v := range sub.AcceptedVersions {
			found := false
			for _, n := range next.Versions {
				if n == v {
					found = true
				}
			}
			if !found {
				return oc.Registration{}, failure(foundation.InvalidState, nil)
			}
		}
		previous[sub.EventType] = sub
	}
	for _, sub := range def.Subscriptions {
		versions := make([]int64, len(sub.Versions))
		for i, v := range sub.Versions {
			versions[i] = int64(v)
		}
		if _, ok := previous[sub.EventType]; ok {
			if _, err = x.Exec(ctx, `UPDATE agenteam_outbox.subscriptions SET accepted_versions=$3 WHERE handler_id=$1 AND event_type=$2`, string(def.ID), string(sub.EventType), versions); err != nil {
				return oc.Registration{}, unavailable(err)
			}
		} else {
			var n int
			if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_outbox.subscriptions WHERE event_type=$1`, string(sub.EventType)).Scan(&n); err != nil {
				return oc.Registration{}, unavailable(err)
			}
			if n >= 128 {
				return oc.Registration{}, failure(foundation.InvalidState, nil)
			}
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_outbox.subscriptions(handler_id,event_type,accepted_versions,accepted_after_sequence,registration_id) VALUES($1,$2,$3,$4,$5)`, string(def.ID), string(sub.EventType), versions, watermark, registrationID); err != nil {
				return oc.Registration{}, unavailable(err)
			}
		}
	}
	if exists {
		if _, err = x.Exec(ctx, `UPDATE agenteam_outbox.handlers SET declaration_digest=$2 WHERE id=$1`, string(def.ID), digest.String()); err != nil {
			return oc.Registration{}, unavailable(err)
		}
	}
	_, subs, err := readRegistration(ctx, x, def, registrationID)
	if err != nil {
		return oc.Registration{}, err
	}
	return oc.Registration{HandlerID: def.ID, Subscriptions: subs}, nil
}

func readRegistration(ctx context.Context, x postgres.SQLExecutor, def oc.HandlerDefinition, registrationID string) (string, []oc.SubscriptionBoundary, error) {
	var digest, effect, ordering string
	err := x.QueryRow(ctx, `SELECT declaration_digest,effect,ordering_policy FROM agenteam_outbox.handlers WHERE id=$1`, string(def.ID)).Scan(&digest, &effect, &ordering)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, err
	}
	if err != nil {
		return "", nil, unavailable(err)
	}
	if effect != string(def.Effect) || ordering != string(def.Ordering) {
		return "", nil, failure(foundation.InvalidState, nil)
	}
	rows, err := x.Query(ctx, `SELECT event_type,accepted_versions,accepted_after_sequence,registered_at,registration_id::text FROM agenteam_outbox.subscriptions WHERE handler_id=$1 ORDER BY event_type`, string(def.ID))
	if err != nil {
		return "", nil, unavailable(err)
	}
	defer rows.Close()
	var out []oc.SubscriptionBoundary
	for rows.Next() {
		var name, id string
		var versions []int64
		var boundary int64
		var at time.Time
		if err = rows.Scan(&name, &versions, &boundary, &at, &id); err != nil {
			return "", nil, unavailable(err)
		}
		b := oc.SubscriptionBoundary{EventType: event.StableName(name), AcceptedAfter: foundation.Progress(boundary), New: id == registrationID}
		b.RegisteredAt, err = foundation.NewInstant(at)
		if err != nil || b.EventType.Validate() != nil || boundary < 0 || len(versions) == 0 || len(versions) > 128 {
			return "", nil, unavailable(err)
		}
		for i, v := range versions {
			if v < 1 || v > 4294967295 || i > 0 && v <= versions[i-1] {
				return "", nil, unavailable(nil)
			}
			b.AcceptedVersions = append(b.AcceptedVersions, uint32(v))
		}
		out = append(out, b)
	}
	if err = rows.Err(); err != nil || len(out) > 128 {
		return "", nil, unavailable(err)
	}
	return digest, out, nil
}

func sameSubscriptions(wanted []oc.Subscription, saved []oc.SubscriptionBoundary) bool {
	if len(wanted) != len(saved) {
		return false
	}
	for i, s := range wanted {
		if s.EventType != saved[i].EventType || len(s.Versions) != len(saved[i].AcceptedVersions) {
			return false
		}
		for j, version := range s.Versions {
			if version != saved[i].AcceptedVersions[j] {
				return false
			}
		}
	}
	return true
}
