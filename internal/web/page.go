package web

import (
	"fmt"
	"html/template"

	"ttsbench/internal/tts"
)

// shiftRow is one row of the shift-factor table.
type shiftRow struct {
	Curve        tts.Curve
	Shift        tts.Shift
	IsReference  bool
	Assessment   tts.ShiftAssessment
	WLFPredicted float64
	WLFResidual  float64
	ArrPredicted float64
	ArrResidual  float64
	InFit        bool
}

// pointRow is one raw point with its exclusion status.
type pointRow struct {
	Point    tts.Point
	Excluded bool
	Reason   string
	Source   string
}

type curvePoints struct {
	Curve  tts.Curve
	Points []pointRow
}

type viewModel struct {
	Run         *tts.Run
	Fingerprint string
	Notice      string
	RawSVG      template.HTML
	ShiftedSVG  template.HTML
	Rows        []shiftRow
	Overlaps    []tts.OverlapError
	Excluded    []tts.ExcludedPoint
	WLFFit      tts.WLFFit
	ArrFit      tts.ArrheniusFit
	CurvePoints []curvePoints
	TempChoices []float64
}

var pageTpl = template.Must(template.New("page").Funcs(template.FuncMap{
	"f3": func(v float64) string { return sprintf("%.3f", v) },
	"f4": func(v float64) string { return sprintf("%.4f", v) },
	"f2": func(v float64) string { return sprintf("%.2f", v) },
	"fg": func(v float64) string { return sprintf("%.6g", v) },
}).Parse(pageHTML))

func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

const pageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>流变叠时台</title>
<style>
 body{font-family:-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;margin:0;background:#f5f6f8;color:#222}
 header{background:#22303f;color:#fff;padding:14px 24px}
 header h1{margin:0;font-size:20px}
 header .meta{font-size:12px;color:#b9c6d2;margin-top:4px}
 main{padding:16px 24px;max-width:1180px;margin:0 auto}
 section{background:#fff;border:1px solid #ddd;border-radius:6px;padding:14px 18px;margin-bottom:16px}
 h2{font-size:16px;margin:0 0 10px;border-bottom:1px solid #eee;padding-bottom:6px}
 table{border-collapse:collapse;width:100%;font-size:13px}
 th,td{border:1px solid #e2e2e2;padding:4px 8px;text-align:right}
 th{background:#f0f2f5;text-align:center}
 td.l,th.l{text-align:left}
 input[type=text],input[type=number]{width:90px;padding:2px 4px;font-size:13px}
 button{padding:3px 12px;font-size:13px;cursor:pointer}
 .notice{background:#e8f4e5;border:1px solid #9ccc9c;padding:8px 12px;border-radius:4px;margin-bottom:12px;font-size:13px}
 .warn{color:#a33}
 .ok{color:#2c8a3b}
 .mono{font-family:ui-monospace,Menlo,monospace;font-size:12px}
 .grid2{display:grid;grid-template-columns:1fr 1fr;gap:16px}
 details{margin-top:8px}
 summary{cursor:pointer;font-size:13px;color:#1f6fb2}
 .diag{font-size:13px;color:#7a4d00;background:#fff7e6;border:1px solid #f0d9a8;padding:6px 10px;border-radius:4px;margin:4px 0}
</style>
</head>
<body>
<header>
 <h1>流变叠时台</h1>
 <div class="meta">时温叠加（TTS）工作台 · 种子 {{.Run.Seed}} · 参考温度 {{f2 .Run.RefTempC}} °C · 一致性容差 {{f3 .Run.Tolerance}} · 运行指纹 <span class="mono">{{.Fingerprint}}</span></div>
</header>
<main>
{{if .Notice}}<div class="notice">{{.Notice}}</div>{{end}}

<section>
 <h2>运行控制</h2>
 <form method="post" action="/settings" style="display:inline-block;margin-right:24px">
  参考温度
  <select name="ref_temp_c">
   {{range .TempChoices}}<option value="{{fg .}}" {{if eq (f2 .) (f2 $.Run.RefTempC)}}selected{{end}}>{{f2 .}} °C</option>{{end}}
  </select>
  一致性容差 <input type="text" name="tolerance" value="{{fg .Run.Tolerance}}">
  <button type="submit">更新设置</button>
 </form>
 <form method="post" action="/reseed" style="display:inline-block;margin-right:24px">
  种子 <input type="text" name="seed" value="{{.Run.Seed}}">
  <button type="submit">清空并按种子重建</button>
 </form>
 <a href="/api/export" download><button type="button">导出运行记录 (JSON)</button></a>
 <form method="post" action="/api/import" enctype="multipart/form-data" style="display:inline-block;margin-left:24px">
  <input type="file" name="file" accept=".json">
  <button type="submit">导入运行记录</button>
 </form>
</section>

<section>
 <h2>原始对数曲线</h2>
 {{.RawSVG}}
</section>

<section>
 <h2>移位后曲线（应用手工移位因子）</h2>
 {{.ShiftedSVG}}
</section>

<section>
 <h2>移位因子表（手工值不会被模型覆盖）</h2>
 <form method="post" action="/shifts">
 <table>
  <tr><th>曲线</th><th>温度 °C</th><th>锁定</th><th>log₁₀ aT（手工）</th><th>竖直移位</th><th>log₁₀ bT</th>
      <th>可识别性 / 诊断</th><th>建议 log₁₀ aT</th><th>WLF 预测</th><th>WLF 残差</th><th>Arrhenius 预测</th><th>Arrhenius 残差</th></tr>
  {{range .Rows}}
  <tr>
   <td class="l">{{.Curve.Label}}{{if .IsReference}}（参考）{{end}}</td>
   <td>{{f2 .Curve.TempC}}</td>
   <td style="text-align:center">
    {{if .IsReference}}—{{else}}<input type="checkbox" name="locked_{{.Curve.ID}}" {{if .Shift.Locked}}checked{{end}}>{{end}}
   </td>
   {{if .IsReference}}
    <td class="mono">0（定义）</td><td style="text-align:center">—</td><td class="mono">0</td>
   {{else if .Shift.Locked}}
    <td class="mono">{{f4 .Shift.LogA}}</td>
    <td style="text-align:center">{{if .Shift.UseVertical}}✓{{end}}</td>
    <td class="mono">{{f4 .Shift.LogB}}</td>
   {{else}}
    <td><input type="text" name="loga_{{.Curve.ID}}" value="{{f4 .Shift.LogA}}"></td>
    <td style="text-align:center"><input type="checkbox" name="usevert_{{.Curve.ID}}" {{if .Shift.UseVertical}}checked{{end}}></td>
    <td><input type="text" name="logb_{{.Curve.ID}}" value="{{f4 .Shift.LogB}}"></td>
   {{end}}
   <td class="l">{{if .Assessment.Identifiable}}<span class="ok">{{.Assessment.Message}}</span>{{else}}<span class="warn">{{.Assessment.Message}}</span>{{end}}</td>
   <td class="mono">{{if .Assessment.Identifiable}}{{if not .IsReference}}{{f4 .Assessment.SuggestedLogA}}{{end}}{{end}}</td>
   {{if .InFit}}
    <td class="mono">{{f4 .WLFPredicted}}</td><td class="mono">{{f4 .WLFResidual}}</td>
    <td class="mono">{{f4 .ArrPredicted}}</td><td class="mono">{{f4 .ArrResidual}}</td>
   {{else}}
    <td colspan="4" style="text-align:center;color:#888">已锁定，不参与拟合</td>
   {{end}}
  </tr>
  {{end}}
 </table>
 <div style="margin-top:8px"><button type="submit">保存移位因子</button>
 <span style="font-size:12px;color:#666;margin-left:12px">WLF：C1={{f4 .WLFFit.C1}}，C2={{f2 .WLFFit.C2}} K（{{.WLFFit.Samples}} 点）· Arrhenius：Ea={{f2 .ArrFit.EaJPerMol}} J/mol（{{.ArrFit.Samples}} 点）</span></div>
 </form>
</section>

<section>
 <h2>相邻温度重叠区误差</h2>
 <table>
  <tr><th>曲线对</th><th>重叠宽度 / decades</th><th>样本数</th><th>RMS(G′)</th><th>RMS(G″)</th><th class="l">说明</th></tr>
  {{range .Overlaps}}
  <tr>
   <td class="l">{{.LabelA}} ↔ {{.LabelB}}</td>
   {{if .OK}}
    <td>{{f3 .OverlapDecades}}</td><td>{{.Samples}}</td>
    <td class="mono">{{f4 .RMSG1}}</td><td class="mono">{{f4 .RMSG2}}</td>
    <td class="l ok">重叠区失配（log10 Pa）</td>
   {{else}}
    <td colspan="4" style="text-align:center;color:#888">无重叠频段</td>
    <td class="l warn">不得外推求精确移位，仅可给出范围</td>
   {{end}}
  </tr>
  {{end}}
 </table>
</section>

<section>
 <h2>排除点（可反查理由）</h2>
 {{if .Excluded}}
 <table>
  <tr><th>点 ID</th><th>曲线</th><th>频率 rad/s</th><th class="l">理由</th><th>来源</th><th></th></tr>
  {{range .Excluded}}
  <tr>
   <td class="mono">{{.PointID}}</td><td>{{.Label}}</td><td class="mono">{{fg .Freq}}</td>
   <td class="l">{{.Reason}}</td><td>{{if eq .Source "user"}}手工{{else}}自动{{end}}</td>
   <td>{{if eq .Source "user"}}<form method="post" action="/include" style="display:inline"><input type="hidden" name="point_id" value="{{.PointID}}"><button type="submit">恢复</button></form>{{end}}</td>
  </tr>
  {{end}}
 </table>
 {{else}}<p>当前无排除点。</p>{{end}}
 <details>
  <summary>逐点查看 / 手工排除（如超出线性黏弹区）</summary>
  {{range .CurvePoints}}
  <h3 style="font-size:13px;margin:10px 0 4px">{{.Curve.Label}}（{{f2 .Curve.TempC}} °C）</h3>
  <table>
   <tr><th>点 ID</th><th>频率</th><th>G′</th><th>G″</th><th>δ °</th><th class="l">状态</th><th></th></tr>
   {{range .Points}}
   <tr>
    <td class="mono">{{.Point.ID}}</td>
    <td class="mono">{{fg .Point.Freq}}</td>
    <td class="mono">{{fg .Point.G1}}</td>
    <td class="mono">{{fg .Point.G2}}</td>
    <td class="mono">{{f2 .Point.PhaseDeg}}</td>
    {{if .Excluded}}
     <td class="l warn">已排除：{{.Reason}}（{{if eq .Source "user"}}手工{{else}}自动{{end}}）</td>
     <td>{{if eq .Source "user"}}<form method="post" action="/include" style="display:inline"><input type="hidden" name="point_id" value="{{.Point.ID}}"><button type="submit">恢复</button></form>{{end}}</td>
    {{else}}
     <td class="l ok">有效</td>
     <td>
      <form method="post" action="/exclude" style="display:inline">
       <input type="hidden" name="point_id" value="{{.Point.ID}}">
       <input type="text" name="reason" value="超出线性黏弹区" style="width:130px">
       <button type="submit">排除</button>
      </form>
     </td>
    {{end}}
   </tr>
   {{end}}
  </table>
  {{end}}
 </details>
</section>

</main>
</body>
</html>
`

// templateHTMLType is trusted, locally generated markup (SVG plots).
type templateHTMLType = template.HTML
