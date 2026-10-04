package object

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

const spoolReserve = 128 << 20

type spoolManifest struct {
	Format    int          `json:"format"`
	ProcessID oc.ProcessID `json:"process_id"`
	PayloadID oc.PayloadID `json:"payload_id"`
	Length    int64        `json:"length"`
	SHA256    string       `json:"sha256"`
	Sealed    bool         `json:"sealed"`
}
type spoolFile struct {
	manifest spoolManifest
	media    string
	busy     bool
}
type spoolState struct {
	mu        sync.Mutex
	root      *os.Root
	lock      *os.File
	process   oc.ProcessID
	files     map[oc.PayloadID]*spoolFile
	reserved  int64
	preparing int
	stopped   bool
	closed    bool
}
type Spool struct{ data func() *spoolState }

// OpenSpool never adopts a permissive, symlinked or differently-owned directory.
// It holds an OS lock until every current preparation/upload has been released.
// Orphans remain private and untouched until exact former-process death is
// confirmed through RecoverOrphans; an unlocked directory alone is not enough.
func OpenSpool(path string, process oc.ProcessID) (*Spool, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || process.Validate() != nil {
		return nil, invalid()
	}
	actual, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		parent := filepath.Dir(path)
		resolved, e := filepath.EvalSymlinks(parent)
		if e != nil || resolved != parent {
			return nil, invalid()
		}
		if err = os.Mkdir(path, 0700); err != nil {
			return nil, unavailable(err)
		}
		actual = path
	} else if err != nil {
		return nil, unavailable(err)
	}
	if actual != path {
		return nil, invalid()
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, unavailable(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0700 || !ok || stat.Uid != uint32(os.Geteuid()) {
		return nil, invalid()
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, unavailable(err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = root.Close()
		}
	}()
	// NOFOLLOW also protects an existing lock file planted by the same local
	// account; the root handle confines later payload operations.
	fd, err := syscall.Open(filepath.Join(path, ".object.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, unavailable(err)
	}
	lock := os.NewFile(uintptr(fd), "object-spool-lock")
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, failure(foundation.ResourceBusy, err)
	}
	lockInfo, err := lock.Stat()
	lockStat, lockOK := func() (*syscall.Stat_t, bool) {
		if err != nil {
			return nil, false
		}
		v, ok := lockInfo.Sys().(*syscall.Stat_t)
		return v, ok
	}()
	if err != nil || !lockInfo.Mode().IsRegular() || lockInfo.Mode().Perm() != 0600 || !lockOK || lockStat.Uid != uint32(os.Geteuid()) || lockStat.Nlink != 1 {
		_ = lock.Close()
		return nil, invalid()
	}
	state := &spoolState{root: root, lock: lock, process: process, files: map[oc.PayloadID]*spoolFile{}}
	s := &Spool{func() *spoolState { return state }}
	if err = s.loadManifests(); err != nil {
		_ = lock.Close()
		return nil, err
	}
	closed = true
	return s, nil
}
func (s *Spool) state() *spoolState          { return s.data() }
func (s Spool) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "object_spool") }
func (s Spool) MarshalJSON() ([]byte, error) { return []byte(`"object_spool"`), nil }
func (*Spool) UnmarshalJSON([]byte) error    { return invalid() }
func (s Spool) LogValue() slog.Value         { return slog.StringValue("object_spool") }

type spoolClaim struct {
	process oc.ProcessID
	initial bool
}

