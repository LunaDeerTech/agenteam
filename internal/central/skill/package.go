// Package skill currently supplies pure, deterministic package preparation. It
// has no initializer, database, object publisher, Agent or Tool implementation.
package skill

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"strings"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

const MaxPackageProcessingTime = 2 * time.Second
const copyChunk = 64 << 10
const canonicalFlags = 0x800
const canonicalMode = uint32(0100644) << 16
const canonicalCreator = 3<<8 | 20
const canonicalDate = 1<<5 | 1 // 1980-01-01, DOS UTC-independent date

type packageData struct {
	bytes    []byte
	manifest sc.Manifest
	digest   f.Digest
	entry    sc.EntryMetadata
}
type Package struct{ data func() packageData }

func invalidPackage() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func (p Package) Validate() error {
	if p.data == nil {
		return invalidPackage()
	}
	return nil
}
func (p Package) Bytes() ([]byte, error) {
	if p.Validate() != nil {
		return nil, invalidPackage()
	}
	return bytes.Clone(p.data().bytes), nil
}
func (p Package) Manifest() (sc.Manifest, error) {
	if p.Validate() != nil {
		return sc.Manifest{}, invalidPackage()
	}
	return p.data().manifest, nil
}
func (p Package) Digest() (f.Digest, error) {
	if p.Validate() != nil {
		return "", invalidPackage()
	}
	return p.data().digest, nil
}
func (p Package) Size() (f.Progress, error) {
	if p.Validate() != nil {
		return 0, invalidPackage()
	}
	return f.Progress(len(p.data().bytes)), nil
}
func (p Package) Entry() (sc.EntryMetadata, error) {
	if p.Validate() != nil {
		return sc.EntryMetadata{}, invalidPackage()
	}
	return p.data().entry, nil
}
func (Package) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "skill_package") }
func (Package) MarshalJSON() ([]byte, error) { return []byte(`"skill_package"`), nil }
func (*Package) UnmarshalJSON([]byte) error  { return invalidPackage() }
func (Package) LogValue() slog.Value         { return slog.StringValue("skill_package") }

func packageContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, invalidPackage()
	}
	ctx, cancel := context.WithTimeout(ctx, MaxPackageProcessingTime)
	return ctx, cancel, nil
}
func sum(raw []byte) f.Digest {
	h := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(h[:]))
}
func textMedia(path string) string {
	if strings.HasSuffix(path, ".md") {
		return "text/markdown; charset=utf-8"
	}
	return "text/plain; charset=utf-8"
}

// BuildPackage accepts only validated TextFiles, not paths in the host file
// system. Every output is ZIP Store with pinned headers and no extra records.
func BuildPackage(ctx context.Context, input sc.TextFiles) (Package, error) {
	ctx, cancel, err := packageContext(ctx)
	if err != nil {
		return Package{}, err
	}
	defer cancel()
	if input.Validate() != nil {
		return Package{}, invalidPackage()
	}
	if err = ctx.Err(); err != nil {
		return Package{}, err
	}
	files, _ := input.Files()
	return encode(ctx, files)
}
func encode(ctx context.Context, files []sc.TextFile) (Package, error) {
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	manifest := make([]sc.File, 0, len(files))
	var entry sc.EntryMetadata
	for _, file := range files {
		if e := ctx.Err(); e != nil {
			return Package{}, e
		}
		raw := []byte(file.UTF8Text)
		checksum := crc32.NewIEEE()
		digest := sha256.New()
		for start := 0; start < len(raw); start += copyChunk {
			if e := ctx.Err(); e != nil {
				return Package{}, e
			}
			end := min(start+copyChunk, len(raw))
			_, _ = checksum.Write(raw[start:end])
			_, _ = digest.Write(raw[start:end])
		}
		h := &zip.FileHeader{Name: file.Path, Method: zip.Store, Flags: canonicalFlags, CreatorVersion: canonicalCreator, ReaderVersion: 20, ModifiedDate: canonicalDate, ExternalAttrs: canonicalMode, CRC32: checksum.Sum32(), CompressedSize64: uint64(len(raw)), UncompressedSize64: uint64(len(raw))}
		body, e := w.CreateRaw(h)
		if e != nil {
			return Package{}, invalidPackage()
		}
		for start := 0; start < len(raw); start += copyChunk {
			if e = ctx.Err(); e != nil {
				return Package{}, e
			}
			end := min(start+copyChunk, len(raw))
			if _, e = body.Write(raw[start:end]); e != nil {
				return Package{}, invalidPackage()
			}
		}
		manifest = append(manifest, sc.File{Path: file.Path, MediaType: textMedia(file.Path), ByteSize: f.Progress(len(raw)), SHA256: f.Digest("sha256:" + hex.EncodeToString(digest.Sum(nil)))})
		if file.Path == sc.EntryPath {
			entry, e = sc.EntryMetadataFromText(file.UTF8Text)
			if e != nil {
				return Package{}, e
			}
		}
	}
	if w.Close() != nil || out.Len() > sc.MaxArchiveBytes {
		return Package{}, invalidPackage()
	}
	if err := ctx.Err(); err != nil {
		return Package{}, err
	}
	m, err := sc.NewManifest(manifest)
	if err != nil {
		return Package{}, err
	}
	raw := out.Bytes()
	data := packageData{raw, m, sum(raw), entry}
	if err := ctx.Err(); err != nil {
		return Package{}, err
	}
	return Package{func() packageData { return data }}, nil
}

