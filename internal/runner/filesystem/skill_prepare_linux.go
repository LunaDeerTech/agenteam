//go:build linux

package filesystem

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
)

type skillCreatedEntry struct {
	path      string
	stat      unix.Stat_t
	directory bool
}

func skillSameInode(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode&unix.S_IFMT == b.Mode&unix.S_IFMT
}
func (r *linuxRoot) skillOwned(fd int) (unix.Stat_t, error) {
	var st unix.Stat_t
	if err := r.calls.stat(fd, &st); err != nil {
		return st, nativeError(err)
	}
	if st.Uid != uint32(os.Geteuid()) || st.Mode&0022 != 0 {
		return st, ErrConflict
	}
	return st, nil
}
func (r *linuxRoot) skillDirectory(parent int, name string, create bool) (fd int, st unix.Stat_t, err error) {
	fd, err = r.writeOpen(parent, name, unix.O_RDONLY|unix.O_DIRECTORY)
	if errors.Is(err, ErrNotFound) && create {
		if e := unix.Mkdirat(parent, name, 0700); e != nil && !errors.Is(e, unix.EEXIST) {
			return -1, st, nativeError(e)
		}
		fd, err = r.writeOpen(parent, name, unix.O_RDONLY|unix.O_DIRECTORY)
	}
	if err != nil {
		return -1, st, err
	}
	st, err = r.skillOwned(fd)
	if err != nil {
		_ = r.calls.close(fd)
		return -1, st, err
	}
	return fd, st, nil
}
func (r *linuxRoot) skillCreateFile(ctx context.Context, root int, name string, data []byte) (entry skillCreatedEntry, err error) {
	entry.path = name
	fd, e := r.calls.open(root, name, &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Mode: 0600, Resolve: writeResolution})
	if e != nil {
		return entry, nativeError(e)
	}
	defer func() {
		if r.calls.close(fd) != nil {
			err = errors.Join(err, ErrUnavailable)
		}
	}()
	if e = r.calls.stat(fd, &entry.stat); e != nil {
		return entry, nativeError(e)
	}
	for done := 0; done < len(data); {
		if e = contextError(ctx); e != nil {
			return entry, e
		}
		n, e := unix.Write(fd, data[done:min(done+64<<10, len(data))])
		if errors.Is(e, unix.EINTR) {
			continue
		}
		if e != nil {
			return entry, nativeError(e)
		}
		if n <= 0 {
			return entry, ErrUnavailable
		}
		done += n
	}
	if e = unix.Fchmod(fd, 0444); e != nil {
		return entry, nativeError(e)
	}
	if e = unix.Fsync(fd); e != nil {
		return entry, nativeError(e)
	}
	return entry, nil
}

