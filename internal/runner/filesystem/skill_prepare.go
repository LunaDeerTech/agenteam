package filesystem

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const skillDirectory = ".agenteam-skills"
const maxSkillFiles = 128
const maxSkillFileBytes = 8 << 20
const maxSkillTotalBytes = 32 << 20

type SkillFile struct {
	Path string
	Data []byte
}

// SkillPrepareRequest is local owned input. Identity fixes the target directory;
// package paths never select a host path, cache directory or replacement target.
type SkillPrepareRequest struct {
	SkillID, RevisionID, ArchiveSHA256 string
	Files                              []SkillFile
}
type SkillPrepareResult struct {
	State           WriteState
	RelativePath    string
	Reused, Durable bool
}

func (SkillPrepareRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "filesystem_skill_prepare")
}
func (SkillPrepareRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"filesystem_skill_prepare"`), nil
}
func (SkillPrepareRequest) LogValue() slog.Value { return slog.StringValue("filesystem_skill_prepare") }

type skillFileProof struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type skillPackageProof struct {
	SkillID       string           `json:"skill_id"`
	RevisionID    string           `json:"revision_id"`
	ArchiveSHA256 string           `json:"archive_sha256"`
	Files         []skillFileProof `json:"files"`
}

func skillUUID(s string) bool {
	if len(s) != 36 || s[14] != '7' || !(s[19] == '8' || s[19] == '9' || s[19] == 'a' || s[19] == 'b') {
		return false
	}
	for n, c := range []byte(s) {
		if n == 8 || n == 13 || n == 18 || n == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return s != "00000000-0000-0000-0000-000000000000"
}
func skillDigest(b []byte) string { v := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(v[:]) }
func (r SkillPrepareRequest) proof() ([]byte, error) {
	if !skillUUID(r.SkillID) || !skillUUID(r.RevisionID) || len(r.ArchiveSHA256) != 71 || !strings.HasPrefix(r.ArchiveSHA256, "sha256:") || len(r.Files) == 0 || len(r.Files) > maxSkillFiles {
		return nil, ErrInvalid
	}
	if b, e := hex.DecodeString(r.ArchiveSHA256[7:]); e != nil || len(b) != 32 || strings.ToLower(r.ArchiveSHA256) != r.ArchiveSHA256 {
		return nil, ErrInvalid
	}
	p := skillPackageProof{SkillID: r.SkillID, RevisionID: r.RevisionID, ArchiveSHA256: r.ArchiveSHA256}
	paths := map[string]bool{}
	total := 0
	entry := false
	previous := ""
	for _, file := range r.Files {
		if !validPath(file.Path, false) || len(file.Path) > 1024 || strings.ContainsAny(file.Path, ":\r\n") || len(file.Data) > maxSkillFileBytes || previous != "" && previous >= file.Path {
			return nil, ErrInvalid
		}
		previous = file.Path
		total += len(file.Data)
		if total > maxSkillTotalBytes {
			return nil, ErrLimit
		}
		key := norm.NFC.String(cases.Fold().String(norm.NFC.String(file.Path)))
		if paths[key] {
			return nil, ErrInvalid
		}
		paths[key] = true
		if file.Path == "SKILL.md" {
			if len(file.Data) == 0 || len(file.Data) > 256<<10 {
				return nil, ErrInvalid
			}
			entry = true
		}
		p.Files = append(p.Files, skillFileProof{file.Path, len(file.Data), skillDigest(file.Data)})
	}
	if !entry {
		return nil, ErrInvalid
	}
	for path := range paths {
		parts := strings.Split(path, "/")
		for n := 1; n < len(parts); n++ {
			if paths[strings.Join(parts[:n], "/")] {
				return nil, ErrInvalid
			}
		}
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, ErrInvalid
	}
	return raw, nil
}

type nativeSkillPreparer interface {
	prepareSkill(context.Context, SkillPrepareRequest, []byte) (SkillPrepareResult, error)
}

func (r *Root) PrepareSkill(ctx context.Context, request SkillPrepareRequest) (SkillPrepareResult, error) {
	result := SkillPrepareResult{State: WriteNotCommitted}
	if _, err := request.proof(); err != nil {
		return result, err
	}
	op, err := r.begin(ctx)
	if err != nil {
		return result, err
	}
	request.Files = slices.Clone(request.Files)
	for i := range request.Files {
		request.Files[i].Data = bytes.Clone(request.Files[i].Data)
	}
	defer func() {
		for _, file := range request.Files {
			clear(file.Data)
		}
	}()
	proof, err := request.proof()
	if err == nil {
		if native, ok := r.fs.(nativeSkillPreparer); ok {
			result, err = native.prepareSkill(op.ctx, request, proof)
		} else {
			err = ErrUnsupported
		}
	}
	r.mu.Lock()
	if cancelled := contextError(op.ctx); cancelled != nil {
		err = errors.Join(err, cancelled)
	}
	delete(r.active, op)
	if r.stopped && len(r.active) == 0 {
		close(r.quiet)
	}
	r.mu.Unlock()
	op.cancel()
	return result, err
}
