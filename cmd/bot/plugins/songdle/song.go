// Package songdle 提供「猜音游曲目」小游戏插件。
//
// 谜底是一首 maimai（舞萌DX）曲目，最终答案是**曲名**。玩家不直接猜整首歌，而是
// 逐个探测谜底的元数据来缩小范围：给一个曲师、流派、类型、版本、BPM、定数或绝赞数，
// 机器人告诉你是「就是它 / 接近 / 不是」，数值项还会提示谜底比所猜更高还是更低；
// 有了足够的线索后再用 /songdle 歌名 <曲名> 提交最终答案即获胜。
//
// 曲库默认来自随包内置的 songs/maimai.json 与 songs/maimai_alias.json，可用
// plugins.songdle.songs_file / alias_file 指向本地文件，或 songs_url / alias_url
// 从远端拉取覆盖，方便音游群换成自维护的数据集。
package songdle

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

//go:embed songs/*.json
var songsFS embed.FS

const (
	embeddedSongsFile = "songs/maimai.json"
	embeddedAliasFile = "songs/maimai_alias.json"
	// fetchTimeout 是远端曲库下载的超时时间。
	fetchTimeout = 20 * time.Second
	// maxFetchBytes 限制远端曲库大小，避免误配 URL 拉爆内存。
	maxFetchBytes = 64 << 20
)

// ErrEmptyPool 表示筛选后没有任何可出题的曲目。
var ErrEmptyPool = errors.New("songdle: 曲库为空")

// Track 是一首可出题 / 可探测的曲目。
type Track struct {
	ID         string
	Title      string
	Artist     string
	Genre      string
	Type       string // SD / DX
	Version    string
	BPM        int
	ExpDS      float64
	ExpLevel   string
	ExpCharter string
	MasDS      float64
	MasLevel   string
	MasCharter string
	MasBreak   int
	RemCharter string
	Aliases    []string
}

// Key 返回曲目的去重 / 索引键：小写并去掉所有空白后的曲名。
func (t Track) Key() string { return normalizeTitle(t.Title) }

// Display 返回「曲名 — 曲师」形式的展示文本；曲师为空时只返回曲名。
func (t Track) Display() string {
	if strings.TrimSpace(t.Artist) == "" {
		return t.Title
	}
	return t.Title + " — " + t.Artist
}

// Matches 报告某个猜名是否与本题匹配（忽略大小写与空白，也匹配别名）。
func (t Track) Matches(guess string) bool {
	g := normalizeTitle(guess)
	if g == "" {
		return false
	}
	if g == t.Key() {
		return true
	}
	return slices.ContainsFunc(t.Aliases, func(a string) bool { return normalizeTitle(a) == g })
}

// normalizeTitle 归一化用于比较的名称：小写、去空白。
func normalizeTitle(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Filter 描述出题时的曲库筛选条件；零值表示全曲库。
type Filter struct {
	Type  string // SD / DX / ""（不限）
	Genre string // 流派子串，""（不限）
}

func (f Filter) match(t Track) bool {
	if f.Type != "" && !strings.EqualFold(strings.TrimSpace(t.Type), f.Type) {
		return false
	}
	if f.Genre != "" && !strings.Contains(strings.ToLower(t.Genre), strings.ToLower(f.Genre)) {
		return false
	}
	return true
}

// key 返回可用于每日题种子的稳定标识。
func (f Filter) key() string { return f.Type + "|" + strings.ToLower(f.Genre) }

// rawTrack 是曲目 JSON 的反序列化结构。
type rawTrack struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Artist     string  `json:"artist"`
	Genre      string  `json:"genre"`
	Type       string  `json:"type"`
	Version    string  `json:"version"`
	BPM        int     `json:"bpm"`
	ExpDS      float64 `json:"expds"`
	ExpLevel   string  `json:"explevel"`
	ExpCharter string  `json:"expcharter"`
	MasDS      float64 `json:"masds"`
	MasLevel   string  `json:"maslevel"`
	MasCharter string  `json:"mascharter"`
	MasBreak   int     `json:"masbreak"`
	RemCharter string  `json:"remcharter"`
}

// rawAlias 是别名 JSON 的反序列化结构。
type rawAlias struct {
	SongID int      `json:"SongID"`
	Alias  []string `json:"Alias"`
}

// poolOptions 描述曲库来源；本地文件优先于远端 URL，二者都为空时使用内置数据。
type poolOptions struct {
	SongsFile string
	AliasFile string
	SongsURL  string
	AliasURL  string
}

