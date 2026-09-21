package tts

import (
	"math"
	"strings"
	"testing"
)

func TestFixtureDeterministic(t *testing.T) {
	a := GenerateFixture(1183)
	b := GenerateFixture(1183)
	if len(a) != 3 || len(b) != 3 {
		t.Fatalf("expected 3 curves, got %d/%d", len(a), len(b))
	}
	for i := range a {
		for j := range a[i].Points {
			if a[i].Points[j] != b[i].Points[j] {
				t.Fatalf("curve %d point %d differs between replays", i, j)
			}
		}
	}
	c := GenerateFixture(7)
	if a[0].Points[0].G1 == c[0].Points[0].G1 {
		t.Fatal("different seeds must produce different data")
	}
}

func TestFixtureShape(t *testing.T) {
	cs := GenerateFixture(1183)
	// One pair overlaps, the third curve does not.
	freqRange := func(c Curve) (float64, float64) {
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, p := range c.Points {
			lo = math.Min(lo, p.Freq)
			hi = math.Max(hi, p.Freq)
		}
		return lo, hi
	}
	lo0, hi0 := freqRange(cs[0])
	lo1, hi1 := freqRange(cs[1])
	lo2, hi2 := freqRange(cs[2])
	if math.Min(hi0, hi1) <= math.Max(lo0, lo1) {
		t.Fatal("T25/T40 must share an overlapping band")
	}
	if math.Min(hi0, hi2) > math.Max(lo0, lo2) || math.Min(hi1, hi2) > math.Max(lo1, lo2) {
		t.Fatal("T60 must not overlap any other curve")
	}
	// Exactly one non-positive modulus point.
	bad := 0
	for _, c := range cs {
		for _, p := range c.Points {
			if p.G1 <= 0 || p.G2 <= 0 {
				bad++
			}
		}
	}
	if bad != 1 {
		t.Fatalf("expected exactly 1 non-positive modulus point, got %d", bad)
	}
}

func TestAutoExclusions(t *testing.T) {
	r := NewRun(1183)
	ex := r.ExcludedPoints()
	var nonPos, inconsistent bool
	for _, e := range ex {
		if e.Source != SourceAuto {
			t.Fatalf("fresh run must only have auto exclusions, got %q", e.Source)
		}
		if strings.Contains(e.Reason, "模量非正") {
			nonPos = true
		}
		if strings.Contains(e.Reason, "相位角与模量不一致") {
			inconsistent = true
		}
		if e.Reason == "" {
			t.Fatal("every excluded point must carry a reason")
		}
	}
	if !nonPos {
		t.Fatal("non-positive modulus point must be auto-excluded")
	}
	if !inconsistent {
		t.Fatal("phase-inconsistent point must be auto-excluded")
	}
}

func TestFrequencyMustBePositive(t *testing.T) {
	p := Point{ID: 1, Freq: 0, G1: 100, G2: 50, PhaseDeg: 26.5}
	if reason := AutoExclusionReason(p, 0.15); !strings.Contains(reason, "频率非正") {
		t.Fatalf("freq=0 must be rejected, got %q", reason)
	}
	p.Freq = -3
	if reason := AutoExclusionReason(p, 0.15); reason == "" {
		t.Fatal("negative frequency must be rejected")
	}
}

func TestUnidentifiableShiftIsRangeOnly(t *testing.T) {
	r := NewRun(1183)
	var t60 Curve
	for _, c := range r.Curves {
		if c.TempC == 60 {
			t60 = c
		}
	}
	a := r.AssessShift(t60)
	if a.Identifiable {
		t.Fatal("T60 has no overlapping band: shift must be unidentifiable")
	}
	if !(a.RangeLo < a.RangeHi) {
		t.Fatalf("unidentifiable shift must still provide a range, got [%v,%v]", a.RangeLo, a.RangeHi)
	}
	// T60 band is 1e4..1e6, reference band 0.1..100: range must be [-7,-2].
	if math.Abs(a.RangeLo-(-7)) > 1e-9 || math.Abs(a.RangeHi-(-2)) > 1e-9 {
		t.Fatalf("unexpected range [%v,%v], want [-7,-2]", a.RangeLo, a.RangeHi)
	}
	if !strings.Contains(a.Message, "不可识别") {
		t.Fatalf("diagnostic must state unidentifiability, got %q", a.Message)
	}
}

func TestIdentifiableShiftSuggestion(t *testing.T) {
	r := NewRun(1183)
	var t40 Curve
	for _, c := range r.Curves {
		if c.TempC == 40 {
			t40 = c
		}
	}
	a := r.AssessShift(t40)
	if !a.Identifiable {
		t.Fatal("T40 overlaps the reference and must be identifiable")
	}
	// The fixture embeds a true shift of +0.9 decades.
	if math.Abs(a.SuggestedLogA-0.9) > 0.05 {
		t.Fatalf("suggested shift %v, want near 0.9", a.SuggestedLogA)
	}
}

