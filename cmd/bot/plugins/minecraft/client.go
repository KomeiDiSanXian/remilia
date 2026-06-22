package minecraft

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultJavaPort    = 25565
	DefaultBedrockPort = 19132
	protocolVersion    = 766
)

// 连接错误 sentinel。
var (
	ErrNotOnline          = errors.New("server is offline or unreachable")
	ErrBedrockNotBedrock  = errors.New("server did not respond with a valid Bedrock pong")
	ErrBedrockUDPTooShort = errors.New("bedrock response too short")
)

var motdColors = map[byte]color.Color{
	'0': color.RGBA{0, 0, 0, 255},
	'1': color.RGBA{0, 0, 170, 255},
	'2': color.RGBA{0, 170, 0, 255},
	'3': color.RGBA{0, 170, 170, 255},
	'4': color.RGBA{170, 0, 0, 255},
	'5': color.RGBA{170, 0, 170, 255},
	'6': color.RGBA{255, 170, 0, 255},
	'7': color.RGBA{170, 170, 170, 255},
	'8': color.RGBA{85, 85, 85, 255},
	'9': color.RGBA{85, 85, 255, 255},
	'a': color.RGBA{85, 255, 85, 255},
	'b': color.RGBA{85, 255, 255, 255},
	'c': color.RGBA{255, 85, 85, 255},
	'd': color.RGBA{255, 85, 255, 255},
	'e': color.RGBA{255, 255, 85, 255},
	'f': color.RGBA{255, 255, 255, 255},
}

var motdBoldColors = map[byte]color.Color{ //nolint:unused
	'0': color.RGBA{30, 30, 30, 255},
	'1': color.RGBA{30, 30, 220, 255},
	'2': color.RGBA{30, 220, 30, 255},
	'3': color.RGBA{30, 220, 220, 255},
	'4': color.RGBA{220, 30, 30, 255},
	'5': color.RGBA{220, 30, 220, 255},
	'6': color.RGBA{255, 200, 30, 255},
	'7': color.RGBA{180, 180, 180, 255},
	'8': color.RGBA{100, 100, 100, 255},
	'9': color.RGBA{100, 100, 255, 255},
	'a': color.RGBA{100, 255, 100, 255},
	'b': color.RGBA{100, 255, 255, 255},
	'c': color.RGBA{255, 100, 100, 255},
	'd': color.RGBA{255, 100, 255, 255},
	'e': color.RGBA{255, 255, 100, 255},
	'f': color.RGBA{255, 255, 255, 255},
}

// MotdSegment 表示 MOTD 中一个具有特定颜色和加粗属性的文本段。
type MotdSegment struct {
	Text  string
	Color color.Color
	Bold  bool
}

// MCServerStatus 包含 Minecraft 服务器查询的完整结果。
type MCServerStatus struct {
	Online    bool
	Host      string
	Port      int
	Latency   time.Duration
	Edition   string
	Version   string
	Protocol  int
	MOTD      []MotdSegment
	MOTDPlain string
	Players   struct {
		Online int
		Max    int
		List   []PlayerInfo
	}
	Favicon []byte
}

// PlayerInfo 表示服务器上的一个在线玩家。
type PlayerInfo struct {
	Name string `json:"name"`
	UUID string `json:"id"`
}

type javaResponse struct {
	Version struct {
		Name     string `json:"name"`
		Protocol int    `json:"protocol"`
	} `json:"version"`
	Players struct {
		Max    int          `json:"max"`
		Online int          `json:"online"`
		Sample []PlayerInfo `json:"sample"`
	} `json:"players"`
	Description        json.RawMessage `json:"description"`
	Favicon            string          `json:"favicon"`
	EnforcesSecureChat bool            `json:"enforcesSecureChat"`
	PreviewsChat       bool            `json:"previewsChat"`
}

