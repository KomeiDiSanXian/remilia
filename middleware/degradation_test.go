package middleware

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// testContext creates a test context with the given event type
func testContext(eventType string) *remilia.Context {
	event := &dto.Payload{
		Type: dto.EventType(eventType),
	}
	return remilia.NewContext(event, nil)
}

func TestAdaptiveDegradation_DefaultConfig(t *testing.T) {
	deg := NewAdaptiveDegradation(DegradationConfig{})

	if deg.config.CPUThreshold != 80.0 {
		t.Errorf("Expected CPU threshold 80.0, got %f", deg.config.CPUThreshold)
	}
	if deg.config.MemoryThreshold != 85.0 {
		t.Errorf("Expected memory threshold 85.0, got %f", deg.config.MemoryThreshold)
	}
	if deg.GetLevel() != LevelNormal {
		t.Errorf("Expected initial level Normal, got %v", deg.GetLevel())
	}
}

func TestAdaptiveDegradation_ForceLevel(t *testing.T) {
	deg := NewAdaptiveDegradation(DegradationConfig{})

	// 测试强制设置级别
	deg.ForceLevel(LevelLight)
	if deg.GetLevel() != LevelLight {
		t.Errorf("Expected level Light, got %v", deg.GetLevel())
	}

	deg.ForceLevel(LevelModerate)
	if deg.GetLevel() != LevelModerate {
		t.Errorf("Expected level Moderate, got %v", deg.GetLevel())
	}

	deg.ForceLevel(LevelSevere)
	if deg.GetLevel() != LevelSevere {
		t.Errorf("Expected level Severe, got %v", deg.GetLevel())
	}

	deg.ForceLevel(LevelNormal)
	if deg.GetLevel() != LevelNormal {
		t.Errorf("Expected level Normal, got %v", deg.GetLevel())
	}
}

func TestAdaptiveDegradation_Middleware_NormalLevel(t *testing.T) {
	deg := NewAdaptiveDegradation(DegradationConfig{})

	executed := false
	handler := func(ctx *remilia.Context) error {
		executed = true
		return nil
	}

	mw := deg.Middleware()
	wrappedHandler := mw(handler)

	ctx := testContext("MESSAGE_CREATE")

	err := wrappedHandler(ctx)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !executed {
		t.Error("Handler should be executed in normal level")
	}

	stats := deg.Stats()
	if stats.TotalEvents != 1 {
		t.Errorf("Expected 1 total event, got %d", stats.TotalEvents)
	}
	if stats.DroppedEvents != 0 {
		t.Errorf("Expected 0 dropped events, got %d", stats.DroppedEvents)
	}
}

func TestAdaptiveDegradation_Middleware_LightLevel(t *testing.T) {
	deg := NewAdaptiveDegradation(DegradationConfig{
		Strategy: DegradationDrop,
	})

	deg.ForceLevel(LevelLight)

	tests := []struct {
		name      string
		eventType string
		shouldRun bool
	}{
		{"Low priority event", "UNKNOWN_EVENT", false}, // PriorityLow, will be dropped
		{"Normal message", "MESSAGE_CREATE", true},     // PriorityNormal, will pass
		{"High priority @message", "GROUP_AT_MESSAGE_CREATE", true},
		{"Private message", "C2C_MESSAGE_CREATE", true},
		{"Guild event", "GUILD_CREATE", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executed := false
			handler := func(ctx *remilia.Context) error {
				executed = true
				return nil
			}

			mw := deg.Middleware()
			wrappedHandler := mw(handler)

			ctx := testContext(tt.eventType)

			err := wrappedHandler(ctx)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if executed != tt.shouldRun {
				t.Errorf("Expected handler execution: %v, got: %v", tt.shouldRun, executed)
			}
		})
	}
}

func TestAdaptiveDegradation_Middleware_ModerateLevel(t *testing.T) {
	deg := NewAdaptiveDegradation(DegradationConfig{
		Strategy: DegradationDrop,
	})

	deg.ForceLevel(LevelModerate)

	tests := []struct {
		name      string
		eventType string
		shouldRun bool
	}{
		{"Normal message", "MESSAGE_CREATE", false},
		{"@message", "GROUP_AT_MESSAGE_CREATE", true},
		{"Private message", "C2C_MESSAGE_CREATE", true},
		{"Guild event", "GUILD_CREATE", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executed := false
			handler := func(ctx *remilia.Context) error {
				executed = true
				return nil
			}

			mw := deg.Middleware()
			wrappedHandler := mw(handler)

			ctx := testContext(tt.eventType)

			err := wrappedHandler(ctx)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if executed != tt.shouldRun {
				t.Errorf("Expected handler execution: %v, got: %v", tt.shouldRun, executed)
			}
		})
	}
}

