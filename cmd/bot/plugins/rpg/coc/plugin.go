package coc

import (
	"context"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/rpg/dice"
)

// Plugin COC 7th 规则插件实例。集成了角色管理、技能检定和 SAN 检定。
type Plugin struct {
	sheet *SheetManager
	dice  dice.Servicer
	log   plugin.Logger
}

func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:    "coc",
		Version: "1.0.0",
		Deps:    []string{"dice", "storage"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "克苏鲁的呼唤 7th — 角色卡管理、技能检定、SAN 检定",
			Category:    "跑团",
			Tags:        []string{"COC", "克苏鲁", "TRPG", "跑团", "角色卡", "SAN"},
			HelpText: `COC 7th 跑团插件

角色管理：
  /coc create <角色名>         — 创建角色（自动生成属性）
  /coc delete <角色名>         — 删除角色
  /coc sheet [角色名]          — 查看角色卡
  /coc list                   — 列出所有角色
  /coc skill <技能名> <值>     — 设置技能值

检定：
  /cc <技能名> [角色名]        — 技能检定（默认使用首个角色）
  /sc [成功损失] [失败损失]     — SAN 检定
  /coc luck [角色名]           — 幸运检定
  /coc push <技能名> [角色名]   — 推骰`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log

			diceSvc, ok := plugin.TryService[dice.Servicer](ctx, "dice")
			if !ok {
				return nil, fmt.Errorf("coc: dice service not available")
			}
			p.dice = diceSvc

			storageSvc, ok := plugin.TryService[*storage.Plugin](ctx, "storage")
			if !ok {
				return nil, fmt.Errorf("coc: storage service not available")
			}

			if err := storageSvc.AutoMigrate(&Character{}, &Record{}); err != nil {
				return nil, fmt.Errorf("coc: auto migrate: %w", err)
			}
			p.sheet = NewSheetManager(storageSvc)

			ctx.OnCommand("", "/coc", p.handleCOC)
			ctx.OnCommand("", "/cc", p.handleCC)
			ctx.OnCommand("", "/sc", p.handleSC)

			return p, nil
		},
	}
}

// handleCOC 处理 /coc 命令路由，分发到各子命令处理器。
func (p *Plugin) handleCOC(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		return ctx.ReplyText("COC 7th 插件 — 输入 /coc help 查看帮助")
	}

	userID := ctx.GetSenderID()
	userName := ctx.GetDisplayName()

	switch parsed.Positional[0] {
	case "create":
		return p.cmdCreate(ctx, userID, userName, parsed)
	case "delete":
		return p.cmdDelete(ctx, userID, parsed)
	case "sheet", "view":
		return p.cmdSheet(ctx, userID, parsed)
	case "list":
		return p.cmdList(ctx, userID)
	case "skill":
		return p.cmdSkill(ctx, userID, parsed)
	case "luck":
		return p.cmdLuck(ctx, userID, parsed)
	case "push":
		return p.cmdPush(ctx, userID, parsed)
	case "help":
		return ctx.ReplyText("COC 7th 跑团插件\n角色管理: /coc create <角色名>, /coc sheet [角色名], /coc list\n检定: /cc <技能名> [角色名], /sc [成功损失] [失败损失], /coc luck [角色名]")
	default:
		return ctx.ReplyError("未知子命令，可用: create, delete, sheet, list, skill, luck, push, help")
	}
}

// cmdCreate 处理 /coc create：创建新角色，自动生成 COC 7th 属性。
func (p *Plugin) cmdCreate(ctx *eventctx.Context, userID, userName string, parsed *command.Args) error {
	name := parsed.Get(1)
	if name == "" {
		return ctx.ReplyError("用法: /coc create <角色名>")
	}

	_, err := p.sheet.GetCharacter(userID, name)
	if err == nil {
		return ctx.ReplyError(fmt.Sprintf("角色 %q 已存在", name))
	}

	c, err := p.sheet.CreateCharacter(userID, userName, name, true)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("创建失败: %v", err))
	}

	edu := c.EDU * 5
	intel := c.INT * 5
	return ctx.ReplyText(fmt.Sprintf("✅ 角色 %q 创建成功！\n\n%s\n\n💡 使用 /coc skill <技能名> <值> 设置技能\n职业点数: %d  兴趣点数: %d", name, FormatSheet(c), edu, intel*2))
}