// loadPool 加载并构建曲库。
func loadPool(opts poolOptions) (*Pool, error) {
	songsData, err := readSource(opts.SongsFile, opts.SongsURL, embeddedSongsFile)
	if err != nil {
		return nil, fmt.Errorf("songdle: 加载曲目数据: %w", err)
	}
	aliasData, err := readSource(opts.AliasFile, opts.AliasURL, embeddedAliasFile)
	if err != nil {
		return nil, fmt.Errorf("songdle: 加载别名数据: %w", err)
	}
	return buildPool(songsData, aliasData)
}

// readSource 依次尝试本地文件、远端 URL、内置文件。
func readSource(file, rawURL, embedded string) ([]byte, error) {
	if file = strings.TrimSpace(file); file != "" {
		return os.ReadFile(file)
	}
	if rawURL = strings.TrimSpace(rawURL); rawURL != "" {
		return fetchURL(rawURL)
	}
	return songsFS.ReadFile(embedded)
}

// fetchURL 下载远端曲库数据。
func fetchURL(rawURL string) ([]byte, error) {
	client := &http.Client{Timeout: fetchTimeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
}

// buildPool 从曲目与别名的 JSON 数据构建曲库。
//
// 按曲目 ID 排序后依次登记，保证索引与每日题抽取的稳定性；缺失曲名或完全为空的
// 数据集返回 [ErrEmptyPool]。别名文件中不存在于曲目表的条目会被忽略。
func buildPool(songsData, aliasData []byte) (*Pool, error) {
	var raw map[string]rawTrack
	if err := json.Unmarshal(songsData, &raw); err != nil {
		return nil, fmt.Errorf("解析曲目 JSON: %w", err)
	}

	aliases := make(map[string][]string)
	if strings.TrimSpace(string(aliasData)) != "" {
		var list []rawAlias
		if err := json.Unmarshal(aliasData, &list); err != nil {
			return nil, fmt.Errorf("解析别名 JSON: %w", err)
		}
		for _, a := range list {
			if len(a.Alias) == 0 {
				continue
			}
			aliases[strconv.Itoa(a.SongID)] = a.Alias
		}
	}

	ids := make([]string, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	p := &Pool{
		genres: make(map[string]int),
		types:  make(map[string]int),
		index:  make(map[string]int, len(raw)),
	}
	for _, id := range ids {
		r := raw[id]
		title := strings.TrimSpace(r.Title)
		if title == "" {
			continue
		}
		key := id
		if r.ID != "" {
			key = r.ID
		}
		t := Track{
			ID:         key,
			Title:      title,
			Artist:     strings.TrimSpace(r.Artist),
			Genre:      strings.TrimSpace(r.Genre),
			Type:       strings.TrimSpace(r.Type),
			Version:    strings.TrimSpace(r.Version),
			BPM:        r.BPM,
			ExpDS:      r.ExpDS,
			ExpLevel:   r.ExpLevel,
			ExpCharter: r.ExpCharter,
			MasDS:      r.MasDS,
			MasLevel:   r.MasLevel,
			MasCharter: r.MasCharter,
			MasBreak:   r.MasBreak,
			RemCharter: r.RemCharter,
			Aliases:    dedupeStrings(aliases[key]),
		}
		p.index[key] = len(p.tracks)
		p.tracks = append(p.tracks, t)
		if t.Genre != "" {
			p.genres[t.Genre]++
		}
		if t.Type != "" {
			p.types[t.Type]++
		}
	}
	if len(p.tracks) == 0 {
		return nil, ErrEmptyPool
	}
	return p, nil
}

// dedupeStrings 去空白、去重（按归一化键）后保持原顺序返回。
func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		k := normalizeTitle(s)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, s)
	}
	return out
}

// TagCount 是某个分类及其曲目数量。
type TagCount struct {
	Tag   string
	Count int
}

// Pool 是构建完成的曲库，提供抽题与统计。
type Pool struct {
	tracks []Track
	genres map[string]int
	types  map[string]int
	// index 是曲目 ID 到 tracks 下标的索引，用于按 ID 解析玩家输入。
	index map[string]int
}

// Len 返回曲库曲目总数。
func (p *Pool) Len() int {
	if p == nil {
		return 0
	}
	return len(p.tracks)
}

// All 返回全部曲目的副本。
func (p *Pool) All() []Track {
	if p == nil {
		return nil
	}
	return slices.Clone(p.tracks)
}

// Filter 返回符合筛选条件的曲目。
func (p *Pool) Filter(f Filter) []Track {
	if p == nil {
		return nil
	}
	out := make([]Track, 0, len(p.tracks))
	for _, t := range p.tracks {
		if f.match(t) {
			out = append(out, t)
		}
	}
	return out
}

// maxMatchCandidates 限制一次查询返回的候选数量，避免刷屏。
const maxMatchCandidates = 12

// ByID 按曲目 ID 精确查找。
func (p *Pool) ByID(id string) (Track, bool) {
	if p == nil {
		return Track{}, false
	}
	i, ok := p.index[strings.TrimSpace(id)]
	if !ok {
		return Track{}, false
	}
	return p.tracks[i], true
}

