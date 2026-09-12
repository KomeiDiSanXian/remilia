// Package fortune 提供浅草寺御神签和塔罗牌占卜功能。
//
// 命令: /omikuji [番号], /tarot [数量]
// AI 工具: draw_omikuji, draw_tarot
//
// 本文件保存塔罗牌数据；御神签的类型与形态见 omikuji.go，
// 签文数据见 omikuji_zh.go。
package fortune

import (
	"fmt"
	"math/rand"
)

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
	Rank       int       // 牌位：大阿尔卡纳为 0，小阿尔卡纳为 1-14（一至十、侍从、骑士、皇后、国王）
	MeaningUp  string    // 正位关键词
	MeaningRev string    // 逆位关键词
	ReadingUp  string    // 正位解读，仅大阿尔卡纳逐张撰写
	ReadingRev string    // 逆位解读，仅大阿尔卡纳逐张撰写
}

// Reading 返回该牌在指定朝向下的完整解读。
//
// 大阿尔卡纳使用逐张撰写的正文；小阿尔卡纳由牌组主管的领域与牌位所处
// 阶段组合而成，因此 ReadingUp / ReadingRev 对它们留空。关键词由
// MeaningUp / MeaningRev 单独提供，不在解读正文里重复。
func (c *TarotCard) Reading(reverse bool) string {
	if c.Suit == SuitMajor {
		if reverse {
			return c.ReadingRev
		}
		return c.ReadingUp
	}

	// 牌位数据异常时退回关键词，避免下标越界。
	if c.Rank < 1 || c.Rank > len(rankStages) {
		return c.keyword(reverse)
	}

	stage := rankStages[c.Rank-1]
	orientation := "正位"
	stageText := stage.up
	if reverse {
		orientation = "逆位"
		stageText = stage.rev
	}
	// 不在这里重复关键词：调用方会另行展示牌意关键词。
	return fmt.Sprintf("%s主管%s；此牌%s，处于「%s」的阶段。",
		c.Suit, suitDomain[c.Suit], orientation, stageText)
}

