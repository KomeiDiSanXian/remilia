package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewMetricsCollector tests creating a new metrics collector
func TestNewMetricsCollector(t *testing.T) {
	tests := []struct {
		name              string
		namespace         string
		expectedNamespace string
	}{
		{
			name:              "with custom namespace",
			namespace:         "test_app",
			expectedNamespace: "test_app",
		},
		{
			name:              "with empty namespace",
			namespace:         "",
			expectedNamespace: "remilia",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := NewMetricsCollector(tt.namespace)
			require.NotNil(t, collector)
			assert.Equal(t, tt.expectedNamespace, collector.namespace)

			// Verify all metrics are initialized
			assert.NotNil(t, collector.deadLetterQueueSize)
			assert.NotNil(t, collector.deadLetterConsumed)
			assert.NotNil(t, collector.deadLetterConsumerTime)
			assert.NotNil(t, collector.pluginHandlers)
			assert.NotNil(t, collector.pluginMatchers)
			assert.NotNil(t, collector.pluginLoadTime)
			assert.NotNil(t, collector.pluginUnloadTime)
			assert.NotNil(t, collector.retryAttempts)
			assert.NotNil(t, collector.retrySuccesses)
			assert.NotNil(t, collector.retryFailures)
			assert.NotNil(t, collector.retryDelay)
			assert.NotNil(t, collector.eventProcessed)
			assert.NotNil(t, collector.eventDropped)
			assert.NotNil(t, collector.eventLatency)
		})
	}
}

// TestSetDeadLetterQueueSize tests setting dead letter queue size
func TestSetDeadLetterQueueSize(t *testing.T) {
	collector := NewMetricsCollector("test_" + t.Name())

	tests := []struct {
		name string
		size int
	}{
		{"zero size", 0},
		{"small size", 10},
		{"large size", 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector.SetDeadLetterQueueSize(tt.size)

			// Verify the gauge value
			value := testutil.ToFloat64(collector.deadLetterQueueSize)
			assert.Equal(t, float64(tt.size), value)
		})
	}
}

// TestRecordDeadLetterConsumed tests recording dead letter consumption
func TestRecordDeadLetterConsumed(t *testing.T) {
	collector := NewMetricsCollector("test_" + t.Name())

	// Record initial value
	initialCount := testutil.ToFloat64(collector.deadLetterConsumed)

	// Record consumption
	duration := 100 * time.Millisecond
	collector.RecordDeadLetterConsumed(duration)

	// Verify counter incremented
	newCount := testutil.ToFloat64(collector.deadLetterConsumed)
	assert.Equal(t, initialCount+1, newCount)

	// Record multiple consumptions
	for i := 0; i < 5; i++ {
		collector.RecordDeadLetterConsumed(50 * time.Millisecond)
	}

	finalCount := testutil.ToFloat64(collector.deadLetterConsumed)
	assert.Equal(t, initialCount+6, finalCount)
}

// TestSetPluginHandlers tests setting plugin handler count
func TestSetPluginHandlers(t *testing.T) {
	collector := NewMetricsCollector("test_" + t.Name())

	tests := []struct {
		plugin string
		count  int
	}{
		{"plugin1", 5},
		{"plugin2", 10},
		{"plugin3", 0},
	}

	for _, tt := range tests {
		t.Run(tt.plugin, func(t *testing.T) {
			collector.SetPluginHandlers(tt.plugin, tt.count)

			value := testutil.ToFloat64(collector.pluginHandlers.WithLabelValues(tt.plugin))
			assert.Equal(t, float64(tt.count), value)
		})
	}
}

// TestSetPluginMatchers tests setting plugin matcher count
func TestSetPluginMatchers(t *testing.T) {
	collector := NewMetricsCollector("test_" + t.Name())

	collector.SetPluginMatchers("plugin1", 3)
	collector.SetPluginMatchers("plugin2", 7)

	value1 := testutil.ToFloat64(collector.pluginMatchers.WithLabelValues("plugin1"))
	value2 := testutil.ToFloat64(collector.pluginMatchers.WithLabelValues("plugin2"))

	assert.Equal(t, float64(3), value1)
	assert.Equal(t, float64(7), value2)
}

