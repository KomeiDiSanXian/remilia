package dnd

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

// Plugin D&D 5e 规则插件实例。集成了角色管理、属性检定、技能检定和先攻。
type Plugin struct {
	sheet *SheetManager
	dice  dice.Servicer
	log   plugin.Logger
}

func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:    "dnd",
		Version: "1.0.0",
		Deps:    []string{"dice", "storage"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "D&D 5e — 角色卡管理、属性检定、技能检定、豁免、先攻",
			Category:    "跑团",
			Tags:        []string{"DND", "龙与地下城", "TRPG", "跑团", "角色卡", "5e"},
			HelpText: `D&D 5e 跑团插件

角色管理：
  /dnd create <角色名>         — 创建角色
  /dnd delete <角色名>         — 删除角色
  /dnd sheet [角色名]          — 查看角色卡
  /dnd list                   — 列出所有角色
  /dnd set <属性> <值>         — 设置属性

检定：
  /check <属性> [优/劣势]      — 属性检定，如 /check STR、/check DEX adv
  /save <属性> [优/劣势]       — 豁免检定
  /skill <技能名> [优/劣势]     — 技能检定
  /init [角色名]               — 先攻检定`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log

			diceSvc, ok := plugin.TryService[dice.Servicer](ctx, "dice")
			if !ok {
				return nil, fmt.Errorf("dnd: dice service not available")
			}
			p.dice = diceSvc

			storageSvc, ok := plugin.TryService[*storage.Plugin](ctx, "storage")
			if !ok {
				return nil, fmt.Errorf("dnd: storage service not available")
			}

			if err := storageSvc.AutoMigrate(&Character{}, &Record{}); err != nil {
				return nil, fmt.Errorf("dnd: auto migrate: %w", err)
			}
			p.sheet = NewSheetManager(storageSvc)

			dndDef := command.NewDef("dnd").Description("D&D 5e 角色管理与属性设置").
				SubCommand(command.NewDef("create").Description("创建角色").Build()).
				SubCommand(command.NewDef("delete").Description("删除角色").Build()).
				SubCommand(command.NewDef("sheet").Description("查看角色卡").Build()).
				SubCommand(command.NewDef("list").Description("列出所有角色").Build()).
				SubCommand(command.NewDef("set").Description("设置属性或角色信息").Build()).
				Build()
			ctx.OnCommandDefWith("", "/dnd", dndDef, p.handleDND)

			checkDef := command.NewDef("check").Description("D&D 属性检定").
				Arg("ability", "属性缩写 STR/DEX/CON/INT/WIS/CHA", true).
				Arg("advantage", "优势/劣势 adv/dis（可选）", false).
				Arg("character_name", "角色名（可选）", false).
				Example("/check STR adv").Example("/check DEX").Build()
			ctx.OnCommandDefWith("", "/check", checkDef, p.handleCheck)

			saveDef := command.NewDef("save").Description("D&D 豁免检定").
				Arg("ability", "属性缩写 STR/DEX/CON/INT/WIS/CHA", true).
				Arg("advantage", "优势/劣势 adv/dis（可选）", false).
				Arg("character_name", "角色名（可选）", false).
				Example("/save DEX adv").Example("/save WIS").Build()
			ctx.OnCommandDefWith("", "/save", saveDef, p.handleSave)

			skillDef := command.NewDef("skill").Description("D&D 技能检定").
				Arg("skill_name", "技能名（如 察觉、隐匿、游说）", true).
				Arg("advantage", "优势/劣势 adv/dis（可选）", false).
				Arg("character_name", "角色名（可选）", false).
				Example("/skill 察觉").Example("/skill 隐匿 adv 盗贼").Build()
			ctx.OnCommandDefWith("", "/skill", skillDef, p.handleSkill)

			initDef := command.NewDef("init").Description("D&D 先攻检定").
				Arg("character_name", "角色名（可选）", false).
				Example("/init").Example("/init 战士").Build()
			ctx.OnCommandDefWith("", "/init", initDef, p.handleInit)

			return p, nil
		},
	}
}