func TestAdaptiveDegradation_Middleware_SevereLevel(t *testing.T) {
	deg := NewAdaptiveDegradation(DegradationConfig{
		Strategy: DegradationDrop,
	})

	deg.ForceLevel(LevelSevere)

	tests := []struct {
		name      string
		eventType string
		shouldRun bool
	}{
		{"Normal message", "MESSAGE_CREATE", false},
		{"@message", "GROUP_AT_MESSAGE_CREATE", false},
		{"Private message", "C2C_MESSAGE_CREATE", false},
		{"Guild create", "GUILD_CREATE", true},
		{"Guild delete", "GUILD_DELETE", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executed := false
			handler := func(ctx *remilia.Context) error {
				executed = true
				return nil
			}

			mw := deg.Middleware()
			wrappedHandler := mw(handler)

			ctx := testContext(tt.eventType)

			err := wrappedHandler(ctx)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if executed != tt.shouldRun {
				t.Errorf("Expected handler execution: %v, got: %v", tt.shouldRun, executed)
			}
		})
	}
}

func TestAdaptiveDegradation_Stats(t *testing.T) {
	deg := NewAdaptiveDegradation(DegradationConfig{
		Strategy: DegradationDrop,
	})

	deg.ForceLevel(LevelLight)

	handler := func(ctx *remilia.Context) error {
		return nil
	}

	mw := deg.Middleware()
	wrappedHandler := mw(handler)

	// 处理一些事件
	for i := 0; i < 10; i++ {
		ctx := testContext("UNKNOWN_EVENT") // 低优先级
		_ = wrappedHandler(ctx)
	}

	for i := 0; i < 5; i++ {
		ctx := testContext("C2C_MESSAGE_CREATE") // 高优先级
		_ = wrappedHandler(ctx)
	}

	stats := deg.Stats()

	if stats.TotalEvents != 15 {
		t.Errorf("Expected 15 total events, got %d", stats.TotalEvents)
	}

	// 低优先级事件应该被丢弃（LightLevel 丢弃 < PriorityNormal）
	if stats.DroppedEvents != 10 {
		t.Errorf("Expected 10 dropped events, got %d", stats.DroppedEvents)
	}

	expectedDropRate := 10.0 / 15.0 * 100
	if stats.DropRate < expectedDropRate-0.1 || stats.DropRate > expectedDropRate+0.1 {
		t.Errorf("Expected drop rate ~%.2f%%, got %.2f%%", expectedDropRate, stats.DropRate)
	}
}

func TestAdaptiveDegradation_Reset(t *testing.T) {
	deg := NewAdaptiveDegradation(DegradationConfig{})

	deg.ForceLevel(LevelLight)

	handler := func(ctx *remilia.Context) error {
		return nil
	}

	mw := deg.Middleware()
	wrappedHandler := mw(handler)

	// 处理一些事件
	for i := 0; i < 5; i++ {
		ctx := testContext("MESSAGE_CREATE")
		_ = wrappedHandler(ctx)
	}

	stats := deg.Stats()
	if stats.TotalEvents == 0 {
		t.Error("Expected non-zero total events before reset")
	}

	// 重置统计
	deg.Reset()

	stats = deg.Stats()
	if stats.TotalEvents != 0 {
		t.Errorf("Expected 0 total events after reset, got %d", stats.TotalEvents)
	}
	if stats.DroppedEvents != 0 {
		t.Errorf("Expected 0 dropped events after reset, got %d", stats.DroppedEvents)
	}
}

func TestAdaptiveDegradation_CustomPriorityClassifier(t *testing.T) {
	customClassifier := func(ctx *remilia.Context) EventPriority {
		// 自定义分类器：所有消息都是高优先级
		return PriorityHigh
	}

	deg := NewAdaptiveDegradation(DegradationConfig{
		Strategy:           DegradationDrop,
		PriorityClassifier: customClassifier,
	})

	deg.ForceLevel(LevelLight) // 轻度降级，应该只丢弃低优先级

	executed := false
	handler := func(ctx *remilia.Context) error {
		executed = true
		return nil
	}

	mw := deg.Middleware()
	wrappedHandler := mw(handler)

	ctx := testContext("MESSAGE_CREATE")

	err := wrappedHandler(ctx)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// 因为自定义分类器将所有消息设为高优先级，所以不应被丢弃
	if !executed {
		t.Error("Handler should be executed with custom high priority")
	}
}

func TestAdaptiveDegradation_OnLevelChange(t *testing.T) {
	callbackCalled := false
	var from, to DegradationLevel

	deg := NewAdaptiveDegradation(DegradationConfig{
		OnLevelChange: func(f, t DegradationLevel) {
			callbackCalled = true
			from = f
			to = t
		},
	})

	deg.ForceLevel(LevelLight)

	if !callbackCalled {
		t.Error("OnLevelChange callback should be called")
	}
	if from != LevelNormal {
		t.Errorf("Expected from level Normal, got %v", from)
	}
	if to != LevelLight {
		t.Errorf("Expected to level Light, got %v", to)
	}
}

