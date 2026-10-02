package songdle

import (
	"slices"
	"strconv"
	"testing"
)

// testSongsJSON / testAliasJSON 是命令与逻辑测试共用的最小曲库。
const testSongsJSON = `{
  "1": {"id":"1","title":"Halcyon","artist":"sasakure.UK","genre":"流行&动漫","type":"DX","version":"maimai DX","bpm":155,"masds":13.5,"maslevel":"13+","mascharter":"X","masbreak":5,"expds":11.0,"explevel":"11","expcharter":"Y"},
  "2": {"id":"2","title":"True Love Song","artist":"Kai","genre":"舞萌","type":"SD","version":"maimai","bpm":150,"masds":12.4,"maslevel":"12","mascharter":"Z","masbreak":6,"expds":10.2,"explevel":"10","expcharter":"W"},
  "3": {"id":"3","title":"Future","artist":"★STAR GUiTAR [cover]","genre":"流行&动漫","type":"SD","version":"maimai","bpm":130,"masds":10.7,"maslevel":"10+","mascharter":"Q","masbreak":9,"expds":9.5,"explevel":"9","expcharter":"E"}
}`

const testAliasJSON = `[
  {"SongID":2,"Name":"true love song","Alias":["true love song","真爱歌","糖糖"]},
  {"SongID":3,"Name":"future","Alias":["future","未来","ftr"]}
]`

// newTestPool 用最小曲库构建 Pool，测试里用固定下标访问曲目。
func newTestPool(t *testing.T) *Pool {
	t.Helper()
	p, err := buildPool([]byte(testSongsJSON), []byte(testAliasJSON))
	if err != nil {
		t.Fatalf("buildPool: %v", err)
	}
	return p
}

// trackByID 从池中按 ID 取曲目，找不到时直接失败。
func trackByID(t *testing.T, p *Pool, id string) Track {
	t.Helper()
	for _, tr := range p.All() {
		if tr.ID == id {
			return tr
		}
	}
	t.Fatalf("曲库中找不到 ID %q", id)
	return Track{}
}