// handleDND 处理 /dnd 命令路由，分发到各子命令处理器。
func (p *Plugin) handleDND(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		return ctx.ReplyText("D&D 5e 插件 — 输入 /dnd help 查看帮助")
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
	case "set":
		return p.cmdSet(ctx, userID, parsed)
	case "help":
		return ctx.ReplyText("D&D 5e 跑团插件\n角色管理: /dnd create <角色名>, /dnd sheet [角色名], /dnd list\n检定: /check <属性> [adv/dis], /save <属性> [adv/dis], /skill <技能名> [adv/dis], /init")
	default:
		return ctx.ReplyError("未知子命令，可用: create, delete, sheet, list, set, help")
	}
}

// cmdCreate 处理 /dnd create：创建初始角色。
func (p *Plugin) cmdCreate(ctx *eventctx.Context, userID, userName string, parsed *command.Args) error {
	name := parsed.Get(1)
	if name == "" {
		return ctx.ReplyError("用法: /dnd create <角色名>")
	}

	_, err := p.sheet.GetCharacter(userID, name)
	if err == nil {
		return ctx.ReplyError(fmt.Sprintf("角色 %q 已存在", name))
	}

	c, err := p.sheet.CreateCharacter(userID, userName, name)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("创建失败: %v", err))
	}

	return ctx.ReplyText(fmt.Sprintf("✅ 角色 %q 创建成功！\n\n%s\n\n💡 使用 /dnd set <属性> <值> 设置属性\n使用 /dnd set class <职业> 和 /dnd set race <种族> 设置角色信息", name, FormatSheet(c)))
}

// cmdDelete 处理 /dnd delete：删除角色。
func (p *Plugin) cmdDelete(ctx *eventctx.Context, userID string, parsed *command.Args) error {
	name := parsed.Get(1)
	if name == "" {
		return ctx.ReplyError("用法: /dnd delete <角色名>")
	}
	if err := p.sheet.DeleteCharacter(userID, name); err != nil {
		return ctx.ReplyError(fmt.Sprintf("删除失败: %v", err))
	}
	return ctx.ReplySuccess(fmt.Sprintf("角色 %q 已删除", name))
}

// cmdSheet 处理 /dnd sheet：查看角色卡。
func (p *Plugin) cmdSheet(ctx *eventctx.Context, userID string, parsed *command.Args) error {
	name := parsed.Get(1)
	if name == "" {
		chars, err := p.sheet.GetCharacters(userID)
		if err != nil || len(chars) == 0 {
			return ctx.ReplyError("你还没有角色，使用 /dnd create <角色名> 创建")
		}
		name = chars[0].Name
	}

	c, err := p.sheet.GetCharacter(userID, name)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("角色 %q 不存在", name))
	}
	return ctx.ReplyText(FormatSheet(c))
}

// cmdList 处理 /dnd list：列出用户所有角色。
func (p *Plugin) cmdList(ctx *eventctx.Context, userID string) error {
	chars, err := p.sheet.GetCharacters(userID)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("查询失败: %v", err))
	}
	if len(chars) == 0 {
		return ctx.ReplyText("你还没有角色，使用 /dnd create <角色名> 创建")
	}

	var names []string
	for _, c := range chars {
		status := ""
		if c.CurrentHP <= 0 {
			status = " 💀"
		} else if c.CurrentHP <= c.MaxHP/2 {
			status = " ⚠️"
		}
		names = append(names, fmt.Sprintf("  Lv.%d %s (HP: %d/%d AC: %d)%s", c.Level, c.Name, c.CurrentHP, c.MaxHP, c.AC, status))
	}
	return ctx.ReplyText("你的角色:\n" + strings.Join(names, "\n"))
}

