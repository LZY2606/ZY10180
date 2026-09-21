// Package web serves the TTS workbench UI and JSON API.
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"ttsbench/internal/store"
	"ttsbench/internal/tts"
)

// Server wires the store to HTTP handlers.
type Server struct {
	st *store.Store
}

// New builds a Server.
func New(st *store.Store) *Server { return &Server{st: st} }

// Handler returns the root handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("POST /settings", s.handleSettings)
	mux.HandleFunc("POST /shifts", s.handleShifts)
	mux.HandleFunc("POST /exclude", s.handleExclude)
	mux.HandleFunc("POST /include", s.handleInclude)
	mux.HandleFunc("POST /reseed", s.handleReseed)
	mux.HandleFunc("GET /api/export", s.handleExport)
	mux.HandleFunc("POST /api/import", s.handleImport)
	mux.HandleFunc("GET /api/exclusions", s.handleExclusions)
	return mux
}

func (s *Server) loadRun(w http.ResponseWriter) *tts.Run {
	r, err := s.st.LoadRun()
	if err != nil {
		http.Error(w, "读取数据库失败: "+err.Error(), http.StatusInternalServerError)
		return nil
	}
	if r == nil {
		http.Error(w, "数据库为空，请先重建或导入运行记录", http.StatusConflict)
		return nil
	}
	return r
}

func redirect(w http.ResponseWriter, req *http.Request, notice string) {
	v := url.Values{}
	if notice != "" {
		v.Set("notice", notice)
	}
	to := "/"
	if len(v) > 0 {
		to += "?" + v.Encode()
	}
	http.Redirect(w, req, to, http.StatusSeeOther)
}

func (s *Server) handleIndex(w http.ResponseWriter, req *http.Request) {
	r := s.loadRun(w)
	if r == nil {
		return
	}
	vm := buildView(r)
	vm.Notice = req.URL.Query().Get("notice")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTpl.Execute(w, vm); err != nil {
		fmt.Fprintf(w, "渲染失败: %v", err)
	}
}

func buildView(r *tts.Run) *viewModel {
	vm := &viewModel{
		Run:         r,
		Fingerprint: tts.ShortFingerprint(r),
		RawSVG:      templateHTML(renderPlot(r, false)),
		ShiftedSVG:  templateHTML(renderPlot(r, true)),
		Overlaps:    r.AdjacentOverlaps(),
		Excluded:    r.ExcludedPoints(),
		WLFFit:      tts.FitWLF(r.FittingSamples(), r.RefTempC),
		ArrFit:      tts.FitArrhenius(r.FittingSamples(), r.RefTempC),
	}
	assess := map[int64]tts.ShiftAssessment{}
	for _, a := range r.Assessments() {
		assess[a.CurveID] = a
	}
	wlfRes := map[int64]tts.ModelResidual{}
	if vm.WLFFit.OK {
		for _, m := range r.Residuals(vm.WLFFit.Predict) {
			wlfRes[m.CurveID] = m
		}
	}
	arrRes := map[int64]tts.ModelResidual{}
	if vm.ArrFit.OK {
		for _, m := range r.Residuals(vm.ArrFit.Predict) {
			arrRes[m.CurveID] = m
		}
	}
	seenTemp := map[float64]bool{}
	for _, c := range r.Curves {
		if !seenTemp[c.TempC] {
			seenTemp[c.TempC] = true
			vm.TempChoices = append(vm.TempChoices, c.TempC)
		}
		sh := r.ShiftOf(c.ID)
		row := shiftRow{
			Curve:       c,
			Shift:       sh,
			IsReference: r.IsReference(c.ID),
			Assessment:  assess[c.ID],
			InFit:       r.IsReference(c.ID) || !sh.Locked,
		}
		if m, ok := wlfRes[c.ID]; ok {
			row.WLFPredicted, row.WLFResidual = m.Predicted, m.Residual
		}
		if m, ok := arrRes[c.ID]; ok {
			row.ArrPredicted, row.ArrResidual = m.Predicted, m.Residual
		}
		vm.Rows = append(vm.Rows, row)
	}
	// Per-point status for the exclusion panel.
	excl := map[int64]tts.ExcludedPoint{}
	for _, e := range r.ExcludedPoints() {
		excl[e.PointID] = e
	}
	for _, c := range r.Curves {
		cp := curvePoints{Curve: c}
		for _, p := range c.Points {
			pr := pointRow{Point: p}
			if e, ok := excl[p.ID]; ok {
				pr.Excluded = true
				pr.Reason = e.Reason
				pr.Source = e.Source
			}
			cp.Points = append(cp.Points, pr)
		}
		vm.CurvePoints = append(vm.CurvePoints, cp)
	}
	return vm
}

