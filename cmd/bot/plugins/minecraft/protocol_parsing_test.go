package minecraft

import (
	"image/color"
	"io"
	"net"
	"testing"
	"time"
)

// ─── readPacket 长度字段加固 ──────────────────────────────────────────────────

// scriptedConn 只提供 Read 的最小 net.Conn 实现：依次返回预设字节，随后 EOF。
type scriptedConn struct{ data []byte }

func (c *scriptedConn) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.data)
	c.data = c.data[n:]
	return n, nil
}

func (c *scriptedConn) Write(p []byte) (int, error)        { return len(p), nil }
func (c *scriptedConn) Close() error                       { return nil }
func (c *scriptedConn) LocalAddr() net.Addr                { return dummyAddr{} }
func (c *scriptedConn) RemoteAddr() net.Addr               { return dummyAddr{} }
func (c *scriptedConn) SetDeadline(t time.Time) error      { return nil }
func (c *scriptedConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *scriptedConn) SetWriteDeadline(t time.Time) error { return nil }

type dummyAddr struct{}

func (dummyAddr) Network() string { return "test" }
func (dummyAddr) String() string  { return "test" }

// TestReadPacketRejectsInvalidLength 回归：长度字段溢出为负数或超长时，
// make([]byte, length) 会 panic（makeslice: len out of range），而该 panic 经
// singleflight 会传播到同 key 的并发等待者。必须返回错误而不是 panic。
func TestReadPacketRejectsInvalidLength(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
	}{
		// 9 个续字节 + 0x7f：移位到 63 位，结果为负 → 历史上直接 panic
		{name: "溢出为负", payload: []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x7f}},
		{name: "varint 过长", payload: []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}},
		{name: "超过包大小上限", payload: []byte{0xff, 0xff, 0xff, 0xff, 0x0f}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("readPacket 不应 panic, got %v", r)
				}
			}()
			if _, err := readPacket(&scriptedConn{data: c.payload}); err == nil {
				t.Error("异常长度字段应返回错误")
			}
		})
	}
}

// TestReadPacketAcceptsNormalPacket 确保加固没有影响正常包读取。
func TestReadPacketAcceptsNormalPacket(t *testing.T) {
	data, err := readPacket(&scriptedConn{data: []byte{0x03, 'a', 'b', 'c'}})
	if err != nil {
		t.Fatalf("readPacket: %v", err)
	}
	if string(data) != "abc" {
		t.Errorf("payload = %q", data)
	}
	// 长度合法但载荷被截断 → EOF 错误（不应 panic）
	if _, err := readPacket(&scriptedConn{data: []byte{0x05, 'a'}}); err == nil {
		t.Error("载荷不足应返回错误")
	}
}

// ─── 握手主机名 ──────────────────────────────────────────────────────────────

// startHandshakeCaptureServer 启动一个记录握手包中 Server Address 字段的伪造
// SLP 服务器，并通过 statusJSON 返回状态。
func startHandshakeCaptureServer(t *testing.T, hostCh chan<- string, statusJSON string) (port int, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(4 * time.Second))

				data, rerr := readPacket(c)
				if rerr != nil {
					return
				}
				r := &packetReader{data: data}
				if _, err := r.readVarInt(); err != nil { // packet id
					return
				}
				if _, err := r.readVarInt(); err != nil { // protocol version
					return
				}
				host, err := r.readString() // Server Address
				if err != nil {
					return
				}
				hostCh <- host

				if _, err := readPacket(c); err != nil { // status request
					return
				}
				payload := (&packetBuffer{}).withString(statusJSON)
				_ = sendPacket(c, 0x00, payload)
			}(conn)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, func() { _ = ln.Close() }
}

