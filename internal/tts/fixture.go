package tts

import (
	"math"
	"math/rand"
)

// True shift factors baked into the fixture (log10 aT per temperature).
// They are only used to synthesise physically consistent curves; the
// workbench itself never sees them.
var fixtureTrueLogA = map[float64]float64{
	25: 0.0,
	40: 0.9,
	60: 1.8,
}

// maxwell evaluates a two-mode Maxwell model at angular frequency w.
func maxwell(w float64) (g1, g2 float64) {
	modes := [][2]float64{
		{1.0e4, 1.0},  // G=10 kPa, tau=1 s
		{3.0e3, 0.05}, // G=3 kPa, tau=50 ms
	}
	for _, m := range modes {
		x := w * m[1]
		g1 += m[0] * x * x / (1 + x*x)
		g2 += m[0] * x / (1 + x*x)
	}
	return g1, g2
}

// GenerateFixture deterministically builds the three-temperature demo
// dataset from a seed:
//   - 25 C and 40 C sweeps share an overlapping frequency band;
//   - the 60 C sweep sits decades away and overlaps nothing;
//   - one point carries a non-positive storage modulus;
//   - one point carries a phase angle inconsistent with G'/G”.
//
// Replaying the same seed yields byte-identical data.
func GenerateFixture(seed int64) []Curve {
	rng := rand.New(rand.NewSource(seed))
	type spec struct {
		label    string
		tempC    float64
		fLo, fHi float64
		n        int
	}
	specs := []spec{
		{"T25", 25, 0.1, 100, 12},
		{"T40", 40, 1, 1000, 12},
		{"T60", 60, 1e4, 1e6, 12},
	}
	var curves []Curve
	pointID := int64(1)
	for ci, sp := range specs {
		curve := Curve{ID: int64(ci + 1), Label: sp.label, TempC: sp.tempC}
		shift := fixtureTrueLogA[sp.tempC]
		for i := 0; i < sp.n; i++ {
			f := sp.fLo * math.Pow(sp.fHi/sp.fLo, float64(i)/float64(sp.n-1))
			// Evaluate the reference model at the thermally shifted frequency.
			g1, g2 := maxwell(f * math.Pow(10, shift))
			// Small deterministic measurement noise on the moduli.
			g1 *= math.Pow(10, 0.01*rng.NormFloat64())
			g2 *= math.Pow(10, 0.01*rng.NormFloat64())
			// Phase noise is applied in tan(delta) space so the
			// phase/modulus relation stays consistent by construction.
			tanDelta := g2 / g1 * (1 + 0.005*rng.NormFloat64())
			phase := math.Atan(tanDelta) * 180 / math.Pi
			curve.Points = append(curve.Points, Point{
				ID: pointID, Freq: f, G1: g1, G2: g2, PhaseDeg: phase,
			})
			pointID++
		}
		curves = append(curves, curve)
	}
	// Defect 1: a non-positive storage modulus on the 40 C curve.
	bad := &curves[1].Points[3]
	bad.G1 = -math.Abs(bad.G1)
	// Defect 2: a phase angle inconsistent with G''/G' on the 25 C curve.
	curves[0].Points[5].PhaseDeg = 6.0
	return curves
}

// NewRun builds a fresh run from the fixture with default settings.
func NewRun(seed int64) *Run {
	r := &Run{
		Seed:      seed,
		RefTempC:  25,
		Tolerance: 0.15,
		Curves:    GenerateFixture(seed),
	}
	for _, c := range r.Curves {
		r.Shifts = append(r.Shifts, Shift{CurveID: c.ID})
	}
	return r
}