// keyword 返回正位或逆位的关键词。
func (c *TarotCard) keyword(reverse bool) string {
	if reverse {
		return c.MeaningRev
	}
	return c.MeaningUp
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

// suitDomain 各牌组主管的领域，用于组合小阿尔卡纳的解读。
var suitDomain = map[TarotSuit]string{
	SuitWands:     "行动、热情与创造力",
	SuitCups:      "情感、关系与直觉",
	SuitSwords:    "思维、沟通与冲突",
	SuitPentacles: "物质、工作与现实基础",
}

// rankStage 小阿尔卡纳某个牌位所处的阶段，含正逆位两种表述。
type rankStage struct {
	up  string
	rev string
}

// rankStages 按牌位排列，依次是一至十与侍从、骑士、皇后、国王，共 14 项。
var rankStages = []rankStage{
	{"事情刚刚萌芽，纯粹而有力的开端", "起点受阻，或想法尚未落地"},
	{"在两者之间权衡、合作与选择", "犹豫不决，或信息不足难以判断"},
	{"初步成果出现，开始向外扩展", "进展延宕，计划需要调整"},
	{"稳定下来，守住已有的一席之地", "过于保守，或因不安而抓紧不放"},
	{"出现摩擦与失落，需要正面应对", "冲突开始缓和，损失可以修复"},
	{"局面重新流动，关系渐入平衡", "旧问题尚未了结，或付出与回报不对等"},
	{"需要策略与耐心，重新评估眼前的处境", "方向不清或一味拖延，让机会溜走"},
	{"投入与熟练带来的稳定推进", "重复劳动或追求完美，反而卡住"},
	{"成果大致到手，只差最后一步", "看似满足，内心仍有不安"},
	{"一个周期走到终点，收获与总结", "该收尾的没收尾，负担还没放下"},
	{"以学习者的姿态接触新事物，消息与机会来临", "心浮气躁，缺乏经验容易出错"},
	{"带着明确目标行动，进展迅速", "急躁冲动，行动缺乏考量"},
	{"以成熟的感受力照顾局面，内敛而有力", "情绪或控制欲盖过了原本的温柔"},
	{"以经验和掌控力主事，稳固而有担当", "过于强势或固守权威，反而失人心"},
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
		{
			NameShort: "ar00", NameEN: "The Fool", NameCN: "愚者", Suit: SuitMajor,
			MeaningUp: "新的开始、冒险、天真", MeaningRev: "鲁莽、不成熟",
			ReadingUp:  "全新的起点，抱着好奇轻装上阵。此刻不必等到万全，先迈出第一步，路会在脚下展开。",
			ReadingRev: "冲动与准备不足容易让好事走偏。先确认脚下是实地，再谈出发。",
		},
		{
			NameShort: "ar01", NameEN: "The Magician", NameCN: "魔术师", Suit: SuitMajor,
			MeaningUp: "创造力、技能、自信", MeaningRev: "欺骗、浪费天赋",
			ReadingUp:  "你手上已握有所需的资源与才能，关键是把想法转成行动。主动出击，事情会向你靠拢。",
			ReadingRev: "才华用错了地方，或话术多过行动。别被人牵着走，也别自我夸大。",
		},
		{
			NameShort: "ar02", NameEN: "The High Priestess", NameCN: "女祭司", Suit: SuitMajor,
			MeaningUp: "直觉、神秘、内在智慧", MeaningRev: "秘密、表面现象",
			ReadingUp:  "答案不在外面，而在你早已察觉却还没承认的直觉里。适合静观、学习与等待。",
			ReadingRev: "过度揣测或刻意回避，会让局面更模糊。有些话需要摊开来讲。",
		},
		{
			NameShort: "ar03", NameEN: "The Empress", NameCN: "女皇", Suit: SuitMajor,
			MeaningUp: "丰收、母性、自然", MeaningRev: "依赖、空虚",
			ReadingUp:  "丰盛与滋养的时期，适合经营关系、身体与创作。付出的温柔会被好好回应。",
			ReadingRev: "一味付出而忽略自己，委屈会慢慢积累。先照顾好自己，再谈照顾别人。",
		},
		{
			NameShort: "ar04", NameEN: "The Emperor", NameCN: "皇帝", Suit: SuitMajor,
			MeaningUp: "权威、稳定、结构", MeaningRev: "专制、僵化",
			ReadingUp:  "建立秩序与边界的时候。清晰的规定和稳定的节奏，比一时热情更管用。",
			ReadingRev: "过于强硬或死守规矩，会把身边的人推远。该松的地方要松。",
		},
		{
			NameShort: "ar05", NameEN: "The Hierophant", NameCN: "教宗", Suit: SuitMajor,
			MeaningUp: "传统、信仰、教导", MeaningRev: "挑战权威",
			ReadingUp:  "循既有的路径与方法，向有经验的人请教，会让你省下很多力气。",
			ReadingRev: "旧规矩未必适合你。可以质疑，但别为了反对而反对。",
		},
		{
			NameShort: "ar06", NameEN: "The Lovers", NameCN: "恋人", Suit: SuitMajor,
			MeaningUp: "爱情、和谐、选择", MeaningRev: "分歧、价值冲突",
			ReadingUp:  "面对关系与价值的抉择，请顺着心里真正的渴望。真诚的沟通会把彼此拉近。",
			ReadingRev: "价值不合或回避选择，正在消耗这段关系。别用沉默替你作决定。",
		},
		{
			NameShort: "ar07", NameEN: "The Chariot", NameCN: "战车", Suit: SuitMajor,
			MeaningUp: "胜利、意志力、决心", MeaningRev: "失控、方向错误",
			ReadingUp:  "目标明确、意志坚定的推进期。只要方向没错，努力会换成实打实的成果。",
			ReadingRev: "用力过猛或方向偏了，越使劲离得越远。停下来校准，比硬冲更重要。",
		},
		{
			NameShort: "ar08", NameEN: "Strength", NameCN: "力量", Suit: SuitMajor,
			MeaningUp: "勇气、内在力量", MeaningRev: "自我怀疑、脆弱",
			ReadingUp:  "真正的力量是温柔而坚定。用耐心与包容化解对抗，比硬碰硬有效得多。",
			ReadingRev: "自我怀疑或情绪起伏，正在削弱你的判断。先把自己安顿好。",
		},
		{
			NameShort: "ar09", NameEN: "The Hermit", NameCN: "隐士", Suit: SuitMajor,
			MeaningUp: "内省、智慧、孤独", MeaningRev: "孤立、封闭",
			ReadingUp:  "适合独处、复盘与沉淀。慢下来，你会看清之前一直忽略的东西。",
			ReadingRev: "独处变成了逃避。该回到人群里，把想法说出来。",
		},
		{
			NameShort: "ar10", NameEN: "Wheel of Fortune", NameCN: "命运之轮", Suit: SuitMajor,
			MeaningUp: "转变、循环、命运", MeaningRev: "时运不济、抗拒改变",
			ReadingUp:  "转折点已至，运势开始转动。顺势而为，别执着于维持现状。",
			ReadingRev: "变化由不得你掌控，或时机还没到。接受节奏，别硬拗。",
		},
		{
			NameShort: "ar11", NameEN: "Justice", NameCN: "正义", Suit: SuitMajor,
			MeaningUp: "公平、真相、因果", MeaningRev: "不公、逃避责任",
			ReadingUp:  "因果分明，公平会得到回应。作决定时把事实与责任摆在前面。",
			ReadingRev: "失衡、偏颇或回避责任，会让事情悬而不决。先诚实面对。",
		},
		{
			NameShort: "ar12", NameEN: "The Hanged Man", NameCN: "倒吊人", Suit: SuitMajor,
			MeaningUp: "暂停、牺牲、新视角", MeaningRev: "拖延、无谓牺牲",
			ReadingUp:  "以退为进，换个角度看，僵局自会松开。此刻的等待是有意义的。",
			ReadingRev: "无谓的拖延与自我消耗。若只是耗着，就该换个做法。",
		},
		{
			NameShort: "ar13", NameEN: "Death", NameCN: "死神", Suit: SuitMajor,
			MeaningUp: "结束、转变、重生", MeaningRev: "抗拒改变、停滞",
			ReadingUp:  "一个阶段确实结束了，就让它结束。清空之后，新的东西才进得来。",
			ReadingRev: "明知该放却迟迟不放，痛苦被拖长了。告别也是一种成全。",
		},
		{
			NameShort: "ar14", NameEN: "Temperance", NameCN: "节制", Suit: SuitMajor,
			MeaningUp: "平衡、中庸、和谐", MeaningRev: "失衡、极端",
			ReadingUp:  "调和与耐心是此刻的关键。把偏离的两端往中间挪，事情自然会顺。",
			ReadingRev: "节奏失衡，用力不均。检查一下哪里过了头、哪里亏欠。",
		},
		{
			NameShort: "ar15", NameEN: "The Devil", NameCN: "恶魔", Suit: SuitMajor,
			MeaningUp: "束缚、物质主义、欲望", MeaningRev: "解放、觉醒",
			ReadingUp:  "某段关系或习惯正束缚着你，欲望被放大成了依赖。先看清枷锁长什么样。",
			ReadingRev: "开始挣脱了。断掉不健康的依赖，会有明显的轻松感。",
		},
		{
			NameShort: "ar16", NameEN: "The Tower", NameCN: "高塔", Suit: SuitMajor,
			MeaningUp: "剧变、崩塌、启示", MeaningRev: "避开灾难、勉强支撑",
			ReadingUp:  "突变与崩塌，往往是虚假的根基被拆掉。会痛，但这是必要的清理。",
			ReadingRev: "危机被避开了，或者你还在硬撑。别把问题继续往后拖。",
		},
		{
			NameShort: "ar17", NameEN: "The Star", NameCN: "星星", Suit: SuitMajor,
			MeaningUp: "希望、灵感、宁静", MeaningRev: "失望、失去方向",
			ReadingUp:  "风雨之后的宁静与希望，适合疗愈与许愿。相信那种缓慢而确定的变好。",
			ReadingRev: "信心暂时缺席，容易自我否定。允许自己慢一点恢复。",
		},
		{
			NameShort: "ar18", NameEN: "The Moon", NameCN: "月亮", Suit: SuitMajor,
			MeaningUp: "幻觉、直觉、潜意识", MeaningRev: "解除困惑、真相浮现",
			ReadingUp:  "看不清全貌，情绪与想象容易放大恐惧。别在雾里作重大决定。",
			ReadingRev: "迷雾正在散开，误会逐渐澄清。真相会让你安心。",
		},
		{
			NameShort: "ar19", NameEN: "The Sun", NameCN: "太阳", Suit: SuitMajor,
			MeaningUp: "成功、喜悦、活力", MeaningRev: "短暂的挫折",
			ReadingUp:  "明朗、顺畅、被看见的时期。适合展示成果，也适合好好享受当下。",
			ReadingRev: "好事打了点折扣，或兴致被杂事分走。别让小事败了心情。",
		},
		{
			NameShort: "ar20", NameEN: "Judgement", NameCN: "审判", Suit: SuitMajor,
			MeaningUp: "重生、觉醒、召唤", MeaningRev: "自我怀疑、迟疑",
			ReadingUp:  "复盘与觉醒的时刻，过去的经历正在给出答案。回应内心真正的召唤。",
			ReadingRev: "自我怀疑，或迟迟不肯下判断。该做的决定，越早越轻松。",
		},
		{
			NameShort: "ar21", NameEN: "The World", NameCN: "世界", Suit: SuitMajor,
			MeaningUp: "完成、成就、圆满", MeaningRev: "未完成、停滞",
			ReadingUp:  "一个周期圆满收束，值得庆祝。带着这份完整感，走向下一段。",
			ReadingRev: "差最后一步没能收尾。补上那个缺口，才算真正完成。",
		},
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
		en    string
	}{
		{SuitWands, "wa", "权杖", "Wands"},
		{SuitCups, "cu", "圣杯", "Cups"},
		{SuitSwords, "sw", "宝剑", "Swords"},
		{SuitPentacles, "pe", "钱币", "Pentacles"},
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
				NameEN:    rankNames[i].en + " of " + s.en,
				NameCN:    s.cn + rankNames[i].cn,
				Suit:      s.suit,
				Rank:      i + 1,
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

// Reading 返回正位或逆位的完整解读。
func (r *TarotReading) Reading() string {
	return r.Card.Reading(r.IsReverse)
}

// Orientation 返回 "正位" 或 "逆位"。
func (r *TarotReading) Orientation() string {
	if r.IsReverse {
		return "逆位"
	}
	return "正位"
}
