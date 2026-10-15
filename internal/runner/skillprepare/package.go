package skillprepare

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"strconv"

	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

var errPackage = errors.New("skill prepare: invalid package")

func packageDigest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func packageDirectory(raw []byte) bool {
	if len(raw) < 22 {
		return false
	}
	end := raw[len(raw)-22:]
	u16, u32 := binary.LittleEndian.Uint16, binary.LittleEndian.Uint32
	count := int(u16(end[10:]))
	if u32(end) != 0x06054b50 || u16(end[4:]) != 0 || u16(end[6:]) != 0 || int(u16(end[8:])) != count || count < 1 || count > p.MaxSkillFiles || u16(end[20:]) != 0 {
		return false
	}
	start, size := uint64(u32(end[16:])), uint64(u32(end[12:]))
	if start+size != uint64(len(raw)-22) {
		return false
	}
	pos, stop := int(start), len(raw)-22
	for n := 0; n < count; n++ {
		if pos > stop-46 || u32(raw[pos:]) != 0x02014b50 {
			return false
		}
		header := raw[pos : pos+46]
		name, extra, comment := int(u16(header[28:])), int(u16(header[30:])), int(u16(header[32:]))
		if name < 1 || name > p.MaxSkillPathBytes || extra != 0 || comment != 0 || name > stop-pos-46 {
			return false
		}
		pos += 46 + name
	}
	return pos == stop
}

// Decode only the installed skill-zip-v1 format. Central business packages are
// deliberately not imported by Runner. Both the whole archive and each file
// must match the original authorized manifest, including sorted identities.
func unpack(ctx context.Context, raw []byte, input p.SkillPreparePayload) (files []filesystem.SkillFile, err error) {
	size, e := strconv.ParseInt(string(input.ArchiveBytes), 10, 64)
	if e != nil || int64(len(raw)) != size || packageDigest(raw) != input.ArchiveSHA256 || !packageDirectory(raw) {
		return nil, errPackage
	}
	z, e := zip.NewReader(bytes.NewReader(raw), size)
	if e != nil || z.Comment != "" || len(z.File) != len(input.Manifest.Files) {
		return nil, errPackage
	}
	defer func() {
		if err != nil {
			for _, file := range files {
				clear(file.Data)
			}
			files = nil
		}
	}()
	for n, file := range z.File {
		if err = ctx.Err(); err != nil {
			return files, err
		}
		expected := input.Manifest.Files[n]
		length, _ := strconv.ParseUint(string(expected.ByteSize), 10, 64)
		if file.Name != expected.Path || file.Method != zip.Store || file.Flags != 0x800 || file.CreatorVersion != 3<<8|20 || file.ReaderVersion != 20 || file.ModifiedDate != 1<<5|1 || file.ModifiedTime != 0 || file.ExternalAttrs != uint32(0100644)<<16 || len(file.Extra) != 0 || file.Comment != "" || file.CompressedSize64 != length || file.UncompressedSize64 != length {
			return files, errPackage
		}
		reader, e := file.Open()
		if e != nil {
			return files, errPackage
		}
		body, e := readBounded(ctx, reader, int64(length))
		closeErr := reader.Close()
		if e != nil || closeErr != nil || uint64(len(body)) != length || packageDigest(body) != expected.SHA256 {
			clear(body)
			if e != nil {
				return files, e
			}
			return files, errPackage
		}
		files = append(files, filesystem.SkillFile{Path: expected.Path, Data: body})
	}
	return files, nil
}
func readBounded(ctx context.Context, reader io.Reader, limit int64) ([]byte, error) {
	var out bytes.Buffer
	buf := make([]byte, 64<<10)
	defer clear(buf)
	for {
		if err := ctx.Err(); err != nil {
			clear(out.Bytes())
			return nil, err
		}
		n, err := reader.Read(buf[:min(int64(len(buf)), limit-int64(out.Len())+1)])
		if n > 0 {
			if int64(out.Len()+n) > limit {
				clear(out.Bytes())
				return nil, errPackage
			}
			_, _ = out.Write(buf[:n])
		}
		if err != nil {
			if err == io.EOF {
				return out.Bytes(), nil
			}
			clear(out.Bytes())
			return nil, err
		}
		if n == 0 {
			clear(out.Bytes())
			return nil, io.ErrNoProgress
		}
	}
}