// Cleanup touches only inodes created by this call under its random temporary
// directory. Concurrent replacement or foreign contents fail closed.
func (r *linuxRoot) skillRemoveCreated(root int, rootStat unix.Stat_t, entries []skillCreatedEntry) error {
	var result error
	byPath := map[string]unix.Stat_t{".": rootStat}
	for _, entry := range entries {
		if entry.directory {
			byPath[entry.path] = entry.stat
		}
	}
	for n := len(entries) - 1; n >= 0; n-- {
		entry := entries[n]
		parentPath, name := path.Split(entry.path)
		parentPath = strings.TrimSuffix(parentPath, "/")
		if parentPath == "" {
			parentPath = "."
		}
		parent, e := r.writeOpen(root, parentPath, unix.O_RDONLY|unix.O_DIRECTORY)
		if e != nil {
			result = errors.Join(result, e)
			continue
		}
		var current, actual unix.Stat_t
		e = r.calls.stat(parent, &current)
		if e == nil && !skillSameInode(current, byPath[parentPath]) {
			e = ErrConflict
		}
		if e == nil {
			e = unix.Fstatat(parent, name, &actual, unix.AT_SYMLINK_NOFOLLOW)
		}
		if e == nil && !skillSameInode(actual, entry.stat) {
			e = ErrConflict
		}
		if e == nil {
			flags := 0
			if entry.directory {
				flags = unix.AT_REMOVEDIR
			}
			e = unix.Unlinkat(parent, name, flags)
		}
		if r.calls.close(parent) != nil {
			e = errors.Join(e, ErrUnavailable)
		}
		if e != nil {
			result = errors.Join(result, ErrUnavailable)
		}
	}
	return result
}
func (r *linuxRoot) skillReadFile(ctx context.Context, root int, name string, expected []byte) (err error) {
	fd, err := r.writeOpen(root, name, unix.O_RDONLY)
	if err != nil {
		return err
	}
	defer func() {
		if r.calls.close(fd) != nil {
			err = errors.Join(err, ErrUnavailable)
		}
	}()
	st, err := r.skillOwned(fd)
	if err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || st.Size != int64(len(expected)) || st.Mode&0222 != 0 {
		return ErrConflict
	}
	buf := make([]byte, 64<<10)
	defer clear(buf)
	for done := 0; done < len(expected); {
		if err = contextError(ctx); err != nil {
			return err
		}
		n, e := r.calls.pread(fd, buf[:min(len(buf), len(expected)-done)], int64(done))
		if errors.Is(e, unix.EINTR) {
			continue
		}
		if e != nil {
			return nativeError(e)
		}
		if n <= 0 || !bytes.Equal(buf[:n], expected[done:done+n]) {
			return ErrConflict
		}
		done += n
	}
	return nil
}
func (r *linuxRoot) skillCheckContents(ctx context.Context, root int, files map[string][]byte, dirs map[string]bool) error {
	// Each directory is opened under the pinned root with no links or mounts.
	names := make([]string, 0, len(dirs))
	for name := range dirs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := contextError(ctx); err != nil {
			return err
		}
		fd, _, err := r.skillDirectory(root, name, false)
		if err != nil {
			return err
		}
		file := os.NewFile(uintptr(fd), "skill-directory")
		entries, readErr := file.ReadDir(maxSkillFiles*2 + 3)
		closeErr := file.Close()
		if readErr != nil && readErr != io.EOF || closeErr != nil {
			return ErrUnavailable
		}
		if len(entries) > maxSkillFiles*2+1 {
			return ErrConflict
		}
		for _, entry := range entries {
			relative := entry.Name()
			if name != "." {
				relative = name + "/" + relative
			}
			if _, ok := files[relative]; ok {
				if entry.Type()&os.ModeType != 0 {
					return ErrConflict
				}
			} else if !dirs[relative] || !entry.IsDir() {
				return ErrConflict
			}
		}
	}
	for name, data := range files {
		if err := r.skillReadFile(ctx, root, name, data); err != nil {
			return err
		}
	}
	return nil
}
func (r *linuxRoot) skillExisting(ctx context.Context, parent int, request SkillPrepareRequest, proof []byte) (SkillPrepareResult, error) {
	result := SkillPrepareResult{State: WriteNotCommitted}
	fd, _, err := r.skillDirectory(parent, request.RevisionID, false)
	if err != nil {
		return result, err
	}
	files := map[string][]byte{"manifest.json": proof}
	dirs := map[string]bool{".": true, "package": true}
	for _, file := range request.Files {
		files["package/"+file.Path] = file.Data
		parts := strings.Split("package/"+file.Path, "/")
		for n := 1; n < len(parts); n++ {
			dirs[strings.Join(parts[:n], "/")] = true
		}
	}
	err = r.skillCheckContents(ctx, fd, files, dirs)
	if err == nil {
		err = nativeErrorOrNil(unix.Fsync(fd))
	}
	if err == nil {
		err = nativeErrorOrNil(unix.Fsync(parent))
	}
	closeErr := r.calls.close(fd)
	if err != nil {
		return result, err
	}
	if closeErr != nil {
		return result, ErrUnavailable
	}
	if err := r.skillSyncAncestors(request.SkillID); err != nil {
		return SkillPrepareResult{State: WriteCommitted, RelativePath: skillDirectory + "/" + request.SkillID + "/" + request.RevisionID + "/package", Reused: true}, err
	}
	return SkillPrepareResult{State: WriteCommitted, RelativePath: skillDirectory + "/" + request.SkillID + "/" + request.RevisionID + "/package", Reused: true, Durable: true}, nil
}
func (r *linuxRoot) prepareSkill(ctx context.Context, request SkillPrepareRequest, proof []byte) (SkillPrepareResult, error) {
	return r.prepareSkillWith(ctx, request, proof, nil)
}
func (r *linuxRoot) prepareSkillWith(ctx context.Context, request SkillPrepareRequest, proof []byte, beforePublish func()) (result SkillPrepareResult, err error) {
	result.State = WriteNotCommitted
	if err = contextError(ctx); err != nil {
		return result, err
	}
	base, _, err := r.skillDirectory(r.fd, skillDirectory, true)
	if err != nil {
		return result, err
	}
	defer func() {
		if r.calls.close(base) != nil {
			err = errors.Join(err, ErrUnavailable)
		}
	}()
	parent, parentStat, err := r.skillDirectory(base, request.SkillID, true)
	if err != nil {
		return result, err
	}
	defer func() {
		if r.calls.close(parent) != nil {
			err = errors.Join(err, ErrUnavailable)
		}
	}()
	if existing, e := r.skillExisting(ctx, parent, request, proof); !errors.Is(e, ErrNotFound) {
		return existing, e
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return result, ErrUnavailable
	}
	temporary := ".prepare-" + hex.EncodeToString(nonce[:])
	if err = unix.Mkdirat(parent, temporary, 0700); err != nil {
		return result, nativeError(err)
	}
	temp, tempStat, err := r.skillDirectory(parent, temporary, false)
	if err != nil {
		return result, err
	}
	entries := []skillCreatedEntry{}
	published := false
	defer func() {
		if !published {
			if e := r.skillRemoveCreated(temp, tempStat, entries); e != nil {
				err = errors.Join(err, e)
			} else {
				var current unix.Stat_t
				e := unix.Fstatat(parent, temporary, &current, unix.AT_SYMLINK_NOFOLLOW)
				if e == nil && skillSameInode(current, tempStat) {
					e = unix.Unlinkat(parent, temporary, unix.AT_REMOVEDIR)
				} else if e == nil {
					e = ErrConflict
				}
				if e != nil {
					err = errors.Join(err, ErrUnavailable)
				}
			}
		}
		if r.calls.close(temp) != nil {
			err = errors.Join(err, ErrUnavailable)
		}
	}()
	dirs := map[string]bool{"package": true}
	for _, file := range request.Files {
		parts := strings.Split("package/"+file.Path, "/")
		for n := 1; n < len(parts); n++ {
			dirs[strings.Join(parts[:n], "/")] = true
		}
	}
	names := make([]string, 0, len(dirs))
	for name := range dirs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err = contextError(ctx); err != nil {
			return result, err
		}
		// All ancestors are this call's exclusive temporary directories; openat2
		// validates the full path again before any file publication.
		parentPath, child := path.Split(name)
		parentPath = strings.TrimSuffix(parentPath, "/")
		if parentPath == "" {
			parentPath = "."
		}
		parentFD, openErr := r.writeOpen(temp, parentPath, unix.O_RDONLY|unix.O_DIRECTORY)
		if openErr != nil {
			return result, openErr
		}
		mkdirErr := unix.Mkdirat(parentFD, child, 0700)
		closeErr := r.calls.close(parentFD)
		if mkdirErr != nil {
			return result, nativeError(mkdirErr)
		}
		if closeErr != nil {
			return result, ErrUnavailable
		}
		fd, st, e := r.skillDirectory(temp, name, false)
		if e != nil {
			return result, e
		}
		entries = append(entries, skillCreatedEntry{name, st, true})
		if r.calls.close(fd) != nil {
			return result, ErrUnavailable
		}
	}
	for _, file := range append(slices.Clone(request.Files), SkillFile{Path: "", Data: proof}) {
		name := "package/" + file.Path
		if file.Path == "" {
			name = "manifest.json"
		}
		entry, e := r.skillCreateFile(ctx, temp, name, file.Data)
		if entry.stat.Ino != 0 {
			entries = append(entries, entry)
		}
		if e != nil {
			return result, e
		}
	}
	for _, name := range names {
		fd, e := r.writeOpen(temp, name, unix.O_RDONLY|unix.O_DIRECTORY)
		if e != nil {
			return result, e
		}
		e = unix.Fsync(fd)
		if r.calls.close(fd) != nil {
			e = ErrUnavailable
		}
		if e != nil {
			return result, nativeError(e)
		}
	}
	if err = unix.Fsync(temp); err != nil {
		return result, nativeError(err)
	}
	if beforePublish != nil {
		beforePublish()
	}
	if err = contextError(ctx); err != nil {
		return result, err
	}
	if err = r.writeParentCurrent(skillDirectory+"/"+request.SkillID, parentStat); err != nil {
		return result, err
	}
	err = unix.Renameat2(parent, temporary, parent, request.RevisionID, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.EEXIST) {
		return r.skillExisting(ctx, parent, request, proof)
	}
	if err != nil {
		return result, nativeError(err)
	}
	published = true
	result = SkillPrepareResult{State: WriteCommitted, RelativePath: skillDirectory + "/" + request.SkillID + "/" + request.RevisionID + "/package"}
	if err = unix.Fsync(parent); err != nil {
		return result, nativeError(err)
	}
	if err = unix.Fsync(base); err != nil {
		return result, nativeError(err)
	}
	if err = r.skillSyncAncestors(request.SkillID); err != nil {
		return result, err
	}
	result.Durable = true
	return result, nil
}

func nativeErrorOrNil(err error) error {
	if err != nil {
		return nativeError(err)
	}
	return nil
}

// Workspace intentionally lends an O_PATH root. Fsync requires a readable
// descriptor: reopen only the pinned root and exact managed ancestors using
// the original no-link/no-mount resolver, and join every descriptor close.
func (r *linuxRoot) skillSyncAncestors(skill string) error {
	for _, name := range []string{skillDirectory + "/" + skill, skillDirectory, "."} {
		fd, _, err := r.skillDirectory(r.fd, name, false)
		if err != nil {
			return err
		}
		syncErr := nativeErrorOrNil(unix.Fsync(fd))
		closeErr := r.calls.close(fd)
		if syncErr != nil || closeErr != nil {
			return errors.Join(syncErr, nativeErrorOrNil(closeErr))
		}
	}
	return nil
}
