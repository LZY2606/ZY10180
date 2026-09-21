package rheo

import (
	"fmt"
	"math"
	"math/rand"
)

// Fixture is a fixed, seed-deterministic set of frequency sweeps.
type Fixture struct {
	Seed     int64          `json:"seed"`
	RefLabel string         `json:"ref_label"`
	Curves   []FixtureCurve `json:"curves"`
}

// FixtureCurve is one sweep inside a fixture.
type FixtureCurve struct {
	Label  string      `json:"label"`
	TempC  float64     `json:"temp_c"`
	Points [][]float64 `json:"points"` // each: [freqHz, G1, G2, phaseDeg]
}

// masterG returns the noise-free master curve (log10 Pa) at log10 frequency x.
func masterG(x float64) (g1, g2 float64) {
	g1 = 2.0 + 1.8/(1.0+math.Exp(-(x-1.0))) + 0.15*x
	g2 = g1 - 0.3 - 0.1*math.Cos(x)
	return g1, g2
}

// trueShifts maps fixture temperature (C) to its true log10 aT.
var trueShifts = map[float64]float64{30: 2.2, 50: 0.0, 110: -7.0}

// GenerateFixture builds the deterministic three-temperature fixture:
// 30C/50C share a wide overlap band, 110C has no overlap with either,
// and one non-positive modulus point is injected on the 110C curve.
func GenerateFixture(seed int64) Fixture {
	rng := rand.New(rand.NewSource(seed))
	f := Fixture{Seed: seed, RefLabel: "50C"}
	temps := []float64{30, 50, 110}
	for _, t := range temps {
		fc := FixtureCurve{Label: fmt.Sprintf("%.0fC", t), TempC: t}
		shift := trueShifts[t]
		for i := 0; i < 25; i++ {
			x := -2.0 + 6.0*float64(i)/24.0 // log10 freq, -2..4
			freq := math.Pow(10, x)
			g1, g2 := masterG(x + shift)
			n1 := 1 + 0.01*(rng.Float64()-0.5)*2
			n2 := 1 + 0.01*(rng.Float64()-0.5)*2
			G1 := math.Pow(10, g1) * n1
			G2 := math.Pow(10, g2) * n2
			phase := math.Atan2(G2, G1) * 180 / math.Pi
			phase += (rng.Float64() - 0.5) * 1.0 // +-0.5 deg noise
			fc.Points = append(fc.Points, []float64{freq, G1, G2, phase})
		}
		f.Curves = append(f.Curves, fc)
	}
	// Inject defects deterministically (appended after the rng stream).
	// 1) Non-positive loss modulus on the 110C curve, lowest frequency.
	f.Curves[2].Points[0][2] = -12.5
	// 2) Phase angle inconsistent with G'/G'' on the 30C curve (10 deg off).
	f.Curves[0].Points[10][3] += 10.0
	return f
}