func TestOverlapErrorDropsAtTrueShift(t *testing.T) {
	r := NewRun(1183)
	overlaps := r.AdjacentOverlaps()
	if len(overlaps) != 2 {
		t.Fatalf("expected 2 adjacent pairs, got %d", len(overlaps))
	}
	if !overlaps[0].OK {
		t.Fatal("T25/T40 pair must overlap")
	}
	if overlaps[1].OK {
		t.Fatal("T40/T60 pair must not overlap")
	}
	before := overlaps[0]
	// Apply the true shift of +0.9 to T40: error must shrink.
	for i := range r.Shifts {
		if r.Shifts[i].CurveID == 2 {
			r.Shifts[i].LogA = 0.9
		}
	}
	after := r.AdjacentOverlaps()[0]
	if !(after.RMSG1 < before.RMSG1 && after.RMSG2 < before.RMSG2) {
		t.Fatalf("true shift should reduce overlap error: before=(%v,%v) after=(%v,%v)",
			before.RMSG1, before.RMSG2, after.RMSG1, after.RMSG2)
	}
}

func TestWLFFitRecoversParameters(t *testing.T) {
	ref := 25.0
	c1, c2 := 8.86, 101.6
	var samples []TempShift
	for _, temp := range []float64{25, 40, 55, 70} {
		dt := temp - ref
		samples = append(samples, TempShift{TempC: temp, LogA: -c1 * dt / (c2 + dt)})
	}
	fit := FitWLF(samples, ref)
	if !fit.OK {
		t.Fatal("WLF fit failed")
	}
	if math.Abs(fit.C1-c1) > 0.05 || math.Abs(fit.C2-c2) > 1.0 {
		t.Fatalf("WLF recovered C1=%v C2=%v, want %v/%v", fit.C1, fit.C2, c1, c2)
	}
}

func TestArrheniusFitRecoversEa(t *testing.T) {
	ref := 25.0
	ea := 80e3
	var samples []TempShift
	for _, temp := range []float64{25, 40, 60, 80} {
		x := 1/(temp+273.15) - 1/(ref+273.15)
		samples = append(samples, TempShift{TempC: temp, LogA: ea / (gasConstant * math.Log(10)) * x})
	}
	fit := FitArrhenius(samples, ref)
	if !fit.OK {
		t.Fatal("Arrhenius fit failed")
	}
	if math.Abs(fit.EaJPerMol-ea)/ea > 1e-6 {
		t.Fatalf("recovered Ea=%v, want %v", fit.EaJPerMol, ea)
	}
}

func TestModelNeverOverwritesManualShift(t *testing.T) {
	r := NewRun(1183)
	r.Shifts[1].LogA = 0.33 // manual value
	wlf := FitWLF(r.FittingSamples(), r.RefTempC)
	if !wlf.OK {
		t.Fatal("fit should succeed")
	}
	_ = r.Residuals(wlf.Predict)
	if r.Shifts[1].LogA != 0.33 {
		t.Fatal("model fitting must not overwrite manual shifts")
	}
	res := r.Residuals(wlf.Predict)
	found := false
	for _, m := range res {
		if m.CurveID == 2 {
			found = true
			if m.Manual != 0.33 {
				t.Fatalf("residual table must keep manual value 0.33, got %v", m.Manual)
			}
			if math.Abs(m.Residual-(m.Manual-m.Predicted)) > 1e-12 {
				t.Fatal("residual must equal manual - predicted")
			}
		}
	}
	if !found {
		t.Fatal("residual table must cover curve 2")
	}
}

func TestToleranceParticipatesInFingerprint(t *testing.T) {
	r := NewRun(1183)
	fp1 := Fingerprint(r)
	r.Tolerance = 0.20
	fp2 := Fingerprint(r)
	if fp1 == fp2 {
		t.Fatal("changing tolerance must change the fingerprint")
	}
}

func TestFingerprintReplayDeterministic(t *testing.T) {
	a := NewRun(1183)
	b := NewRun(1183)
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatal("same seed must yield the same fingerprint")
	}
	c := NewRun(42)
	if Fingerprint(a) == Fingerprint(c) {
		t.Fatal("different seeds must yield different fingerprints")
	}
}

func TestLockedCurveExcludedFromFit(t *testing.T) {
	r := NewRun(1183)
	r.Shifts[1].Locked = true
	for _, s := range r.FittingSamples() {
		if s.Label == "T40" {
			t.Fatal("locked curve must not enter fitting samples")
		}
	}
	// Reference anchor is always present.
	foundRef := false
	for _, s := range r.FittingSamples() {
		if s.Label == "T25" && s.LogA == 0 {
			foundRef = true
		}
	}
	if !foundRef {
		t.Fatal("reference anchor (Tref, 0) must be part of fitting samples")
	}
}
