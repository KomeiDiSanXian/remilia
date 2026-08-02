package welcome

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/builtin/permission/permcheck"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/kv"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// globalGroupID 全局默认配置在 configs 中使用的保留键。
// 群组未显式配置（从未 set/off）时，回退到该全局配置。
const globalGroupID = "__global__"

type GroupConfig struct {
	WelcomeMessage  string `json:"welcome_message,omitempty"`
	FarewellMessage string `json:"farewell_message,omitempty"`
	WelcomeEnabled  bool   `json:"welcome_enabled"`
	FarewellEnabled bool   `json:"farewell_enabled"`
}

type Plugin struct {
	mu      sync.RWMutex
	configs map[string]*GroupConfig
	kvPath  string
	store   *kv.DB
	permSvc *permission.Plugin
}

type Option func(*Plugin)

func WithStore(path string) Option {
	return func(p *Plugin) { p.kvPath = path }
}

func NewPlugin(opts ...Option) *Plugin {
	p := &Plugin{
		configs: make(map[string]*GroupConfig),
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

func New(opts ...Option) *plugin.Descriptor {
	return NewPlugin(opts...).Descriptor()
}

func (p *Plugin) Descriptor() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:         "welcome",
		Version:      "1.0.0",
		Privileged:   true,
		OptionalDeps: []string{"permission"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "入群欢迎/退群告别消息",
			Category:    "管理",
			Tags:        []string{"欢迎", "告别", "群管"},
			HelpText: `欢迎/告别消息管理：
  /welcome set <消息>      — 设置欢迎消息（支持 {user} {group}）
  /welcome off             — 关闭欢迎消息
  /welcome status          — 查看当前设置
  /welcome global set <消息> — 设置全局欢迎消息（所有未单独配置的群生效）
  /welcome global on       — 全局开启欢迎消息
  /welcome global off      — 全局关闭欢迎消息
  /farewell set <消息>     — 设置告别消息
  /farewell off            — 关闭告别消息
  /farewell global set <消息> — 设置全局告别消息
  /farewell global on      — 全局开启告别消息
  /farewell global off     — 全局关闭告别消息`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			if svc, ok := plugin.TryService[*permission.Plugin](ctx, "permission"); ok {
				p.permSvc = svc
			}
			if !ctx.DryRun && p.kvPath != "" {
				db, err := kv.Open(p.kvPath)
				if err != nil {
					return nil, fmt.Errorf("failed to open kv store: %w", err)
				}
				p.store = db
				p.load()
			}
			p.registerCommands(ctx)
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			p.save()
			if p.store != nil {
				p.store.Close()
			}
			return nil
		},
	}
}

func (p *Plugin) registerCommands(ctx *plugin.SetupContext) {
	welcomeCmd := &command.Definition{
		Name:        "welcome",
		Description: "管理入群欢迎/退群告别设置",
		Usage:       "/welcome <set|off|status|global> [消息]",
		Category:    "管理",
		SubCommands: []*command.Definition{
			{Name: "set", Description: "设置欢迎消息", Usage: "/welcome set <消息>", Examples: []string{"/welcome set 欢迎 {user} 加入本群！"}},
			{Name: "off", Description: "关闭欢迎消息", Usage: "/welcome off", Examples: []string{"/welcome off"}},
			{Name: "status", Description: "查看当前设置", Usage: "/welcome status", Examples: []string{"/welcome status"}},
			{Name: "global", Description: "管理全局默认欢迎消息", Usage: "/welcome global <set|on|off|status> [消息]", Examples: []string{"/welcome global set 欢迎 {user} 加入本群！", "/welcome global on"}},
		},
	}
	ctx.Reg.RegisterCommand("", "/welcome").SetDefinition(welcomeCmd).Handle(p.handleWelcomeCommand)

	farewellCmd := &command.Definition{
		Name:        "farewell",
		Description: "管理退群告别设置",
		Usage:       "/farewell <set|off|global> [消息]",
		Category:    "管理",
		SubCommands: []*command.Definition{
			{Name: "set", Description: "设置告别消息", Usage: "/farewell set <消息>", Examples: []string{"/farewell set {user} 离开了群聊"}},
			{Name: "off", Description: "关闭告别消息", Usage: "/farewell off", Examples: []string{"/farewell off"}},
			{Name: "global", Description: "管理全局默认告别消息", Usage: "/farewell global <set|on|off|status> [消息]", Examples: []string{"/farewell global set {user} 离开了群聊", "/farewell global on"}},
		},
	}
	ctx.Reg.RegisterCommand("", "/farewell").SetDefinition(farewellCmd).Handle(p.handleFarewellCommand)

	ctx.Reg.RegisterMatcher(string(platform.EventKindMemberJoin)).Handle(func(ctx *eventctx.Context) error {
		chat := ctx.GetPlatformEvent().Chat()
		cfg := p.effectiveConfig(chat.ID)
		if !cfg.WelcomeEnabled || cfg.WelcomeMessage == "" {
			return nil
		}
		msg := cfg.WelcomeMessage
		user := ctx.GetSenderInfo()
		msg = strings.ReplaceAll(msg, "{user}", user.DisplayName)
		msg = strings.ReplaceAll(msg, "{group}", chat.Name)
		ctx.Reply(platform.TextMessage(msg))
		return nil
	})

	ctx.Reg.RegisterMatcher(string(platform.EventKindMemberLeave)).Handle(func(ctx *eventctx.Context) error {
		chat := ctx.GetPlatformEvent().Chat()
		cfg := p.effectiveConfig(chat.ID)
		if !cfg.FarewellEnabled || cfg.FarewellMessage == "" {
			return nil
		}
		msg := cfg.FarewellMessage
		user := ctx.GetSenderInfo()
		msg = strings.ReplaceAll(msg, "{user}", user.DisplayName)
		msg = strings.ReplaceAll(msg, "{group}", chat.Name)
		ctx.Reply(platform.TextMessage(msg))
		return nil
	})
}

