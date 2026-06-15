// Package dnd 提供龙与地下城 5e 规则集插件。
//
// 依赖: dice（骰子引擎）、storage（SQLite 持久化）
//
// 角色管理:
//   - /dnd create <角色名> — 创建角色（默认属性 10）
//   - /dnd sheet [角色名] — 查看角色卡
//   - /dnd list — 列出所有角色
//   - /dnd set <属性> <值> [角色名] — 设置属性
//   - /dnd delete <角色名> — 删除角色
//
// 检定:
//   - /check <属性> [adv/dis] [角色名] — 属性检定（支持优势/劣势）
//   - /save <属性> [adv/dis] [角色名] — 豁免检定
//   - /skill <技能名> [adv/dis] [角色名] — 技能检定
//   - /init [角色名] — 先攻检定
//
// AI 工具:
//   - dnd_ability_check, dnd_skill_check, dnd_saving_throw, dnd_initiative, view_dnd_character
//
// AI 技能:
//   - dnd_dungeon_master — 地下城主助手
package dnd

import (
	"time"

	"gorm.io/gorm"
)

// Character 龙与地下城 5e 角色卡 GORM 模型。
type Character struct {
	ID        uint           `gorm:"primaryKey;autoIncrement"`
	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	UserID   string `gorm:"index:idx_dnd_user;not null"`
	UserName string `gorm:""`
	Name     string `gorm:"uniqueIndex:idx_dnd_user_name;not null;size:64"`

	// D&D 5e 六项属性（范围 1-30）
	STR int `gorm:"not null"` // 力量
	DEX int `gorm:"not null"` // 敏捷
	CON int `gorm:"not null"` // 体质
	INT int `gorm:"not null"` // 智力
	WIS int `gorm:"not null"` // 感知
	CHA int `gorm:"not null"` // 魅力

	Level            int `gorm:"not null"` // 等级（1-20）
	ProficiencyBonus int `gorm:"not null"` // 熟练加值（自动计算）

	// 战斗属性
	MaxHP      int `gorm:"not null"`
	CurrentHP  int `gorm:"not null"`
	TempHP     int `gorm:"not null"`
	AC         int `gorm:"not null"` // 护甲等级
	Speed      int `gorm:"not null"` // 移动速度（英尺/轮）
	Initiative int `gorm:"not null"` // 先攻加值

	// 角色背景
	Race       string `gorm:"size:32"`
	Class      string `gorm:"size:64"`
	Subclass   string `gorm:"size:64"`
	Background string `gorm:"size:64"`
	Alignment  string `gorm:"size:32"`

	// JSON 存储的列表型字段
	SkillProfsJSON  string `gorm:"column:skill_proficiencies;type:text"` // {"运动":true,"察觉":true}
	SavingProfsJSON string `gorm:"column:saving_throws;type:text"`       // {"STR":true,"DEX":true}
	FeaturesJSON    string `gorm:"column:features;type:text"`            // ["动作如潮","施法"]
	SpellsJSON      string `gorm:"column:spells;type:text"`              // 法术列表 JSON
	EquipmentJSON   string `gorm:"column:equipment;type:text"`           // 装备列表 JSON
	Notes           string `gorm:"type:text"`                            // 玩家笔记
}

func (Character) TableName() string { return "dnd_characters" }

func (Record) TableName() string { return "dnd_records" }

// Record 检定记录，用于追溯历史。
type Record struct {
	ID        uint      `gorm:"primaryKey;autoIncrement"`
	CreatedAt time.Time `gorm:"autoCreateTime"`

	UserID        string `gorm:"index:idx_dnd_record_user;not null"`
	CharacterName string `gorm:"index:idx_dnd_record_user;not null;size:64"`
	Type          string `gorm:"not null;size:32"` // 检定类型
	Detail        string `gorm:"type:text"`
	Result        string `gorm:"type:text"`
	DiceResult    string `gorm:"type:text"`
}
