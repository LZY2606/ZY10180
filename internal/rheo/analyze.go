package rheo

import (
	"fmt"
	"math"
	"sort"
)

// series is one curve reduced to log-space samples.
type series struct {
	x  []float64 // log10 freq
	y1 []float64 // log10 G'
	y2 []float64 // log10 G''
}

// toSeries keeps only plottable points (freq>0, moduli>0, not excluded).
func toSeries(pts []Point, excluded map[int64]bool) series {
	var s series
	for _, p := range pts {
		if excluded[p.ID] || p.FreqHz <= 0 || p.G1 <= 0 || p.G2 <= 0 {
			continue
		}
		s.x = append(s.x, math.Log10(p.FreqHz))
		s.y1 = append(s.y1, math.Log10(p.G1))
		s.y2 = append(s.y2, math.Log10(p.G2))
	}
	return s
}

func (s series) min() float64 {
	if len(s.x) == 0 {
		return 0
	}
	m := s.x[0]
	for _, v := range s.x {
		if v < m {
			m = v
		}
	}
	return m
}

func (s series) max() float64 {
	if len(s.x) == 0 {
		return 0
	}
	m := s.x[0]
	for _, v := range s.x {
		if v > m {
			m = v
		}
	}
	return m
}

// interp linearly interpolates y(x); x must lie inside the sampled range.
func interp(xs, ys []float64, x float64) float64 {
	n := len(xs)
	if x <= xs[0] {
		return ys[0]
	}
	if x >= xs[n-1] {
		return ys[n-1]
	}
	i := sort.Search(n, func(i int) bool { return xs[i] >= x })
	if xs[i] == x {
		return ys[i]
	}
	t := (x - xs[i-1]) / (xs[i] - xs[i-1])
	return ys[i-1] + t*(ys[i]-ys[i-1])
}

// overlapBracket returns the shift interval (for the free curve b) in which
// the two shifted ranges intersect, given a is already shifted by sA.
func overlapBracket(a, b series, sA float64) (lo, hi float64, ok bool) {
	lo = a.min() + sA - b.max()
	hi = a.max() + sA - b.min()
	return lo, hi, lo < hi
}

// rmsAt computes the combined G'/G” RMS difference over the overlap region
// with b shifted by s and a shifted by sA.
func rmsAt(a, b series, sA, s float64) (float64, int) {
	lo := math.Max(a.min()+sA, b.min()+s)
	hi := math.Min(a.max()+sA, b.max()+s)
	if hi <= lo {
		return math.NaN(), 0
	}
	var sum float64
	var n int
	// accum samples curve (xs,ys) shifted by s1 against curve (ox,oy) shifted by s2.
	accum := func(xs, ys []float64, s1 float64, ox, oy []float64, s2 float64) {
		for i, x := range xs {
			xs_ := x + s1
			if xs_ < lo || xs_ > hi {
				continue
			}
			d := ys[i] - interp(ox, oy, xs_-s2)
			sum += d * d
			n++
		}
	}
	// b sampled against a, and a sampled against b, for both moduli.
	accum(b.x, b.y1, s, a.x, a.y1, sA)
	accum(b.x, b.y2, s, a.x, a.y2, sA)
	accum(a.x, a.y1, sA, b.x, b.y1, s)
	accum(a.x, a.y2, sA, b.x, b.y2, s)
	if n < 3 {
		return math.NaN(), n
	}
	return math.Sqrt(sum / float64(n)), n
}

// SolveShift finds the horizontal shift of free curve b against curve a
// (already shifted by sA) by golden-section minimisation of the overlap RMS.
// ok is false when the two curves share no overlapping frequency band, or
// when the minimum sits on the bracket boundary (extrapolation, not data).
func SolveShift(a, b series, sA float64) (s float64, rms float64, n int, ok bool) {
	blo, bhi, has := overlapBracket(a, b, sA)
	if !has {
		return 0, 0, 0, false
	}
	lo, hi := blo, bhi
	const gr = 0.6180339887498949
	c := hi - gr*(hi-lo)
	d := lo + gr*(hi-lo)
	fc, _ := rmsAt(a, b, sA, c)
	fd, _ := rmsAt(a, b, sA, d)
	for i := 0; i < 200; i++ {
		if fc < fd {
			hi = d
			d, fd = c, fc
			c = hi - gr*(hi-lo)
			fc, _ = rmsAt(a, b, sA, c)
		} else {
			lo = c
			c, fc = d, fd
			d = lo + gr*(hi-lo)
			fd, _ = rmsAt(a, b, sA, d)
		}
	}
	s = 0.5 * (lo + hi)
	rms, n = rmsAt(a, b, sA, s)
	if math.IsNaN(rms) {
		return 0, 0, n, false
	}
	// Reject boundary solutions: without interior overlap the shift would be
	// an extrapolation, not an identifiable measurement.
	span := bhi - blo
	if s-blo < 0.02*span || bhi-s < 0.02*span {
		return 0, 0, n, false
	}
	// Require a substantial overlap at the solution: at least half a decade
	// wide and at least three samples from each curve.
	olo := math.Max(a.min()+sA, b.min()+s)
	ohi := math.Min(a.max()+sA, b.max()+s)
	if ohi-olo < 0.5 {
		return 0, 0, n, false
	}
	var na, nb int
	for _, x := range a.x {
		if x+sA >= olo && x+sA <= ohi {
			na++
		}
	}
	for _, x := range b.x {
		if x+s >= olo && x+s <= ohi {
			nb++
		}
	}
	if na < 3 || nb < 3 {
		return 0, 0, n, false
	}
	return s, rms, n, true
}

// PairOverlapError evaluates adjacent-pair agreement at the given shifts.
func PairOverlapError(a, b series, sA, sB float64, aID, bID int64) PairError {
	pe := PairError{AID: aID, BID: bID}
	rms, n := rmsAt(a, b, sA, sB)
	if math.IsNaN(rms) {
		lo, hi, _ := overlapBracket(a, b, sA)
		pe.Overlap = false
		pe.Message = fmt.Sprintf("无重叠频段：移位不可识别，仅给出可拼接范围 [%.3f, %.3f]，不含精确解", lo, hi)
		return pe
	}
	pe.Overlap = true
	pe.RMS = rms
	pe.N = n
	pe.Message = fmt.Sprintf("重叠区 RMS=%.4f（%d 个样本）", rms, n)
	return pe
}
