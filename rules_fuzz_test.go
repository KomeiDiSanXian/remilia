//go:build go1.18
// +build go1.18

package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// FuzzOnRegex 模糊测试正则表达式规则
//
// 测试目标：
//   - OnRegexSafe 不应该 panic
//   - 无效的正则应该返回错误
//   - 有效的正则应该正常编译
func FuzzOnRegex(f *testing.F) {
	// 种子语料 - 各种正则表达式模式
	f.Add("[a-z]+")
	f.Add("\\d{3}-\\d{4}")
	f.Add(".*")
	f.Add("^test$")
	f.Add("[0-9a-zA-Z]+")
	f.Add("(hello|world)")
	f.Add("[[:alpha:]]+")
	f.Add("\\s*")
	f.Add("(?i)case")
	f.Add("")
	f.Add("[")         // 无效的正则
	f.Add("(")         // 无效的正则
	f.Add("(?P<name>") // 无效的正则

	f.Fuzz(func(t *testing.T, pattern string) {
		// 测试 OnRegexSafe 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("OnRegexSafe panicked with pattern %q: %v", pattern, r)
			}
		}()

		rule, err := OnRegexSafe(pattern)

		// 如果有错误，应该是无效的正则表达式
		if err != nil {
			// 错误是预期的，不是 bug
			return
		}

		// 如果没有错误，规则应该可以使用
		if rule == nil {
			t.Errorf("OnRegexSafe returned nil rule for valid pattern %q", pattern)
			return
		}

		// 测试规则可以应用
		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "fuzz-test",
		}
		ctx := NewContext(event, nil)
		// 规则执行不应该 panic
		_ = rule(ctx)
	})
}

// FuzzOnCommand 模糊测试命令规则
//
// 测试目标：
//   - OnCommand 不应该 panic
//   - 各种命令前缀都应该正常处理
//   - 空字符串和特殊字符应该被正确处理
func FuzzOnCommand(f *testing.F) {
	// 种子语料 - 各种命令前缀
	f.Add("/ping")
	f.Add("/help")
	f.Add("")
	f.Add("/test with spaces")
	f.Add("!")
	f.Add("#")
	f.Add("@")
	f.Add("/")
	f.Add("//")
	f.Add("/test/nested")
	f.Add("中文命令")
	f.Add("/emoji🎉")
	f.Add("/\n")
	f.Add("/\t")
	f.Add("/\r\n")

	f.Fuzz(func(t *testing.T, prefix string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("OnCommand panicked with prefix %q: %v", prefix, r)
			}
		}()

		rule := OnCommand(prefix)

		// 规则应该被创建
		if rule == nil {
			t.Errorf("OnCommand returned nil rule for prefix %q", prefix)
			return
		}

		// 测试规则可以应用到各种消息内容
		testMessages := []string{
			prefix + " hello",
			prefix,
			prefix + "test",
			"  " + prefix,
			prefix + "\n",
			"",
		}

		for _, content := range testMessages {
			event := &dto.Payload{
				Type: dto.C2CMessageCreate,
				ID:   "fuzz-test",
			}
			ctx := NewContext(event, nil)
			ctx.SetState("message_content", content) // 模拟消息内容

			// 规则执行不应该 panic
			_ = rule(ctx)
		}
	})
}

// FuzzOnKeyword 模糊测试关键词规则
//
// 测试目标：
//   - OnKeyword 不应该 panic
//   - 各种关键词都应该正常处理
//   - Unicode 字符应该被正确处理
func FuzzOnKeyword(f *testing.F) {
	// 种子语料
	f.Add("hello")
	f.Add("test")
	f.Add("")
	f.Add("中文")
	f.Add("🎉")
	f.Add("hello world")
	f.Add("\n")
	f.Add("\t")
	f.Add("test\ntest")
	f.Add("   ")
	f.Add("a")
	f.Add(string([]byte{0x00})) // null byte

	f.Fuzz(func(t *testing.T, keyword string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("OnKeyword panicked with keyword %q: %v", keyword, r)
			}
		}()

		rule := OnKeyword(keyword)

		// 规则应该被创建
		if rule == nil {
			t.Errorf("OnKeyword returned nil rule for keyword %q", keyword)
			return
		}

		// 测试规则应用
		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "fuzz-test",
		}
		ctx := NewContext(event, nil)

		// 规则执行不应该 panic
		_ = rule(ctx)
	})
}

