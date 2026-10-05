package account

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wenlng/go-captcha/v2/base/imagedata"
	"github.com/wenlng/go-captcha/v2/base/option"
	"github.com/wenlng/go-captcha/v2/rotate"
)

func decodeChallengeArtwork(t *testing.T, encoded string) (image.Image, []byte) {
	t.Helper()
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(encoded, prefix) {
		t.Fatal("missing PNG data prefix")
	}
	raw, e := base64.StdEncoding.Strict().DecodeString(encoded[len(prefix):])
	if e != nil {
		t.Fatal(e)
	}
	img, e := png.Decode(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	return img, raw
}

func checkChallengePixels(t *testing.T, actual, original image.Image, padded bool) {
	t.Helper()
	b := original.Bounds()
	for y := 0; y < actual.Bounds().Dy(); y++ {
		for x := 0; x < actual.Bounds().Dx(); x++ {
			r, g, bl, a := actual.At(x, y).RGBA()
			if padded && (x == 159 || y == 159) {
				if a != 0 {
					t.Fatalf("missing edge was not transparent at %d,%d", x, y)
				}
				continue
			}
			wr, wg, wb, wa := original.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if r != wr || g != wg || bl != wb || a != wa {
				t.Fatalf("encoded pixel moved or changed at %d,%d", x, y)
			}
		}
	}
}

func TestChallengeArtworkFixedSDKAngles(t *testing.T) {
	// Same fixed public input as the independent SDK-only diagnostic. The
	// random crop origin has no effect on this image; all nine angles are fixed.
	src := image.NewRGBA(image.Rect(0, 0, 512, 512))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.RGBA{80, 120, 160, 255}), image.Point{}, draw.Src)
	artifactDir := os.Getenv("AGENTEAM_ARTWORK_TEST_OUTPUT") // test evidence only
	if artifactDir != "" {
		if e := os.Mkdir(artifactDir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	type picture struct {
		File   string `json:"file"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		SHA256 string `json:"sha256"`
	}
	save := func(name string, raw []byte) picture {
		t.Helper()
		cfg, e := png.DecodeConfig(bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		h := sha256.Sum256(raw)
		if artifactDir != "" {
			if e = os.WriteFile(filepath.Join(artifactDir, name), raw, 0600); e != nil {
				t.Fatal(e)
			}
		}
		return picture{name, cfg.Width, cfg.Height, hex.EncodeToString(h[:])}
	}
	var input bytes.Buffer
	if e := png.Encode(&input, src); e != nil {
		t.Fatal(e)
	}
	inputPicture := save("input.png", input.Bytes())
	type observation struct {
		Angle      int     `json:"fixed_angle"`
		SDKBounds  string  `json:"sdk_thumb_bounds"`
		Master     picture `json:"master"`
		Thumb      picture `json:"thumb"`
		WasAdapted bool    `json:"was_adapted"`
	}
	var observations []observation
	for _, angle := range []int{89, 90, 91, 179, 180, 181, 269, 270, 271} {
		t.Run(fmt.Sprint(angle), func(t *testing.T) {
			b := rotate.NewBuilder()
			b.SetOptions(rotate.WithImageSquareSize(220), rotate.WithRangeThumbImageSquareSize([]int{160}), rotate.WithRangeAnglePos([]option.RangeVal{{Min: angle, Max: angle}}))
			b.SetResources(rotate.WithImages([]image.Image{src}))
			v, e := b.Make().Generate()
			if e != nil {
				t.Fatal(e)
			}
			if v.GetData().Angle != angle {
				t.Fatal("fixed SDK angle changed")
			}
			masterBefore, e := v.GetMasterImage().ToBase64()
			if e != nil {
				t.Fatal(e)
			}
			thumbBefore, e := v.GetThumbImage().ToBase64()
			if e != nil {
				t.Fatal(e)
			}
			beforeBounds := v.GetThumbImage().Get().Bounds()
			adapted := angle == 90 || angle == 180 || angle == 270
			if adapted && beforeBounds != image.Rect(1, 1, 160, 160) {
				t.Fatalf("known SDK bounds changed: %v", beforeBounds)
			}
			master, thumb, gotAngle, e := encodeChallengeArtwork(v.GetMasterImage(), v.GetThumbImage(), v.GetData().Angle)
			if e != nil || gotAngle != angle || master != masterBefore || (!adapted && thumb != thumbBefore) {
				t.Fatal("artwork encoding changed normal data/angle or failed", e)
			}
			m, mb := decodeChallengeArtwork(t, master)
			th, tb := decodeChallengeArtwork(t, thumb)
			if m.Bounds() != image.Rect(0, 0, 220, 220) || th.Bounds() != image.Rect(0, 0, 160, 160) || len(master)+len(thumb) > 256<<10 {
				t.Fatal("actual encoded dimensions/size")
			}
			checkChallengePixels(t, m, v.GetMasterImage().Get(), false)
			checkChallengePixels(t, th, v.GetThumbImage().Get(), adapted)
			after, e := v.GetThumbImage().ToBase64()
			if e != nil || after != thumbBefore || v.GetThumbImage().Get().Bounds() != beforeBounds || v.GetData().Angle != angle {
				t.Fatal("adapter mutated SDK data")
			}
			observations = append(observations, observation{angle, beforeBounds.String(), save(fmt.Sprintf("%03d-master.png", angle), mb), save(fmt.Sprintf("%03d-thumb.png", angle), tb), adapted})
		})
	}
	if artifactDir != "" {
		raw, e := json.MarshalIndent(struct {
			Input picture       `json:"input"`
			Cases []observation `json:"cases"`
		}{inputPicture, observations}, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(artifactDir, "observations.json"), append(raw, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
}

func TestChallengeArtworkPreservesCropOriginAndRejectsOtherShapes(t *testing.T) {
	master := imagedata.NewPNGImageData(image.NewNRGBA(image.Rect(0, 0, 220, 220)))
	original := image.NewNRGBA(image.Rect(1, 1, 160, 160))
	for y := 1; y < 160; y++ {
		for x := 1; x < 160; x++ {
			original.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	for _, angle := range []int{90, 180, 270} {
		_, encoded, got, e := encodeChallengeArtwork(master, imagedata.NewPNGImageData(original), angle)
		if e != nil || got != angle {
			t.Fatal("exact known crop rejected", e)
		}
		decoded, _ := decodeChallengeArtwork(t, encoded)
		checkChallengePixels(t, decoded, original, true)
	}
	for _, tc := range []struct {
		name   string
		bounds image.Rectangle
		angle  int
	}{
		{"wrong-angle", original.Bounds(), 89},
		{"wrong-angle-after", original.Bounds(), 271},
		{"zero-origin", image.Rect(0, 0, 159, 159), 90},
		{"other-origin", image.Rect(2, 2, 161, 161), 180},
		{"one-axis", image.Rect(1, 1, 160, 161), 270},
		{"too-small", image.Rect(1, 1, 159, 159), 90},
		{"too-large", image.Rect(0, 0, 161, 161), 90},
		{"negative-angle", image.Rect(0, 0, 160, 160), -1},
		{"oversized-angle", image.Rect(0, 0, 160, 160), 361},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, th, angle, e := encodeChallengeArtwork(master, imagedata.NewPNGImageData(image.NewNRGBA(tc.bounds)), tc.angle)
			if e == nil || m != "" || th != "" || angle != 0 {
				t.Fatal("unexpected artwork yielded material")
			}
		})
	}
	for _, bad := range []imagedata.PNGImageData{nil, imagedata.NewPNGImageData(nil), imagedata.NewPNGImageData(image.NewNRGBA(image.Rect(0, 0, 219, 220)))} {
		if _, _, _, e := encodeChallengeArtwork(bad, imagedata.NewPNGImageData(original), 90); e == nil {
			t.Fatal("invalid master accepted")
		}
	}
	for _, bad := range []imagedata.PNGImageData{nil, imagedata.NewPNGImageData(nil)} {
		if _, _, _, e := encodeChallengeArtwork(master, bad, 90); e == nil {
			t.Fatal("missing thumbnail accepted")
		}
	}
}

func TestChallengeArtworkKeepsCombinedEncodedLimit(t *testing.T) {
	var state uint32 = 0x8f79d412
	noise := func(size int) imagedata.PNGImageData {
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		for i := range img.Pix {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			img.Pix[i] = byte(state)
		}
		return imagedata.NewPNGImageData(img)
	}
	m, th := noise(220), noise(160)
	mraw, e := m.ToBase64()
	if e != nil {
		t.Fatal(e)
	}
	traw, e := th.ToBase64()
	if e != nil || len(mraw)+len(traw) <= 256<<10 {
		t.Fatal("deterministic size-limit preparation", e)
	}
	master, thumb, angle, e := encodeChallengeArtwork(m, th, 90)
	if e == nil || master != "" || thumb != "" || angle != 0 {
		t.Fatal("oversized combined artwork yielded material")
	}
}
