package coc

import (
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/rpg/dice"
)

// RollResult COC 检定结果。
type RollResult struct {
	Value     int    // D100 结果
	Target    int    // 目标技能值
	Level     string // 成功等级
	IsSuccess bool   // 是否成功
	Raw       string // 格式化文本
}

// CheckSkill 执行 COC 7th 技能检定。
// 掷 D100 并与目标值比较，按规则判断成功等级。
func CheckSkill(d dice.Servicer, skillValue int) (*RollResult, error) {
	r, err := d.Roll("1d100")
	if err != nil {
		return nil, err
	}
	return evaluateCOCRoll(r.Total, skillValue), nil
}

func evaluateCOCRoll(value int, target int) *RollResult {
	level := ""
	success := false

	switch {
	case value == 1:
		level = "✨ 大成功!"
		success = true
	case value >= 96:
		level = "💀 大失败!"
		success = false
	case value <= target/5:
		level = "⭐ 极难成功"
		success = true
	case value <= target/2:
		level = "★ 困难成功"
		success = true
	case value <= target:
		level = "✅ 成功"
		success = true
	default:
		level = "❌ 失败"
		success = false
	}

	return &RollResult{
		Value:     value,
		Target:    target,
		Level:     level,
		IsSuccess: success,
		Raw:       fmt.Sprintf("D100=%d [目标:%d] %s", value, target, level),
	}
}

// SanCheck 执行 COC 7th 理智（SAN）检定。
// 成功时损失 lossSuccess 点 SAN，失败时损失 lossFailure 点。
// 返回检定结果、实际损失值、状态描述。
func SanCheck(d dice.Servicer, currentSAN, lossSuccess, lossFailure int) (*RollResult, int, string, error) {
	r, err := CheckSkill(d, currentSAN)
	if err != nil {
		return nil, 0, "", err
	}

	loss := lossFailure
	desc := "理智检定失败"
	if r.IsSuccess {
		loss = lossSuccess
		desc = "理智检定成功"
	}

	newSAN := max(currentSAN-loss, 0)

	status := "正常"
	if newSAN <= 0 {
		status = "🛑 永久疯狂!"
	} else if loss >= 5 {
		status = "⚠️ 临时 insanity!"
	}

	result := fmt.Sprintf("%s: 损失 %d SAN (%d → %d) %s", desc, loss, currentSAN, newSAN, status)
	return r, loss, strings.TrimSpace(result), nil
}

// OpposedRoll 执行 COC 7th 对抗检定。双方各掷 D100，成功且值低者胜。
func OpposedRoll(d dice.Servicer, val1, val2 int) (string, error) {
	r1, err := CheckSkill(d, val1)
	if err != nil {
		return "", err
	}
	r2, err := CheckSkill(d, val2)
	if err != nil {
		return "", err
	}

	winner := "平手"
	if r1.IsSuccess && !r2.IsSuccess {
		winner = "我方胜"
	} else if !r1.IsSuccess && r2.IsSuccess {
		winner = "对方胜"
	} else if r1.IsSuccess && r2.IsSuccess {
		if r1.Value < r2.Value {
			winner = "我方胜"
		} else if r2.Value < r1.Value {
			winner = "对方胜"
		}
	}

	return fmt.Sprintf("我方: %s\n对方: %s\n结果: %s", r1.Raw, r2.Raw, winner), nil
}

// PushedRoll 执行 COC 7th 推骰。首次失败后可尝试，但推骰失败后果更严重。
func PushedRoll(d dice.Servicer, skillValue int) (*RollResult, error) {
	r, err := CheckSkill(d, skillValue)
	if err != nil {
		return nil, err
	}
	r.Raw = "推骰: " + r.Raw
	if !r.IsSuccess && r.Value < 96 {
		r.Raw += "\n⚠️ 推骰失败将付出代价！"
	}
	return r, nil
}

// GetSkillValue 获取角色某项技能或属性（×5）的值。
// 对于 STR/CON/POW 等属性返回 ×5 后的百分比值。
func GetSkillValue(c *Character, skillName string) int {
	abbr := strings.ToUpper(skillName)
	switch abbr {
	case "STR":
		return c.STR * 5
	case "CON":
		return c.CON * 5
	case "POW":
		return c.POW * 5
	case "DEX":
		return c.DEX * 5
	case "APP":
		return c.APP * 5
	case "SIZ":
		return c.SIZ * 5
	case "INT":
		return c.INT * 5
	case "EDU":
		return c.EDU * 5
	case "幸运", "LUCK":
		return c.LUCK
	default:
		if c.SkillsJSON != "" {
			skills := parseSkills(c.SkillsJSON)
			if val, ok := skills[skillName]; ok {
				return val
			}
			for k, v := range skills {
				if strings.EqualFold(k, skillName) {
					return v
				}
			}
		}
		return 0
	}
}
