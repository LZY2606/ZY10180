package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"ttsbench/internal/tts"
)

// exportFile is the portable run-record format ("tts-run/v1").
type exportFile struct {
	Format      string        `json:"format"`
	Fingerprint string        `json:"fingerprint"`
	Run         *tts.Run      `json:"run"`
	Derived     exportDerived `json:"derived"`
}

type exportDerived struct {
	WLFFit         tts.WLFFit            `json:"wlf_fit"`
	ArrheniusFit   tts.ArrheniusFit      `json:"arrhenius_fit"`
	ExcludedPoints []tts.ExcludedPoint   `json:"excluded_points"`
	Assessments    []tts.ShiftAssessment `json:"assessments"`
	Overlaps       []tts.OverlapError    `json:"overlaps"`
}

func buildExport(r *tts.Run) *exportFile {
	return &exportFile{
		Format:      "tts-run/v1",
		Fingerprint: tts.Fingerprint(r),
		Run:         r,
		Derived: exportDerived{
			WLFFit:         tts.FitWLF(r.FittingSamples(), r.RefTempC),
			ArrheniusFit:   tts.FitArrhenius(r.FittingSamples(), r.RefTempC),
			ExcludedPoints: r.ExcludedPoints(),
			Assessments:    r.Assessments(),
			Overlaps:       r.AdjacentOverlaps(),
		},
	}
}

func (s *Server) handleExport(w http.ResponseWriter, req *http.Request) {
	r := s.loadRun(w)
	if r == nil {
		return
	}
	ef := buildExport(r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="tts-run-%s.json"`, ef.Fingerprint[:12]))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(ef)
}

func (s *Server) handleImport(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseMultipartForm(8 << 20); err != nil {
		redirect(w, req, "导入失败：表单解析错误")
		return
	}
	var raw []byte
	if f, _, err := req.FormFile("file"); err == nil {
		defer f.Close()
		raw, err = io.ReadAll(io.LimitReader(f, 16<<20))
		if err != nil {
			redirect(w, req, "导入失败：读取文件错误")
			return
		}
	} else {
		redirect(w, req, "导入失败：未选择文件")
		return
	}
	var ef exportFile
	if err := json.Unmarshal(raw, &ef); err != nil || ef.Run == nil {
		// Also accept a bare Run document.
		var bare tts.Run
		if err2 := json.Unmarshal(raw, &bare); err2 != nil || len(bare.Curves) == 0 {
			redirect(w, req, "导入失败：无法识别的运行记录格式")
			return
		}
		ef.Run = &bare
	}
	if err := validateRun(ef.Run); err != nil {
		redirect(w, req, "导入失败："+err.Error())
		return
	}
	if err := s.st.SaveRun(ef.Run); err != nil {
		http.Error(w, "导入失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	loaded, err := s.st.LoadRun()
	if err != nil {
		http.Error(w, "导入失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	recomputed := tts.Fingerprint(loaded)
	notice := fmt.Sprintf("导入完成，运行指纹 %s", recomputed[:12])
	if ef.Fingerprint != "" {
		if ef.Fingerprint == recomputed {
			notice += "，与记录指纹一致"
		} else {
			notice += "，与记录指纹不一致（数据可能被改动）"
		}
	}
	redirect(w, req, notice)
}

// validateRun checks referential integrity of an imported run.
func validateRun(r *tts.Run) error {
	if len(r.Curves) == 0 {
		return fmt.Errorf("至少包含一条曲线")
	}
	if !(r.Tolerance > 0) {
		return fmt.Errorf("容差必须为正")
	}
	curveIDs := map[int64]bool{}
	pointIDs := map[int64]bool{}
	for _, c := range r.Curves {
		if curveIDs[c.ID] {
			return fmt.Errorf("曲线 ID %d 重复", c.ID)
		}
		curveIDs[c.ID] = true
		for _, p := range c.Points {
			if pointIDs[p.ID] {
				return fmt.Errorf("点 ID %d 重复", p.ID)
			}
			pointIDs[p.ID] = true
		}
	}
	for _, sh := range r.Shifts {
		if !curveIDs[sh.CurveID] {
			return fmt.Errorf("移位因子引用了不存在的曲线 %d", sh.CurveID)
		}
	}
	for _, e := range r.Exclusions {
		if !pointIDs[e.PointID] {
			return fmt.Errorf("排除记录引用了不存在的点 %d", e.PointID)
		}
		if e.Reason == "" {
			return fmt.Errorf("点 %d 的排除理由为空", e.PointID)
		}
	}
	return nil
}