// TestPingJavaSRVKeepsOriginalHostInHandshake 回归：SRV 解析后握手包曾发送解析
// 结果（dialHost），而协议要求发送客户端实际使用的主机名；BungeeCord/Velocity
// 的虚拟主机路由（forced-host）依据该字段决定响应内容。
func TestPingJavaSRVKeepsOriginalHostInHandshake(t *testing.T) {
	hostCh := make(chan string, 1)
	statusJSON := `{"version":{"name":"1.21.1","protocol":767},"players":{"max":10,"online":0},"description":"SRV"}`
	port, stop := startHandshakeCaptureServer(t, hostCh, statusJSON)
	defer stop()

	// 预置 SRV 结果，避免测试依赖真实 DNS
	srvCache.set("tcp|mc.srv-test.example", srvEntry{host: "127.0.0.1", port: port})

	status, err := PingJava("mc.srv-test.example", 0, 4*time.Second)
	if err != nil {
		t.Fatalf("PingJava: %v", err)
	}

	select {
	case got := <-hostCh:
		if got != "mc.srv-test.example" {
			t.Errorf("握手 Server Address = %q，期望原始主机名", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("未捕获握手包")
	}

	// 实际连接目标仍是 SRV 解析结果
	if status.Host != "127.0.0.1" || status.Port != port {
		t.Errorf("解析后地址 = %s:%d，期望 127.0.0.1:%d", status.Host, status.Port, port)
	}
}

// ─── API MOTD 颜色 ──────────────────────────────────────────────────────────

// TestApplyAPIMOTDPreservesRawColors 回归：clean 曾无条件覆盖 MOTD，
// 导致走 API 的查询（direct_query=false 或直连失败回退）颜色全部退化为白色。
func TestApplyAPIMOTDPreservesRawColors(t *testing.T) {
	st := &MCServerStatus{}
	applyAPIMOTD(st, []string{"\u00a7aGreen \u00a7bAqua", "\u00a7cRed"}, []string{"Green Aqua", "Red"})

	if st.MOTDPlain != "Green Aqua" {
		t.Errorf("MOTDPlain = %q", st.MOTDPlain)
	}
	if st.SubMOTDPlain != "Red" {
		t.Errorf("SubMOTDPlain = %q", st.SubMOTDPlain)
	}
	if len(st.MOTD) != 2 {
		t.Fatalf("MOTD 分段数 = %d，期望 2（raw 的两段颜色）", len(st.MOTD))
	}
	wantGreen := color.RGBA{R: 85, G: 255, B: 85, A: 255}
	if st.MOTD[0].Color != wantGreen {
		t.Errorf("MOTD[0] 颜色 = %v，期望 %v（raw 颜色码不应被 clean 覆盖）", st.MOTD[0].Color, wantGreen)
	}
	wantAqua := color.RGBA{R: 85, G: 255, B: 255, A: 255}
	if st.MOTD[1].Color != wantAqua {
		t.Errorf("MOTD[1] 颜色 = %v，期望 %v", st.MOTD[1].Color, wantAqua)
	}
	wantRed := color.RGBA{R: 255, G: 85, B: 85, A: 255}
	if len(st.SubMOTD) != 1 || st.SubMOTD[0].Color != wantRed {
		t.Errorf("SubMOTD = %+v，期望单段红色", st.SubMOTD)
	}
}

// TestApplyAPIMOTDFallbackToClean 验证 raw 缺失时的兜底：用 clean 构造白色分段，
// 并在没有 clean 时用 raw 去掉颜色码填充纯文本。
func TestApplyAPIMOTDFallbackToClean(t *testing.T) {
	onlyClean := &MCServerStatus{}
	applyAPIMOTD(onlyClean, nil, []string{"Plain A", "Plain B"})
	if len(onlyClean.MOTD) != 1 || onlyClean.MOTD[0].Text != "Plain A" || onlyClean.MOTD[0].Color == nil {
		t.Errorf("raw 缺失时应由 clean 兜底: %+v", onlyClean.MOTD)
	}
	if len(onlyClean.SubMOTD) != 1 || onlyClean.SubMOTD[0].Text != "Plain B" {
		t.Errorf("次级 MOTD 兜底失败: %+v", onlyClean.SubMOTD)
	}

	onlyRaw := &MCServerStatus{}
	applyAPIMOTD(onlyRaw, []string{"\u00a7aColored"}, nil)
	if onlyRaw.MOTDPlain != "Colored" {
		t.Errorf("clean 缺失时 MOTDPlain 应由 raw 去码得到, got %q", onlyRaw.MOTDPlain)
	}
	if len(onlyRaw.MOTD) != 1 {
		t.Errorf("raw 分段数 = %d", len(onlyRaw.MOTD))
	}
}

// ─── 地址解析与端点构造 ─────────────────────────────────────────────────────

func TestParseHostPortExtras(t *testing.T) {
	cases := []struct {
		in   string
		host string
		port int
		err  bool
	}{
		// 裸 IPv6 的方括号形式（SplitHostPort 不带端口时会失败）
		{in: "[::1]", host: "::1"},
		{in: "[2001:db8::1]", host: "2001:db8::1"},
		// 粘贴完整 URL：路径与查询串应被丢弃
		{in: "https://mc.example.com:25565/status?a=1", host: "mc.example.com", port: 25565},
		{in: "mc.example.com/path", host: "mc.example.com"},
		{in: "tcp://mc.example.com:25565#frag", host: "mc.example.com", port: 25565},
	}
	for _, c := range cases {
		host, port, err := parseHostPort(c.in)
		if c.err {
			if err == nil {
				t.Errorf("parseHostPort(%q) 期望报错", c.in)
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

func TestNormalizeHostKey(t *testing.T) {
	cases := map[string]string{
		"MC.Example.COM":   "mc.example.com",
		"mc.example.com.":  "mc.example.com",
		"  mc.example.com": "mc.example.com",
		"127.0.0.1":        "127.0.0.1",
	}
	for in, want := range cases {
		if got := normalizeHostKey(in); got != want {
			t.Errorf("normalizeHostKey(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestResolveSRVCacheHit 验证 SRV 结果的 key 归一化（大小写与末尾根点不敏感）。
func TestResolveSRVCacheHit(t *testing.T) {
	srvCache.set("udp|mc.udp-cache.example", srvEntry{host: "127.0.0.1", port: 19133})
	host, port := resolveSRV("udp", "MC.UDP-CACHE.EXAMPLE.", DefaultBedrockPort)
	if host != "127.0.0.1" || port != 19133 {
		t.Errorf("resolveSRV = %s:%d，期望命中缓存 127.0.0.1:19133", host, port)
	}
}

// TestAPIEndpoint 验证未指定端口时省略端口，让 API 自行做 SRV 解析
// （显式写默认端口会跳过 SRV，把只配了 SRV 的服务器误判为离线）。
func TestAPIEndpoint(t *testing.T) {
	if got := apiEndpoint("3", "mc.example.com", 25565); got != "https://api.mcsrvstat.us/3/mc.example.com:25565" {
		t.Errorf("显式端口 = %q", got)
	}
	if got := apiEndpoint("3", "mc.example.com", 0); got != "https://api.mcsrvstat.us/3/mc.example.com" {
		t.Errorf("未指定端口应省略 = %q", got)
	}
	if got := apiEndpoint("bedrock/3", "play.example.com", 0); got != "https://api.mcsrvstat.us/bedrock/3/play.example.com" {
		t.Errorf("bedrock = %q", got)
	}
}

// ─── 服务端软件识别 ─────────────────────────────────────────────────────────

func TestGuessSoftwareFromVersionName(t *testing.T) {
	cases := map[string]string{
		"Paper 1.21.1":       "Paper",
		"Purpur 1.20.4":      "Purpur",
		"Velocity 1.2.3":     "Velocity",
		"Fabric 1.21":        "Fabric",
		"1.21.1-Purpur":      "Purpur",
		"paper 1.21.1":       "Paper", // 大小写不敏感，但按品牌规范输出
		"1.21.1":             "",      // 原版纯版本号
		"":                   "",
		"Some Custom 1.21.1": "",
	}
	for in, want := range cases {
		if got := guessSoftwareFromVersionName(in); got != want {
			t.Errorf("guessSoftwareFromVersionName(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestApplyGS4SoftwarePrecedence 验证 GS4 的服务端描述优先于版本名推断。
func TestApplyGS4SoftwarePrecedence(t *testing.T) {
	st := &MCServerStatus{Version: "Paper 1.21.1", Software: "Paper"}
	applyGS4(st, &GS4Stat{
		NumPlayers: 3,
		MaxPlayers: 20,
		Plugins:    "Paper on 1.21.1: WorldEdit, Essentials",
	})
	if st.Software != "Paper on 1.21.1" {
		t.Errorf("Software = %q，期望 GS4 的描述覆盖推断值", st.Software)
	}
	if st.PluginCount != 2 || len(st.PluginNames) != 2 {
		t.Errorf("插件清单 = %v (count %d)", st.PluginNames, st.PluginCount)
	}

	// GS4 未提供 plugins 时保留推断值（不覆盖、不清空）
	st2 := &MCServerStatus{Version: "Paper 1.21.1", Software: "Paper"}
	applyGS4(st2, &GS4Stat{NumPlayers: 1, MaxPlayers: 20})
	if st2.Software != "Paper" || st2.PluginCount != 0 {
		t.Errorf("Software = %q, PluginCount = %d", st2.Software, st2.PluginCount)
	}
}

// TestDisplaySoftwareDedup 验证服务端行不再与版本行重复。
func TestDisplaySoftwareDedup(t *testing.T) {
	dup := &MCServerStatus{Version: "Paper 1.21.1", Software: "Paper"}
	if got := displaySoftware(dup); got != "" {
		t.Errorf("版本行已含品牌且无插件时应省略, got %q", got)
	}
	withPlugins := &MCServerStatus{Version: "Paper 1.21.1", Software: "Paper", PluginCount: 3}
	if got := displaySoftware(withPlugins); got != "Paper（3 个插件）" {
		t.Errorf("displaySoftware = %q", got)
	}
	extra := &MCServerStatus{Version: "1.21.1", Software: "Paper"}
	if got := displaySoftware(extra); got != "Paper" {
		t.Errorf("版本行不含品牌时应展示, got %q", got)
	}
}
