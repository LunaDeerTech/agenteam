package account

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type BootstrapStatus struct {
	UserID   c.UserID `json:"user_id"`
	Created  bool     `json:"created"`
	LogState string   `json:"log_state"`
}
type bootstrapFact struct{ operation, user, process, state, attempt string }

func (s *Service) bootstrapFact(ctx context.Context) (bootstrapFact, error) {
	var v bootstrapFact
	e := s.state().store.QueryRow(ctx, `SELECT operation_id::text,user_id::text,creator_process_id::text,log_state,coalesce(log_attempt_id::text,'') FROM agenteam_account.bootstrap WHERE singleton`).Scan(&v.operation, &v.user, &v.process, &v.state, &v.attempt)
	return v, e
}
func bootstrapPassword() (sc.SecretMaterial, error) {
	b := make([]byte, 18)
	defer clear(b)
	if _, e := rand.Read(b); e != nil {
		return sc.SecretMaterial{}, unavailable(e)
	}
	raw := []byte(base64.RawURLEncoding.EncodeToString(b))
	defer clear(raw)
	m, e := sc.NewSecretMaterial(raw)
	return m, portError(e)
}
func (s *Service) Bootstrap(ctx context.Context) (BootstrapStatus, error) {
	op, e := s.begin(ctx, false)
	if e != nil {
		return BootstrapStatus{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	st := s.state()
	if old, e := s.bootstrapFact(ctx); e == nil {
		uid, e := parseID[identity.User](old.user)
		return BootstrapStatus{uid, false, old.state}, e
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return BootstrapStatus{}, unavailable(e)
	}
	password, e := bootstrapPassword()
	if e != nil {
		return BootstrapStatus{}, e
	}
	defer password.Destroy()
	phc, e := st.hasher.Hash(ctx, password)
	if e != nil {
		return BootstrapStatus{}, e
	}
	uid, e := foundation.NewID[identity.User]()
	if e != nil {
		return BootstrapStatus{}, unavailable(e)
	}
	operationID, e := foundation.NewID[c.Command]()
	if e != nil {
		return BootstrapStatus{}, unavailable(e)
	}
	st.mu.Lock()
	st.bootstraps[operationID.String()] = op
	st.mu.Unlock()
	key, e := foundation.NewCommandIdentity("account.bootstrap", nil, "initialize", foundation.IdempotencyKey("initial-administrator"))
	if e != nil {
		return BootstrapStatus{}, e
	}
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return BootstrapStatus{}, e
	}
	locks := []foundation.LockRequest{commandLock(key), configLock("account-directory", foundation.Exclusive), userLock(uid.String(), foundation.Exclusive)}
	created := false
	var out BootstrapStatus
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		var user, state string
		e = x.QueryRow(ctx, `SELECT user_id::text,log_state FROM agenteam_account.bootstrap WHERE singleton`).Scan(&user, &state)
		if e == nil {
			u, e := parseID[identity.User](user)
			out = BootstrapStatus{u, false, state}
			return e
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return unavailable(e)
		}
		var conflict bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.users WHERE email='admin@mail.com' OR username='admin')`).Scan(&conflict); e != nil {
			return unavailable(e)
		}
		if conflict {
			return fault(foundation.InvalidState, nil)
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.users(id,email,username,display_name,role,password_phc,password_version,version,auth_sequence,initial_password_suggestion,theme) VALUES($1,'admin@mail.com','admin','Administrator','admin',$2,1,1,1,true,'system')`, uid.String(), phc.encoded()); e != nil {
			return unavailable(e)
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.bootstrap(singleton,operation_id,user_id,creator_process_id,log_state) VALUES(true,$1,$2,$3,'eligible')`, operationID.String(), uid.String(), st.process.String()); e != nil {
			return unavailable(e)
		}
		actor, e := serviceActor(identity.AccountBootstrap, operationID.String())
		if e != nil {
			return e
		}
		m, e := ac.AccountMetadata(ac.AccountBootstrap, ac.AccountMetadataFields{UserID: uid.String(), Version: 1, Phase: ac.AccountCreated})
		if e != nil {
			return e
		}
		resource, e := ac.NewResource(ac.UserResource, uid.String())
		if e != nil {
			return e
		}
		entry, e := ac.NewEntry(ac.EntryFields{Scope: identity.SystemScope(), Actor: actor, Action: ac.AccountBootstrap, Outcome: ac.Success, Resource: resource, Metadata: m})
		if e != nil {
			return e
		}
		appendKey, e := ac.NewAppendKey(ac.AccountProducer, operationID.String(), 0)
		if e != nil {
			return e
		}
		if _, e = st.deps.Audit.AppendInTx(ctx, tx, entry, appendKey); e != nil {
			return portError(e)
		}
		out = BootstrapStatus{uid, true, "eligible"}
		created = true
		return nil
	})
	if result.State() == foundation.Unknown {
		confirmation := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
				return unavailable(e)
			}
			x, e := st.store.InTx(tx)
			if e != nil {
				return unavailable(e)
			}
			var u, op, process, state string
			e = x.QueryRow(ctx, `SELECT user_id::text,operation_id::text,creator_process_id::text,log_state FROM agenteam_account.bootstrap WHERE singleton`).Scan(&u, &op, &process, &state)
			if e != nil {
				return fault(foundation.CommitUnknown, e)
			}
			if u != uid.String() || op != operationID.String() || process != st.process.String() {
				return fault(foundation.CommitUnknown, nil)
			}
			out = BootstrapStatus{uid, true, state}
			created = true
			return nil
		})
		if e = resultError(confirmation); e != nil {
			return BootstrapStatus{}, confirmationError(result, e)
		}
	} else if e = resultError(result); e != nil {
		return BootstrapStatus{}, e
	}
	if !created {
		return out, nil
	}
	attempt, e := foundation.NewID[c.Attempt]()
	if e != nil {
		return out, unavailable(e)
	}
	result = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_account.bootstrap SET log_state='attempted',log_attempt_id=$4 WHERE operation_id=$1 AND user_id=$2 AND creator_process_id=$3 AND log_state='eligible'`, operationID.String(), uid.String(), st.process.String(), attempt.String())
		if e != nil {
			return unavailable(e)
		}
		if tag.RowsAffected() != 1 {
			return fault(foundation.InvalidState, nil)
		}
		return nil
	})
	if result.State() == foundation.Unknown {
		confirmation := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
				return unavailable(e)
			}
			x, e := st.store.InTx(tx)
			if e != nil {
				return unavailable(e)
			}
			var valid bool
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.bootstrap WHERE operation_id=$1 AND user_id=$2 AND creator_process_id=$3 AND log_state='attempted' AND log_attempt_id=$4)`, operationID.String(), uid.String(), st.process.String(), attempt.String()).Scan(&valid)
			if e != nil || !valid {
				return fault(foundation.CommitUnknown, e)
			}
			return nil
		})
		if e = resultError(confirmation); e != nil {
			return out, confirmationError(result, e)
		}
	} else if e = resultError(result); e != nil {
		return out, e
	}
	var owned sc.SecretMaterial
	e = password.Use(func(b []byte) error { var e error; owned, e = sc.NewSecretMaterial(b); return e })
	if e != nil {
		return out, portError(e)
	}
	logOp, e := s.begin(context.Background(), false)
	if e != nil {
		owned.Destroy()
		return out, e
	}
	st.mu.Lock()
	st.bootstraps[operationID.String()] = logOp
	st.mu.Unlock()
	type logResult struct {
		state string
		err   error
	}
	done := make(chan logResult, 1)
	go func() {
		defer s.finish(logOp)
		defer owned.Destroy()
		record, e := recoverylog.NewBootstrapRecord(uid.String(), attempt.String(), owned)
		state := "failed"
		if e == nil {
			var result recoverylog.Result
			result, e = st.deps.RecoveryLog.WriteBootstrap(context.Background(), record)
			switch result.State {
			case recoverylog.Written:
				state = "written"
			case recoverylog.Unknown:
				state = "unknown"
			}
		}
		// Write's context is deliberately never cancelled. Its natural return
		// joins that exact queued/active item; caller cancellation is not join.
		st.mu.Lock()
		parent := st.force
		st.mu.Unlock()
		if parent == nil {
			parent = context.Background()
		}
		cleanup, cancel := context.WithTimeout(parent, 2*time.Second)
		defer cancel()
		r := st.store.WithinTx(cleanup, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
				return unavailable(e)
			}
			x, e := st.store.InTx(tx)
			if e != nil {
				return unavailable(e)
			}
			_, e = x.Exec(ctx, `UPDATE agenteam_account.bootstrap SET log_state=$5 WHERE operation_id=$1 AND user_id=$2 AND creator_process_id=$3 AND log_attempt_id=$4 AND log_state='attempted'`, operationID.String(), uid.String(), st.process.String(), attempt.String(), state)
			return portError(e)
		})
		if commitErr := resultError(r); commitErr != nil {
			e = commitErr
			state = "unknown"
		}
		done <- logResult{state, portError(e)}
	}()
	select {
	case log := <-done:
		out.LogState = log.state
		return out, log.err
	case <-ctx.Done():
		out.LogState = "unknown"
		return out, unavailable(ctx.Err())
	}
}
