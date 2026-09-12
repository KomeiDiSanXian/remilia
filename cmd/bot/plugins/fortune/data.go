// Package fortune 提供浅草寺御神签和塔罗牌占卜功能。
//
// 命令: /omikuji [番号], /tarot [数量]
// AI 工具: draw_omikuji, draw_tarot
package fortune

import (
	"fmt"
	"math/rand"
)

// drawOmikuji 抽取签号：number 在 1-100 之间时原样返回，否则随机抽取一张。
//
// 番号、吉凶、漢詩与解签都印在签纸扫描件上（见 assets/omikuji，
// 每番两页：签文页与解签页），因此这里只负责选号，不保存任何签文数据。
func drawOmikuji(number int) int {
	if number >= 1 && number <= 100 {
		return number
	}
	return rand.Intn(100) + 1
}

func init() {
	initTarot()
}

// TarotSuit 塔罗牌的牌组类型。
type TarotSuit int

const (
	SuitMajor     TarotSuit = iota // 大阿尔卡纳
	SuitWands                      // 权杖
	SuitCups                       // 圣杯
	SuitSwords                     // 宝剑
	SuitPentacles                  // 钱币
)

func (s TarotSuit) String() string {
	switch s {
	case SuitMajor:
		return "大阿尔卡纳"
	case SuitWands:
		return "权杖"
	case SuitCups:
		return "圣杯"
	case SuitSwords:
		return "宝剑"
	case SuitPentacles:
		return "钱币"
	}
	return "?"
}

// TarotCard 塔罗牌定义，包含名称与正逆位释义。
// 牌面取自内置的公有领域 RWS 牌图，文件名即 NameShort。
type TarotCard struct {
	NameShort  string    // 缩写，如 "ar00"、"wa01"，同时作为内置牌面文件名
	NameEN     string    // 英文名
	NameCN     string    // 中文名
	Suit       TarotSuit // 所属牌组
	MeaningUp  string    // 正位释义
	MeaningRev string    // 逆位释义
}

// tarotDeck 78 张塔罗牌的 map，key 为 NameShort。
var tarotDeck = map[string]*TarotCard{}

type minorMeaning struct {
	up, rev string
}

// minorMeanings 小阿尔卡纳各牌正逆位中文释义。
var minorMeanings = map[string][]minorMeaning{
	"wands": {
		{"创造力、新的开始", "缺乏方向、延迟"},
		{"计划、决策", "恐惧选择、犹豫"},
		{"扩张、进步", "阻碍、延误"},
		{"庆祝、和谐", "不稳定、冲突"},
		{"竞争、冲突", "妥协、和解"},
		{"胜利、自信", "傲慢、失败"},
		{"挑战、防守", "疲惫、放弃"},
		{"速度、行动", "混乱、延迟"},
		{"坚持、韧性", "固执、疲惫"},
		{"负担、压力", "卸下负担"},
		{"热情、消息", "缺乏方向"},
		{"行动、冒险", "急躁、冲动"},
		{"勇气、决心", "嫉妒、竞争"},
		{"领导力、远见", "专制、强势"},
	},
	"cups": {
		{"爱、情感、直觉", "空虚、情感阻塞"},
		{"和谐、连结", "分裂、误解"},
		{"欢庆、友谊", "过度、享乐"},
		{"冥想、冷漠", "觉醒、新视角"},
		{"失落、悲伤", "接受、释怀"},
		{"回忆、怀旧", "活在当下"},
		{"幻象、选择", "清晰、决断"},
		{"放下、前进", "迷失、停滞"},
		{"满足、幸福", "不满足、空虚"},
		{"美满、幸福家庭", "不和谐、争吵"},
		{"直觉、消息", "不成熟、情感"},
		{"浪漫、魅力", "过度理想化"},
		{"情感成熟、直觉", "情感依赖"},
		{"情感稳定、慈悲", "情感封闭"},
	},
	"swords": {
		{"清晰、真理、胜利", "混乱、误解"},
		{"僵局、选择", "犹豫、信息过载"},
		{"心痛、悲伤", "康复、释放"},
		{"休息、冥想", "恢复、行动"},
		{"冲突、损失", "和解、修复"},
		{"过渡、前行", "阻力、未解决"},
		{"策略、欺骗", "诚实、觉醒"},
		{"限制、困惑", "解放、清晰"},
		{"焦虑、噩梦", "释放、希望"},
		{"结束、痛苦", "复苏、重生"},
		{"警觉、沟通", "冲动、八卦"},
		{"勇气、行动", "鲁莽、急躁"},
		{"独立、经验", "冷酷、苦涩"},
		{"权威、理智", "滥权、压迫"},
	},
	"pentacles": {
		{"繁荣、新的开始", "浪费、错失机会"},
		{"平衡、适应", "混乱、过度"},
		{"合作、学习", "缺乏团队精神"},
		{"节约、守护", "贪婪、吝啬"},
		{"贫困、忧虑", "改善、康复"},
		{"分享、慷慨", "不平衡、负债"},
		{"评估、收获", "拖延、浪费"},
		{"技能、勤奋", "完美主义"},
		{"自律、独立", "孤独、过劳"},
		{"财富、传承", "损失、破产"},
		{"消息、务实", "缺乏计划"},
		{"责任、勤奋", "懒惰、拖延"},
		{"繁荣、滋养", "疏忽"},
		{"成功、领导", "贪婪、物质主义"},
	},
}

