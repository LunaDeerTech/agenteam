package skill

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
	"testing"

	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func testInput(t *testing.T) sc.TextFiles {
	t.Helper()
	v, e := sc.NewTextFiles([]sc.TextFile{{Path: "references/\u4f8b.txt", UTF8Text: "owned guide\n"}, {Path: sc.EntryPath, UTF8Text: addSkillsText}, {Path: "empty.txt", UTF8Text: ""}})
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestCanonicalPackageDeterminismAndExactContent(t *testing.T) {
	in := testInput(t)
	a, e := BuildPackage(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	files, _ := in.Files()
	files[0], files[2] = files[2], files[0]
	in2, _ := sc.NewTextFiles(files)
	b, e := BuildPackage(context.Background(), in2)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := a.Bytes()
	raw2, _ := b.Bytes()
	if !bytes.Equal(raw, raw2) {
		t.Fatal("order changed canonical bytes")
	}
	parsed, e := ParseCanonicalPackage(context.Background(), raw)
	if e != nil {
		t.Fatal(e)
	}
	ha, _ := a.Digest()
	hb, _ := parsed.Digest()
	if ha != hb {
		t.Fatal("hash drift")
	}
	zr, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		t.Fatal(e)
	}
	expected, _ := in.Files()
	m, _ := a.Manifest()
	md, _ := m.Details()
	for i, f := range zr.File {
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		body, e := io.ReadAll(r)
		_ = r.Close()
		if e != nil || string(body) != expected[i].UTF8Text || f.Name != expected[i].Path || md.Files[i].SHA256 != sum(body) {
			t.Fatal("content/manifest mismatch", e)
		}
	}
	raw[0] ^= 255
	again, _ := a.Bytes()
	if bytes.Equal(raw, again) {
		t.Fatal("returned bytes alias")
	}
	if strings.Contains(fmt.Sprintf("%#v", struct{ private Package }{a}), "owned guide") {
		t.Fatal("recursive body leak")
	}
}
func TestCanonicalPackageRejectsNoncanonicalAndCorruptContainers(t *testing.T) {
	p, e := BuildPackage(context.Background(), testInput(t))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := p.Bytes()
	zr, _ := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	offset, _ := zr.File[0].DataOffset()
	corrupt := bytes.Clone(raw)
	corrupt[offset] ^= 1
	localName := bytes.Clone(raw)
	localName[30] ^= 1
	forgedSize := bytes.Clone(raw)
	central := bytes.Index(forgedSize, []byte{0x50, 0x4b, 1, 2})
	binary.LittleEndian.PutUint32(forgedSize[central+24:], uint32(sc.MaxFileBytes+1))
	for name, b := range map[string][]byte{"tail": append(bytes.Clone(raw), 0), "prefix": append([]byte("prefix"), raw...), "concat": append(bytes.Clone(raw), raw...), "crc": corrupt, "local-name": localName, "declared-size": forgedSize, "truncated": raw[:len(raw)-1]} {
		t.Run(name, func(t *testing.T) {
			if _, e := ParseCanonicalPackage(context.Background(), b); e == nil {
				t.Fatal("accepted malformed container")
			}
		})
	}
	for name, mutate := range map[string]func(*zip.FileHeader){"symlink": func(h *zip.FileHeader) { h.ExternalAttrs = 0120777 << 16 }, "device": func(h *zip.FileHeader) { h.ExternalAttrs = 0020644 << 16 }, "extra": func(h *zip.FileHeader) { h.Extra = []byte{1, 0, 0, 0} }, "comment": func(h *zip.FileHeader) { h.Comment = "x" }, "date": func(h *zip.FileHeader) { h.ModifiedDate++ }, "flags": func(h *zip.FileHeader) { h.Flags = 0 }, "escape": func(h *zip.FileHeader) { h.Name = "../SKILL.md" }, "directory": func(h *zip.FileHeader) { h.Name = "d/" }, "deflate": func(h *zip.FileHeader) { h.Method = zip.Deflate }} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			w := zip.NewWriter(&out)
			data := []byte(addSkillsText)
			h := &zip.FileHeader{Name: sc.EntryPath, Method: zip.Store, Flags: canonicalFlags, CreatorVersion: canonicalCreator, ReaderVersion: 20, ModifiedDate: canonicalDate, ExternalAttrs: canonicalMode, CRC32: crc32.ChecksumIEEE(data), CompressedSize64: uint64(len(data)), UncompressedSize64: uint64(len(data))}
			mutate(h)
			body, e := w.CreateRaw(h)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = body.Write(data); e != nil && name != "directory" {
				t.Fatal(e)
			}
			if e = w.Close(); e != nil {
				t.Fatal(e)
			}
			if _, e = ParseCanonicalPackage(context.Background(), out.Bytes()); e == nil {
				t.Fatal("accepted noncanonical header")
			}
		})
	}
}
func TestPackageCancellationAndZeroHandles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := BuildPackage(ctx, testInput(t)); !errors.Is(e, context.Canceled) {
		t.Fatal("lost cancellation", e)
	}
	if _, e := ParseCanonicalPackage(ctx, []byte("zip")); !errors.Is(e, context.Canceled) {
		t.Fatal("lost cancellation", e)
	}
	if _, e := BuildPackage(nil, testInput(t)); e == nil {
		t.Fatal("nil context")
	}
	if _, e := BuildPackage(context.Background(), sc.TextFiles{}); e == nil {
		t.Fatal("zero source")
	}
	if _, e := (Package{}).Bytes(); e == nil {
		t.Fatal("zero package")
	}
}

func TestCanonicalDirectoryCountIsBoundedBeforeZIPReader(t *testing.T) {
	p, e := BuildPackage(context.Background(), testInput(t))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := p.Bytes()
	if !boundedDirectory(raw) {
		t.Fatal("valid directory")
	}
	for _, count := range []uint16{0, 1, 129, 65535} {
		bad := bytes.Clone(raw)
		binary.LittleEndian.PutUint16(bad[len(bad)-14:], count)
		binary.LittleEndian.PutUint16(bad[len(bad)-12:], count)
		if boundedDirectory(bad) {
			t.Fatal("unbounded or mismatched actual directory")
		}
		if _, e := ParseCanonicalPackage(context.Background(), bad); e == nil {
			t.Fatal("invalid count accepted")
		}
	}
}
