package account

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func b04AvatarPNG(t *testing.T, width, height int, fill color.Color) []byte {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(m, m.Bounds(), image.NewUniform(fill), image.Point{}, draw.Src)
	var out bytes.Buffer
	if err := png.Encode(&out, m); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func b04AvatarJPEG(t *testing.T) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 31, 23))
	draw.Draw(m, m.Bounds(), image.NewUniform(color.RGBA{R: 180, G: 75, B: 31, A: 255}), image.Point{}, draw.Src)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, m, &jpeg.Options{Quality: 91}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func b04AvatarFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "b04-avatar", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func b04AvatarReject(t *testing.T, raw []byte, media string) {
	t.Helper()
	result, err := prepareAvatarImage(raw, media)
	var f *foundation.Fault
	if !errors.As(err, &f) || f.CommitState != foundation.NotStarted || len(result.jpeg) != 0 || result.originalDigest != "" {
		t.Fatalf("invalid image produced a payload or unsafe error: %v", err)
	}
	if f.Code != foundation.InvalidArgument && f.Code != foundation.PayloadTooLarge && f.Code != foundation.UnsupportedMediaType {
		t.Fatal("wrong image error class", f)
	}
}

func b04PNGChunk(kind string, payload []byte) []byte {
	b := make([]byte, 12+len(payload))
	binary.BigEndian.PutUint32(b, uint32(len(payload)))
	copy(b[4:8], kind)
	copy(b[8:], payload)
	binary.BigEndian.PutUint32(b[len(b)-4:], crc32.ChecksumIEEE(b[4:len(b)-4]))
	return b
}

func b04PNGInsert(raw []byte, kind string, payload []byte) []byte {
	b := append([]byte(nil), raw[:33]...)
	b = append(b, b04PNGChunk(kind, payload)...)
	return append(b, raw[33:]...)
}

func b04JPEGSegment(raw []byte, marker byte, payload []byte) []byte {
	b := []byte{0xff, 0xd8, 0xff, marker, 0, 0}
	binary.BigEndian.PutUint16(b[4:6], uint16(len(payload)+2))
	b = append(b, payload...)
	return append(b, raw[2:]...)
}

func b04WebPChunk(kind string, payload []byte) []byte {
	b := make([]byte, 8+len(payload)+len(payload)%2)
	copy(b[:4], kind)
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(payload)))
	copy(b[8:], payload)
	return b
}

func b04WebPAppend(raw []byte, kind string, payload []byte) []byte {
	b := append(append([]byte(nil), raw...), b04WebPChunk(kind, payload)...)
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(b)-8))
	return b
}

func TestB04AvatarStaticFormatsReencodeAndRetainOriginalDigest(t *testing.T) {
	for _, tc := range []struct {
		name, mime string
		raw        []byte
		w, h       int
	}{
		{"baseline_jpeg", "image/jpeg", b04AvatarJPEG(t), 31, 23},
		{"progressive_multiscan", "image/jpeg", b04AvatarFixture(t, "progressive.jpg"), 96, 64},
		{"png", "image/png", b04AvatarPNG(t, 61, 43, color.NRGBA{G: 190, A: 255}), 61, 43},
		{"webp_lossy", "image/webp", b04AvatarFixture(t, "lossy.webp"), 32, 24},
		{"webp_lossless", "image/webp", b04AvatarFixture(t, "lossless.webp"), 32, 16},
		{"webp_extended_alpha_metadata", "image/webp", b04AvatarFixture(t, "extended.webp"), 32, 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]byte(nil), tc.raw...)
			result, err := prepareAvatarImage(tc.raw, tc.mime)
			if err != nil {
				t.Fatal(err)
			}
			decoded, format, err := image.Decode(bytes.NewReader(result.jpeg))
			if err != nil || format != "jpeg" || decoded.Bounds().Dx() != tc.w || decoded.Bounds().Dy() != tc.h || result.width != tc.w || result.height != tc.h {
				t.Fatal("wrong re-encoded image", format, err)
			}
			if result.originalDigest != digest(before) || !bytes.Equal(before, tc.raw) || !completeAvatarJPEG(result.jpeg) {
				t.Fatal("raw input semantic digest/bytes or output container changed")
			}
			for _, metadata := range []string{"Exif", "B04", "ICC", "XMP"} {
				if bytes.Contains(result.jpeg, []byte(metadata)) {
					t.Fatal("input metadata copied into output")
				}
			}
			if tc.name == "progressive_multiscan" && bytes.Count(tc.raw, []byte{0xff, 0xda}) < 2 {
				t.Fatal("fixture no longer contains multiple scans")
			}
		})
	}
}

