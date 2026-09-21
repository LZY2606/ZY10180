package rheo

import (
	"fmt"
	"math"
)

// knownShift pairs a temperature (C) with a known log10 aT.
type knownShift struct {
	tempC float64
	logA  float64
}

// FitWLF fits log10 aT = -C1 (T-Tref) / (C2 + (T-Tref)) via its linearised
// form. It needs at least two non-reference points; otherwise the fit is
// reported as unidentifiable instead of being extrapolated.
func FitWLF(ks []knownShift, trefC float64) FitResult {
	fr := FitResult{Model: "wlf", Params: map[string]float64{"TrefC": trefC}}
	var sx, sy, sxx, sxy float64
	var n float64
	for _, k := range ks {
		dt := k.tempC - trefC
		if dt == 0 || k.logA == 0 {
			continue
		}
		x := dt
		y := -dt / k.logA
		sx += x
		sy += y
		sxx += x * x
		sxy += x * y
		n++
	}
	if n < 2 {
		fr.Note = fmt.Sprintf("WLF 不可辨识：仅 %d 个非参考已知点，需要至少 2 个", int(n))
		return fr
	}
	den := n*sxx - sx*sx
	if den == 0 {
		fr.Note = "WLF 不可辨识：温度跨距退化"
		return fr
	}
	slope := (n*sxy - sx*sy) / den
	intercept := (sy - slope*sx) / n
	if slope == 0 {
		fr.Note = "WLF 不可辨识：拟合斜率为零"
		return fr
	}
	c1 := 1 / slope
	c2 := intercept * c1
	fr.OK = true
	fr.Params["C1"] = c1
	fr.Params["C2"] = c2
	fr.Note = fmt.Sprintf("WLF: C1=%.4f, C2=%.4f K（Tref=%.1f°C, n=%d）", c1, c2, trefC, int(n))
	return fr
}

// PredictWLF evaluates the fitted WLF model at tempC.
func (f FitResult) PredictWLF(tempC float64) float64 {
	dt := tempC - f.Params["TrefC"]
	return -f.Params["C1"] * dt / (f.Params["C2"] + dt)
}

// FitArrhenius fits log10 aT = k (1/T - 1/Tref) with k = Ea/(R ln 10).
// A single non-reference point already identifies Ea.
func FitArrhenius(ks []knownShift, trefC float64) FitResult {
	fr := FitResult{Model: "arrhenius", Params: map[string]float64{"TrefC": trefC}}
	trefK := trefC + 273.15
	var sxy, sxx float64
	var n int
	for _, k := range ks {
		x := 1/(k.tempC+273.15) - 1/trefK
		if x == 0 {
			continue
		}
		sxy += x * k.logA
		sxx += x * x
		n++
	}
	if n == 0 || sxx == 0 {
		fr.Note = "Arrhenius 不可辨识：无非参考已知点"
		return fr
	}
	k := sxy / sxx
	ea := k * 8.314462618 * math.Log(10)
	fr.OK = true
	fr.Params["EaJmol"] = ea
	fr.Note = fmt.Sprintf("Arrhenius: Ea=%.1f kJ/mol（Tref=%.1f°C, n=%d）", ea/1000, trefC, n)
	return fr
}

// PredictArrhenius evaluates the fitted Arrhenius model at tempC.
func (f FitResult) PredictArrhenius(tempC float64) float64 {
	k := f.Params["EaJmol"] / (8.314462618 * math.Log(10))
	return k * (1/(tempC+273.15) - 1/(f.Params["TrefC"]+273.15))
}
