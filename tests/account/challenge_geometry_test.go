package account_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/wenlng/go-captcha/v2/rotate"
)

func geometryImage(t *testing.T, name string) image.Image {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "challenge", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return im
}

// These masters are unchanged public images from the original fixed-64 red
// run. Its answers were not retained. New deterministic rotations below have
// explicit test-owned oracles; they are not claims about the old private answer.
func TestPublicRotationKnownGeometry(t *testing.T) {
	type encodedImage struct {
		Width  int    `json:"width"`
		Height int    `json:"height"`
		RGBA   string `json:"rgba"`
	}
	type vector struct {
		Name   string       `json:"name"`
		Master encodedImage `json:"master"`
		Thumb  encodedImage `json:"thumb"`
		Want   int          `json:"want"`
		Go     int          `json:"go"`
	}
	encode := func(im image.Image) encodedImage {
		p := publicPixels(im)
		b := make([]byte, len(p.rgba))
		for i, v := range p.rgba {
			b[i] = byte(v)
		}
		return encodedImage{p.w, p.h, base64.StdEncoding.EncodeToString(b)}
	}
	var vectors []vector
	for _, number := range []int{39, 44, 58} {
		master := geometryImage(t, fmt.Sprintf("original-%d-master.png", number))
		for _, angle := range []int{30, 31, 44, 89, 90, 91, 135, 180, 197, 225, 270, 329, 330} {
			name := fmt.Sprintf("public-master-%d/known-rotation-%d", number, angle)
			t.Run(name, func(t *testing.T) {
				thumb, err := rotate.NewDrawImage().DrawWithCropCircle(&rotate.DrawCropCircleImageParams{Background: master, Rotate: angle, SquareSize: 160, ScaleRatioSize: 30, Alpha: 1})
				if err != nil {
					t.Fatal(err)
				}
				got, err := solvePublicRotation(master, thumb)
				if err != nil || !rotate.Validate(got.Angle, angle, 5) {
					t.Fatalf("known public geometry mismatch: result=%+v error=%v", got, err)
				}
				if os.Getenv("ACCOUNT_SOLVER_CORPUS_OUT") != "" {
					vectors = append(vectors, vector{name, encode(master), encode(thumb), (360 - angle) % 360, got.Angle})
				}
			})
		}
	}
	if path := os.Getenv("ACCOUNT_SOLVER_CORPUS_OUT"); path != "" {
		b, err := json.Marshal(vectors)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append(b, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPublicRotationOriginalCounterexamples(t *testing.T) {
	for _, number := range []int{39, 44, 58} {
		master := geometryImage(t, fmt.Sprintf("original-%d-master.png", number))
		thumb := geometryImage(t, fmt.Sprintf("original-%d-thumb.png", number))
		got, err := solvePublicRotation(master, thumb)
		if err != nil || got.Score > 4 {
			t.Fatalf("public alignment %d: result=%+v error=%v", number, got, err)
		}
		// The original central-only outputs 0 and 4 are outside every possible
		// answer for the pinned 30..330 rotation range. The old oracle is lost;
		// this asserts observable pixel alignment, not server validation.
		if number != 58 && (got.Angle == 0 || got.Angle == 4) {
			t.Fatal("still selected a flat central-disc minimum")
		}
	}
}

func TestPublicRotationRejectsUnobservableGeometry(t *testing.T) {
	m := image.NewNRGBA(image.Rect(0, 0, 220, 220))
	for y := range 220 {
		for x := range 220 {
			m.SetNRGBA(x, y, color.NRGBA{120, 120, 120, 255})
		}
	}
	thumb, err := rotate.NewDrawImage().DrawWithCropCircle(&rotate.DrawCropCircleImageParams{Background: m, Rotate: 90, SquareSize: 160, ScaleRatioSize: 30, Alpha: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = solvePublicRotation(m, thumb); err == nil {
		t.Fatal("a uniform disc cannot supply a trustworthy answer")
	}
	if _, err = solvePublicRotation(m, image.NewNRGBA(image.Rect(0, 0, 160, 160))); err == nil {
		t.Fatal("transparent image was accepted")
	}
}