func (p *Plugin) handleWelcomeCommand(ctx *eventctx.Context) error {
	chat := ctx.GetPlatformEvent().Chat()
	if !chat.IsGroup {
		ctx.Reply(platform.TextMessage("该命令仅支持群聊"))
		return nil
	}
	args := strings.Fields(ctx.GetMessageContent())
	if len(args) < 2 {
		ctx.Reply(platform.TextMessage("用法: /welcome set|off|status|global [消息]"))
		return nil
	}

	subCmd := args[1]
	if subCmd == "global" {
		return p.handleWelcomeGlobal(ctx, args[2:])
	}

	if subCmd == "status" {
		eff := p.effectiveConfig(chat.ID)
		var sb strings.Builder
		fmt.Fprintf(&sb, "欢迎消息: %s\n", boolStr(eff.WelcomeEnabled))
		if eff.WelcomeMessage != "" {
			fmt.Fprintf(&sb, "内容: %s\n", eff.WelcomeMessage)
		}
		sb.WriteString(fmt.Sprintf("告别消息: %s", boolStr(eff.FarewellEnabled)))
		if eff.FarewellMessage != "" {
			sb.WriteString(fmt.Sprintf("\n内容: %s", eff.FarewellMessage))
		}
		ctx.Reply(platform.TextMessage(sb.String()))
		return nil
	}

	p.mu.Lock()
	cfg := p.getOrCreateConfig(chat.ID)

	switch subCmd {
	case "set":
		if !p.checkPermission(ctx, "welcome.manage") {
			p.mu.Unlock()
			ctx.Reply(platform.TextMessage("权限不足：需要 welcome.manage 权限"))
			return nil
		}
		if len(args) < 3 {
			p.mu.Unlock()
			ctx.Reply(platform.TextMessage("用法: /welcome set <消息>"))
			return nil
		}
		cfg.WelcomeMessage = strings.Join(args[2:], " ")
		cfg.WelcomeEnabled = true
		p.mu.Unlock()
		p.save()
		ctx.Reply(platform.TextMessage("欢迎消息已设置"))
		return nil
	case "off":
		cfg.WelcomeEnabled = false
		p.mu.Unlock()
		p.save()
		ctx.Reply(platform.TextMessage("欢迎消息已关闭"))
		return nil
	default:
		p.mu.Unlock()
		ctx.Reply(platform.TextMessage("未知子命令，可用: set, off, status, global"))
		return nil
	}
}

