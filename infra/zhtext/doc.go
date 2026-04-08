// Package zhtext 提供面向中文 Bot 场景的轻量级文本处理工具集。
//
// 本包不依赖 CGO，所有功能均为纯 Go 实现，适合在任意平台编译。
//
// # 子功能模块
//
// CJK 字符工具（cjk.go）：
//   - [IsHan]           — 判断单个 rune 是否为 CJK 汉字
//   - [ContainsChinese] — 字符串是否包含汉字
//   - [ChineseCharCount]— 统计字符串中的汉字数量
//   - [SplitCJK]        — 按 CJK/非CJK 边界分词（简易 tokenizer）
//   - [IsFullWidth]     — 判断是否为全角字符
//
// 文本规范化（normalize.go）：
//   - [FullToHalf]    — 全角 ASCII 转半角
//   - [HalfToFull]    — 半角 ASCII 转全角
//   - [NormalizeCJK]  — 全角转半角 + 去除首尾空白（一步规范化）
//   - [CollapseSpaces]— 连续空白折叠为单个空格
//
// 模糊搜索（fuzzy.go）：
//   - [Match]          — 判断 source 是否模糊匹配 target（区分大小写）
//   - [MatchFold]      — 同上，不区分大小写
//   - [Find]           — 在候选列表中查找所有模糊匹配项
//   - [FindFold]       — 同上，不区分大小写
//   - [RankFind]       — 模糊查找并按相关度排序
//   - [RankFindFold]   — 同上，不区分大小写
//   - [Rank]           / [RankSource] — 计算匹配得分（越小越相关）
//
// 简繁转换（t2s.go）：
//   - [TraditionalToSimplified] — 常用繁体字转简体（约 500 字对照表，非穷举）
//   - [SimplifiedToTraditional] — 常用简体字转繁体（同表反查）
//
// # 使用示例
//
//	// 模糊搜索
//	results := zhtext.FindFold([]string{"天气预报", "天氣預報", "温度查询"}, "天气")
//	// results = ["天气预报", "天氣預報"]
//
//	// 规范化用户输入（全角→半角，去空白）
//	input := zhtext.NormalizeCJK("／ping　　hello")
//	// input = "/ping hello"
//
//	// 繁简转换
//	s := zhtext.TraditionalToSimplified("臺灣天氣預報")
//	// s = "台湾天气预报"
package zhtext
