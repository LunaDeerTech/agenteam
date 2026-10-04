package account

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// LoginResponse owns one independent material-read attempt. The adapter must
// Close it after the synchronous cookie write, including all failed writes.
// Cancellation closes admission but never claims a running Use has joined.
type LoginResponse struct{ data func() *responseState }
type responseState struct {
	mu                      sync.Mutex
	service                 *Service
	op                      *operation
	plan                    responsePlan
	identity                foundation.CommandIdentity
	user                    c.User
	session                 c.Session
	material                sc.SecretMaterial
	uses                    int
	closing, releaseStarted bool
	done                    chan struct{}
	err                     error
	stop                    func() bool
}

func (r LoginResponse) User() c.User {
	if r.data == nil {
		return c.User{}
	}
	return r.data().user
}
func (r LoginResponse) Session() c.Session {
	if r.data == nil {
		return c.Session{}
	}
	return r.data().session
}
func (r LoginResponse) AttemptID() c.AttemptID {
	if r.data == nil {
		return c.AttemptID{}
	}
	v, _ := foundation.ParseID[c.Attempt](r.data().plan.id)
	return v
}
func (r LoginResponse) UseCookie(fn func([]byte) error) error {
	if r.data == nil || fn == nil {
		return invalid()
	}
	v := r.data()
	v.mu.Lock()
	if v.closing || v.op.ctx.Err() != nil {
		v.mu.Unlock()
		return fault(foundation.ShuttingDown, nil)
	}
	v.uses++
	v.mu.Unlock()
	defer func() { v.mu.Lock(); v.uses--; v.startReleaseLocked(); v.mu.Unlock() }()
	return portError(v.material.Use(fn))
}
func (v *responseState) startReleaseLocked() {
	if !v.closing || v.uses != 0 || v.releaseStarted {
		return
	}
	v.releaseStarted = true
	v.material.Destroy()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	cleanup, beginErr := v.service.begin(ctx, true)
	go func() {
		defer cancel()
		e := beginErr
		if e == nil {
			e = v.service.releaseResponse(cleanup.ctx, v.plan)
			v.service.finish(cleanup)
		}
		v.mu.Lock()
		v.err = e
		v.mu.Unlock()
		if v.stop != nil {
			v.stop()
		}
		if e == nil {
			st := v.service.state()
			st.mu.Lock()
			delete(st.responses, v.plan.id)
			st.mu.Unlock()
		}
		close(v.done)
		v.service.finish(v.op)
	}()
}
func (v *responseState) close() { v.mu.Lock(); v.closing = true; v.startReleaseLocked(); v.mu.Unlock() }
func (r LoginResponse) Close(ctx context.Context) error {
	if r.data == nil {
		return nil
	}
	v := r.data()
	v.close()
	select {
	case <-v.done:
		v.mu.Lock()
		defer v.mu.Unlock()
		return v.err
	case <-ctx.Done():
		return unavailable(ctx.Err())
	}
}
func (r LoginResponse) Joined() bool {
	if r.data == nil {
		return true
	}
	select {
	case <-r.data().done:
		return true
	default:
		return false
	}
}
func (r LoginResponse) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "account_login_response")
}
func (r LoginResponse) MarshalJSON() ([]byte, error) { return []byte(`"account_login_response"`), nil }
func (*LoginResponse) UnmarshalJSON([]byte) error    { return invalid() }
func (r LoginResponse) LogValue() slog.Value         { return slog.StringValue("account_login_response") }