// cmdSet 处理 /dnd set：设置角色属性（STR/DEX/CON/INT/WIS/CHA、等级、HP、AC 等）。
func (p *Plugin) cmdSet(ctx *eventctx.Context, userID string, parsed *command.Args) error {
	if len(parsed.Positional) < 3 {
		return ctx.ReplyError("用法: /dnd set <属性> <值> [角色名]")
	}

	field := strings.ToUpper(parsed.Positional[1])
	valStr := parsed.Positional[2]
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

	switch field {
	case "STR", "DEX", "CON", "INT", "WIS", "CHA":
		val := 0
		fmt.Sscanf(valStr, "%d", &val)
		if val < 1 || val > 30 {
			return ctx.ReplyError("属性值需在 1-30 之间")
		}
		switch field {
		case "STR":
			c.STR = val
		case "DEX":
			c.DEX = val
		case "CON":
			c.CON = val
		case "INT":
			c.INT = val
		case "WIS":
			c.WIS = val
		case "CHA":
			c.CHA = val
		}
	case "CLASS":
		c.Class = valStr
	case "RACE":
		c.Race = valStr
	case "LEVEL":
		lvl := 0
		fmt.Sscanf(valStr, "%d", &lvl)
		if lvl < 1 || lvl > 20 {
			return ctx.ReplyError("等级需在 1-20 之间")
		}
		c.Level = lvl
		c.ProficiencyBonus = ((lvl-1)/4 + 1) + 1
	case "HP", "MAXHP":
		val := 0
		fmt.Sscanf(valStr, "%d", &val)
		if val > 0 {
			c.MaxHP = val
			if c.CurrentHP > c.MaxHP {
				c.CurrentHP = c.MaxHP
			}
		}
	case "AC":
		val := 0
		fmt.Sscanf(valStr, "%d", &val)
		c.AC = val
	case "CURRENTHP":
		val := 0
		fmt.Sscanf(valStr, "%d", &val)
		if val < 0 {
			val = 0
		}
		if val > c.MaxHP {
			val = c.MaxHP
		}
		c.CurrentHP = val
	default:
		return ctx.ReplyError(fmt.Sprintf("未知属性 %q，可用: STR/DEX/CON/INT/WIS/CHA, CLASS, RACE, LEVEL, HP, AC", field))
	}

	if err := p.sheet.UpdateCharacter(c); err != nil {
		return ctx.ReplyError(fmt.Sprintf("保存失败: %v", err))
	}
	return ctx.ReplySuccess(fmt.Sprintf("角色 %q 已更新", charName))
}

// handleCheck 处理 /check 命令：属性检定（支持优势/劣势）。
func (p *Plugin) handleCheck(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		return ctx.ReplyError("用法: /check <属性> [adv/dis] [角色名]，如 /check STR adv")
	}

	userID := ctx.GetSenderID()
	abbr := AbilityAbbr(parsed.Positional[0])
	advantage := parseAdvantage(parsed, 1)

	charName := getCharName(parsed, 2)
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

	mod := GetModifier(c, abbr)
	r, err := AbilityCheck(p.dice, mod, advantage)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("检定失败: %v", err))
	}

	p.sheet.SaveRecord(userID, charName, "属性检定", fmt.Sprintf("%s %+d", abbr, mod), r.Raw, fmt.Sprintf("D20=%d", r.Roll))
	return ctx.ReplyText(fmt.Sprintf("🎲 %s %s检定:\n%s", charName, abbr, r.Raw))
}

// handleSave 处理 /save 命令：豁免检定（支持优势/劣势和熟练加值）。
func (p *Plugin) handleSave(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		return ctx.ReplyError("用法: /save <属性> [adv/dis] [角色名]")
	}

	userID := ctx.GetSenderID()
	abbr := AbilityAbbr(parsed.Positional[0])
	advantage := parseAdvantage(parsed, 1)

	charName := getCharName(parsed, 2)
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

	mod := GetModifier(c, abbr)
	if IsSavingProficient(c, abbr) {
		mod += c.ProficiencyBonus
	}

	r, err := SavingThrow(p.dice, mod, advantage)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("豁免检定失败: %v", err))
	}

	p.sheet.SaveRecord(userID, charName, "豁免检定", fmt.Sprintf("%s %+d", abbr, mod), r.Raw, fmt.Sprintf("D20=%d", r.Roll))
	return ctx.ReplyText(fmt.Sprintf("🛡️ %s %s豁免:\n%s", charName, abbr, r.Raw))
}

