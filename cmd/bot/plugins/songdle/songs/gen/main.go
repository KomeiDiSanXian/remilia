// Command gen 从上游数据源生成 songdle 插件内置的曲库 JSON。
//
// 数据来源都是无需鉴权、且在持续维护的公开接口：
//   - 曲目信息：水鱼查分器 https://www.diving-fish.com/api/maimaidxprober/music_data
//   - 曲目别名：YuzuChaN maimaiDX https://www.yuzuchan.moe/api/v2/aliases/maimaidx/aliases
//
// 输出字段与 song.go 中的 rawTrack / rawAlias 一一对应，默认写到上一级 songs/ 目录。
// 用 -songs-src / -alias-src 可以指定本地缓存文件，便于离线复现某次生成结果。
//
// 用法（在 cmd/bot 目录下执行）：
//
//	go run ./plugins/songdle/songs/gen
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSongsURL = "https://www.diving-fish.com/api/maimaidxprober/music_data"
	defaultAliasURL = "https://www.yuzuchan.moe/api/v2/aliases/maimaidx/aliases"

	fetchTimeout = 60 * time.Second
	// maxID 是普通曲目的最大 ID；更大的 ID（≥ 100000）是「宴会場」谱面，
	// 曲名带 [xxx] 前缀、定数带 ?，不适合作为谜底，历代数据集都将其排除。
	maxID = 100000
	// userAgent 用于访问上游接口，避免被默认 UA 拦截。
	userAgent = "remilia-songdle-gen/1.0 (+https://github.com/KomeiDiSanXian/remilia)"
)

// dxVersionCN 把水鱼数据中的日文 DX 版本名映射为中文版本名，与既有数据集保持一致。
// 国服把「x.5」小版本并入年度版本，因此 PLUS 与本体共用一个中文名。
var dxVersionCN = map[string]string{
	"maimai でらっくす":               "舞萌DX",
	"maimai でらっくす PLUS":          "舞萌DX",
	"maimai でらっくす Splash":        "舞萌DX2021",
	"maimai でらっくす Splash PLUS":   "舞萌DX2021",
	"maimai でらっくす UNiVERSE":      "舞萌DX2022",
	"maimai でらっくす UNiVERSE PLUS": "舞萌DX2022",
	"maimai でらっくす FESTiVAL":      "舞萌DX2023",
	"maimai でらっくす FESTiVAL PLUS": "舞萌DX2023",
	"maimai でらっくす BUDDiES":       "舞萌DX2024",
	"maimai でらっくす BUDDiES PLUS":  "舞萌DX2024",
	"maimai でらっくす PRiSM":         "舞萌DX2025",
	"maimai でらっくす PRiSM PLUS":    "舞萌DX2026",
}

// dfMusic 是水鱼接口返回的单首曲目。
type dfMusic struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	Type      string      `json:"type"`
	DS        []float64   `json:"ds"`
	Level     []string    `json:"level"`
	Charts    []dfChart   `json:"charts"`
	BasicInfo dfBasicInfo `json:"basic_info"`
}

// dfChart 是单个难度的谱面信息；notes 依次为 tap / hold / slide / touch? / break，
// SD 谱面没有 touch，因此绝赞数取数组最后一位而不是固定下标。
type dfChart struct {
	Notes   []int  `json:"notes"`
	Charter string `json:"charter"`
}

// dfBasicInfo 是曲目基础信息。
type dfBasicInfo struct {
	Title  string  `json:"title"`
	Artist string  `json:"artist"`
	Genre  string  `json:"genre"`
	BPM    float64 `json:"bpm"`
	From   string  `json:"from"`
}

// yuzuAlias 是 YuzuChaN 别名接口返回的单条数据。
type yuzuAlias struct {
	SongID int      `json:"song_id"`
	Name   string   `json:"name"`
	Alias  []string `json:"alias"`
}

// songRecord 是写入 maimai.json 的单条记录，字段顺序与既有数据保持一致。
type songRecord struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	MasDS      float64 `json:"masds"`
	MasLevel   string  `json:"maslevel"`
	MasCharter string  `json:"mascharter"`
	MasBreak   int     `json:"masbreak"`
	ExpDS      float64 `json:"expds"`
	ExpLevel   string  `json:"explevel"`
	ExpCharter string  `json:"expcharter"`
	Type       string  `json:"type"`
	Artist     string  `json:"artist"`
	BPM        int     `json:"bpm"`
	Genre      string  `json:"genre"`
	Version    string  `json:"version"`
	RemCharter string  `json:"remcharter,omitempty"`
}

// aliasRecord 是写入 maimai_alias.json 的单条记录。
type aliasRecord struct {
	SongID int      `json:"SongID"`
	Name   string   `json:"Name"`
	Alias  []string `json:"Alias"`
}

