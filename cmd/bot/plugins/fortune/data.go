// Package fortune 提供浅草寺御神签和塔罗牌占卜功能。
//
// 命令: /omikuji [番号], /tarot [数量]
// AI 工具: draw_omikuji, draw_tarot
package fortune

import (
	"fmt"
	"math/rand"
)

// FortuneLevel 运势等级（大吉～大凶）。
type FortuneLevel int

const (
	Daikichi   FortuneLevel = iota // 大吉
	Kichi                          // 吉
	Chukichi                       // 中吉
	Shokichi                       // 小吉
	Matsukichi                     // 末吉
	Kyo                            // 凶
	Daikyo                         // 大凶
)

func (f FortuneLevel) String() string {
	switch f {
	case Daikichi:
		return "大吉"
	case Kichi:
		return "吉"
	case Chukichi:
		return "中吉"
	case Shokichi:
		return "小吉"
	case Matsukichi:
		return "末吉"
	case Kyo:
		return "凶"
	case Daikyo:
		return "大凶"
	default:
		return "?"
	}
}

// LuckyAttrs 运势对应的幸运属性。
type LuckyAttrs struct {
	Direction string // 幸运方向
	Color     string // 幸运色
	Number    int    // 幸运数字
}

// levelAttrs 等级→幸运属性映射。
var levelAttrs = map[FortuneLevel]*LuckyAttrs{
	Daikichi:   {Direction: "东", Color: "金", Number: 7},
	Kichi:      {Direction: "东南", Color: "赤", Number: 5},
	Chukichi:   {Direction: "南", Color: "橙", Number: 3},
	Shokichi:   {Direction: "西", Color: "绿", Number: 2},
	Matsukichi: {Direction: "西北", Color: "青", Number: 1},
	Kyo:        {Direction: "北", Color: "灰", Number: 6},
	Daikyo:     {Direction: "东北", Color: "黑", Number: 4},
}

// OmikujiSlip 御神签数据，包含等级、签文、解签和幸运属性。
type OmikujiSlip struct {
	Number      int          // 签号 1-100
	Level       FortuneLevel // 运势等级
	Translation string       // 中文签文/解签
	Wish        string       // 愿望
	Waiting     string       // 待人
	LostItem    string       // 失物
	Travel      string       // 旅行
}

// LuckyAttrs 返回该签对应的幸运属性。
func (s *OmikujiSlip) LuckyAttrs() *LuckyAttrs {
	return levelAttrs[s.Level]
}

