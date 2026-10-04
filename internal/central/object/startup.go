package object

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type storageProbe struct {
	id, process      oc.ProcessID
	key, phase, mode string
	digest           foundation.Digest
	closed           bool
}

func (s *Service) checkTransferStorage(ctx context.Context) error {
	b := s.state().transferBackend
	if b == nil {
		return failure(foundation.DependencyUnbound, nil)
	}
	if err := b.CheckBucket(ctx); err != nil {
		return err
	}
	var store string
	if err := s.state().store.QueryRow(ctx, `SELECT instance_id::text FROM agenteam_object.store_identity WHERE singleton AND phase='confirmed'`).Scan(&store); err != nil {
		return unavailable(err)
	}
	marker, err := b.readControl(ctx)
	if err != nil {
		return err
	}
	if marker != store {
		return unavailable(nil)
	}
	return nil
}

// ProbeStorage tests real PUT, full GET through both origins, and DELETE. A
// failed/unknown write never becomes permission to delete an externally late key.
func (s *Service) ProbeStorage(ctx context.Context, g *ProcessGuard) error {
	if g == nil || g.data == nil || g.state().service != s {
		return invalid()
	}
	g.state().mu.Lock()
	bound := g.state().bound
	g.state().mu.Unlock()
	if !bound {
		return unavailable(nil)
	}
	op, done, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	ctx = op.ctx
	if s.state().transferBackend == nil {
		return failure(foundation.DependencyUnbound, nil)
	}
	payload := make([]byte, 32)
	if _, err = rand.Read(payload); err != nil {
		return unavailable(err)
	}
	sum := sha256.Sum256(payload)
	id, err := foundation.NewID[oc.Process]()
	if err != nil {
		return unavailable(err)
	}
	p := storageProbe{id: id, process: s.state().process, key: "control/probe/" + id.String(), phase: "reserved", mode: "zero_marker", digest: foundation.Digest("sha256:" + hex.EncodeToString(sum[:]))}
	result := s.probeTx(ctx, p.id, func(ctx context.Context, tx foundation.Tx) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		_, err = e.Exec(ctx, `INSERT INTO agenteam_object.startup_probes(id,process_id,storage_key,byte_size,sha256,phase) VALUES($1,$2,$3,32,$4,'reserved')`, p.id.String(), p.process.String(), p.key, digestBytes(p.digest))
		return unavailableIf(err)
	})
	if err = commitError(result); err != nil {
		return err
	}
	if err = s.state().backend.put(ctx, p.key, "application/octet-stream", 32, p.digest, bytes.NewReader(payload), true); err != nil {
		return err
	}
	if err = s.state().backend.verify(ctx, p.key, 32, p.digest); err != nil {
		return err
	}
	if err = s.state().transferBackend.verify(ctx, p.key, 32, p.digest); err != nil {
		return err
	}
	result = s.probeTx(ctx, p.id, func(ctx context.Context, tx foundation.Tx) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_object.startup_probes SET phase='verified',cleanup_mode='delete',io_closed=true WHERE id=$1 AND phase='reserved'`, p.id.String())
		return unavailableIf(err)
	})
	if err = commitError(result); err != nil {
		return err
	}
	p.phase = "verified"
	p.mode = "delete"
	p.closed = true
	return s.cleanProbe(ctx, p)
}
func (s *Service) probeTx(ctx context.Context, id oc.ProcessID, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	key, _ := foundation.SystemConfigLock("object-probe-" + id.String())
	return s.state().store.WithinTx(ctx, recoveryCause(), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
			return unavailable(err)
		}
		return fn(ctx, tx)
	})
}
func (s *Service) cleanProbe(ctx context.Context, p storageProbe) error {
	// The caller has either joined this probe or proved its exact old process
	// dead. No probe URL was issued; delete is legal only for confirmed writes.
	var fence int64
	result := s.probeTx(ctx, p.id, func(ctx context.Context, tx foundation.Tx) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		return unavailableIf(e.QueryRow(ctx, `UPDATE agenteam_object.startup_probes SET phase='cleaning',fence=fence+1,recovery_pass=recovery_pass+1 WHERE id=$1 AND phase<>'complete' RETURNING fence,cleanup_mode,io_closed`, p.id.String()).Scan(&fence, &p.mode, &p.closed))
	})
	if err := commitError(result); err != nil {
		return err
	}
	if p.mode == "delete" && p.closed {
		if err := s.state().backend.remove(ctx, p.key); err != nil {
			return err
		}
		absent, err := s.state().transferBackend.absent(ctx, p.key)
		if err != nil {
			return err
		}
		if !absent {
			return unavailable(nil)
		}
	} else {
		if err := s.state().backend.zero(ctx, p.key); err != nil {
			return err
		}
		empty := sha256.Sum256(nil)
		if err := s.state().transferBackend.verify(ctx, p.key, 0, foundation.Digest("sha256:"+hex.EncodeToString(empty[:]))); err != nil {
			return err
		}
	}
	result = s.probeTx(ctx, p.id, func(ctx context.Context, tx foundation.Tx) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		tag, err := e.Exec(ctx, `UPDATE agenteam_object.startup_probes SET phase='complete',io_closed=true WHERE id=$1 AND phase='cleaning' AND fence=$2`, p.id.String(), fence)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return failure(foundation.ResourceBusy, nil)
		}
		return nil
	})
	return commitError(result)
}
func (s *Service) recoverProbes(ctx context.Context, g *ProcessGuard) error {
	return s.recoverProbeProgress(ctx, g, &recoveryFailures{})
}
func (s *Service) recoverProbeProgress(ctx context.Context, g *ProcessGuard, failures *recoveryFailures) (out error) {
	defer func() { failures.remember(out) }()
	rows, err := s.state().store.Query(ctx, `SELECT id::text,process_id::text,storage_key,phase,cleanup_mode,sha256,io_closed FROM agenteam_object.startup_probes WHERE phase<>'complete' ORDER BY recovery_pass,id LIMIT 100`)
	if err != nil {
		return unavailable(err)
	}
	var all []storageProbe
	for rows.Next() {
		var p storageProbe
		var id, process string
		var sha []byte
		if err = rows.Scan(&id, &process, &p.key, &p.phase, &p.mode, &sha, &p.closed); err != nil {
			rows.Close()
			return unavailable(err)
		}
		p.id, err = foundation.ParseID[oc.Process](id)
		if err != nil {
			rows.Close()
			return unavailable(err)
		}
		p.process, err = foundation.ParseID[oc.Process](process)
		if err != nil {
			rows.Close()
			return unavailable(err)
		}
		p.digest = foundation.Digest("sha256:" + hex.EncodeToString(sha))
		all = append(all, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return unavailable(err)
	}
	for _, p := range all {
		if err = ctx.Err(); err != nil {
			return failures.remember(unavailable(err))
		}
		// Even io_closed records retain an exact process claim. For a foreign
		// process we never infer death from a DB checkpoint's age.
		if p.process != s.state().process || !p.closed {
			if err = g.ConfirmStopped(ctx, p.process); err != nil {
				failures.remember(err)
				continue
			}
		}
		failures.remember(s.cleanProbe(ctx, p))
	}
	return failures.first
}
func (s *Service) recoverEmpty(ctx context.Context, g *ProcessGuard) error {
	var business bool
	err := s.state().store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.objects) OR EXISTS(SELECT 1 FROM agenteam_object.uploads) OR EXISTS(SELECT 1 FROM agenteam_object.upload_attempts) OR EXISTS(SELECT 1 FROM agenteam_object.object_references) OR EXISTS(SELECT 1 FROM agenteam_object.object_leases) OR EXISTS(SELECT 1 FROM agenteam_object.object_transfers) OR EXISTS(SELECT 1 FROM agenteam_download.grants) OR EXISTS(SELECT 1 FROM agenteam_download.attempts)`).Scan(&business)
	if err != nil {
		return unavailable(err)
	}
	if business {
		return failure(foundation.DependencyUnbound, nil)
	}
	if err = s.state().spool.RecoverOrphans(ctx, g); err != nil {
		return err
	}
	s.state().spool.state().mu.Lock()
	pending := len(s.state().spool.state().files)
	s.state().spool.state().mu.Unlock()
	if pending != 0 {
		return failure(foundation.DependencyUnbound, nil)
	}
	return s.recoverProbes(ctx, g)
}
