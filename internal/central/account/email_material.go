package account

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func (s *Service) prepareEmailMaterial(ctx context.Context, cmd commandRecord, actor identity.Actor, value sc.SecretMaterial) (secret.PreparedServiceWrite, sc.UsageRequest, sc.UsageDependencies, error) {
	var empty secret.PreparedServiceWrite
	var noUsage sc.UsageRequest
	var noPlan sc.UsageDependencies
	st := s.state()
	key, e := foundation.NewCommandIdentity("secret.account", []string{cmd.resource}, "create", foundation.IdempotencyKey(cmd.id.String()))
	if e != nil {
		return empty, noUsage, noPlan, e
	}
	writeActor, e := serviceActor(identity.AccountAuth, cmd.id.String())
	if e != nil {
		return empty, noUsage, noPlan, e
	}
	write, e := sc.NewServiceWriteRequest(sc.ServiceWriteFields{Actor: writeActor, Scope: identity.SystemScope(), Identity: key, OwnerKind: sc.EmailCodeMaterial, OwnerID: cmd.resource, Kind: sc.Create, Purpose: sc.System, Value: value})
	if e != nil {
		return empty, noUsage, noPlan, e
	}
	binding, e := sc.ServiceWriteBinding(write)
	if e != nil {
		return empty, noUsage, noPlan, e
	}
	dg, e := canonicalCommandDigest(key)
	if e != nil {
		return empty, noUsage, noPlan, e
	}
	cause, e := foundation.NewCommandsCause(cmd.identity)
	if e != nil {
		return empty, noUsage, noPlan, e
	}
	save := func(ref string) error {
		r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := st.store.AcquireAll(ctx, tx, commandLocks(cmd)); e != nil {
				return unavailable(e)
			}
			if e := st.deps.Authority.RequireCurrentSession(ctx, tx, actor); e != nil {
				return e
			}
			x, e := st.store.InTx(tx)
			if e != nil {
				return unavailable(e)
			}
			current, e := loadCommand(ctx, x, cmd.id.String(), false)
			if e != nil {
				return e
			}
			if current.phase != "planned" || commandMapping(current) != commandMapping(cmd) {
				return fault(foundation.ResourceBusy, nil)
			}
			if ref == "" {
				_, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET secret_command_digest=$2,secret_write_binding=$3,planned_secret_ref=NULL WHERE id=$1`, cmd.id.String(), string(dg), string(binding))
			} else {
				if current.secretBinding != string(binding) {
					return fault(foundation.ResourceBusy, nil)
				}
				_, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET planned_secret_ref=$2 WHERE id=$1`, cmd.id.String(), ref)
			}
			return portError(e)
		})
		return resultError(r)
	}
	if e = save(""); e != nil {
		return empty, noUsage, noPlan, e
	}
	prepared, e := st.deps.Secrets.PrepareServiceWrite(ctx, write)
	if e != nil {
		return empty, noUsage, noPlan, portError(e)
	}
	if e = save(prepared.Ref().Details().ID.String()); e != nil {
		return empty, noUsage, noPlan, e
	}
	usage, e := linkUsage(cmd.resource, prepared.Ref(), true)
	if e != nil {
		return empty, noUsage, noPlan, e
	}
	plan, e := st.deps.Secrets.DiscoverUsage(ctx, usage)
	return prepared, usage, plan, portError(e)
}

func (s *Service) expireEmailRequest(ctx context.Context, id string) error {
	r, e := loadEmailRequest(ctx, s.state().store, id)
	if e != nil {
		return e
	}
	if r.ref == "" {
		return nil
	}
	l, e := loadEmailLink(ctx, s.state().store, "email_code", id)
	if e != nil {
		return e
	}
	if l.live {
		return fault(foundation.ResourceBusy, nil)
	}
	p, e := s.planLinkRemoval(ctx, l)
	if e != nil {
		return e
	}
	guard, e := s.state().deps.Authority.mailExclusive(ctx)
	if e != nil {
		return e
	}
	defer guard.release()
	cause, e := recoveryCause("email-expire")
	if e != nil {
		return e
	}
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, p.locks()); e != nil {
			return unavailable(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadEmailLink(ctx, x, "email_code", id)
		if e != nil {
			return e
		}
		if current.live {
			return fault(foundation.ResourceBusy, nil)
		}
		if e = s.retireEmailMaterialInTx(ctx, tx, p, guard); e != nil {
			return e
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.email_requests SET phase=CASE WHEN phase='pending' THEN 'cancelled' ELSE phase END,material_ref=NULL WHERE id=$1`, id)
		return portError(e)
	})
	return resultError(result)
}