func parseJavaDescription(raw json.RawMessage, result *MCServerStatus) {
	if len(raw) == 0 {
		result.MOTD = []MotdSegment{{Text: "A Minecraft Server", Color: color.White}}
		result.MOTDPlain = "A Minecraft Server"
		return
	}
	if raw[0] == '"' {
		var s string
		json.Unmarshal(raw, &s)
		result.MOTD = ParseMotd(s)
		result.MOTDPlain = stripMotd(s)
		return
	}
	var sb strings.Builder
	segments := parseTextComponents(raw)
	result.MOTD = segments
	for _, s := range segments {
		sb.WriteString(s.Text)
	}
	result.MOTDPlain = sb.String()
}

type textComponent struct {
	Text   string            `json:"text"`
	Bold   bool              `json:"bold,omitempty"`
	Italic bool              `json:"italic,omitempty"`
	Color  string            `json:"color,omitempty"`
	Extra  []json.RawMessage `json:"extra,omitempty"`
}

func parseTextComponents(raw json.RawMessage) []MotdSegment {
	var root textComponent
	if err := json.Unmarshal(raw, &root); err != nil {
		return []MotdSegment{{Text: string(raw), Color: color.White}}
	}
	var segments []MotdSegment
	collectTextComponents(&root, &segments, false, color.White)
	return segments
}

func collectTextComponents(c *textComponent, out *[]MotdSegment, parentBold bool, parentColor color.Color) {
	bold := parentBold || c.Bold
	col := parentColor
	if c.Color != "" {
		if clr, ok := resolveColor(c.Color); ok {
			col = clr
		}
	}
	if c.Text != "" {
		*out = append(*out, MotdSegment{Text: c.Text, Color: col, Bold: bold})
	}
	for _, extra := range c.Extra {
		var child textComponent
		if err := json.Unmarshal(extra, &child); err != nil {
			continue
		}
		collectTextComponents(&child, out, bold, col)
	}
}

func resolveColor(nameOrCode string) (color.Color, bool) {
	named := map[string]color.Color{
		"black":        color.RGBA{0, 0, 0, 255},
		"dark_blue":    color.RGBA{0, 0, 170, 255},
		"dark_green":   color.RGBA{0, 170, 0, 255},
		"dark_aqua":    color.RGBA{0, 170, 170, 255},
		"dark_red":     color.RGBA{170, 0, 0, 255},
		"dark_purple":  color.RGBA{170, 0, 170, 255},
		"gold":         color.RGBA{255, 170, 0, 255},
		"gray":         color.RGBA{170, 170, 170, 255},
		"dark_gray":    color.RGBA{85, 85, 85, 255},
		"blue":         color.RGBA{85, 85, 255, 255},
		"green":        color.RGBA{85, 255, 85, 255},
		"aqua":         color.RGBA{85, 255, 255, 255},
		"red":          color.RGBA{255, 85, 85, 255},
		"light_purple": color.RGBA{255, 85, 255, 255},
		"yellow":       color.RGBA{255, 255, 85, 255},
		"white":        color.RGBA{255, 255, 255, 255},
	}
	if c, ok := named[nameOrCode]; ok {
		return c, true
	}
	if len(nameOrCode) == 6 || len(nameOrCode) == 8 {
		return parseHexColor(nameOrCode), true
	}
	return nil, false
}

func parseHexColor(hex string) color.Color {
	hex = strings.TrimPrefix(hex, "#")
	var r, g, b, a uint8 = 0, 0, 0, 255
	switch len(hex) {
	case 6:
		fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	case 8:
		fmt.Sscanf(hex, "%02x%02x%02x%02x", &r, &g, &b, &a)
	}
	return color.RGBA{R: r, G: g, B: b, A: a}
}

// Ping 自动探测服务器版本，先尝试 Java 版查询（超时减半），失败后回退到 Bedrock。
func Ping(host string, port int, timeout time.Duration) (*MCServerStatus, error) {
	half := max(timeout/2, 2*time.Second)
	status, err := PingJava(host, port, half)
	if err == nil {
		return status, nil
	}
	remaining := max(timeout-half, 2*time.Second)
	return PingBedrock(host, port, remaining)
}

