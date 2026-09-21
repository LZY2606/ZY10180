package web

import (
	"fmt"
	"html"
	"math"
	"strings"

	"ttsbench/internal/tts"
)

var palette = []string{"#1f6fb2", "#c9392c", "#2c8a3b", "#7d4fa3", "#b26a1f", "#1f8a8a"}

type plotPoint struct {
	x, y float64
}

type plotSeries struct {
	color  string
	dashed bool
	points []plotPoint
}

// renderPlot draws the log-log plot of G' and G” vs frequency. When
// shifted is true the manual shift factors are applied. Excluded points
// that are still plottable (positive moduli) are drawn as grey crosses.
func renderPlot(r *tts.Run, shifted bool) string {
	included := map[int64]bool{}
	excludedPlottable := map[int64]bool{}
	user := r.UserExclusionMap()
	for _, c := range r.Curves {
		for _, p := range c.Points {
			_, userEx := user[p.ID]
			autoReason := tts.AutoExclusionReason(p, r.Tolerance)
			switch {
			case userEx:
				excludedPlottable[p.ID] = p.G1 > 0 && p.G2 > 0 && p.Freq > 0
			case autoReason != "":
				excludedPlottable[p.ID] = p.G1 > 0 && p.G2 > 0 && p.Freq > 0
			default:
				included[p.ID] = true
			}
		}
	}

	var series []plotSeries
	var crosses []plotPoint
	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)
	observe := func(x, y float64) {
		minX = math.Min(minX, x)
		maxX = math.Max(maxX, x)
		minY = math.Min(minY, y)
		maxY = math.Max(maxY, y)
	}
	for _, c := range r.Curves {
		s := r.ShiftOf(c.ID)
		logA, logB := 0.0, 0.0
		if shifted {
			logA = s.LogA
			if s.UseVertical {
				logB = s.LogB
			}
		}
		var g1, g2 plotSeries
		g1.color = palette[int(c.ID-1)%len(palette)]
		g2.color = g1.color
		g2.dashed = true
		for _, p := range c.Points {
			if included[p.ID] {
				x := math.Log10(p.Freq) + logA
				y1 := math.Log10(p.G1) + logB
				y2 := math.Log10(p.G2) + logB
				g1.points = append(g1.points, plotPoint{x, y1})
				g2.points = append(g2.points, plotPoint{x, y2})
				observe(x, y1)
				observe(x, y2)
			} else if excludedPlottable[p.ID] {
				x := math.Log10(p.Freq) + logA
				y := math.Log10(p.G1) + logB
				crosses = append(crosses, plotPoint{x, y})
				observe(x, y)
			}
		}
		series = append(series, g1, g2)
	}
	if len(series) == 0 || math.IsInf(minX, 1) {
		return "<p>无可绘制数据。</p>"
	}
	x0 := math.Floor(minX) - 0.5
	x1 := math.Ceil(maxX) + 0.5
	y0 := math.Floor(minY) - 0.5
	y1 := math.Ceil(maxY) + 0.5

	const (
		W, H   = 760.0, 420.0
		mL, mR = 64.0, 16.0
		mT, mB = 16.0, 48.0
	)
	px := func(x float64) float64 { return mL + (x-x0)/(x1-x0)*(W-mL-mR) }
	py := func(y float64) float64 { return H - mB - (y-y0)/(y1-y0)*(H-mT-mB) }

	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %.0f %.0f" width="%.0f" height="%.0f" xmlns="http://www.w3.org/2000/svg" role="img">`, W, H, W, H)
	fmt.Fprintf(&b, `<rect x="0" y="0" width="%.0f" height="%.0f" fill="#fff" stroke="#ccc"/>`, W, H)
	// Grid + tick labels at each decade.
	for k := math.Ceil(x0); k <= x1; k++ {
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#eee"/>`, px(k), mT, px(k), H-mB)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="11" text-anchor="middle" fill="#555">10<tspan baseline-shift="super" font-size="8">%s</tspan></text>`,
			px(k), H-mB+16, html.EscapeString(formatExp(k)))
	}
	for k := math.Ceil(y0); k <= y1; k++ {
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#eee"/>`, mL, py(k), W-mR, py(k))
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="11" text-anchor="end" fill="#555">10<tspan baseline-shift="super" font-size="8">%s</tspan></text>`,
			mL-6, py(k)+4, html.EscapeString(formatExp(k)))
	}
	fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="none" stroke="#999"/>`,
		mL, mT, W-mL-mR, H-mT-mB)
	// Axis titles.
	xTitle := "log10 频率 ω / (rad·s⁻¹)"
	if shifted {
		xTitle = "log10 约化频率 ω·aT / (rad·s⁻¹)"
	}
	fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="12" text-anchor="middle" fill="#333">%s</text>`, W/2, H-6, xTitle)
	yTitle := "log10 模量 G′, G″ / Pa"
	if shifted {
		yTitle = "log10 约化模量 bT·G′, bT·G″ / Pa"
	}
	fmt.Fprintf(&b, `<text x="14" y="%.1f" font-size="12" text-anchor="middle" fill="#333" transform="rotate(-90 14 %.1f)">%s</text>`, H/2, H/2, yTitle)

	for _, s := range series {
		if len(s.points) == 0 {
			continue
		}
		var pts strings.Builder
		for _, p := range s.points {
			fmt.Fprintf(&pts, "%.1f,%.1f ", px(p.x), py(p.y))
		}
		dash := ""
		if s.dashed {
			dash = ` stroke-dasharray="5 4"`
		}
		fmt.Fprintf(&b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="1.6"%s/>`, pts.String(), s.color, dash)
		for _, p := range s.points {
			if s.dashed {
				fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="3.2" fill="#fff" stroke="%s" stroke-width="1.6"/>`, px(p.x), py(p.y), s.color)
			} else {
				fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="3.2" fill="%s"/>`, px(p.x), py(p.y), s.color)
			}
		}
	}
	for _, p := range crosses {
		x, y := px(p.x), py(p.y)
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#888" stroke-width="1.6"/>`, x-4, y-4, x+4, y+4)
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#888" stroke-width="1.6"/>`, x-4, y+4, x+4, y-4)
	}
	// Legend.
	lx, ly := mL+10, mT+14
	for _, c := range r.Curves {
		color := palette[int(c.ID-1)%len(palette)]
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="2"/>`, lx, ly-4, lx+22, ly-4, color)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="11" fill="#333">%s（%.0f °C）</text>`, lx+26, ly, html.EscapeString(c.Label), c.TempC)
		ly += 16
	}
	fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="10" fill="#777">实线/实心=G′，虚线/空心=G″，灰叉=已排除点</text>`, mL+10, ly)
	b.WriteString(`</svg>`)
	return b.String()
}

func formatExp(v float64) string {
	return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", v), "0"), ".")
}
