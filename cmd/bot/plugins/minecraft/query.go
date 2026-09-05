package minecraft

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// GS4（GameSpy4）Query 协议：服务器需开启 enable-query。
// 用于获取完整在线玩家列表，绕过 SLP sample 的数量限制（vanilla 只返回少量玩家）。

// ErrQueryNoResponse Query 无响应（服务器未开启 enable-query 时为正常现象）。
var ErrQueryNoResponse = errors.New("query: no response")

const (
	queryMagicSplitnum = "splitnum"
	queryMagicPlayer   = "player_"
	// queryStatMaxBytes 全量统计响应大小上限（玩家列表可能较长）。
	queryStatMaxBytes = 8192
)

// GS4Stat GS4 全量统计结果。
type GS4Stat struct {
	Hostname   string
	GameType   string
	GameID     string
	Version    string
	Plugins    string
	Map        string
	NumPlayers int
	MaxPlayers int
	Port       int
	IP         string
	Players    []string
}

// QueryGS4 向 host:port 发送 GS4 全量查询，返回服务器统计与完整玩家列表。
// port 应为服务器 query.port（通常与服务器端口一致）。
func QueryGS4(ctx context.Context, host string, port int, timeout time.Duration) (*GS4Stat, error) {
	if port <= 0 {
		port = DefaultJavaPort
	}
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	ra, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve: %w", ErrQueryNoResponse, err)
	}
	laddr, err := net.ResolveUDPAddr("udp4", ":0")
	if err != nil {
		return nil, fmt.Errorf("%w: local addr: %w", ErrQueryNoResponse, err)
	}
	conn, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		return nil, fmt.Errorf("%w: listen: %w", ErrQueryNoResponse, err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)

	var session [4]byte
	if _, err := rand.Read(session[:]); err != nil {
		return nil, fmt.Errorf("%w: session: %w", ErrQueryNoResponse, err)
	}

	// 握手：0xFE 0xFD 0x09 + 会话 ID → 0x09 + 会话 ID + 挑战令牌
	if err := queryWrite(conn, ra, 0x09, session[:], nil); err != nil {
		return nil, fmt.Errorf("%w: handshake write: %w", ErrQueryNoResponse, err)
	}
	buf := make([]byte, 32)
	n, _, err := conn.ReadFrom(buf)
	if err != nil || n < 9 || buf[0] != 0x09 {
		return nil, fmt.Errorf("%w: handshake", ErrQueryNoResponse)
	}
	token := buf[5:9]

	// 全量统计请求：挑战令牌 + 4 个 0x00 填充
	payload := make([]byte, 0, 8)
	payload = append(payload, token...)
	payload = append(payload, 0x00, 0x00, 0x00, 0x00)
	if err := queryWrite(conn, ra, 0x00, session[:], payload); err != nil {
		return nil, fmt.Errorf("%w: stat write: %w", ErrQueryNoResponse, err)
	}

	resp := make([]byte, queryStatMaxBytes)
	n, _, err = conn.ReadFrom(resp)
	if err != nil {
		return nil, fmt.Errorf("%w: stat read: %w", ErrQueryNoResponse, err)
	}
	return parseGS4Stat(resp[:n])
}

func queryWrite(conn *net.UDPConn, ra *net.UDPAddr, reqType byte, session, payload []byte) error {
	buf := make([]byte, 0, 3+len(session)+len(payload))
	buf = append(buf, 0xFE, 0xFD, reqType)
	buf = append(buf, session...)
	buf = append(buf, payload...)
	_, err := conn.WriteTo(buf, ra)
	return err
}