// cmdDelete 处理 /coc delete：删除角色。
func (p *Plugin) cmdDelete(ctx *eventctx.Context, userID string, parsed *command.Args) error {
	name := parsed.Get(1)
	if name == "" {
		return ctx.ReplyError("用法: /coc delete <角色名>")
	}

	if err := p.sheet.DeleteCharacter(userID, name); err != nil {
		return ctx.ReplyError(fmt.Sprintf("删除失败: %v", err))
	}
	return ctx.ReplySuccess(fmt.Sprintf("角色 %q 已删除", name))
}

// cmdSheet 处理 /coc sheet：查看角色卡。
func (p *Plugin) cmdSheet(ctx *eventctx.Context, userID string, parsed *command.Args) error {
	name := parsed.Get(1)
	if name == "" {
		chars, err := p.sheet.GetCharacters(userID)
		if err != nil || len(chars) == 0 {
			return ctx.ReplyError("你还没有角色，使用 /coc create <角色名> 创建")
		}
		name = chars[0].Name
	}

	c, err := p.sheet.GetCharacter(userID, name)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("角色 %q 不存在", name))
	}

	return ctx.ReplyText(FormatSheet(c))
}

// cmdList 处理 /coc list：列出用户所有角色状态。
func (p *Plugin) cmdList(ctx *eventctx.Context, userID string) error {
	chars, err := p.sheet.GetCharacters(userID)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("查询失败: %v", err))
	}
	if len(chars) == 0 {
		return ctx.ReplyText("你还没有角色，使用 /coc create <角色名> 创建")
	}

	var names []string
	for _, c := range chars {
		status := ""
		if c.CurrentHP <= 0 {
			status = " 💀"
		} else if c.CurrentHP <= c.HP/2 {
			status = " ⚠️"
		}
		names = append(names, fmt.Sprintf("  %s (HP: %d/%d SAN: %d/%d)%s", c.Name, c.CurrentHP, c.HP, c.CurrentSAN, c.SAN, status))
	}
	return ctx.ReplyText("你的角色:\n" + strings.Join(names, "\n"))
}

// cmdSkill 处理 /coc skill：设置角色技能值。
func (p *Plugin) cmdSkill(ctx *eventctx.Context, userID string, parsed *command.Args) error {
	if len(parsed.Positional) < 3 {
		return ctx.ReplyError("用法: /coc skill <技能名> <值> [角色名]")
	}

	skillName := parsed.Positional[1]
	skillVal := 0
	fmt.Sscanf(parsed.Positional[2], "%d", &skillVal)

	charName := parsed.Get(3)
	if charName == "" {
		chars, err := p.sheet.GetCharacters(userID)
		if err != nil || len(chars) == 0 {
			return ctx.ReplyError("你还没有角色，请指定角色名或先创建角色")
		}
		charName = chars[0].Name
	}

	c, err := p.sheet.GetCharacter(userID, charName)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("角色 %q 不存在", charName))
	}

	skills := parseSkills(c.SkillsJSON)
	if skills == nil {
		skills = make(map[string]int)
	}
	skills[skillName] = skillVal
	c.SkillsJSON = skillsToJSON(skills)

	if err := p.sheet.UpdateCharacter(c); err != nil {
		return ctx.ReplyError(fmt.Sprintf("保存失败: %v", err))
	}

	return ctx.ReplySuccess(fmt.Sprintf("角色 %q 技能 %s 设为 %d%%", charName, skillName, skillVal))
}

// cmdLuck 处理 /coc luck：幸运检定。
func (p *Plugin) cmdLuck(ctx *eventctx.Context, userID string, parsed *command.Args) error {
	charName := parsed.Get(1)
	if charName == "" {
		chars, err := p.sheet.GetCharacters(userID)
		if err != nil || len(chars) == 0 {
			return ctx.ReplyError("你还没有角色，请指定角色名或先创建角色")
		}
		charName = chars[0].Name
	}

	c, err := p.sheet.GetCharacter(userID, charName)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("角色 %q 不存在", charName))
	}

	r, err := CheckSkill(p.dice, c.LUCK)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("检定失败: %v", err))
	}

	p.sheet.SaveRecord(userID, charName, "幸运检定", fmt.Sprintf("幸运 %d%%", c.LUCK), r.Level, r.Raw)
	return ctx.ReplyText(fmt.Sprintf("🍀 %s 幸运检定:\n%s", charName, r.Raw))
}

