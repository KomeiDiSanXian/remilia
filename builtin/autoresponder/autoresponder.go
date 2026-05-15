package autoresponder

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/builtin/internal/jsonfile"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

type MatchMode int

const (
	MatchExact MatchMode = iota
	MatchContains
	MatchPrefix
	MatchRegex
)

func (m MatchMode) String() string {
	switch m {
	case MatchExact:
		return "精确"
	case MatchContains:
		return "包含"
	case MatchPrefix:
		return "前缀"
	case MatchRegex:
		return "正则"
	default:
		return "未知"
	}
}

type Rule struct {
	ID        string    `json:"id"`
	Keyword   string    `json:"keyword"`
	Response  string    `json:"response"`
	Mode      MatchMode `json:"mode"`
	CreatedAt time.Time `json:"created_at"`
	AuthorID  string    `json:"author_id"`
	Cooldown  int       `json:"cooldown"`
}

type Plugin struct {
	mu          sync.RWMutex
	rules       []*Rule
	nextID      int64
	lastTrigger map[string]time.Time
	dataFile    string
	prefix      string
	permSvc     *plugin.ServiceProxy[*permission.Plugin]
}

type Option func(*Plugin)

func WithDataFile(path string) Option {
	return func(p *Plugin) { p.dataFile = path }
}

func WithPrefix(pfx string) Option {
	return func(p *Plugin) { p.prefix = pfx }
}

