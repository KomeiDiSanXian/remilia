package minecraft

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultJavaPort    = 25565
	DefaultBedrockPort = 19132
	protocolVersion    = 766
	// maxStatusPacketBytes 单个 SLP 响应包的字节上限。
	// 状态 JSON（含 base64 favicon）实测在数百 KB 以内，1MB 余量充足；
	// 设上限是为了避免异常/恶意服务器用超长长度字段触发巨额内存分配。
	maxStatusPacketBytes = 1 << 20
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

// motdNamedColors Java 文本组件中的命名颜色（JSON color 字段）。
var motdNamedColors = map[string]color.Color{
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
	"aqua":         color.RGBA{85, 85, 255, 255},
	"red":          color.RGBA{255, 85, 85, 255},
	"light_purple": color.RGBA{255, 85, 255, 255},
	"yellow":       color.RGBA{255, 255, 85, 255},
	"white":        color.RGBA{255, 255, 255, 255},
}

// MotdSegment 表示 MOTD 中一个具有特定颜色和加粗属性的文本段。
type MotdSegment struct {
	Text  string
	Color color.Color
	Bold  bool
}

// MCServerStatus 包含 Minecraft 服务器查询的完整结果。
type MCServerStatus struct {
	Online   bool
	Host     string
	Port     int
	Latency  time.Duration
	Edition  string
	Version  string
	Protocol int
	// EnforcesSecureChat / PreviewsChat 聊天签名相关标志（SLP 可选字段）。
	EnforcesSecureChat bool
	PreviewsChat       bool
	// Via 查询途径：slp（Java 直连）/ raknet（Bedrock 直连）/ api（mcsrvstat.us）。
	Via       string
	MOTD      []MotdSegment
	MOTDPlain string
	// SubMOTD 第二行 MOTD（Bedrock sub-MOTD / API 多行 MOTD），可为空。
	SubMOTD      []MotdSegment
	SubMOTDPlain string
	GameMode     string
	Map          string
	// Software 服务端软件（GS4 plugins 字段解析，如 "Paper 1.21.1"）。
	Software string
	// PluginCount / PluginNames 服务端插件数量与名称（GS4，可为空）。
	PluginCount int
	PluginNames []string
	Players     struct {
		Online int
		Max    int
		List   []PlayerInfo
	}
	Favicon []byte
	// Error 服务器无法连接时的错误描述（仅离线状态卡片使用）。
	Error string
}

// PlayerInfo 表示服务器上的一个在线玩家。
type PlayerInfo struct {
	Name string `json:"name"`
	UUID string `json:"id"`
	// Head 玩家头像 PNG 字节（mc-heads.net，仅开启头像功能时填充）。
	Head []byte `json:"-"`
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
	var segments []MotdSegment
	if raw[0] == '"' {
		var s string
		json.Unmarshal(raw, &s)
		segments = ParseMotd(s)
	} else {
		segments = parseTextComponents(raw)
	}

	// 按 \n 拆分两行 MOTD（MC 惯例两行）；第二行复用 SubMOTD 渲染
	line1, line2 := splitMotdLines(segments)
	if len(line1) == 0 {
		line1 = []MotdSegment{{Text: "A Minecraft Server", Color: color.White}}
	}
	result.MOTD = line1
	result.SubMOTD = line2
	result.MOTDPlain = motdLinesText(line1, line2)
	result.SubMOTDPlain = motdSegmentText(line2)
}

// splitMotdLines 将分段文本按换行拆为最多两行（换行后的所有内容都归第二行）。
func splitMotdLines(segments []MotdSegment) (line1, line2 []MotdSegment) {
	line := 0
	for _, seg := range segments {
		parts := strings.Split(seg.Text, "\n")
		for j, part := range parts {
			if j > 0 {
				line++ // 先推进行号：空段（如换行结尾）也计入换行
			}
			if part == "" {
				continue
			}
			if line >= 1 {
				line2 = append(line2, MotdSegment{Text: part, Color: seg.Color, Bold: seg.Bold})
			} else {
				line1 = append(line1, MotdSegment{Text: part, Color: seg.Color, Bold: seg.Bold})
			}
		}
	}
	return line1, line2
}

