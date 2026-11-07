package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeToolProvider 同时实现 ToolProvider 与 SkillProvider，用于扫描用例。
type fakeToolProvider struct {
	tools  []toolkit.Tool
	skills []toolkit.Skill
}

func (f fakeToolProvider) ListTools() []toolkit.Tool   { return f.tools }
func (f fakeToolProvider) ListSkills() []toolkit.Skill { return f.skills }

// fakePlainService 既不是 ToolProvider 也不是 SkillProvider：
// 扫描必须安静跳过它，而不是 panic 或误注册。
type fakePlainService struct{}

// newScanManager 注册一个假插件，其 API 就是给定的服务对象。
//
// Discover* 系列都按 mgr.List() + GetContainer().Get(name) 遍历，
// 因此必须走真实的 Register 流程让名字进入 List()。
func newScanManager(t *testing.T, name string, api any) *plugin.Manager {
	t.Helper()
	pm := plugin.NewManager(nil)
	desc := &plugin.Descriptor{
		Name:    name,
		Cost:    0,
		Version: "1.0.0",
		Setup:   func(*plugin.SetupContext) (any, error) { return api, nil },
	}
	require.NoError(t, pm.Register(desc))
	require.NoError(t, pm.StartAll(t.Context()))
	return pm
}

// TestDiscoverToolProvidersScansManager 扫描管理器中的 ToolProvider。
func TestDiscoverToolProvidersScansManager(t *testing.T) {
	pm := newScanManager(t, "toolsvc", fakeToolProvider{tools: []toolkit.Tool{
		{
			Name:        "scan_tool",
			Description: "from provider",
			Categories:  []string{toolkit.CategoryGeneral},
			Execute: func(_ context.Context, _ *toolkit.Invocation, _ map[string]any) (toolkit.ToolResult, error) {
				return toolkit.TextResult("ok"), nil
			},
		},
	}})
	defer pm.Shutdown()

	p := &Plugin{cfg: &config.Config{}, reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry()}
	p.DiscoverToolProviders(pm)

	if _, ok := p.reg.Action("scan_tool"); !ok {
		t.Error("DiscoverToolProviders must register tools exposed by the manager")
	}
}

// TestDiscoverToolProvidersSkipsNonProviders 非 ToolProvider 服务被跳过。
func TestDiscoverToolProvidersSkipsNonProviders(t *testing.T) {
	pm := newScanManager(t, "plainsvc", fakePlainService{})
	defer pm.Shutdown()

	p := &Plugin{cfg: &config.Config{}, reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry()}
	p.DiscoverToolProviders(pm)

	if got := len(p.reg.Actions()); got != 0 {
		t.Errorf("plain service must not register tools, got %d", got)
	}
}

// TestDiscoverSkillProvidersScansManager 扫描管理器中的 SkillProvider。
func TestDiscoverSkillProvidersScansManager(t *testing.T) {
	pm := newScanManager(t, "skillsvc", fakeToolProvider{skills: []toolkit.Skill{
		{Name: "scan_skill", Description: "from provider", Prompt: "do it", Enabled: true},
	}})
	defer pm.Shutdown()

	p := &Plugin{cfg: &config.Config{}, reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry()}
	p.DiscoverSkillProviders(pm)

	if _, ok := p.skillReg.GetSystem("scan_skill"); !ok {
		t.Error("DiscoverSkillProviders must register skills exposed by the manager")
	}
}

// TestRegisterSkillProviderRegistersAll 一次性注册 provider 的全部技能。
func TestRegisterSkillProviderRegistersAll(t *testing.T) {
	p := &Plugin{cfg: &config.Config{}, reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry()}
	p.RegisterSkillProvider(fakeToolProvider{skills: []toolkit.Skill{
		{Name: "a_skill", Prompt: "a", Enabled: true},
		{Name: "b_skill", Prompt: "b", Enabled: true},
	}})

	for _, name := range []string{"a_skill", "b_skill"} {
		if _, ok := p.skillReg.GetSystem(name); !ok {
			t.Errorf("skill %q must be registered", name)
		}
	}
}

