package customcommands

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/builtin/permission/permcheck"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/kv"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

type CustomCommand struct {
	Name     string    `json:"name"`
	Response string    `json:"response"`
	AuthorID string    `json:"author_id"`
	Created  time.Time `json:"created"`
}

type Plugin struct {
	mu      sync.RWMutex
	cmds    map[string]*CustomCommand
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
		cmds: make(map[string]*CustomCommand),
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
		Name:         "customcommands",
		Version:      "1.0.0",
		Privileged:   true,
		OptionalDeps: []string{"permission"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "用户自定义命令，无需写 Go 代码即可添加聊天命令",
			Category:    "管理",
			Tags:        []string{"自定义", "命令", "管理"},
			HelpText: `自定义命令管理：
  /cc add <名称> <响应>    — 添加自定义命令
  /cc delete <名称>        — 删除自定义命令
  /cc list                 — 列出所有自定义命令
  /cc raw <名称>           — 查看命令原始内容
支持变量: {user} {group} {time} {date}`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			if svc, ok := ctx.TryService[*permission.Plugin]("permission"); ok {
				p.permSvc = svc
			}
			if !ctx.DryRun && p.kvPath != "" {
				var err error
				p.store, err = kv.Open(p.kvPath)
				if err != nil {
					return nil, fmt.Errorf("failed to open kv store: %w", err)
				}
				p.load()
			}
			p.registerManagementCommands(ctx)
			p.registerCatchAll(ctx)
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			p.save()
			if p.store != nil {
				return p.store.Close()
			}
			return nil
		},
	}
}

func (p *Plugin) registerManagementCommands(ctx *plugin.SetupContext) {
	ccCmd := &command.Definition{
		Name:        "cc",
		Description: "自定义命令管理",
		Usage:       "/cc <子命令> [参数]",
		Category:    "管理",
		SubCommands: []*command.Definition{
			{Name: "add", Description: "添加自定义命令", Usage: "/cc add <名称> <响应>", Examples: []string{`/cc add greet 你好 {user}!`}},
			{Name: "delete", Description: "删除自定义命令", Usage: "/cc delete <名称>", Examples: []string{"/cc delete greet"}},
			{Name: "list", Description: "列出所有自定义命令", Usage: "/cc list", Examples: []string{"/cc list"}},
			{Name: "raw", Description: "查看命令原始内容", Usage: "/cc raw <名称>", Examples: []string{"/cc raw greet"}},
		},
	}
	ctx.Reg.RegisterCommand("", "/cc").
		Where(eventctx.OnMentionedBotOrNoMentions()).
		SetDefinition(ccCmd).
		Handle(p.handleCC)
}

func (p *Plugin) registerCatchAll(ctx *plugin.SetupContext) {
	m := ctx.Reg.RegisterMatcher("", eventctx.OnMentionedBotOrNoMentions(), func(c *eventctx.Context) bool {
		content := c.GetMessageContent()
		if !strings.HasPrefix(content, "/") {
			return false
		}
		parts := strings.Fields(content)
		if len(parts) == 0 {
			return false
		}
		cmdName := strings.TrimPrefix(parts[0], "/")
		if strings.Contains(cmdName, " ") {
			return false
		}
		p.mu.RLock()
		_, exists := p.cmds[cmdName]
		p.mu.RUnlock()
		return exists
	})
	m.SetBlock(true)
	m.Handle(func(ctx *eventctx.Context) error {
		content := ctx.GetMessageContent()
		cmdName := strings.TrimPrefix(strings.Fields(content)[0], "/")

		p.mu.RLock()
		cmd := p.cmds[cmdName]
		p.mu.RUnlock()
		if cmd == nil {
			return nil
		}

		msg := cmd.Response
		msg = strings.ReplaceAll(msg, "{user}", ctx.GetSenderInfo().DisplayName)
		msg = strings.ReplaceAll(msg, "{group}", ctx.GetPlatformEvent().Chat().Name)
		msg = strings.ReplaceAll(msg, "{time}", time.Now().Format("15:04"))
		msg = strings.ReplaceAll(msg, "{date}", time.Now().Format("01-02"))
		ctx.Reply(platform.TextMessage(msg))
		return nil
	})
}

func (p *Plugin) handleCC(ctx *eventctx.Context) error {
	args := strings.Fields(ctx.GetMessageContent())
	if len(args) < 2 {
		ctx.Reply(platform.TextMessage("用法: /cc add|delete|list|raw [参数]"))
		return nil
	}
	switch args[1] {
	case "add":
		return p.handleAdd(ctx, args[2:])
	case "delete":
		return p.handleDelete(ctx, args[2:])
	case "list":
		return p.handleList(ctx)
	case "raw":
		return p.handleRaw(ctx, args[2:])
	default:
		ctx.Reply(platform.TextMessage("未知子命令，可用: add, delete, list, raw"))
		return nil
	}
}

