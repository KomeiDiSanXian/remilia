// Package pic 提供按标签发送随机图片的功能。
//
// 聚合多个 booru 图库（Safebooru / Gelbooru / rule34 / Konachan / Yande.re），
// 按全局 rating 策略自动过滤站点与内容，命令 /pic 发送图片并附带作品信息。
package pic

import (
	"math/rand"
	"slices"
	"strings"
)

// ── 内容分级 ─────────────────────────────────────────────────────────────

// Rating 内容分级档位（由轻到重排序）。
//
// 站点实际使用的分级体系可能不同，经 site.RatingTags 映射为站点搜索标签：
//   - gelbooru.com 已迁移至 Danbooru 式分级（general/sensitive/questionable/explicit），
//     其中 general 与内部 safe 同档，经映射处理
//   - safebooru / rule34 / yande.re 保留旧体系（safe/questionable/explicit）
//   - moebooru（konachan / yande.re）请求标签为单字母形式（rating:s / rating:q / rating:e）
type Rating string

const (
	// RatingSafe 安全级内容（gelbooru 新体系中的 general）
	RatingSafe Rating = "safe"
	// RatingSensitive 轻度敏感级内容（泳装、暗示等；gelbooru 新体系新增档位）
	RatingSensitive Rating = "sensitive"
	// RatingQuestionable 敏感级内容
	RatingQuestionable Rating = "questionable"
	// RatingExplicit 露骨级内容（NSFW）
	RatingExplicit Rating = "explicit"
)

// rank 返回分级的严格度等级，用于大小比较（safe < sensitive < questionable < explicit）。
func (r Rating) rank() int {
	switch r {
	case RatingSafe:
		return 1
	case RatingSensitive:
		return 2
	case RatingQuestionable:
		return 3
	case RatingExplicit:
		return 4
	}
	return 0
}

// RatingRange 内容分级区间（闭区间 [Min, Max]）。
//
// 配置语法（plugins.pic.rating）：
//   - 单档精确匹配：如 "safe"、"explicit"，表示只发该档内容
//   - 区间：如 "safe..questionable"，表示档位在 [Min, Max] 之间的内容
//   - "all"：全部档位（等价 "safe..explicit"）
//
// 站点可用性 = 站点提供的档位与区间有交集；请求时按区间生成搜索标签
// （单档用正标签，多档用排除法，见 site.rangeTags）。
type RatingRange struct {
	Min Rating
	Max Rating
}

// contains 报告档位 c 是否落在区间内。
func (r RatingRange) contains(c Rating) bool {
	return c.rank() > 0 && r.Min.rank() <= c.rank() && c.rank() <= r.Max.rank()
}

// String 返回区间的配置表示（如 "safe..questionable"、单档时 "safe"、"all" 时全区间）。
func (r RatingRange) String() string {
	if r.Min.rank() <= 0 || r.Max.rank() <= 0 {
		return string(RatingSafe)
	}
	if r.Min == RatingSafe && r.Max == RatingExplicit {
		return "all"
	}
	if r.Min == r.Max {
		return string(r.Min)
	}
	return string(r.Min) + ".." + string(r.Max)
}

// ratingTag 返回单档对应的基础 rating 搜索标签（如 "rating:safe"）。
func ratingTag(r Rating) string {
	return "rating:" + string(r)
}

// parseRating 解析单档名称，非法值回退为 RatingSafe。
// 兼容旧配置中的 "safe"；"all" 请使用 parseRatingRange。
func parseRating(s string) Rating {
	switch Rating(strings.TrimSpace(s)) {
	case RatingSafe, RatingSensitive, RatingQuestionable, RatingExplicit:
		return Rating(strings.TrimSpace(s))
	}
	return RatingSafe
}

