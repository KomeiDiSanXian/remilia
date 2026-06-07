package iss

import "time"

// Trend 轨道高度趋势分析结果。
type Trend struct {
	Slope     float64 // 变化率 (km/天)，正值表示上升，负值表示下降
	MinAlt    float64 // 最低高度 (km)
	MaxAlt    float64 // 最高高度 (km)
}

// computeTrend 分析高度时间序列，计算最低、最高高度和线性变化率。
func computeTrend(records []AltRecord) Trend {
	n := len(records)
	if n == 0 {
		return Trend{}
	}

	minAlt := records[0].Altitude
	maxAlt := records[0].Altitude
	for _, r := range records {
		if r.Altitude < minAlt {
			minAlt = r.Altitude
		}
		if r.Altitude > maxAlt {
			maxAlt = r.Altitude
		}
	}

	if n < 3 {
		return Trend{MinAlt: minAlt, MaxAlt: maxAlt}
	}

	t0 := records[0].Time
	var sumX, sumY, sumXY, sumX2 float64
	nf := float64(n)
	for _, r := range records {
		x := float64(r.Time.Sub(t0)) / float64(time.Hour)
		y := r.Altitude
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	slope := (nf*sumXY - sumX*sumY) / (nf*sumX2 - sumX*sumX)

	return Trend{
		Slope:  slope * 24,
		MinAlt: minAlt,
		MaxAlt: maxAlt,
	}
}