func (p *Plugin) handleAdd(ctx *eventctx.Context, args []string) error {
	if !p.checkPermission(ctx, "customcommands.manage") {
		ctx.Reply(platform.TextMessage("权限不足：需要 customcommands.manage 权限"))
		return nil
	}
	if len(args) < 2 {
		ctx.Reply(platform.TextMessage("用法: /cc add <名称> <响应>"))
		return nil
	}
	name := strings.TrimPrefix(args[0], "/")
	response := strings.Join(args[1:], " ")
	author := ctx.GetSenderInfo()

	p.mu.Lock()
	if _, exists := p.cmds[name]; exists {
		p.mu.Unlock()
		ctx.Reply(platform.TextMessage(fmt.Sprintf("命令 /%s 已存在", name)))
		return nil
	}
	p.cmds[name] = &CustomCommand{
		Name:     name,
		Response: response,
		AuthorID: author.ID,
		Created:  time.Now(),
	}
	p.mu.Unlock()

	p.save()
	logger.Infof("[CustomCommands] Added command /%s by %s", name, author.ID)
	ctx.Reply(platform.TextMessage(fmt.Sprintf("已添加命令 /%s", name)))
	return nil
}

func (p *Plugin) handleDelete(ctx *eventctx.Context, args []string) error {
	if !p.checkPermission(ctx, "customcommands.manage") {
		ctx.Reply(platform.TextMessage("权限不足：需要 customcommands.manage 权限"))
		return nil
	}
	if len(args) < 1 {
		ctx.Reply(platform.TextMessage("用法: /cc delete <名称>"))
		return nil
	}
	name := strings.TrimPrefix(args[0], "/")

	p.mu.Lock()
	delete(p.cmds, name)
	p.mu.Unlock()

	p.save()
	logger.Infof("[CustomCommands] Deleted command /%s", name)
	ctx.Reply(platform.TextMessage(fmt.Sprintf("已删除命令 /%s", name)))
	return nil
}

func (p *Plugin) handleList(ctx *eventctx.Context) error {
	p.mu.RLock()
	if len(p.cmds) == 0 {
		p.mu.RUnlock()
		ctx.Reply(platform.TextMessage("暂无自定义命令"))
		return nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "自定义命令 (共 %d 个):\n", len(p.cmds))
	for _, cmd := range p.cmds {
		preview := cmd.Response
		if len(preview) > 50 {
			preview = preview[:50] + "..."
		}
		fmt.Fprintf(&sb, "  /%s → %s\n", cmd.Name, preview)
	}
	p.mu.RUnlock()
	ctx.Reply(platform.TextMessage(sb.String()))
	return nil
}

func (p *Plugin) handleRaw(ctx *eventctx.Context, args []string) error {
	if !p.checkPermission(ctx, "customcommands.manage") {
		ctx.Reply(platform.TextMessage("权限不足：需要 customcommands.manage 权限"))
		return nil
	}
	if len(args) < 1 {
		ctx.Reply(platform.TextMessage("用法: /cc raw <名称>"))
		return nil
	}
	name := strings.TrimPrefix(args[0], "/")

	p.mu.RLock()
	cmd, ok := p.cmds[name]
	p.mu.RUnlock()

	if !ok {
		ctx.Reply(platform.TextMessage(fmt.Sprintf("命令 /%s 不存在", name)))
		return nil
	}
	ctx.Reply(platform.TextMessage(fmt.Sprintf("/%s 的原始内容:\n%s", name, cmd.Response)))
	return nil
}

func (p *Plugin) checkPermission(ctx *eventctx.Context, perm string) bool {
	return permcheck.HasPermission(p.permSvc, ctx, perm)
}

func (p *Plugin) save() {
	if p.store == nil {
		return
	}
	p.mu.RLock()
	data := make(map[string]*CustomCommand, len(p.cmds))
	maps.Copy(data, p.cmds)
	p.mu.RUnlock()
	bytes, err := json.Marshal(data)
	if err != nil {
		logger.WithError(err).Warn("[CustomCommands] Failed to marshal")
		return
	}
	if err := p.store.Set([]byte("state"), bytes); err != nil {
		logger.WithError(err).Warn("[CustomCommands] Failed to save")
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
	var data map[string]*CustomCommand
	if err := json.Unmarshal(bytes, &data); err != nil {
		return
	}
	p.mu.Lock()
	p.cmds = data
	p.mu.Unlock()
	logger.Infof("[CustomCommands] Loaded %d commands", len(data))
}