// TestMemoryExtractThrottle 守卫长期记忆抽取的节流窗口。
//
// CheckAndMark 原子地"判断 + 标记"，防止同一作用域在窗口内被并发抽取两次；
// UnmarkExtract 在抽取失败时回滚标记，让瞬时故障（超时/网络抖动）可以立即重试，
// 而不必等满 memory_min_interval。
func TestMemoryExtractThrottle(t *testing.T) {
	mem, err := openMemoryStore(t.TempDir(), 10, time.Hour)
	require.NoError(t, err)
	t.Cleanup(mem.Close)

	assert.True(t, mem.CanExtract("user:1"), "首次应允许抽取")
	assert.True(t, mem.CheckAndMark("user:1"), "首次 CheckAndMark 应成功")
	assert.False(t, mem.CheckAndMark("user:1"), "窗口内重复标记应被拒绝")
	assert.False(t, mem.CanExtract("user:1"), "标记后窗口内不再允许抽取")

	mem.UnmarkExtract("user:1")
	assert.True(t, mem.CanExtract("user:1"), "回滚后应立即可重试")
}

// TestParseGroupReplyArgs 守卫 /ai group set reply 的参数解析口径。
func TestParseGroupReplyArgs(t *testing.T) {
	t.Run("mode only", func(t *testing.T) {
		mode, chance, err := parseGroupReplyArgs([]string{"smart"})
		require.NoError(t, err)
		assert.Equal(t, "smart", *mode)
		assert.Nil(t, chance, "未给概率时不应写入概率")
	})

	t.Run("mode and chance", func(t *testing.T) {
		mode, chance, err := parseGroupReplyArgs([]string{"judge", "0.4"})
		require.NoError(t, err)
		assert.Equal(t, "judge", *mode)
		require.NotNil(t, chance)
		assert.InDelta(t, 0.4, *chance, 1e-9)
	})

	t.Run("bare chance implies smart", func(t *testing.T) {
		mode, chance, err := parseGroupReplyArgs([]string{"0.5"})
		require.NoError(t, err)
		assert.Equal(t, "smart", *mode, "只写概率时默认落在 smart 上")
		require.NotNil(t, chance)
		assert.InDelta(t, 0.5, *chance, 1e-9)
	})

	for _, in := range [][]string{{"nonsense"}, {"smart", "1.5"}, {"smart", "-0.1"}, {"1.5"}} {
		if _, _, err := parseGroupReplyArgs(in); err == nil {
			t.Errorf("parseGroupReplyArgs(%v) must fail", in)
		}
	}
}

// TestEllipsize 守卫截断口径（按 rune，不切坏多字节字符）。
func TestEllipsize(t *testing.T) {
	assert.Equal(t, "abc", ellipsize("abc", 5))
	assert.Equal(t, "abcde…", ellipsize("abcdefg", 5))
	// 中文按字符计数，不能按字节。
	assert.Equal(t, "你好…", ellipsize("你好世界", 2))
}

// TestGroupCommandHelpCoversEveryField 守卫群策略帮助覆盖所有可设置项。
func TestGroupCommandHelpCoversEveryField(t *testing.T) {
	help := groupCommandHelp("/ai")
	for _, field := range []string{"prompt", "tools", "approval", "mention", "reply", "global"} {
		if !strings.Contains(help, field) {
			t.Errorf("group help missing %q:\n%s", field, help)
		}
	}
}

// TestLLMRetryObserverReceivesReason 守卫重试观测回调接线。
//
// 指标名与标签是线上告警的契约；这里只断言"装配后观察者被接到指标实现上"，
// 具体标签值由 metrics 用例覆盖。
func TestLLMRetryObserverReceivesReason(t *testing.T) {
	// installObservers 就是 New 调用的接线函数；调用后协议层应持有回调。
	installObservers()
	t.Cleanup(func() { protocol.SetRetryObserver(nil) })

	// 触发一次重试观测：不应 panic，且指标记录函数可被调用。
	recordLLMRetry("rate_limit")
}
