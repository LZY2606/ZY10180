// Package web serves the TTS workbench page and its form endpoints.
package web

import (
	"fmt"
	"html/template"
	"math"
	"net/http"
	"sort"
	"strconv"

	"ttsworkbench/internal/rheo"
	"ttsworkbench/internal/store"
)

// Server wires the store and fixture to HTTP handlers.
type Server struct {
	st      *store.Store
	fixture rheo.Fixture
	mux     *http.ServeMux
}

// NewServer builds the HTTP handler tree.
func NewServer(st *store.Store, fx rheo.Fixture) *Server {
	s := &Server{st: st, fixture: fx}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("POST /state", s.handleState)
	s.mux.HandleFunc("POST /exclude", s.handleExclude)
	s.mux.HandleFunc("POST /run", s.handleRun)
	s.mux.HandleFunc("POST /reset", s.handleReset)
	s.mux.HandleFunc("GET /export", s.handleExport)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func redirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

type curveRow struct {
	Curve rheo.Curve
	State rheo.CurveState
	Shift *rheo.ShiftResult
	PredW *rheo.Prediction
	PredA *rheo.Prediction
	IsRef bool
}

type pairRow struct {
	Label string
	P     rheo.PairError
}

type exclRow struct {
	PointID    int64
	Curve      string
	Freq       float64
	Source     string
	Reason     string
	Restorable bool
}

type pointRow struct {
	P          rheo.Point
	AutoReason string
}

type curvePoints struct {
	Curve rheo.Curve
	Rows  []pointRow
}

type pageData struct {
	Curves      []rheo.Curve
	Rows        []curveRow
	Pairs       []pairRow
	Exclusions  []exclRow
	Points      []curvePoints
	Runs        []store.RunMeta
	Meta        *store.RunMeta
	Fits        []rheo.FitResult
	RawSVG      template.HTML
	ShiftSVG    template.HTML
	PhaseSVG    template.HTML
	SeedDefault int64
	TolDefault  float64
	RefID       int64
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	curves, err := s.st.Curves()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	points, err := s.st.Points()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	states, err := s.st.States()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	meta, run, err := s.st.LatestRun()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	runs, err := s.st.Runs()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	d := pageData{Curves: curves, Runs: runs, Meta: meta, SeedDefault: s.fixture.Seed, TolDefault: 2.0}
	if meta != nil {
		d.SeedDefault = meta.Seed
		d.TolDefault = meta.TolDeg
		d.RefID = meta.RefCurveID
	} else {
		d.RefID = defaultRef(curves, s.fixture.RefLabel)
	}

	shifts := map[int64]*rheo.ShiftResult{}
	preds := map[int64]map[string]*rheo.Prediction{}
	autoReason := map[int64]string{}
	eff := map[int64]float64{}
	if run != nil {
		d.Fits = run.Fits
		for i := range run.Shifts {
			shifts[run.Shifts[i].CurveID] = &run.Shifts[i]
		}
		for i := range run.Predictions {
			p := &run.Predictions[i]
			if preds[p.CurveID] == nil {
				preds[p.CurveID] = map[string]*rheo.Prediction{}
			}
			preds[p.CurveID][p.Model] = p
		}
		for _, ex := range run.Exclusions {
			autoReason[ex.PointID] = ex.Reason
		}
		eff = rheo.EffectiveShifts(curves, states, run.Shifts)
	} else {
		for _, c := range curves {
			if st := states[c.ID]; st.ManualHSet {
				eff[c.ID] = st.ManualH
			}
		}
	}

	labelOf := map[int64]string{}
	for _, c := range curves {
		labelOf[c.ID] = c.Label
		row := curveRow{Curve: c, State: states[c.ID], Shift: shifts[c.ID], IsRef: c.ID == d.RefID}
		if m := preds[c.ID]; m != nil {
			row.PredW = m["wlf"]
			row.PredA = m["arrhenius"]
		}
		d.Rows = append(d.Rows, row)
	}
	if run != nil {
		for _, p := range run.Pairs {
			d.Pairs = append(d.Pairs, pairRow{Label: labelOf[p.AID] + " ↔ " + labelOf[p.BID], P: p})
		}
	}
	// exclusions: user-flagged plus per-run auto reasons
	for _, c := range curves {
		for _, p := range points[c.ID] {
			if p.UserExcluded {
				d.Exclusions = append(d.Exclusions, exclRow{p.ID, c.Label, p.FreqHz, "用户", p.UserReason, true})
			} else if reason, ok := autoReason[p.ID]; ok {
				d.Exclusions = append(d.Exclusions, exclRow{p.ID, c.Label, p.FreqHz, "一致性检查", reason, false})
			}
		}
	}
	sort.Slice(d.Exclusions, func(i, j int) bool { return d.Exclusions[i].PointID < d.Exclusions[j].PointID })

	for _, c := range curves {
		cp := curvePoints{Curve: c}
		for _, p := range points[c.ID] {
			cp.Rows = append(cp.Rows, pointRow{P: p, AutoReason: autoReason[p.ID]})
		}
		d.Points = append(d.Points, cp)
	}

	d.RawSVG = template.HTML(s.rawChart(curves, points, autoReason))
	d.ShiftSVG = template.HTML(s.shiftChart(curves, points, states, eff, autoReason))
	d.PhaseSVG = template.HTML(s.phaseChart(curves, points, eff, autoReason))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.Execute(w, d); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func defaultRef(curves []rheo.Curve, refLabel string) int64 {
	for _, c := range curves {
		if c.Label == refLabel {
			return c.ID
		}
	}
	if len(curves) > 0 {
		return curves[len(curves)/2].ID
	}
	return 0
}

func (s *Server) rawChart(curves []rheo.Curve, points map[int64][]rheo.Point, auto map[int64]string) string {
	var series []chartSeries
	for i, c := range curves {
		var g1, g2 chartSeries
		g1.Name = c.Label + " G'"
		g2.Name = c.Label + " G''"
		g1.Color, g2.Color = palette[i%len(palette)], palette[i%len(palette)]
		g2.Dash = true
		for _, p := range points[c.ID] {
			if p.UserExcluded || auto[p.ID] != "" || p.FreqHz <= 0 || p.G1 <= 0 || p.G2 <= 0 {
				if p.FreqHz > 0 && p.G1 > 0 && p.G2 > 0 {
					g1.CrossX = append(g1.CrossX, math.Log10(p.FreqHz))
					g1.CrossY = append(g1.CrossY, math.Log10(p.G1))
				}
				continue
			}
			x := math.Log10(p.FreqHz)
			g1.X = append(g1.X, x)
			g1.Y = append(g1.Y, math.Log10(p.G1))
			g2.X = append(g2.X, x)
			g2.Y = append(g2.Y, math.Log10(p.G2))
		}
		series = append(series, g1, g2)
	}
	return chartSVG("原始频率扫描", "log10 频率 (Hz)", "log10 模量 (Pa)", series)
}

func (s *Server) shiftChart(curves []rheo.Curve, points map[int64][]rheo.Point, states map[int64]rheo.CurveState, eff map[int64]float64, auto map[int64]string) string {
	var series []chartSeries
	for i, c := range curves {
		h, ok := eff[c.ID]
		if !ok {
			continue
		}
		v := states[c.ID].ManualV
		var g1, g2 chartSeries
		g1.Name = c.Label + " G'"
		g2.Name = c.Label + " G''"
		g1.Color, g2.Color = palette[i%len(palette)], palette[i%len(palette)]
		g2.Dash = true
		for _, p := range points[c.ID] {
			if p.UserExcluded || auto[p.ID] != "" || p.FreqHz <= 0 || p.G1 <= 0 || p.G2 <= 0 {
				continue
			}
			x := math.Log10(p.FreqHz) + h
			g1.X = append(g1.X, x)
			g1.Y = append(g1.Y, math.Log10(p.G1)+v)
			g2.X = append(g2.X, x)
			g2.Y = append(g2.Y, math.Log10(p.G2)+v)
		}
		series = append(series, g1, g2)
	}
	return chartSVG("叠加主曲线", "log10 折算频率 (Hz)", "log10 折算模量 (Pa)", series)
}

func (s *Server) phaseChart(curves []rheo.Curve, points map[int64][]rheo.Point, eff map[int64]float64, auto map[int64]string) string {
	var series []chartSeries
	for i, c := range curves {
		h, ok := eff[c.ID]
		if !ok {
			continue
		}
		var ph chartSeries
		ph.Name = c.Label + " δ"
		ph.Color = palette[i%len(palette)]
		for _, p := range points[c.ID] {
			if p.UserExcluded || auto[p.ID] != "" || p.FreqHz <= 0 || p.G1 <= 0 || p.G2 <= 0 {
				continue
			}
			ph.X = append(ph.X, math.Log10(p.FreqHz)+h)
			ph.Y = append(ph.Y, p.PhaseDeg)
		}
		series = append(series, ph)
	}
	return chartSVG("相位角", "log10 折算频率 (Hz)", "相位角 (°)", series)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	cid, err := strconv.ParseInt(r.FormValue("curve_id"), 10, 64)
	if err != nil {
		http.Error(w, "curve_id 无效", 400)
		return
	}
	st := rheo.CurveState{CurveID: cid, Locked: r.FormValue("locked") != ""}
	if hv := r.FormValue("manual_h"); hv != "" {
		v, err := strconv.ParseFloat(hv, 64)
		if err != nil {
			http.Error(w, "手工 log aT 无效", 400)
			return
		}
		st.ManualH, st.ManualHSet = v, true
	}
	if vv := r.FormValue("manual_v"); vv != "" {
		v, err := strconv.ParseFloat(vv, 64)
		if err != nil {
			http.Error(w, "手工 log bT 无效", 400)
			return
		}
		st.ManualV = v
	}
	if err := s.st.SetState(st); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	redirect(w, r)
}

func (s *Server) handleExclude(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	pid, err := strconv.ParseInt(r.FormValue("point_id"), 10, 64)
	if err != nil {
		http.Error(w, "point_id 无效", 400)
		return
	}
	switch r.FormValue("op") {
	case "exclude":
		reason := r.FormValue("reason")
		if reason == "" {
			reason = "用户排除"
		}
		err = s.st.SetPointExcluded(pid, true, reason)
	case "restore":
		err = s.st.SetPointExcluded(pid, false, "")
	default:
		err = fmt.Errorf("op 无效")
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	redirect(w, r)
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	seed, err := strconv.ParseInt(r.FormValue("seed"), 10, 64)
	if err != nil {
		http.Error(w, "seed 无效", 400)
		return
	}
	tol, err := strconv.ParseFloat(r.FormValue("tol_deg"), 64)
	if err != nil || tol <= 0 {
		http.Error(w, "容差必须为正数", 400)
		return
	}
	refID, err := strconv.ParseInt(r.FormValue("ref_curve_id"), 10, 64)
	if err != nil {
		http.Error(w, "ref_curve_id 无效", 400)
		return
	}
	curves, err := s.st.Curves()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	points, err := s.st.Points()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	states, err := s.st.States()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	res := rheo.Run(curves, points, states, seed, tol, refID)
	if _, err := s.st.SaveRun(res); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	redirect(w, r)
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if err := s.st.ImportFixture(s.fixture); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	redirect(w, r)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	buf, err := s.st.Export()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="tts-runs.json"`)
	w.Write(buf)
}