// motdSegmentText 拼接分段文本（分段已不含颜色码）。
func motdSegmentText(segments []MotdSegment) string {
	var sb strings.Builder
	for _, s := range segments {
		sb.WriteString(s.Text)
	}
	return sb.String()
}

// motdLinesText 拼接两行 MOTD 为纯文本（行间以 \n 分隔）。
func motdLinesText(line1, line2 []MotdSegment) string {
	text := motdSegmentText(line1)
	if sub := motdSegmentText(line2); sub != "" {
		return text + "\n" + sub
	}
	return text
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
		// 兼容旧式 § 颜色码混入 JSON 文本组件（继承组件自身的颜色/加粗为基准）
		*out = append(*out, parseMotdWithBase(c.Text, col, bold)...)
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
	if c, ok := motdNamedColors[nameOrCode]; ok {
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

// parseHostPort 解析 "主机[:端口]" 形式的服务器地址。
// 兼容 IPv6：[::1]:25565 正确拆分端口，裸 IPv6（多个冒号且无端口）整体视为主机名。
// 返回 port=0 表示未指定端口，由调用方按版本选择默认值。
func parseHostPort(addr string) (string, int, error) {
	addr = strings.TrimSpace(addr)
	for _, scheme := range []string{"https://", "http://", "tcp://"} {
		addr = strings.TrimPrefix(addr, scheme)
	}
	// 容忍粘贴自浏览器/面板的完整 URL：丢弃端口之后的部分（路径、查询串等）
	if i := strings.IndexAny(addr, "/?#"); i >= 0 {
		addr = addr[:i]
	}
	if addr == "" {
		return "", 0, errors.New("服务器地址为空")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		// 无端口：整体视为主机名（含裸 IPv6 与 [IPv6] 括号形式）
		return unwrapIPv6Literal(addr), 0, nil
	}
	if host == "" {
		return "", 0, fmt.Errorf("服务器地址无效: %q", addr)
	}
	if portStr == "" {
		return unwrapIPv6Literal(host), 0, nil
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("端口无效: %q", portStr)
	}
	return host, port, nil
}

// unwrapIPv6Literal 去掉裸 IPv6 字面量的方括号（"[::1]" → "::1"）。
// net.SplitHostPort 只接受带端口的括号形式，不带端口时括号会残留，
// 直接当成主机名会导致解析失败。
func unwrapIPv6Literal(host string) string {
	if len(host) >= 2 && host[0] == '[' && host[len(host)-1] == ']' {
		return host[1 : len(host)-1]
	}
	return host
}

// remainingTimeout 返回自 start 起的剩余超时预算。
//
// 下限取 min(2s, total)：既保证预算耗尽时回退链路（API 查询等）仍有
// 一次尝试机会，又避免固定 2s 下限在小 timeout 配置下把总耗时拉长到
// 远超配置值（回退链路上限约为 timeout + 各跳下限之和）。
func remainingTimeout(start time.Time, total time.Duration) time.Duration {
	return max(total-time.Since(start), min(2*time.Second, total))
}

// Ping 自动探测服务器版本，先尝试 Java 版查询，失败后回退到 Bedrock。
// 显式指定 Bedrock 默认端口（19132）时优先探测 Bedrock。
// timeout 为两版探测共享的总预算，直连与 API 回退均从该预算扣减。
func Ping(host string, port int, timeout time.Duration) (*MCServerStatus, error) {
	start := time.Now()
	if port == DefaultBedrockPort {
		status, err := PingBedrock(host, port, timeout/2)
		if err == nil {
			return status, nil
		}
		return PingJava(host, port, remainingTimeout(start, timeout))
	}
	status, err := PingJava(host, port, timeout/2)
	if err == nil {
		return status, nil
	}
	return PingBedrock(host, port, remainingTimeout(start, timeout))
}

// srvEntry SRV 解析结果缓存条目（成功与"无 SRV 记录"均缓存，避免重复 DNS 查询）。
type srvEntry struct {
	host string
	port int
}

// srvCache SRV 解析缓存。
var srvCache = newTTLCache[srvEntry](5*time.Minute, 1024)

// ResolveAddr 解析 Java 版服务器地址。若 port > 0 直接返回；否则查询
// minecraft._tcp SRV 记录，无记录时回退 25565。返回的 port 始终 > 0。
func ResolveAddr(host string, port int) (string, int, error) {
	if port > 0 {
		return host, port, nil
	}
	target, tport := resolveSRV("tcp", host, DefaultJavaPort)
	return target, tport, nil
}

// ResolveBedrockAddr 解析 Bedrock 版服务器地址。若 port > 0 直接返回；
// 否则查询 minecraft._udp SRV 记录（Bedrock 的 SRV 约定不如 Java 普及），
// 无记录时回退 19132。返回的 port 始终 > 0。
func ResolveBedrockAddr(host string, port int) (string, int, error) {
	if port > 0 {
		return host, port, nil
	}
	target, tport := resolveSRV("udp", host, DefaultBedrockPort)
	return target, tport, nil
}

// resolveSRV 查询 _minecraft._<proto> SRV 记录，失败或缺失时回退 fallbackPort。
//
// 结果（含"无 SRV 记录"的否定结果）缓存 5 分钟，避免高频查询反复打 DNS。
func resolveSRV(proto, host string, fallbackPort int) (string, int) {
	key := proto + "|" + normalizeHostKey(host)
	if e, ok := srvCache.get(key); ok {
		return e.host, e.port
	}
	target, tport := host, fallbackPort
	if _, srvs, err := net.LookupSRV("minecraft", proto, host); err == nil && len(srvs) > 0 {
		// Go 的 LookupSRV 返回带尾点的绝对域名，去掉尾点便于展示与后续复用
		if name := strings.TrimSuffix(srvs[0].Target, "."); name != "" {
			target = name
		}
		if srvs[0].Port > 0 {
			tport = int(srvs[0].Port)
		}
	}
	srvCache.set(key, srvEntry{host: target, port: tport})
	return target, tport
}

// normalizeHostKey 归一化用于缓存与 singleflight 的主机名 key
// （大小写不敏感、忽略末尾根点），避免 "MC.a.com"/"mc.a.com." 被当成两个目标。
func normalizeHostKey(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

// isPrivateIP 判断 IP 是否为私有/保留地址（回环、链路本地、RFC1918、CGNAT、IPv6 ULA）。
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 10:
			return true
		case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31:
			return true
		case v4[0] == 192 && v4[1] == 168:
			return true
		case v4[0] == 169 && v4[1] == 254:
			return true
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127: // CGNAT（Tailscale 等）
			return true
		}
		return false
	}
	// IPv6 ULA fc00::/7
	return len(ip) >= 2 && ip[0]&0xfe == 0xfc
}