// ResolveAddr 解析服务器地址。若 port > 0 直接返回；否则尝试 SRV 记录查询 minecraft._tcp。
func ResolveAddr(host string, port int) (string, int, error) {
	if port > 0 {
		return host, port, nil
	}
	_, srvs, err := net.LookupSRV("minecraft", "tcp", host)
	if err == nil && len(srvs) > 0 {
		return srvs[0].Target, int(srvs[0].Port), nil
	}
	return host, DefaultJavaPort, nil
}

// PingJava 使用 Minecraft Server List Ping 协议查询 Java 版服务器状态。
// 首先尝试 TCP SLP 直连，失败后自动回退到 mcsrvstat.us HTTP API。
func PingJava(host string, port int, timeout time.Duration) (*MCServerStatus, error) {
	origHost, origPort := host, port
	host, port, _ = ResolveAddr(host, port)
	addr := net.JoinHostPort(host, fmt.Sprint(port))

	conn, err := net.DialTimeout("tcp4", addr, timeout)
	if err != nil {
		return pingJavaViaAPI(origHost, origPort, timeout)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	pkt := &packetBuffer{}
	pkt.writeVarInt(protocolVersion)
	pkt.writeString(host)
	pkt.writeUint16(uint16(port))
	pkt.writeVarInt(1)

	sendPacket(conn, 0x00, pkt.bytes())

	sendPacket(conn, 0x00, nil)

	start := time.Now()
	respData, err := readPacket(conn)
	if err != nil {
		status, apiErr := pingJavaViaAPI(origHost, origPort, timeout)
		if apiErr == nil {
			return status, nil
		}
		return nil, fmt.Errorf("read status response: %w", err)
	}
	r := &packetReader{data: respData}
	pid, err := r.readVarInt()
	if err != nil {
		return nil, fmt.Errorf("read packet id: %w", err)
	}
	_ = pid
	jsonStr, err := r.readString()
	if err != nil {
		return nil, fmt.Errorf("read json string: %w", err)
	}

	var jr javaResponse
	if err := json.Unmarshal([]byte(jsonStr), &jr); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}

	sendPing := &packetBuffer{}
	sendPing.writeInt64(start.UnixMilli())
	pingStart := time.Now()
	sendPacket(conn, 0x01, sendPing.bytes())
	pongData, err := readPacket(conn)
	if err == nil {
		pr := &packetReader{data: pongData}
		_, _ = pr.readVarInt()
		_, _ = pr.readInt64()
	}
	latency := time.Since(pingStart)

	result := &MCServerStatus{
		Online:   true,
		Host:     host,
		Port:     port,
		Latency:  latency,
		Edition:  "java",
		Version:  jr.Version.Name,
		Protocol: jr.Version.Protocol,
		Players: struct {
			Online int
			Max    int
			List   []PlayerInfo
		}{
			Online: jr.Players.Online,
			Max:    jr.Players.Max,
			List:   jr.Players.Sample,
		},
	}

	parseJavaDescription(jr.Description, result)

	if jr.Favicon != "" {
		clean := strings.TrimPrefix(jr.Favicon, "data:image/png;base64,")
		if data, err := b64Decode(clean); err == nil {
			result.Favicon = data
		}
	}

	return result, nil
}