// handleSkill 处理 /skill 命令：技能检定（自动计算熟练加值）。
func (p *Plugin) handleSkill(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		return ctx.ReplyError("用法: /skill <技能名> [adv/dis] [角色名]")
	}

	userID := ctx.GetSenderID()
	skill := parsed.Positional[0]
	advantage := parseAdvantage(parsed, 1)

	charName := getCharName(parsed, 2)
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

	r, err := SkillCheck(p.dice, c, skill, advantage)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("技能检定失败: %v", err))
	}

	p.sheet.SaveRecord(userID, charName, "技能检定", skill, r.Raw, fmt.Sprintf("D20=%d", r.Roll))
	return ctx.ReplyText(fmt.Sprintf("🎲 %s %s检定:\n%s", charName, skill, r.Raw))
}

// handleInit 处理 /init 命令：先攻检定（D20 + 敏捷调整值）。
func (p *Plugin) handleInit(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err == nil && len(parsed.Positional) > 0 {
		charName := parsed.Positional[0]
		userID := ctx.GetSenderID()
		c, err := p.sheet.GetCharacter(userID, charName)
		if err != nil {
			return ctx.ReplyError(fmt.Sprintf("角色 %q 不存在", charName))
		}
		mod := GetModifier(c, "DEX")
		r, err := Initiative(p.dice, mod)
		if err != nil {
			return ctx.ReplyError(fmt.Sprintf("先攻检定失败: %v", err))
		}
		return ctx.ReplyText(fmt.Sprintf("⚔️ %s 先攻:\n%s", charName, r.Raw))
	}

	userID := ctx.GetSenderID()
	chars, err := p.sheet.GetCharacters(userID)
	if err != nil || len(chars) == 0 {
		return ctx.ReplyError("你还没有角色，请先创建角色")
	}

	c := &chars[0]
	mod := GetModifier(c, "DEX")
	r, err := Initiative(p.dice, mod)
	if err != nil {
		return ctx.ReplyError(fmt.Sprintf("先攻检定失败: %v", err))
	}
	return ctx.ReplyText(fmt.Sprintf("⚔️ %s 先攻:\n%s", c.Name, r.Raw))
}

// parseAdvantage 从命令参数中解析优势/劣势状态。
func parseAdvantage(parsed *command.Args, idx int) string {
	tok := parsed.Get(idx)
	switch strings.ToLower(tok) {
	case "adv", "advantage", "优":
		return "advantage"
	case "dis", "disadvantage", "劣":
		return "disadvantage"
	default:
		return "none"
	}
}

// getCharName 从命令参数中提取角色名（跳过优势/劣势标记）。
func getCharName(parsed *command.Args, startIdx int) string {
	for i := startIdx; i < parsed.Len(); i++ {
		tok := parsed.Get(i)
		if tok != "" && !isAdvantageToken(tok) {
			return tok
		}
	}
	return ""
}

// isAdvantageToken 判断 token 是否为优势/劣势标记。
func isAdvantageToken(tok string) bool {
	switch strings.ToLower(tok) {
	case "adv", "advantage", "优", "dis", "disadvantage", "劣":
		return true
	}
	return false
}