func TestB04AvatarPNGFullContainerCRCAndAnimation(t *testing.T) {
	raw := b04AvatarPNG(t, 12, 9, color.NRGBA{B: 255, A: 255})
	for _, kind := range []string{"acTL", "fcTL", "fdAT"} {
		t.Run(kind, func(t *testing.T) { b04AvatarReject(t, b04PNGInsert(raw, kind, make([]byte, 26)), "image/png") })
	}
	metadata := []byte("Description\x00private B04 EXIF ICC XMP filename")
	withMetadata := b04PNGInsert(raw, "tEXt", metadata)
	plain, err := prepareAvatarImage(raw, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	decorated, err := prepareAvatarImage(withMetadata, "image/png")
	if err != nil || !bytes.Equal(plain.jpeg, decorated.jpeg) || plain.originalDigest == decorated.originalDigest {
		t.Fatal("stripped metadata must still distinguish original-byte semantics", err)
	}
	badCRC := append([]byte(nil), withMetadata...)
	badCRC[33+8] ^= 1
	b04AvatarReject(t, badCRC, "image/png")
	for _, bad := range [][]byte{
		append(append([]byte(nil), raw...), 0),
		append(append([]byte(nil), raw...), raw...),
		raw[:len(raw)-1], raw[:len(raw)-12],
		b04PNGInsert(raw, "IHDR", raw[16:29]),
		b04PNGInsert(raw, "CUST", []byte{1}),
		b04PNGInsert(raw, "texT", []byte{1}),
	} {
		b04AvatarReject(t, bad, "image/png")
	}
	badLength := append([]byte(nil), raw...)
	binary.BigEndian.PutUint32(badLength[8:12], math.MaxUint32)
	b04AvatarReject(t, badLength, "image/png")
	// A standard decoder accepting a first image is deliberately insufficient.
	trailing := append(append([]byte(nil), raw...), []byte("unconsumed tail")...)
	if _, err := png.Decode(bytes.NewReader(trailing)); err != nil {
		t.Fatal("counterexample no longer exercises decoder's ignored tail", err)
	}
	b04AvatarReject(t, trailing, "image/png")
	// Consecutive IDAT splitting is legal; ancillary data between them is not.
	pos := 33
	for string(raw[pos+4:pos+8]) != "IDAT" {
		pos += 12 + int(binary.BigEndian.Uint32(raw[pos:pos+4]))
	}
	n := int(binary.BigEndian.Uint32(raw[pos : pos+4]))
	data := raw[pos+8 : pos+8+n]
	head := append([]byte(nil), raw[:pos]...)
	head = append(head, b04PNGChunk("IDAT", data[:len(data)/2])...)
	tail := append(b04PNGChunk("IDAT", data[len(data)/2:]), raw[pos+12+n:]...)
	joined := append(append([]byte(nil), head...), tail...)
	if _, err := prepareAvatarImage(joined, "image/png"); err != nil {
		t.Fatal("legal split IDAT rejected", err)
	}
	interrupted := append(append(append([]byte(nil), head...), b04PNGChunk("tEXt", metadata)...), tail...)
	b04AvatarReject(t, interrupted, "image/png")
}

func TestB04AvatarJPEGRealMarkersAndMultipleScans(t *testing.T) {
	raw := b04AvatarJPEG(t)
	// False EOI/SOS/SOI bytes in APP metadata are not structural markers.
	metadata := append([]byte("Exif\x00\x00B04 private metadata"), 0xff, 0xd9, 0xff, 0xda, 0xff, 0xd8)
	withMetadata := b04JPEGSegment(raw, 0xe1, metadata)
	if _, err := prepareAvatarImage(withMetadata, "image/jpeg"); err != nil {
		t.Fatal("length-delimited marker bytes treated as end of image", err)
	}
	progressive := b04AvatarFixture(t, "progressive.jpg")
	for _, valid := range [][]byte{raw, progressive, withMetadata} {
		for _, bad := range [][]byte{
			append(append([]byte(nil), valid...), 0),
			append(append([]byte(nil), valid...), raw...),
			valid[:len(valid)-1], valid[:len(valid)-2],
			append([]byte{0xff, 0xd8, 0xff, 0xd8}, valid[2:]...),
		} {
			b04AvatarReject(t, bad, "image/jpeg")
		}
	}
	badLength := append([]byte(nil), withMetadata...)
	badLength[4], badLength[5] = 0, 1
	b04AvatarReject(t, badLength, "image/jpeg")
	badLength[4], badLength[5] = 0xff, 0xff
	b04AvatarReject(t, badLength, "image/jpeg")
	// Unit-level entropy syntax confirms stuffed FF and restart markers do not
	// terminate a scan. The real progressive fixture covers decoding semantics.
	entropy := []byte{1, 0xff, 0x00, 2, 0xff, 0xd0, 3, 0xff, 0xff, 0xc4}
	if got := avatarJPEGNextMarker(entropy, 0); got != 7 {
		t.Fatalf("wrong entropy marker boundary: %d", got)
	}
	if avatarJPEGNextMarker([]byte{1, 0xff}, 0) != -1 {
		t.Fatal("truncated entropy accepted")
	}
}

func TestB04AvatarWebPCompleteRIFFRejectsAnimationAndExtraImages(t *testing.T) {
	raw := b04AvatarFixture(t, "lossy.webp")
	extended := b04AvatarFixture(t, "extended.webp")
	for _, kind := range []string{"ANIM", "ANMF"} {
		b04AvatarReject(t, b04WebPAppend(raw, kind, make([]byte, 16)), "image/webp")
	}
	animationFlag := append([]byte(nil), extended...)
	animationFlag[20] |= 2
	b04AvatarReject(t, animationFlag, "image/webp")
	for _, source := range [][]byte{raw, extended, b04AvatarFixture(t, "lossless.webp")} {
		for _, bad := range [][]byte{
			append(append([]byte(nil), source...), 0),
			append(append([]byte(nil), source...), raw...),
			source[:len(source)-1], source[:11],
		} {
			b04AvatarReject(t, bad, "image/webp")
		}
	}
	n := int(binary.LittleEndian.Uint32(raw[16:20]))
	duplicate := b04WebPAppend(raw, "VP8 ", raw[20:20+n])
	if _, _, err := image.Decode(bytes.NewReader(duplicate)); err != nil {
		t.Fatal("counterexample no longer decodes only the first WebP image", err)
	}
	b04AvatarReject(t, duplicate, "image/webp")
	badLength := append([]byte(nil), raw...)
	binary.LittleEndian.PutUint32(badLength[16:20], math.MaxUint32)
	b04AvatarReject(t, badLength, "image/webp")
	padded := b04WebPAppend(raw, "TEST", []byte{1})
	if _, err := prepareAvatarImage(padded, "image/webp"); err != nil {
		t.Fatal("legal ignored chunk/padding rejected", err)
	}
	padded[len(padded)-1] = 1
	b04AvatarReject(t, padded, "image/webp")
	canvasMismatch := append([]byte(nil), extended...)
	canvasMismatch[24]++
	b04AvatarReject(t, canvasMismatch, "image/webp")
	missingMetadataFlag := append([]byte(nil), extended...)
	missingMetadataFlag[20] &^= 0x08
	b04AvatarReject(t, missingMetadataFlag, "image/webp")
	for _, flags := range []byte{0x01, 0x40, 0x80} {
		reserved := append([]byte(nil), extended...)
		reserved[20] |= flags
		b04AvatarReject(t, reserved, "image/webp")
	}
}

func TestB04AvatarLimitsMIMEAndBeforeDecodeDimensions(t *testing.T) {
	pngBytes := b04AvatarPNG(t, 1, 1, color.White)
	jpegBytes := b04AvatarJPEG(t)
	webpBytes := b04AvatarFixture(t, "lossless.webp")
	for _, input := range []struct {
		raw   []byte
		media string
	}{
		{pngBytes, "image/jpeg"}, {jpegBytes, "image/png"}, {webpBytes, "image/png"},
		{[]byte("<svg/>"), "image/svg+xml"}, {[]byte("GIF89a"), "image/gif"},
		{pngBytes, "image/png;filename=private.png"}, {pngBytes, "IMAGE/PNG"},
		{nil, "image/png"}, {make([]byte, avatarInputLimit+1), "image/png"},
	} {
		b04AvatarReject(t, input.raw, input.media)
	}
	for _, dimensions := range [][2]int{{0, 1}, {1, 0}, {-1, 1}, {1, -1}, {4097, 1}, {1, 4097}, {math.MaxInt, math.MaxInt}} {
		if validAvatarDimensions(dimensions[0], dimensions[1]) {
			t.Fatal("out-of-bound dimensions accepted")
		}
	}
	if !validAvatarDimensions(4096, 4096) {
		t.Fatal("maximum dimension and pixel boundary rejected")
	}
	// Structurally valid huge headers with tiny payloads must fail before decode
	// allocates a pixel buffer; do not rely on a decompression error afterward.
	hugePNG := append([]byte(nil), pngBytes...)
	binary.BigEndian.PutUint32(hugePNG[16:20], math.MaxUint32)
	binary.BigEndian.PutUint32(hugePNG[29:33], crc32.ChecksumIEEE(hugePNG[12:29]))
	if completeAvatarPNG(hugePNG) {
		t.Fatal("PNG oversized header passed the container bound")
	}
	b04AvatarReject(t, hugePNG, "image/png")
	hugeWebP := append([]byte(nil), webpBytes...)
	bits := binary.LittleEndian.Uint32(hugeWebP[21:25])
	bits = bits&^0x3fff | 4096 // VP8L width becomes 4097.
	binary.LittleEndian.PutUint32(hugeWebP[21:25], bits)
	if completeAvatarWebP(hugeWebP) {
		t.Fatal("WebP oversized bitstream header passed the container bound")
	}
	b04AvatarReject(t, hugeWebP, "image/webp")
	// A legal chunk can bring the complete input to the exact compressed cap.
	paddingSize := avatarInputLimit - len(pngBytes) - 12
	exact := b04PNGInsert(pngBytes, "tEXt", append([]byte("note\x00"), bytes.Repeat([]byte{'a'}, paddingSize-5)...))
	if len(exact) != avatarInputLimit {
		t.Fatal("compressed boundary fixture length")
	}
	if _, err := prepareAvatarImage(exact, "image/png"); err != nil {
		t.Fatal("exact compressed-byte cap rejected", err)
	}
}

func TestB04AvatarFitWhiteBackgroundAndJPEGQuality(t *testing.T) {
	for _, size := range [][4]int{{600, 300, 512, 256}, {300, 600, 256, 512}, {512, 512, 512, 512}, {9, 7, 9, 7}, {4096, 1, 512, 1}, {1, 4096, 1, 512}, {4096, 4096, 512, 512}} {
		raw := b04AvatarPNG(t, size[0], size[1], color.NRGBA{R: 255, A: 0})
		result, err := prepareAvatarImage(raw, "image/png")
		if err != nil || result.width != size[2] || result.height != size[3] {
			t.Fatal("fit dimensions", size, err)
		}
		decoded, err := jpeg.Decode(bytes.NewReader(result.jpeg))
		if err != nil {
			t.Fatal(err)
		}
		r, g, b, a := decoded.At(size[2]/2, size[3]/2).RGBA()
		if r < 0xf500 || g < 0xf500 || b < 0xf500 || a != 0xffff {
			t.Fatal("transparent input was not composited on opaque white")
		}
	}
	raw := b04AvatarPNG(t, 17, 13, color.NRGBA{R: 120, G: 70, B: 230, A: 255})
	got, err := prepareAvatarImage(raw, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	opaque := image.NewRGBA(image.Rect(0, 0, 17, 13))
	draw.Draw(opaque, opaque.Bounds(), image.NewUniform(color.RGBA{R: 120, G: 70, B: 230, A: 255}), image.Point{}, draw.Src)
	var expected bytes.Buffer
	if err := jpeg.Encode(&expected, opaque, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.jpeg, expected.Bytes()) {
		t.Fatal("output did not use the fixed JPEG85 encoding")
	}
}

// The PNG decoder's pixel completion is not the compressed stream boundary.
func TestB04AvatarPNGExactZlibStream(t *testing.T) {
	compress := func(payload []byte) []byte {
		var b bytes.Buffer
		w := zlib.NewWriter(&b)
		if _, e := w.Write(payload); e != nil {
			t.Fatal(e)
		}
		if e := w.Close(); e != nil {
			t.Fatal(e)
		}
		return b.Bytes()
	}
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header[:4], 1)
	binary.BigEndian.PutUint32(header[4:8], 1)
	header[8] = 8
	header[9] = 6
	stream := compress([]byte{0, 20, 40, 60, 255})
	build := func(h []byte, parts ...[]byte) []byte {
		raw := append([]byte("\x89PNG\r\n\x1a\n"), b04PNGChunk("IHDR", h)...)
		for _, part := range parts {
			raw = append(raw, b04PNGChunk("IDAT", part)...)
		}
		raw = append(raw, b04PNGChunk("tEXt", []byte("note\x00after pixels"))...)
		return append(raw, b04PNGChunk("IEND", nil)...)
	}
	for split := 0; split <= len(stream); split++ {
		for _, interlace := range []byte{0, 1} {
			h := append([]byte(nil), header...)
			h[12] = interlace
			raw := build(h, nil, stream[:split], nil, stream[split:], nil)
			if _, e := prepareAvatarImage(raw, "image/png"); e != nil {
				t.Fatalf("legal split=%d interlace=%d: %v", split, interlace, e)
			}
		}
	}
	for _, tail := range [][]byte{[]byte("garbage"), stream, compress(nil)} {
		b04AvatarReject(t, build(header, stream, tail), "image/png")
		b04AvatarReject(t, build(header, append(append([]byte(nil), stream...), tail...)), "image/png")
	}
	b04AvatarReject(t, build(header, stream[:len(stream)-1]), "image/png")
	broken := append([]byte(nil), stream...)
	broken[len(broken)-1] ^= 1
	b04AvatarReject(t, build(header, broken), "image/png")
	b04AvatarReject(t, build(header, compress([]byte{0, 20, 40, 60, 255, 0})), "image/png")
}