// TestRecordPluginLoad tests recording plugin load time
func TestRecordPluginLoad(t *testing.T) {
	collector := NewMetricsCollector("test_plugin_load")

	duration := 150 * time.Millisecond
	collector.RecordPluginLoad("test-plugin", duration)

	// Verify histogram recorded the value
	count := testutil.CollectAndCount(collector.pluginLoadTime)
	assert.Greater(t, count, 0)
}

// TestRecordPluginUnload tests recording plugin unload time
func TestRecordPluginUnload(t *testing.T) {
	collector := NewMetricsCollector("test_" + t.Name())

	duration := 50 * time.Millisecond
	collector.RecordPluginUnload("test-plugin", duration)

	count := testutil.CollectAndCount(collector.pluginUnloadTime)
	assert.Greater(t, count, 0)
}

// TestFormatAttempt tests retry attempt label formatting
func TestFormatAttempt(t *testing.T) {
	tests := []struct {
		name     string
		attempt  int
		expected string
	}{
		{"negative", -1, "0"},
		{"zero", 0, "0"},
		{"one", 1, "1"},
		{"five", 5, "5"},
		{"nine", 9, "9"},
		{"ten", 10, "10+"},
		{"eleven", 11, "10+"},
		{"large", 100, "10+"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatAttempt(tt.attempt)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRecordRetryAttempt tests recording retry attempts
func TestRecordRetryAttempt(t *testing.T) {
	collector := NewMetricsCollector("test_" + t.Name())

	// Record various retry attempts
	collector.RecordRetryAttempt(1, 100*time.Millisecond)
	collector.RecordRetryAttempt(2, 200*time.Millisecond)
	collector.RecordRetryAttempt(3, 400*time.Millisecond)
	collector.RecordRetryAttempt(10, 1*time.Second)

	// Verify counters incremented
	value1 := testutil.ToFloat64(collector.retryAttempts.WithLabelValues("1"))
	value2 := testutil.ToFloat64(collector.retryAttempts.WithLabelValues("2"))
	value3 := testutil.ToFloat64(collector.retryAttempts.WithLabelValues("3"))
	value10 := testutil.ToFloat64(collector.retryAttempts.WithLabelValues("10+"))

	assert.Equal(t, float64(1), value1)
	assert.Equal(t, float64(1), value2)
	assert.Equal(t, float64(1), value3)
	assert.Equal(t, float64(1), value10)
}

// TestRecordRetrySuccess tests recording retry success
func TestRecordRetrySuccess(t *testing.T) {
	collector := NewMetricsCollector("test_" + t.Name())

	initial := testutil.ToFloat64(collector.retrySuccesses)

	collector.RecordRetrySuccess()
	collector.RecordRetrySuccess()
	collector.RecordRetrySuccess()

	final := testutil.ToFloat64(collector.retrySuccesses)
	assert.Equal(t, initial+3, final)
}

// TestRecordRetryFailure tests recording retry failure
func TestRecordRetryFailure(t *testing.T) {
	collector := NewMetricsCollector("test_" + t.Name())

	initial := testutil.ToFloat64(collector.retryFailures)

	collector.RecordRetryFailure()
	collector.RecordRetryFailure()

	final := testutil.ToFloat64(collector.retryFailures)
	assert.Equal(t, initial+2, final)
}

// TestRecordEventDropped tests recording dropped events
func TestRecordEventDropped(t *testing.T) {
	collector := NewMetricsCollector("test_" + t.Name())

	reasons := []string{"timeout", "invalid", "overload"}

	for _, reason := range reasons {
		collector.RecordEventDropped(reason)
	}

	// Verify each reason was recorded
	for _, reason := range reasons {
		value := testutil.ToFloat64(collector.eventDropped.WithLabelValues(reason))
		assert.Equal(t, float64(1), value)
	}

	// Record same reason multiple times
	collector.RecordEventDropped("timeout")
	collector.RecordEventDropped("timeout")

	value := testutil.ToFloat64(collector.eventDropped.WithLabelValues("timeout"))
	assert.Equal(t, float64(3), value)
}

// TestRecordEventProcessed tests recording processed events
func TestRecordEventProcessed(t *testing.T) {
	collector := NewMetricsCollector("test_" + t.Name())

	tests := []struct {
		eventType string
		source    string
		duration  time.Duration
	}{
		{"MESSAGE_CREATE", "global", 50 * time.Millisecond},
		{"MESSAGE_DELETE", "plugin", 30 * time.Millisecond},
		{"GUILD_CREATE", "global", 100 * time.Millisecond},
	}

	for _, tt := range tests {
		collector.RecordEventProcessed(tt.eventType, tt.source, tt.duration)

		value := testutil.ToFloat64(collector.eventProcessed.WithLabelValues(tt.eventType, tt.source))
		assert.Equal(t, float64(1), value)
	}

	// Record same event multiple times
	collector.RecordEventProcessed("MESSAGE_CREATE", "global", 60*time.Millisecond)

	value := testutil.ToFloat64(collector.eventProcessed.WithLabelValues("MESSAGE_CREATE", "global"))
	assert.Equal(t, float64(2), value)
}

// TestGetPoolMetrics tests getting pool metrics snapshot
func TestGetPoolMetrics(t *testing.T) {
	collector := NewMetricsCollector("test_" + t.Name())

	t.Run("initial state", func(t *testing.T) {
		snapshot := collector.GetPoolMetrics()
		assert.Equal(t, uint64(0), snapshot.Gets)
		assert.Equal(t, uint64(0), snapshot.News)
		assert.Equal(t, 0.0, snapshot.HitRate)
	})

	t.Run("with gets but no news", func(t *testing.T) {
		collector.internalPoolGets.Store(100)
		collector.internalPoolNews.Store(0)

		snapshot := collector.GetPoolMetrics()
		assert.Equal(t, uint64(100), snapshot.Gets)
		assert.Equal(t, uint64(0), snapshot.News)
		assert.Equal(t, 1.0, snapshot.HitRate)
	})

	t.Run("with gets and news", func(t *testing.T) {
		collector.internalPoolGets.Store(100)
		collector.internalPoolNews.Store(20)

		snapshot := collector.GetPoolMetrics()
		assert.Equal(t, uint64(100), snapshot.Gets)
		assert.Equal(t, uint64(20), snapshot.News)
		assert.Equal(t, 0.8, snapshot.HitRate)
	})

	t.Run("all news (0% hit rate)", func(t *testing.T) {
		collector.internalPoolGets.Store(50)
		collector.internalPoolNews.Store(50)

		snapshot := collector.GetPoolMetrics()
		assert.Equal(t, uint64(50), snapshot.Gets)
		assert.Equal(t, uint64(50), snapshot.News)
		assert.Equal(t, 0.0, snapshot.HitRate)
	})
}

// TestEventDroppedCounter tests backward compatibility method
func TestEventDroppedCounter(t *testing.T) {
	collector := NewMetricsCollector("test_" + t.Name())

	counter := collector.EventDroppedCounter()
	require.NotNil(t, counter)

	// Verify it's the same as internal eventDropped
	assert.Equal(t, collector.eventDropped, counter)

	// Verify it can be used
	counter.WithLabelValues("test-reason").Inc()
	value := testutil.ToFloat64(counter.WithLabelValues("test-reason"))
	assert.Equal(t, float64(1), value)
}

// TestMetricsIntegration tests integration of multiple metrics
func TestMetricsIntegration(t *testing.T) {
	collector := NewMetricsCollector("integration_test")

	// Simulate a full workflow

	// 1. Set dead letter queue size
	collector.SetDeadLetterQueueSize(5)

	// 2. Record plugin load
	collector.RecordPluginLoad("test-plugin", 200*time.Millisecond)
	collector.SetPluginHandlers("test-plugin", 3)
	collector.SetPluginMatchers("test-plugin", 2)

	// 3. Process events
	collector.RecordEventProcessed("MESSAGE_CREATE", "global", 50*time.Millisecond)
	collector.RecordEventProcessed("MESSAGE_CREATE", "plugin", 80*time.Millisecond)

	// 4. Record retry attempts
	collector.RecordRetryAttempt(1, 100*time.Millisecond)
	collector.RecordRetryAttempt(2, 200*time.Millisecond)
	collector.RecordRetrySuccess()

	// 5. Record dropped event
	collector.RecordEventDropped("timeout")

	// 6. Consume dead letter
	collector.RecordDeadLetterConsumed(150 * time.Millisecond)

	// Verify all metrics are recorded correctly
	assert.Equal(t, float64(5), testutil.ToFloat64(collector.deadLetterQueueSize))
	assert.Equal(t, float64(3), testutil.ToFloat64(collector.pluginHandlers.WithLabelValues("test-plugin")))
	assert.Equal(t, float64(2), testutil.ToFloat64(collector.pluginMatchers.WithLabelValues("test-plugin")))
	assert.Equal(t, float64(1), testutil.ToFloat64(collector.eventProcessed.WithLabelValues("MESSAGE_CREATE", "global")))
	assert.Equal(t, float64(1), testutil.ToFloat64(collector.retrySuccesses))
	assert.Equal(t, float64(1), testutil.ToFloat64(collector.eventDropped.WithLabelValues("timeout")))
	assert.Equal(t, float64(1), testutil.ToFloat64(collector.deadLetterConsumed))
}

// TestMetricsLabels tests that labels are properly set
func TestMetricsLabels(t *testing.T) {
	collector := NewMetricsCollector("test_" + t.Name())

	// Test different label combinations
	collector.RecordEventProcessed("TYPE_A", "source1", 10*time.Millisecond)
	collector.RecordEventProcessed("TYPE_A", "source2", 20*time.Millisecond)
	collector.RecordEventProcessed("TYPE_B", "source1", 30*time.Millisecond)

	// Verify each label combination
	assert.Equal(t, float64(1), testutil.ToFloat64(collector.eventProcessed.WithLabelValues("TYPE_A", "source1")))
	assert.Equal(t, float64(1), testutil.ToFloat64(collector.eventProcessed.WithLabelValues("TYPE_A", "source2")))
	assert.Equal(t, float64(1), testutil.ToFloat64(collector.eventProcessed.WithLabelValues("TYPE_B", "source1")))
}

// TestConcurrentMetrics tests concurrent metric updates
func TestConcurrentMetrics(t *testing.T) {
	collector := NewMetricsCollector("test_" + t.Name())

	done := make(chan bool)
	iterations := 100

	// Concurrent counter increments
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				collector.RecordRetrySuccess()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify final count
	final := testutil.ToFloat64(collector.retrySuccesses)
	assert.Equal(t, float64(10*iterations), final)
}

// BenchmarkNewMetricsCollector benchmarks collector creation
func BenchmarkNewMetricsCollector(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = NewMetricsCollector("bench")
	}
}

// BenchmarkRecordEventProcessed benchmarks event recording
func BenchmarkRecordEventProcessed(b *testing.B) {
	collector := NewMetricsCollector("bench")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		collector.RecordEventProcessed("TEST_EVENT", "global", 50*time.Millisecond)
	}
}

// BenchmarkRecordRetryAttempt benchmarks retry recording
func BenchmarkRecordRetryAttempt(b *testing.B) {
	collector := NewMetricsCollector("bench")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		collector.RecordRetryAttempt(i%10, 100*time.Millisecond)
	}
}

// BenchmarkGetPoolMetrics benchmarks pool metrics retrieval
func BenchmarkGetPoolMetrics(b *testing.B) {
	collector := NewMetricsCollector("bench")
	collector.internalPoolGets.Store(1000)
	collector.internalPoolNews.Store(200)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = collector.GetPoolMetrics()
	}
}

// BenchmarkConcurrentMetrics benchmarks concurrent metric updates
func BenchmarkConcurrentMetrics(b *testing.B) {
	collector := NewMetricsCollector("bench")

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			collector.RecordEventProcessed("BENCH_EVENT", "global", 10*time.Millisecond)
		}
	})
}
