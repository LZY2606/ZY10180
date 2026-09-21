package rheo

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func loadFixture(t *testing.T) Fixture {
	t.Helper()
	raw, err := os.ReadFile("../../fixtures/sweeps.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fx Fixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return fx
}

// Replay: regenerating with the same seed reproduces the committed fixture.
func TestFixtureReplayDeterministic(t *testing.T) {
	want, err := os.ReadFile("../../fixtures/sweeps.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	got, err := json.MarshalIndent(GenerateFixture(42), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if string(got) != string(want) {
		t.Fatalf("fixture regeneration mismatch: same seed must replay identical bytes")
	}
	other := GenerateFixture(43)
	if len(other.Curves) != 3 {
		t.Fatalf("want 3 curves, got %d", len(other.Curves))
	}
}

func fixtureInputs(fx Fixture) ([]Curve, map[int64][]Point, map[int64]CurveState, int64) {
	var curves []Curve
	points := map[int64][]Point{}
	states := map[int64]CurveState{}
	var refID int64
	for i, fc := range fx.Curves {
		c := Curve{ID: int64(i + 1), Label: fc.Label, TempC: fc.TempC, Sort: i}
		curves = append(curves, c)
		if fc.Label == fx.RefLabel {
			refID = c.ID
		}
		states[c.ID] = CurveState{CurveID: c.ID}
		for j, p := range fc.Points {
			points[c.ID] = append(points[c.ID], Point{
				ID: int64(i*1000 + j + 1), CurveID: c.ID,
				FreqHz: p[0], G1: p[1], G2: p[2], PhaseDeg: p[3],
			})
		}
	}
	return curves, points, states, refID
}

func shiftOf(res RunResult, id int64) ShiftResult {
	for _, s := range res.Shifts {
		if s.CurveID == id {
			return s
		}
	}
	return ShiftResult{}
}

// The overlapping pair gets a precise solved shift; the isolated curve only
// gets a range plus an unidentifiable diagnostic.
func TestRunShiftIdentifiability(t *testing.T) {
	fx := loadFixture(t)
	curves, points, states, refID := fixtureInputs(fx)
	res := Run(curves, points, states, fx.Seed, 2.0, refID)

	s30 := shiftOf(res, 1)
	if !s30.Identifiable || !s30.SolvedOK {
		t.Fatalf("30C shift must be identifiable, got %+v", s30)
	}
	if math.Abs(s30.SolvedH-2.2) > 0.05 {
		t.Fatalf("30C solved shift = %.4f, want ~2.2", s30.SolvedH)
	}
	s50 := shiftOf(res, 2)
	if !s50.Identifiable || s50.SolvedH != 0 {
		t.Fatalf("reference shift must be exactly 0, got %+v", s50)
	}
	s110 := shiftOf(res, 3)
	if s110.Identifiable || s110.SolvedOK {
		t.Fatalf("110C shift must NOT be identifiable, got %+v", s110)
	}
	if !(s110.RangeLo < s110.RangeHi) {
		t.Fatalf("110C must carry a range, got [%.3f, %.3f]", s110.RangeLo, s110.RangeHi)
	}
	if s110.Note == "" {
		t.Fatalf("110C must carry an unidentifiable diagnostic")
	}
}

// Consistency: the non-positive modulus point and the phase-inconsistent
// point are excluded with queryable reasons.
func TestRunExclusionsHaveReasons(t *testing.T) {
	fx := loadFixture(t)
	curves, points, states, refID := fixtureInputs(fx)
	res := Run(curves, points, states, fx.Seed, 2.0, refID)
	if len(res.Exclusions) != 2 {
		t.Fatalf("want 2 auto exclusions, got %d: %+v", len(res.Exclusions), res.Exclusions)
	}
	for _, ex := range res.Exclusions {
		if ex.Reason == "" {
			t.Fatalf("exclusion for point %d lacks a reason", ex.PointID)
		}
	}
	// Tighter tolerance must exclude more points, never fewer.
	res2 := Run(curves, points, states, fx.Seed, 0.1, refID)
	if len(res2.Exclusions) < len(res.Exclusions) {
		t.Fatalf("tighter tolerance excluded fewer points: %d < %d", len(res2.Exclusions), len(res.Exclusions))
	}
}

// Replay: identical configuration reproduces identical fingerprints; the
// tolerance participates in the fingerprint.
func TestRunFingerprintReplay(t *testing.T) {
	fx := loadFixture(t)
	curves, points, states, refID := fixtureInputs(fx)
	a := Run(curves, points, states, fx.Seed, 2.0, refID)
	b := Run(curves, points, states, fx.Seed, 2.0, refID)
	if a.Fingerprint != b.Fingerprint {
		t.Fatalf("same seed must replay identical fingerprint:\n%s\n%s", a.Fingerprint, b.Fingerprint)
	}
	c := Run(curves, points, states, fx.Seed, 3.0, refID)
	if c.Fingerprint == a.Fingerprint {
		t.Fatalf("tolerance must participate in the run fingerprint")
	}
	d := Run(curves, points, states, 99, 2.0, refID)
	if d.Fingerprint == a.Fingerprint {
		t.Fatalf("seed must participate in the run fingerprint")
	}
}

// Models: with default data WLF is underdetermined (reported, not
// extrapolated); Arrhenius is identified. Locking the isolated curve with a
// manual value makes WLF identifiable, and manual values are never
// overwritten by model predictions.
func TestModelFits(t *testing.T) {
	fx := loadFixture(t)
	curves, points, states, refID := fixtureInputs(fx)
	res := Run(curves, points, states, fx.Seed, 2.0, refID)
	if res.Fits[0].OK {
		t.Fatalf("WLF must be unidentifiable with a single non-reference point")
	}
	if !res.Fits[1].OK {
		t.Fatalf("Arrhenius must be identifiable: %s", res.Fits[1].Note)
	}
	// Manual value on the isolated curve stays manual; residual is separate.
	st := states[3]
	st.ManualH, st.ManualHSet, st.Locked = -7.0, true, true
	states[3] = st
	res2 := Run(curves, points, states, fx.Seed, 2.0, refID)
	if !res2.Fits[0].OK {
		t.Fatalf("WLF must fit once the third curve is locked: %s", res2.Fits[0].Note)
	}
	s110 := shiftOf(res2, 3)
	if s110.SolvedH != -7.0 {
		t.Fatalf("locked manual shift must stay -7.0, got %.4f", s110.SolvedH)
	}
	var found bool
	for _, p := range res2.Predictions {
		if p.CurveID == 3 && p.Model == "wlf" {
			found = true
			if !p.HasResidual {
				t.Fatalf("locked curve must carry a WLF residual")
			}
			if math.Abs(p.Residual-(-7.0-p.Pred)) > 1e-9 {
				t.Fatalf("residual must be manual minus prediction")
			}
		}
	}
	if !found {
		t.Fatalf("missing WLF prediction for curve 3")
	}
}

// WLF/Arrhenius recover known ground truth from synthetic shifts.
func TestFitGroundTruth(t *testing.T) {
	// Synthetic shifts generated from a true WLF law (C1=8.86, C2=101.6).
	wlfOf := func(tempC float64) float64 {
		dt := tempC - 30
		return -8.86 * dt / (101.6 + dt)
	}
	ks := []knownShift{{30, 0}, {50, wlfOf(50)}, {70, wlfOf(70)}}
	w := FitWLF(ks, 30)
	if !w.OK {
		t.Fatalf("WLF fit failed: %s", w.Note)
	}
	if math.Abs(w.Params["C1"]-8.86) > 1e-6 || math.Abs(w.Params["C2"]-101.6) > 1e-6 {
		t.Fatalf("WLF params off: %+v", w.Params)
	}
	for _, k := range ks[1:] {
		if math.Abs(w.PredictWLF(k.tempC)-k.logA) > 1e-9 {
			t.Fatalf("WLF prediction off at %.0fC", k.tempC)
		}
	}
	// Synthetic shifts generated from a true Arrhenius law (Ea=80 kJ/mol).
	kArr := 80000.0 / (8.314462618 * math.Log(10))
	arrOf := func(tempC float64) float64 {
		return kArr * (1/(tempC+273.15) - 1/(30+273.15))
	}
	ks = []knownShift{{30, 0}, {50, arrOf(50)}, {70, arrOf(70)}}
	a := FitArrhenius(ks, 30)
	if !a.OK {
		t.Fatalf("Arrhenius fit failed: %s", a.Note)
	}
	if math.Abs(a.Params["EaJmol"]-80000) > 1 {
		t.Fatalf("Arrhenius Ea off: %+v", a.Params)
	}
	for _, k := range ks[1:] {
		if math.Abs(a.PredictArrhenius(k.tempC)-k.logA) > 1e-9 {
			t.Fatalf("Arrhenius prediction off at %.0fC: got %.4f want %.4f",
				k.tempC, a.PredictArrhenius(k.tempC), k.logA)
		}
	}
}

// CheckConsistency: non-positive moduli and frequencies are rejected with reasons.
func TestCheckConsistency(t *testing.T) {
	if r := CheckConsistency(Point{FreqHz: 0, G1: 1, G2: 1}, 2); r == "" {
		t.Fatal("zero frequency must be rejected")
	}
	if r := CheckConsistency(Point{FreqHz: 1, G1: -1, G2: 1}, 2); r == "" {
		t.Fatal("non-positive modulus must be rejected")
	}
	p := Point{FreqHz: 1, G1: 100, G2: 50, PhaseDeg: math.Atan2(50, 100) * 180 / math.Pi}
	if r := CheckConsistency(p, 2); r != "" {
		t.Fatalf("consistent point rejected: %s", r)
	}
	p.PhaseDeg += 10
	if r := CheckConsistency(p, 2); r == "" {
		t.Fatal("phase-inconsistent point must be rejected")
	}
}
