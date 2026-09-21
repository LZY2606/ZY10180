package rheo

import (
	"fmt"
	"math"
)

// CheckConsistency returns the auto-exclusion reason for a point, or "".
// tolDeg is the allowed deviation between the measured phase angle and
// atan2(G”, G'); it is a run parameter, never a hidden constant.
func CheckConsistency(p Point, tolDeg float64) string {
	if p.FreqHz <= 0 {
		return fmt.Sprintf("频率非正 (%.6g Hz)，拒绝入谱", p.FreqHz)
	}
	if p.G1 <= 0 || p.G2 <= 0 {
		return fmt.Sprintf("模量非正 (G'=%.4g, G''=%.4g)，无法进入对数轴", p.G1, p.G2)
	}
	want := math.Atan2(p.G2, p.G1) * 180 / math.Pi
	dev := math.Abs(p.PhaseDeg - want)
	if dev > tolDeg {
		return fmt.Sprintf("相位角与模量不一致：偏差 %.2f° 超容差 %.2f°", dev, tolDeg)
	}
	return ""
}