func claimName(id oc.PayloadID, process oc.ProcessID, initial bool) string {
	suffix := ".owner"
	if initial {
		suffix = ".init"
	}
	return id.String() + "." + process.String() + suffix
}
func (s *Spool) loadManifests() error {
	r := s.state()
	dir, err := r.root.Open(".")
	if err != nil {
		return unavailable(err)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(1025)
	if err != nil && err != io.EOF {
		return unavailable(err)
	}
	if len(entries) > 1024 {
		return failure(foundation.ResourceBusy, nil)
	}
	claims := map[oc.PayloadID]spoolClaim{}
	names := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		names[name] = true
		if name == ".object.lock" {
			continue
		}
		info, e := r.root.Lstat(name)
		if e != nil || !ownedRegular(info) {
			return invalid()
		}
		initial := strings.HasSuffix(name, ".init")
		if initial || strings.HasSuffix(name, ".owner") {
			suffix := ".owner"
			if initial {
				suffix = ".init"
			}
			parts := strings.Split(strings.TrimSuffix(name, suffix), ".")
			if len(parts) != 2 || info.Size() != 0 {
				return invalid()
			}
			id, e := foundation.ParseID[oc.Payload](parts[0])
			if e != nil {
				return invalid()
			}
			process, e := foundation.ParseID[oc.Process](parts[1])
			if e != nil {
				return invalid()
			}
			if _, duplicate := claims[id]; duplicate {
				return invalid()
			}
			claims[id] = spoolClaim{process, initial}
		} else if strings.HasSuffix(name, ".payload") {
			if info.Size() > oc.MaxObjectSize {
				return invalid()
			}
		} else if strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".json.next") {
			if info.Size() > 2048 {
				return invalid()
			}
		} else {
			return invalid()
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id, e := foundation.ParseID[oc.Payload](strings.TrimSuffix(name, ".json"))
		if e != nil {
			return invalid()
		}
		claim, claimed := claims[id]
		f, e := r.root.Open(name)
		if e != nil {
			return unavailable(e)
		}
		raw, e := io.ReadAll(io.LimitReader(f, 2049))
		_ = f.Close()
		if e != nil {
			return unavailable(e)
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		var m spoolManifest
		decodeErr := decoder.Decode(&m)
		if decodeErr == nil && decoder.Decode(new(any)) != io.EOF {
			return invalid()
		}
		if decodeErr != nil {
			// A persisted initial claim precedes the first JSON write. Only an exact
			// prefix of our initial serialization, before any payload/next file could
			// exist, proves this is that creation window rather than arbitrary damage.
			if !claimed || !claim.initial || names[id.String()+".payload"] || names[id.String()+".json.next"] ||
				!(errors.Is(decodeErr, io.EOF) || errors.Is(decodeErr, io.ErrUnexpectedEOF)) || !initialManifestPrefix(raw, id, claim.process) {
				return invalid()
			}
			m = spoolManifest{Format: 1, PayloadID: id, ProcessID: claim.process}
		} else {
			if m.Format != 1 || m.PayloadID != id || m.ProcessID.Validate() != nil || m.Length < 0 || m.Length > oc.MaxObjectSize || m.Sealed && foundation.Digest(m.SHA256).Validate() != nil {
				return invalid()
			}
			if claimed && (claim.process != m.ProcessID || claim.initial && (m.Sealed || names[id.String()+".payload"] || names[id.String()+".json.next"])) {
				return invalid()
			}
		}
		info, e := r.root.Lstat(id.String() + ".payload")
		// Deletion can leave a sealed manifest with no body. The retained identity
		// still requires exact-death/DB authority before any residue is removed.
		if !errors.Is(e, os.ErrNotExist) && (e != nil || !ownedRegular(info) || m.Sealed && info.Size() != m.Length) {
			return invalid()
		}
		r.files[id] = &spoolFile{manifest: m}
		r.reserved += m.Length
	}
	for id, claim := range claims {
		if r.files[id] != nil {
			continue
		}
		// Claim-only is normal before JSON creation or after the final JSON unlink.
		// It is not evidence for adopting an otherwise unidentified payload.
		if names[id.String()+".payload"] || names[id.String()+".json.next"] {
			return invalid()
		}
		r.files[id] = &spoolFile{manifest: spoolManifest{Format: 1, PayloadID: id, ProcessID: claim.process}}
	}
	for _, entry := range entries {
		name := entry.Name()
		suffix := ""
		if strings.HasSuffix(name, ".payload") {
			suffix = ".payload"
		}
		if strings.HasSuffix(name, ".json.next") {
			suffix = ".json.next"
		}
		if suffix != "" {
			id, e := foundation.ParseID[oc.Payload](strings.TrimSuffix(name, suffix))
			if e != nil || r.files[id] == nil {
				return invalid()
			}
		}
	}
	return nil
}
func initialManifestPrefix(raw []byte, id oc.PayloadID, process oc.ProcessID) bool {
	prefix := `{"format":1,"process_id":"` + process.String() + `","payload_id":"` + id.String() + `","length":`
	value := string(raw)
	if strings.HasPrefix(prefix, value) {
		return true
	}
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	rest := value[len(prefix):]
	n := 0
	for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
		n++
	}
	if n == 0 {
		return false
	}
	length, err := strconv.ParseInt(rest[:n], 10, 64)
	if err != nil || length > oc.MaxObjectSize || strconv.FormatInt(length, 10) != rest[:n] {
		return false
	}
	return strings.HasPrefix(`,"sha256":"","sealed":false}`, rest[n:])
}
func (s *Spool) syncDirectory() error {
	dir, err := s.state().root.Open(".")
	if err != nil {
		return unavailable(err)
	}
	defer dir.Close()
	return unavailableIf(dir.Sync())
}
func (s *Spool) initialClaim(m spoolManifest) error {
	f, err := s.state().root.OpenFile(claimName(m.PayloadID, m.ProcessID, true), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return unavailable(err)
	}
	err = f.Sync()
	closed := f.Close()
	if err != nil {
		return unavailable(err)
	}
	if closed != nil {
		return unavailable(closed)
	}
	return s.syncDirectory()
}
func ownedRegular(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Geteuid()) && st.Nlink == 1
}
func (s *Spool) writeManifest(m spoolManifest) error {
	if !m.Sealed {
		if err := s.initialClaim(m); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return unavailable(err)
	}
	r := s.state()
	name := m.PayloadID.String() + ".json"
	writeName := name
	if m.Sealed {
		writeName += ".next"
	}
	f, err := r.root.OpenFile(writeName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return unavailable(err)
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return unavailable(err)
	}
	if m.Sealed {
		if err = r.root.Rename(writeName, name); err != nil {
			return unavailable(err)
		}
	}
	dir, err := r.root.Open(".")
	if err != nil {
		return unavailable(err)
	}
	err = dir.Sync()
	_ = dir.Close()
	if err != nil {
		return unavailable(err)
	}
	if !m.Sealed {
		if err = r.root.Rename(claimName(m.PayloadID, m.ProcessID, true), claimName(m.PayloadID, m.ProcessID, false)); err != nil {
			return unavailable(err)
		}
		return s.syncDirectory()
	}
	return nil
}
func (s *Spool) Prepare(ctx context.Context, media string, length int64, expected *foundation.Digest, source io.ReadCloser) (out oc.PreparedPayload, err error) {
	if nilPort(source) {
		return out, invalid()
	}
	defer source.Close()
	media, err = oc.NormalizeMediaType(media)
	if err != nil || length < 0 || expected != nil && expected.Validate() != nil {
		return out, invalid()
	}
	if length > oc.MaxObjectSize {
		return out, failure(foundation.PayloadTooLarge, nil)
	}
	if err = ctx.Err(); err != nil {
		return out, unavailable(err)
	}
	r := s.state()
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return out, failure(foundation.ShuttingDown, nil)
	}
	var stat syscall.Statfs_t
	if err = syscall.Fstatfs(int(r.lock.Fd()), &stat); err != nil {
		r.mu.Unlock()
		return out, unavailable(err)
	}
	free := uint64(stat.Bavail) * uint64(stat.Bsize)
	if len(r.files)+r.preparing >= 2 || r.reserved > 2*oc.MaxObjectSize-length || free < uint64(spoolReserve)+uint64(r.reserved)+uint64(length) {
		r.mu.Unlock()
		return out, failure(foundation.RateLimited, nil)
	}
	r.preparing++
	r.reserved += length
	r.mu.Unlock()
	id, idErr := foundation.NewID[oc.Payload]()
	if idErr != nil {
		r.mu.Lock()
		r.preparing--
		r.reserved -= length
		r.mu.Unlock()
		return out, unavailable(idErr)
	}
	m := spoolManifest{Format: 1, ProcessID: r.process, PayloadID: id, Length: length}
	keep := false
	defer func() {
		r.mu.Lock()
		r.preparing--
		if !keep {
			r.reserved -= length
		}
		r.mu.Unlock()
		if !keep {
			_ = s.removeFiles(id, m.ProcessID)
		}
	}()
	// Manifest precedes the file so a crash never leaves an unidentifiable body.
	if err = s.writeManifest(m); err != nil {
		return out, err
	}
	f, err := r.root.OpenFile(id.String()+".payload", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return out, unavailable(err)
	}
	op, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	closed := make(chan struct{})
	stop := context.AfterFunc(op, func() { _ = source.Close(); _ = f.Close(); close(closed) })
	defer func() {
		if !stop() {
			<-closed
		}
		_ = f.Close()
	}()
	h := sha256.New()
	buf := make([]byte, oc.StreamBufferSize)
	remaining := length
	for remaining > 0 {
		n, re := source.Read(buf[:min(int64(len(buf)), remaining)])
		if n > 0 {
			if _, we := f.Write(buf[:n]); we != nil {
				return out, unavailable(we)
			}
			_, _ = h.Write(buf[:n])
			remaining -= int64(n)
		}
		if e := op.Err(); e != nil {
			return out, unavailable(e)
		}
		if re != nil {
			if re == io.EOF && remaining == 0 {
				break
			}
			return out, invalid()
		}
		if n == 0 {
			select {
			case <-op.Done():
				return out, unavailable(op.Err())
			default:
			}
			return out, invalid()
		}
	}
	var extra [1]byte
	n, re := source.Read(extra[:])
	if e := op.Err(); e != nil {
		return out, unavailable(e)
	}
	if n != 0 || re != io.EOF {
		return out, invalid()
	}
	m.SHA256 = "sha256:" + hex.EncodeToString(h.Sum(nil))
	if expected != nil && expected.String() != m.SHA256 {
		return out, invalid()
	}
	if err = f.Sync(); err != nil {
		return out, unavailable(err)
	}
	if err = f.Close(); err != nil {
		return out, unavailable(err)
	}
	m.Sealed = true
	if err = s.writeManifest(m); err != nil {
		return out, err
	}
	if e := op.Err(); e != nil {
		return out, unavailable(e)
	}
	out, err = oc.NewPreparedPayload(oc.PreparedDetails{ID: id, MediaType: media, Length: length, SHA256: foundation.Digest(m.SHA256)})
	if err != nil {
		return out, invalid()
	}
	r.mu.Lock()
	r.files[id] = &spoolFile{manifest: m, media: media}
	r.mu.Unlock()
	keep = true
	return out, nil
}
func (s *Spool) open(p oc.PreparedPayload) (io.ReadCloser, error) {
	if p.Validate() != nil {
		return nil, invalid()
	}
	d := p.Details()
	r := s.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.files[d.ID]
	if f == nil || f.busy || !f.manifest.Sealed || f.manifest.ProcessID != r.process || f.manifest.Length != d.Length || f.manifest.SHA256 != d.SHA256.String() || f.media != d.MediaType {
		return nil, invalid()
	}
	name := d.ID.String() + ".payload"
	info, e := r.root.Lstat(name)
	if e != nil || !ownedRegular(info) || info.Size() != d.Length {
		return nil, unavailable(e)
	}
	file, e := r.root.Open(name)
	if e != nil {
		return nil, unavailable(e)
	}
	f.busy = true
	return &spoolReader{ReadCloser: file, release: func() { r.mu.Lock(); f.busy = false; r.mu.Unlock() }}, nil
}

