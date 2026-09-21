// Package rheo implements time-temperature superposition (TTS) analysis:
// consistency checks, shift solving, WLF/Arrhenius fits and diagnostics.
package rheo

// Curve is one isothermal frequency sweep.
type Curve struct {
	ID    int64   `json:"id"`
	Label string  `json:"label"`
	TempC float64 `json:"temp_c"`
	Sort  int     `json:"sort"`
}

// Point is one measured frequency point of a curve.
type Point struct {
	ID           int64   `json:"id"`
	CurveID      int64   `json:"curve_id"`
	FreqHz       float64 `json:"freq_hz"`
	G1           float64 `json:"g1"` // storage modulus Pa
	G2           float64 `json:"g2"` // loss modulus Pa
	PhaseDeg     float64 `json:"phase_deg"`
	UserExcluded bool    `json:"user_excluded"`
	UserReason   string  `json:"user_reason"`
}

// CurveState holds user-controlled per-curve shift settings.
type CurveState struct {
	CurveID    int64   `json:"curve_id"`
	ManualH    float64 `json:"manual_h"`
	ManualHSet bool    `json:"manual_h_set"`
	ManualV    float64 `json:"manual_v"`
	Locked     bool    `json:"locked"`
}

// ShiftResult is the solved shift for one curve within a run.
type ShiftResult struct {
	CurveID      int64
	SolvedH      float64
	SolvedOK     bool
	RangeLo      float64
	RangeHi      float64
	Identifiable bool
	Note         string
}

// FitResult holds one model fit (WLF or Arrhenius).
type FitResult struct {
	Model  string // "wlf" or "arrhenius"
	OK     bool
	Params map[string]float64 // wlf: C1,C2,Tref ; arrhenius: EaJmol,TrefK
	Note   string
}

// Prediction is a model-predicted shift for one curve.
type Prediction struct {
	CurveID     int64
	Model       string
	Pred        float64
	HasResidual bool
	Residual    float64 // known shift (manual or solved) minus prediction
}

// PairError describes the overlap-region agreement of adjacent curves.
type PairError struct {
	AID     int64
	BID     int64
	Overlap bool
	RMS     float64
	N       int
	Message string
}

// Exclusion records one point excluded during a run, with reason.
type Exclusion struct {
	PointID int64
	Reason  string
}

// RunResult is the full deterministic output of one analysis run.
type RunResult struct {
	Seed        int64
	TolDeg      float64
	RefCurveID  int64
	Shifts      []ShiftResult
	Fits        []FitResult
	Predictions []Prediction
	Pairs       []PairError
	Exclusions  []Exclusion
	Fingerprint string
}
