package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type command struct{}
type commandRecord struct {
	id        f.ID[command]
	project   id.ProjectID
	document  kc.DocumentID
	user      id.UserID
	name      kc.CommandName
	key       f.IdempotencyKey
	digest    f.Digest
	state     kc.LookupState
	receipt   *kc.MutationReceipt
	created   f.Instant
	committed *f.Instant
}

func scanCommand(row interface{ Scan(...any) error }) (*commandRecord, error) {
	var key, project, document, user, name, commandKey, digest, state string
	var raw []byte
	var created time.Time
	var committed *time.Time
	if err := row.Scan(&key, &project, &document, &user, &name, &commandKey, &digest, &state, &raw, &created, &committed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, unavailable(err)
	}
	out := &commandRecord{name: kc.CommandName(name), key: f.IdempotencyKey(commandKey), digest: f.Digest(digest)}
	var err error
	if out.id, err = f.ParseID[command](key); err != nil {
		return nil, internal(err)
	}
	if out.project, err = f.ParseID[id.Project](project); err != nil {
		return nil, internal(err)
	}
	if out.document, err = f.ParseID[kc.Document](document); err != nil {
		return nil, internal(err)
	}
	if out.user, err = f.ParseID[id.User](user); err != nil {
		return nil, internal(err)
	}
	if out.name.Validate() != nil || out.key.Validate() != nil || out.digest.Validate() != nil {
		return nil, internal(nil)
	}
	if out.created, err = f.NewInstant(created); err != nil {
		return nil, internal(err)
	}
	switch state {
	case "planned":
		if raw != nil || committed != nil {
			return nil, internal(nil)
		}
		out.state = kc.InProgress
	case "completed":
		if len(raw) == 0 || len(raw) > 4<<20 || committed == nil || committed.Before(created) {
			return nil, internal(nil)
		}
		var receipt kc.MutationReceipt
		if err = json.Unmarshal(raw, &receipt); err != nil {
			return nil, internal(err)
		}
		if err = receiptForCommand(receipt, out); err != nil {
			return nil, err
		}
		at, err := f.NewInstant(*committed)
		if err != nil {
			return nil, internal(err)
		}
		out.state = kc.Committed
		out.receipt = &receipt
		out.committed = &at
	default:
		return nil, internal(nil)
	}
	return out, nil
}

func receiptForCommand(receipt kc.MutationReceipt, record *commandRecord) error {
	if receipt.Validate() != nil || receipt.Command != record.name {
		return internal(nil)
	}
	if receipt.Command == kc.DeleteSubtree {
		if *receipt.RootID != record.document {
			return internal(nil)
		}
	} else if receipt.Document.ID != record.document || receipt.Document.ProjectID != record.project {
		return internal(nil)
	}
	return nil
}

func loadCommand(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, name kc.CommandName, key f.IdempotencyKey) (*commandRecord, error) {
	return scanCommand(x.QueryRow(ctx, `SELECT id::text,project_id::text,document_id::text,actor_user_id::text,
 command_name,command_key,semantic_digest,state,receipt,created_at,committed_at
 FROM agenteam_knowledge.commands WHERE project_id=$1 AND command_name=$2 AND command_key=$3`, project.String(), string(name), string(key)))
}

func insertCompletedCommand(ctx context.Context, x postgres.SQLExecutor, actor id.Actor, project id.ProjectID, document kc.DocumentID, name kc.CommandName, key f.IdempotencyKey, semantic f.Digest, receipt kc.MutationReceipt, now f.Instant) error {
	if receipt.Validate() != nil || receipt.Command != name || semantic.Validate() != nil || now.Validate() != nil {
		return internal(nil)
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return internal(err)
	}
	if len(raw) > 4<<20 {
		return fault(f.ResourceBusy)
	}
	operation, err := f.NewID[command]()
	if err != nil {
		return unavailable(err)
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_knowledge.commands
 (id,project_id,document_id,actor_user_id,command_name,command_key,semantic_digest,state,receipt,created_at,committed_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,'completed',$8,$9,$9)`, operation.String(), project.String(), document.String(), actor.Details().UserID, string(name), string(key), semantic.String(), raw, now.Time())
	return portError(err)
}

func (s *Service) LookupCommand(ctx context.Context, actor id.Actor, request kc.LookupRequest) (kc.CommandLookup, error) {
	if err := readInput(ctx, actor, request.ProjectID); err != nil {
		return kc.CommandLookup{}, err
	}
	if request.Validate() != nil {
		return kc.CommandLookup{}, fault(f.InvalidArgument)
	}
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return kc.CommandLookup{}, err
	}
	defer done()
	identity, err := kc.CommandIdentity(request.ProjectID, request.Command, request.Key)
	if err != nil {
		return kc.CommandLookup{}, portError(err)
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return kc.CommandLookup{}, portError(err)
	}
	locks, err := scopeLocks(actor, request.ProjectID, false)
	if err != nil {
		return kc.CommandLookup{}, err
	}
	commandKey, err := f.CommandLock(identity)
	if err != nil {
		return kc.CommandLookup{}, portError(err)
	}
	locks, err = ob.NormalizeLocks(append(locks, f.LockRequest{Key: commandKey, Mode: f.Exclusive}))
	if err != nil {
		return kc.CommandLookup{}, portError(err)
	}
	out := kc.CommandLookup{State: kc.NotObserved}
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.readScope(ctx, tx, actor, request.ProjectID)
		if err != nil {
			return err
		}
		row, err := loadCommand(ctx, x, request.ProjectID, request.Command, request.Key)
		if err != nil {
			return err
		}
		if row == nil {
			return nil
		}
		if row.project != request.ProjectID || row.name != request.Command || row.key != request.Key {
			return internal(nil)
		}
		if row.user.String() != actor.Details().UserID || row.digest != request.SemanticDigest {
			return fault(f.IdempotencyKeyReused)
		}
		out = kc.CommandLookup{State: row.state, Receipt: row.receipt}
		return out.Validate()
	})
	if err = txError(result); err != nil {
		return kc.CommandLookup{}, err
	}
	return out, nil
}
