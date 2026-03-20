// Package acl 提供独立的黑白名单（ACL）插件。
//
// 这是从 admin 插件中拆分出的独立 ACL 插件，可单独注册使用，
// 也可与 admin 插件一起使用（admin 插件会自动识别并使用本插件）。
//
// 功能：
//   - 黑名单/白名单模式切换
//   - 用户添加/移除/查询
//   - 规则函数，可直接用于 engine.On()
//   - 可选持久化（依赖 storage 插件）
//
// 使用示例:
//
//	pm.RegisterV2(acl.New())
//	aclPlugin := ctx.MustGet("acl").(*acl.Plugin)
//	engine.On(string(platform.EventKindGroupMessage), aclPlugin.Rule()).Handle(handler)
package acl

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/core/storage"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Mode ACL 模式
type Mode int

const (
	// ModeDisabled 关闭（所有用户都允许）
	ModeDisabled Mode = iota
	// ModeBlacklist 黑名单模式（列表中的用户被拦截）
	ModeBlacklist
	// ModeWhitelist 白名单模式（只有列表中的用户被允许）
	ModeWhitelist
)

func (m Mode) String() string {
	switch m {
	case ModeDisabled:
		return "disabled"
	case ModeBlacklist:
		return "blacklist"
	case ModeWhitelist:
		return "whitelist"
	default:
		return "unknown"
	}
}

// ParseMode 解析字符串为 Mode
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(s) {
	case "disabled", "off", "none":
		return ModeDisabled, nil
	case "blacklist", "black":
		return ModeBlacklist, nil
	case "whitelist", "white":
		return ModeWhitelist, nil
	default:
		return ModeDisabled, fmt.Errorf("unknown ACL mode: %s", s)
	}
}

// Entry ACL 条目
type Entry struct {
	UserID  string    `json:"user_id"`
	Remark  string    `json:"remark,omitempty"`
	AddedAt time.Time `json:"added_at"`
}

// Plugin ACL 插件
type Plugin struct {
	mu      sync.RWMutex
	mode    Mode
	entries map[string]Entry // userID -> Entry
	storage storage.Client
}

// NewPlugin 创建 Plugin 实例
func NewPlugin() *Plugin {
	return &Plugin{
		mode:    ModeDisabled,
		entries: make(map[string]Entry),
	}
}

// New 创建 ACL 插件描述符（便捷入口）。
// 若需要在注册前持有 Plugin 引用（如测试），改用 NewPlugin() + Descriptor()。
func New() *plugin.PluginDescriptor {
	return &plugin.PluginDescriptor{
		Name:    "acl",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.PluginMeta{
			Author:      "Remilia Team",
			Description: "黑白名单（ACL）访问控制插件",
			Category:    "安全",
			Tags:        []string{"安全", "访问控制", "黑白名单"},
			HelpText: `ACL 插件使用说明：
  p := acl.NewPlugin()
  pm.RegisterV2(acl.Descriptor(p))
  engine.OnGroupAt(p.Rule()).Handle(handler)
  p.SetMode(acl.ModeBlacklist)
  p.Add("userOpenID", "备注")`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p := NewPlugin() // ← Plugin 在 Setup 内创建，可读取 Config
			if ctx.Config != nil {
				if modeStr := ctx.Config.GetString("mode", "disabled"); modeStr != "disabled" {
					if mode, err := ParseMode(modeStr); err == nil {
						p.mode = mode
					}
				}
			}
			ctx.Log.Info("Plugin loaded")
			if sb, ok := plugin.Try[storage.Plugin](ctx, "storage"); ok {
				p.storage = sb
				p.load()
			}
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.API.(*Plugin).save()
			return nil
		},
	}
}