// isPrivateTarget 判断目标是否为私有地址（字面 IP 直接判断；主机名解析后判断，
// 解析失败视为非私有——交由直连/API 各自报错）。
func isPrivateTarget(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return isPrivateIP(ip)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return false
	}
	for _, s := range ips {
		if ip := net.ParseIP(s); ip != nil && isPrivateIP(ip) {
			return true
		}
	}
	return false
}

// protocolNames 常见协议号 → 版本名（仅用于 version name 缺失时的兜底展示）。
var protocolNames = map[int]string{
	47:  "1.8",
	107: "1.9",
	110: "1.9.4",
	210: "1.10.2",
	315: "1.11",
	316: "1.11.1",
	335: "1.12",
	338: "1.12.1",
	340: "1.12.2",
	393: "1.13",
	401: "1.13.1",
	404: "1.13.2",
	441: "1.14",
	480: "1.14.4",
	498: "1.15",
	578: "1.15.2",
	735: "1.16",
	751: "1.16.5",
	754: "1.17.1",
	757: "1.18.2",
	758: "1.19.2",
	761: "1.19.4",
	763: "1.20.1",
	765: "1.20.4",
	766: "1.20.6",
	767: "1.21",
	768: "1.21.1",
	769: "1.21.4",
}

// protocolVersionName 返回协议号对应的近似版本名（未知协议返回 "未知"）。
func protocolVersionName(protocol int) string {
	if name, ok := protocolNames[protocol]; ok {
		return name
	}
	return "未知"
}

