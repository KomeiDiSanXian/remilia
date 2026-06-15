package coc

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/storage"
)

var (
	rng   *rand.Rand
	rngMu sync.Mutex
)

func init() {
	rng = rand.New(rand.NewSource(time.Now().UnixNano()))
}

func rngInt(n int) int {
	rngMu.Lock()
	defer rngMu.Unlock()
	return rng.Intn(n) + 1
}

// SheetManager COC 角色卡 CRUD 管理器，封装 GORM 操作。
type SheetManager struct {
	db *storage.Plugin
}

// NewSheetManager 创建角色卡管理器。
func NewSheetManager(db *storage.Plugin) *SheetManager {
	return &SheetManager{db: db}
}

// CreateCharacter 创建新角色。autoGen=true 时自动按 COC 7th 规则生成属性。
func (m *SheetManager) CreateCharacter(userID, userName, name string, autoGen bool) (*Character, error) {
	c := &Character{
		UserID:   userID,
		UserName: userName,
		Name:     name,
	}
	if autoGen {
		m.autoGenerateAttrs(c)
	}
	return c, m.db.Create(c)
}

func (m *SheetManager) autoGenerateAttrs(c *Character) {
	c.STR = roll3d6()
	c.CON = roll3d6()
	c.POW = roll3d6()
	c.DEX = roll3d6()
	c.APP = roll3d6()
	c.SIZ = roll2d6plus6()
	c.INT = roll2d6plus6()
	c.EDU = roll2d6plus6()
	c.LUCK = roll3d6()

	c.SAN = c.POW * 5
	c.HP = int(math.Round(float64(c.CON+c.SIZ) / 10))
	c.MP = c.POW
	c.CurrentHP = c.HP
	c.CurrentMP = c.MP
	c.CurrentSAN = c.SAN

	strSum := c.STR + c.SIZ
	switch {
	case strSum <= 64:
		c.DB = -4
		c.Build = -2
	case strSum <= 84:
		c.DB = -2
		c.Build = -1
	case strSum <= 104:
		c.DB = 0
		c.Build = 0
	case strSum <= 124:
		c.DB = 2
		c.Build = 1
	case strSum <= 164:
		c.DB = 4
		c.Build = 2
	case strSum <= 204:
		c.DB = 6
		c.Build = 3
	default:
		c.DB = 8
		c.Build = 4
	}

	switch {
	case c.DEX <= 64:
		c.Move = 6
	case c.DEX <= 84:
		c.Move = 7
	case c.DEX <= 104:
		c.Move = 8
	default:
		c.Move = 9
	}
}

func roll3d6() int {
	return rngInt(6) + rngInt(6) + rngInt(6)
}

func roll2d6plus6() int {
	return rngInt(6) + rngInt(6) + 6
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

// FormatSheet 格式化角色卡为可读文本。
func FormatSheet(c *Character) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("╔══ %s ══╗\n", c.Name))
	b.WriteString(fmt.Sprintf("职业: %s\n", c.Occupation))
	if c.Age > 0 {
		b.WriteString(fmt.Sprintf("年龄: %d  性别: %s\n", c.Age, c.Gender))
	}
	b.WriteString("\n── 属性 ──\n")
	b.WriteString(fmt.Sprintf("STR:%3d  CON:%3d  POW:%3d  DEX:%3d\n", c.STR, c.CON, c.POW, c.DEX))
	b.WriteString(fmt.Sprintf("APP:%3d  SIZ:%3d  INT:%3d  EDU:%3d\n", c.APP, c.APP, c.INT, c.EDU))
	b.WriteString(fmt.Sprintf("LUCK:%3d\n", c.LUCK))

	b.WriteString("\n── 战斗 ──\n")
	b.WriteString(fmt.Sprintf("HP: %d/%d  MP: %d/%d  SAN: %d/%d\n", c.CurrentHP, c.HP, c.CurrentMP, c.MP, c.CurrentSAN, c.SAN))
	b.WriteString(fmt.Sprintf("DB: %+d  Build: %+d  Move: %d\n", c.DB, c.Build, c.Move))

	if c.SkillsJSON != "" {
		b.WriteString("\n── 技能 ──\n")
		skills := parseSkills(c.SkillsJSON)
		for name, val := range skills {
			b.WriteString(fmt.Sprintf("  %s: %d%%\n", name, val))
		}
	}
	return b.String()
}

func parseSkills(jsonStr string) map[string]int {
	var result map[string]int
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil
	}
	return result
}

func skillsToJSON(skills map[string]int) string {
	b, err := json.Marshal(skills)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// AttrAbbr 将中文属性名映射为 COC 7th 标准缩写。
func AttrAbbr(name string) string {
	switch strings.ToUpper(name) {
	case "力量", "STR":
		return "STR"
	case "体质", "CON":
		return "CON"
	case "意志", "POW":
		return "POW"
	case "敏捷", "DEX":
		return "DEX"
	case "外貌", "APP":
		return "APP"
	case "体型", "SIZ":
		return "SIZ"
	case "智力", "INT", "灵感":
		return "INT"
	case "教育", "EDU", "知识":
		return "EDU"
	case "幸运", "LUCK":
		return "LUCK"
	default:
		return name
	}
}
