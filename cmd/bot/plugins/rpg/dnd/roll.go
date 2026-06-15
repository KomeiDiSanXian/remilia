package dnd

import (
	"encoding/json"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/rpg/dice"
)

// CheckResult D&D 5e 检定结果，包含骰子值、加值、总计和特殊状态。
type CheckResult struct {
	Total     int    // 最终值（骰子 + 加值）
	Roll      int    // 骰子原始值（优势/劣势时为首个骰子）
	Mod       int    // 属性调整值 + 熟练加值
	Advantage string // 优势状态: advantage/disadvantage/none
	Raw       string // 格式化文本
	IsCrit    bool   // D20=20 重击
	IsFail    bool   // D20=1 大失败
}

// AbilityCheck 执行 D&D 5e 属性检定。支持优势（2d20^1）和劣势（2d20v1）。
func AbilityCheck(d dice.Servicer, mod int, advantage string) (*CheckResult, error) {
	expr := "1d20"
	if advantage == "advantage" {
		expr = "2d20^1"
	} else if advantage == "disadvantage" {
		expr = "2d20v1"
	}

	r, err := d.Roll(expr)
	if err != nil {
		return nil, err
	}

	roll := r.Details[0].Results[0]
	if advantage == "none" {
		roll = r.Total
	}

	total := roll + mod

	crit := roll == 20
	fail := roll == 1

	advLabel := ""
	switch advantage {
	case "advantage":
		advLabel = " (优势)"
	case "disadvantage":
		advLabel = " (劣势)"
	}

	raw := fmt.Sprintf("D20=%d%s %+d = %d", roll, advLabel, mod, total)
	if crit {
		raw += " 💥 重击!"
	}
	if fail {
		raw += " 💀 大失败!"
	}

	return &CheckResult{
		Total:     total,
		Roll:      roll,
		Mod:       mod,
		Advantage: advantage,
		Raw:       raw,
		IsCrit:    crit,
		IsFail:    fail,
	}, nil
}

// Initiative 执行 D&D 5e 先攻检定：D20 + 敏捷调整值。
func Initiative(d dice.Servicer, dexMod int) (*CheckResult, error) {
	r, err := d.Roll("1d20")
	if err != nil {
		return nil, err
	}

	total := r.Total + dexMod
	raw := fmt.Sprintf("D20=%d %+d = %d", r.Total, dexMod, total)

	return &CheckResult{
		Total: total,
		Roll:  r.Total,
		Mod:   dexMod,
		Raw:   raw,
	}, nil
}

// SavingThrow 执行 D&D 5e 豁免检定，与属性检定共享相同逻辑。
func SavingThrow(d dice.Servicer, mod int, advantage string) (*CheckResult, error) {
	return AbilityCheck(d, mod, advantage)
}

// GetModifier 计算角色某属性的调整值。
func GetModifier(c *Character, abbr string) int {
	return AbilityMod(getAbility(c, abbr))
}

// IsProficient 判断角色是否熟练某项技能。
func IsProficient(c *Character, skill string) bool {
	if c.SkillProfsJSON == "" {
		return false
	}
	var profs map[string]bool
	if err := json.Unmarshal([]byte(c.SkillProfsJSON), &profs); err != nil {
		return false
	}
	return profs[skill]
}

// IsSavingProficient 判断角色是否熟练某属性的豁免。
func IsSavingProficient(c *Character, abbr string) bool {
	if c.SavingProfsJSON == "" {
		return false
	}
	var profs map[string]bool
	if err := json.Unmarshal([]byte(c.SavingProfsJSON), &profs); err != nil {
		return false
	}
	return profs[abbr]
}

// SkillCheck 执行 D&D 5e 技能检定。自动计算对应属性调整值 + 熟练加值。
func SkillCheck(d dice.Servicer, c *Character, skill string, advantage string) (*CheckResult, error) {
	ab := skillAbility(skill)
	mod := AbilityMod(getAbility(c, ab))
	if IsProficient(c, skill) {
		mod += c.ProficiencyBonus
	}
	return AbilityCheck(d, mod, advantage)
}
