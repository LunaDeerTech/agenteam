package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type commandRecord struct {
	ID            c.CommandID
	User, Session string
	Intent        c.Intent
	Semantic      f.Digest
	Receipt       c.Receipt
}

func loadCommand(ctx context.Context, x postgres.SQLExecutor, actor id.Actor, key f.IdempotencyKey, in c.Intent) (*commandRecord, error) {
	var rawID, user, session, semantic string
	var request, receipt []byte
	e := x.QueryRow(ctx, `SELECT id::text,actor_user_id::text,actor_session_id::text,semantic_digest,request,receipt FROM agenteam_runner.commands
 WHERE actor_user_id=$1 AND runner_id=$2 AND command_name=$3 AND idempotency_key=$4`, actor.Details().UserID, in.Target().String(), string(in.Command()), string(key)).Scan(&rawID, &user, &session, &semantic, &request, &receipt)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, unavailable(e)
	}
	rid, e := f.ParseID[c.Command](rawID)
	if e != nil {
		return nil, unavailable(e)
	}
	saved, e := c.DecodeIntent(in.Command(), in.Target(), request)
	if e != nil {
		return nil, unavailable(e)
	}
	var out c.Receipt
	if json.Unmarshal(receipt, &out) != nil || out.CommandID != rid || out.Command != in.Command() || out.Runner.ID != in.Target() {
		return nil, unavailable(nil)
	}
	expected, e := in.Digest(actor)
	if e != nil {
		return nil, e
	}
	actual, e := saved.Digest(actor)
	if e != nil || actual != f.Digest(semantic) {
		return nil, unavailable(e)
	}
	if expected != actual {
		return nil, fault(f.IdempotencyKeyReused)
	}
	return &commandRecord{rid, user, session, saved, actual, out}, nil
}
func (s *Service) Lookup(ctx context.Context, actor id.Actor, key f.IdempotencyKey, in c.Intent) (c.Lookup, error) {
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return c.Lookup{}, e
	}
	defer done()
	identity, e := in.Identity(actor, key)
	if e != nil {
		return c.Lookup{}, e
	}
	target := in.Target()
	held, e := locks(actor, &target, &identity, false)
	if e != nil {
		return c.Lookup{}, e
	}
	cause, _ := f.NewCommandsCause(identity)
	var out c.Lookup
	st := s.state().authority.state()
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, held); e != nil {
			return portError(e)
		}
		if e := s.state().authority.authorize(ctx, tx, actor, id.Read); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		record, e := loadCommand(ctx, x, actor, key, in)
		if e != nil {
			return e
		}
		if record != nil {
			receipt := record.Receipt.Clone()
			out.Receipt = &receipt
		}
		return nil
	})
	if e = resultError(result); e != nil {
		return c.Lookup{}, e
	}
	return out, nil
}
func (s *Service) Execute(ctx context.Context, actor id.Actor, key f.IdempotencyKey, in c.Intent) (c.Mutation, error) {
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return c.Mutation{}, e
	}
	defer done()
	identity, e := in.Identity(actor, key)
	if e != nil {
		return c.Mutation{}, e
	}
	semantic, e := in.Digest(actor)
	if e != nil {
		return c.Mutation{}, e
	}
	target := in.Target()
	held, e := locks(actor, &target, &identity, true)
	if e != nil {
		return c.Mutation{}, e
	}
	cause, _ := f.NewCommandsCause(identity)
	st := s.state().authority.state()
	var out c.Mutation
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, held); e != nil {
			return portError(e)
		}
		if e := s.state().authority.authorize(ctx, tx, actor, id.Read); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		record, e := loadCommand(ctx, x, actor, key, in)
		if e != nil {
			return e
		}
		if record != nil {
			out.Receipt = record.Receipt.Clone()
			return nil
		}
		if e = s.state().authority.authorize(ctx, tx, actor, id.Mutate); e != nil {
			return e
		}
		before, e := loadRunner(ctx, x, target)
		if in.Command() == c.Create {
			if e == nil {
				return fault(f.InvalidState)
			}
			var ff *f.Fault
			if !errors.As(e, &ff) || ff.Code != f.NotFound {
				return e
			}
		} else {
			if e != nil {
				return e
			}
			if before.Version != in.ExpectedVersion() {
				return fault(f.VersionConflict)
			}
		}
		at, e := databaseNow(ctx, x)
		if e != nil {
			return e
		}
		if in.Command() != c.Create && at.Time().Before(before.UpdatedAt.Time()) {
			at = before.UpdatedAt
		}
		cmd, e := f.NewID[c.Command]()
		if e != nil {
			return unavailable(e)
		}
		after, changed, e := applyCommand(ctx, x, in, before, at)
		if e != nil {
			return e
		}
		receipt := c.Receipt{CommandID: cmd, Command: in.Command(), Runner: after, Changed: len(changed) > 0}
		if receipt.Validate() != nil {
			return unavailable(nil)
		}
		raw, _ := in.RequestJSON()
		body, e := json.Marshal(receipt)
		if e != nil {
			return unavailable(e)
		}
		tag, e := x.Exec(ctx, `INSERT INTO agenteam_runner.commands(id,runner_id,actor_user_id,actor_session_id,command_name,idempotency_key,semantic_digest,request,receipt,created_at,committed_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10,greatest(clock_timestamp(),$10))`, cmd.String(), target.String(), actor.Details().UserID, actor.Details().SessionID, string(in.Command()), string(key), string(semantic), string(raw), string(body), at.Time())
		if e = affected(tag, e); e != nil {
			return e
		}
		record = &commandRecord{cmd, actor.Details().UserID, actor.Details().SessionID, in, semantic, receipt}
		if in.Command() == c.Create || in.Command() == c.IssueEnrollment {
			token, e := p.NewEnrollmentToken()
			if e != nil {
				return unavailable(e)
			}
			hash, e := token.Digest()
			if e != nil {
				return unavailable(e)
			}
			expires, _ := f.NewInstant(at.Time().Add(10 * time.Minute))
			tag, e := x.Exec(ctx, `INSERT INTO agenteam_runner.enrollment_tokens(token_hash,runner_id,credential_generation,issued_command_id,issued_at,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, hash[:], target.String(), int64(after.CredentialGeneration), cmd.String(), at.Time(), expires.Time())
			if e = affected(tag, e); e != nil {
				return e
			}
			if e = identityEvent(ctx, x, record, "enrollment_issued", nil, at); e != nil {
				return e
			}
			out.Material = &c.EnrollmentMaterial{Token: token, ExpiresAt: expires}
		}
		if in.Command() == c.Revoke || in.Command() == c.IssueEnrollment && before.PublicKeyFingerprint != nil {
			if e = identityEvent(ctx, x, record, "revoked", before.PublicKeyFingerprint, at); e != nil {
				return e
			}
		}
		if len(changed) > 0 {
			if e = s.appendManagement(ctx, tx, actor, key, record, before, changed); e != nil {
				return e
			}
		}
		out.Receipt = receipt.Clone()
		return nil
	})
	if e = resultError(result); e != nil {
		return c.Mutation{}, e
	}
	return out, nil
}
func databaseNow(ctx context.Context, x postgres.SQLExecutor) (f.Instant, error) {
	var t time.Time
	if e := x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&t); e != nil {
		return f.Instant{}, unavailable(e)
	}
	v, e := f.NewInstant(t)
	if e != nil {
		return v, unavailable(e)
	}
	return v, nil
}
func affected(tag pgconn.CommandTag, e error) error {
	if e != nil {
		return unavailable(e)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	return nil
}
func applyCommand(ctx context.Context, x postgres.SQLExecutor, in c.Intent, before c.Snapshot, at f.Instant) (c.Snapshot, []string, error) {
	raw, _ := in.RequestJSON()
	after := before.Clone()
	var changed []string
	if in.Command() == c.Create {
		var v c.CreateRequest
		if json.Unmarshal(raw, &v) != nil {
			return c.Snapshot{}, nil, unavailable(nil)
		}
		tags, _ := json.Marshal(v.Tags)
		tag, e := x.Exec(ctx, `INSERT INTO agenteam_runner.runners(id,name,description,tags,root_path,version,credential_generation,created_at,updated_at) VALUES($1,$2,$3,$4::jsonb,$5,1,1,$6,$6)`, v.RunnerID.String(), v.Name, v.Description, string(tags), v.RootPath, at.Time())
		if e = affected(tag, e); e != nil {
			return c.Snapshot{}, nil, e
		}
		after, e = loadRunner(ctx, x, v.RunnerID)
		return after, []string{"created"}, e
	}
	if in.Command() == c.Update {
		var v c.UpdateRequest
		if json.Unmarshal(raw, &v) != nil {
			return c.Snapshot{}, nil, unavailable(nil)
		}
		if v.Description != nil && *v.Description != before.Description {
			after.Description = *v.Description
			changed = append(changed, "description")
		}
		if v.Name != nil && *v.Name != before.Name {
			after.Name = *v.Name
			changed = append(changed, "name")
		}
		if v.Tags != nil && !slices.Equal(*v.Tags, before.Tags) {
			after.Tags = append([]string{}, (*v.Tags)...)
			changed = append(changed, "tags")
		}
		if len(changed) == 0 {
			return before.Clone(), nil, nil
		}
	} else {
		changed = []string{"credential"}
		if before.CredentialGeneration == math.MaxInt64 {
			return c.Snapshot{}, nil, fault(f.ResourceBusy)
		}
		after.CredentialGeneration++
		after.PublicKeyFingerprint = nil
		after.EnrolledAt = nil
		after.Status = c.Offline
	}
	if before.Version == math.MaxInt64 {
		return c.Snapshot{}, nil, fault(f.ResourceBusy)
	}
	after.Version++
	after.UpdatedAt = at
	var tag pgconn.CommandTag
	var e error
	if in.Command() == c.Update {
		tags, _ := json.Marshal(after.Tags)
		tag, e = x.Exec(ctx, `UPDATE agenteam_runner.runners SET name=$2,description=$3,tags=$4::jsonb,version=$5,updated_at=$6 WHERE id=$1 AND version=$7`, after.ID.String(), after.Name, after.Description, string(tags), int64(after.Version), at.Time(), int64(before.Version))
	} else {
		tag, e = x.Exec(ctx, `UPDATE agenteam_runner.runners SET device_public_key=NULL,enrolled_at=NULL,credential_generation=$2,version=$3,updated_at=$4,incompatible=false WHERE id=$1 AND version=$5`, after.ID.String(), int64(after.CredentialGeneration), int64(after.Version), at.Time(), int64(before.Version))
	}
	if e = affected(tag, e); e != nil {
		return c.Snapshot{}, nil, e
	}
	if in.Command() != c.Update {
		for _, table := range []string{"enrollment_tokens", "challenges"} {
			if _, e = x.Exec(ctx, `UPDATE agenteam_runner.`+table+` SET revoked_at=greatest($2,issued_at) WHERE runner_id=$1 AND revoked_at IS NULL`, after.ID.String(), at.Time()); e != nil {
				return c.Snapshot{}, nil, unavailable(e)
			}
		}
		if _, e = x.Exec(ctx, `DELETE FROM agenteam_runner.connections WHERE runner_id=$1`, after.ID.String()); e != nil {
			return c.Snapshot{}, nil, unavailable(e)
		}
	}
	after, e = loadRunner(ctx, x, after.ID)
	return after, changed, e
}
func identityEvent(ctx context.Context, x postgres.SQLExecutor, record *commandRecord, kind string, fingerprint *f.Digest, at f.Instant) error {
	event, e := f.NewID[c.IdentityEvent]()
	if e != nil {
		return unavailable(e)
	}
	var fp any
	if fingerprint != nil {
		fp = string(*fingerprint)
	}
	tag, e := x.Exec(ctx, `INSERT INTO agenteam_runner.identity_events(id,runner_id,kind,credential_generation,version,command_id,public_key_fingerprint,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, event.String(), record.Intent.Target().String(), kind, int64(record.Receipt.Runner.CredentialGeneration), int64(record.Receipt.Runner.Version), record.ID.String(), fp, at.Time())
	return affected(tag, e)
}
func sameJSON(a, b []byte) bool {
	left, e := cursor.CanonicalJSON(a)
	if e != nil {
		return false
	}
	right, e := cursor.CanonicalJSON(b)
	return e == nil && string(left) == string(right)
}

var _ c.Commands = (*Service)(nil)
