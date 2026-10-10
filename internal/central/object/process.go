package object

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type processClaim struct {
	Format        int    `json:"format"`
	Process       string `json:"process"`
	Deployment    string `json:"deployment"`
	SpoolIdentity string `json:"spool_identity"`
	SpoolDevice   uint64 `json:"spool_device"`
	SpoolInode    uint64 `json:"spool_inode"`
	Host          string `json:"host"`
	Boot          string `json:"boot"`
	Nonce         string `json:"nonce"`
}
type processState struct {
	mu            sync.Mutex
	spool         *Spool
	process       oc.ProcessID
	root          *os.Root
	file          *os.File
	claim         processClaim
	device, inode uint64
	service       *Service
	bound, closed bool
}

// ProcessGuard holds an exact per-instance flock. A PID, an elapsed interval
// or a storage error is never evidence that an instance has stopped.
type ProcessGuard struct{ data func() *processState }

func (g *ProcessGuard) state() *processState        { return g.data() }
func (g ProcessGuard) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "object_process_guard") }
func (g ProcessGuard) MarshalJSON() ([]byte, error) { return []byte(`"object_process_guard"`), nil }
func (*ProcessGuard) UnmarshalJSON([]byte) error    { return invalid() }
func (g ProcessGuard) LogValue() slog.Value         { return slog.StringValue("object_process_guard") }