// PingBedrock 使用 RakNet Unconnected Ping 协议查询 Bedrock 版服务器状态。
// 首先尝试 UDP 直连（速度快），失败后自动回退到 mcsrvstat.us HTTP API。
func PingBedrock(host string, port int, timeout time.Duration) (*MCServerStatus, error) {
	if port <= 0 {
		port = DefaultBedrockPort
	}

	addr := net.JoinHostPort(host, fmt.Sprint(port))
	ra, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve: %w", ErrNotOnline, err)
	}
	laddr, err := net.ResolveUDPAddr("udp4", ":0")
	if err != nil {
		return nil, fmt.Errorf("%w: local addr: %w", ErrNotOnline, err)
	}
	conn, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		return nil, fmt.Errorf("%w: listen: %w", ErrNotOnline, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	pingData := make([]byte, 25)
	pingData[0] = 0x01
	binary.BigEndian.PutUint64(pingData[1:9], uint64(time.Now().UnixMilli()))
	copy(pingData[9:25], []byte{0x00, 0xff, 0xff, 0x00, 0xfe, 0xfe, 0xfe, 0xfe, 0xfd, 0xfd, 0xfd, 0xfd, 0x12, 0x34, 0x56, 0x78})

	start := time.Now()
	if _, err := conn.WriteTo(pingData, ra); err != nil {
		return nil, fmt.Errorf("%w: write: %w", ErrNotOnline, err)
	}

	resp := make([]byte, 2048)
	n, _, err := conn.ReadFrom(resp)
	if err != nil {
		status, apiErr := pingBedrockViaAPI(host, port, timeout)
		if apiErr == nil {
			return status, nil
		}
		return nil, fmt.Errorf("%w: read: %w", ErrNotOnline, err)
	}
	latency := time.Since(start)

	if n < 35 {
		return nil, fmt.Errorf("%w: too short (%d)", ErrBedrockUDPTooShort, n)
	}
	if resp[0] != 0x1c {
		return nil, fmt.Errorf("%w: unexpected packet id 0x%02x", ErrBedrockNotBedrock, resp[0])
	}

	strLen := int(binary.BigEndian.Uint16(resp[33:35]))
	if 35+strLen > n {
		strLen = n - 35
	}
	infoStr := string(resp[35 : 35+strLen])
	fields := strings.Split(infoStr, ";")

	result := &MCServerStatus{
		Online:  true,
		Host:    host,
		Port:    port,
		Latency: latency,
		Edition: "bedrock",
	}

	if len(fields) >= 1 {
		_ = fields[0]
	}
	if len(fields) >= 2 {
		result.MOTDPlain = fields[1]
		result.MOTD = []MotdSegment{{Text: fields[1], Color: color.White}}
	}
	if len(fields) >= 4 {
		result.Version = fields[3]
	}
	if len(fields) >= 5 {
		fmt.Sscanf(fields[4], "%d", &result.Players.Online)
	}
	if len(fields) >= 6 {
		fmt.Sscanf(fields[5], "%d", &result.Players.Max)
	}

	return result, nil
}

// bedrockAPIResponse mcsrvstat.us Bedrock API 响应结构。
type bedrockAPIResponse struct {
	Online   bool   `json:"online"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Hostname string `json:"hostname"`
	Version  string `json:"version"`
	Protocol struct {
		Version int    `json:"version"`
		Name    string `json:"name"`
	} `json:"protocol"`
	MOTD struct {
		Clean []string `json:"clean"`
		Raw   []string `json:"raw"`
	} `json:"motd"`
	Players struct {
		Max    int `json:"max"`
		Online int `json:"online"`
	} `json:"players"`
}

// pingBedrockViaAPI 通过 mcsrvstat.us HTTP API 查询 Bedrock 服务器状态。
// 用于 UDP 直连失败时的回退方案，API 使用 HTTPS 因而能穿透 UDP 封锁。
func pingBedrockViaAPI(host string, port int, timeout time.Duration) (*MCServerStatus, error) {
	if port <= 0 {
		port = DefaultBedrockPort
	}
	apiURL := fmt.Sprintf("https://api.mcsrvstat.us/bedrock/3/%s:%d", host, port)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RemiliaBot/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	defer resp.Body.Close()

	var apiResp bedrockAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("api json: %w", err)
	}
	if !apiResp.Online {
		return nil, ErrNotOnline
	}

	result := &MCServerStatus{
		Online:    true,
		Host:      apiResp.Hostname,
		Port:      apiResp.Port,
		Edition:   "bedrock",
		MOTDPlain: strings.Join(apiResp.MOTD.Clean, " | "),
	}

	version := apiResp.Version
	if apiResp.Protocol.Name != "" {
		version = apiResp.Protocol.Name
	}
	result.Version = version

	if len(apiResp.MOTD.Raw) > 0 {
		result.MOTD = []MotdSegment{{Text: strings.Join(apiResp.MOTD.Raw, " | "), Color: color.White}}
	} else if len(apiResp.MOTD.Clean) > 0 {
		result.MOTD = []MotdSegment{{Text: strings.Join(apiResp.MOTD.Clean, " | "), Color: color.White}}
	}

	result.Players.Online = apiResp.Players.Online
	result.Players.Max = apiResp.Players.Max

	if result.Host == "" {
		result.Host = host
	}
	return result, nil
}

// javaAPIResponse mcsrvstat.us Java API 响应结构。
type javaAPIResponse struct {
	Online   bool   `json:"online"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Hostname string `json:"hostname"`
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
	MOTD     struct {
		Clean []string `json:"clean"`
		Raw   []string `json:"raw"`
	} `json:"motd"`
	Players struct {
		Max    int `json:"max"`
		Online int `json:"online"`
	} `json:"players"`
	Ping    int    `json:"ping"`
	Favicon string `json:"favicon"`
}

