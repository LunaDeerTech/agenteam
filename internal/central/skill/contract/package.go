package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	PackageFormat    = "skill-zip-v1"
	PackageMediaType = "application/zip"
	EntryPath        = "SKILL.md"
	MaxFiles         = 128
	MaxFileBytes     = 8 << 20
	MaxTotalBytes    = 32 << 20
	MaxPathBytes     = 1024
	MaxEntryBytes    = 256 << 10
	MaxArchiveBytes  = MaxTotalBytes + MaxFiles*(2*MaxPathBytes+128) + 22
	MaxManifestBytes = MaxFiles*(6*(MaxPathBytes+256)+256) + 1024
)

// ValidatePath preserves valid spelling. Collision checks use NFC and full
// Unicode case folding separately, including collisions with file ancestors.
func ValidatePath(path string) error {
	if len(path) == 0 || len(path) > MaxPathBytes || !utf8.ValidString(path) || strings.ContainsAny(path, "\\:") {
		return invalid()
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return invalid()
		}
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return invalid()
		}
	}
	return nil
}
func pathKey(path string) string { return norm.NFC.String(cases.Fold().String(norm.NFC.String(path))) }
func validPathSet(paths []string) bool {
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		if ValidatePath(path) != nil {
			return false
		}
		k := pathKey(path)
		if seen[k] {
			return false
		}
		seen[k] = true
	}
	for k := range seen {
		parts := strings.Split(k, "/")
		for i := 1; i < len(parts); i++ {
			if seen[strings.Join(parts[:i], "/")] {
				return false
			}
		}
	}
	return true
}

type File struct {
	Path      string     `json:"path"`
	MediaType string     `json:"media_type"`
	ByteSize  f.Progress `json:"byte_size"`
	SHA256    f.Digest   `json:"sha256"`
}

func (v File) Validate() error {
	kind, params, e := mime.ParseMediaType(v.MediaType)
	if ValidatePath(v.Path) != nil || e != nil || len(v.MediaType) > 256 || mime.FormatMediaType(kind, params) != v.MediaType || v.ByteSize.Validate() != nil || v.ByteSize > MaxFileBytes || v.SHA256.Validate() != nil {
		return invalid()
	}
	if v.Path == EntryPath && (v.ByteSize <= 0 || v.ByteSize > MaxEntryBytes) {
		return invalid()
	}
	return nil
}
func (v File) MarshalJSON() ([]byte, error) {
	if e := v.Validate(); e != nil {
		return nil, e
	}
	type wire File
	return json.Marshal(wire(v))
}
func (v *File) UnmarshalJSON(raw []byte) error {
	if strictObject(raw, 6*(MaxPathBytes+256)+256, []string{"path", "media_type", "byte_size", "sha256"}) != nil {
		return invalid()
	}
	type wire File
	var out wire
	if json.Unmarshal(raw, &out) != nil || File(out).Validate() != nil {
		return invalid()
	}
	*v = File(out)
	return nil
}

type ManifestDetails struct {
	Format    string `json:"format"`
	EntryPath string `json:"entry_path"`
	Files     []File `json:"files"`
}
type Manifest struct{ data func() ManifestDetails }

