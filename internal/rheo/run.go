package rheo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Run executes one deterministic TTS analysis.
func Run(curves []Curve, points map[int64][]Point, states map[int64]CurveState, seed int64, tolDeg float64, refID int64) RunResult {
	res := RunResult{Seed: seed, TolDeg: tolDeg, RefCurveID: refID}
	sorted := append([]Curve(nil), curves...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].TempC < sorted[j].TempC })

	// 1) Exclusions: user-flagged plus consistency auto-checks.
	excluded := map[int64]bool{}
	for _, c := range sorted {
		for _, p := range points[c.ID] {
			if p.UserExcluded {
				excluded[p.ID] = true
				continue
			}
			if reason := CheckConsistency(p, tolDeg); reason != "" {
				excluded[p.ID] = true
				res.Exclusions = append(res.Exclusions, Exclusion{PointID: p.ID, Reason: reason})
			}
		}
	}
	sort.Slice(res.Exclusions, func(i, j int) bool { return res.Exclusions[i].PointID < res.Exclusions[j].PointID })

	// 2) Log-space series per curve.
	ser := map[int64]series{}
	for _, c := range sorted {
		ser[c.ID] = toSeries(points[c.ID], excluded)
	}

	// 3) Known shifts: reference = 0, locked curves = their manual value.
	known := map[int64]float64{refID: 0}
	origin := map[int64]string{refID: "参考温度"}
	for _, c := range sorted {
		st := states[c.ID]
		if st.Locked && c.ID != refID {
			known[c.ID] = st.ManualH
			origin[c.ID] = "锁定（手工值）"
		}
	}
	// Propagate along adjacent temperatures until no progress.
	for progress := true; progress; {
		progress = false
		for i := 0; i < len(sorted)-1; i++ {
			a, b := sorted[i], sorted[i+1]
			if _, okA := known[a.ID]; okA {
				if _, okB := known[b.ID]; !okB {
					if s, _, _, ok := SolveShift(ser[a.ID], ser[b.ID], known[a.ID]); ok {
						known[b.ID] = s
						origin[b.ID] = "重叠区求解"
						progress = true
					}
				}
			} else if kb, okB := known[b.ID]; okB {
				if s, _, _, ok := SolveShift(ser[b.ID], ser[a.ID], kb); ok {
					known[a.ID] = s
					origin[a.ID] = "重叠区求解"
					progress = true
				}
			}
		}
	}

	// 4) Per-curve shift results; unidentifiable curves get a range only.
	var gMin, gMax float64
	first := true
	for _, c := range sorted {
		if s, ok := known[c.ID]; ok {
			lo, hi := ser[c.ID].min()+s, ser[c.ID].max()+s
			if first || lo < gMin {
				gMin = lo
			}
			if first || hi > gMax {
				gMax = hi
			}
			first = false
		}
	}
	for _, c := range sorted {
		sr := ShiftResult{CurveID: c.ID}
		if s, ok := known[c.ID]; ok {
			sr.SolvedH = s
			sr.SolvedOK = true
			sr.Identifiable = true
			sr.Note = origin[c.ID]
		} else {
			// Range in which the curve would just touch the known group.
			sr.RangeLo = gMin - ser[c.ID].max()
			sr.RangeHi = gMax - ser[c.ID].min()
			sr.Identifiable = false
			sr.Note = fmt.Sprintf("无重叠频段：移位不可识别，仅范围 [%.3f, %.3f]", sr.RangeLo, sr.RangeHi)
		}
		res.Shifts = append(res.Shifts, sr)
	}

	// 5) Adjacent-pair overlap errors at effective shifts.
	eff := EffectiveShifts(sorted, states, res.Shifts)
	for i := 0; i < len(sorted)-1; i++ {
		a, b := sorted[i], sorted[i+1]
		sA, okA := eff[a.ID]
		sB, okB := eff[b.ID]
		pe := PairError{AID: a.ID, BID: b.ID}
		if !okA || !okB {
			pe.Message = "存在不可识别移位，跳过重叠区误差"
		} else {
			pe = PairOverlapError(ser[a.ID], ser[b.ID], sA, sB, a.ID, b.ID)
		}
		res.Pairs = append(res.Pairs, pe)
	}

	// 6) Model fits on identifiable knowns (reference, locked, solved).
	var ks []knownShift
	tempOf := map[int64]float64{}
	for _, c := range sorted {
		tempOf[c.ID] = c.TempC
		if s, ok := known[c.ID]; ok {
			ks = append(ks, knownShift{tempC: c.TempC, logA: s})
		}
	}
	tref := tempOf[refID]
	wlf := FitWLF(ks, tref)
	arr := FitArrhenius(ks, tref)
	res.Fits = []FitResult{wlf, arr}

	// 7) Predictions for every curve; residuals against known effective
	// shifts (manual values stay untouched, models never overwrite them).
	for _, c := range sorted {
		st := states[c.ID]
		knownVal, hasKnown := known[c.ID]
		if st.ManualHSet {
			knownVal, hasKnown = st.ManualH, true
		}
		for _, f := range res.Fits {
			if !f.OK {
				continue
			}
			var pred float64
			if f.Model == "wlf" {
				pred = f.PredictWLF(c.TempC)
			} else {
				pred = f.PredictArrhenius(c.TempC)
			}
			pr := Prediction{CurveID: c.ID, Model: f.Model, Pred: pred}
			if hasKnown {
				pr.HasResidual = true
				pr.Residual = knownVal - pred
			}
			res.Predictions = append(res.Predictions, pr)
		}
	}

	res.Fingerprint = fingerprint(res, states, points)
	return res
}

