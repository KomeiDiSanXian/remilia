// Package coc 提供克苏鲁的呼唤 7th 规则集插件。
//
// 依赖: dice（骰子引擎）、storage（SQLite 持久化）
//
// 角色管理:
//   - /coc create <角色名> — 自动生成 COC 7th 属性
//   - /coc sheet [角色名] — 查看角色卡
//   - /coc list — 列出所有角色
//   - /coc skill <技能名> <值> [角色名] — 设置技能
//   - /coc delete <角色名> — 删除角色
//
// 检定:
//   - /cc <技能名> [角色名] — 技能检定（D100）
//   - /sc [成功损失] [失败损失] — SAN 理智检定
//   - /coc luck [角色名] — 幸运检定
//   - /coc push <技能名> [角色名] — 推骰
//
// AI 工具:
//   - coc_skill_check, coc_sanity_check, coc_luck_check, view_coc_character
//
// AI 技能:
//   - coc_referee — COC 守秘人助手
package coc

import (
	"time"

	"gorm.io/gorm"
)

// Character 克苏鲁的呼唤 7th 角色卡 GORM 模型。
type Character struct {
	ID        uint           `gorm:"primaryKey;autoIncrement"`
	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	UserID   string `gorm:"index:idx_coc_user;not null"`                    // 平台用户 ID
	UserName string `gorm:""`                                               // 用户显示名
	Name     string `gorm:"uniqueIndex:idx_coc_user_name;not null;size:64"` // 角色名（同用户下唯一）

	// COC 7th 八项基础属性（3D6 或 2D6+6 生成）
	STR int `gorm:"not null"` // 力量 (3D6)
	CON int `gorm:"not null"` // 体质 (3D6)
	POW int `gorm:"not null"` // 意志 (3D6)
	DEX int `gorm:"not null"` // 敏捷 (3D6)
	APP int `gorm:"not null"` // 外貌 (3D6)
	SIZ int `gorm:"not null"` // 体型 (2D6+6)
	INT int `gorm:"not null"` // 智力 (2D6+6)
	EDU int `gorm:"not null"` // 教育 (2D6+6)

	// 派生战斗属性
	HP    int `gorm:"not null"` // 生命值上限 (CON+SIZ)/10 向上取整
	MP    int `gorm:"not null"` // 魔法值 = POW
	SAN   int `gorm:"not null"` // 理智值 = POW×5
	LUCK  int `gorm:"not null"` // 幸运 (3D6)
	DB    int `gorm:"not null"` // 伤害加值（基于 STR+SIZ）
	Build int `gorm:"not null"` // 体型等级
	Move  int `gorm:"not null"` // 移动力（基于 DEX）

	// 当前值（可能因战斗/理智损失而变化）
	CurrentHP  int `gorm:"not null"`
	CurrentMP  int `gorm:"not null"`
	CurrentSAN int `gorm:"not null"`

	// 角色背景信息
	Occupation  string `gorm:"size:64"`
	Age         int    `gorm:""`
	Gender      string `gorm:"size:16"`
	Background  string `gorm:"type:text"` // 背景故事
	Story       string `gorm:"type:text"` // 经历
	Personality string `gorm:"type:text"` // 性格描述
	Idea        string `gorm:"type:text"` // 信念与理想

	SkillsJSON string `gorm:"column:skills;type:text"` // 技能列表 JSON: {"侦查":60,"图书馆":50}

	// 装备与资产
	Weapon      string `gorm:"type:text"`
	Possessions string `gorm:"type:text"`
	Cash        string `gorm:"size:128"`
	Assets      string `gorm:"size:128"`
}

func (Character) TableName() string { return "coc_characters" }

func (Record) TableName() string { return "coc_records" }

// Record 检定记录，用于追溯历史。
type Record struct {
	ID        uint      `gorm:"primaryKey;autoIncrement"`
	CreatedAt time.Time `gorm:"autoCreateTime"`

	UserID        string `gorm:"index:idx_coc_record_user;not null"`
	CharacterName string `gorm:"index:idx_coc_record_user;not null;size:64"`
	Type          string `gorm:"not null;size:32"` // 检定类型：技能检定/SAN检定/幸运检定/推骰
	Detail        string `gorm:"type:text"`        // 检定详情
	Result        string `gorm:"type:text"`        // 结果描述
	DiceResult    string `gorm:"type:text"`        // 骰子结果
}