// Descriptor 从已有 Plugin 创建描述符
func Descriptor(p *Plugin) *plugin.PluginDescriptor {
	return &plugin.PluginDescriptor{
		Name:    "acl",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.PluginMeta{
			Author:      "Remilia Team",
			Description: "黑白名单（ACL）访问控制插件",
			Category:    "安全",
			Tags:        []string{"安全", "访问控制", "黑白名单"},
			HelpText: `ACL 插件使用说明：
  p := acl.NewPlugin()
  pm.RegisterV2(acl.Descriptor(p))
  engine.OnGroupAt(p.Rule()).Handle(handler)
  p.SetMode(acl.ModeBlacklist)
  p.Add("userOpenID", "备注")`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Info("Plugin loaded")
			if sb, ok := plugin.Try[storage.Plugin](ctx, "storage"); ok {
				p.storage = sb
				p.load()
			}
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.API.(*Plugin).save()
			return nil
		},
	}
}

// SetMode 设置 ACL 模式
func (p *Plugin) SetMode(mode Mode) {
	p.mu.Lock()
	p.mode = mode
	p.mu.Unlock()
	go p.save()
	logger.Infof("[ACL] Mode set to %s", mode)
}

// GetMode 获取当前 ACL 模式
func (p *Plugin) GetMode() Mode {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.mode
}

// Add 添加用户到 ACL 列表
func (p *Plugin) Add(userID, remark string) {
	p.mu.Lock()
	p.entries[userID] = Entry{
		UserID:  userID,
		Remark:  remark,
		AddedAt: time.Now(),
	}
	p.mu.Unlock()
	go p.save()
	logger.Infof("[ACL] Added user %s (remark: %s)", userID, remark)
}

// Remove 从 ACL 列表移除用户
func (p *Plugin) Remove(userID string) bool {
	p.mu.Lock()
	_, exists := p.entries[userID]
	delete(p.entries, userID)
	p.mu.Unlock()
	if exists {
		go p.save()
	}
	return exists
}

// Contains 检查用户是否在 ACL 列表中
func (p *Plugin) Contains(userID string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.entries[userID]
	return ok
}

// List 返回所有 ACL 条目
func (p *Plugin) List() []Entry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]Entry, 0, len(p.entries))
	for _, e := range p.entries {
		result = append(result, e)
	}
	return result
}

// Clear 清空 ACL 列表
func (p *Plugin) Clear() {
	p.mu.Lock()
	p.entries = make(map[string]Entry)
	p.mu.Unlock()
	go p.save()
}

// Count 返回 ACL 列表中的用户数量
func (p *Plugin) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.entries)
}

// IsAllowed 检查用户是否被允许（根据当前模式）
func (p *Plugin) IsAllowed(userID string) bool {
	p.mu.RLock()
	mode := p.mode
	_, inList := p.entries[userID]
	p.mu.RUnlock()

	switch mode {
	case ModeDisabled:
		return true
	case ModeBlacklist:
		return !inList // 黑名单：在列表中则拒绝
	case ModeWhitelist:
		return inList // 白名单：不在列表中则拒绝
	default:
		return true
	}
}

// Rule 返回可用于 engine.On() 的访问控制规则
func (p *Plugin) Rule() eventctx.Rule {
	return func(ctx *eventctx.Context) bool {
		userID := ctx.GetSenderInfo().ID
		if userID == "" {
			return true
		}
		if !p.IsAllowed(userID) {
			logger.Debugf("[ACL] Blocked user %s (mode: %s)", userID, p.GetMode())
			return false
		}
		return true
	}
}

// ----- 持久化 -----

type snapshot struct {
	Mode    int              `json:"mode"`
	Entries map[string]Entry `json:"entries"`
}

func (p *Plugin) save() {
	if p.storage == nil {
		return
	}
	p.mu.RLock()
	snap := snapshot{
		Mode:    int(p.mode),
		Entries: make(map[string]Entry, len(p.entries)),
	}
	maps.Copy(snap.Entries, p.entries)
	p.mu.RUnlock()

	data, err := json.Marshal(snap)
	if err != nil {
		return
	}
	_ = p.storage.Set("acl:data", data, 0)
}

func (p *Plugin) load() {
	if p.storage == nil {
		return
	}
	data, err := p.storage.Get("acl:data")
	if err != nil {
		return
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return
	}
	p.mu.Lock()
	p.mode = Mode(snap.Mode)
	p.entries = snap.Entries
	p.mu.Unlock()
	logger.Infof("[ACL] Loaded %d entries (mode: %s)", len(snap.Entries), Mode(snap.Mode))
}