// EffectiveShifts picks the display shift per curve: manual value when set,
// otherwise the solved value. Model predictions are never substituted.
func EffectiveShifts(sorted []Curve, states map[int64]CurveState, shifts []ShiftResult) map[int64]float64 {
	solved := map[int64]float64{}
	for _, sr := range shifts {
		if sr.SolvedOK {
			solved[sr.CurveID] = sr.SolvedH
		}
	}
	eff := map[int64]float64{}
	for _, c := range sorted {
		st := states[c.ID]
		if st.ManualHSet {
			eff[c.ID] = st.ManualH
		} else if s, ok := solved[c.ID]; ok {
			eff[c.ID] = s
		}
	}
	return eff
}

// fingerprint hashes the full run configuration and results.
func fingerprint(res RunResult, states map[int64]CurveState, points map[int64][]Point) string {
	type stateRec struct {
		CurveID int64   `json:"c"`
		ManualH float64 `json:"h"`
		Set     bool    `json:"s"`
		ManualV float64 `json:"v"`
		Locked  bool    `json:"l"`
	}
	var statesList []stateRec
	for _, st := range states {
		statesList = append(statesList, stateRec{st.CurveID, st.ManualH, st.ManualHSet, st.ManualV, st.Locked})
	}
	sort.Slice(statesList, func(i, j int) bool { return statesList[i].CurveID < statesList[j].CurveID })
	type userEx struct {
		PointID int64  `json:"p"`
		Reason  string `json:"r"`
	}
	var uex []userEx
	for _, pts := range points {
		for _, p := range pts {
			if p.UserExcluded {
				uex = append(uex, userEx{p.ID, p.UserReason})
			}
		}
	}
	sort.Slice(uex, func(i, j int) bool { return uex[i].PointID < uex[j].PointID })
	canonical := struct {
		Seed    int64      `json:"seed"`
		TolDeg  float64    `json:"tol_deg"`
		RefID   int64      `json:"ref"`
		States  []stateRec `json:"states"`
		UserEx  []userEx   `json:"user_exclusions"`
		Results RunResult  `json:"results"`
	}{res.Seed, res.TolDeg, res.RefCurveID, statesList, uex, RunResult{
		Seed: res.Seed, TolDeg: res.TolDeg, RefCurveID: res.RefCurveID,
		Shifts: res.Shifts, Fits: res.Fits, Predictions: res.Predictions,
		Pairs: res.Pairs, Exclusions: res.Exclusions,
	}}
	buf, _ := json.Marshal(canonical)
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}
