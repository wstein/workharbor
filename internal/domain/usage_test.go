package domain

import (
	"math"
	"testing"
)

func TestSatAddSaturates(t *testing.T) {
	if got := SatAdd(1, 2, 3); got != 6 {
		t.Errorf("SatAdd(1,2,3) = %d", got)
	}
	if got := SatAdd(math.MaxInt64, 1); got != math.MaxInt64 {
		t.Errorf("overflow wrapped to %d", got)
	}
	if got := SatAdd(math.MaxInt64/2+1, math.MaxInt64/2+1); got != math.MaxInt64 {
		t.Errorf("overflow wrapped to %d", got)
	}
	if got := SatAdd(math.MinInt64, -1); got != math.MinInt64 {
		t.Errorf("underflow wrapped to %d", got)
	}
}
