// Package minecraft rcon.go — Minecraft RCON（Remote Console）协议客户端与连接池。
//
// 协议格式（参考 https://wiki.vg/RCON），每个包为：
//
//	[int32 LE 长度][int32 LE 请求 ID][int32 LE 类型][正文][0x00 0x00]
//
// 其中"长度"字段 = 4(ID) + 4(类型) + len(正文) + 2(结尾 NUL)，不含自身 4 字节。
//
// 已知实现差异（决定了这里为什么这样写）：
//   - 认证失败时，服务端回一个请求 ID 为 -1 的包（不是专门的错误类型）；
//   - 超过单包上限的输出会被拆成多个"满包"，末尾再补一个不满包，
//     因此读取必须以"收到不满包"为结束条件，而不是读一次就收工；
//   - 请求 ID 的回显并不总是一致（部分服务端固定回 1），
//     所以这里不依赖 ID 匹配响应，而是靠"同一连接串行执行"来保证不串包。
package minecraft

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// RCON 包类型。
const (
	rconTypeResponse = 0 // 服务端返回的命令输出
	rconTypeCommand  = 2 // 客户端发送的命令
	rconTypeAuth     = 3 // 客户端发送的登录密码
)

const (
	// rconMaxPayload 单个 RCON 包的最大长度（含 4 字节长度字段自身）。
	rconMaxPayload = 4096
	// rconBodyMax 单个包的正文上限（扣除 长度/ID/类型 共 10 字节头部）。
	rconBodyMax = rconMaxPayload - 10
	// rconMaxPackets 单条命令最多读取的响应包数。
	// 防御性上限：遇到持续回包的畸形服务端时不会让读取循环无限进行。
	rconMaxPackets = 256
	// rconMaxOutput 单条命令累计回显的正文上限，避免超长输出撑爆消息体。
	rconMaxOutput = 8192
	// DefaultRCONPort RCON 默认端口。
	DefaultRCONPort = 25575
	// rconIdleTimeout RCON 空闲连接回收时长。
	// 服务器控制台常被成串使用（say + whitelist + list），保留一段时间
	// 可以省下每条命令的 TCP 握手与认证往返。
	rconIdleTimeout = 5 * time.Minute
	// rconIdleSweep 空闲连接回收的检查间隔。
	rconIdleSweep = time.Minute
)

var (
	// ErrRconAuth 认证失败（密码错误）。
	ErrRconAuth = errors.New("RCON 认证失败：密码错误")
	// ErrRconUnreachable 无法建立 RCON 连接。
	ErrRconUnreachable = errors.New("无法连接 RCON 端口")
)

// rconPacket 一个已解析的 RCON 包。
type rconPacket struct {
	ID   int32
	Type int32
	Body string
}

// rconClient 单个服务器上的 RCON 连接。
//
// RCON 是"一问一答"协议：同一条连接上不能同时等待两条命令的响应，
// 因此用互斥锁把命令串行化（这也使得响应与请求的对应关系无需依赖 ID）。
// 任何读写失败都会丢弃连接，下次调用自动重连并重新认证。
type rconClient struct {
	addr     string
	password string
	timeout  time.Duration

	mu       sync.Mutex
	conn     net.Conn
	lastUsed time.Time
	// nextID 命令包的自增请求 ID（0 已被登录占用）。
	nextID int32
}

// dialRCON 建立连接并完成认证。返回的客户端可直接执行命令。
func dialRCON(addr, password string, timeout time.Duration) (*rconClient, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	c := &rconClient{addr: addr, password: password, timeout: timeout}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.connectLocked(); err != nil {
		return nil, err
	}
	return c, nil
}

// connectLocked 建立 TCP 连接并认证。调用方须持有 c.mu。
func (c *rconClient) connectLocked() error {
	conn, err := net.DialTimeout("tcp", c.addr, c.timeout)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrRconUnreachable, c.addr, err)
	}
	c.conn = conn
	c.lastUsed = time.Now()
	if err := c.authLocked(); err != nil {
		c.closeLocked()
		return err
	}
	return nil
}

// authLocked 执行登录握手。调用方须持有 c.mu 且 c.conn 非 nil。
func (c *rconClient) authLocked() error {
	if err := c.writePacket(0, rconTypeAuth, c.password); err != nil {
		return err
	}
	resp, err := c.readPacket()
	if err != nil {
		return err
	}
	// 认证失败的唯一信号：响应包请求 ID 为 -1。
	if resp.ID == -1 {
		return ErrRconAuth
	}
	return nil
}

// Command 在连接上执行一条控制台命令，返回服务端输出。
//
// 命令在同一连接上串行执行；连接未建立或此前已失效时自动重连。
func (c *rconClient) Command(cmd string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		if err := c.connectLocked(); err != nil {
			return "", err
		}
	}

	c.nextID++
	if err := c.writePacket(c.nextID, rconTypeCommand, cmd); err != nil {
		c.closeLocked()
		return "", err
	}
	out, err := c.readResponse()
	if err != nil {
		c.closeLocked()
		return "", err
	}
	c.lastUsed = time.Now()
	return out, nil
}