func TestAdaptiveDegradation_DelayStrategy(t *testing.T) {
	deg := NewAdaptiveDegradation(DegradationConfig{
		Strategy: DegradationDelay,
	})

	deg.ForceLevel(LevelLight)

	executed := false
	handler := func(ctx *remilia.Context) error {
		executed = true
		return nil
	}

	mw := deg.Middleware()
	wrappedHandler := mw(handler)

	ctx := testContext("MESSAGE_CREATE") // 普通优先级

	start := time.Now()
	err := wrappedHandler(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !executed {
		t.Error("Handler should be executed even with delay strategy")
	}

	// 应该有延迟
	if elapsed < 90*time.Millisecond {
		t.Errorf("Expected delay of at least 90ms, got %v", elapsed)
	}

	stats := deg.Stats()
	if stats.DelayedEvents != 1 {
		t.Errorf("Expected 1 delayed event, got %d", stats.DelayedEvents)
	}
}

func TestAdaptiveDegradation_SimplifyStrategy(t *testing.T) {
	deg := NewAdaptiveDegradation(DegradationConfig{
		Strategy: DegradationSimplify,
	})

	deg.ForceLevel(LevelLight)

	handler := func(ctx *remilia.Context) error {
		// 业务逻辑可以检查 degraded 标记并简化处理
		if IsDegraded(ctx) {
			return nil // 简化处理
		}
		return nil
	}

	mw := deg.Middleware()
	wrappedHandler := mw(handler)

	ctx := testContext("MESSAGE_CREATE")

	err := wrappedHandler(ctx)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// 检查 degraded 标记是否被设置（typed extension 优先）
	if !IsDegraded(ctx) {
		t.Error("Expected degraded flag to be set")
	}
}

func TestAdaptiveDegradation_CalculateLevel(t *testing.T) {
	deg := NewAdaptiveDegradation(DegradationConfig{
		CPUThreshold:    80.0,
		MemoryThreshold: 85.0,
	})

	tests := []struct {
		name       string
		cpu        float64
		memory     float64
		goroutines int
		expected   DegradationLevel
	}{
		{"Normal load", 50.0, 60.0, 100, LevelNormal},       // score = 0
		{"Light load", 105.0, 105.0, 100, LevelLight},       // score = (25*0.4 + 20*0.4) = 10 + 8 = 18
		{"Moderate load", 110.0, 110.0, 100, LevelModerate}, // score = (30*0.4 + 25*0.4) = 12 + 10 = 22
		{"Severe load", 120.0, 120.0, 100, LevelSevere},     // score = (40*0.4 + 35*0.4) = 16 + 14 = 30
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := deg.calculateLevel(tt.cpu, tt.memory, tt.goroutines)
			if level != tt.expected {
				t.Errorf("Expected level %v, got %v", tt.expected, level)
			}
		})
	}
}

func TestDefaultPriorityClassifier(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		expected  EventPriority
	}{
		{"Normal message", "MESSAGE_CREATE", PriorityNormal},
		{"@message", "GROUP_AT_MESSAGE_CREATE", PriorityHigh},
		{"Private message", "C2C_MESSAGE_CREATE", PriorityHigh},
		{"Guild create", "GUILD_CREATE", PriorityCritical},
		{"Guild delete", "GUILD_DELETE", PriorityCritical},
		{"Group add robot", "GROUP_ADD_ROBOT", PriorityCritical},
		{"Group del robot", "GROUP_DEL_ROBOT", PriorityCritical},
		{"Interaction", "INTERACTION_CREATE", PriorityHigh},
		{"Unknown event", "UNKNOWN_EVENT", PriorityLow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testContext(tt.eventType)

			priority := defaultPriorityClassifier(ctx)
			if priority != tt.expected {
				t.Errorf("Expected priority %v, got %v", tt.expected, priority)
			}
		})
	}
}

// 基准测试
func BenchmarkAdaptiveDegradation_Normal(b *testing.B) {
	deg := NewAdaptiveDegradation(DegradationConfig{})

	handler := func(ctx *remilia.Context) error {
		return nil
	}

	mw := deg.Middleware()
	wrappedHandler := mw(handler)

	ctx := testContext("MESSAGE_CREATE")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = wrappedHandler(ctx)
	}
}

func BenchmarkAdaptiveDegradation_Degraded(b *testing.B) {
	deg := NewAdaptiveDegradation(DegradationConfig{
		Strategy: DegradationDrop,
	})
	deg.ForceLevel(LevelLight)

	handler := func(ctx *remilia.Context) error {
		return nil
	}

	mw := deg.Middleware()
	wrappedHandler := mw(handler)

	ctx := testContext("MESSAGE_CREATE")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = wrappedHandler(ctx)
	}
}