// FuzzOnPrefix 模糊测试前缀规则
//
// 测试目标：
//   - OnPrefix 不应该 panic
//   - 各种前缀都应该正常处理
func FuzzOnPrefix(f *testing.F) {
	// 种子语料
	f.Add("!")
	f.Add("/")
	f.Add("#")
	f.Add("bot:")
	f.Add("")
	f.Add("@")
	f.Add(">>")
	f.Add("中文前缀")
	f.Add("🤖")
	f.Add("\n")

	f.Fuzz(func(t *testing.T, prefix string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("OnPrefix panicked with prefix %q: %v", prefix, r)
			}
		}()

		rule := OnPrefix(prefix)

		if rule == nil {
			t.Errorf("OnPrefix returned nil rule for prefix %q", prefix)
			return
		}

		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "fuzz-test",
		}
		ctx := NewContext(event, nil)
		_ = rule(ctx)
	})
}

// FuzzOnSuffix 模糊测试后缀规则
//
// 测试目标：
//   - OnSuffix 不应该 panic
//   - 各种后缀都应该正常处理
func FuzzOnSuffix(f *testing.F) {
	// 种子语料
	f.Add("?")
	f.Add("!")
	f.Add("...")
	f.Add("")
	f.Add("吗")
	f.Add("呢")
	f.Add("\n")
	f.Add("？")
	f.Add("！")

	f.Fuzz(func(t *testing.T, suffix string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("OnSuffix panicked with suffix %q: %v", suffix, r)
			}
		}()

		rule := OnSuffix(suffix)

		if rule == nil {
			t.Errorf("OnSuffix returned nil rule for suffix %q", suffix)
			return
		}

		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "fuzz-test",
		}
		ctx := NewContext(event, nil)
		_ = rule(ctx)
	})
}

// FuzzOnFullMatch 模糊测试完全匹配规则
//
// 测试目标：
//   - OnFullMatch 不应该 panic
//   - 各种完整消息都应该正常处理
func FuzzOnFullMatch(f *testing.F) {
	// 种子语料
	f.Add("ping")
	f.Add("help")
	f.Add("")
	f.Add("hello world")
	f.Add("中文消息")
	f.Add("emoji 🎉")
	f.Add("\n\n")
	f.Add("   ")

	f.Fuzz(func(t *testing.T, message string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("OnFullMatch panicked with message %q: %v", message, r)
			}
		}()

		rule := OnFullMatch(message)

		if rule == nil {
			t.Errorf("OnFullMatch returned nil rule for message %q", message)
			return
		}

		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "fuzz-test",
		}
		ctx := NewContext(event, nil)
		_ = rule(ctx)
	})
}

// FuzzOnMessageType 模糊测试消息类型处理
//
// 测试目标：
//   - 各种事件类型都应该正常处理
//   - Context 创建不应该 panic
func FuzzOnMessageType(f *testing.F) {
	// 种子语料
	f.Add(uint8(0))
	f.Add(uint8(1))
	f.Add(uint8(2))
	f.Add(uint8(10))
	f.Add(uint8(255))

	f.Fuzz(func(t *testing.T, typeNum uint8) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Message type handling panicked with type %d: %v", typeNum, r)
			}
		}()

		// 根据数字选择事件类型
		var eventType dto.EventType
		switch typeNum % 2 {
		case 0:
			eventType = dto.C2CMessageCreate
		case 1:
			eventType = dto.GroupAtMessageCreate
		}

		event := &dto.Payload{
			Type: eventType,
			ID:   "fuzz-test",
		}

		// 创建 Context 并测试基本操作
		ctx := NewContext(event, nil)
		if ctx == nil {
			t.Errorf("NewContext returned nil")
			return
		}

		// 测试 Context 状态操作
		ctx.SetState("test", "value")
		_, _ = ctx.GetState("test")
	})
}