// ListTools 返回 AI 可调用的工具列表。实现 ai.ToolProvider。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "dnd_ability_check",
			Categories:  []string{"dnd", "rpg"},
			Description: "进行 D&D 5e 属性检定。属性: STR/DEX/CON/INT/WIS/CHA。优势: advantage, disadvantage, none。",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"character_name": {Type: "string", Description: "角色名"},
					"ability":        {Type: "string", Description: "属性缩写（STR/DEX/CON/INT/WIS/CHA）"},
					"advantage":      {Type: "string", Description: "优势状态: advantage/disadvantage/none（可选）"},
				},
				Required: []string{"character_name", "ability"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				charName, _ := args["character_name"].(string)
				ability, _ := args["ability"].(string)
				adv, _ := args["advantage"].(string)
				if charName == "" || ability == "" {
					return "", fmt.Errorf("请提供角色名和属性")
				}
				if adv == "" {
					adv = "none"
				}
				c, err := p.sheet.GetCharacter("ai", charName)
				if err != nil {
					return "", fmt.Errorf("角色 %q 不存在", charName)
				}
				mod := GetModifier(c, AbilityAbbr(ability))
				r, err := AbilityCheck(p.dice, mod, adv)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%s %s检定: %s", charName, ability, r.Raw), nil
			},
		},
		{
			Name:        "dnd_skill_check",
			Categories:  []string{"dnd", "rpg"},
			Description: "进行 D&D 5e 技能检定。技能名如: 运动、察觉、隐匿、游说等。",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"character_name": {Type: "string", Description: "角色名"},
					"skill":          {Type: "string", Description: "技能名"},
					"advantage":      {Type: "string", Description: "优势状态: advantage/disadvantage/none（可选）"},
				},
				Required: []string{"character_name", "skill"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				charName, _ := args["character_name"].(string)
				skill, _ := args["skill"].(string)
				adv, _ := args["advantage"].(string)
				if charName == "" || skill == "" {
					return "", fmt.Errorf("请提供角色名和技能名")
				}
				if adv == "" {
					adv = "none"
				}
				c, err := p.sheet.GetCharacter("ai", charName)
				if err != nil {
					return "", fmt.Errorf("角色 %q 不存在", charName)
				}
				r, err := SkillCheck(p.dice, c, skill, adv)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%s %s检定: %s", charName, skill, r.Raw), nil
			},
		},
		{
			Name:        "dnd_saving_throw",
			Categories:  []string{"dnd", "rpg"},
			Description: "进行 D&D 5e 豁免检定。属性: STR/DEX/CON/INT/WIS/CHA。",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"character_name": {Type: "string", Description: "角色名"},
					"ability":        {Type: "string", Description: "属性缩写（STR/DEX/CON/INT/WIS/CHA）"},
					"advantage":      {Type: "string", Description: "优势状态: advantage/disadvantage/none（可选）"},
				},
				Required: []string{"character_name", "ability"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				charName, _ := args["character_name"].(string)
				ability, _ := args["ability"].(string)
				adv, _ := args["advantage"].(string)
				if charName == "" || ability == "" {
					return "", fmt.Errorf("请提供角色名和属性")
				}
				if adv == "" {
					adv = "none"
				}
				c, err := p.sheet.GetCharacter("ai", charName)
				if err != nil {
					return "", fmt.Errorf("角色 %q 不存在", charName)
				}
				mod := GetModifier(c, AbilityAbbr(ability))
				if IsSavingProficient(c, AbilityAbbr(ability)) {
					mod += c.ProficiencyBonus
				}
				r, err := SavingThrow(p.dice, mod, adv)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%s %s豁免: %s", charName, ability, r.Raw), nil
			},
		},
		{
			Name:        "dnd_initiative",
			Categories:  []string{"dnd", "rpg"},
			Description: "进行 D&D 5e 先攻检定。",
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
				mod := GetModifier(c, "DEX")
				r, err := Initiative(p.dice, mod)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%s 先攻: %s", charName, r.Raw), nil
			},
		},
		{
			Name:        "view_dnd_character",
			Categories:  []string{"dnd", "rpg"},
			Description: "查看 D&D 5e 角色卡信息。",
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
			Name:        "dnd_dungeon_master",
			Description: "D&D 地下城主助手 — 属性检定、技能检定、豁免、先攻、角色查询",
			Prompt: `你是一个龙与地下城 5e 的地下城主助手。
当用户需要以下操作时，使用对应的工具：
  1. 属性检定 → dnd_ability_check（支持 STR/DEX/CON/INT/WIS/CHA）
  2. 技能检定 → dnd_skill_check（如 运动、察觉、隐匿等）
  3. 豁免检定 → dnd_saving_throw
  4. 先攻检定 → dnd_initiative
  5. 查看角色 → view_dnd_character

检定规则：
  - D20 + 属性调整值（(属性-10)/2 向下取整）
  - 熟练技能额外加熟练加值
  - 优势: 掷 2d20 取高；劣势: 掷 2d20 取低
  - D20=20: 重击；D20=1: 大失败

用生动的叙事风格描述检定结果，营造跑团氛围。`,
			Tools: p.ListTools(),
		},
	}
}
