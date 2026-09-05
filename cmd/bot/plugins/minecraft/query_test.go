package minecraft

import (
	"context"
	"net"
	"testing"
	"time"
)

// buildFullStatPayload 构造一段 GS4 全量统计响应（不含头部类型/会话字段）。
func buildFullStatPayload() []byte {
	var b []byte
	b = append(b, []byte(queryMagicSplitnum)...)
	b = append(b, 0x00, 0x80, 0x00)
	kv := "hostname\x00My Server\x00gametype\x00SMP\x00game_id\x00MINECRAFT\x00" +
		"version\x001.21.1\x00plugins\x00\x00map\x00world\x00" +
		"numplayers\x002\x00maxplayers\x0010\x00hostport\x0025565\x00hostip\x00127.0.0.1\x00"
	b = append(b, kv...)
	b = append(b, 0x00, 0x00)       // KV 段结束（双 \0）
	b = append(b, 0x00, 0x00, 0x00) // 填充
	b = append(b, 0x01)             // 玩家段标记
	b = append(b, queryMagicPlayer...)
	b = append(b, 0x00, 0x00)
	b = append(b, "Steve\x00Alex\x00"...)
	b = append(b, 0x00) // 玩家列表结束
	return b
}

func TestParseGS4Stat(t *testing.T) {
	// 前置：0x00 类型 + 4 字节会话 ID（解析器以魔数定位，应忽略前置内容）
	data := append([]byte{0x00, 0x01, 0x02, 0x03, 0x04}, buildFullStatPayload()...)

	stat, err := parseGS4Stat(data)
	if err != nil {
		t.Fatalf("parseGS4Stat: %v", err)
	}
	if stat.Hostname != "My Server" {
		t.Errorf("Hostname = %q", stat.Hostname)
	}
	if stat.GameType != "SMP" {
		t.Errorf("GameType = %q", stat.GameType)
	}
	if stat.Version != "1.21.1" {
		t.Errorf("Version = %q", stat.Version)
	}
	if stat.Map != "world" {
		t.Errorf("Map = %q", stat.Map)
	}
	if stat.NumPlayers != 2 || stat.MaxPlayers != 10 {
		t.Errorf("Players = %d/%d", stat.NumPlayers, stat.MaxPlayers)
	}
	if stat.Port != 25565 {
		t.Errorf("Port = %d", stat.Port)
	}
	if len(stat.Players) != 2 || stat.Players[0] != "Steve" || stat.Players[1] != "Alex" {
		t.Errorf("Players list = %v", stat.Players)
	}
}

func TestParseGS4StatEmptyPlayers(t *testing.T) {
	var b []byte
	b = append(b, []byte(queryMagicSplitnum)...)
	b = append(b, 0x00, 0x80, 0x00)
	b = append(b, "numplayers\x000\x00maxplayers\x0020\x00"...)
	b = append(b, 0x00, 0x00)
	b = append(b, 0x00, 0x00, 0x00, 0x00)
	b = append(b, 0x01)
	b = append(b, queryMagicPlayer...)
	b = append(b, 0x00, 0x00)
	b = append(b, 0x00)

	stat, err := parseGS4Stat(b)
	if err != nil {
		t.Fatalf("parseGS4Stat: %v", err)
	}
	if stat.NumPlayers != 0 || stat.MaxPlayers != 20 {
		t.Errorf("Players = %d/%d", stat.NumPlayers, stat.MaxPlayers)
	}
	if len(stat.Players) != 0 {
		t.Errorf("期望空玩家列表, got %v", stat.Players)
	}
}

func TestParseGS4StatMissingMagic(t *testing.T) {
	if _, err := parseGS4Stat([]byte{0x00, 0x01, 0x02}); err == nil {
		t.Fatal("缺少 splitnum 魔数时应报错")
	}
}

func TestApplyGS4(t *testing.T) {
	status := &MCServerStatus{
		Online:  true,
		Edition: "java",
		Version: "1.21.1",
		Players: struct {
			Online, Max int
			List        []PlayerInfo
		}{Online: 2, Max: 100, List: []PlayerInfo{{Name: "Sample"}}},
		MOTDPlain: "SLP MOTD",
	}
	gs4 := &GS4Stat{
		GameType:   "SMP",
		Map:        "world",
		NumPlayers: 8,
		MaxPlayers: 100,
		Players:    []string{"Steve", "Alex", "Notch", "Herobrine"},
		Hostname:   "§aMy §bServer",
	}
	applyGS4(status, gs4)

	if status.Players.Online != 8 {
		t.Errorf("Online = %d, 期望 8", status.Players.Online)
	}
	if len(status.Players.List) != 4 {
		t.Errorf("List 数量 = %d, 期望 4", len(status.Players.List))
	}
	if status.Version != "1.21.1" { // SLP 已有版本，不覆盖
		t.Errorf("Version = %q, 期望保留 SLP 版本", status.Version)
	}
	if status.GameMode != "SMP" {
		t.Errorf("GameMode = %q", status.GameMode)
	}
	if status.Map != "world" {
		t.Errorf("Map = %q", status.Map)
	}
	if status.MOTDPlain != "SLP MOTD" { // SLP 已有 MOTD，不覆盖
		t.Errorf("MOTDPlain = %q, 期望保留 SLP MOTD", status.MOTDPlain)
	}
}

// TestQueryGS4Local 用本地 UDP 仿真服务器验证完整 GS4 握手 + 全量查询流程。
func TestQueryGS4Local(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer pc.Close()

	go func() {
		buf := make([]byte, 128)
		for {
			n, ra, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 7 {
				continue
			}
			switch buf[2] {
			case 0x09: // 握手：0x09 + 会话 ID + 挑战令牌
				resp := []byte{0x09, buf[3], buf[4], buf[5], buf[6], 0xAA, 0xBB, 0xCC, 0xDD}
				if _, err := pc.WriteTo(resp, ra); err != nil {
					return
				}
			case 0x00: // 全量统计
				resp := append([]byte{0x00, buf[3], buf[4], buf[5], buf[6]}, buildFullStatPayload()...)
				if _, err := pc.WriteTo(resp, ra); err != nil {
					return
				}
			}
		}
	}()

	port := pc.LocalAddr().(*net.UDPAddr).Port
	stat, err := QueryGS4(context.Background(), "127.0.0.1", port, 3*time.Second)
	if err != nil {
		t.Fatalf("QueryGS4: %v", err)
	}
	if stat.Hostname != "My Server" {
		t.Errorf("Hostname = %q", stat.Hostname)
	}
	if len(stat.Players) != 2 || stat.Players[0] != "Steve" {
		t.Errorf("Players = %v", stat.Players)
	}
}

// TestQueryGS4Timeout 无人应答时应超时报错且不 panic。
func TestQueryGS4Timeout(t *testing.T) {
	_, err := QueryGS4(context.Background(), "127.0.0.1", 1, 300*time.Millisecond)
	if err == nil {
		t.Fatal("无人应答时应报错")
	}
}