// levelPoems 按等级分类的签文池。
var levelPoems = [][]string{
	Daikichi: {
		"春风吹动 万物皆新。福寿双全 喜气洋洋。",
		"明月照庭 福至心灵。花开富贵 好运连连。",
		"青松翠竹 四季长青。龙腾云起 万事亨通。",
		"凤凰展翅 光辉万丈。金玉满堂 禄位高升。",
		"紫气东来 祥瑞万千。旭日升天 普照大地。",
		"锦上添花 喜上加喜。百鸟朝凤 万象更新。",
		"福如东海 寿比南山。天赐良缘 美满团圆。",
		"鹏程万里 前途无限。喜鹊登枝 好事将近。",
		"春华秋实 硕果累累。瑞雪兆丰 年谷丰登。",
		"龙飞凤舞 祥云缭绕。天降福星 吉庆有余。",
		"阳和启蛰 万物复苏。一帆风顺 万事如意。",
		"祥光普照 瑞气盈门。心想事成 美梦成真。",
		"春风得意 马到成功。花团锦簇 富贵荣华。",
		"吉星高照 鸿运当头。风生水起 好运连绵。",
		"天从人愿 诸事吉祥。福星高照 禄寿双全。",
		"日照龙鳞 万点金光。月映珠帘 一片祥和。",
		"云蒸霞蔚 气象万千。花开富贵 竹报平安。",
	},
	Kichi: {
		"和风细雨 润物无声。顺水行舟 事半功倍。",
		"良辰美景 赏心乐事。灯火阑珊 前程光明。",
		"柳暗花明 又见新村。春回大地 草木萌生。",
		"远山含笑 近水含情。云开见日 雾散天明。",
		"花好月圆 人寿年丰。风调雨顺 五谷丰登。",
		"知足常乐 平安是福。日进斗金 财源广进。",
		"步步高升 节节胜利。一番好意 终有回报。",
		"勤耕苦读 必有收成。善缘结善果 好心有好报。",
		"细水长流 安稳自在。胸有成竹 事在人为。",
		"光明正大 无愧于心。安居乐业 家和万兴。",
		"金榜题名 名扬天下。同心协力 共创佳绩。",
		"顺天应时 诸事大吉。心存善念 福泽绵长。",
		"谦虚受益 满招损谦。脚踏实地 步步为营。",
		"厚德载物 自强不息。仁者爱人 人恒爱之。",
		"天时地利 人和为贵。积善之家 必有余庆。",
		"春种秋收 天道酬勤。随遇而安 知命乐天。",
		"己所不欲 勿施于人。中庸之道 不偏不倚。",
		"学而时习 温故知新。有朋远来 不亦乐乎。",
		"人而无信 不知其可。言必信 行必果。",
		"见贤思齐 见不贤自省。三人行 必有我师。",
		"欲速则不达 见小利大事不成。",
		"工欲善其事 必先利其器。",
		"君子成人之美 不成人之恶。",
		"往事不可谏 来者犹可追。",
		"岁寒然后知松柏之后凋也。",
		"士不可以不弘毅 任重而道远。",
		"富与贵 是人之所欲也。",
		"不义而富且贵 于我如浮云。",
		"德不孤 必有邻。四海之内皆兄弟。",
		"躬自厚而薄责于人 则远怨矣。",
		"小不忍则乱大谋。三思而后行。",
		"君子坦荡荡 小人长戚戚。",
		"以直报怨 以德报德。",
		"居易以俟命 君子无入而不自得焉。",
	},
	Chukichi: {
		"半晴半雨 阴阳调和。顺其自然 莫强求之。",
		"守得云开 方见月明。忍耐一时 风平浪静。",
		"进退有度 得失随缘。中和之道 不偏不倚。",
		"塞翁失马 焉知非福。淡泊明志 宁静致远。",
		"不急不躁 稳步前行。量力而为 适可而止。",
		"花开花落 自有时节。水到渠成 不必强求。",
		"退一步海阔天空。谋事在人 成事在天。",
		"清风徐来 水波不兴。明月几时 把酒问天。",
		"行到水穷处 坐看云起时。",
		"山重水复疑无路 柳暗花明又一村。",
		"不忘初心 方得始终。静待时机 顺势而为。",
		"凡事预则立 不预则废。安之若素 处之泰然。",
		"得失随缘 心无增减。因上努力 果上随缘。",
		"心平气和 万事通达。静观其变 顺势而行。",
		"中庸为德 其至矣乎。过犹不及 允执厥中。",
		"无欲速 无见小利。欲速则不达 见小利大事不成。",
		"可与共学 未可与适道。可与适道 未可与立。",
		"学而不思则罔 思而不学则殆。",
		"知之为知之 不知为不知 是知也。",
		"敏而好学 不耻下问 是以谓之文也。",
		"默而识之 学而不厌 诲人不倦。",
		"发愤忘食 乐以忘忧 不知老之将至。",
		"其身正 不令而行。其身不正 虽令不从。",
		"临之以庄则敬 孝慈则忠 举善而教不能则劝。",
		"苟正其身 于从政乎何有。不能正其身 如正人何。",
		"近者说 远者来。",
		"君子和而不同 小人同而不和。",
		"君子泰而不骄 小人骄而不泰。",
	},
	Shokichi: {
		"微风轻拂 小有收获。点滴之功 积少成多。",
		"小善不为 何以成德。知足者富 小乐即安。",
		"鸟啼花落 皆为文章。一叶知秋 见微知著。",
		"不积跬步 无以至千里。不积小流 无以成江海。",
	},
	Matsukichi: {
		"先苦后甘 终将如意。暮色将至 黎明不远。",
		"冬天来了 春天不远。否极泰来 时来运转。",
		"晚成之器 大器晚成。坚持到底 必有转机。",
		"守得云开 终见月明。好事多磨 终成眷属。",
		"山穷水尽 柳暗花明。绝处逢生 枯木逢春。",
		"路遥知马力 日久见人心。",
		"锲而不舍 金石可镂。锲而舍之 朽木不折。",
	},
	Kyo: {
		"乌云蔽日 暂避锋芒。行路艰难 谨慎为上。",
		"祸从口出 病从口入。三思而行 免生后悔。",
		"急流勇退 明哲保身。暂守现状 不宜妄动。",
		"小人当道 远之则吉。自省吾身 改过迁善。",
		"作事不顺 所求多阻。静待时机 不宜妄动。",
		"人心叵测 防人之心不可无。害人之心不可有。",
		"虎落平阳 龙困浅滩。忍辱负重 以待天时。",
		"覆水难收 悔之晚矣。前车之鉴 后事之师。",
	},
	Daikyo: {
		"天倾西北 地陷东南。大难临头 各自飞散。",
		"天地不交 万物不通。上下不和 百事不成。",
	},
}

