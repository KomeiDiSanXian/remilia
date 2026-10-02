package songdle

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// ErrBadValue 表示玩家提交的属性值无法解析（如 BPM 给了非数字）。
var ErrBadValue = errors.New("songdle: 属性取值无效")

// Mark 表示一次猜测相对谜底的命中程度。
type Mark uint8

const (
	// Miss 不匹配（⬜）。
	Miss Mark = iota
	// Close 接近 / 部分匹配（🟨）。
	Close
	// Match 完全匹配（🟩）。
	Match
)

// Emoji 返回判定对应的方块。
func (m Mark) Emoji() string {
	switch m {
	case Match:
		return "🟩"
	case Close:
		return "🟨"
	default:
		return "⬜"
	}
}

// Dir 表示谜底的数值相对本次猜测更高还是更低。
type Dir uint8

const (
	// DirNone 无方向（相等或无法比较）。
	DirNone Dir = iota
	// DirUp 谜底更高（猜低了，提示 ↑）。
	DirUp
	// DirDown 谜底更低（猜高了，提示 ↓）。
	DirDown
)

// Arrow 返回方向箭头；无方向时为空串。
func (d Dir) Arrow() string {
	switch d {
	case DirUp:
		return "↑"
	case DirDown:
		return "↓"
	default:
		return ""
	}
}

// Attribute 是谜底的一个可探测属性；[AttrTitle] 是最终答案。
type Attribute uint8

const (
	// AttrTitle 曲名——猜中即获胜。
	AttrTitle Attribute = iota
	// AttrArtist 曲师。
	AttrArtist
	// AttrGenre 流派。
	AttrGenre
	// AttrType 谱面类型（SD / DX）。
	AttrType
	// AttrVersion 版本。
	AttrVersion
	// AttrBPM BPM。
	AttrBPM
	// AttrConst MASTER 定数。
	AttrConst
	// AttrBreak 绝赞数。
	AttrBreak
)

// ClueAttributes 是除曲名外按展示顺序排列的可探测属性。
var ClueAttributes = []Attribute{AttrArtist, AttrGenre, AttrType, AttrVersion, AttrBPM, AttrConst, AttrBreak}

// String 返回属性的英文标识（配置 / 内部用）。
func (a Attribute) String() string {
	switch a {
	case AttrTitle:
		return "title"
	case AttrArtist:
		return "artist"
	case AttrGenre:
		return "genre"
	case AttrType:
		return "type"
	case AttrVersion:
		return "version"
	case AttrBPM:
		return "bpm"
	case AttrConst:
		return "const"
	case AttrBreak:
		return "break"
	default:
		return "unknown"
	}
}

// LabelKey 返回属性名对应的 i18n key。
func (a Attribute) LabelKey() string { return "songdle.attr." + a.String() }

// Numeric 报告该属性是否为数值型（需要大小 / 方向提示）。
func (a Attribute) Numeric() bool {
	return a == AttrBPM || a == AttrConst || a == AttrBreak
}

// attrAliases 把子命令名映射到属性；同时接受常见中文写法。
var attrAliases = map[string]Attribute{
	"title": AttrTitle, "name": AttrTitle, "answer": AttrTitle, "guess": AttrTitle, "song": AttrTitle,
	"歌名": AttrTitle, "曲名": AttrTitle, "标题": AttrTitle,
	"artist": AttrArtist, "author": AttrArtist, "singer": AttrArtist,
	"曲师": AttrArtist, "歌手": AttrArtist, "作者": AttrArtist,
	"genre": AttrGenre, "genres": AttrGenre, "流派": AttrGenre, "曲风": AttrGenre,
	"type": AttrType, "类型": AttrType, "谱面类型": AttrType,
	"version": AttrVersion, "ver": AttrVersion, "版本": AttrVersion,
	"bpm": AttrBPM, "speed": AttrBPM, "速度": AttrBPM,
	"const": AttrConst, "ds": AttrConst, "constant": AttrConst,
	"定数": AttrConst, "常数": AttrConst,
	"break": AttrBreak, "brakes": AttrBreak, "breaks": AttrBreak,
	"绝赞": AttrBreak, "绝赞数": AttrBreak, "赞": AttrBreak,
}

// ParseAttribute 解析属性名（英文或中文），无法识别时 ok 为 false。
func ParseAttribute(s string) (Attribute, bool) {
	a, ok := attrAliases[strings.ToLower(strings.TrimSpace(s))]
	return a, ok
}

// Probe 一次针对某个属性的猜测及其结果。
type Probe struct {
	Attr   Attribute
	Raw    string // 玩家原始输入
	Value  string // 归一化后的展示值
	Mark   Mark
	Dir    Dir
	Solved bool // 是否为命中谜底曲名的猜测
}

// 数值属性的「接近」阈值。
const (
	// bpmCloseRatio / bpmCloseAbs 是 BPM 判为接近的相对 / 绝对阈值。
	bpmCloseRatio = 0.05
	bpmCloseAbs   = 5
	// dsCloseDiff 是定数判为接近的最大差值；dsEqualDiff 内视为完全相同。
	dsCloseDiff = 0.5
	dsEqualDiff = 0.05
	// breakCloseDiff 是绝赞数判为接近的最大差值。
	breakCloseDiff = 2
)

