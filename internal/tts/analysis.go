package tts

import (
	"fmt"
	"math"
	"sort"
)

// Exclusion sources.
const (
	SourceAuto = "auto"
	SourceUser = "user"
)

// ExcludedPoint pairs a point with the reason it cannot enter analysis.
type ExcludedPoint struct {
	PointID int64   `json:"point_id"`
	CurveID int64   `json:"curve_id"`
	Label   string  `json:"label"`
	Freq    float64 `json:"freq"`
	Reason  string  `json:"reason"`
	Source  string  `json:"source"` // "auto" | "user"
}

// AutoExclusionReason returns the validation reason a point is excluded,
// or "" if the point is usable. tol is the relative tolerance of the
// phase/modulus consistency check |tan(delta) - G”/G'| <= tol * tan(delta).
func AutoExclusionReason(p Point, tol float64) string {
	if !(p.Freq > 0) {
		return "频率非正，无法取对数 (freq<=0)"
	}
	if !(p.G1 > 0) || !(p.G2 > 0) {
		return "模量非正，不能进入对数轴 (G'或G''<=0)"
	}
	tanDelta := math.Tan(p.PhaseDeg * math.Pi / 180)
	ratio := p.G2 / p.G1
	if tanDelta <= 0 || math.Abs(tanDelta-ratio) > tol*tanDelta {
		return fmt.Sprintf("相位角与模量不一致 |tanδ-G''/G'|=%.4g 超出容差 %.4g",
			math.Abs(tanDelta-ratio), tol*math.Max(tanDelta, 1e-12))
	}
	return ""
}

// ExcludedPoints lists every excluded point of the run with its reason,
// user exclusions first then auto exclusions, each sorted by point ID.
func (r *Run) ExcludedPoints() []ExcludedPoint {
	user := r.UserExclusionMap()
	var out []ExcludedPoint
	for _, c := range r.Curves {
		for _, p := range c.Points {
			if reason, ok := user[p.ID]; ok {
				out = append(out, ExcludedPoint{p.ID, c.ID, c.Label, p.Freq, reason, SourceUser})
			}
		}
	}
	for _, c := range r.Curves {
		for _, p := range c.Points {
			if _, ok := user[p.ID]; ok {
				continue
			}
			if reason := AutoExclusionReason(p, r.Tolerance); reason != "" {
				out = append(out, ExcludedPoint{p.ID, c.ID, c.Label, p.Freq, reason, SourceAuto})
			}
		}
	}
	return out
}

// includedSet returns the IDs of points usable in analysis.
func (r *Run) includedSet() map[int64]bool {
	user := r.UserExclusionMap()
	in := make(map[int64]bool)
	for _, c := range r.Curves {
		for _, p := range c.Points {
			if _, bad := user[p.ID]; bad {
				continue
			}
			if AutoExclusionReason(p, r.Tolerance) != "" {
				continue
			}
			in[p.ID] = true
		}
	}
	return in
}

// shiftedSeries extracts the reduced log-coordinates of a curve.
// x = log10(w)+LogA, y1/y2 = log10(G)+LogB (if vertical shift enabled).
// Rows are sorted by x.
func shiftedSeries(c Curve, s Shift, included map[int64]bool) (x, y1, y2 []float64) {
	logB := 0.0
	if s.UseVertical {
		logB = s.LogB
	}
	type row struct{ x, a, b float64 }
	var rows []row
	for _, p := range c.Points {
		if !included[p.ID] {
			continue
		}
		rows = append(rows, row{math.Log10(p.Freq) + s.LogA, math.Log10(p.G1) + logB, math.Log10(p.G2) + logB})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].x < rows[j].x })
	for _, r := range rows {
		x = append(x, r.x)
		y1 = append(y1, r.a)
		y2 = append(y2, r.b)
	}
	return x, y1, y2
}