// suitKey 将 TarotSuit 转换为小写英文键名。
func suitKey(suit TarotSuit) string {
	switch suit {
	case SuitWands:
		return "wands"
	case SuitCups:
		return "cups"
	case SuitSwords:
		return "swords"
	case SuitPentacles:
		return "pentacles"
	}
	return ""
}

// initTarot 初始化 78 张塔罗牌数据。
func initTarot() {
	majorArcana := []TarotCard{
		{NameShort: "ar00", NameEN: "The Fool", NameCN: "愚者", Suit: SuitMajor, MeaningUp: "新的开始、冒险、天真", MeaningRev: "鲁莽、冒险、不成熟"},
		{NameShort: "ar01", NameEN: "The Magician", NameCN: "魔术师", Suit: SuitMajor, MeaningUp: "创造力、技能、自信", MeaningRev: "欺骗、浪费天赋"},
		{NameShort: "ar02", NameEN: "The High Priestess", NameCN: "女祭司", Suit: SuitMajor, MeaningUp: "直觉、神秘、内在智慧", MeaningRev: "秘密、表面现象"},
		{NameShort: "ar03", NameEN: "The Empress", NameCN: "女皇", Suit: SuitMajor, MeaningUp: "丰收、母性、自然", MeaningRev: "依赖、空虚"},
		{NameShort: "ar04", NameEN: "The Emperor", NameCN: "皇帝", Suit: SuitMajor, MeaningUp: "权威、稳定、结构", MeaningRev: "专制、僵化"},
		{NameShort: "ar05", NameEN: "The Hierophant", NameCN: "教宗", Suit: SuitMajor, MeaningUp: "传统、信仰、教导", MeaningRev: "挑战权威"},
		{NameShort: "ar06", NameEN: "The Lovers", NameCN: "恋人", Suit: SuitMajor, MeaningUp: "爱情、和谐、选择", MeaningRev: "分歧、价值冲突"},
		{NameShort: "ar07", NameEN: "The Chariot", NameCN: "战车", Suit: SuitMajor, MeaningUp: "胜利、意志力、决心", MeaningRev: "失控、方向错误"},
		{NameShort: "ar08", NameEN: "Strength", NameCN: "力量", Suit: SuitMajor, MeaningUp: "勇气、内在力量", MeaningRev: "自我怀疑、脆弱"},
		{NameShort: "ar09", NameEN: "The Hermit", NameCN: "隐士", Suit: SuitMajor, MeaningUp: "内省、智慧、孤独", MeaningRev: "孤立、孤独"},
		{NameShort: "ar10", NameEN: "Wheel of Fortune", NameCN: "命运之轮", Suit: SuitMajor, MeaningUp: "转变、循环、命运", MeaningRev: "坏运气、抗拒改变"},
		{NameShort: "ar11", NameEN: "Justice", NameCN: "正义", Suit: SuitMajor, MeaningUp: "公平、真相、因果", MeaningRev: "不公、逃避责任"},
		{NameShort: "ar12", NameEN: "The Hanged Man", NameCN: "倒吊人", Suit: SuitMajor, MeaningUp: "暂停、牺牲、新视角", MeaningRev: "拖延、抗拒"},
		{NameShort: "ar13", NameEN: "Death", NameCN: "死神", Suit: SuitMajor, MeaningUp: "结束、转变、重生", MeaningRev: "抗拒改变、停滞"},
		{NameShort: "ar14", NameEN: "Temperance", NameCN: "节制", Suit: SuitMajor, MeaningUp: "平衡、中庸、和谐", MeaningRev: "失衡、冲突"},
		{NameShort: "ar15", NameEN: "The Devil", NameCN: "恶魔", Suit: SuitMajor, MeaningUp: "束缚、物质主义、欲望", MeaningRev: "解放、觉醒"},
		{NameShort: "ar16", NameEN: "The Tower", NameCN: "高塔", Suit: SuitMajor, MeaningUp: "剧变、毁灭、启示", MeaningRev: "避免灾难"},
		{NameShort: "ar17", NameEN: "The Star", NameCN: "星星", Suit: SuitMajor, MeaningUp: "希望、灵感、宁静", MeaningRev: "绝望、失去方向"},
		{NameShort: "ar18", NameEN: "The Moon", NameCN: "月亮", Suit: SuitMajor, MeaningUp: "幻觉、直觉、潜意识", MeaningRev: "解除困惑"},
		{NameShort: "ar19", NameEN: "The Sun", NameCN: "太阳", Suit: SuitMajor, MeaningUp: "成功、喜悦、活力", MeaningRev: "暂时的挫折"},
		{NameShort: "ar20", NameEN: "Judgement", NameCN: "审判", Suit: SuitMajor, MeaningUp: "重生、觉醒、召唤", MeaningRev: "自我怀疑"},
		{NameShort: "ar21", NameEN: "The World", NameCN: "世界", Suit: SuitMajor, MeaningUp: "完成、成就、旅行", MeaningRev: "未完成、停滞"},
	}

	rankNames := []struct {
		en string
		cn string
	}{
		{"Ace", "一"}, {"Two", "二"}, {"Three", "三"}, {"Four", "四"}, {"Five", "五"},
		{"Six", "六"}, {"Seven", "七"}, {"Eight", "八"}, {"Nine", "九"}, {"Ten", "十"},
		{"Page", "侍从"}, {"Knight", "骑士"}, {"Queen", "皇后"}, {"King", "国王"},
	}

	suitData := []struct {
		suit  TarotSuit
		short string
		cn    string
	}{
		{SuitWands, "wa", "权杖"},
		{SuitCups, "cu", "圣杯"},
		{SuitSwords, "sw", "宝剑"},
		{SuitPentacles, "pe", "钱币"},
	}

	for _, c := range majorArcana {
		card := c
		tarotDeck[card.NameShort] = &card
	}

	for _, s := range suitData {
		meanings := minorMeanings[suitKey(s.suit)]
		for i := range 14 {
			ns := s.short + fmt.Sprintf("%02d", i+1)
			card := &TarotCard{
				NameShort: ns,
				NameEN:    rankNames[i].en + " of " + s.cn,
				NameCN:    s.cn + rankNames[i].cn,
				Suit:      s.suit,
			}
			if i < len(meanings) {
				card.MeaningUp = meanings[i].up
				card.MeaningRev = meanings[i].rev
			}
			tarotDeck[ns] = card
		}
	}
}

// drawTarot 随机抽取 count 张塔罗牌，每张随机正位或逆位。
// count 超过牌库总数时返回全部牌。
func drawTarot(count int) []TarotReading {
	all := make([]*TarotCard, 0, len(tarotDeck))
	for _, c := range tarotDeck {
		all = append(all, c)
	}
	perm := rand.Perm(len(all))
	if count > len(all) {
		count = len(all)
	}
	readings := make([]TarotReading, count)
	for i := 0; i < count; i++ {
		readings[i] = TarotReading{
			Card:      *all[perm[i]],
			IsReverse: rand.Intn(2) == 0,
		}
	}
	return readings
}

// TarotReading 一次塔罗占卜的结果（一张牌及其正逆位）。
type TarotReading struct {
	Card      TarotCard // 牌
	IsReverse bool      // true=逆位, false=正位
}

// Meaning 返回正位或逆位的中文释义。
func (r *TarotReading) Meaning() string {
	if r.IsReverse {
		return r.Card.MeaningRev
	}
	return r.Card.MeaningUp
}

// Orientation 返回 "正位" 或 "逆位"。
func (r *TarotReading) Orientation() string {
	if r.IsReverse {
		return "逆位"
	}
	return "正位"
}
