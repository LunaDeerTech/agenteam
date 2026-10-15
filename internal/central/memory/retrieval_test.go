package memory

import (
	"math"
	"testing"
)

func TestVectorRejectsDimensionMismatchAndNonfiniteOrZeroValues(t *testing.T) {
	for _, v := range []struct {
		vector    []float64
		dimension int
	}{{[]float64{1, 2}, 3}, {[]float64{math.NaN()}, 1}, {[]float64{math.Inf(1)}, 1}, {[]float64{0, 0}, 2}, {[]float64{math.MaxFloat64}, 1}} {
		if _, err := vectorText(v.vector, v.dimension); err == nil {
			t.Fatal("invalid vector accepted")
		}
	}
	text, err := vectorText([]float64{0.2, -1}, 2)
	if err != nil || text != "[0.2,-1]" {
		t.Fatal(text, err)
	}
}