// levelWish 按等级的愿望解签文本池。
var levelWish = map[FortuneLevel][]string{
	Daikichi:   {"万事如意", "心想事成", "美梦成真", "福至心灵", "天从人愿"},
	Kichi:      {"顺其自然", "可望成功", "尽力而为", "终有回报", "努力有成"},
	Chukichi:   {"不急不躁", "耐心等待", "顺势而为", "平心静气"},
	Shokichi:   {"小有所成", "知足常乐"},
	Matsukichi: {"先难后易", "终将如愿"},
	Kyo:        {"谨慎行事", "暂缓为宜"},
	Daikyo:     {"不宜强求", "静待时机"},
}

// levelWaiting 按等级的待人解签文本池。
var levelWaiting = map[FortuneLevel][]string{
	Daikichi:   {"可至", "必来", "将至"},
	Kichi:      {"迟来", "终至"},
	Chukichi:   {"需待时日", "稍等"},
	Shokichi:   {"难来"},
	Matsukichi: {"无望", "不来"},
	Kyo:        {"不来"},
	Daikyo:     {"不来"},
}

// levelLostItem 按等级的失物解签文本池。
var levelLostItem = map[FortuneLevel][]string{
	Daikichi:   {"可寻回", "容易找到"},
	Kichi:      {"迟早寻得", "耐心寻找"},
	Chukichi:   {"较难寻得", "不易找到"},
	Shokichi:   {"难寻"},
	Matsukichi: {"寻不回"},
	Kyo:        {"寻不回"},
	Daikyo:     {"寻不回"},
}

// levelTravel 按等级的旅行解签文本池。
var levelTravel = map[FortuneLevel][]string{
	Daikichi:   {"大吉大利", "一路顺风"},
	Kichi:      {"平安顺利", "旅途愉快"},
	Chukichi:   {"谨慎而行", "注意安全"},
	Shokichi:   {"不宜远行"},
	Matsukichi: {"迟疑不定"},
	Kyo:        {"不宜出行"},
	Daikyo:     {"大凶勿行"},
}

// pick 从字符串数组中随机选一个。
func pick(arr []string) string {
	return arr[rand.Intn(len(arr))]
}