// serverSoftwareBrands 常见服务端软件品牌。
//
// 用于从 SLP 的 version.name 里补充"服务端软件"信息：多数非原版服务端会把
// 品牌写进版本名（"Paper 1.21.1"、"Purpur 1.20.4"、"Velocity 1.2.3"），
// 这样无需额外发包即可展示；GS4 Query 不可用时（绝大多数公网服务器并未开启
// enable-query）这是唯一零成本的来源。原版服务器通常只给纯版本号。
var serverSoftwareBrands = []string{
	"Paper", "Purpur", "Pufferfish", "Spigot", "CraftBukkit", "Bukkit",
	"Folia", "Leaves", "Leaf", "Airplane", "Tuinity", "Yatopia",
	"Fabric", "Forge", "NeoForge", "Quilt", "Sponge", "Mohist", "Magma",
	"Arclight", "CatServer", "Kettle", "Thermos", "Crucible",
	"Velocity", "BungeeCord", "Waterfall", "Travertine", "Gate",
	"Geyser", "Nukkit", "Cloudburst", "PocketMine-MP", "Glowstone", "Cuberite",
	"Vanilla",
}

// guessSoftwareFromVersionName 从 SLP 的 version.name 中识别服务端软件品牌。
// 识别不出（原版纯版本号等）时返回空串，调用方保留原有空值语义。
func guessSoftwareFromVersionName(name string) string {
	if name == "" {
		return ""
	}
	tokens := strings.FieldsFunc(name, func(r rune) bool {
		switch r {
		case ' ', '\t', '-', '_', '/', '\\', '(', ')', '[', ']', ',', '+', '|':
			return true
		}
		return false
	})
	for _, tok := range tokens {
		for _, brand := range serverSoftwareBrands {
			if strings.EqualFold(tok, brand) {
				return brand
			}
		}
	}
	return ""
}

// PingViaAPI 仅通过 mcsrvstat.us HTTP API 查询，跳过直连发包。
//
// 直连发包是裸 TCP/UDP socket，无法经过 HTTP 代理；在出站需代理或有
// 防火墙限制的部署环境中，直连仅内网可用、外网必失败。此类环境可将
// 插件配置 direct_query 设为 false，全部改走 API（net/http 遵循代理
// 环境变量，因而可穿透）。
func PingViaAPI(host string, port int, edition string, timeout time.Duration) (*MCServerStatus, error) {
	start := time.Now()
	switch edition {
	case "java":
		return pingJavaViaAPI(host, port, timeout)
	case "bedrock":
		// Bedrock 未指定端口时按 _minecraft._udp SRV 解析，无记录则回退 19132
		_, bport, _ := ResolveBedrockAddr(host, port)
		return pingBedrockViaAPI(host, bport, timeout)
	default:
		if port == DefaultBedrockPort {
			status, err := pingBedrockViaAPI(host, port, timeout/2)
			if err == nil {
				return status, nil
			}
			return pingJavaViaAPI(host, port, remainingTimeout(start, timeout))
		}
		status, err := pingJavaViaAPI(host, port, timeout/2)
		if err == nil {
			return status, nil
		}
		return pingBedrockViaAPI(host, port, remainingTimeout(start, timeout))
	}
}