func (p *Plugin) handleWelcomeGlobal(ctx *eventctx.Context, args []string) error {
	if !p.checkGlobalPermission(ctx) {
		ctx.Reply(platform.TextMessage("权限不足：需要 superadmin 角色或 welcome.global 权限"))
		return nil
	}
	if len(args) < 1 {
		ctx.Reply(platform.TextMessage("用法: /welcome global set <消息>|on|off|status"))
		return nil
	}

	p.mu.Lock()
	cfg := p.getOrCreateConfig(globalGroupID)

	switch args[0] {
	case "set":
		if len(args) < 2 {
			p.mu.Unlock()
			ctx.Reply(platform.TextMessage("用法: /welcome global set <消息>"))
			return nil
		}
		cfg.WelcomeMessage = strings.Join(args[1:], " ")
		cfg.WelcomeEnabled = true
		p.mu.Unlock()
		p.save()
		ctx.Reply(platform.TextMessage("全局欢迎消息已设置"))
		return nil
	case "on":
		cfg.WelcomeEnabled = true
		p.mu.Unlock()
		p.save()
		ctx.Reply(platform.TextMessage("全局欢迎消息已开启"))
		return nil
	case "off":
		cfg.WelcomeEnabled = false
		p.mu.Unlock()
		p.save()
		ctx.Reply(platform.TextMessage("全局欢迎消息已关闭"))
		return nil
	case "status":
		we := cfg.WelcomeEnabled
		wm := cfg.WelcomeMessage
		fe := cfg.FarewellEnabled
		fm := cfg.FarewellMessage
		p.mu.Unlock()
		var sb strings.Builder
		fmt.Fprintf(&sb, "全局欢迎消息: %s\n", boolStr(we))
		if wm != "" {
			fmt.Fprintf(&sb, "内容: %s\n", wm)
		}
		fmt.Fprintf(&sb, "全局告别消息: %s", boolStr(fe))
		if fm != "" {
			sb.WriteString(fmt.Sprintf("\n内容: %s", fm))
		}
		ctx.Reply(platform.TextMessage(sb.String()))
		return nil
	default:
		p.mu.Unlock()
		ctx.Reply(platform.TextMessage("未知子命令，可用: set, on, off, status"))
		return nil
	}
}

func (p *Plugin) handleFarewellCommand(ctx *eventctx.Context) error {
	chat := ctx.GetPlatformEvent().Chat()
	if !chat.IsGroup {
		ctx.Reply(platform.TextMessage("该命令仅支持群聊"))
		return nil
	}
	args := strings.Fields(ctx.GetMessageContent())
	if len(args) < 2 {
		ctx.Reply(platform.TextMessage("用法: /farewell set|off|global [消息]"))
		return nil
	}

	if args[1] == "global" {
		return p.handleFarewellGlobal(ctx, args[2:])
	}

	p.mu.Lock()
	cfg := p.getOrCreateConfig(chat.ID)

	switch args[1] {
	case "set":
		if !p.checkPermission(ctx, "welcome.manage") {
			p.mu.Unlock()
			ctx.Reply(platform.TextMessage("权限不足：需要 welcome.manage 权限"))
			return nil
		}
		if len(args) < 3 {
			p.mu.Unlock()
			ctx.Reply(platform.TextMessage("用法: /farewell set <消息>"))
			return nil
		}
		cfg.FarewellMessage = strings.Join(args[2:], " ")
		cfg.FarewellEnabled = true
		p.mu.Unlock()
		p.save()
		ctx.Reply(platform.TextMessage("告别消息已设置"))
		return nil
	case "off":
		cfg.FarewellEnabled = false
		p.mu.Unlock()
		p.save()
		ctx.Reply(platform.TextMessage("告别消息已关闭"))
		return nil
	default:
		p.mu.Unlock()
		ctx.Reply(platform.TextMessage("未知子命令，可用: set, off, global"))
		return nil
	}
}

