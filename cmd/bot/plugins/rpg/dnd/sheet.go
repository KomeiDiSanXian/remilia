package dnd

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/KomeiDiSanXian/remilia/infra/storage"
)

// SheetManager D&D 5e 角色卡 CRUD 管理器，封装 GORM 操作。
type SheetManager struct {
	db *storage.Plugin
}

// NewSheetManager 创建角色卡管理器。
func NewSheetManager(db *storage.Plugin) *SheetManager {
	return &SheetManager{db: db}
}

// AbilityMod 计算 D&D 5e 属性调整值：(属性-10)/2 向下取整。
func AbilityMod(score int) int {
	return int(math.Floor(float64(score-10) / 2))
}

// CreateCharacter 创建初始角色，默认全属性 10，等级 1。
func (m *SheetManager) CreateCharacter(userID, userName, name string) (*Character, error) {
	c := &Character{
		UserID:           userID,
		UserName:         userName,
		Name:             name,
		Level:            1,
		ProficiencyBonus: 2,
		STR:              10, DEX: 10, CON: 10, INT: 10, WIS: 10, CHA: 10,
		MaxHP:      10,
		CurrentHP:  10,
		AC:         10,
		Speed:      30,
		Initiative: 0,
	}
	return c, m.db.Create(c)
}

// GetCharacter 按用户和角色名查询角色。
func (m *SheetManager) GetCharacter(userID, name string) (*Character, error) {
	var c Character
	err := m.db.Where("user_id = ? AND name = ?", userID, name).First(&c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetCharacters 查询某用户的所有角色。
func (m *SheetManager) GetCharacters(userID string) ([]Character, error) {
	var chars []Character
	err := m.db.Where("user_id = ?", userID).Find(&chars)
	return chars, err
}

// UpdateCharacter 保存角色变更。
func (m *SheetManager) UpdateCharacter(c *Character) error {
	return m.db.Save(c)
}

// DeleteCharacter 删除指定角色。
func (m *SheetManager) DeleteCharacter(userID, name string) error {
	return m.db.Where("user_id = ? AND name = ?", userID, name).Delete(&Character{})
}

// SaveRecord 保存检定记录供追溯。
func (m *SheetManager) SaveRecord(userID, charName, recType, detail, result, diceResult string) {
	_ = m.db.Create(&Record{
		UserID:        userID,
		CharacterName: charName,
		Type:          recType,
		Detail:        detail,
		Result:        result,
		DiceResult:    diceResult,
	})
}

// FormatSheet 格式化角色卡为可读文本，显示属性、战斗数据、技能熟练和特性。
func FormatSheet(c *Character) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("╔══ %s ══╗\n", c.Name))
	b.WriteString(fmt.Sprintf("种族: %s  职业: %s\n", c.Race, c.Class))
	b.WriteString(fmt.Sprintf("等级: %d  阵营: %s  熟练加值: +%d\n", c.Level, c.Alignment, c.ProficiencyBonus))

	b.WriteString("\n── 属性 ──\n")
	for _, ab := range []struct{ Name, Abbr string }{
		{"力量", "STR"}, {"敏捷", "DEX"}, {"体质", "CON"},
		{"智力", "INT"}, {"感知", "WIS"}, {"魅力", "CHA"},
	} {
		val := getAbility(c, ab.Abbr)
		mod := AbilityMod(val)
		modStr := fmt.Sprintf("%+d", mod)
		b.WriteString(fmt.Sprintf("  %s(%s): %d (%s)\n", ab.Name, ab.Abbr, val, modStr))
	}

	b.WriteString("\n── 战斗 ──\n")
	b.WriteString(fmt.Sprintf("HP: %d/%d  AC: %d  速度: %dft\n", c.CurrentHP, c.MaxHP, c.AC, c.Speed))
	b.WriteString(fmt.Sprintf("先攻: %+d\n", c.Initiative))

	if c.SkillProfsJSON != "" {
		var profs map[string]bool
		if err := json.Unmarshal([]byte(c.SkillProfsJSON), &profs); err == nil && len(profs) > 0 {
			b.WriteString("\n── 技能熟练 ──\n")
			for skill, proficient := range profs {
				if proficient {
					ab := skillAbility(skill)
					mod := AbilityMod(getAbility(c, ab))
					total := mod + c.ProficiencyBonus
					b.WriteString(fmt.Sprintf("  %s: %+d\n", skill, total))
				}
			}
		}
	}

	if c.FeaturesJSON != "" {
		var features []string
		if err := json.Unmarshal([]byte(c.FeaturesJSON), &features); err == nil && len(features) > 0 {
			b.WriteString("\n── 特性 ──\n")
			b.WriteString("  " + strings.Join(features, ", "))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func getAbility(c *Character, abbr string) int {
	switch strings.ToUpper(abbr) {
	case "STR":
		return c.STR
	case "DEX":
		return c.DEX
	case "CON":
		return c.CON
	case "INT":
		return c.INT
	case "WIS":
		return c.WIS
	case "CHA":
		return c.CHA
	default:
		return 10
	}
}

func skillAbility(skill string) string {
	m := map[string]string{
		"运动": "STR", "杂技": "DEX", "巧手": "DEX", "隐匿": "DEX",
		"奥秘": "INT", "历史": "INT", "调查": "INT", "自然": "INT", "宗教": "INT",
		"驯兽": "WIS", "洞悉": "WIS", "医药": "WIS", "察觉": "WIS", "生存": "WIS",
		"欺瞒": "CHA", "威吓": "CHA", "表演": "CHA", "游说": "CHA",
	}
	if v, ok := m[skill]; ok {
		return v
	}
	return "WIS"
}

// AbilityAbbr 将中文/英文属性名映射为标准缩写（STR/DEX/CON/INT/WIS/CHA）。
func AbilityAbbr(name string) string {
	m := map[string]string{
		"力量": "STR", "STR": "STR",
		"敏捷": "DEX", "DEX": "DEX",
		"体质": "CON", "CON": "CON",
		"智力": "INT", "INT": "INT",
		"感知": "WIS", "WIS": "WIS",
		"魅力": "CHA", "CHA": "CHA",
	}
	if v, ok := m[strings.ToUpper(name)]; ok {
		return v
	}
	return strings.ToUpper(name)
}