// ParseCanonicalPackage validates bounded text-only ZIP v1. It is a pure codec,
// not an external installation or authorization port. Re-encoding must match
// every input byte, rejecting prefix/tail/concatenation/alternate ZIP layouts.
func ParseCanonicalPackage(ctx context.Context, raw []byte) (Package, error) {
	ctx, cancel, err := packageContext(ctx)
	if err != nil {
		return Package{}, err
	}
	defer cancel()
	if len(raw) == 0 || len(raw) > sc.MaxArchiveBytes {
		return Package{}, invalidPackage()
	}
	if err = ctx.Err(); err != nil {
		return Package{}, err
	}
	input := bytes.Clone(raw)
	if !boundedDirectory(input) {
		return Package{}, invalidPackage()
	}
	r, err := zip.NewReader(bytes.NewReader(input), int64(len(input)))
	if err != nil || r.Comment != "" || len(r.File) == 0 || len(r.File) > sc.MaxFiles {
		return Package{}, invalidPackage()
	}
	files := make([]sc.TextFile, 0, len(r.File))
	var total uint64
	for _, file := range r.File {
		if err = ctx.Err(); err != nil {
			return Package{}, err
		}
		if sc.ValidatePath(file.Name) != nil || file.Method != zip.Store || file.Flags != canonicalFlags || file.CreatorVersion != canonicalCreator || file.ReaderVersion != 20 || file.ModifiedDate != canonicalDate || file.ModifiedTime != 0 || file.ExternalAttrs != canonicalMode || len(file.Extra) != 0 || file.Comment != "" || file.CompressedSize64 != file.UncompressedSize64 || file.UncompressedSize64 > sc.MaxFileBytes {
			return Package{}, invalidPackage()
		}
		total += file.UncompressedSize64
		if total > sc.MaxTotalBytes {
			return Package{}, invalidPackage()
		}
		reader, e := file.Open()
		if e != nil {
			return Package{}, invalidPackage()
		}
		var body bytes.Buffer
		buf := make([]byte, copyChunk)
		for {
			if e = ctx.Err(); e != nil {
				_ = reader.Close()
				return Package{}, e
			}
			n, readErr := reader.Read(buf)
			if n > 0 {
				if body.Len()+n > sc.MaxFileBytes {
					_ = reader.Close()
					return Package{}, invalidPackage()
				}
				_, _ = body.Write(buf[:n])
			}
			if readErr != nil {
				if readErr != io.EOF {
					_ = reader.Close()
					return Package{}, invalidPackage()
				}
				break
			}
		}
		if reader.Close() != nil || uint64(body.Len()) != file.UncompressedSize64 {
			return Package{}, invalidPackage()
		}
		files = append(files, sc.TextFile{Path: file.Name, UTF8Text: body.String()})
	}
	validated, e := sc.NewTextFiles(files)
	if e != nil {
		return Package{}, e
	}
	ordered, _ := validated.Files()
	result, e := encode(ctx, ordered)
	if e != nil {
		return Package{}, e
	}
	if !bytes.Equal(input, result.data().bytes) {
		return Package{}, invalidPackage()
	}
	return result, nil
}

// Bound the actual directory walk before archive/zip allocates per-entry data.
// Checking len(Reader.File) afterwards would already have parsed an attacker's
// oversized directory (including ZIP's modulo-65536 entry-count ambiguity).
func boundedDirectory(raw []byte) bool {
	if len(raw) < 22 {
		return false
	}
	end := raw[len(raw)-22:]
	u16 := binary.LittleEndian.Uint16
	u32 := binary.LittleEndian.Uint32
	count := int(u16(end[10:]))
	if u32(end) != 0x06054b50 || u16(end[4:]) != 0 || u16(end[6:]) != 0 || int(u16(end[8:])) != count || count < 1 || count > sc.MaxFiles || u16(end[20:]) != 0 {
		return false
	}
	start, size := uint64(u32(end[16:])), uint64(u32(end[12:]))
	if start+size != uint64(len(raw)-22) {
		return false
	}
	pos := int(start)
	stop := len(raw) - 22
	for i := 0; i < count; i++ {
		if pos > stop-46 || u32(raw[pos:]) != 0x02014b50 {
			return false
		}
		header := raw[pos : pos+46]
		name := int(u16(header[28:]))
		extra := int(u16(header[30:]))
		comment := int(u16(header[32:]))
		if name < 1 || name > sc.MaxPathBytes || extra != 0 || comment != 0 || name > stop-pos-46 {
			return false
		}
		pos += 46 + name
	}
	return pos == stop
}
