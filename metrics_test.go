package remilia

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewMetricsCollector(t *testing.T) {
	mc := NewMetricsCollector("test_new_" + t.Name())
	assert.NotNil(t, mc)
	assert.Contains(t, mc.namespace, "test_new_")
}

func TestMetricsCollector_Pool(t *testing.T) {
	mc := NewMetricsCollector("test_pool_" + t.Name())

	// 注意：对象池已移除，此测试验证 GetPoolMetrics 不会 panic
	snapshot := mc.GetPoolMetrics()

	// 对象池移除后，统计数据应该为零或不可用
	// 这里只验证调用不会崩溃
	_ = snapshot
}

func TestMetricsCollector_DeadLetter(t *testing.T) {
	mc := NewMetricsCollector("test_deadletter_" + t.Name())

	// 设置队列大小
	mc.SetDeadLetterQueueSize(5)

	// 记录消费
	mc.RecordDeadLetterConsumed(100 * time.Millisecond)
	mc.RecordDeadLetterConsumed(200 * time.Millisecond)

	// 这里主要验证不会 panic
	// 实际的 Prometheus 指标需要在集成测试中验证
}

func TestMetricsCollector_Plugin(t *testing.T) {
	mc := NewMetricsCollector("test_plugin_" + t.Name())

	// 设置插件指标
	mc.SetPluginHandlers("plugin1", 5)
	mc.SetPluginMatchers("plugin1", 10)

	// 记录加载/卸载时间
	mc.RecordPluginLoad("plugin1", 50*time.Millisecond)
	mc.RecordPluginUnload("plugin1", 30*time.Millisecond)

	// 验证不会 panic
}

func TestMetricsCollector_Retry(t *testing.T) {
	mc := NewMetricsCollector("test_retry_" + t.Name())

	// 记录重试
	mc.RecordRetryAttempt(1, 100*time.Millisecond)
	mc.RecordRetryAttempt(2, 200*time.Millisecond)
	mc.RecordRetryAttempt(3, 400*time.Millisecond)

	// 记录结果
	mc.RecordRetrySuccess()
	mc.RecordRetryFailure()

	// 验证不会 panic
}

func TestMetricsCollector_Event(t *testing.T) {
	mc := NewMetricsCollector("test_event_" + t.Name())

	// 记录事件处理
	mc.RecordEventProcessed("C2C_MESSAGE_CREATE", "plugin:test", 10*time.Millisecond)
	mc.RecordEventProcessed("GROUP_AT_MESSAGE_CREATE", "global", 20*time.Millisecond)

	// 记录事件丢弃
	mc.RecordEventDropped("concurrency_limit")
	mc.RecordEventDropped("invalid_event")

	// 验证不会 panic
}

func TestFormatAttempt(t *testing.T) {
	tests := []struct {
		attempt  int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{5, "5"},
		{9, "9"},
		{10, "10+"},
		{15, "10+"},
		{100, "10+"},
		{-1, "0"},
	}

	for _, tt := range tests {
		t.Run(string(rune('0'+tt.attempt)), func(t *testing.T) {
			result := formatAttempt(tt.attempt)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEngine_SetMetricsCollector(t *testing.T) {
	engine := NewEngine()
	mc := NewMetricsCollector("test_engine_set_" + t.Name())

	engine.SetMetricsCollector(mc)

	retrieved := engine.GetMetricsCollector()
	assert.Equal(t, mc, retrieved)
}

func TestMetricsCollector_MultiplePlugins(t *testing.T) {
	mc := NewMetricsCollector("test_multipleplugins_" + t.Name())

	// 多个插件的指标
	plugins := []string{"plugin1", "plugin2", "plugin3"}
	for i, plugin := range plugins {
		mc.SetPluginHandlers(plugin, (i+1)*5)
		mc.SetPluginMatchers(plugin, (i+1)*10)
		mc.RecordPluginLoad(plugin, time.Duration(i+1)*10*time.Millisecond)
	}

	// 验证不会 panic
}

func TestMetricsCollector_LargeRetryAttempt(t *testing.T) {
	mc := NewMetricsCollector("test_largeretry_" + t.Name())

	// 测试大的重试次数
	mc.RecordRetryAttempt(100, time.Second)
	mc.RecordRetryAttempt(1000, time.Second)

	// 应该被格式化为 "10+"
	label := formatAttempt(100)
	assert.Equal(t, "10+", label)
}

func BenchmarkMetricsCollector_RecordEventProcessed(b *testing.B) {
	mc := NewMetricsCollector("bench")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mc.RecordEventProcessed("TEST_EVENT", "global", time.Millisecond)
	}
}
