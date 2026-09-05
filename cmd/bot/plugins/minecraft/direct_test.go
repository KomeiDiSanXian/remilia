package minecraft

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net"
	"testing"
	"time"
)

// ─── Java SLP 仿真服务器 ──────────────────────────────────────────────────────

// startFakeSLPServer 启动一个最小化的 Minecraft Server List Ping 服务器，
// 用于端到端验证 PingJava 的握手、状态响应解析与 ping/pong 往返。
func startFakeSLPServer(t *testing.T, statusJSON string) (port int, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSLPConn(conn, statusJSON)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, func() { _ = ln.Close() }
}

func handleSLPConn(conn net.Conn, statusJSON string) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	readPacketID := func() (int, bool) {
		data, err := readPacket(conn)
		if err != nil {
			return 0, false
		}
		pid, err := (&packetReader{data: data}).readVarInt()
		if err != nil {
			return 0, false
		}
		return pid, true
	}

	// 1. 握手包（0x00: 协议版本 + 主机 + 端口 + 下一状态）
	if pid, ok := readPacketID(); !ok || pid != 0x00 {
		return
	}

	// 2. 状态请求（0x00，空载荷）
	if pid, ok := readPacketID(); !ok || pid != 0x00 {
		return
	}

	// 3. 状态响应：0x00 + JSON 字符串
	payload := (&packetBuffer{}).withString(statusJSON)
	if err := sendPacket(conn, 0x00, payload); err != nil {
		return
	}

	// 4. ping（0x01 + 8 字节）→ 回显 pong
	pingData, err := readPacket(conn)
	if err != nil {
		return
	}
	pr := &packetReader{data: pingData}
	pid, err := pr.readVarInt()
	if err != nil || pid != 0x01 {
		return
	}
	nonce, err := pr.readInt64()
	if err != nil {
		return
	}
	pong := &packetBuffer{}
	pong.writeInt64(nonce)
	_ = sendPacket(conn, 0x01, pong.bytes())
}

func (p *packetBuffer) withString(s string) []byte {
	p.writeString(s)
	return p.bytes()
}

func tinyFaviconDataURI(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// TestPingJavaFakeServer 端到端验证 Java SLP 直连协议实现。
func TestPingJavaFakeServer(t *testing.T) {
	statusJSON := `{
		"version": {"name": "1.21.1", "protocol": 767},
		"players": {"max": 100, "online": 3, "sample": [
			{"name": "Steve", "id": "a1"},
			{"name": "Alex", "id": "a2"},
			{"name": "Notch", "id": "a3"}
		]},
		"description": {"text": "§aHello ", "extra": [{"text": "§lWorld", "color": "yellow"}]},
		"favicon": "` + tinyFaviconDataURI(t) + `"
	}`
	port, stop := startFakeSLPServer(t, statusJSON)
	defer stop()

	status, err := PingJava("127.0.0.1", port, 5*time.Second)
	if err != nil {
		t.Fatalf("PingJava: %v", err)
	}

	if !status.Online || status.Edition != "java" {
		t.Errorf("Online/Edition = %v/%q", status.Online, status.Edition)
	}
	if status.Version != "1.21.1" || status.Protocol != 767 {
		t.Errorf("Version/Protocol = %q/%d", status.Version, status.Protocol)
	}
	if status.Players.Online != 3 || status.Players.Max != 100 || len(status.Players.List) != 3 {
		t.Errorf("Players = %d/%d (list %d)", status.Players.Online, status.Players.Max, len(status.Players.List))
	}
	if status.Players.List[0].Name != "Steve" {
		t.Errorf("Player[0] = %q", status.Players.List[0].Name)
	}
	if status.MOTDPlain != "Hello World" {
		t.Errorf("MOTDPlain = %q", status.MOTDPlain)
	}
	// 颜色与加粗解析
	if len(status.MOTD) < 2 || status.MOTD[0].Color == nil {
		t.Errorf("MOTD segments = %+v", status.MOTD)
	}
	boldSeg := status.MOTD[len(status.MOTD)-1]
	if !boldSeg.Bold {
		t.Errorf("末段应为加粗, got %+v", boldSeg)
	}
	if len(status.Favicon) == 0 {
		t.Error("favicon 未解析")
	}
	if status.Latency < 0 {
		t.Errorf("Latency = %v", status.Latency)
	}
}

// ─── Bedrock RakNet 仿真服务器 ────────────────────────────────────────────────

func startFakeRakNetServer(t *testing.T, infoStr string) (port int, stop func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		buf := make([]byte, 512)
		for {
			n, ra, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 25 || buf[0] != 0x01 {
				continue
			}
			// Unconnected Pong (0x1c)：回显时间(8) + 魔数(16) + GUID(8) + 字符串
			pong := make([]byte, 0, 35+len(infoStr))
			pong = append(pong, 0x1c)
			pong = append(pong, buf[1:9]...)
			pong = append(pong, 0x00, 0xff, 0xff, 0x00, 0xfe, 0xfe, 0xfe, 0xfe, 0xfd, 0xfd, 0xfd, 0xfd, 0x12, 0x34, 0x56, 0x78)
			pong = append(pong, 0, 0, 0, 0, 0, 0, 0, 1)
			pong = append(pong, byte(len(infoStr)>>8), byte(len(infoStr)))
			pong = append(pong, infoStr...)
			if _, err := pc.WriteTo(pong, ra); err != nil {
				return
			}
		}
	}()
	return pc.LocalAddr().(*net.UDPAddr).Port, func() { _ = pc.Close() }
}

// TestPingBedrockFakeServer 端到端验证 Bedrock RakNet 直连协议实现。
func TestPingBedrockFakeServer(t *testing.T) {
	// 0 GameName | 1 MOTD | 2 协议 | 3 版本 | 4 在线 | 5 最大 | 6 GUID | 7 次级 MOTD | 8 模式
	info := "MCPE;A Bedrock Server;729;1.21.0;5;20;1234;Second Line;Survival;1;19132;19133;"
	port, stop := startFakeRakNetServer(t, info)
	defer stop()

	status, err := PingBedrock("127.0.0.1", port, 5*time.Second)
	if err != nil {
		t.Fatalf("PingBedrock: %v", err)
	}

	if !status.Online || status.Edition != "bedrock" {
		t.Errorf("Online/Edition = %v/%q", status.Online, status.Edition)
	}
	if status.MOTDPlain != "A Bedrock Server" {
		t.Errorf("MOTDPlain = %q", status.MOTDPlain)
	}
	if status.SubMOTDPlain != "Second Line" {
		t.Errorf("SubMOTDPlain = %q", status.SubMOTDPlain)
	}
	if status.Version != "1.21.0" || status.Protocol != 729 {
		t.Errorf("Version/Protocol = %q/%d", status.Version, status.Protocol)
	}
	if status.Players.Online != 5 || status.Players.Max != 20 {
		t.Errorf("Players = %d/%d", status.Players.Online, status.Players.Max)
	}
	if status.GameMode != "Survival" {
		t.Errorf("GameMode = %q", status.GameMode)
	}
	if status.Latency < 0 {
		t.Errorf("Latency = %v", status.Latency)
	}
}