// MatchResult 是一次玩家输入的解析结果。
type MatchResult struct {
	// Exact 为真表示候选都来自精确匹配（曲目 ID / 完全同名 / 完全同别名），
	// 可以直接作为一次猜测；为假表示只是包含匹配的「你是不是想找」建议，
	// 不应据此判定胜负，也不会消耗次数。
	Exact bool
	// Candidates 是按贴合程度排序的候选曲目，最多 [maxMatchCandidates] 首。
	Candidates []Track
}

// Match 把玩家的自由输入解析为候选曲目，按贴合程度排序：
// 精确 ID → 完全同名 → 完全同别名；都没有命中时退化为包含匹配的建议。
//
// 同名 / 同别名的歧义（如同一首歌的 SD 与 DX 谱面共用曲名）会一并返回，
// 由调用方决定是判定「猜中谜底」还是提示玩家改用 ID 指定。
func (p *Pool) Match(query string) MatchResult {
	if p == nil {
		return MatchResult{}
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return MatchResult{}
	}
	if isDigits(q) {
		if t, ok := p.ByID(q); ok {
			return MatchResult{Exact: true, Candidates: []Track{t}}
		}
	}
	key := normalizeTitle(q)
	if key == "" {
		return MatchResult{}
	}

	var exactTitle, exactAlias, fuzzy []Track
	for _, t := range p.tracks {
		switch {
		case t.Key() == key:
			exactTitle = append(exactTitle, t)
		case hasAlias(t, key):
			exactAlias = append(exactAlias, t)
		case fuzzyTitle(t, key):
			fuzzy = append(fuzzy, t)
		}
	}
	if len(exactTitle)+len(exactAlias) > 0 {
		return MatchResult{Exact: true, Candidates: capCandidates(exactTitle, exactAlias)}
	}
	return MatchResult{Candidates: capCandidates(fuzzy)}
}

// capCandidates 按顺序拼接候选桶并截断到 [maxMatchCandidates]，同时按 ID 去重。
func capCandidates(buckets ...[]Track) []Track {
	out := make([]Track, 0, maxMatchCandidates)
	seen := make(map[string]struct{}, maxMatchCandidates)
	for _, bucket := range buckets {
		for _, t := range bucket {
			if len(out) >= maxMatchCandidates {
				return out
			}
			if _, ok := seen[t.ID]; ok {
				continue
			}
			seen[t.ID] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}

// hasAlias 报告曲目是否拥有与归一化键完全一致的别名。
func hasAlias(t Track, key string) bool {
	return slices.ContainsFunc(t.Aliases, func(a string) bool { return normalizeTitle(a) == key })
}

// fuzzyTitle 报告查询串是否是曲名 / 别名的子串；过短的查询不参与模糊匹配。
func fuzzyTitle(t Track, key string) bool {
	if len([]rune(key)) < 2 {
		return false
	}
	if containsEither(t.Key(), key) {
		return true
	}
	return slices.ContainsFunc(t.Aliases, func(a string) bool { return containsEither(normalizeTitle(a), key) })
}

// isDigits 报告字符串是否只由 ASCII 数字组成。
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Pick 从筛选后的曲库里随机抽取一首。
func (p *Pool) Pick(f Filter) (Track, error) {
	cands := p.Filter(f)
	if len(cands) == 0 {
		return Track{}, ErrEmptyPool
	}
	rng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	return cands[rng.IntN(len(cands))], nil
}

// PickDaily 以 seed 稳定地抽取一首曲目，让同一天同一筛选条件下所有人拿到同一题。
func (p *Pool) PickDaily(f Filter, seed string) (Track, error) {
	cands := p.Filter(f)
	if len(cands) == 0 {
		return Track{}, ErrEmptyPool
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte("songdle:daily:" + seed))
	return cands[int(h.Sum64()%uint64(len(cands)))], nil
}

// GenreCounts 返回按数量降序（同数量按名称）排列的流派统计。
func (p *Pool) GenreCounts() []TagCount {
	if p == nil {
		return nil
	}
	return sortedCounts(p.genres)
}

// TypeCounts 返回按数量降序（同数量按名称）排列的类型（SD/DX）统计。
func (p *Pool) TypeCounts() []TagCount {
	if p == nil {
		return nil
	}
	return sortedCounts(p.types)
}

func sortedCounts(m map[string]int) []TagCount {
	out := make([]TagCount, 0, len(m))
	for name, n := range m {
		out = append(out, TagCount{Tag: name, Count: n})
	}
	slices.SortFunc(out, func(a, b TagCount) int {
		if c := b.Count - a.Count; c != 0 {
			return c
		}
		return strings.Compare(a.Tag, b.Tag)
	})
	return out
}
