// Package pic 提供按标签发送随机图片的功能。
//
// 聚合多个 booru 图库（Safebooru / Gelbooru / rule34 / Konachan / Yande.re），
// 按全局 rating 策略自动过滤站点与内容，命令 /pic 发送图片并附带作品信息。
package pic

import (
	"math/rand"
	"strings"
)

// ── 内容分级 ─────────────────────────────────────────────────────────────

// Rating 内容分级，与 booru 站点的 rating 标签一致。
type Rating string

const (
	// RatingSafe 安全级内容
	RatingSafe Rating = "safe"
	// RatingQuestionable 敏感级内容
	RatingQuestionable Rating = "questionable"
	// RatingExplicit 露骨级内容（NSFW）
	RatingExplicit Rating = "explicit"
)

// rank 返回分级的严格度等级，用于大小比较（safe < questionable < explicit）。
func (r Rating) rank() int {
	switch r {
	case RatingSafe:
		return 1
	case RatingQuestionable:
		return 2
	case RatingExplicit:
		return 3
	}
	return 0
}

// allows 报告允许的最高分级 r 是否包含 c（c <= r 即可）。
func (r Rating) allows(c Rating) bool {
	// "all" 表示不限制
	if r == "all" {
		return true
	}
	return c.rank() > 0 && c.rank() <= r.rank()
}

// ratingTag 返回站点请求要附加的 rating 标签；r 为 "all" 时不附加。
func ratingTag(r Rating) string {
	if r == "all" {
		return ""
	}
	return "rating:" + string(r)
}

// parseRating 解析配置字符串为 Rating，非法值回退为 RatingSafe。
func parseRating(s string) Rating {
	switch Rating(strings.TrimSpace(s)) {
	case RatingSafe, RatingQuestionable, RatingExplicit, "all":
		return Rating(strings.TrimSpace(s))
	}
	return RatingSafe
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
	// Ratings 该站点可提供的全部内容分级。
	// 站点选择时与全局 rating 策略求交集，交集为空则不可用。
	Ratings []Rating
}

// maxRating 返回站点可提供的最高分级。
func (s site) maxRating() Rating {
	max := RatingSafe
	for _, r := range s.Ratings {
		if r.rank() > max.rank() {
			max = r
		}
	}
	return max
}

// effectiveRating 返回在该站点上请求时使用的实际分级标签值。
// 取 min(全局允许值, 站点最高分级)；全局为 "all" 时取站点最高分级。
func (s site) effectiveRating(allowed Rating) Rating {
	if allowed == "all" {
		return s.maxRating()
	}
	top := allowed
	if s.maxRating().rank() < top.rank() {
		top = s.maxRating()
	}
	return top
}

// usable 报告在 allowed 策略下该站点是否可用（两者分级集合有交集）。
func (s site) usable(allowed Rating) bool {
	for _, r := range s.Ratings {
		if allowed.allows(r) {
			return true
		}
	}
	return false
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
var builtinSites = []site{
	{Name: "safebooru", DisplayName: "Safebooru", Domain: "safebooru.org", Protocol: protocolGelbooru,
		Ratings: []Rating{RatingSafe}},
	{Name: "gelbooru", DisplayName: "Gelbooru", Domain: "gelbooru.com", Protocol: protocolGelbooru,
		Ratings: []Rating{RatingSafe, RatingQuestionable, RatingExplicit}},
	{Name: "rule34", DisplayName: "rule34.xxx", Domain: "api.rule34.xxx", Protocol: protocolGelbooru,
		Ratings: []Rating{RatingExplicit}},
	{Name: "konachan", DisplayName: "Konachan", Domain: "konachan.net", Protocol: protocolMoebooru,
		Ratings: []Rating{RatingSafe}},
	{Name: "yandere", DisplayName: "Yande.re", Domain: "yande.re", Protocol: protocolMoebooru,
		Ratings: []Rating{RatingSafe, RatingQuestionable, RatingExplicit}},
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
// enabled 为白名单（空 = 全部内置站点）；allowed 为全局 rating 策略。
// 无可用站点时返回空切片。
//
// 返回打乱顺序的列表而非单个站点：调用方逐个尝试，
// 单个站点请求失败时自动降级到下一个，避免一个慢站点拖垮整次命令。
func candidateSites(enabled []string, allowed Rating) []site {
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
		if s.usable(allowed) {
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