// parseRatingRange 解析内容分级区间配置。
//
// 语法：
//   - "safe"                    单档精确匹配
//   - "safe..questionable"      区间（Min..Max，须 Min <= Max）
//   - "all"                     全部档位
//
// 非法值回退为 {RatingSafe, RatingSafe}。
func parseRatingRange(s string) RatingRange {
	v := strings.TrimSpace(s)
	if strings.EqualFold(v, "all") {
		return RatingRange{Min: RatingSafe, Max: RatingExplicit}
	}
	if lo, hi, ok := strings.Cut(v, ".."); ok {
		min, max := parseRating(lo), parseRating(hi)
		if min.rank() > max.rank() {
			min, max = max, min
		}
		return RatingRange{Min: min, Max: max}
	}
	r := parseRating(v)
	return RatingRange{Min: r, Max: r}
}

// ── 站点表 ───────────────────────────────────────────────────────────────

// protocol booru API 协议类型。
type protocol string

const (
	// protocolGelbooru Gelbooru 系 API（safebooru / gelbooru / rule34 等）
	protocolGelbooru protocol = "gelbooru"
	// protocolMoebooru Moebooru API（konachan / yande.re）
	protocolMoebooru protocol = "moebooru"
)

// site 描述一个 booru 图库站点。
type site struct {
	Name string
	// DisplayName 展示用名称（如 "Safebooru"、"Yande.re"）。
	DisplayName string
	Domain      string
	Protocol    protocol
	// Ratings 该站点可提供的全部内容分级（按严格度升序）。
	// 站点选择时与请求区间求交集，交集为空则不可用。
	Ratings []Rating
	// RatingTags 该站点各档位对应的正向搜索标签，key 为内部 Rating。
	// 未配置的档位回退为 "rating:<name>"；空字符串表示该档无需过滤
	// （仅限整站仅含该档内容的站点，如 safebooru / konachan）。
	RatingTags map[Rating]string
}

// ratingSearchTag 返回该站点上某档位的正向搜索标签。
// 优先使用站点自身的 RatingTags 映射（应对站点分级体系迁移），
// 未配置时回退到全局 ratingTag。
func (s site) ratingSearchTag(r Rating) string {
	if tag, ok := s.RatingTags[r]; ok {
		return tag
	}
	return ratingTag(r)
}

// rangeTags 返回该站点上按区间 rng 请求时附加的 rating 搜索标签列表。
//
// 规则：
//   - 区间与站点档位无交集 → 返回 nil（站点不可用）
//   - 区间覆盖站点全部档位 → 返回空列表（无需过滤）
//   - 区间内只有一个档位 → 正向标签（如 rating:general）
//   - 区间内多个档位 → 排除法：排除区间外（低于 Min / 高于 Max）的档位，
//     使用 -rating:xxx 形式（Gelbooru 系多排除实测可靠）
//
// moebooru（konachan / yande.re）的多个 -rating 排除不可靠（实测只取最后一个），
// 但这两站档位少（≤3 档），区间多档时排除数至多 1 个，天然规避该问题。
func (s site) rangeTags(rng RatingRange) []string {
	inRange := make([]Rating, 0, len(s.Ratings))
	for _, r := range s.Ratings {
		if rng.contains(r) {
			inRange = append(inRange, r)
		}
	}
	if len(inRange) == 0 {
		return nil
	}
	if len(inRange) == 1 {
		if tag := s.ratingSearchTag(inRange[0]); tag != "" {
			return []string{tag}
		}
		return nil
	}
	var out []string
	for _, r := range s.Ratings {
		if rng.contains(r) {
			continue
		}
		if tag := s.ratingSearchTag(r); tag != "" {
			out = append(out, "-"+tag)
		}
	}
	return out
}

// usable 报告在 rng 区间下该站点是否可用（站点档位与区间有交集）。
func (s site) usable(rng RatingRange) bool {
	return slices.ContainsFunc(s.Ratings, rng.contains)
}