func (s *Server) handleSettings(w http.ResponseWriter, req *http.Request) {
	r := s.loadRun(w)
	if r == nil {
		return
	}
	if err := req.ParseForm(); err != nil {
		http.Error(w, "表单解析失败", http.StatusBadRequest)
		return
	}
	ref, err := strconv.ParseFloat(req.FormValue("ref_temp_c"), 64)
	if err != nil {
		redirect(w, req, "参考温度无效")
		return
	}
	tol, err := strconv.ParseFloat(req.FormValue("tolerance"), 64)
	if err != nil || !(tol > 0) {
		redirect(w, req, "容差必须为正数")
		return
	}
	known := false
	for _, c := range r.Curves {
		if c.TempC == ref {
			known = true
		}
	}
	if !known {
		redirect(w, req, "参考温度必须对应某条等温曲线")
		return
	}
	r.RefTempC = ref
	r.Tolerance = tol
	if err := s.st.SaveRun(r); err != nil {
		http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	redirect(w, req, "设置已更新")
}

func (s *Server) handleShifts(w http.ResponseWriter, req *http.Request) {
	r := s.loadRun(w)
	if r == nil {
		return
	}
	if err := req.ParseForm(); err != nil {
		http.Error(w, "表单解析失败", http.StatusBadRequest)
		return
	}
	for i, sh := range r.Shifts {
		if r.IsReference(sh.CurveID) {
			r.Shifts[i] = tts.Shift{CurveID: sh.CurveID, Locked: true}
			continue
		}
		sh.Locked = req.FormValue(fmt.Sprintf("locked_%d", sh.CurveID)) != ""
		if !sh.Locked {
			if v := req.FormValue(fmt.Sprintf("loga_%d", sh.CurveID)); v != "" {
				f, err := strconv.ParseFloat(v, 64)
				if err != nil {
					redirect(w, req, fmt.Sprintf("曲线 %d 的 log aT 无效", sh.CurveID))
					return
				}
				sh.LogA = f
			}
			if v := req.FormValue(fmt.Sprintf("logb_%d", sh.CurveID)); v != "" {
				f, err := strconv.ParseFloat(v, 64)
				if err != nil {
					redirect(w, req, fmt.Sprintf("曲线 %d 的 log bT 无效", sh.CurveID))
					return
				}
				sh.LogB = f
			}
			sh.UseVertical = req.FormValue(fmt.Sprintf("usevert_%d", sh.CurveID)) != ""
		}
		r.Shifts[i] = sh
	}
	if err := s.st.SaveRun(r); err != nil {
		http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	redirect(w, req, "移位因子已保存（模型预测不覆盖手工值）")
}

func (s *Server) handleExclude(w http.ResponseWriter, req *http.Request) {
	s.setExclusion(w, req, true)
}

func (s *Server) handleInclude(w http.ResponseWriter, req *http.Request) {
	s.setExclusion(w, req, false)
}

func (s *Server) setExclusion(w http.ResponseWriter, req *http.Request, exclude bool) {
	r := s.loadRun(w)
	if r == nil {
		return
	}
	if err := req.ParseForm(); err != nil {
		http.Error(w, "表单解析失败", http.StatusBadRequest)
		return
	}
	pid, err := strconv.ParseInt(req.FormValue("point_id"), 10, 64)
	if err != nil {
		redirect(w, req, "点 ID 无效")
		return
	}
	known := false
	for _, c := range r.Curves {
		for _, p := range c.Points {
			if p.ID == pid {
				known = true
			}
		}
	}
	if !known {
		redirect(w, req, "点不存在")
		return
	}
	if exclude {
		reason := req.FormValue("reason")
		if reason == "" {
			reason = "超出线性黏弹区"
		}
		found := false
		for i, e := range r.Exclusions {
			if e.PointID == pid {
				r.Exclusions[i].Reason = reason
				found = true
			}
		}
		if !found {
			r.Exclusions = append(r.Exclusions, tts.UserExclusion{PointID: pid, Reason: reason})
		}
	} else {
		out := r.Exclusions[:0]
		for _, e := range r.Exclusions {
			if e.PointID != pid {
				out = append(out, e)
			}
		}
		r.Exclusions = out
	}
	if err := s.st.SaveRun(r); err != nil {
		http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if exclude {
		redirect(w, req, fmt.Sprintf("点 %d 已排除", pid))
	} else {
		redirect(w, req, fmt.Sprintf("点 %d 已恢复", pid))
	}
}

func (s *Server) handleReseed(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		http.Error(w, "表单解析失败", http.StatusBadRequest)
		return
	}
	seed, err := strconv.ParseInt(req.FormValue("seed"), 10, 64)
	if err != nil {
		redirect(w, req, "种子无效")
		return
	}
	if err := s.st.SaveRun(tts.NewRun(seed)); err != nil {
		http.Error(w, "重建失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	redirect(w, req, fmt.Sprintf("已按种子 %d 重建数据库", seed))
}

func (s *Server) handleExclusions(w http.ResponseWriter, req *http.Request) {
	r := s.loadRun(w)
	if r == nil {
		return
	}
	writeJSON(w, r.ExcludedPoints())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// templateHTML marks trusted, locally generated SVG markup.
func templateHTML(s string) templateHTMLType { return templateHTMLType(s) }