// interp linearly interpolates y(x) at xq; x must be sorted ascending.
// ok is false when xq lies outside [x[0], x[len-1]].
func interp(x, y []float64, xq float64) (float64, bool) {
	n := len(x)
	if n == 0 || xq < x[0] || xq > x[n-1] {
		return 0, false
	}
	if n == 1 || xq == x[n-1] {
		return y[n-1], true
	}
	i := sort.Search(n, func(i int) bool { return x[i] >= xq })
	if i < n && x[i] == xq {
		return y[i], true
	}
	if i == 0 {
		return y[0], true
	}
	t := (xq - x[i-1]) / (x[i] - x[i-1])
	return y[i-1] + t*(y[i]-y[i-1]), true
}

// OverlapError summarises the mismatch of two shifted curves over their
// shared reduced-frequency band.
type OverlapError struct {
	CurveAID       int64   `json:"curve_a_id"`
	CurveBID       int64   `json:"curve_b_id"`
	LabelA         string  `json:"label_a"`
	LabelB         string  `json:"label_b"`
	OK             bool    `json:"ok"`
	OverlapDecades float64 `json:"overlap_decades"`
	Samples        int     `json:"samples"`
	RMSG1          float64 `json:"rms_g1"`
	RMSG2          float64 `json:"rms_g2"`
}

// PairwiseOverlap computes the overlap error between two curves under the
// given shifts. OK is false when the shifted bands do not overlap.
func PairwiseOverlap(a, b Curve, sa, sb Shift, included map[int64]bool) OverlapError {
	res := OverlapError{CurveAID: a.ID, CurveBID: b.ID, LabelA: a.Label, LabelB: b.Label}
	xa, ya1, ya2 := shiftedSeries(a, sa, included)
	xb, yb1, yb2 := shiftedSeries(b, sb, included)
	if len(xa) == 0 || len(xb) == 0 {
		return res
	}
	lo := math.Max(xa[0], xb[0])
	hi := math.Min(xa[len(xa)-1], xb[len(xb)-1])
	if !(hi > lo) {
		return res
	}
	var sum1, sum2 float64
	var n int
	accum := func(x, y1, y2 []float64, xo, yo1, yo2 []float64) {
		for i, xq := range x {
			if xq < lo || xq > hi {
				continue
			}
			o1, ok1 := interp(xo, yo1, xq)
			o2, ok2 := interp(xo, yo2, xq)
			if !ok1 || !ok2 {
				continue
			}
			d1 := y1[i] - o1
			d2 := y2[i] - o2
			sum1 += d1 * d1
			sum2 += d2 * d2
			n++
		}
	}
	accum(xa, ya1, ya2, xb, yb1, yb2)
	accum(xb, yb1, yb2, xa, ya1, ya2)
	if n == 0 {
		return res
	}
	res.OK = true
	res.OverlapDecades = hi - lo
	res.Samples = n
	res.RMSG1 = math.Sqrt(sum1 / float64(n))
	res.RMSG2 = math.Sqrt(sum2 / float64(n))
	return res
}

// AdjacentOverlaps computes overlap errors between temperature-adjacent
// curves using the current manual shifts.
func (r *Run) AdjacentOverlaps() []OverlapError {
	included := r.includedSet()
	curves := append([]Curve(nil), r.Curves...)
	sort.Slice(curves, func(i, j int) bool {
		if curves[i].TempC != curves[j].TempC {
			return curves[i].TempC < curves[j].TempC
		}
		return curves[i].ID < curves[j].ID
	})
	var out []OverlapError
	for i := 0; i+1 < len(curves); i++ {
		a, b := curves[i], curves[i+1]
		out = append(out, PairwiseOverlap(a, b, r.ShiftOf(a.ID), r.ShiftOf(b.ID), included))
	}
	return out
}

// ShiftAssessment describes whether a curve's shift versus the reference
// is identifiable from overlapping data.
type ShiftAssessment struct {
	CurveID       int64   `json:"curve_id"`
	Label         string  `json:"label"`
	IsReference   bool    `json:"is_reference"`
	Identifiable  bool    `json:"identifiable"`
	RangeLo       float64 `json:"range_lo"` // logA bounds that would create any overlap
	RangeHi       float64 `json:"range_hi"`
	SuggestedLogA float64 `json:"suggested_log_a"` // only when Identifiable
	Message       string  `json:"message"`
}

