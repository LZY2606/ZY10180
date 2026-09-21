package web

import "html/template"

var pageTmpl = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>流变叠时台</title>
<style>
body{font-family:-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;margin:24px;color:#222;max-width:960px}
h1{font-size:24px} h2{font-size:17px;margin-top:28px;border-bottom:1px solid #ddd;padding-bottom:4px}
table{border-collapse:collapse;font-size:13px;margin:8px 0}
th,td{border:1px solid #ccc;padding:4px 8px;text-align:right}
th{background:#f4f4f4}
td.l,th.l{text-align:left}
input[type=text],input[type=number]{width:90px;font-size:13px}
button{font-size:13px}
.warn{color:#b00}.ok{color:#080}
.mono{font-family:ui-monospace,monospace;font-size:12px}
form.inline{display:inline}
section{margin-bottom:8px}
.note{color:#666;font-size:12px}
</style>
</head>
<body>
<h1>流变叠时台</h1>
<p class="note">时温叠加（TTS）工作台：水平/竖直移位、WLF 与 Arrhenius 拟合、重叠区误差与一致性检查。手工偏移永不模型值覆盖。</p>

<h2>运行分析</h2>
<form method="post" action="/run">
<label>随机种子 <input type="number" name="seed" value="{{.SeedDefault}}" step="1"></label>
<label>相位容差(°) <input type="text" name="tol_deg" value="{{printf "%.2f" .TolDefault}}"></label>
<label>参考温度 <select name="ref_curve_id">
{{range .Curves}}<option value="{{.ID}}" {{if eq .ID $.RefID}}selected{{end}}>{{.Label}}（{{printf "%.1f" .TempC}}°C）</option>{{end}}
</select></label>
<button type="submit">运行分析</button>
</form>
{{if .Meta}}<p>最近运行 #{{.Meta.ID}}（{{.Meta.Created}}），种子 {{.Meta.Seed}}，容差 {{printf "%.2f" .Meta.TolDeg}}°，指纹 <span class="mono">{{.Meta.Fingerprint}}</span></p>{{else}}<p class="warn">尚未运行分析。</p>{{end}}
{{range .Fits}}<p class="{{if .OK}}ok{{else}}warn{{end}}">{{.Note}}</p>{{end}}

<h2>原始对数曲线</h2>
{{.RawSVG}}
<h2>叠加主曲线（手工值优先，其次求解值）</h2>
{{.ShiftSVG}}
<h2>相位角</h2>
{{.PhaseSVG}}

<h2>移位因子表</h2>
<table>
<tr><th class="l">曲线</th><th>温度(°C)</th><th>手工 log aT</th><th>手工 log bT</th><th>锁定</th><th></th><th>求解 log aT</th><th class="l">诊断</th><th>WLF 预测</th><th>WLF 残差</th><th>Arrhenius 预测</th><th>Arrhenius 残差</th></tr>
{{range .Rows}}
<tr>
<td class="l">{{.Curve.Label}}{{if .IsRef}}（参考）{{end}}</td>
<td>{{printf "%.1f" .Curve.TempC}}</td>
<form method="post" action="/state"><input type="hidden" name="curve_id" value="{{.Curve.ID}}">
<td><input type="text" name="manual_h" value="{{if .State.ManualHSet}}{{printf "%.4f" .State.ManualH}}{{end}}" placeholder="未设"></td>
<td><input type="text" name="manual_v" value="{{printf "%.4f" .State.ManualV}}"></td>
<td><input type="checkbox" name="locked" {{if .State.Locked}}checked{{end}}></td>
<td><button type="submit">保存</button></td>
</form>
<td>{{if .Shift}}{{if .Shift.SolvedOK}}{{printf "%.4f" .Shift.SolvedH}}{{else}}—{{end}}{{else}}—{{end}}</td>
<td class="l">{{if .Shift}}{{.Shift.Note}}{{end}}</td>
<td>{{if .PredW}}{{printf "%.4f" .PredW.Pred}}{{else}}—{{end}}</td>
<td>{{if and .PredW .PredW.HasResidual}}{{printf "%+.4f" .PredW.Residual}}{{else}}—{{end}}</td>
<td>{{if .PredA}}{{printf "%.4f" .PredA.Pred}}{{else}}—{{end}}</td>
<td>{{if and .PredA .PredA.HasResidual}}{{printf "%+.4f" .PredA.Residual}}{{else}}—{{end}}</td>
</tr>
{{end}}
</table>
<p class="note">残差 = 已知移位（手工或求解）− 模型预测；模型预测只读，不写回手工值。</p>

<h2>相邻温度重叠区误差</h2>
<table>
<tr><th class="l">温度对</th><th>重叠</th><th>RMS</th><th>样本数</th><th class="l">说明</th></tr>
{{range .Pairs}}<tr><td class="l">{{.Label}}</td><td>{{if .P.Overlap}}是{{else}}否{{end}}</td>
<td>{{if .P.Overlap}}{{printf "%.4f" .P.RMS}}{{else}}—{{end}}</td><td>{{if .P.Overlap}}{{.P.N}}{{else}}—{{end}}</td>
<td class="l">{{.P.Message}}</td></tr>{{end}}
</table>

<h2>排除点（理由可反查）</h2>
<table>
<tr><th>点 ID</th><th class="l">曲线</th><th>频率(Hz)</th><th class="l">来源</th><th class="l">理由</th><th></th></tr>
{{range .Exclusions}}<tr><td>{{.PointID}}</td><td class="l">{{.Curve}}</td><td>{{printf "%.4g" .Freq}}</td>
<td class="l">{{.Source}}</td><td class="l">{{.Reason}}</td>
<td>{{if .Restorable}}<form class="inline" method="post" action="/exclude"><input type="hidden" name="point_id" value="{{.PointID}}"><input type="hidden" name="op" value="restore"><button>恢复</button></form>{{end}}</td></tr>
{{else}}<tr><td colspan="6" class="l">无排除点</td></tr>{{end}}
</table>

<h2>数据点（可手工排除超出线性黏弹区的点）</h2>
{{range .Points}}
<h3>{{.Curve.Label}}</h3>
<table>
<tr><th>ID</th><th>频率(Hz)</th><th>G'(Pa)</th><th>G''(Pa)</th><th>相位(°)</th><th class="l">状态</th><th></th></tr>
{{range .Rows}}<tr>
<td>{{.P.ID}}</td><td>{{printf "%.4g" .P.FreqHz}}</td><td>{{printf "%.4g" .P.G1}}</td><td>{{printf "%.4g" .P.G2}}</td><td>{{printf "%.2f" .P.PhaseDeg}}</td>
<td class="l">{{if .P.UserExcluded}}<span class="warn">用户排除：{{.P.UserReason}}</span>{{else if .AutoReason}}<span class="warn">{{.AutoReason}}</span>{{else}}正常{{end}}</td>
<td>{{if .P.UserExcluded}}
<form class="inline" method="post" action="/exclude"><input type="hidden" name="point_id" value="{{.P.ID}}"><input type="hidden" name="op" value="restore"><button>恢复</button></form>
{{else}}
<form class="inline" method="post" action="/exclude"><input type="hidden" name="point_id" value="{{.P.ID}}"><input type="hidden" name="op" value="exclude"><input type="text" name="reason" value="超出线性黏弹区" style="width:130px"><button>排除</button></form>
{{end}}</td>
</tr>{{end}}
</table>
{{end}}

<h2>运行记录</h2>
<p><a href="/export" download>导出运行记录 (JSON)</a></p>
<form method="post" action="/reset" onsubmit="return confirm('清空数据库并重新导入 fixture？')"><button>清空并重新导入 fixture</button></form>
<table>
<tr><th>ID</th><th class="l">时间(UTC)</th><th>种子</th><th>容差(°)</th><th>参考曲线</th><th class="l">指纹</th></tr>
{{range .Runs}}<tr><td>{{.ID}}</td><td class="l">{{.Created}}</td><td>{{.Seed}}</td><td>{{printf "%.2f" .TolDeg}}</td><td>{{.RefCurveID}}</td><td class="mono l">{{.Fingerprint}}</td></tr>
{{else}}<tr><td colspan="6" class="l">暂无运行</td></tr>{{end}}
</table>
</body>
</html>`))