// PingJava 使用 Minecraft Server List Ping 协议查询 Java 版服务器状态。
// 首先尝试 TCP SLP 直连，失败后自动回退到 mcsrvstat.us HTTP API；
// 直连与 API 回退共享 timeout 总预算。
func PingJava(host string, port int, timeout time.Duration) (*MCServerStatus, error) {
	start := time.Now()
	origHost, origPort := host, port
	dialHost, dialPort, _ := ResolveAddr(host, port)
	addr := net.JoinHostPort(dialHost, fmt.Sprint(dialPort))

	// 直连失败或响应无法解析时统一回退 API：端口上跑着非 Minecraft 服务、
	// 响应被截断等情况下，API 能给出更准确的"离线"判定，比只抛一个晦涩的
	// 解析错误更有用。
	viaAPI := func(cause error) (*MCServerStatus, error) {
		status, apiErr := pingJavaViaAPI(origHost, origPort, remainingTimeout(start, timeout))
		if apiErr == nil {
			return status, nil
		}
		return nil, fmt.Errorf("%w（API 回退亦失败: %v）", cause, apiErr)
	}

	// "tcp" 双栈：IPv4/IPv6 均可（Go 会按解析结果逐一尝试）
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return viaAPI(fmt.Errorf("dial %s: %w", addr, err))
	}
	defer conn.Close()
	// 整个握手 + 往返共享同一 deadline，避免各阶段超时被逐一拉长
	conn.SetDeadline(start.Add(timeout))

	pkt := &packetBuffer{}
	pkt.writeVarInt(protocolVersion)
	// 握手携带用户请求的原始主机名，而非 SRV 解析后的目标地址：
	// BungeeCord/Velocity 的虚拟主机路由（forced-host）依据该字段决定响应内容。
	pkt.writeString(origHost)
	pkt.writeUint16(uint16(dialPort))
	pkt.writeVarInt(1)

	if err := sendPacket(conn, 0x00, pkt.bytes()); err != nil {
		return viaAPI(fmt.Errorf("send handshake: %w", err))
	}
	// 握手后立即发送空的 Status Request
	if err := sendPacket(conn, 0x00, nil); err != nil {
		return viaAPI(fmt.Errorf("send status request: %w", err))
	}

	respData, err := readPacket(conn)
	if err != nil {
		return viaAPI(fmt.Errorf("read status response: %w", err))
	}
	r := &packetReader{data: respData}
	pid, err := r.readVarInt()
	if err != nil {
		return viaAPI(fmt.Errorf("read packet id: %w", err))
	}
	if pid != 0x00 {
		return viaAPI(fmt.Errorf("unexpected status packet id 0x%02x", pid))
	}
	jsonStr, err := r.readString()
	if err != nil {
		return viaAPI(fmt.Errorf("read json string: %w", err))
	}

	var jr javaResponse
	if err := json.Unmarshal([]byte(jsonStr), &jr); err != nil {
		return viaAPI(fmt.Errorf("parse json: %w", err))
	}

	pingStart := time.Now()
	sendPing := &packetBuffer{}
	sendPing.writeInt64(pingStart.UnixMilli())
	if err := sendPacket(conn, 0x01, sendPing.bytes()); err != nil {
		return viaAPI(fmt.Errorf("send ping: %w", err))
	}
	pongData, err := readPacket(conn)
	if err == nil {
		pr := &packetReader{data: pongData}
		_, _ = pr.readVarInt()
		_, _ = pr.readInt64()
	}
	latency := time.Since(pingStart)

	result := &MCServerStatus{
		Online:             true,
		Host:               dialHost,
		Port:               dialPort,
		Latency:            latency,
		Edition:            "java",
		Via:                "slp",
		Version:            jr.Version.Name,
		Protocol:           jr.Version.Protocol,
		EnforcesSecureChat: jr.EnforcesSecureChat,
		PreviewsChat:       jr.PreviewsChat,
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
	// 服务端软件：优先由 version.name 推断（零成本），GS4 可用时会覆盖为更准确的描述
	result.Software = guessSoftwareFromVersionName(jr.Version.Name)

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
// 首先尝试 UDP 直连（速度快），失败后自动回退到 mcsrvstat.us HTTP API；
// 直连与 API 回退共享 timeout 总预算。
func PingBedrock(host string, port int, timeout time.Duration) (*MCServerStatus, error) {
	start := time.Now()
	// Bedrock 也有 SRV 约定（_minecraft._udp）；未指定端口时先查 SRV，
	// 无记录则回退默认端口 19132。
	dialHost, dialPort, _ := ResolveBedrockAddr(host, port)
	port = dialPort

	addr := net.JoinHostPort(dialHost, fmt.Sprint(port))
	ra, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve: %w", ErrNotOnline, err)
	}
	laddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		return nil, fmt.Errorf("%w: local addr: %w", ErrNotOnline, err)
	}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return nil, fmt.Errorf("%w: listen: %w", ErrNotOnline, err)
	}
	defer conn.Close()
	conn.SetDeadline(start.Add(timeout))

	pingData := make([]byte, 25)
	pingData[0] = 0x01
	binary.BigEndian.PutUint64(pingData[1:9], uint64(time.Now().UnixMilli()))
	copy(pingData[9:25], []byte{0x00, 0xff, 0xff, 0x00, 0xfe, 0xfe, 0xfe, 0xfe, 0xfd, 0xfd, 0xfd, 0xfd, 0x12, 0x34, 0x56, 0x78})

	if _, err := conn.WriteTo(pingData, ra); err != nil {
		return nil, fmt.Errorf("%w: write: %w", ErrNotOnline, err)
	}

	resp := make([]byte, 2048)
	n, _, err := conn.ReadFrom(resp)
	if err != nil {
		status, apiErr := pingBedrockViaAPI(host, port, remainingTimeout(start, timeout))
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
		Host:    dialHost,
		Port:    port,
		Latency: latency,
		Edition: "bedrock",
		Via:     "raknet",
	}

	// Bedrock pong 字段布局：
	// 0 GameName | 1 MOTD | 2 协议版本 | 3 游戏版本 | 4 在线玩家 | 5 最大玩家
	// 6 服务器 ID | 7 次级 MOTD | 8 游戏模式 | 9 模式编号 | 10/11 IPv4/IPv6 端口
	if len(fields) > 1 && fields[1] != "" {
		result.MOTD = ParseMotd(fields[1])
		result.MOTDPlain = stripMotd(fields[1])
	}
	if len(fields) > 2 {
		result.Protocol, _ = strconv.Atoi(fields[2])
	}
	if len(fields) > 3 {
		result.Version = fields[3]
	}
	if len(fields) > 4 {
		result.Players.Online, _ = strconv.Atoi(fields[4])
	}
	if len(fields) > 5 {
		result.Players.Max, _ = strconv.Atoi(fields[5])
	}
	if len(fields) > 7 && fields[7] != "" {
		result.SubMOTD = ParseMotd(fields[7])
		result.SubMOTDPlain = stripMotd(fields[7])
	}
	if len(fields) > 8 {
		result.GameMode = fields[8]
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

// apiHTTPClient mcsrvstat.us API 共享客户端。
// 请求级超时由各调用方的 context 控制，Client.Timeout 仅作兜底上限。
var apiHTTPClient = &http.Client{Timeout: 20 * time.Second}

// apiResponseError 将非 2xx 的 API 响应转为可读错误。
// mcsrvstat.us 存在速率限制，429 时给出明确提示。
func apiResponseError(resp *http.Response) error {
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("mcsrvstat.us API 限流（HTTP 429），请稍后重试")
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	return fmt.Errorf("mcsrvstat.us API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

// applyAPIMOTD 将 API 返回的多行 MOTD 写入状态。
//
// raw 保留 § 颜色码，是彩色渲染的唯一来源，因此优先采用；clean 是 API 预先
// 去码的纯文本，只用于 MOTDPlain，并在 raw 缺失时兜底构造分段。此前 clean
// 会无条件覆盖 MOTD，导致走 API 的查询颜色全部退化为白色。
func applyAPIMOTD(result *MCServerStatus, raw, clean []string) {
	hasRawMain := len(raw) > 0 && strings.TrimSpace(raw[0]) != ""
	hasRawSub := len(raw) > 1 && strings.TrimSpace(raw[1]) != ""

	if hasRawMain {
		result.MOTD = ParseMotd(raw[0])
	}
	if hasRawSub {
		result.SubMOTD = ParseMotd(raw[1])
	}

	if len(clean) > 0 {
		result.MOTDPlain = clean[0]
		if !hasRawMain {
			result.MOTD = []MotdSegment{{Text: clean[0], Color: color.White}}
		}
	} else if hasRawMain {
		result.MOTDPlain = stripMotd(raw[0])
	}

	if len(clean) > 1 {
		result.SubMOTDPlain = clean[1]
		if !hasRawSub {
			result.SubMOTD = []MotdSegment{{Text: clean[1], Color: color.White}}
		}
	} else if hasRawSub {
		result.SubMOTDPlain = stripMotd(raw[1])
	}
}

// pingBedrockViaAPI 通过 mcsrvstat.us HTTP API 查询 Bedrock 服务器状态。
// 用于 UDP 直连失败时的回退方案，API 使用 HTTPS 因而能穿透 UDP 封锁。
// 私有地址不经过第三方 API（公网 API 无法路由内网地址，且会泄露内网拓扑）。
func pingBedrockViaAPI(host string, port int, timeout time.Duration) (*MCServerStatus, error) {
	if isPrivateTarget(host) {
		return nil, fmt.Errorf("%w（私有地址不通过第三方 API 查询）", ErrNotOnline)
	}
	apiURL := apiEndpoint("bedrock/3", host, port)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RemiliaBot/1.0")
	resp, err := apiHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, apiResponseError(resp)
	}

	var apiResp bedrockAPIResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("api json: %w", err)
	}
	if !apiResp.Online {
		return nil, ErrNotOnline
	}

	result := &MCServerStatus{
		Online:  true,
		Host:    apiResp.Hostname,
		Port:    apiResp.Port,
		Edition: "bedrock",
		Via:     "api",
	}

	version := apiResp.Version
	if apiResp.Protocol.Name != "" {
		version = apiResp.Protocol.Name
	}
	result.Version = version
	result.Protocol = apiResp.Protocol.Version

	applyAPIMOTD(result, apiResp.MOTD.Raw, apiResp.MOTD.Clean)

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
	// Software 服务端软件品牌（Paper/Spigot/Fabric 等，API 可能不返回）。
	Software string `json:"software"`
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

// apiEndpoint 构造 mcsrvstat.us API 地址。
//
// port <= 0 时省略端口：API 会自行做 SRV 解析，而显式写死默认端口反而会
// 跳过 SRV 记录，导致只配置了 SRV 的服务器被误判为离线。
func apiEndpoint(kind, host string, port int) string {
	if port > 0 {
		return fmt.Sprintf("https://api.mcsrvstat.us/%s/%s:%d", kind, host, port)
	}
	return fmt.Sprintf("https://api.mcsrvstat.us/%s/%s", kind, host)
}

// pingJavaViaAPI 通过 mcsrvstat.us HTTP API 查询 Java 版服务器状态。
// 私有地址不经过第三方 API（公网 API 无法路由内网地址，且会泄露内网拓扑）。
func pingJavaViaAPI(host string, port int, timeout time.Duration) (*MCServerStatus, error) {
	if isPrivateTarget(host) {
		return nil, fmt.Errorf("%w（私有地址不通过第三方 API 查询）", ErrNotOnline)
	}
	apiURL := apiEndpoint("3", host, port)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RemiliaBot/1.0")
	resp, err := apiHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, apiResponseError(resp)
	}

	var apiResp javaAPIResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&apiResp); err != nil {
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
		Via:      "api",
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
	}

	applyAPIMOTD(result, apiResp.MOTD.Raw, apiResp.MOTD.Clean)

	// 服务端软件：API 提供时优先采用，否则由版本名推断（零成本兜底）
	result.Software = apiResp.Software
	if result.Software == "" {
		result.Software = guessSoftwareFromVersionName(result.Version)
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
// '&' 仅在其后紧跟合法代码字符时作为前缀，避免误伤正文中的字面 &。
func ParseMotd(raw string) []MotdSegment {
	return parseMotdWithBase(raw, color.White, false)
}

// parseMotdWithBase 同 ParseMotd，但初始颜色/加粗继承调用方——用于 JSON
// 文本组件内嵌旧式颜色码的场景（继承组件自身的颜色与加粗属性）。
// 按 rune 迭代：§ 在 UTF-8 中占两个字节，按字节处理会混入杂散 0xC2。
func parseMotdWithBase(raw string, baseColor color.Color, baseBold bool) []MotdSegment {
	runes := []rune(raw)
	if len(runes) == 0 {
		return []MotdSegment{{Text: "A Minecraft Server", Color: color.White}}
	}
	var segments []MotdSegment
	var buf strings.Builder
	currentColor := baseColor
	bold := baseBold

	flush := func() {
		if buf.Len() > 0 {
			segments = append(segments, MotdSegment{Text: buf.String(), Color: currentColor, Bold: bold})
			buf.Reset()
		}
	}
	for i := 0; i < len(runes); {
		r := runes[i]
		if r == '\u00a7' || (r == '&' && i+1 < len(runes) && isMotdCode(runes[i+1])) {
			i++
			if i >= len(runes) {
				break
			}
			code := toLowerRune(runes[i])
			i++
			switch {
			case code >= '0' && code <= '9' || code >= 'a' && code <= 'f':
				flush()
				if c, ok := motdColors[byte(code)]; ok {
					currentColor = c
				}
				bold = false
			case code == 'l':
				// 样式变更前先落盘缓冲文本：加粗只作用于之后的文本
				flush()
				bold = true
			case code == 'r':
				flush()
				currentColor = color.White
				bold = false
			case code == 'k' || code == 'm' || code == 'n' || code == 'o':
			}
			continue
		}
		buf.WriteRune(r)
		i++
	}
	flush()
	if len(segments) == 0 {
		return []MotdSegment{{Text: raw, Color: color.White}}
	}
	return segments
}

// stripMotd 去除 MOTD 文本中的 § 颜色码（含紧随其后的代码字符）。
// '&'/§ 处理规则与 ParseMotd 一致。按 rune 迭代避免 UTF-8 杂散字节。
func stripMotd(raw string) string {
	runes := []rune(raw)
	var sb strings.Builder
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\u00a7' || (r == '&' && i+1 < len(runes) && isMotdCode(runes[i+1])) {
			i++ // 跳过代码字符
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// isMotdCode 判断 rune 是否为合法的 MOTD 格式代码字符。
func isMotdCode(r rune) bool {
	switch {
	case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		return true
	case r >= 'A' && r <= 'Z':
		r += 32
	}
	switch r {
	case 'l', 'r', 'k', 'm', 'n', 'o':
		return true
	}
	return false
}

func toLowerRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return r
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

// readPacket 读取一个完整的 SLP 数据包（变长长度前缀 + 载荷）。
//
// 长度字段必须做上限校验：变长整数在移位过程中可能溢出为负数，或被恶意
// 服务器设为极大值，直接 make([]byte, length) 会 panic（makeslice: len out
// of range），而 panic 会经 singleflight 传播到同 key 的并发等待者。
func readPacket(conn net.Conn) ([]byte, error) {
	var length int
	{
		var tmp [1]byte
		for shift := 0; ; shift += 7 {
			// MC 的长度前缀最多 5 字节（21 位有效 + 冗余），超出即为异常数据
			if shift > 28 {
				return nil, errors.New("packet length varint too long")
			}
			if _, err := io.ReadFull(conn, tmp[:]); err != nil {
				return nil, err
			}
			length |= int(tmp[0]&0x7F) << shift
			if tmp[0]&0x80 == 0 {
				break
			}
		}
	}
	if length < 0 || length > maxStatusPacketBytes {
		return nil, fmt.Errorf("packet length out of range: %d", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, err
	}
	return data, nil
}

// b64Decode 解码 base64 数据（favicon 等）。
// 优先按标准填充解码，失败时回退到无填充解码（部分服务器省略 padding）。
func b64Decode(s string) ([]byte, error) {
	if data, err := base64.StdEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}