// CurrentProcess identifies this bound, still-held guard. It does not pin the
// guard, prove any other process dead, or replace the owner's actual join. The
// composition root must retain this guard until all borrowers have joined.
func (g *ProcessGuard) CurrentProcess() (oc.ProcessID, error) {
	if g == nil || g.data == nil {
		return oc.ProcessID{}, failure(foundation.DependencyUnbound, nil)
	}
	r := g.state()
	if r == nil {
		return oc.ProcessID{}, failure(foundation.DependencyUnbound, nil)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.bound || r.closed || r.service == nil || r.process.Validate() != nil {
		return oc.ProcessID{}, unavailable(nil)
	}
	return r.process, nil
}

func OpenProcessGuard(spool *Spool, process oc.ProcessID) (*ProcessGuard, error) {
	if spool == nil || spool.data == nil || process.Validate() != nil || spool.state().process != process {
		return nil, invalid()
	}
	spool.state().mu.Lock()
	stopped := spool.state().stopped || spool.state().closed
	path := spool.state().root.Name()
	spool.state().mu.Unlock()
	if stopped {
		return nil, failure(foundation.ShuttingDown, nil)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, unavailable(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != 0700 || st.Uid != uint32(os.Geteuid()) {
		return nil, invalid()
	}
	host, boot, err := trustedHostBoot()
	if err != nil {
		return nil, err
	}
	sibling := path + ".processes"
	if err = os.Mkdir(sibling, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, unavailable(err)
	}
	real, err := filepath.EvalSymlinks(sibling)
	if err != nil || real != sibling {
		return nil, invalid()
	}
	parentInfo, err := os.Lstat(sibling)
	if err != nil {
		return nil, unavailable(err)
	}
	ps, ok := parentInfo.Sys().(*syscall.Stat_t)
	if !ok || !parentInfo.IsDir() || parentInfo.Mode().Perm() != 0700 || ps.Uid != uint32(os.Geteuid()) {
		return nil, invalid()
	}
	root, err := os.OpenRoot(sibling)
	if err != nil {
		return nil, unavailable(err)
	}
	success := false
	defer func() {
		if !success {
			_ = root.Close()
		}
	}()
	deployment, err := processDeployment(root)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(path))
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		return nil, unavailable(err)
	}
	claim := processClaim{1, process.String(), deployment, hex.EncodeToString(sum[:]), st.Dev, st.Ino, host, boot, hex.EncodeToString(nonce)}
	f, err := root.OpenFile(process.String()+".claim", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, unavailable(err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = f.Close()
		}
	}()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, failure(foundation.ResourceBusy, err)
	}
	raw, err := json.Marshal(claim)
	if err != nil {
		return nil, unavailable(err)
	}
	if _, err = f.Write(raw); err != nil {
		return nil, unavailable(err)
	}
	if err = f.Sync(); err != nil {
		return nil, unavailable(err)
	}
	if err = syncProcessDirectory(root); err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil || !ownedRegular(fi) {
		return nil, invalid()
	}
	fs := fi.Sys().(*syscall.Stat_t)
	state := &processState{spool: spool, process: process, root: root, file: f, claim: claim, device: fs.Dev, inode: fs.Ino}
	keep = true
	success = true
	return &ProcessGuard{func() *processState { return state }}, nil
}
func trustedHostBoot() (string, string, error) {
	raw, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return "", "", unavailable(err)
	}
	machine := strings.TrimSpace(string(raw))
	if len(machine) != 32 {
		return "", "", unavailable(nil)
	}
	decoded, err := hex.DecodeString(machine)
	if err != nil || bytes.Equal(decoded, make([]byte, 16)) {
		return "", "", unavailable(err)
	}
	host := sha256.Sum256(append([]byte("agenteam-object-host-v1\x00"), decoded...))
	raw, err = os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", "", unavailable(err)
	}
	boot := strings.TrimSpace(string(raw))
	if len(boot) != 36 || boot[8] != '-' || boot[13] != '-' || boot[18] != '-' || boot[23] != '-' {
		return "", "", unavailable(nil)
	}
	if _, err = hex.DecodeString(strings.ReplaceAll(boot, "-", "")); err != nil {
		return "", "", unavailable(err)
	}
	return hex.EncodeToString(host[:]), boot, nil
}
func processDeployment(root *os.Root) (string, error) {
	const name = "deployment"
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		id, e := foundation.NewID[oc.Process]()
		if e != nil {
			return "", unavailable(e)
		}
		f, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return "", unavailable(e)
		}
		_, e = f.WriteString(id.String())
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			return "", unavailable(e)
		}
		if e = syncProcessDirectory(root); e != nil {
			return "", e
		}
		return id.String(), nil
	}
	if err != nil || !ownedRegular(info) {
		return "", invalid()
	}
	f, err := root.Open(name)
	if err != nil {
		return "", unavailable(err)
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !os.SameFile(info, current) {
		return "", invalid()
	}
	raw, err := io.ReadAll(io.LimitReader(f, 37))
	if err != nil {
		return "", unavailable(err)
	}
	if _, err = foundation.ParseID[oc.Process](string(raw)); err != nil {
		return "", invalid()
	}
	return string(raw), nil
}
func syncProcessDirectory(root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return unavailable(err)
	}
	defer f.Close()
	return unavailableIf(f.Sync())
}
func (g *ProcessGuard) bind(ctx context.Context, s *Service) error {
	r := g.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.service != s || s.state().spool != r.spool || s.state().process != r.process {
		return invalid()
	}
	key, _ := foundation.SystemConfigLock("object-process-" + r.process.String())
	result := s.state().store.WithinTx(ctx, recoveryCause(), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
			return unavailable(err)
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		var storeID string
		if err = e.QueryRow(ctx, `SELECT instance_id::text FROM agenteam_object.store_identity WHERE singleton AND phase='confirmed'`).Scan(&storeID); err != nil {
			return unavailable(err)
		}
		c := r.claim
		tag, err := e.Exec(ctx, `INSERT INTO agenteam_object.process_claims(process_id,store_id,deployment_id,spool_identity,spool_device,spool_inode,claim_device,claim_inode,host_identity,boot_id,claim_nonce,state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'claimed') ON CONFLICT(process_id) DO NOTHING`, c.Process, storeID, c.Deployment, decodeHex(c.SpoolIdentity), int64(c.SpoolDevice), int64(c.SpoolInode), int64(r.device), int64(r.inode), decodeHex(c.Host), c.Boot, decodeHex(c.Nonce))
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() == 1 {
			return nil
		}
		persisted, state, dev, ino, err := readProcessClaim(ctx, e, r.process)
		if err != nil {
			return err
		}
		if persisted != c || state != "claimed" || dev != r.device || ino != r.inode {
			return unavailable(nil)
		}
		return nil
	})
	if err := commitError(result); err != nil {
		return err
	}
	r.bound = true
	return nil
}
func decodeHex(raw string) []byte { value, _ := hex.DecodeString(raw); return value }
func readProcessClaim(ctx context.Context, e postgres.SQLExecutor, id oc.ProcessID) (processClaim, string, uint64, uint64, error) {
	var c processClaim
	var state, storeID string
	var path, host, nonce []byte
	var sd, si, cd, ci int64
	err := e.QueryRow(ctx, `SELECT c.process_id::text,c.deployment_id::text,c.spool_identity,c.spool_device,c.spool_inode,c.claim_device,c.claim_inode,c.host_identity,c.boot_id,c.claim_nonce,c.state,c.store_id::text FROM agenteam_object.process_claims c JOIN agenteam_object.store_identity s ON s.singleton AND s.instance_id=c.store_id AND s.phase='confirmed' WHERE c.process_id=$1`, id.String()).Scan(&c.Process, &c.Deployment, &path, &sd, &si, &cd, &ci, &host, &c.Boot, &nonce, &state, &storeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, "", 0, 0, failure(foundation.DependencyUnavailable, nil)
	}
	if err != nil {
		return c, "", 0, 0, unavailable(err)
	}
	c.Format = 1
	c.SpoolIdentity = hex.EncodeToString(path)
	c.Host = hex.EncodeToString(host)
	c.Nonce = hex.EncodeToString(nonce)
	c.SpoolDevice = uint64(sd)
	c.SpoolInode = uint64(si)
	return c, state, uint64(cd), uint64(ci), nil
}
func (g *ProcessGuard) ConfirmStopped(ctx context.Context, id oc.ProcessID) error {
	if id.Validate() != nil {
		return invalid()
	}
	r := g.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || !r.bound || r.service == nil {
		return unavailable(nil)
	}
	if err := ctx.Err(); err != nil {
		return unavailable(err)
	}
	if id == r.process {
		return failure(foundation.ResourceBusy, nil)
	}
	c, _, dev, ino, err := readProcessClaim(ctx, r.service.state().store, id)
	if err != nil {
		return err
	}
	host, _, err := trustedHostBoot()
	if err != nil {
		return err
	}
	if c.Host != host || c.Host != r.claim.Host || c.Deployment != r.claim.Deployment || c.SpoolIdentity != r.claim.SpoolIdentity || c.SpoolDevice != r.claim.SpoolDevice || c.SpoolInode != r.claim.SpoolInode {
		return failure(foundation.DependencyUnavailable, nil)
	}
	f, err := openVerifiedProcessClaim(r.root, c, dev, ino)
	if err != nil {
		return err
	}
	defer f.Close()
	// Require the exact old flock even after a boot change. The disk claim is
	// bound to a trusted stable host; a different host never inherits evidence.
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return failure(foundation.ResourceBusy, err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return unavailableIf(ctx.Err())
}
func openVerifiedProcessClaim(root *os.Root, c processClaim, dev, ino uint64) (*os.File, error) {
	name := c.Process + ".claim"
	info, err := root.Lstat(name)
	if err != nil || !ownedRegular(info) {
		return nil, unavailable(err)
	}
	f, err := root.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return nil, unavailable(err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = f.Close()
		}
	}()
	after, err := f.Stat()
	if err != nil || !ownedRegular(after) || !os.SameFile(info, after) {
		return nil, unavailable(err)
	}
	st := after.Sys().(*syscall.Stat_t)
	if st.Dev != dev || st.Ino != ino {
		return nil, unavailable(nil)
	}
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(raw) > 4096 {
		return nil, unavailable(err)
	}
	var actual processClaim
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&actual); err != nil {
		return nil, unavailable(err)
	}
	var tail any
	if err = decoder.Decode(&tail); err != io.EOF || actual != c {
		return nil, unavailable(err)
	}
	// Our claims use a single canonical encoding, rejecting duplicate fields.
	canonical, _ := json.Marshal(c)
	if !bytes.Equal(raw, canonical) {
		return nil, unavailable(nil)
	}
	ok = true
	return f, nil
}
func (g *ProcessGuard) finish(ctx context.Context) error {
	r := g.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	if r.service != nil {
		s := r.service.state()
		s.mu.Lock()
		joined := s.drained && len(s.operations) == 0 && len(s.cleanupRequests) == 0 && !s.maintenance.Running
		s.mu.Unlock()
		if !joined {
			return failure(foundation.ResourceBusy, nil)
		}
		if r.bound {
			key, _ := foundation.SystemConfigLock("object-process-" + r.process.String())
			result := s.store.WithinTx(ctx, recoveryCause(), func(ctx context.Context, tx foundation.Tx) error {
				if err := s.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
					return unavailable(err)
				}
				e, err := executor(r.service, tx)
				if err != nil {
					return err
				}
				c, _, dev, ino, err := readProcessClaim(ctx, e, r.process)
				if err != nil {
					return err
				}
				if c != r.claim || dev != r.device || ino != r.inode {
					return unavailable(nil)
				}
				_, err = e.Exec(ctx, `UPDATE agenteam_object.process_claims SET state='stopped',stopped_at=coalesce(stopped_at,clock_timestamp()) WHERE process_id=$1`, r.process.String())
				return unavailableIf(err)
			})
			if err := commitError(result); err != nil {
				return err
			}
		}
	}
	r.closed = true
	err := r.file.Close()
	rootErr := r.root.Close()
	if err == nil {
		err = rootErr
	}
	return unavailableIf(err)
}

// Close releases an unbound constructor resource only. A bound guard belongs
// to Runtime, which must prove local joins before closing it.
func (g *ProcessGuard) Close() error {
	if g == nil || g.data == nil {
		return nil
	}
	r := g.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	if r.service != nil {
		return failure(foundation.ResourceBusy, nil)
	}
	r.closed = true
	err := r.file.Close()
	rootErr := r.root.Close()
	if err == nil {
		err = rootErr
	}
	return unavailableIf(err)
}