// numberLevel 签号 1-100 对应的等级映射表。
// 分布: 大吉×17 / 吉×34 / 中吉×28 / 小吉×4 / 末吉×7 / 凶×8 / 大凶×2
var numberLevel = [100]FortuneLevel{
	Daikyo,     // 1
	Kichi,      // 2
	Kichi,      // 3
	Daikichi,   // 4
	Chukichi,   // 5
	Kichi,      // 6
	Chukichi,   // 7
	Daikichi,   // 8
	Kichi,      // 9
	Chukichi,   // 10
	Chukichi,   // 11
	Daikichi,   // 12
	Kichi,      // 13
	Chukichi,   // 14
	Chukichi,   // 15
	Daikichi,   // 16
	Kichi,      // 17
	Chukichi,   // 18
	Chukichi,   // 19
	Daikichi,   // 20
	Kichi,      // 21
	Chukichi,   // 22
	Chukichi,   // 23
	Daikichi,   // 24
	Kichi,      // 25
	Chukichi,   // 26
	Shokichi,   // 27
	Daikichi,   // 28
	Kichi,      // 29
	Chukichi,   // 30
	Shokichi,   // 31
	Daikichi,   // 32
	Kichi,      // 33
	Chukichi,   // 34
	Shokichi,   // 35
	Daikichi,   // 36
	Kichi,      // 37
	Chukichi,   // 38
	Shokichi,   // 39
	Daikichi,   // 40
	Kichi,      // 41
	Chukichi,   // 42
	Matsukichi, // 43
	Daikichi,   // 44
	Kichi,      // 45
	Chukichi,   // 46
	Matsukichi, // 47
	Daikichi,   // 48
	Kichi,      // 49
	Chukichi,   // 50
	Matsukichi, // 51
	Daikichi,   // 52
	Kichi,      // 53
	Chukichi,   // 54
	Matsukichi, // 55
	Daikichi,   // 56
	Kichi,      // 57
	Chukichi,   // 58
	Matsukichi, // 59
	Daikichi,   // 60
	Kichi,      // 61
	Chukichi,   // 62
	Matsukichi, // 63
	Daikichi,   // 64
	Kichi,      // 65
	Chukichi,   // 66
	Matsukichi, // 67
	Kichi,      // 68
	Kichi,      // 69
	Kyo,        // 70
	Kyo,        // 71
	Daikichi,   // 72
	Kichi,      // 73
	Chukichi,   // 74
	Kyo,        // 75
	Kichi,      // 76
	Chukichi,   // 77
	Kyo,        // 78
	Kyo,        // 79
	Kichi,      // 80
	Kichi,      // 81
	Chukichi,   // 82
	Kyo,        // 83
	Kichi,      // 84
	Kichi,      // 85
	Chukichi,   // 86
	Kyo,        // 87
	Kichi,      // 88
	Kichi,      // 89
	Chukichi,   // 90
	Kyo,        // 91
	Kichi,      // 92
	Kichi,      // 93
	Chukichi,   // 94
	Daikyo,     // 95
	Kichi,      // 96
	Kichi,      // 97
	Kichi,      // 98
	Kichi,      // 99
	Kichi,      // 100
}

// omikujiSlips 按签号索引的已生成签文数组（由 init 填充）。
var omikujiSlips [100]*OmikujiSlip

func init() {
	var levelCount [7]int
	for i, level := range numberLevel {
		number := i + 1
		idx := levelCount[level] % len(levelPoems[level])
		levelCount[level]++
		omikujiSlips[i] = &OmikujiSlip{
			Number:      number,
			Level:       level,
			Translation: levelPoems[level][idx],
			Wish:        pick(levelWish[level]),
			Waiting:     pick(levelWaiting[level]),
			LostItem:    pick(levelLostItem[level]),
			Travel:      pick(levelTravel[level]),
		}
	}
	initTarot()
}

// drawOmikuji 抽取指定番号的御神签。number 为 0 时随机抽取。
func drawOmikuji(number int) *OmikujiSlip {
	if number >= 1 && number <= 100 {
		return omikujiSlips[number-1]
	}
	return omikujiSlips[rand.Intn(100)]
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

// TarotCard 塔罗牌定义，包含名称、正逆位释义和图片 URL。
type TarotCard struct {
	NameShort  string    // 缩写，如 "ar00"、"wa01"
	NameEN     string    // 英文名
	NameCN     string    // 中文名
	Suit       TarotSuit // 所属牌组
	MeaningUp  string    // 正位释义
	MeaningRev string    // 逆位释义
	ImageURL   string    // 卡牌图片 URL
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
		card.ImageURL = "https://www.sacred-texts.com/tarot/xr/" + c.NameShort + ".jpg"
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
				ImageURL:  "https://www.sacred-texts.com/tarot/xr/" + ns + ".jpg",
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
