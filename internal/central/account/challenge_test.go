package account

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/png"
	"strings"
	"testing"
)

func TestChallengeGeneratedArtworkIsBoundedAndDecodable(t *testing.T) {
	master, thumb, angle, e := generateChallenge()
	if e != nil {
		t.Fatal(e)
	}
	for i, raw := range []string{master, thumb} {
		encoded := raw
		if i := strings.Index(raw, ","); i >= 0 {
			encoded = raw[i+1:]
		}
		b, e := base64.StdEncoding.DecodeString(encoded)
		if e != nil {
			t.Fatal(e)
		}
		cfg, _, e := image.DecodeConfig(bytes.NewReader(b))
		if e != nil {
			t.Fatal(e)
		}
		want := []int{220, 160}[i]
		if cfg.Width != want || cfg.Height != want || len(b) > 256<<10 {
			t.Fatal("unexpected generated image dimensions or size")
		}
	}
	if angle < 0 || angle > 360 {
		t.Fatal("invalid library angle")
	}
}
