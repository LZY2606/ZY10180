package tts

import "math"

// TempShift is one (temperature, manual log10 aT) sample used for fitting.
type TempShift struct {
	TempC float64 `json:"temp_c"`
	LogA  float64 `json:"log_a"`
	Label string  `json:"label"`
}

// FittingSamples collects manual shift factors for model fitting.
// The reference curve contributes its defining anchor point (Tref, 0);
// locked curves are excluded from fitting.
func (r *Run) FittingSamples() []TempShift {
	var out []TempShift
	for _, c := range r.Curves {
		s := r.ShiftOf(c.ID)
		if r.IsReference(c.ID) {
			out = append(out, TempShift{TempC: c.TempC, LogA: 0, Label: c.Label})
			continue
		}
		if s.Locked {
			continue
		}
		out = append(out, TempShift{TempC: c.TempC, LogA: s.LogA, Label: c.Label})
	}
	return out
}

// WLFFit holds fitted WLF parameters: log10 aT = -C1 (T-Tref) / (C2 + T - Tref).
type WLFFit struct {
	OK       bool    `json:"ok"`
	C1       float64 `json:"c1"`
	C2       float64 `json:"c2"`
	RefTempC float64 `json:"ref_temp_c"`
	Samples  int     `json:"samples"`
}

// Predict evaluates the WLF shift law at tempC.
func (f WLFFit) Predict(tempC float64) float64 {
	dt := tempC - f.RefTempC
	return -f.C1 * dt / (f.C2 + dt)
}

// FitWLF fits C1, C2 deterministically: C1 is linear given C2, so a
// golden-section search over C2 with a closed-form C1 suffices.
func FitWLF(samples []TempShift, refTempC float64) WLFFit {
	fit := WLFFit{RefTempC: refTempC, Samples: len(samples)}
	if len(samples) < 2 {
		return fit
	}
	sse := func(c2 float64) (float64, float64) {
		var suy, suu float64
		for _, s := range samples {
			dt := s.TempC - refTempC
			den := c2 + dt
			if den == 0 {
				return math.Inf(1), 0
			}
			u := -dt / den
			suy += u * s.LogA
			suu += u * u
		}
		if suu == 0 {
			return math.Inf(1), 0
		}
		c1 := suy / suu
		var sse float64
		for _, s := range samples {
			dt := s.TempC - refTempC
			r := s.LogA - (-c1 * dt / (c2 + dt))
			sse += r * r
		}
		return sse, c1
	}
	lo, hi := 0.02, 5000.0
	gr := (math.Sqrt(5) - 1) / 2
	x1, x2 := hi-gr*(hi-lo), lo+gr*(hi-lo)
	f1, _ := sse(x1)
	f2, _ := sse(x2)
	for i := 0; i < 200; i++ {
		if f1 > f2 {
			lo = x1
			x1, f1 = x2, f2
			x2 = lo + gr*(hi-lo)
			f2, _ = sse(x2)
		} else {
			hi = x2
			x2, f2 = x1, f1
			x1 = hi - gr*(hi-lo)
			f1, _ = sse(x1)
		}
	}
	best := 0.5 * (lo + hi)
	sseV, c1 := sse(best)
	if math.IsInf(sseV, 1) {
		return fit
	}
	fit.OK = true
	fit.C1 = c1
	fit.C2 = best
	return fit
}

// ArrheniusFit holds a fitted Arrhenius shift law:
// log10 aT = Ea / (R ln 10) * (1/T - 1/Tref), T in kelvin.
type ArrheniusFit struct {
	OK        bool    `json:"ok"`
	EaJPerMol float64 `json:"ea_j_per_mol"`
	RefTempC  float64 `json:"ref_temp_c"`
	Samples   int     `json:"samples"`
}

const gasConstant = 8.314462618

// Predict evaluates the Arrhenius shift law at tempC.
func (f ArrheniusFit) Predict(tempC float64) float64 {
	t := tempC + 273.15
	tref := f.RefTempC + 273.15
	return f.EaJPerMol / (gasConstant * math.Log(10)) * (1/t - 1/tref)
}

// FitArrhenius fits Ea by least squares through the origin (closed form).
func FitArrhenius(samples []TempShift, refTempC float64) ArrheniusFit {
	fit := ArrheniusFit{RefTempC: refTempC, Samples: len(samples)}
	if len(samples) < 2 {
		return fit
	}
	tref := refTempC + 273.15
	var sxy, sxx float64
	for _, s := range samples {
		x := 1/(s.TempC+273.15) - 1/tref
		sxy += x * s.LogA
		sxx += x * x
	}
	if sxx == 0 {
		return fit
	}
	fit.OK = true
	fit.EaJPerMol = sxy / sxx * gasConstant * math.Log(10)
	return fit
}

// ModelResidual pairs a curve's manual shift with a model prediction.
// Model values are never written back into the manual shift.
type ModelResidual struct {
	CurveID   int64   `json:"curve_id"`
	Label     string  `json:"label"`
	TempC     float64 `json:"temp_c"`
	Manual    float64 `json:"manual"`
	Predicted float64 `json:"predicted"`
	Residual  float64 `json:"residual"` // manual - predicted
}

// Residuals evaluates a fitted model against the manual shifts of every
// curve that takes part in fitting (reference anchor included).
func (r *Run) Residuals(predict func(tempC float64) float64) []ModelResidual {
	var out []ModelResidual
	for _, c := range r.Curves {
		s := r.ShiftOf(c.ID)
		if !r.IsReference(c.ID) && s.Locked {
			continue
		}
		manual := s.LogA
		if r.IsReference(c.ID) {
			manual = 0
		}
		p := predict(c.TempC)
		out = append(out, ModelResidual{
			CurveID: c.ID, Label: c.Label, TempC: c.TempC,
			Manual: manual, Predicted: p, Residual: manual - p,
		})
	}
	return out
}
