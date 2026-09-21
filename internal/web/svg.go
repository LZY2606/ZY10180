package web

import (
	"fmt"
	"html"
	"math"
	"strings"
)

// chartSeries is one drawable polyline with markers.
type chartSeries struct {
	Name           string
	Color          string
	Dash           bool
	X, Y           []float64
	CrossX, CrossY []float64 // excluded points drawn as red crosses
}

var palette = []string{"#1f77b4", "#d62728", "#2ca02c", "#9467bd", "#ff7f0e", "#17becf"}

// chartSVG renders a small log-log style chart (axes already in log10 units).
func chartSVG(title, xLabel, yLabel string, series []chartSeries) string {
	const w, h, padL, padR, padT, padB = 880.0, 380.0, 64.0, 16.0, 34.0, 44.0
	xMin, xMax := math.Inf(1), math.Inf(-1)
	yMin, yMax := math.Inf(1), math.Inf(-1)
	for _, s := range series {
		for i := range s.X {
			xMin = math.Min(xMin, s.X[i])
			xMax = math.Max(xMax, s.X[i])
			yMin = math.Min(yMin, s.Y[i])
			yMax = math.Max(yMax, s.Y[i])
		}
		for i := range s.CrossX {
			xMin = math.Min(xMin, s.CrossX[i])
			xMax = math.Max(xMax, s.CrossX[i])
			yMin = math.Min(yMin, s.CrossY[i])
			yMax = math.Max(yMax, s.CrossY[i])
		}
	}
	if xMin > xMax {
		xMin, xMax = 0, 1
	}
	if yMin > yMax {
		yMin, yMax = 0, 1
	}
	xMin, xMax = math.Floor(xMin), math.Ceil(xMax)
	yMin, yMax = math.Floor(yMin), math.Ceil(yMax)
	sx := func(x float64) float64 { return padL + (x-xMin)/(xMax-xMin)*(w-padL-padR) }
	sy := func(y float64) float64 { return h - padB - (y-yMin)/(yMax-yMin)*(h-padT-padB) }

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg viewBox="0 0 %.0f %.0f" width="%.0f" height="%.0f" xmlns="http://www.w3.org/2000/svg" font-family="sans-serif" font-size="12">`, w, h, w, h))
	b.WriteString(fmt.Sprintf(`<rect x="0" y="0" width="%.0f" height="%.0f" fill="#fff" stroke="#ccc"/>`, w, h))
	b.WriteString(fmt.Sprintf(`<text x="%.0f" y="22" text-anchor="middle" font-size="15" font-weight="bold">%s</text>`, w/2, html.EscapeString(title)))
	// grid + ticks
	for x := xMin; x <= xMax; x++ {
		b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#eee"/>`, sx(x), padT, sx(x), h-padB))
		b.WriteString(fmt.Sprintf(`<text x="%.1f" y="%.1f" text-anchor="middle" fill="#555">%g</text>`, sx(x), h-padB+16, x))
	}
	for y := yMin; y <= yMax; y++ {
		b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#eee"/>`, padL, sy(y), w-padR, sy(y)))
		b.WriteString(fmt.Sprintf(`<text x="%.1f" y="%.1f" text-anchor="end" fill="#555">%g</text>`, padL-6, sy(y)+4, y))
	}
	b.WriteString(fmt.Sprintf(`<text x="%.0f" y="%.0f" text-anchor="middle" fill="#333">%s</text>`, (padL+w-padR)/2, h-6, html.EscapeString(xLabel)))
	b.WriteString(fmt.Sprintf(`<text x="16" y="%.0f" text-anchor="middle" fill="#333" transform="rotate(-90 16 %.0f)">%s</text>`, (padT+h-padB)/2, (padT+h-padB)/2, html.EscapeString(yLabel)))

	// series
	legendX := padL + 10
	for i, s := range series {
		if len(s.X) > 1 {
			var pts strings.Builder
			for j := range s.X {
				fmt.Fprintf(&pts, "%.1f,%.1f ", sx(s.X[j]), sy(s.Y[j]))
			}
			dash := ""
			if s.Dash {
				dash = ` stroke-dasharray="6 4"`
			}
			b.WriteString(fmt.Sprintf(`<polyline points="%s" fill="none" stroke="%s" stroke-width="1.6"%s/>`, pts.String(), s.Color, dash))
		}
		for j := range s.X {
			b.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="2.6" fill="%s"/>`, sx(s.X[j]), sy(s.Y[j]), s.Color))
		}
		for j := range s.CrossX {
			cx, cy := sx(s.CrossX[j]), sy(s.CrossY[j])
			b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#e00" stroke-width="1.8"/>`, cx-4, cy-4, cx+4, cy+4))
			b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#e00" stroke-width="1.8"/>`, cx-4, cy+4, cx+4, cy-4))
		}
		dash := ""
		if s.Dash {
			dash = ` stroke-dasharray="6 4"`
		}
		ly := padT + 14 + float64(i)*16
		b.WriteString(fmt.Sprintf(`<line x1="%.0f" y1="%.0f" x2="%.0f" y2="%.0f" stroke="%s" stroke-width="2"%s/>`, legendX, ly-4, legendX+22, ly-4, s.Color, dash))
		b.WriteString(fmt.Sprintf(`<text x="%.0f" y="%.0f" fill="#333">%s</text>`, legendX+26, ly, html.EscapeString(s.Name)))
	}
	b.WriteString(`</svg>`)
	return b.String()
}
