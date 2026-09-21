// Package tts implements the core domain logic of the time-temperature
// superposition (TTS) workbench: data model, validation/exclusion rules,
// overlap error metrics, shift identifiability, WLF / Arrhenius fitting
// and the deterministic run fingerprint.
package tts

// Point is a single frequency-sweep measurement.
type Point struct {
	ID       int64   `json:"id"`
	Freq     float64 `json:"freq"`      // angular frequency, rad/s; must be > 0
	G1       float64 `json:"g1"`        // storage modulus G', Pa
	G2       float64 `json:"g2"`        // loss modulus G'', Pa
	PhaseDeg float64 `json:"phase_deg"` // phase angle delta, degrees
}

// Curve is one isothermal frequency sweep.
type Curve struct {
	ID     int64   `json:"id"`
	Label  string  `json:"label"`
	TempC  float64 `json:"temp_c"`
	Points []Point `json:"points"`
}

// Shift holds the manual shift factors of one curve.
// Reduced coordinates: log10(w_red) = log10(w) + LogA,
// log10(G_red) = log10(G) + LogB (only when UseVertical).
type Shift struct {
	CurveID     int64   `json:"curve_id"`
	LogA        float64 `json:"log_a"`
	LogB        float64 `json:"log_b"`
	UseVertical bool    `json:"use_vertical"`
	Locked      bool    `json:"locked"`
}

// UserExclusion is a manual, reason-annotated point exclusion
// (e.g. outside the linear viscoelastic region).
type UserExclusion struct {
	PointID int64  `json:"point_id"`
	Reason  string `json:"reason"`
}

// Run is the full mutable state of the workbench.
type Run struct {
	Seed       int64           `json:"seed"`
	RefTempC   float64         `json:"ref_temp_c"`
	Tolerance  float64         `json:"tolerance"` // phase/modulus consistency tolerance (relative)
	Curves     []Curve         `json:"curves"`
	Shifts     []Shift         `json:"shifts"`
	Exclusions []UserExclusion `json:"exclusions"` // user exclusions only; auto exclusions are derived
}

// ShiftOf returns the manual shift for a curve. The reference-temperature
// curve always reports a zero, locked shift by definition.
func (r *Run) ShiftOf(curveID int64) Shift {
	for _, s := range r.Shifts {
		if s.CurveID == curveID {
			if r.IsReference(curveID) {
				return Shift{CurveID: curveID, Locked: true}
			}
			return s
		}
	}
	return Shift{CurveID: curveID, Locked: r.IsReference(curveID)}
}

// IsReference reports whether the curve sits at the reference temperature
// (lowest curve ID among curves sharing RefTempC).
func (r *Run) IsReference(curveID int64) bool {
	ref, ok := r.ReferenceCurve()
	return ok && ref.ID == curveID
}

// ReferenceCurve returns the curve used as superposition reference.
func (r *Run) ReferenceCurve() (Curve, bool) {
	var best *Curve
	for i := range r.Curves {
		c := &r.Curves[i]
		if c.TempC == r.RefTempC {
			if best == nil || c.ID < best.ID {
				best = c
			}
		}
	}
	if best != nil {
		return *best, true
	}
	// Fall back to the lowest-temperature curve so the workbench stays usable.
	for i := range r.Curves {
		c := &r.Curves[i]
		if best == nil || c.TempC < best.TempC || (c.TempC == best.TempC && c.ID < best.ID) {
			best = c
		}
	}
	if best == nil {
		return Curve{}, false
	}
	return *best, true
}

// UserExclusionMap indexes user exclusions by point ID.
func (r *Run) UserExclusionMap() map[int64]string {
	m := make(map[int64]string, len(r.Exclusions))
	for _, e := range r.Exclusions {
		m[e.PointID] = e.Reason
	}
	return m
}
