package runnerprotocol

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	SkillPrepareOperation         = "prepare-skill"
	SkillPrepareRevision  Decimal = "1"
	SkillPackageFormat            = "skill-zip-v1"
	MaxSkillFiles                 = 128
	MaxSkillFileBytes             = 8 << 20
	MaxSkillTotalBytes            = 32 << 20
	MaxSkillPathBytes             = 1024
	MaxSkillArchiveBytes          = MaxSkillTotalBytes + MaxSkillFiles*(2*MaxSkillPathBytes+128) + 22
	// This name is reserved for platform-managed immutable Skill revisions.
	SkillDirectory = ".agenteam-skills"
)

type SkillFile struct {
	Path      string  `json:"path"`
	MediaType string  `json:"media_type"`
	ByteSize  Decimal `json:"byte_size"`
	SHA256    string  `json:"sha256"`
}
type SkillManifest struct {
	Format    string      `json:"format"`
	EntryPath string      `json:"entry_path"`
	Files     []SkillFile `json:"files"`
}

// SkillTransfer is private control material. It is never a Tool result, log
// value or authority; only the exact authenticated operation may consume it.
type SkillTransfer struct {
	ID        ID          `json:"id"`
	Method    string      `json:"method"`
	URL       string      `json:"url"`
	Headers   http.Header `json:"headers"`
	ExpiresAt Instant     `json:"expires_at"`
}
type SkillPreparePayload struct {
	WorkspaceName string        `json:"workspace_name"`
	SkillID       ID            `json:"skill_id"`
	RevisionID    ID            `json:"revision_id"`
	Revision      Decimal       `json:"revision"`
	ArchiveBytes  Decimal       `json:"archive_bytes"`
	ArchiveSHA256 string        `json:"archive_sha256"`
	Manifest      SkillManifest `json:"manifest"`
	Transfer      SkillTransfer `json:"transfer"`
}
type SkillPrepareResult struct {
	SkillID       ID      `json:"skill_id"`
	RevisionID    ID      `json:"revision_id"`
	Revision      Decimal `json:"revision"`
	TransferID    ID      `json:"transfer_id"`
	ArchiveSHA256 string  `json:"archive_sha256"`
	RelativePath  string  `json:"relative_path"`
	Reused        bool    `json:"reused"`
	Durable       bool    `json:"durable"`
}

func (SkillTransfer) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "private_skill_transfer") }
func (SkillTransfer) MarshalJSON() ([]byte, error) { return []byte(`"private_skill_transfer"`), nil }
func (SkillTransfer) LogValue() slog.Value         { return slog.StringValue("private_skill_transfer") }
func (SkillPreparePayload) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_prepare_payload")
}
func (SkillPreparePayload) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_prepare_payload"`), nil
}
func (SkillPreparePayload) LogValue() slog.Value { return slog.StringValue("skill_prepare_payload") }

func skillPath(value string) bool {
	if len(value) == 0 || len(value) > MaxSkillPathBytes || !utf8.ValidString(value) || strings.ContainsAny(value, "\\:") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." || len(part) > 255 {
			return false
		}
	}
	return true
}
func (v SkillManifest) Validate() error {
	if v.Format != SkillPackageFormat || v.EntryPath != "SKILL.md" || len(v.Files) == 0 || len(v.Files) > MaxSkillFiles {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(v.Files))
	var total uint64
	entry := false
	previous := ""
	for _, f := range v.Files {
		kind, params, err := mime.ParseMediaType(f.MediaType)
		if !skillPath(f.Path) || previous != "" && previous >= f.Path || err != nil || len(f.MediaType) > 256 || mime.FormatMediaType(kind, params) != f.MediaType || !decimal(f.ByteSize, MaxSkillFileBytes, false) || !digest(f.SHA256) {
			return ErrInvalid
		}
		previous = f.Path
		size, _ := strconv.ParseUint(string(f.ByteSize), 10, 64)
		total += size
		if total > MaxSkillTotalBytes || f.Path == v.EntryPath && (size == 0 || size > 256<<10) {
			return ErrInvalid
		}
		entry = entry || f.Path == v.EntryPath
		key := norm.NFC.String(cases.Fold().String(norm.NFC.String(f.Path)))
		if seen[key] {
			return ErrInvalid
		}
		seen[key] = true
	}
	if !entry {
		return ErrInvalid
	}
	for path := range seen {
		parts := strings.Split(path, "/")
		for n := 1; n < len(parts); n++ {
			if seen[strings.Join(parts[:n], "/")] {
				return ErrInvalid
			}
		}
	}
	return nil
}
func (v SkillTransfer) Validate() error {
	u, err := url.Parse(v.URL)
	if !v.ID.Valid() || v.Method != http.MethodGet || err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.Scheme != "https" && u.Scheme != "http" || len(v.URL) > 16<<10 || !v.ExpiresAt.Valid() || v.Headers == nil || len(v.Headers) > 32 {
		return ErrInvalid
	}
	size := 0
	for k, values := range v.Headers {
		if k == "" || http.CanonicalHeaderKey(k) != k || len(values) == 0 || len(values) > 8 {
			return ErrInvalid
		}
		for _, c := range k {
			if c <= 32 || c >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}", c) {
				return ErrInvalid
			}
		}
		for _, s := range values {
			if strings.ContainsAny(s, "\r\n\x00") {
				return ErrInvalid
			}
			size += len(k) + len(s)
		}
	}
	if size > 32<<10 {
		return ErrTooLarge
	}
	return nil
}
func (v SkillPreparePayload) Validate() error {
	if !readFileWorkspace(v.WorkspaceName) || !v.SkillID.Valid() || !v.RevisionID.Valid() || !decimal(v.Revision, math.MaxInt64, true) || !decimal(v.ArchiveBytes, MaxSkillArchiveBytes, true) || !digest(v.ArchiveSHA256) || v.Manifest.Validate() != nil || v.Transfer.Validate() != nil {
		return ErrInvalid
	}
	return nil
}
func SkillRevisionPath(skill, revision ID) (string, error) {
	if !skill.Valid() || !revision.Valid() {
		return "", ErrInvalid
	}
	return SkillDirectory + "/" + string(skill) + "/" + string(revision) + "/package", nil
}
func (v SkillPrepareResult) Validate() error {
	path, err := SkillRevisionPath(v.SkillID, v.RevisionID)
	if err != nil || !decimal(v.Revision, math.MaxInt64, true) || !v.TransferID.Valid() || !digest(v.ArchiveSHA256) || v.RelativePath != path {
		return ErrInvalid
	}
	return nil
}

