package stats

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQuantileHistogram_Basic(t *testing.T) {
	qh := NewQuantileHistogram()

	// 观测 100 个值 1-100
	for i := int64(1); i <= 100; i++ {
		qh.Observe(i)
	}

	assert.Equal(t, 100, qh.Count())
	assert.InDelta(t, 50.0, float64(qh.P50()), 2.0)
	assert.InDelta(t, 90.0, float64(qh.P90()), 2.0)
	assert.InDelta(t, 95.0, float64(qh.P95()), 2.0)
	assert.InDelta(t, 99.0, float64(qh.P99()), 2.0)
}

func TestQuantileHistogram_Empty(t *testing.T) {
	qh := NewQuantileHistogram()

	assert.Equal(t, 0, qh.Count())
	assert.Equal(t, int64(0), qh.P50())
	assert.Equal(t, int64(0), qh.P99())
}

func TestQuantileHistogram_Overflow(t *testing.T) {
	maxSamples := 10
	qh := NewQuantileHistogramWithSize(maxSamples)

	// 写入超过 maxSamples 的数据
	for i := int64(1); i <= 20; i++ {
		qh.Observe(i)
	}

	// 样本数不超过 maxSamples
	assert.Equal(t, maxSamples, qh.Count())
}

func TestQuantileHistogram_Reset(t *testing.T) {
	qh := NewQuantileHistogram()
	qh.Observe(100)
	qh.Observe(200)
	assert.Equal(t, 2, qh.Count())

	qh.Reset()
	assert.Equal(t, 0, qh.Count())
	assert.Equal(t, int64(0), qh.P99())
}

func TestQuantileHistogram_ConcurrentSafe(t *testing.T) {
	qh := NewQuantileHistogram()
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(1)
		go func(v int64) {
			defer wg.Done()
			qh.Observe(v)
		}(int64(i))
	}
	wg.Wait()

	assert.Equal(t, 100, qh.Count())
	p99 := qh.P99()
	assert.True(t, p99 >= 90 && p99 <= 100, "P99=%d should be near 99", p99)
}

func TestQuantileHistogram_Quantile_Extremes(t *testing.T) {
	qh := NewQuantileHistogram()
	qh.Observe(5)
	qh.Observe(10)
	qh.Observe(15)

	assert.Equal(t, int64(5), qh.Quantile(0.0))
	assert.Equal(t, int64(15), qh.Quantile(1.0))
}