// Close 关闭底层连接。可重复调用。
func (c *rconClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeLocked()
}

// closeLocked 关闭并丢弃连接。调用方须持有 c.mu。
func (c *rconClient) closeLocked() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// lastUsedAt 返回上次成功使用的时刻（供空闲回收判断）。
func (c *rconClient) lastUsedAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastUsed
}

// writePacket 发送一个 RCON 包。
func (c *rconClient) writePacket(id, typ int32, body string) error {
	if c.conn == nil {
		return errors.New("RCON 连接未建立")
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(c.timeout))

	// 长度字段不含自身：ID(4) + 类型(4) + 正文 + 两个结尾 NUL
	buf := make([]byte, 0, 14+len(body))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(10+len(body)))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(id))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(typ))
	buf = append(buf, body...)
	buf = append(buf, 0x00, 0x00)

	_, err := c.conn.Write(buf)
	return err
}

// readPacket 读取并解析一个 RCON 包。
func (c *rconClient) readPacket() (*rconPacket, error) {
	_ = c.conn.SetReadDeadline(time.Now().Add(c.timeout))

	var hdr [4]byte
	if _, err := io.ReadFull(c.conn, hdr[:]); err != nil {
		return nil, err
	}
	size := int32(binary.LittleEndian.Uint32(hdr[:]))
	// 正文至少要有 ID + 类型 共 8 字节，加上结尾 NUL 即 10；
	// 超过单包上限说明是畸形或非 RCON 数据。
	if size < 10 || size > rconMaxPayload {
		return nil, fmt.Errorf("RCON 包长度异常: %d", size)
	}

	payload := make([]byte, size)
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return nil, err
	}
	body := strings.TrimRight(string(payload[8:]), "\x00")
	return &rconPacket{
		ID:   int32(binary.LittleEndian.Uint32(payload[0:4])),
		Type: int32(binary.LittleEndian.Uint32(payload[4:8])),
		Body: body,
	}, nil
}

// readResponse 连续读取响应包直到输出结束，返回拼接后的正文。
//
// 结束条件：收到正文不足一个满包（rconBodyMax）的包。
// 长输出被拆成多个满包时继续读，直到服务端补上的尾包（通常为空包）。
func (c *rconClient) readResponse() (string, error) {
	var sb strings.Builder
	for i := 0; i < rconMaxPackets; i++ {
		pkt, err := c.readPacket()
		if err != nil {
			return "", err
		}
		sb.WriteString(pkt.Body)
		if len(pkt.Body) < rconBodyMax {
			return clipRCONOutput(sb.String()), nil
		}
		if sb.Len() >= rconMaxOutput {
			break
		}
	}
	return clipRCONOutput(sb.String()), nil
}

// clipRCONOutput 截断超长输出，保留尾部提示。
func clipRCONOutput(s string) string {
	if len(s) <= rconMaxOutput {
		return s
	}
	return s[:rconMaxOutput] + "\n…（输出过长已截断）"
}

// rconPool 按服务器名维护 RCON 长连接。
//
// 目的是复用连接：RCON 每次命令都要重新 TCP 握手与认证，而服务器控制台
// 常常被连续使用（say + whitelist + list）。同时保证同一服务器上的命令
// 串行执行（rconClient 内部互斥锁），不会出现响应串包。
type rconPool struct {
	mu      sync.Mutex
	clients map[string]*rconClient
}

// newRconPool 创建连接池。
func newRconPool() *rconPool {
	return &rconPool{clients: make(map[string]*rconClient)}
}

// Do 在指定服务器上执行一条命令。
func (p *rconPool) Do(name, addr, password string, timeout time.Duration, cmd string) (string, error) {
	return p.client(name, addr, password, timeout).Command(cmd)
}

// client 返回服务器对应的客户端，必要时新建或替换。
//
// 地址或密码变更（配置热重载）时丢弃旧连接，避免继续用旧凭据。
func (p *rconPool) client(name, addr, password string, timeout time.Duration) *rconClient {
	p.mu.Lock()
	defer p.mu.Unlock()

	if c, ok := p.clients[name]; ok {
		if c.addr == addr && c.password == password {
			return c
		}
		_ = c.Close()
		delete(p.clients, name)
	}
	c := &rconClient{addr: addr, password: password, timeout: timeout}
	p.clients[name] = c
	return c
}

// CloseIdle 关闭空闲超过 idle 的连接，返回关闭数量。
//
// 正在执行命令的连接（互斥锁已被持有）会被跳过，不会打断进行中的命令。
func (p *rconPool) CloseIdle(idle time.Duration) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	closed := 0
	for name, c := range p.clients {
		if !c.mu.TryLock() {
			continue // 正在执行命令
		}
		stale := c.conn != nil && now.Sub(c.lastUsed) > idle
		if stale {
			_ = c.closeLocked()
		}
		c.mu.Unlock()
		if stale {
			delete(p.clients, name)
			closed++
		}
	}
	return closed
}

// Close 关闭全部连接。
func (p *rconPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, c := range p.clients {
		_ = c.Close()
		delete(p.clients, name)
	}
}