// parseGS4Stat 解析全量统计响应。
//
// 响应结构（不同实现的前置填充有差异，以 "splitnum" 魔数定位）：
// 0x00 + 会话 ID + "splitnum" + 0x00 0x80 0x00 + KV 段（key/value 交替的
// \0 分隔 token 流，空 key 结束）+ 填充 + 0x01 "player_" 0x00 0x00
// + 玩家名（\0 分隔，额外 \0 结束）。
func parseGS4Stat(data []byte) (*GS4Stat, error) {
	pos := bytes.Index(data, []byte(queryMagicSplitnum))
	if pos < 0 {
		return nil, errors.New("query: missing splitnum magic")
	}
	pos += len(queryMagicSplitnum) + 3 // 魔数后固定跟 0x00 0x80 0x00

	// KV 段：逐 token 读取，key/value 交替；空 value 合法（如未安装插件的 "plugins"）。
	readToken := func() (string, bool) {
		if pos >= len(data) {
			return "", false
		}
		end := bytes.IndexByte(data[pos:], 0x00)
		if end < 0 {
			return "", false
		}
		tok := string(data[pos : pos+end])
		pos += end + 1
		return tok, true
	}

	stat := &GS4Stat{}
	for {
		key, ok := readToken()
		if !ok || key == "" {
			break
		}
		value, _ := readToken()
		switch key {
		case "hostname":
			stat.Hostname = value
		case "gametype":
			stat.GameType = value
		case "game_id":
			stat.GameID = value
		case "version":
			stat.Version = value
		case "plugins":
			stat.Plugins = value
		case "map":
			stat.Map = value
		case "numplayers":
			stat.NumPlayers, _ = strconv.Atoi(value)
		case "maxplayers":
			stat.MaxPlayers, _ = strconv.Atoi(value)
		case "hostport":
			stat.Port, _ = strconv.Atoi(value)
		case "hostip":
			stat.IP = value
		}
	}

	rest := data[pos:]
	if marker := []byte(queryMagicPlayer + "\x00\x00"); bytes.Contains(rest, marker) {
		idx := bytes.Index(rest, marker) + len(marker)
		for name := range strings.SplitSeq(string(rest[idx:]), "\x00") {
			if name == "" {
				break
			}
			stat.Players = append(stat.Players, name)
		}
	}
	return stat, nil
}

// applyGS4 将 GS4 查询结果合并进 SLP 状态（GS4 的玩家列表更完整）。
func applyGS4(status *MCServerStatus, gs4 *GS4Stat) {
	if gs4.NumPlayers > 0 {
		status.Players.Online = gs4.NumPlayers
	}
	if gs4.MaxPlayers > 0 {
		status.Players.Max = gs4.MaxPlayers
	}
	if len(gs4.Players) > 0 {
		list := make([]PlayerInfo, 0, len(gs4.Players))
		for _, name := range gs4.Players {
			list = append(list, PlayerInfo{Name: name})
		}
		status.Players.List = list
	}
	if status.Version == "" && gs4.Version != "" {
		status.Version = gs4.Version
	}
	if status.GameMode == "" && gs4.GameType != "" {
		status.GameMode = gs4.GameType
	}
	if gs4.Map != "" && !strings.EqualFold(gs4.Map, "default") {
		status.Map = gs4.Map
	}
	if status.MOTDPlain == "" && gs4.Hostname != "" {
		status.MOTD = ParseMotd(gs4.Hostname)
		status.MOTDPlain = stripMotd(gs4.Hostname)
	}
	if status.Software == "" && gs4.Plugins != "" {
		status.Software, status.PluginNames = parsePluginList(gs4.Plugins)
		status.PluginCount = len(status.PluginNames)
	}
}

// parsePluginList 解析 GS4 "plugins" 字段。
// 格式通常为 "服务端描述: 插件A, 插件B, ..."（无 ": " 时整串视为服务端描述）。
func parsePluginList(raw string) (software string, names []string) {
	software = strings.TrimSpace(raw)
	if before, after, ok := strings.Cut(raw, ": "); ok {
		software = strings.TrimSpace(before)
		for name := range strings.SplitSeq(after, ", ") {
			name = strings.TrimSpace(name)
			if name != "" {
				names = append(names, name)
			}
		}
	}
	return software, names
}
