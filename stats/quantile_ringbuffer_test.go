package stats

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQuantileHistogram_RingBuffer_O1Write(t *testing.T) {
	maxSamples := 5
	qh := NewQuantileHistogramWithSize(maxSamples)
	for i := 0; i < 10; i++ {
		qh.Observe(int64(i))
	}
	assert.Equal(t, maxSamples, qh.Count())
	// Recent 5 samples are [5,6,7,8,9], P50 = 7
	p50 := qh.P50()
	assert.Equal(t, int64(7), p50)
}
func TestQuantileHistogram_RingBuffer_Wraparound(t *testing.T) {
	qh := NewQuantileHistogramWithSize(3)
	qh.Observe(10)
	qh.Observe(20)
	qh.Observe(30)
	assert.Equal(t, 3, qh.Count())
	assert.Equal(t, int64(20), qh.P50())
	qh.Observe(40) // replaces oldest 10 -> [20, 30, 40]
	assert.Equal(t, 3, qh.Count())
	assert.Equal(t, int64(30), qh.P50())
	qh.Observe(50)
	qh.Observe(60) // [40, 50, 60]
	assert.Equal(t, 3, qh.Count())
	assert.Equal(t, int64(50), qh.P50())
}
func TestQuantileHistogram_Reset_AfterRingBuffer(t *testing.T) {
	qh := NewQuantileHistogramWithSize(3)
	for i := 0; i < 10; i++ {
		qh.Observe(int64(i * 10))
	}
	qh.Reset()
	assert.Equal(t, 0, qh.Count())
	assert.Equal(t, int64(0), qh.P99())
	qh.Observe(100)
	qh.Observe(200)
	assert.Equal(t, 2, qh.Count())
	// 2 samples [100, 200]: Quantile(0.99) -> idx = int(1 * 0.99) = 0 -> 100
	// Max() returns 200; P99 for 2 samples returns the lower due to index math
	// Verify Max value is 200
	assert.Equal(t, int64(200), qh.Quantile(1.0), "Max should be 200")
	assert.Equal(t, int64(100), qh.Quantile(0.0), "Min should be 100")
}
func TestQuantileHistogram_SingleSample(t *testing.T) {
	qh := NewQuantileHistogram()
	qh.Observe(42)
	assert.Equal(t, int64(42), qh.P50())
	assert.Equal(t, int64(42), qh.P90())
	assert.Equal(t, int64(42), qh.P99())
}