func responseUsage(p responsePlan, action sc.UsageAction) (sc.UsageRequest, error) {
	id, e := foundation.ParseID[sc.Credential](p.ref)
	if e != nil {
		return sc.UsageRequest{}, unavailable(e)
	}
	ref, e := sc.NewCredentialRef(id, identity.SystemScope())
	if e != nil {
		return sc.UsageRequest{}, e
	}
	registration, e := identity.RegisterService(identity.SecretService)
	if e != nil {
		return sc.UsageRequest{}, e
	}
	actor, e := registration.Actor(p.id, identity.SystemScope())
	if e != nil {
		return sc.UsageRequest{}, e
	}
	owner, e := sc.NewCredentialLeaseOwner(sc.AccountResponseOwner, p.id)
	if e != nil {
		return sc.UsageRequest{}, e
	}
	lease, e := foundation.ParseID[sc.Lease](p.lease)
	if e != nil {
		return sc.UsageRequest{}, e
	}
	return sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.System, LeaseOwner: owner, LeaseID: lease, Action: action}, nil
}
func (s *Service) releaseResponse(ctx context.Context, p responsePlan) error {
	st := s.state()
	request, e := responseUsage(p, sc.ReleaseLeaseUsage)
	if e != nil {
		return e
	}
	deps, e := st.deps.Secrets.DiscoverUsage(ctx, request)
	if e != nil {
		return portError(e)
	}
	cause, e := recoveryCause("response-release")
	if e != nil {
		return e
	}
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, deps.RequiredLocks()); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadResponsePlan(ctx, x, p.id)
		if e != nil {
			return e
		}
		if responseMapping(current) != responseMapping(p) {
			return fault(foundation.ResourceBusy, nil)
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.response_plans SET joined_at=coalesce(joined_at,clock_timestamp()) WHERE id=$1 AND process_id=$2 AND lease_id=$3`, p.id, p.process, p.lease); e != nil {
			return unavailable(e)
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.auth_attempts SET phase='released',completed_at=coalesce(completed_at,clock_timestamp()) WHERE id=$1 AND kind='response_read' AND process_id=$2 AND lease_id=$3`, p.id, p.process, p.lease); e != nil {
			return unavailable(e)
		}
		_, e = st.deps.Secrets.ApplyUsageInTx(ctx, tx, request, deps)
		return portError(e)
	})
	return resultError(r)
}