// pingJavaViaAPI 通过 mcsrvstat.us HTTP API 查询 Java 版服务器状态。
func pingJavaViaAPI(host string, port int, timeout time.Duration) (*MCServerStatus, error) {
	if port <= 0 {
		port = DefaultJavaPort
	}
	apiURL := fmt.Sprintf("https://api.mcsrvstat.us/3/%s:%d", host, port)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RemiliaBot/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	defer resp.Body.Close()

	var apiResp javaAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("api json: %w", err)
	}
	if !apiResp.Online {
		return nil, ErrNotOnline
	}

	result := &MCServerStatus{
		Online:   true,
		Host:     apiResp.Hostname,
		Port:     apiResp.Port,
		Latency:  time.Duration(apiResp.Ping) * time.Millisecond,
		Edition:  "java",
		Version:  apiResp.Version,
		Protocol: apiResp.Protocol,
		Players: struct {
			Online int
			Max    int
			List   []PlayerInfo
		}{
			Online: apiResp.Players.Online,
			Max:    apiResp.Players.Max,
		},
		MOTDPlain: strings.Join(apiResp.MOTD.Clean, " | "),
	}

	if len(apiResp.MOTD.Raw) > 0 {
		result.MOTD = []MotdSegment{{Text: strings.Join(apiResp.MOTD.Raw, " | "), Color: color.White}}
	} else if len(apiResp.MOTD.Clean) > 0 {
		result.MOTD = []MotdSegment{{Text: strings.Join(apiResp.MOTD.Clean, " | "), Color: color.White}}
	}

	if apiResp.Favicon != "" {
		clean := strings.TrimPrefix(apiResp.Favicon, "data:image/png;base64,")
		if data, err := b64Decode(clean); err == nil {
			result.Favicon = data
		}
	}

	if result.Host == "" {
		result.Host = host
	}
	return result, nil
}

// ParseMotd 解析 Minecraft MOTD 中的 § 颜色代码，返回带颜色和属性的文本段切片。
// 支持 §0-§f 颜色、§l 加粗、§r 重置；§k、§m、§n、§o 被直接忽略。
func ParseMotd(raw string) []MotdSegment {
	if raw == "" {
		return []MotdSegment{{Text: "A Minecraft Server", Color: color.White}}
	}
	var segments []MotdSegment
	var buf strings.Builder
	var currentColor color.Color = color.White
	bold := false

	i := 0
	flush := func() {
		if buf.Len() > 0 {
			segments = append(segments, MotdSegment{Text: buf.String(), Color: currentColor, Bold: bold})
			buf.Reset()
		}
	}
	for i < len(raw) {
		if raw[i] == '\u00a7' || raw[i] == '\u0026' {
			i++
			if i >= len(raw) {
				break
			}
			code := raw[i]
			i++
			switch {
			case code >= '0' && code <= '9' || code >= 'a' && code <= 'f' || code >= 'A' && code <= 'F':
				flush()
				if c, ok := motdColors[toLower(code)]; ok {
					currentColor = c
				}
				bold = false
			case code == 'l' || code == 'L':
				bold = true
			case code == 'r' || code == 'R':
				flush()
				currentColor = color.White
				bold = false
			case code == 'k' || code == 'm' || code == 'n' || code == 'o':
			}
			continue
		}
		buf.WriteByte(raw[i])
		i++
	}
	flush()
	if len(segments) == 0 {
		return []MotdSegment{{Text: raw, Color: color.White}}
	}
	return segments
}