// cmdPush 处理 /coc push：技能推骰。
func (p *Plugin) cmdPush(ctx *eventctx.Context, userID string, parsed *command.Args) error {
	skillName := parsed.Get(1)
	if skillName == "" {
		return ctx.ReplyError("用法: /coc push <技能名> [角色名]")
	}

	charName := parsed.Get(2)
	if charName == "" {
		chars, err := p.sheet.GetCharacters(userID)
		if err != nil || len(chars) == 0 {
			return ctx.ReplyError("你还没有角色，请指定角色名或先创建角色")
		}
		charName = chars[0].Name
	}

	c, err := p.sheet.GetCharacter(userID, charName)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("角色 %q 不存在", charName))
	}

	skillVal := GetSkillValue(c, skillName)
	if skillVal == 0 {
		return ctx.ReplyError(fmt.Sprintf("角色 %q 没有技能 %q", charName, skillName))
	}

	r, err := PushedRoll(p.dice, skillVal)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("检定失败: %v", err))
	}

	p.sheet.SaveRecord(userID, charName, "推骰", fmt.Sprintf("%s %d%%", skillName, skillVal), r.Level, r.Raw)
	return ctx.ReplyText(fmt.Sprintf("🎲 %s 推骰 [%s]:\n%s", charName, skillName, r.Raw))
}

// handleCC 处理 /cc 命令：简捷技能检定。
func (p *Plugin) handleCC(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		return ctx.ReplyError("用法: /cc <技能名> [角色名]")
	}

	userID := ctx.GetSenderID()
	skillName := parsed.Positional[0]
	charName := parsed.Get(1)

	if charName == "" {
		chars, err := p.sheet.GetCharacters(userID)
		if err != nil || len(chars) == 0 {
			return ctx.ReplyError("你还没有角色，请指定角色名或先创建角色")
		}
		charName = chars[0].Name
	}

	c, err := p.sheet.GetCharacter(userID, charName)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("角色 %q 不存在", charName))
	}

	skillVal := GetSkillValue(c, skillName)
	if skillVal == 0 {
		return ctx.ReplyError(fmt.Sprintf("角色 %q 没有技能 %q", charName, skillName))
	}

	r, err := CheckSkill(p.dice, skillVal)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("检定失败: %v", err))
	}

	p.sheet.SaveRecord(userID, charName, "技能检定", fmt.Sprintf("%s %d%%", skillName, skillVal), r.Level, r.Raw)
	return ctx.ReplyText(fmt.Sprintf("🎲 %s %s检定:\n%s", charName, skillName, r.Raw))
}

// handleSC 处理 /sc 命令：SAN 理智检定。
func (p *Plugin) handleSC(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil {
		return ctx.ReplyError("用法: /sc [成功损失] [失败损失]")
	}

	userID := ctx.GetSenderID()
	chars, err := p.sheet.GetCharacters(userID)
	if err != nil || len(chars) == 0 {
		return ctx.ReplyError("你还没有角色，请先创建角色")
	}
	c := &chars[0]
	charName := c.Name

	lossSuccess := 0
	lossFailure := 1
	if len(parsed.Positional) > 0 {
		fmt.Sscanf(parsed.Positional[0], "%d", &lossSuccess)
	}
	if len(parsed.Positional) > 1 {
		fmt.Sscanf(parsed.Positional[1], "%d", &lossFailure)
	}

	r, loss, resultText, err := SanCheck(p.dice, c.CurrentSAN, lossSuccess, lossFailure)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("SAN 检定失败: %v", err))
	}

	c.CurrentSAN -= loss
	if c.CurrentSAN < 0 {
		c.CurrentSAN = 0
	}
	_ = p.sheet.UpdateCharacter(c)

	p.sheet.SaveRecord(userID, charName, "SAN检定",
		fmt.Sprintf("SAN %d/%d -> %d/%d", c.CurrentSAN+loss, c.SAN, c.CurrentSAN, c.SAN),
		r.Level, r.Raw)

	return ctx.ReplyText(fmt.Sprintf("😱 %s SAN检定:\n%s\n%s", charName, r.Raw, resultText))
}