func NewManifest(files []File) (Manifest, error) {
	if len(files) == 0 || len(files) > MaxFiles {
		return Manifest{}, invalid()
	}
	copy := slices.Clone(files)
	paths := make([]string, len(copy))
	var total int64
	entry := false
	for i, v := range copy {
		if v.Validate() != nil {
			return Manifest{}, invalid()
		}
		total += int64(v.ByteSize)
		if total > MaxTotalBytes {
			return Manifest{}, invalid()
		}
		paths[i] = v.Path
		entry = entry || v.Path == EntryPath
	}
	if !entry || !validPathSet(paths) {
		return Manifest{}, invalid()
	}
	slices.SortFunc(copy, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return Manifest{func() ManifestDetails { return ManifestDetails{PackageFormat, EntryPath, slices.Clone(copy)} }}, nil
}
func (m Manifest) Validate() error {
	if m.data == nil {
		return invalid()
	}
	return nil
}
func (m Manifest) Details() (ManifestDetails, error) {
	if m.Validate() != nil {
		return ManifestDetails{}, invalid()
	}
	return m.data(), nil
}
func (m Manifest) Digest() (f.Digest, error) {
	raw, e := m.MarshalJSON()
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(append([]byte("skill.manifest.v1\x00"), raw...))
	return f.Digest("sha256:" + hex.EncodeToString(h[:])), nil
}
func (m Manifest) MarshalJSON() ([]byte, error) {
	if m.Validate() != nil {
		return nil, invalid()
	}
	return json.Marshal(m.data())
}
func (*Manifest) UnmarshalJSON([]byte) error { return invalid() }
func (Manifest) Format(w fmt.State, _ rune)  { _, _ = io.WriteString(w, "skill_manifest") }
func (Manifest) LogValue() slog.Value        { return slog.StringValue("skill_manifest") }
func DecodeManifest(raw []byte) (Manifest, error) {
	if strictObject(raw, MaxManifestBytes, []string{"format", "entry_path", "files"}) != nil {
		return Manifest{}, invalid()
	}
	var v ManifestDetails
	if json.Unmarshal(raw, &v) != nil || v.Format != PackageFormat || v.EntryPath != EntryPath {
		return Manifest{}, invalid()
	}
	m, e := NewManifest(v.Files)
	if e != nil {
		return Manifest{}, e
	}
	// Repository encoding is canonical, including file ordering. Object-key order
	// and insignificant JSON whitespace are not authority and may differ.
	d, _ := m.Details()
	if !slices.Equal(d.Files, v.Files) {
		return Manifest{}, invalid()
	}
	return m, nil
}

type TextFile struct {
	Path     string
	UTF8Text string
}
type TextFiles struct{ data func() []TextFile }
type EntryMetadata struct{ Name, Description string }

// EntryMetadataFromText supports only two literal, single-line frontmatter
// values in the fixed order below. No YAML tags, aliases or implicit conversion.
func EntryMetadataFromText(text string) (EntryMetadata, error) {
	if len(text) == 0 || len(text) > MaxEntryBytes || !validText(text) || !strings.HasPrefix(text, "---\n") {
		return EntryMetadata{}, invalid()
	}
	header, body, ok := strings.Cut(text[4:], "\n---\n")
	lines := strings.Split(header, "\n")
	if !ok || len(lines) != 2 || strings.TrimSpace(body) == "" || !strings.HasPrefix(lines[0], "name: ") || !strings.HasPrefix(lines[1], "description: ") {
		return EntryMetadata{}, invalid()
	}
	v := EntryMetadata{strings.TrimPrefix(lines[0], "name: "), strings.TrimPrefix(lines[1], "description: ")}
	if _, e := NormalizeName(v.Name); e != nil || !displayText(v.Description, MaxDescriptionBytes, false) || strings.TrimSpace(v.Description) != v.Description {
		return EntryMetadata{}, invalid()
	}
	return v, nil
}
func validText(text string) bool {
	return utf8.ValidString(text) && !strings.ContainsAny(text, "\x00\r")
}
func NewTextFiles(files []TextFile) (TextFiles, error) {
	if len(files) == 0 || len(files) > MaxFiles {
		return TextFiles{}, invalid()
	}
	copied := slices.Clone(files)
	paths := make([]string, len(copied))
	total := 0
	entry := false
	for i, v := range copied {
		if len(v.UTF8Text) > MaxFileBytes || !validText(v.UTF8Text) {
			return TextFiles{}, invalid()
		}
		total += len(v.UTF8Text)
		if total > MaxTotalBytes {
			return TextFiles{}, invalid()
		}
		paths[i] = v.Path
		if v.Path == EntryPath {
			if _, e := EntryMetadataFromText(v.UTF8Text); e != nil {
				return TextFiles{}, e
			}
			entry = true
		}
	}
	if !entry || !validPathSet(paths) {
		return TextFiles{}, invalid()
	}
	slices.SortFunc(copied, func(a, b TextFile) int { return strings.Compare(a.Path, b.Path) })
	return TextFiles{func() []TextFile { return slices.Clone(copied) }}, nil
}
func (t TextFiles) Validate() error {
	if t.data == nil {
		return invalid()
	}
	return nil
}
func (t TextFiles) Files() ([]TextFile, error) {
	if t.Validate() != nil {
		return nil, invalid()
	}
	return t.data(), nil
}
func (TextFiles) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "skill_text_files") }
func (TextFiles) MarshalJSON() ([]byte, error) { return []byte(`"skill_text_files"`), nil }
func (*TextFiles) UnmarshalJSON([]byte) error  { return invalid() }
func (TextFiles) LogValue() slog.Value         { return slog.StringValue("skill_text_files") }
