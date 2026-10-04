package account

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

type commandMAC func(string) ([]byte, error)

func (s *Service) checkCommand(cmd commandRecord, key foundation.CommandIdentity, mac commandMAC) error {
	want, e := mac(cmd.kid)
	if e != nil {
		return e
	}
	defer clear(want)
	if cmd.identity.Canonical() != key.Canonical() || subtle.ConstantTimeCompare(want, cmd.mac) != 1 {
		return fault(foundation.IdempotencyKeyReused, nil)
	}
	return nil
}
func (s *Service) lookupMutation(ctx context.Context, key foundation.CommandIdentity, mac commandMAC, actor identity.Actor, browser c.BrowserIdentity, admin bool) (commandRecord, error) {
	if browser.Validate() == nil {
		if e := s.requireBrowser(ctx, browser); e != nil {
			return commandRecord{}, e
		}
	}
	locks := []foundation.LockRequest{commandLock(key), configLock("account-directory", foundation.Shared)}
	if actor.Validate() == nil {
		locks = append(locks, userLock(actor.Details().UserID, foundation.Shared))
	}
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return commandRecord{}, e
	}
	var out commandRecord
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if actor.Validate() == nil {
			if admin {
				if _, e := s.state().deps.Authority.AuthorizeSystem(ctx, tx, actor, identity.Mutate); e != nil {
					return e
				}
			} else if e := s.state().deps.Authority.RequireCurrentSession(ctx, tx, actor); e != nil {
				return e
			}
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		if browser.Validate() == nil {
			if e := s.validateBrowserInTx(ctx, x, browser); e != nil {
				return e
			}
		}
		out, e = loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e != nil {
			return e
		}
		return s.checkCommand(out, key, mac)
	})
	return out, resultError(result)
}
func (s *Service) mutationAudit(ctx context.Context, tx foundation.Tx, actor identity.Actor, action ac.Action, resourceKind ac.ResourceKind, resourceID, cause string, fields ac.AccountMetadataFields) error {
	m, e := ac.AccountMetadata(action, fields)
	if e != nil {
		return e
	}
	resource, e := ac.NewResource(resourceKind, resourceID)
	if e != nil {
		return e
	}
	entry, e := ac.NewEntry(ac.EntryFields{Scope: identity.SystemScope(), Actor: actor, Action: action, Outcome: ac.Success, Resource: resource, Metadata: m})
	if e != nil {
		return e
	}
	key, e := ac.NewAppendKey(ac.AccountProducer, cause, 0)
	if e != nil {
		return e
	}
	_, e = s.state().deps.Audit.AppendInTx(ctx, tx, entry, key)
	return portError(e)
}
func tokenVerifier(kind c.TokenKind, raw []byte) ([]byte, error) {
	b, e := base64.RawURLEncoding.Strict().DecodeString(string(raw))
	if e != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != string(raw) {
		return nil, fault(foundation.ResourceDeleted, nil)
	}
	h := sha256.Sum256(append([]byte("agenteam.account.link.v1\x00"+string(kind)+"\x00"), b...))
	clear(b)
	return h[:], nil
}
func passwordPair(password, confirmation sc.SecretMaterial) error {
	return portError(password.Use(func(p []byte) error {
		return confirmation.Use(func(c []byte) error {
			if subtle.ConstantTimeCompare(p, c) != 1 {
				return field("/confirmation", "MISMATCH")
			}
			return ValidatePassword(p)
		})
	}))
}
func accountConflict(e error) error {
	var p *pgconn.PgError
	if errors.As(e, &p) && p.Code == "23505" {
		switch p.ConstraintName {
		case "account_email_unique":
			return field("/email", "ALREADY_EXISTS")
		case "account_username_unique":
			return field("/username", "ALREADY_EXISTS")
		}
	}
	return portError(e)
}
func linkRef(raw string) (sc.CredentialRef, error) {
	id, e := parseID[sc.Credential](raw)
	if e != nil {
		return sc.CredentialRef{}, e
	}
	return sc.NewCredentialRef(id, identity.SystemScope())
}
func linkUsage(owner string, ref sc.CredentialRef, retain bool) (sc.UsageRequest, error) {
	a, e := serviceActor(identity.SecretService, owner)
	if e != nil {
		return sc.UsageRequest{}, e
	}
	action := sc.ReleaseReferenceUsage
	if retain {
		action = sc.RetainReferenceUsage
	}
	return sc.UsageRequest{Actor: a, Ref: ref, Purpose: sc.System, ReferenceOwner: owner, Action: action, Retain: retain}, nil
}
func completeCommand(ctx context.Context, x postgres.SQLExecutor, id string, version int64) error {
	_, e := x.Exec(ctx, `UPDATE agenteam_account.commands SET phase='committed',result_code='COMPLETED',result_version=$2,completed_at=clock_timestamp() WHERE id=$1`, id, version)
	return portError(e)
}
func (s *Service) validateBrowserInTx(ctx context.Context, x postgres.SQLExecutor, b c.BrowserIdentity) error {
	if !b.IssuedBy(s.state().browserIssuer) {
		return fault(foundation.Unauthenticated, nil)
	}
	var now time.Time
	if e := x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return unavailable(e)
	}
	if !now.Before(b.ExpiresAt().Time()) {
		return fault(foundation.Unauthenticated, nil)
	}
	return nil
}