// ListTools 返回 AI 可调用的工具列表。实现 ai.ToolProvider。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "coc_skill_check",
			Categories:  []string{"coc", "rpg"},
			Description: "进行 COC 7th 技能检定。返回 D100 结果和成功等级（大成功/极难成功/困难成功/成功/失败/大失败）。",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"character_name": {Type: "string", Description: "角色名"},
					"skill_name":     {Type: "string", Description: "技能名或属性名（如 侦查、STR、图书馆）"},
				},
				Required: []string{"character_name", "skill_name"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				charName, _ := args["character_name"].(string)
				skillName, _ := args["skill_name"].(string)
				if charName == "" || skillName == "" {
					return "", fmt.Errorf("请提供角色名和技能名")
				}
				c, err := p.sheet.GetCharacter("ai", charName)
				if err != nil {
					return "", fmt.Errorf("角色 %q 不存在", charName)
				}
				val := GetSkillValue(c, skillName)
				if val == 0 {
					return "", fmt.Errorf("角色 %q 没有技能 %q", charName, skillName)
				}
				r, err := CheckSkill(p.dice, val)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%s %s检定: %s", charName, skillName, r.Raw), nil
			},
		},
		{
			Name:        "coc_sanity_check",
			Categories:  []string{"coc", "rpg"},
			Description: "进行 COC 7th 理智(SAN)检定。成功和失败时损失的 SAN 值可选，默认为 0/1d6。",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"character_name": {Type: "string", Description: "角色名"},
					"loss_success":   {Type: "integer", Description: "成功时 SAN 损失（可选，默认0）"},
					"loss_failure":   {Type: "integer", Description: "失败时 SAN 损失（可选，默认1）"},
				},
				Required: []string{"character_name"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				charName, _ := args["character_name"].(string)
				if charName == "" {
					return "", fmt.Errorf("请提供角色名")
				}
				lossS := 0
				if v, ok := args["loss_success"].(float64); ok {
					lossS = int(v)
				}
				lossF := 1
				if v, ok := args["loss_failure"].(float64); ok {
					lossF = int(v)
				}
				c, err := p.sheet.GetCharacter("ai", charName)
				if err != nil {
					return "", fmt.Errorf("角色 %q 不存在", charName)
				}
				r, loss, resultText, err := SanCheck(p.dice, c.CurrentSAN, lossS, lossF)
				if err != nil {
					return "", err
				}
				_ = loss
				return fmt.Sprintf("%s SAN检定: %s\n%s", charName, r.Raw, resultText), nil
			},
		},
		{
			Name:        "coc_luck_check",
			Categories:  []string{"coc", "rpg"},
			Description: "进行 COC 7th 幸运检定。",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"character_name": {Type: "string", Description: "角色名"},
				},
				Required: []string{"character_name"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				charName, _ := args["character_name"].(string)
				if charName == "" {
					return "", fmt.Errorf("请提供角色名")
				}
				c, err := p.sheet.GetCharacter("ai", charName)
				if err != nil {
					return "", fmt.Errorf("角色 %q 不存在", charName)
				}
				r, err := CheckSkill(p.dice, c.LUCK)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%s 幸运检定: %s", charName, r.Raw), nil
			},
		},
		{
			Name:        "view_coc_character",
			Categories:  []string{"coc", "rpg"},
			Description: "查看 COC 角色卡的完整信息。",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"character_name": {Type: "string", Description: "角色名"},
				},
				Required: []string{"character_name"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				charName, _ := args["character_name"].(string)
				if charName == "" {
					return "", fmt.Errorf("请提供角色名")
				}
				c, err := p.sheet.GetCharacter("ai", charName)
				if err != nil {
					return "", fmt.Errorf("角色 %q 不存在", charName)
				}
				return FormatSheet(c), nil
			},
		},
	}
}

// ListSkills 返回 AI 技能列表。实现 ai.SkillProvider。
func (p *Plugin) ListSkills() []ai.Skill {
	return []ai.Skill{
		{
			Name:        "coc_referee",
			Description: "COC 守秘人助手 — 技能检定、SAN 检定、幸运检定、角色查询",
			Prompt: `你是一个克苏鲁的呼唤 7th 守秘人助手。
当用户需要以下操作时，使用对应的工具：
  1. 技能检定 → coc_skill_check
  2. 理智检定 → coc_sanity_check
  3. 幸运检定 → coc_luck_check
  4. 查看角色 → view_coc_character

检定规则：
  - D100 ≤ 技能值 = 普通成功
  - D100 ≤ 技能值/2 = 困难成功
  - D100 ≤ 技能值/5 = 极难成功
  - D100 = 1 = 大成功
  - D100 ≥ 96 = 大失败（≥ 50%技能值）
  - D100 = 100 = 大失败

用生动的叙事风格描述检定结果，营造跑团氛围。`,
			Tools: p.ListTools(),
		},
	}
}