// FuzzContextSetState 模糊测试 Context 状态设置
//
// 测试目标：
//   - SetState 不应该 panic
//   - 各种键值对都应该正常处理
func FuzzContextSetState(f *testing.F) {
	// 种子语料
	f.Add("key", "value")
	f.Add("", "")
	f.Add("中文键", "中文值")
	f.Add("emoji🎉", "value🎉")
	f.Add("key\n", "value\n")
	f.Add("key\t", "value\t")

	f.Fuzz(func(t *testing.T, key, value string) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SetState panicked with key=%q, value=%q: %v", key, value, r)
			}
		}()

		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "fuzz-test",
		}
		ctx := NewContext(event, nil)

		// 设置状态
		ctx.SetState(key, value)

		// 获取状态
		retrievedValue, ok := ctx.GetState(key)
		if ok {
			if retrievedValue != value {
				t.Errorf("GetState returned different value: expected %q, got %q", value, retrievedValue)
			}
		}

		// 删除状态
		ctx.DeleteState(key)
	})
}

// FuzzMatcherSetPriority 模糊测试 Matcher 优先级设置
//
// 测试目标：
//   - SetPriority 不应该 panic
//   - 各种优先级值都应该正常处理
func FuzzMatcherSetPriority(f *testing.F) {
	// 种子语料
	f.Add(uint(0))
	f.Add(uint(10))
	f.Add(uint(50))
	f.Add(uint(100))
	f.Add(uint(1000))
	f.Add(uint(^uint(0))) // 最大值

	f.Fuzz(func(t *testing.T, priority uint) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SetPriority panicked with priority %d: %v", priority, r)
			}
		}()

		engine := NewEngine()
		matcher := engine.OnC2C()

		// 设置优先级
		result := matcher.SetPriority(priority)

		// 应该返回自身
		if result != matcher {
			t.Errorf("SetPriority should return itself")
		}

		// 验证优先级被设置
		if matcher.Priority != priority {
			t.Errorf("Priority not set correctly: expected %d, got %d", priority, matcher.Priority)
		}
	})
}

// FuzzEngineProcessEvent 模糊测试 Engine 事件处理
//
// 测试目标：
//   - ProcessEvent 不应该 panic
//   - 各种事件 ID 和类型都应该正常处理
func FuzzEngineProcessEvent(f *testing.F) {
	// 种子语料
	f.Add("event-1", uint32(1))
	f.Add("", uint32(0))
	f.Add("very-long-event-id-with-many-characters", uint32(100))
	f.Add("中文事件ID", uint32(2))
	f.Add("event\nwith\nnewlines", uint32(3))

	f.Fuzz(func(t *testing.T, eventID string, eventTypeNum uint32) {
		// 不应该 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("ProcessEvent panicked with eventID=%q, type=%d: %v", eventID, eventTypeNum, r)
			}
		}()

		engine := NewEngine()

		// 注册一个简单的 Handler
		engine.OnAny().HandleE(func(ctx *Context) error {
			return nil
		})

		// 创建事件（使用预定义的事件类型）
		var eventType dto.EventType
		switch eventTypeNum % 2 {
		case 0:
			eventType = dto.C2CMessageCreate
		default:
			eventType = dto.GroupAtMessageCreate
		}

		event := &dto.Payload{
			Type: eventType,
			ID:   dto.EventID(eventID),
		}

		ctx := NewContext(event, nil)
		// 处理事件
		engine.ProcessEvent(ctx)
	})
}