func stripMotd(raw string) string {
	var sb strings.Builder
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\u00a7' || raw[i] == '\u0026' {
			i++
			continue
		}
		sb.WriteByte(raw[i])
	}
	return sb.String()
}

func toLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}

type packetBuffer struct {
	data []byte
}

func (p *packetBuffer) writeVarInt(v int) {
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		p.data = append(p.data, b)
		if v == 0 {
			break
		}
	}
}

func (p *packetBuffer) writeString(s string) {
	p.writeVarInt(len(s))
	p.data = append(p.data, []byte(s)...)
}

func (p *packetBuffer) writeUint16(v uint16) {
	p.data = append(p.data, byte(v>>8), byte(v))
}

func (p *packetBuffer) writeInt64(v int64) {
	p.data = append(p.data,
		byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v),
	)
}

func (p *packetBuffer) bytes() []byte { return p.data }

type packetReader struct {
	data []byte
	off  int
}

func (r *packetReader) remaining() int {
	return len(r.data) - r.off
}

func (r *packetReader) readVarInt() (int, error) {
	result := 0
	shift := 0
	for {
		if r.off >= len(r.data) {
			return 0, io.ErrUnexpectedEOF
		}
		b := r.data[r.off]
		r.off++
		result |= int(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, nil
		}
		shift += 7
		if shift > 63 {
			return 0, errors.New("varint too long")
		}
	}
}

func (r *packetReader) readString() (string, error) {
	length, err := r.readVarInt()
	if err != nil {
		return "", err
	}
	if length < 0 || length > r.remaining() {
		return "", io.ErrUnexpectedEOF
	}
	s := string(r.data[r.off : r.off+length])
	r.off += length
	return s, nil
}

func (r *packetReader) readInt64() (int64, error) {
	if r.off+8 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := int64(r.data[r.off])<<56 | int64(r.data[r.off+1])<<48 |
		int64(r.data[r.off+2])<<40 | int64(r.data[r.off+3])<<32 |
		int64(r.data[r.off+4])<<24 | int64(r.data[r.off+5])<<16 |
		int64(r.data[r.off+6])<<8 | int64(r.data[r.off+7])
	r.off += 8
	return v, nil
}

func sendPacket(conn net.Conn, packetID int, payload []byte) error {
	buf := &packetBuffer{}
	inner := &packetBuffer{}
	inner.writeVarInt(packetID)
	inner.data = append(inner.data, payload...)
	buf.writeVarInt(len(inner.data))
	buf.data = append(buf.data, inner.data...)
	_, err := conn.Write(buf.data)
	return err
}

func readPacket(conn net.Conn) ([]byte, error) {
	var length int
	{
		var tmp [1]byte
		for shift := 0; ; shift += 7 {
			if _, err := io.ReadFull(conn, tmp[:]); err != nil {
				return nil, err
			}
			length |= int(tmp[0]&0x7F) << shift
			if tmp[0]&0x80 == 0 {
				break
			}
		}
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, err
	}
	return data, nil
}

func b64Decode(s string) ([]byte, error) {
	data := make([]byte, len(s)*3/4)
	ndst := 0
	pad := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		var val byte
		switch {
		case c >= 'A' && c <= 'Z':
			val = c - 'A'
		case c >= 'a' && c <= 'z':
			val = c - 'a' + 26
		case c >= '0' && c <= '9':
			val = c - '0' + 52
		case c == '+':
			val = 62
		case c == '/':
			val = 63
		case c == '=':
			pad++
			if pad > 2 {
				return nil, fmt.Errorf("invalid base64: too many padding")
			}
			continue
		default:
			return nil, fmt.Errorf("invalid base64 char: %c", c)
		}
		switch i % 4 {
		case 0:
			data[ndst] = val << 2
		case 1:
			data[ndst] |= val >> 4
			ndst++
			data[ndst] = val << 4
		case 2:
			data[ndst] |= val >> 2
			ndst++
			data[ndst] = val << 6
		case 3:
			data[ndst] |= val
			ndst++
		}
	}
	if pad > 0 {
		ndst -= pad
	}
	return data[:ndst], nil
}
