package account_test

import (
	"errors"
	"image"
	"image/color"
	"math"
)

// This is a test-only public-image matcher, not an account answer port. It
// searches all integer rotations against pixels already visible to a user.
// Geometry follows the pinned GoCaptcha CropScaleCircle/Rotate implementation:
// central crop, expanded rotation canvas, pixel centers, and overCrop's +1.
// Unlike the former radius-45 sampler it observes the entire opaque disc.
type publicRotation struct {
	Angle   int
	Score   float64
	Texture float64
}

type publicImage struct {
	w, h int
	rgba []float64
}

func publicPixels(im image.Image) publicImage {
	b := im.Bounds()
	p := publicImage{w: b.Dx(), h: b.Dy(), rgba: make([]float64, b.Dx()*b.Dy()*4)}
	for y := range p.h {
		for x := range p.w {
			v := color.NRGBAModel.Convert(im.At(x+b.Min.X, y+b.Min.Y)).(color.NRGBA)
			i := (y*p.w + x) * 4
			p.rgba[i], p.rgba[i+1], p.rgba[i+2], p.rgba[i+3] = float64(v.R), float64(v.G), float64(v.B), float64(v.A)
		}
	}
	return p
}

func solvePublicRotation(master, thumb image.Image) (publicRotation, error) {
	m, t := publicPixels(master), publicPixels(thumb)
	if m.w != 220 || m.h != 220 || t.w < 140 || t.w > 170 || t.h != t.w {
		return publicRotation{}, errors.New("invalid public puzzle dimensions")
	}
	type point struct {
		x, y float64
		rgb  [3]float64
	}
	var points []point
	var sum, squares [3]float64
	center, radius := float64(t.w)/2-1, float64(t.w)/2-4
	for y := 4; y < t.h-4; y += 4 {
		for x := 4; x < t.w-4; x += 4 {
			if dx, dy := float64(x)-center, float64(y)-center; dx*dx+dy*dy > radius*radius {
				continue
			}
			i := (y*t.w + x) * 4
			if t.rgba[i+3] < 250 {
				continue
			}
			p := point{x: float64(x), y: float64(y)}
			for k := range 3 {
				p.rgb[k] = t.rgba[i+k]
				sum[k] += p.rgb[k]
				squares[k] += p.rgb[k] * p.rgb[k]
			}
			points = append(points, p)
		}
	}
	if len(points) < 128 {
		return publicRotation{}, errors.New("insufficient opaque public pixels")
	}
	texture := 0.0
	for k := range 3 {
		texture += squares[k]/float64(len(points)) - math.Pow(sum[k]/float64(len(points)), 2)
	}
	if texture < 1 {
		return publicRotation{}, errors.New("public puzzle has no directional texture")
	}
	best := publicRotation{Score: math.Inf(1), Texture: texture}
	for rotation := range 360 {
		a := float64(rotation) * math.Pi / 180
		co, si := math.Cos(a), math.Sin(a)
		w, h := publicRotatedSize(t.w, co, si)
		hx := float64(w)/2 - float64((w-t.w)/2)
		hy := float64(h)/2 - float64((h-t.h)/2)
		crop := float64((m.w - t.w) / 2)
		errorSum, count := 0.0, 0
		for _, p := range points {
			sx, sy := p.x+crop, p.y+crop
			if rotation != 0 {
				dx, dy := p.x+1.5-hx, p.y+1.5-hy
				sx = co*dx + si*dy + hx - .5 + crop
				sy = -si*dx + co*dy + hy - .5 + crop
			}
			x, y := int(math.Floor(sx)), int(math.Floor(sy))
			if x < 0 || y < 0 || x+1 >= m.w || y+1 >= m.h {
				continue
			}
			fx, fy := sx-float64(x), sy-float64(y)
			i := (y*m.w + x) * 4
			if m.rgba[i+3] < 250 || m.rgba[i+7] < 250 || m.rgba[i+4*m.w+3] < 250 || m.rgba[i+4*m.w+7] < 250 {
				continue
			}
			for k := range 3 {
				upper := m.rgba[i+k]*(1-fx) + m.rgba[i+4+k]*fx
				lower := m.rgba[i+4*m.w+k]*(1-fx) + m.rgba[i+4*m.w+4+k]*fx
				d := upper*(1-fy) + lower*fy - p.rgb[k]
				errorSum += d * d
			}
			count++
		}
		if count == len(points) && errorSum/float64(count) < best.Score {
			best.Angle = (360 - rotation) % 360
			best.Score = errorSum / float64(count)
		}
	}
	if math.IsInf(best.Score, 0) {
		return publicRotation{}, errors.New("public geometry could not be matched")
	}
	return best, nil
}

func publicRotatedSize(size int, co, si float64) (int, int) {
	d := float64(size - 1)
	xs := []float64{0, d * co, d*co - d*si, -d * si}
	ys := []float64{0, d * si, d*si + d*co, d * co}
	span := func(values []float64) int {
		lo, hi := values[0], values[0]
		for _, v := range values[1:] {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
		width := hi - lo + 1
		if width-math.Floor(width) > .1 {
			width++
		}
		return int(width)
	}
	return span(xs), span(ys)
}