func main() {
	songsSrc := flag.String("songs-src", "", "本地曲目 JSON（留空则请求水鱼接口）")
	aliasSrc := flag.String("alias-src", "", "本地别名 JSON（留空则请求 YuzuChaN 接口）")
	songsURL := flag.String("songs-url", defaultSongsURL, "曲目接口地址")
	aliasURL := flag.String("alias-url", defaultAliasURL, "别名接口地址")
	outDir := flag.String("out-dir", "", "输出目录（默认与本生成器同级的上一级 songs/）")
	flag.Parse()

	dir := *outDir
	if dir == "" {
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			fatal("无法定位生成器源码路径，请用 -out-dir 指定输出目录")
		}
		dir = filepath.Dir(filepath.Dir(file))
	}

	dfRaw, err := load(*songsSrc, *songsURL)
	if err != nil {
		fatal("加载曲目数据失败: %v", err)
	}
	var music []dfMusic
	if err := json.Unmarshal(dfRaw, &music); err != nil {
		fatal("解析曲目数据失败: %v", err)
	}
	aliasRaw, err := load(*aliasSrc, *aliasURL)
	if err != nil {
		fatal("加载别名数据失败: %v", err)
	}
	var aliases []yuzuAlias
	if err := json.Unmarshal(aliasRaw, &aliases); err != nil {
		fatal("解析别名数据失败: %v", err)
	}

	records, skipped := convertSongs(music)
	if len(records) == 0 {
		fatal("转换后曲库为空")
	}
	aliasRecords := convertAliases(aliases, records)

	if err := writeSongs(filepath.Join(dir, "maimai.json"), records); err != nil {
		fatal("写入 maimai.json 失败: %v", err)
	}
	if err := writeJSON(filepath.Join(dir, "maimai_alias.json"), aliasRecords); err != nil {
		fatal("写入 maimai_alias.json 失败: %v", err)
	}
	fmt.Printf("完成：曲目 %d 首（跳过宴会場 %d 首），别名 %d 条 -> %s\n",
		len(records), skipped, len(aliasRecords), dir)
}

// load 优先读取本地文件，否则从 url 下载。
func load(file, url string) ([]byte, error) {
	if file = strings.TrimSpace(file); file != "" {
		return os.ReadFile(file)
	}
	client := &http.Client{Timeout: fetchTimeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s 返回 HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// convertSongs 把水鱼曲目转换成插件格式，按数字 ID 升序返回。
func convertSongs(music []dfMusic) ([]songRecord, int) {
	records := make([]songRecord, 0, len(music))
	skipped := 0
	for _, m := range music {
		id, err := strconv.Atoi(strings.TrimSpace(m.ID))
		if err != nil || id <= 0 || id > maxID {
			skipped++
			continue
		}
		if strings.TrimSpace(m.Title) == "" || len(m.DS) < 4 || len(m.Level) < 4 || len(m.Charts) < 4 {
			skipped++
			continue
		}
		rec := songRecord{
			ID:         m.ID,
			Title:      m.Title,
			ExpDS:      m.DS[2],
			ExpLevel:   m.Level[2],
			ExpCharter: m.Charts[2].Charter,
			MasDS:      m.DS[3],
			MasLevel:   m.Level[3],
			MasCharter: m.Charts[3].Charter,
			MasBreak:   lastNote(m.Charts[3].Notes),
			Type:       m.Type,
			Artist:     m.BasicInfo.Artist,
			BPM:        int(math.Round(m.BasicInfo.BPM)),
			Genre:      m.BasicInfo.Genre,
			Version:    localizeVersion(m.BasicInfo.From),
		}
		if len(m.DS) >= 5 {
			rec.RemCharter = m.Charts[4].Charter
		}
		records = append(records, rec)
	}
	sort.Slice(records, func(i, j int) bool { return lessID(records[i].ID, records[j].ID) })
	return records, skipped
}

// convertAliases 把 YuzuChaN 别名转换成插件格式，只保留曲库中存在的曲目，按 ID 升序返回。
func convertAliases(aliases []yuzuAlias, songs []songRecord) []aliasRecord {
	known := make(map[string]struct{}, len(songs))
	for _, s := range songs {
		known[s.ID] = struct{}{}
	}
	out := make([]aliasRecord, 0, len(aliases))
	for _, a := range aliases {
		id := strconv.Itoa(a.SongID)
		if _, ok := known[id]; !ok {
			continue
		}
		cleaned := dedupe(a.Alias)
		if len(cleaned) == 0 {
			continue
		}
		out = append(out, aliasRecord{SongID: a.SongID, Name: a.Name, Alias: cleaned})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SongID < out[j].SongID })
	return out
}

// localizeVersion 把日文 DX 版本名映射为中文版本名，其他版本名原样返回。
func localizeVersion(from string) string {
	if v, ok := dxVersionCN[from]; ok {
		return v
	}
	return from
}

// lastNote 返回 notes 数组最后一位（绝赞数）；空数组返回 0。
func lastNote(notes []int) int {
	if len(notes) == 0 {
		return 0
	}
	return notes[len(notes)-1]
}

// dedupe 去空白、按小写去重后保持原有顺序返回别名。
func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		k := strings.ToLower(strings.Join(strings.Fields(s), ""))
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, s)
	}
	return out
}

// lessID 按数字大小比较两个 ID 字符串。
func lessID(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}

// writeSongs 以「数字 ID 升序」写出曲目对象，与既有文件的排版保持一致。
func writeSongs(path string, records []songRecord) error {
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, rec := range records {
		body, err := indentJSON(rec, "    ", "    ")
		if err != nil {
			return err
		}
		buf.WriteString("    ")
		buf.WriteString(strconv.Quote(rec.ID))
		buf.WriteString(": ")
		buf.Write(body)
		if i < len(records)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// writeJSON 以四空格缩进写出任意 JSON 值。
func writeJSON(path string, v any) error {
	body, err := indentJSON(v, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}

// indentJSON 序列化 JSON 且不转义 HTML 字符、保留原始 Unicode。
func indentJSON(v any, prefix, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent(prefix, indent)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// fatal 打印错误并退出。
func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen: "+format+"\n", args...)
	os.Exit(1)
}