func TestNormalizeTitle(t *testing.T) {
	cases := map[string]string{
		"Night of Nights":    "nightofnights",
		"  Stay   With  Me ": "staywithme",
		"":                   "",
		"Ｆｕｔｕｒｅ":             "ｆｕｔｕｒｅ",
	}
	for in, want := range cases {
		if got := normalizeTitle(in); got != want {
			t.Errorf("normalizeTitle(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestBuildPoolMergesAliases(t *testing.T) {
	p := newTestPool(t)
	if p.Len() != 3 {
		t.Fatalf("曲目数 = %d, 期望 3", p.Len())
	}
	tr := trackByID(t, p, "2")
	if len(tr.Aliases) != 3 {
		t.Fatalf("别名应被去重并保留 3 个，实际 %v", tr.Aliases)
	}
	if !tr.Matches("糖糖") || !tr.Matches("真爱歌") {
		t.Errorf("别名应可命中 True Love Song，实际 %v", tr.Aliases)
	}
}

func TestTrackMatches(t *testing.T) {
	tr := Track{Title: "True Love Song", Aliases: []string{"糖糖", "真爱歌"}}
	for _, guess := range []string{"true love song", "TRUE LOVE SONG", "糖糖", " 真爱歌 "} {
		if !tr.Matches(guess) {
			t.Errorf("%q 应命中", guess)
		}
	}
	if tr.Matches("未来") {
		t.Error("无关别名不应命中")
	}
	if tr.Matches("") {
		t.Error("空猜测不应命中")
	}
}

func TestPoolFilter(t *testing.T) {
	p := newTestPool(t)
	if got := p.Filter(Filter{Type: "DX"}); len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("按 DX 过滤应只剩 Halcyon，实际 %+v", titles(got))
	}
	if got := p.Filter(Filter{Genre: "流行"}); len(got) != 2 {
		t.Fatalf("按流派「流行」过滤应有 2 首，实际 %+v", titles(got))
	}
	if got := p.Filter(Filter{Type: "SD", Genre: "舞萌"}); len(got) != 1 || got[0].ID != "2" {
		t.Fatalf("组合过滤应只剩 True Love Song，实际 %+v", titles(got))
	}
	if got := p.Filter(Filter{Type: "DX", Genre: "舞萌"}); len(got) != 0 {
		t.Fatalf("无交集时应为空，实际 %+v", titles(got))
	}
}

func TestPoolPickAndDaily(t *testing.T) {
	p := newTestPool(t)
	for range 10 {
		got, err := p.Pick(Filter{})
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if got.ID == "" {
			t.Fatal("Pick 返回空曲目")
		}
	}
	if _, err := p.Pick(Filter{Type: "DX", Genre: "舞萌"}); err == nil {
		t.Fatal("空池应返回错误")
	}

	a, err := p.PickDaily(Filter{}, "2026-10-02|")
	if err != nil {
		t.Fatalf("PickDaily: %v", err)
	}
	b, err := p.PickDaily(Filter{}, "2026-10-02|")
	if err != nil {
		t.Fatalf("PickDaily: %v", err)
	}
	if a.ID != b.ID {
		t.Fatalf("同一 seed 的每日题应稳定: %s != %s", a.ID, b.ID)
	}
}

func TestPoolCounts(t *testing.T) {
	p := newTestPool(t)
	types := p.TypeCounts()
	if len(types) != 2 {
		t.Fatalf("类型统计应有 SD / DX 两项，实际 %+v", types)
	}
	if types[0].Tag != "SD" || types[0].Count != 2 {
		t.Errorf("SD 应为首项且计数 2，实际 %+v", types)
	}
	genres := p.GenreCounts()
	if len(genres) != 2 || genres[0].Tag != "流行&动漫" || genres[0].Count != 2 {
		t.Errorf("流派统计错误: %+v", genres)
	}
	var empty Pool
	if empty.Len() != 0 || empty.All() != nil {
		t.Error("空池应安全返回零值")
	}
}

func TestEmbeddedPoolLoads(t *testing.T) {
	p, err := loadPool(poolOptions{})
	if err != nil {
		t.Fatalf("加载内置曲库失败: %v", err)
	}
	if p.Len() < 1000 {
		t.Fatalf("内置曲库曲目数 = %d, 期望 >= 1000", p.Len())
	}
	var found bool
	for _, tr := range p.All() {
		if tr.Matches("真爱歌") {
			found = true
			if tr.Title != "True Love Song" {
				t.Errorf("别名「真爱歌」应命中 True Love Song，实际 %q", tr.Title)
			}
			if tr.MasDS <= 0 || tr.BPM <= 0 {
				t.Errorf("内置曲目应带有定数与 BPM: %+v", tr)
			}
			break
		}
	}
	if !found {
		t.Fatal("内置别名「真爱歌」应能命中曲目")
	}
	// 曲库按 ID 排序登记，保证索引稳定。
	if !slices.IsSortedFunc(p.All(), func(a, b Track) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	}) {
		t.Error("曲库应按 ID 排序")
	}
}

func titles(tracks []Track) []string {
	out := make([]string, len(tracks))
	for i, t := range tracks {
		out[i] = t.Title
	}
	return out
}

func TestPoolByID(t *testing.T) {
	p := newTestPool(t)
	if tr, ok := p.ByID("2"); !ok || tr.Title != "True Love Song" {
		t.Fatalf("ByID(2) = %+v,%v", tr, ok)
	}
	if tr, ok := p.ByID("  3 "); !ok || tr.Title != "Future" {
		t.Errorf("ByID 应忽略首尾空白，实际 %+v,%v", tr, ok)
	}
	if _, ok := p.ByID("999"); ok {
		t.Error("不存在的 ID 不应命中")
	}
	var empty *Pool
	if _, ok := empty.ByID("1"); ok {
		t.Error("空池不应命中")
	}
}

func TestPoolMatchExact(t *testing.T) {
	p := newTestPool(t)
	cases := []struct {
		query string
		id    string
	}{
		{"2", "2"},              // 曲目 ID
		{"true love song", "2"}, // 完全同名（忽略大小写）
		{"TRUE LOVE SONG", "2"}, // 完全同名（全大写）
		{" 糖糖 ", "2"},           // 完全同别名（含空白）
		{"未来", "3"},             // 完全同别名
		{"Future", "3"},         // 曲名精确匹配
	}
	for _, c := range cases {
		res := p.Match(c.query)
		if !res.Exact {
			t.Errorf("Match(%q) 应为精确匹配，实际 %+v", c.query, res)
			continue
		}
		if len(res.Candidates) != 1 || res.Candidates[0].ID != c.id {
			t.Errorf("Match(%q) 候选 = %+v, 期望 ID %q", c.query, titles(res.Candidates), c.id)
		}
	}
}

func TestPoolMatchFuzzyAndEmpty(t *testing.T) {
	p := newTestPool(t)
	res := p.Match("fut")
	if res.Exact {
		t.Error("包含匹配不应标为精确")
	}
	if len(res.Candidates) != 1 || res.Candidates[0].ID != "3" {
		t.Fatalf("Match(fut) 候选 = %+v", titles(res.Candidates))
	}
	if got := p.Match("zzz"); len(got.Candidates) != 0 || got.Exact {
		t.Errorf("无匹配应返回空，实际 %+v", got)
	}
	if got := p.Match("  "); len(got.Candidates) != 0 {
		t.Errorf("空查询应返回空，实际 %+v", got)
	}
	if got := p.Match("x"); len(got.Candidates) != 0 {
		t.Errorf("单字符查询过于宽泛，应被忽略，实际 %+v", titles(got.Candidates))
	}
	var empty *Pool
	if got := empty.Match("x"); len(got.Candidates) != 0 || got.Exact {
		t.Errorf("空池 Match 应返回零值，实际 %+v", got)
	}
}

func TestPoolMatchSameTitleSDDX(t *testing.T) {
	const data = `{
  "1": {"id":"1","title":"Twin","artist":"A","type":"SD","version":"maimai","bpm":100,"masds":10,"masbreak":1},
  "10001": {"id":"10001","title":"Twin","artist":"A","type":"DX","version":"maimai","bpm":100,"masds":10.5,"masbreak":1}
}`
	p, err := buildPool([]byte(data), nil)
	if err != nil {
		t.Fatalf("buildPool: %v", err)
	}
	res := p.Match("twin")
	if !res.Exact || len(res.Candidates) != 2 {
		t.Fatalf("同名 SD/DX 应作为精确候选一并返回，实际 %+v", res)
	}
	if res.Candidates[0].Key() != res.Candidates[1].Key() {
		t.Error("同名候选应有相同的 Key")
	}
}

func TestCapCandidatesDedupAndTruncate(t *testing.T) {
	mk := func(id string) Track { return Track{ID: id, Title: "T" + id} }
	bucket := make([]Track, 0, maxMatchCandidates+8)
	for i := range maxMatchCandidates + 8 {
		bucket = append(bucket, mk(strconv.Itoa(i)))
	}
	got := capCandidates(bucket, []Track{mk("0"), mk("1")})
	if len(got) != maxMatchCandidates {
		t.Fatalf("候选应截断到 %d，实际 %d", maxMatchCandidates, len(got))
	}
	seen := make(map[string]bool, len(got))
	for _, tr := range got {
		if seen[tr.ID] {
			t.Errorf("候选应按 ID 去重，出现重复 %q", tr.ID)
		}
		seen[tr.ID] = true
	}
}

func TestPoolMatchExplicitID(t *testing.T) {
	p := newTestPool(t)
	if got := p.Match("#2"); !got.Exact || len(got.Candidates) != 1 || got.Candidates[0].ID != "2" {
		t.Errorf("Match(#2) 应为 ID 2，实际 %+v", titles(got.Candidates))
	}
	if got := p.Match("id:3"); len(got.Candidates) != 1 || got.Candidates[0].ID != "3" {
		t.Errorf("Match(id:3) 应为 ID 3，实际 %+v", titles(got.Candidates))
	}
	if got := p.Match("ID:  3 "); len(got.Candidates) != 1 || got.Candidates[0].ID != "3" {
		t.Errorf("显式 ID 应忽略大小写与空白，实际 %+v", titles(got.Candidates))
	}
	if got := p.Match("#999"); len(got.Candidates) != 0 {
		t.Errorf("不存在的显式 ID 应返回空，实际 %+v", titles(got.Candidates))
	}
	if got := p.Match("#abc"); len(got.Candidates) != 0 {
		t.Errorf("#abc 不是合法 ID，不应命中，实际 %+v", titles(got.Candidates))
	}
}

// TestPoolMatchNumericCollision 验证「数字既是曲目 ID 又是别名」时返回并集，
// 由上层「谜底在候选里就判中」的逻辑消歧，必要时再用 #ID 精确指定。
func TestPoolMatchNumericCollision(t *testing.T) {
	const data = `{
  "9": {"id":"9","title":"Nine","artist":"A","type":"SD","version":"maimai","bpm":100,"masds":10,"masbreak":1},
  "302": {"id":"302","title":"Other","artist":"B","type":"SD","version":"maimai","bpm":120,"masds":11,"masbreak":2}
}`
	const aliases = `[{"SongID":302,"Alias":["9"]}]`
	p, err := buildPool([]byte(data), []byte(aliases))
	if err != nil {
		t.Fatalf("buildPool: %v", err)
	}
	res := p.Match("9")
	if !res.Exact {
		t.Fatalf("数字冲突应仍为精确匹配，实际 %+v", res)
	}
	ids := make([]string, 0, len(res.Candidates))
	for _, c := range res.Candidates {
		ids = append(ids, c.ID)
	}
	slices.Sort(ids)
	if len(ids) != 2 || ids[0] != "302" || ids[1] != "9" {
		t.Fatalf("Match(9) 应同时包含 ID 9 与别名 9 的曲目，实际 %v", ids)
	}
	if got := p.Match("#9"); len(got.Candidates) != 1 || got.Candidates[0].ID != "9" {
		t.Errorf("Match(#9) 应精确到 ID 9，实际 %v", titles(got.Candidates))
	}
	if got := p.Match("#302"); len(got.Candidates) != 1 || got.Candidates[0].ID != "302" {
		t.Errorf("Match(#302) 应精确到 ID 302，实际 %v", titles(got.Candidates))
	}
}