func (s *Service) openLoginResponse(ctx context.Context, r c.LoginRequest, email string, cmd commandRecord) (LoginResponse, error) {
	if cmd.phase != "committed" || cmd.responseRef == "" {
		return LoginResponse{}, fault(foundation.Unauthenticated, nil)
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return LoginResponse{}, e
	}
	owned := false
	defer func() {
		if !owned {
			s.finish(op)
		}
	}()
	ctx = op.ctx
	st := s.state()
	id, e := foundation.NewID[c.Attempt]()
	if e != nil {
		return LoginResponse{}, unavailable(e)
	}
	lease, e := foundation.NewID[sc.Lease]()
	if e != nil {
		return LoginResponse{}, unavailable(e)
	}
	p := responsePlan{id: id.String(), command: cmd.id.String(), browser: cmd.browser, user: cmd.user, session: cmd.session, kid: cmd.kid, mac: append([]byte(nil), cmd.mac...), passwordVersion: cmd.passwordVersion, ref: cmd.responseRef, expires: cmd.responseExpires, process: st.process.String(), fence: 1, lease: lease.String()}
	v := &responseState{service: s, op: op, plan: p, identity: cmd.identity, done: make(chan struct{})}
	st.mu.Lock()
	st.responses[p.id] = v
	st.mu.Unlock()
	locks := append(commandLocks(cmd), recordLock(p.id))
	cause, e := foundation.NewCommandsCause(cmd.identity)
	if e != nil {
		return LoginResponse{}, e
	}
	check := func(ctx context.Context, tx foundation.Tx) error {
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadCommand(ctx, x, p.command, false)
		if e != nil {
			return e
		}
		if e = s.verifyLoginCommand(ctx, x, r, email, current); e != nil {
			return e
		}
		if current.user != p.user || current.session != p.session || current.responseRef != p.ref || current.passwordVersion != p.passwordVersion || !current.responseExpires.Equal(p.expires) || subtle.ConstantTimeCompare(current.mac, p.mac) != 1 {
			return fault(foundation.ResourceBusy, nil)
		}
		return st.deps.Authority.responseCommandCurrent(ctx, tx, current)
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if e := check(ctx, tx); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.response_plans(id,command_id,browser_id,user_id,session_id,semantic_kid,semantic_mac,password_version,credential_id,scope,purpose,consumer,expires_at,process_id,fence,lease_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'system','system','system',$10,$11,$12,$13)`, p.id, p.command, p.browser, p.user, p.session, p.kid, p.mac, p.passwordVersion, p.ref, p.expires, p.process, p.fence, p.lease)
		return portError(e)
	})
	if result.State() == foundation.Unknown {
		confirmation := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
				return unavailable(e)
			}
			if e := check(ctx, tx); e != nil {
				return e
			}
			x, e := st.store.InTx(tx)
			if e != nil {
				return unavailable(e)
			}
			actual, e := loadResponsePlan(ctx, x, p.id)
			if e != nil {
				return e
			}
			if responseMapping(actual) != responseMapping(p) {
				return fault(foundation.Forbidden, nil)
			}
			return nil
		})
		if confirmation.State() != foundation.Committed {
			// A failed confirmation, including failure to acquire the original
			// writer lock, says nothing about that writer's eventual COMMIT.
			// Retain local actual-join proof for the existing recovery scan. Its
			// serialized confirmed absence/terminal read retires this bookkeeping.
			return LoginResponse{}, confirmationError(result, resultError(confirmation))
		}
	} else if e = resultError(result); e != nil {
		st.mu.Lock()
		delete(st.responses, p.id)
		st.mu.Unlock()
		return LoginResponse{}, e
	}
	request, e := responseUsage(p, sc.AcquireLeaseUsage)
	if e != nil {
		return LoginResponse{}, e
	}
	deps, e := st.deps.Secrets.DiscoverUsage(ctx, request)
	if e != nil {
		return LoginResponse{}, portError(e)
	}
	result = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, deps.RequiredLocks()); e != nil {
			return unavailable(e)
		}
		if e := check(ctx, tx); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.auth_attempts(id,kind,command_id,user_id,outcome,phase,browser_id,session_id,password_version,semantic_kid,semantic_mac,credential_id,scope,purpose,consumer,expires_at,process_id,fence,lease_id) VALUES($1,'response_read',$2,$3,'success','active',$4,$5,$6,$7,$8,$9,'system','system','system',$10,$11,$12,$13)`, p.id, p.command, p.user, p.browser, p.session, p.passwordVersion, p.kid, p.mac, p.ref, p.expires, p.process, p.fence, p.lease)
		if e != nil {
			return unavailable(e)
		}
		_, e = st.deps.Secrets.ApplyUsageInTx(ctx, tx, request, deps)
		return portError(e)
	})
	if result.State() == foundation.Unknown {
		// The account row and lease were written in that same transaction. The
		// original writer lock serializes verification with its actual outcome.
		confirmation := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := st.store.AcquireAll(ctx, tx, deps.RequiredLocks()); e != nil {
				return unavailable(e)
			}
			if e := check(ctx, tx); e != nil {
				return e
			}
			x, e := st.store.InTx(tx)
			if e != nil {
				return unavailable(e)
			}
			var present bool
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.auth_attempts WHERE id=$1 AND kind='response_read' AND process_id=$2 AND fence=$3 AND lease_id=$4 AND phase='active')`, p.id, p.process, p.fence, p.lease).Scan(&present)
			if e != nil {
				return unavailable(e)
			}
			if !present {
				return fault(foundation.CommitUnknown, nil)
			}
			return nil
		})
		if e = resultError(confirmation); e != nil {
			return LoginResponse{}, confirmationError(result, e)
		}
	} else if e = resultError(result); e != nil {
		return LoginResponse{}, e
	}
	owned = true
	material, e := st.deps.Secrets.ReadCredentialForRequest(ctx, request.Actor, request.LeaseID)
	if e == nil {
		uid, _ := foundation.ParseID[identity.User](p.user)
		sid, _ := foundation.ParseID[identity.Session](p.session)
		actor, _ := identity.NewHuman(uid, sid)
		var current sessionRecord
		current, e = st.deps.Authority.current(ctx, foundation.Tx{}, actor)
		if e == nil {
			v.user = current.user.user
			v.session = current.session
		}
	}
	if e != nil {
		material.Destroy()
		v.close()
		return LoginResponse{}, portError(e)
	}
	v.mu.Lock()
	v.material = material
	v.stop = context.AfterFunc(op.ctx, v.close)
	v.mu.Unlock()
	if ctx.Err() != nil {
		v.close()
		return LoginResponse{}, unavailable(ctx.Err())
	}
	return LoginResponse{func() *responseState { return v }}, nil
}
