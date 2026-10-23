package engine

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
)

// TestTempMatcherEvaluatesFirstRule 验证临时索引只按事件类型提供候选，
// 不做命令/正则预筛，因此临时 matcher 不能跳过 Rules[0]。否则一次性命令/
// 正则处理器会被同类事件的任意消息触发。
func TestTempMatcherEvaluatesFirstRule(t *testing.T) {
	m := &Matcher{
		Rules: []context.Rule{
			func(*context.Context) bool { return false }, // 模拟 OnCommand 前缀检查：不匹配
			func(*context.Context) bool { return true },
		},
	}
	m.commandIndexed.Store(true)

	// 临时索引：必须完整求值首条规则。
	atomic.StoreInt32(&m.rt.isTemp, 1)
	if m.Match(nil) {
		t.Fatal("temp matcher must evaluate Rules[0] and reject non-matching events")
	}

	// 普通 state 索引：已由 commandIndex 预筛，允许跳过首条规则（原有优化）。
	atomic.StoreInt32(&m.rt.isTemp, 0)
	if !m.Match(nil) {
		t.Fatal("indexed (non-temp) matcher may skip the prefixed first rule")
	}
}
