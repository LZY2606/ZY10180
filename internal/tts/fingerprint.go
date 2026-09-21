package tts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Fingerprint computes the deterministic run fingerprint. Everything that
// influences analysis output is folded in: seed, reference temperature,
// consistency tolerance, raw points, manual shifts and user exclusions.
// Floats are rendered with strconv 'g'/12 so identical inputs always hash
// identically, and any change (including the tolerance) changes the hash.
func Fingerprint(r *Run) string {
	var b strings.Builder
	fmt.Fprintf(&b, "seed=%d\n", r.Seed)
	fmt.Fprintf(&b, "ref_temp_c=%s\n", ftoa(r.RefTempC))
	fmt.Fprintf(&b, "tolerance=%s\n", ftoa(r.Tolerance))
	curves := append([]Curve(nil), r.Curves...)
	sort.Slice(curves, func(i, j int) bool { return curves[i].ID < curves[j].ID })
	for _, c := range curves {
		fmt.Fprintf(&b, "curve id=%d label=%q temp_c=%s\n", c.ID, c.Label, ftoa(c.TempC))
		pts := append([]Point(nil), c.Points...)
		sort.Slice(pts, func(i, j int) bool { return pts[i].ID < pts[j].ID })
		for _, p := range pts {
			fmt.Fprintf(&b, "point id=%d freq=%s g1=%s g2=%s phase=%s\n",
				p.ID, ftoa(p.Freq), ftoa(p.G1), ftoa(p.G2), ftoa(p.PhaseDeg))
		}
	}
	shifts := append([]Shift(nil), r.Shifts...)
	sort.Slice(shifts, func(i, j int) bool { return shifts[i].CurveID < shifts[j].CurveID })
	for _, s := range shifts {
		fmt.Fprintf(&b, "shift curve=%d loga=%s logb=%s vert=%t locked=%t\n",
			s.CurveID, ftoa(s.LogA), ftoa(s.LogB), s.UseVertical, s.Locked)
	}
	ex := append([]UserExclusion(nil), r.Exclusions...)
	sort.Slice(ex, func(i, j int) bool { return ex[i].PointID < ex[j].PointID })
	for _, e := range ex {
		fmt.Fprintf(&b, "exclusion point=%d reason=%q\n", e.PointID, e.Reason)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// ShortFingerprint is the display form used in the UI.
func ShortFingerprint(r *Run) string {
	fp := Fingerprint(r)
	if len(fp) > 12 {
		return fp[:12]
	}
	return fp
}

func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'g', 12, 64)
}