// MakeProbe 校验并计算一次属性猜测；返回 [ErrBadValue] 表示输入的取值非法。
func MakeProbe(target Track, attr Attribute, raw string) (Probe, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Probe{}, ErrBadValue
	}
	pr := Probe{Attr: attr, Raw: raw, Value: raw}

	switch attr {
	case AttrTitle:
		pr.Mark = titleMark(target, raw)
		pr.Solved = pr.Mark == Match
	case AttrArtist:
		pr.Mark = nameMark(normalizeTitle(target.Artist), normalizeTitle(raw))
	case AttrGenre:
		pr.Mark = textMark(normalizeTitle(target.Genre), normalizeTitle(raw))
	case AttrType:
		ty := strings.ToUpper(raw)
		if ty != "SD" && ty != "DX" {
			return Probe{}, ErrBadValue
		}
		pr.Value = ty
		pr.Mark = textMark(strings.ToUpper(target.Type), ty)
	case AttrVersion:
		pr.Mark = versionMark(target.Version, raw)
	case AttrBPM:
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return Probe{}, ErrBadValue
		}
		pr.Value = strconv.Itoa(n)
		if target.BPM > 0 {
			t, g := float64(target.BPM), float64(n)
			pr.Mark, pr.Dir = numberMark(t, g, max(float64(bpmCloseAbs), t*bpmCloseRatio), 0)
		}
	case AttrConst:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil || f <= 0 {
			return Probe{}, ErrBadValue
		}
		pr.Value = formatDS(f)
		pr.Mark, pr.Dir = dsMark(target.MasDS, f)
	case AttrBreak:
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return Probe{}, ErrBadValue
		}
		pr.Value = strconv.Itoa(n)
		pr.Mark, pr.Dir = numberMark(float64(target.MasBreak), float64(n), breakCloseDiff, 0)
	default:
		return Probe{}, ErrBadValue
	}
	return pr, nil
}

// ProbeNumber 返回数值型猜测的数值；非数值属性返回 0, false。
func ProbeNumber(attr Attribute, value string) (float64, bool) {
	if !attr.Numeric() {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// titleMark 判定曲名猜测：命中曲名或别名 → 绿；部分包含 → 黄；否则 → 灰。
func titleMark(target Track, guess string) Mark {
	if target.Matches(guess) {
		return Match
	}
	g := normalizeTitle(guess)
	if g == "" {
		return Miss
	}
	if containsEither(target.Key(), g) {
		return Close
	}
	for _, a := range target.Aliases {
		if containsEither(normalizeTitle(a), g) {
			return Close
		}
	}
	return Miss
}

func containsEither(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.Contains(a, b) || strings.Contains(b, a)
}

// numberMark 比较谜底与猜测的数值：差值在 equalWithin 内视为完全相同，在 closeWithin
// 内视为接近，否则不匹配；并给出谜底相对猜测的方向（相等时为 DirNone）。
func numberMark(target, guess, closeWithin, equalWithin float64) (Mark, Dir) {
	diff := math.Abs(target - guess)
	dir := DirNone
	switch {
	case target > guess:
		dir = DirUp
	case target < guess:
		dir = DirDown
	}
	switch {
	case diff <= equalWithin:
		return Match, DirNone
	case diff <= closeWithin:
		return Close, dir
	default:
		return Miss, dir
	}
}

// dsMark 比较 MASTER 定数；任一侧缺失（<= 0）时不作比较。
func dsMark(target, guess float64) (Mark, Dir) {
	if target <= 0 || guess <= 0 {
		return Miss, DirNone
	}
	return numberMark(target, guess, dsCloseDiff, dsEqualDiff)
}

// textMark 比较两个已归一化的文本：相等 → 绿，互相包含 → 黄，否则 → 灰。
func textMark(target, guess string) Mark {
	if target == "" || guess == "" {
		return Miss
	}
	if target == guess {
		return Match
	}
	if containsEither(target, guess) {
		return Close
	}
	return Miss
}

// nameMark 比较创作者名：完全一致 / 包含为黄，此外若共享较长的词元也算接近。
func nameMark(target, guess string) Mark {
	if m := textMark(target, guess); m != Miss {
		return m
	}
	if shareToken(target, guess) {
		return Close
	}
	return Miss
}

// shareToken 报告两段文本是否共享一个长度 >= 2 的词元（按非字母数字切分）。
func shareToken(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	set := make(map[string]struct{}, 4)
	for _, tok := range splitTokens(a) {
		set[tok] = struct{}{}
	}
	for _, tok := range splitTokens(b) {
		if _, ok := set[tok]; ok {
			return true
		}
	}
	return false
}

func splitTokens(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) >= 2 {
			out = append(out, string(cur))
		}
		cur = cur[:0]
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur = append(cur, r)
			continue
		}
		flush()
	}
	flush()
	return out
}

// versionMark 比较版本：完全一致 → 绿；同一大版本（去掉尾部数字）或互相包含 → 黄。
func versionMark(target, guess string) Mark {
	t, g := normalizeTitle(target), normalizeTitle(guess)
	if t == "" || g == "" {
		return Miss
	}
	if t == g {
		return Match
	}
	if tm, gm := versionMajor(t), versionMajor(g); tm != "" && tm == gm {
		return Close
	}
	if containsEither(t, g) {
		return Close
	}
	return Miss
}

// versionMajor 去掉尾部数字与空白，得到「舞萌DX」「maimai」这类大版本前缀。
func versionMajor(s string) string {
	rs := []rune(s)
	end := len(rs)
	for end > 0 && (unicode.IsDigit(rs[end-1]) || unicode.IsSpace(rs[end-1])) {
		end--
	}
	return string(rs[:end])
}

// formatDS 把定数格式化为展示文本；<= 0 表示该难度不存在，展示为占位符。
func formatDS(v float64) string {
	if v <= 0 {
		return "—"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