// builtinSites 内置站点表。
//
// 站点域的选择依据（2026 年实测）：
//   - safebooru.org：公开 API 直连可用
//   - gelbooru.com：dapi 自 2023 年起要求 api_key（免费注册），否则返回 401
//   - api.rule34.xxx：rule34 的 API 独立子域，公开请求无 CAPTCHA，
//     但要求 user_id + api_key 认证（账号设置页获取）；
//     主域 rule34.xxx 的 API 路径被 CAPTCHA 反爬拦截，不可用
//   - konachan.net：konachan 的 SFW 镜像子域，无 Cloudflare 挑战，
//     仅提供 safe 内容；konachan.com 被 Cloudflare 挑战拦截，不可用
//   - yande.re：公开 API 直连可用
//
// RatingTags 为各档位正向搜索标签（实测 2026-08）：
//   - gelbooru.com 已迁移至 Danbooru 式 4 档：general / sensitive / questionable / explicit，
//     其中 general 与内部 safe 同档；旧标签 rating:safe 仅剩 4 张遗留图，不可用
//   - safebooru.org 新旧评级并存（safe 与 general 均有），整站仅 safe 内容，
//     无需 rating 过滤（过滤会漏掉一半）
//   - api.rule34.xxx 保留旧体系；站点定位仅 explicit（遗留少量 safe/questionable 帖），
//     显式过滤到 rating:explicit 符合站点定位
//   - konachan.net 为 SFW 镜像，整站仅 safe 内容，无需 rating 过滤
//   - yande.re 保留旧体系（s/q/e）
var builtinSites = []site{
	{Name: "safebooru", DisplayName: "Safebooru", Domain: "safebooru.org", Protocol: protocolGelbooru,
		Ratings:    []Rating{RatingSafe},
		RatingTags: map[Rating]string{RatingSafe: ""}},
	{Name: "gelbooru", DisplayName: "Gelbooru", Domain: "gelbooru.com", Protocol: protocolGelbooru,
		Ratings: []Rating{RatingSafe, RatingSensitive, RatingQuestionable, RatingExplicit},
		RatingTags: map[Rating]string{
			RatingSafe:         "rating:general",
			RatingSensitive:    "rating:sensitive",
			RatingQuestionable: "rating:questionable",
			RatingExplicit:     "rating:explicit",
		}},
	{Name: "rule34", DisplayName: "rule34.xxx", Domain: "api.rule34.xxx", Protocol: protocolGelbooru,
		Ratings:    []Rating{RatingExplicit},
		RatingTags: map[Rating]string{RatingExplicit: "rating:explicit"}},
	{Name: "konachan", DisplayName: "Konachan", Domain: "konachan.net", Protocol: protocolMoebooru,
		Ratings:    []Rating{RatingSafe},
		RatingTags: map[Rating]string{RatingSafe: ""}},
	{Name: "yandere", DisplayName: "Yande.re", Domain: "yande.re", Protocol: protocolMoebooru,
		Ratings: []Rating{RatingSafe, RatingQuestionable, RatingExplicit},
		RatingTags: map[Rating]string{
			RatingSafe:         "rating:safe",
			RatingQuestionable: "rating:questionable",
			RatingExplicit:     "rating:explicit",
		}},
}

// findSite 按名称查找站点，不区分大小写。
func findSite(name string) (site, bool) {
	for _, s := range builtinSites {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return site{}, false
}

// candidateSites 返回可用站点列表（已随机打乱顺序）。
// enabled 为白名单（空 = 全部内置站点）；rng 为请求的内容分级区间。
// 无可用站点时返回空切片。
//
// 返回打乱顺序的列表而非单个站点：调用方逐个尝试，
// 单个站点请求失败时自动降级到下一个，避免一个慢站点拖垮整次命令。
func candidateSites(enabled []string, rng RatingRange) []site {
	allowSet := make(map[string]struct{}, len(enabled))
	for _, name := range enabled {
		allowSet[strings.ToLower(name)] = struct{}{}
	}

	var candidates []site
	for _, s := range builtinSites {
		if len(allowSet) > 0 {
			if _, ok := allowSet[strings.ToLower(s.Name)]; !ok {
				continue
			}
		}
		if s.usable(rng) {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	rand.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})
	return candidates
}
