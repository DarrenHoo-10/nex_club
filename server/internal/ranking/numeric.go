package ranking

import "fmt"

const scoreScale int64 = 10000

func scale(v float64) int64 {
	if v <= 0 {
		return 0
	}
	if v >= 100 {
		return 100 * scoreScale
	}
	scaled := int64(v*float64(scoreScale) + 0.5)
	if scaled > 100*scoreScale {
		return 100 * scoreScale
	}
	return scaled
}

func recommendScaled(quality, heat, fresh int64) int64 {
	num := qualityWeight*quality + heatWeight*heat + freshWeight*fresh
	if num >= 0 {
		return (num + 50) / 100
	}
	return (num - 50) / 100
}

func formatScaled(v int64) string {
	if v < 0 {
		v = 0
	}
	if v > 100*scoreScale {
		v = 100 * scoreScale
	}
	return fmt.Sprintf("%d.%04d", v/scoreScale, v%scoreScale)
}
