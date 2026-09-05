package minecraft

import (
	"strings"
	"testing"
	"time"
)

func TestParseHostPort(t *testing.T) {
	cases := []struct {
		in   string
		host string
		port int
		err  bool
	}{
		{in: "mc.hypixel.net", host: "mc.hypixel.net", port: 0},
		{in: "mc.hypixel.net:25565", host: "mc.hypixel.net", port: 25565},
		{in: " mc.example.com:19132 ", host: "mc.example.com", port: 19132},
		// IPv6
		{in: "[::1]:25565", host: "::1", port: 25565},
		{in: "[2001:db8::1]:19132", host: "2001:db8::1", port: 19132},
		{in: "2001:db8::1", host: "2001:db8::1", port: 0},
		{in: "::1", host: "::1", port: 0},
		// scheme 前缀剥离
		{in: "https://mc.example.com:25565", host: "mc.example.com", port: 25565},
		{in: "http://mc.example.com", host: "mc.example.com", port: 0},
		// 错误路径
		{in: "", err: true},
		{in: "   ", err: true},
		{in: "host:abc", err: true},
		{in: "host:0", err: true},
		{in: "host:70000", err: true},
		{in: ":25565", err: true},
	}
	for _, c := range cases {
		host, port, err := parseHostPort(c.in)
		if c.err {
			if err == nil {
				t.Errorf("parseHostPort(%q) = %q,%d, 期望报错", c.in, host, port)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseHostPort(%q) 报错: %v", c.in, err)
			continue
		}
		if host != c.host || port != c.port {
			t.Errorf("parseHostPort(%q) = %q,%d；期望 %q,%d", c.in, host, port, c.host, c.port)
		}
	}
}

func TestTTLCache(t *testing.T) {
	c := newTTLCache[string](50*time.Millisecond, 3)

	if _, ok := c.get("a"); ok {
		t.Fatal("空缓存不应命中")
	}
	c.set("a", "1")
	if v, ok := c.get("a"); !ok || v != "1" {
		t.Fatalf("get(a) = %q,%v", v, ok)
	}

	// 过期
	c.set("b", "2")
	time.Sleep(60 * time.Millisecond)
	if _, ok := c.get("a"); ok {
		t.Fatal("过期后不应命中")
	}

	// 容量淘汰：最早过期者先出
	c2 := newTTLCache[int](time.Minute, 2)
	c2.set("k1", 1)
	time.Sleep(2 * time.Millisecond)
	c2.set("k2", 2)
	time.Sleep(2 * time.Millisecond)
	c2.set("k3", 3)
	if _, ok := c2.get("k1"); ok {
		t.Fatal("容量超限后最早过期者应被淘汰")
	}
	if _, ok := c2.get("k2"); !ok {
		t.Fatal("较新条目不应被淘汰")
	}
	if _, ok := c2.get("k3"); !ok {
		t.Fatal("最新条目不应被淘汰")
	}
}

func TestDisplayPort(t *testing.T) {
	if got := displayPort(0, ""); got != DefaultJavaPort {
		t.Errorf("displayPort(0,\"\") = %d, 期望 %d", got, DefaultJavaPort)
	}
	if got := displayPort(0, "bedrock"); got != DefaultBedrockPort {
		t.Errorf("displayPort(0,\"bedrock\") = %d, 期望 %d", got, DefaultBedrockPort)
	}
	if got := displayPort(12345, "java"); got != 12345 {
		t.Errorf("displayPort(12345,\"java\") = %d, 期望 12345", got)
	}
}

func TestParseMotdUTF8Section(t *testing.T) {
	// § 为 UTF-8 双字节（0xC2 0xA7），按字节解析会混入杂散 0xC2
	segs := ParseMotd("§aHello §lWorld")
	if len(segs) != 2 {
		t.Fatalf("段数 = %d, 期望 2: %+v", len(segs), segs)
	}
	if segs[0].Text != "Hello " || segs[1].Text != "World" {
		t.Errorf("文本 = %q / %q", segs[0].Text, segs[1].Text)
	}
	if strings.ContainsRune(segs[0].Text, 0xC2) || strings.ContainsRune(segs[1].Text, 0xC2) {
		t.Error("文本中不应残留 0xC2 杂散字节")
	}
	if segs[1].Color != motdColors['a'] || segs[1].Bold != true {
		t.Errorf("颜色码应跨段生效且加粗保持: %+v", segs[1])
	}
	if segs[0].Bold != false {
		t.Errorf("首段不应加粗: %+v", segs[0])
	}
}

func TestParseMotdAmpersand(t *testing.T) {
	// & 后跟合法代码字符 → 作为颜色码
	segs := ParseMotd("&aGreen")
	if len(segs) != 1 || segs[0].Text != "Green" {
		t.Fatalf("got %+v", segs)
	}
	if segs[0].Color != motdColors['a'] {
		t.Errorf("&a 颜色未生效: %+v", segs[0])
	}
	// & 后跟非法字符 → 视为字面 &（不吞正文）
	segs = ParseMotd("A & B")
	if len(segs) != 1 || segs[0].Text != "A & B" {
		t.Fatalf("got %+v", segs)
	}
}

func TestParseMotdComponentWithCodes(t *testing.T) {
	// JSON 组件文本内嵌 § 码：颜色码应生效且不残留
	segs := parseTextComponents([]byte(`{"text":"§6[Hub] ","extra":[{"text":"§fWelcome","bold":true}]}`))
	if len(segs) != 2 {
		t.Fatalf("段数 = %d: %+v", len(segs), segs)
	}
	if segs[0].Text != "[Hub] " || segs[1].Text != "Welcome" {
		t.Errorf("文本 = %q / %q", segs[0].Text, segs[1].Text)
	}
	if segs[0].Color != motdColors['6'] {
		t.Errorf("§6 颜色未生效: %+v", segs[0])
	}
	// MC 语义：颜色码（§f）会重置加粗
	if segs[1].Color != motdColors['f'] || segs[1].Bold {
		t.Errorf("§f 应设白色并重置加粗: %+v", segs[1])
	}
	var sb strings.Builder
	for _, s := range segs {
		sb.WriteString(s.Text)
	}
	if strings.ContainsRune(sb.String(), '\u00a7') {
		t.Errorf("纯文本不应包含 §: %q", sb.String())
	}

	// 组件 bold 且文本不含颜色码 → 加粗应从组件继承
	segs = parseTextComponents([]byte(`{"text":"§6[Hub] ","extra":[{"text":"Welcome","bold":true}]}`))
	if len(segs) != 2 || !segs[1].Bold {
		t.Errorf("组件加粗未继承: %+v", segs)
	}
}

func TestStripMotdUTF8(t *testing.T) {
	if got := stripMotd("§aHello §lWorld"); got != "Hello World" {
		t.Errorf("stripMotd = %q", got)
	}
	if got := stripMotd("A & B"); got != "A & B" {
		t.Errorf("字面 & 不应被吞: %q", got)
	}
	if got := stripMotd("&&aX"); got != "&X" { // 第一个 & 后跟 & 非代码 → 字面；&a 为代码
		t.Errorf("stripMotd = %q", got)
	}
}

func TestSplitMotdLines(t *testing.T) {
	green := motdColors['a']
	segs := []MotdSegment{
		{Text: "Hypixel Network [1.8]\n", Color: green},
		{Text: " SB 0.27.1", Color: green, Bold: true},
	}
	line1, line2 := splitMotdLines(segs)
	if len(line1) != 1 || line1[0].Text != "Hypixel Network [1.8]" {
		t.Errorf("line1 = %+v", line1)
	}
	if len(line2) != 1 || line2[0].Text != " SB 0.27.1" || !line2[0].Bold {
		t.Errorf("line2 = %+v", line2)
	}

	// 换行后的所有内容都归第二行
	line1, line2 = splitMotdLines([]MotdSegment{
		{Text: "A\nB"},
		{Text: "C"},
	})
	if len(line1) != 1 || line1[0].Text != "A" {
		t.Errorf("line1 = %+v", line1)
	}
	if len(line2) != 2 || line2[0].Text != "B" || line2[1].Text != "C" {
		t.Errorf("line2 = %+v", line2)
	}
}

func TestParseJavaDescriptionMultiLine(t *testing.T) {
	status := &MCServerStatus{}
	parseJavaDescription([]byte(`{"text":"Line One\nLine Two","extra":[{"text":" More"}]}`), status)
	if status.MOTDPlain != "Line One\nLine Two More" {
		t.Errorf("MOTDPlain = %q", status.MOTDPlain)
	}
	if len(status.MOTD) != 1 || status.MOTD[0].Text != "Line One" {
		t.Errorf("MOTD = %+v", status.MOTD)
	}
	if len(status.SubMOTD) != 2 || status.SubMOTD[0].Text != "Line Two" {
		t.Errorf("SubMOTD = %+v", status.SubMOTD)
	}
	if status.SubMOTDPlain != "Line Two More" {
		t.Errorf("SubMOTDPlain = %q", status.SubMOTDPlain)
	}
}

func TestFormatMCTextOnline(t *testing.T) {
	status := &MCServerStatus{
		Online:   true,
		Host:     "mc.example.com",
		Port:     25565,
		Latency:  42 * time.Millisecond,
		Edition:  "java",
		Version:  "1.21.1",
		GameMode: "SMP",
		Map:      "world",
		Players: struct {
			Online int
			Max    int
			List   []PlayerInfo
		}{Online: 2, Max: 20, List: []PlayerInfo{{Name: "Steve"}, {Name: "Alex"}}},
	}
	out := formatMCText(status)
	for _, want := range []string{"模式: SMP", "地图: world", "玩家: 2 / 20", "Steve"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatMCText 缺少 %q:\n%s", want, out)
		}
	}
}

func TestFormatMCTextBedrockSubMOTD(t *testing.T) {
	status := &MCServerStatus{
		Online:       true,
		Host:         "play.example.com",
		Port:         19132,
		Edition:      "bedrock",
		MOTDPlain:    "Main MOTD",
		SubMOTDPlain: "Second Line",
		Version:      "1.21.0",
	}
	out := formatMCText(status)
	if !strings.Contains(out, "MOTD: Main MOTD") || !strings.Contains(out, "次级 MOTD: Second Line") {
		t.Errorf("formatMCText 缺少次级 MOTD:\n%s", out)
	}
}
