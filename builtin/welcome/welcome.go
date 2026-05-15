package welcome

import (
	"fmt"
	"strings"
	"sync"

	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/builtin/internal/jsonfile"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

type GroupConfig struct {
	WelcomeMessage  string `json:"welcome_message,omitempty"`
	FarewellMessage string `json:"farewell_message,omitempty"`
	WelcomeEnabled  bool   `json:"welcome_enabled"`
	FarewellEnabled bool   `json:"farewell_enabled"`
}

type Plugin struct {
	mu       sync.RWMutex
	configs  map[string]*GroupConfig
	dataFile string
	permSvc  *plugin.ServiceProxy[*permission.Plugin]
}

type Option func(*Plugin)

func WithDataFile(path string) Option {
	return func(p *Plugin) { p.dataFile = path }
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
  /farewell set <消息>     — 设置告别消息
  /farewell off            — 关闭告别消息
  /welcome status          — 查看当前设置`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			if svc, ok := plugin.TryService[*permission.Plugin](ctx, "permission"); ok {
				p.permSvc = svc
			}
			p.load()
			p.registerCommands(ctx)
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			p.save()
			return nil
		},
	}
}

func (p *Plugin) registerCommands(ctx *plugin.SetupContext) {
	welcomeCmd := &command.Definition{
		Name:        "welcome",
		Description: "管理入群欢迎/退群告别设置",
		Usage:       "/welcome <set|off|status> [消息]",
		Category:    "管理",
		SubCommands: []*command.Definition{
			{Name: "set", Description: "设置欢迎消息", Usage: "/welcome set <消息>", Examples: []string{"/welcome set 欢迎 {user} 加入本群！"}},
			{Name: "off", Description: "关闭欢迎消息", Usage: "/welcome off", Examples: []string{"/welcome off"}},
			{Name: "status", Description: "查看当前设置", Usage: "/welcome status", Examples: []string{"/welcome status"}},
		},
	}
	ctx.Reg.RegisterCommand("", "/welcome").SetDefinition(welcomeCmd).Handle(p.handleWelcomeCommand)

	farewellCmd := &command.Definition{
		Name:        "farewell",
		Description: "管理退群告别设置",
		Usage:       "/farewell <set|off> [消息]",
		Category:    "管理",
		SubCommands: []*command.Definition{
			{Name: "set", Description: "设置告别消息", Usage: "/farewell set <消息>", Examples: []string{"/farewell set {user} 离开了群聊"}},
			{Name: "off", Description: "关闭告别消息", Usage: "/farewell off", Examples: []string{"/farewell off"}},
		},
	}
	ctx.Reg.RegisterCommand("", "/farewell").SetDefinition(farewellCmd).Handle(p.handleFarewellCommand)

	ctx.Reg.RegisterMatcher(string(platform.EventKindMemberJoin)).Handle(func(ctx *eventctx.Context) error {
		chat := ctx.GetPlatformEvent().Chat()
		p.mu.RLock()
		cfg, ok := p.configs[chat.ID]
		p.mu.RUnlock()
		if !ok || !cfg.WelcomeEnabled || cfg.WelcomeMessage == "" {
			return nil
		}
		msg := cfg.WelcomeMessage
		user := ctx.GetSenderInfo()
		msg = strings.ReplaceAll(msg, "{user}", user.DisplayName)
		msg = strings.ReplaceAll(msg, "{group}", chat.Name)
		_, err := ctx.Reply(platform.TextMessage(msg))
		return err
	})

	ctx.Reg.RegisterMatcher(string(platform.EventKindMemberLeave)).Handle(func(ctx *eventctx.Context) error {
		chat := ctx.GetPlatformEvent().Chat()
		p.mu.RLock()
		cfg, ok := p.configs[chat.ID]
		p.mu.RUnlock()
		if !ok || !cfg.FarewellEnabled || cfg.FarewellMessage == "" {
			return nil
		}
		msg := cfg.FarewellMessage
		user := ctx.GetSenderInfo()
		msg = strings.ReplaceAll(msg, "{user}", user.DisplayName)
		msg = strings.ReplaceAll(msg, "{group}", chat.Name)
		_, err := ctx.Reply(platform.TextMessage(msg))
		return err
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
		ctx.Reply(platform.TextMessage("用法: /welcome set|off|status [消息]"))
		return nil
	}

	subCmd := args[1]
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
	case "status":
		we := cfg.WelcomeEnabled
		wm := cfg.WelcomeMessage
		fe := cfg.FarewellEnabled
		fm := cfg.FarewellMessage
		p.mu.Unlock()
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("欢迎消息: %s\n", boolStr(we)))
		if wm != "" {
			sb.WriteString(fmt.Sprintf("内容: %s\n", wm))
		}
		sb.WriteString(fmt.Sprintf("告别消息: %s", boolStr(fe)))
		if fm != "" {
			sb.WriteString(fmt.Sprintf("\n内容: %s", fm))
		}
		ctx.Reply(platform.TextMessage(sb.String()))
		return nil
	default:
		p.mu.Unlock()
		ctx.Reply(platform.TextMessage("未知子命令，可用: set, off, status"))
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
		ctx.Reply(platform.TextMessage("用法: /farewell set|off [消息]"))
		return nil
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
		ctx.Reply(platform.TextMessage("未知子命令，可用: set, off"))
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

func (p *Plugin) checkPermission(ctx *eventctx.Context, perm string) bool {
	if p.permSvc == nil {
		return true
	}
	pp, ok := p.permSvc.Get()
	if !ok || pp == nil {
		return true
	}
	return pp.HasPermission(ctx.GetUserID(), perm)
}

func (p *Plugin) save() {
	if p.dataFile == "" {
		return
	}
	p.mu.RLock()
	data := make(map[string]*GroupConfig, len(p.configs))
	for k, v := range p.configs {
		data[k] = v
	}
	p.mu.RUnlock()
	if err := jsonfile.Write(p.dataFile, data); err != nil {
		logger.WithError(err).Warn("[Welcome] Failed to save")
	}
}

func (p *Plugin) load() {
	if p.dataFile == "" {
		return
	}
	data, err := jsonfile.Read[map[string]*GroupConfig](p.dataFile)
	if err != nil {
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