func NewPlugin(opts ...Option) *Plugin {
	p := &Plugin{
		rules:       make([]*Rule, 0),
		lastTrigger: make(map[string]time.Time),
		prefix:      "/",
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
		Name:         "autoresponder",
		Version:      "1.0.0",
		Privileged:   true,
		OptionalDeps: []string{"permission"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "关键词触发自动回复",
			Category:    "管理",
			Tags:        []string{"自动回复", "关键词", "管理"},
			HelpText: `自动回复管理：
  /ar add <关键词> <响应>        — 添加包含匹配（默认）
  /ar add exact <词> <响应>      — 精确匹配
  /ar add prefix <词> <响应>     — 前缀匹配
  /ar add regex <表达式> <响应>  — 正则匹配
  /ar remove <ID>                — 删除规则
  /ar list                       — 列出所有规则
  /ar cooldown <ID> <秒>         — 设置规则冷却`,
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
	arCmd := &command.Definition{
		Name:        "ar",
		Description: "自动回复管理",
		Usage:       "/ar <add|remove|list|cooldown> [参数]",
		Category:    "管理",
		SubCommands: []*command.Definition{
			{Name: "add", Description: "添加自动回复规则", Usage: "/ar add [模式] <关键词> <响应>", Examples: []string{"/ar add 你好 你好呀！", "/ar add exact 签到 签到成功！"}},
			{Name: "remove", Description: "删除规则", Usage: "/ar remove <ID>", Examples: []string{"/ar remove 1"}},
			{Name: "list", Description: "列出所有规则", Usage: "/ar list", Examples: []string{"/ar list"}},
			{Name: "cooldown", Description: "设置规则冷却", Usage: "/ar cooldown <ID> <秒>", Examples: []string{"/ar cooldown 1 60"}},
		},
	}
	ctx.Reg.RegisterCommand("", "/ar").SetDefinition(arCmd).Handle(p.handleAR)
}

func (p *Plugin) handleAR(ctx *eventctx.Context) error {
	args := strings.Fields(ctx.GetMessageContent())
	if len(args) < 2 {
		ctx.Reply(platform.TextMessage("用法: /ar add|remove|list|cooldown [参数]"))
		return nil
	}
	switch args[1] {
	case "add":
		return p.handleAdd(ctx, args[2:])
	case "remove":
		return p.handleRemove(ctx, args[2:])
	case "list":
		return p.handleList(ctx)
	case "cooldown":
		return p.handleCooldown(ctx, args[2:])
	default:
		ctx.Reply(platform.TextMessage("未知子命令，可用: add, remove, list, cooldown"))
		return nil
	}
}

func (p *Plugin) handleAdd(ctx *eventctx.Context, args []string) error {
	if !p.checkPermission(ctx, "autoresponder.manage") {
		ctx.Reply(platform.TextMessage("权限不足：需要 autoresponder.manage 权限"))
		return nil
	}
	if len(args) < 2 {
		ctx.Reply(platform.TextMessage("用法: /ar add [模式] <关键词> <响应>\n模式: exact, prefix, regex（默认=包含）"))
		return nil
	}
	mode := MatchContains
	keywordIdx := 0
	switch args[0] {
	case "exact":
		mode = MatchExact
		keywordIdx = 1
	case "prefix":
		mode = MatchPrefix
		keywordIdx = 1
	case "regex":
		mode = MatchRegex
		keywordIdx = 1
	}
	if keywordIdx >= len(args) {
		ctx.Reply(platform.TextMessage("缺少关键词"))
		return nil
	}
	keyword := args[keywordIdx]
	response := strings.Join(args[keywordIdx+1:], " ")

	if mode == MatchRegex {
		if _, err := regexp.Compile(keyword); err != nil {
			ctx.Reply(platform.TextMessage(fmt.Sprintf("正则表达式无效: %v", err)))
			return nil
		}
	}

	p.mu.Lock()
	p.nextID++
	rule := &Rule{
		ID:        fmt.Sprintf("%d", p.nextID),
		Keyword:   keyword,
		Response:  response,
		Mode:      mode,
		CreatedAt: time.Now(),
		AuthorID:  ctx.GetSenderInfo().ID,
	}
	p.rules = append(p.rules, rule)
	p.mu.Unlock()

	p.save()
	logger.Infof("[AutoResponder] Added rule #%s: [%s] %q -> %q", rule.ID, mode, keyword, response)
	ctx.Reply(platform.TextMessage(fmt.Sprintf("已添加规则 #%s (模式: %s)", rule.ID, mode)))
	return nil
}

func (p *Plugin) handleRemove(ctx *eventctx.Context, args []string) error {
	if !p.checkPermission(ctx, "autoresponder.manage") {
		ctx.Reply(platform.TextMessage("权限不足：需要 autoresponder.manage 权限"))
		return nil
	}
	if len(args) < 1 {
		ctx.Reply(platform.TextMessage("用法: /ar remove <ID>"))
		return nil
	}
	id := args[0]

	p.mu.Lock()
	removed := false
	for i, r := range p.rules {
		if r.ID == id {
			p.rules = append(p.rules[:i], p.rules[i+1:]...)
			removed = true
			break
		}
	}
	p.mu.Unlock()

	if removed {
		p.save()
		ctx.Reply(platform.TextMessage(fmt.Sprintf("已删除规则 #%s", id)))
		return nil
	}
	ctx.Reply(platform.TextMessage(fmt.Sprintf("规则 #%s 不存在", id)))
	return nil
}

func (p *Plugin) handleList(ctx *eventctx.Context) error {
	p.mu.RLock()
	if len(p.rules) == 0 {
		p.mu.RUnlock()
		ctx.Reply(platform.TextMessage("暂无自动回复规则"))
		return nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("自动回复规则 (共 %d 条):\n", len(p.rules)))
	for _, r := range p.rules {
		preview := r.Response
		if len(preview) > 40 {
			preview = preview[:40] + "..."
		}
		cooldown := ""
		if r.Cooldown > 0 {
			cooldown = fmt.Sprintf(" [冷却:%ds]", r.Cooldown)
		}
		sb.WriteString(fmt.Sprintf("  #%s [%s] %q → %q%s\n", r.ID, r.Mode, r.Keyword, preview, cooldown))
	}
	p.mu.RUnlock()
	ctx.Reply(platform.TextMessage(sb.String()))
	return nil
}

func (p *Plugin) handleCooldown(ctx *eventctx.Context, args []string) error {
	if !p.checkPermission(ctx, "autoresponder.manage") {
		ctx.Reply(platform.TextMessage("权限不足：需要 autoresponder.manage 权限"))
		return nil
	}
	if len(args) < 2 {
		ctx.Reply(platform.TextMessage("用法: /ar cooldown <ID> <秒>"))
		return nil
	}
	id := args[0]
	var seconds int
	fmt.Sscanf(args[1], "%d", &seconds)

	p.mu.Lock()
	for _, r := range p.rules {
		if r.ID == id {
			r.Cooldown = seconds
			p.mu.Unlock()
			p.save()
			ctx.Reply(platform.TextMessage(fmt.Sprintf("规则 #%s 冷却已设置为 %d 秒", id, seconds)))
			return nil
		}
	}
	p.mu.Unlock()
	ctx.Reply(platform.TextMessage(fmt.Sprintf("规则 #%s 不存在", id)))
	return nil
}

func (p *Plugin) Match(content string) []*Rule {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var matched []*Rule
	for _, r := range p.rules {
		switch r.Mode {
		case MatchExact:
			if content == r.Keyword {
				matched = append(matched, r)
			}
		case MatchContains:
			if strings.Contains(content, r.Keyword) {
				matched = append(matched, r)
			}
		case MatchPrefix:
			if strings.HasPrefix(content, r.Keyword) {
				matched = append(matched, r)
			}
		case MatchRegex:
			re, err := regexp.Compile(r.Keyword)
			if err == nil && re.MatchString(content) {
				matched = append(matched, r)
			}
		}
	}
	return matched
}

func (p *Plugin) CheckCooldown(ruleID, userID string) bool {
	key := ruleID + ":" + userID
	p.mu.RLock()
	last, ok := p.lastTrigger[key]
	p.mu.RUnlock()
	if !ok {
		return true
	}
	for _, r := range p.rules {
		if r.ID == ruleID && r.Cooldown > 0 {
			return time.Since(last) > time.Duration(r.Cooldown)*time.Second
		}
	}
	return true
}

func (p *Plugin) recordTrigger(ruleID, userID string) {
	key := ruleID + ":" + userID
	p.mu.Lock()
	p.lastTrigger[key] = time.Now()
	p.mu.Unlock()
}

func (p *Plugin) Middleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			content := ctx.GetMessageContent()
			if strings.HasPrefix(content, p.prefix) {
				return next(ctx)
			}
			matches := p.Match(content)
			for _, r := range matches {
				userID := ctx.GetSenderInfo().ID
				if !p.CheckCooldown(r.ID, userID) {
					continue
				}
				p.recordTrigger(r.ID, userID)
				ctx.Reply(platform.TextMessage(r.Response))
			}
			return next(ctx)
		}
	}
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
	data := struct {
		Rules  []*Rule `json:"rules"`
		NextID int64   `json:"next_id"`
	}{
		Rules:  make([]*Rule, len(p.rules)),
		NextID: p.nextID,
	}
	copy(data.Rules, p.rules)
	p.mu.RUnlock()
	if err := jsonfile.Write(p.dataFile, data); err != nil {
		logger.WithError(err).Warn("[AutoResponder] Failed to save")
	}
}

func (p *Plugin) load() {
	if p.dataFile == "" {
		return
	}
	data, err := jsonfile.Read[struct {
		Rules  []*Rule `json:"rules"`
		NextID int64   `json:"next_id"`
	}](p.dataFile)
	if err != nil {
		return
	}
	p.mu.Lock()
	p.rules = data.Rules
	p.nextID = data.NextID
	p.mu.Unlock()
	logger.Infof("[AutoResponder] Loaded %d rules", len(data.Rules))
}