// overlapRMS is the combined G'/G” RMS mismatch at a trial horizontal shift.
func overlapRMS(a, b Curve, sa, sb Shift, included map[int64]bool) (float64, bool) {
	oe := PairwiseOverlap(a, b, sa, sb, included)
	if !oe.OK {
		return 0, false
	}
	return math.Hypot(oe.RMSG1, oe.RMSG2), true
}

// AssessShift evaluates one curve against the reference curve.
func (r *Run) AssessShift(c Curve) ShiftAssessment {
	res := ShiftAssessment{CurveID: c.ID, Label: c.Label, IsReference: r.IsReference(c.ID)}
	if res.IsReference {
		res.Identifiable = true
		res.Message = "参考曲线，移位定义为 0"
		return res
	}
	ref, ok := r.ReferenceCurve()
	if !ok {
		res.Message = "无参考曲线"
		return res
	}
	included := r.includedSet()
	sr := r.ShiftOf(ref.ID)
	sc := r.ShiftOf(c.ID)
	xr, _, _ := shiftedSeries(ref, sr, included)
	xc, _, _ := shiftedSeries(c, sc, included)
	if len(xr) == 0 || len(xc) == 0 {
		res.Message = "有效数据点为空，无法评估"
		return res
	}
	// Bounds of logA for which the shifted band would touch the reference band.
	base := sc.LogA
	res.RangeLo = xr[0] - xc[len(xc)-1]
	res.RangeHi = xr[len(xr)-1] - xc[0]

	oe := PairwiseOverlap(c, ref, sc, sr, included)
	if !oe.OK {
		res.Identifiable = false
		res.Message = fmt.Sprintf("与参考曲线无重叠频段：移位不可识别，仅能提供范围 [%.3f, %.3f]（log10 aT），不做外推估计",
			res.RangeLo, res.RangeHi)
		return res
	}
	res.Identifiable = true
	// Deterministic suggestion: coarse grid + golden-section refinement.
	best, bestV := base, math.Inf(1)
	const gridN = 61
	for i := 0; i < gridN; i++ {
		v := res.RangeLo + (res.RangeHi-res.RangeLo)*float64(i)/float64(gridN-1)
		trial := sc
		trial.LogA = v
		if rms, ok := overlapRMS(c, ref, trial, sr, included); ok && rms < bestV {
			best, bestV = v, rms
		}
	}
	lo, hi := res.RangeLo, res.RangeHi
	gr := (math.Sqrt(5) - 1) / 2
	x1, x2 := hi-gr*(hi-lo), lo+gr*(hi-lo)
	f1 := evalOrInf(c, ref, sr, sc, included, x1)
	f2 := evalOrInf(c, ref, sr, sc, included, x2)
	for i := 0; i < 80; i++ {
		if f1 > f2 {
			lo = x1
			x1, f1 = x2, f2
			x2 = lo + gr*(hi-lo)
			f2 = evalOrInf(c, ref, sr, sc, included, x2)
		} else {
			hi = x2
			x2, f2 = x1, f1
			x1 = hi - gr*(hi-lo)
			f1 = evalOrInf(c, ref, sr, sc, included, x1)
		}
	}
	mid := 0.5 * (lo + hi)
	if v := evalOrInf(c, ref, sr, sc, included, mid); v < bestV {
		best = mid
	}
	res.SuggestedLogA = best
	res.Message = fmt.Sprintf("与参考曲线重叠 %.3f 个数量级，可识别；建议 log10 aT = %.4f（仅供参考，不覆盖手工值）",
		oe.OverlapDecades, best)
	return res
}

func evalOrInf(a, b Curve, sa, sb Shift, included map[int64]bool, logA float64) float64 {
	trial := sb
	trial.LogA = logA
	if rms, ok := overlapRMS(a, b, trial, sa, included); ok {
		return rms
	}
	return math.Inf(1)
}

// Assessments evaluates every curve against the reference.
func (r *Run) Assessments() []ShiftAssessment {
	out := make([]ShiftAssessment, 0, len(r.Curves))
	for _, c := range r.Curves {
		out = append(out, r.AssessShift(c))
	}
	return out
}