type spoolReader struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (r *spoolReader) Close() error { err := r.ReadCloser.Close(); r.once.Do(r.release); return err }
func (s *Spool) Discard(p oc.PreparedPayload) error {
	if p.Validate() != nil {
		return invalid()
	}
	r := s.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	id := p.Details().ID
	f := r.files[id]
	if f == nil {
		return nil
	}
	if f.busy {
		return failure(foundation.ResourceBusy, nil)
	}
	if err := s.removeFiles(id, f.manifest.ProcessID); err != nil {
		return err
	}
	r.reserved -= f.manifest.Length
	delete(r.files, id)
	return nil
}

// removeFiles leaves the durable identity manifest until every dependent file
// is gone. Directory sync makes that ordering survive a storage restart too.
func (s *Spool) removeFiles(id oc.PayloadID, process oc.ProcessID) error {
	r := s.state()
	for _, suffix := range []string{".json.next", ".payload"} {
		if err := r.root.Remove(id.String() + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return unavailable(err)
		}
	}
	dir, err := r.root.Open(".")
	if err != nil {
		return unavailable(err)
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return unavailable(err)
	}
	if err = r.root.Remove(id.String() + ".json"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return unavailable(err)
	}
	if err = dir.Sync(); err != nil {
		return unavailable(err)
	}
	for _, initial := range []bool{true, false} {
		if err = r.root.Remove(claimName(id, process, initial)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return unavailable(err)
		}
	}
	return unavailableIf(dir.Sync())
}