func (p *Plugin) handleFarewellGlobal(ctx *eventctx.Context, args []string) error {
	if !p.checkGlobalPermission(ctx) {
		ctx.Reply(platform.TextMessage("权限不足：需要 superadmin 角色或 welcome.global 权限"))
		return nil
	}
	if len(args) < 1 {
		ctx.Reply(platform.TextMessage("用法: /farewell global set <消息>|on|off|status"))
		return nil
	}

	p.mu.Lock()
	cfg := p.getOrCreateConfig(globalGroupID)

	switch args[0] {
	case "set":
		if len(args) < 2 {
			p.mu.Unlock()
			ctx.Reply(platform.TextMessage("用法: /farewell global set <消息>"))
			return nil
		}
		cfg.FarewellMessage = strings.Join(args[1:], " ")
		cfg.FarewellEnabled = true
		p.mu.Unlock()
		p.save()
		ctx.Reply(platform.TextMessage("全局告别消息已设置"))
		return nil
	case "on":
		cfg.FarewellEnabled = true
		p.mu.Unlock()
		p.save()
		ctx.Reply(platform.TextMessage("全局告别消息已开启"))
		return nil
	case "off":
		cfg.FarewellEnabled = false
		p.mu.Unlock()
		p.save()
		ctx.Reply(platform.TextMessage("全局告别消息已关闭"))
		return nil
	case "status":
		we := cfg.WelcomeEnabled
		wm := cfg.WelcomeMessage
		fe := cfg.FarewellEnabled
		fm := cfg.FarewellMessage
		p.mu.Unlock()
		var sb strings.Builder
		fmt.Fprintf(&sb, "全局欢迎消息: %s\n", boolStr(we))
		if wm != "" {
			fmt.Fprintf(&sb, "内容: %s\n", wm)
		}
		fmt.Fprintf(&sb, "全局告别消息: %s", boolStr(fe))
		if fm != "" {
			sb.WriteString(fmt.Sprintf("\n内容: %s", fm))
		}
		ctx.Reply(platform.TextMessage(sb.String()))
		return nil
	default:
		p.mu.Unlock()
		ctx.Reply(platform.TextMessage("未知子命令，可用: set, on, off, status"))
		return nil
	}
}

func (p *Plugin) getOrCreateConfig(groupID string) *GroupConfig {
	if cfg, ok := p.configs[groupID]; ok {
		return cfg
	}
	cfg := &GroupConfig{}
	p.configs[groupID] = cfg
	return cfg
}

// effectiveConfig 返回指定群的生效配置：
// 群内已显式设置（set/off 过任意字段）时使用群配置，否则回退到全局配置。
func (p *Plugin) effectiveConfig(groupID string) *GroupConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if cfg, ok := p.configs[groupID]; ok {
		return cfg
	}
	if g, ok := p.configs[globalGroupID]; ok {
		return g
	}
	return &GroupConfig{}
}

// EffectiveConfig 返回指定群的生效配置（公开接口，供测试或其他插件查询）。
// 语义与 effectiveConfig 一致：群内显式配置优先，否则回退全局配置。
func (p *Plugin) EffectiveConfig(groupID string) *GroupConfig {
	return p.effectiveConfig(groupID)
}

// SetGroupWelcome 设置指定群的欢迎消息并指定开关状态。
func (p *Plugin) SetGroupWelcome(groupID, message string, enabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cfg := p.getOrCreateConfig(groupID)
	cfg.WelcomeMessage = message
	cfg.WelcomeEnabled = enabled
}

// SetGlobalWelcome 设置全局默认欢迎消息并指定开关状态。
func (p *Plugin) SetGlobalWelcome(message string, enabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cfg := p.getOrCreateConfig(globalGroupID)
	cfg.WelcomeMessage = message
	cfg.WelcomeEnabled = enabled
}

func (p *Plugin) checkPermission(ctx *eventctx.Context, perm string) bool {
	return permcheck.HasPermission(p.permSvc, ctx, perm)
}

// checkGlobalPermission 全局设置要求 superadmin 角色或 welcome.global 权限。
func (p *Plugin) checkGlobalPermission(ctx *eventctx.Context) bool {
	if p.permSvc == nil {
		return true
	}
	if p.permSvc.HasPermission(ctx.GetUserID(), "welcome.global") {
		return true
	}
	return slices.Contains(p.permSvc.GetUserRoles(ctx.GetUserID()), "superadmin")
}

func (p *Plugin) save() {
	if p.store == nil {
		return
	}
	p.mu.RLock()
	data := make(map[string]*GroupConfig, len(p.configs))
	maps.Copy(data, p.configs)
	p.mu.RUnlock()
	bytes, err := json.Marshal(data)
	if err != nil {
		logger.WithError(err).Warn("[Welcome] Failed to marshal")
		return
	}
	if err := p.store.Set([]byte("state"), bytes); err != nil {
		logger.WithError(err).Warn("[Welcome] Failed to save")
	}
}

func (p *Plugin) load() {
	if p.store == nil {
		return
	}
	bytes, err := p.store.Get([]byte("state"))
	if err != nil {
		return
	}
	var data map[string]*GroupConfig
	if err := json.Unmarshal(bytes, &data); err != nil {
		return
	}
	p.mu.Lock()
	p.configs = data
	p.mu.Unlock()
	logger.Infof("[Welcome] Loaded %d group configs", len(data))
}

func boolStr(b bool) string {
	if b {
		return "✅ 已开启"
	}
	return "❌ 已关闭"
}