type skillTransferWire SkillTransfer
type skillPrepareWire struct {
	WorkspaceName string            `json:"workspace_name"`
	SkillID       ID                `json:"skill_id"`
	RevisionID    ID                `json:"revision_id"`
	Revision      Decimal           `json:"revision"`
	ArchiveBytes  Decimal           `json:"archive_bytes"`
	ArchiveSHA256 string            `json:"archive_sha256"`
	Manifest      SkillManifest     `json:"manifest"`
	Transfer      skillTransferWire `json:"transfer"`
}

func EncodeSkillPreparePayload(v SkillPreparePayload) ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(skillPrepareWire{v.WorkspaceName, v.SkillID, v.RevisionID, v.Revision, v.ArchiveBytes, v.ArchiveSHA256, v.Manifest, skillTransferWire(v.Transfer)})
	if err != nil {
		return nil, ErrInvalid
	}
	if len(raw) > MaxMessageBytes {
		return nil, ErrTooLarge
	}
	return raw, nil
}
func DecodeSkillPreparePayload(raw []byte) (SkillPreparePayload, error) {
	root, err := parseJSON(raw)
	if err != nil {
		return SkillPreparePayload{}, err
	}
	m, err := fields(root, words("workspace_name skill_id revision_id revision archive_bytes archive_sha256 manifest transfer"), nil)
	if err != nil {
		return SkillPreparePayload{}, err
	}
	manifest, err := fields(m["manifest"], words("format entry_path files"), nil)
	if err != nil {
		return SkillPreparePayload{}, err
	}
	files, ok := manifest["files"].([]any)
	if !ok || len(files) > MaxSkillFiles {
		return SkillPreparePayload{}, ErrInvalid
	}
	for _, file := range files {
		if _, err := fields(file, words("path media_type byte_size sha256"), nil); err != nil {
			return SkillPreparePayload{}, err
		}
	}
	if _, err := fields(m["transfer"], words("id method url headers expires_at"), nil); err != nil {
		return SkillPreparePayload{}, err
	}
	var w skillPrepareWire
	if json.Unmarshal(raw, &w) != nil {
		return SkillPreparePayload{}, ErrInvalid
	}
	v := SkillPreparePayload{w.WorkspaceName, w.SkillID, w.RevisionID, w.Revision, w.ArchiveBytes, w.ArchiveSHA256, w.Manifest, SkillTransfer(w.Transfer)}
	if err := v.Validate(); err != nil {
		return SkillPreparePayload{}, err
	}
	return v, nil
}
func EncodeSkillPrepareResult(v SkillPrepareResult) ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
func DecodeSkillPrepareResult(raw []byte) (SkillPrepareResult, error) {
	root, err := parseJSON(raw)
	if err != nil {
		return SkillPrepareResult{}, err
	}
	if _, err = fields(root, words("skill_id revision_id revision transfer_id archive_sha256 relative_path reused durable"), nil); err != nil {
		return SkillPrepareResult{}, err
	}
	var v SkillPrepareResult
	if json.Unmarshal(raw, &v) != nil || v.Validate() != nil {
		return SkillPrepareResult{}, ErrInvalid
	}
	return v, nil
}