func (s *Spool) RecoverOrphans(ctx context.Context, authority oc.ProcessAuthority) error {
	r := s.state()
	r.mu.Lock()
	var orphans []spoolManifest
	for _, f := range r.files {
		if f.manifest.ProcessID != r.process {
			orphans = append(orphans, f.manifest)
		}
	}
	r.mu.Unlock()
	if len(orphans) > 0 && nilPort(authority) {
		return failure(foundation.DependencyUnbound, nil)
	}
	var deferred error
	for _, m := range orphans {
		if err := ctx.Err(); err != nil {
			return unavailable(err)
		}
		if err := authority.ConfirmStopped(ctx, m.ProcessID); err != nil {
			if deferred == nil {
				deferred = portError(err)
			}
			continue
		}
		r.mu.Lock()
		f := r.files[m.PayloadID]
		if f != nil && !f.busy {
			if e := s.removeFiles(m.PayloadID, m.ProcessID); e != nil {
				r.mu.Unlock()
				return e
			}
			r.reserved -= m.Length
			delete(r.files, m.PayloadID)
		}
		r.mu.Unlock()
	}
	return deferred
}
func (s *Spool) Close() error {
	r := s.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
	if r.closed {
		return nil
	}
	if r.preparing != 0 {
		return failure(foundation.ResourceBusy, nil)
	}
	for _, f := range r.files {
		if f.busy {
			return failure(foundation.ResourceBusy, nil)
		}
	}
	// Sealed files may be retained for recovery; no live I/O remains at this
	// point, and closing does not pretend their owning operation committed.
	err := r.lock.Close()
	other := r.root.Close()
	r.closed = true
	if err != nil {
		return unavailable(err)
	}
	if other != nil {
		return unavailable(other)
	}
	return nil
}
