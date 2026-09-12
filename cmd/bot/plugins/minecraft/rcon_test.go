package minecraft

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ─── RCON 仿真服务器 ──────────────────────────────────────────────────────────
//
// 服务端侧独立实现协议读写（不复用客户端代码），
// 这样客户端的分包重组、认证判定等问题才会真正暴露出来。

type fakePacket struct {
	id   int32
	typ  int32
	body string
}

func readFakePacket(conn net.Conn) (fakePacket, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return fakePacket{}, err
	}
	size := int32(binary.LittleEndian.Uint32(hdr[:]))
	if size < 10 || size > rconMaxPayload {
		return fakePacket{}, errors.New("bad packet size")
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fakePacket{}, err
	}
	return fakePacket{
		id:   int32(binary.LittleEndian.Uint32(buf[0:4])),
		typ:  int32(binary.LittleEndian.Uint32(buf[4:8])),
		body: strings.TrimRight(string(buf[8:]), "\x00"),
	}, nil
}

func writeFakePacket(conn net.Conn, id, typ int32, body string) error {
	buf := make([]byte, 0, 14+len(body))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(10+len(body)))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(id))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(typ))
	buf = append(buf, body...)
	buf = append(buf, 0x00, 0x00)
	_, err := conn.Write(buf)
	return err
}

// chunkRCON 把输出切成满包 + 尾包，模拟真实服务端对长输出的分片方式
// （尾包可能为空，是客户端判断"输出结束"的依据）。
func chunkRCON(s string) []string {
	var out []string
	for len(s) > rconBodyMax {
		out = append(out, s[:rconBodyMax])
		s = s[rconBodyMax:]
	}
	return append(out, s)
}

type fakeRCON struct {
	ln        net.Listener
	password  string
	respond   func(cmd string) string
	onCommand func(cmd string) bool // 返回 false 表示不响应并断开连接
	conns     atomic.Int32
}

func (f *fakeRCON) addr() string { return f.ln.Addr().String() }

func (f *fakeRCON) serve(conn net.Conn) {
	f.conns.Add(1)
	defer conn.Close()
	for {
		pkt, err := readFakePacket(conn)
		if err != nil {
			return
		}
		switch pkt.typ {
		case rconTypeAuth:
			if pkt.body != f.password {
				// 认证失败：请求 ID 置 -1
				_ = writeFakePacket(conn, -1, rconTypeResponse, "")
				return
			}
			_ = writeFakePacket(conn, pkt.id, rconTypeResponse, "")
		case rconTypeCommand:
			if f.onCommand != nil && !f.onCommand(pkt.body) {
				return
			}
			out := ""
			if f.respond != nil {
				out = f.respond(pkt.body)
			}
			for _, chunk := range chunkRCON(out) {
				if err := writeFakePacket(conn, pkt.id, rconTypeResponse, chunk); err != nil {
					return
				}
			}
		default:
			return
		}
	}
}

func startFakeRCON(t *testing.T, password string, respond func(string) string) *fakeRCON {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeRCON{ln: ln, password: password, respond: respond}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

// ─── 客户端行为 ───────────────────────────────────────────────────────────────

func TestRCONCommandRoundTrip(t *testing.T) {
	f := startFakeRCON(t, "s3cret", func(cmd string) string {
		return "There are 0 of a max of 20 players online"
	})

	c, err := dialRCON(f.addr(), "s3cret", 3*time.Second)
	if err != nil {
		t.Fatalf("dialRCON: %v", err)
	}
	defer c.Close()

	out, err := c.Command("list")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if out != "There are 0 of a max of 20 players online" {
		t.Errorf("输出 = %q", out)
	}
}

func TestRCONAuthFailureRejected(t *testing.T) {
	f := startFakeRCON(t, "right", nil)

	_, err := dialRCON(f.addr(), "wrong", 3*time.Second)
	if !errors.Is(err, ErrRconAuth) {
		t.Fatalf("密码错误应返回 ErrRconAuth, got %v", err)
	}
}

func TestRCONUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // 立刻关闭，制造连接拒绝

	_, err = dialRCON(addr, "pw", time.Second)
	if !errors.Is(err, ErrRconUnreachable) {
		t.Fatalf("应返回 ErrRconUnreachable, got %v", err)
	}
}

// TestRCONMultipacketReassembly 验证超过单包上限的输出被完整重组。
// 这是最容易出错的地方：只读一个包会截断，读多了会阻塞。
// 长度取 7000：跨 2 个包（超过 rconBodyMax），但不触发 rconMaxOutput 截断。
func TestRCONMultipacketReassembly(t *testing.T) {
	long := strings.Repeat("abcdefghij", 700)
	if len(long) <= rconBodyMax || len(long) > rconMaxOutput {
		t.Fatalf("测试数据需落在 (%d, %d] 区间，实际 %d", rconBodyMax, rconMaxOutput, len(long))
	}
	f := startFakeRCON(t, "pw", func(string) string { return long })

	c, err := dialRCON(f.addr(), "pw", 3*time.Second)
	if err != nil {
		t.Fatalf("dialRCON: %v", err)
	}
	defer c.Close()

	out, err := c.Command("list")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if out != long {
		t.Errorf("多包重组结果长度 = %d, 期望 %d", len(out), len(long))
	}
}

// TestRCONOutputTruncated 验证超长输出被截断而非无限累积。
func TestRCONOutputTruncated(t *testing.T) {
	huge := strings.Repeat("x", rconMaxOutput*3)
	f := startFakeRCON(t, "pw", func(string) string { return huge })

	c, err := dialRCON(f.addr(), "pw", 3*time.Second)
	if err != nil {
		t.Fatalf("dialRCON: %v", err)
	}
	defer c.Close()

	out, err := c.Command("list")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if len(out) < rconMaxOutput {
		t.Fatalf("输出过短: %d", len(out))
	}
	if len(out) > rconMaxOutput+64 {
		t.Errorf("输出未截断: %d", len(out))
	}
	if !strings.Contains(out, "已截断") {
		t.Error("截断输出应带提示")
	}
}

// TestRCONReconnectAfterDrop 验证连接被服务端断开后，下一次调用能自动重连。
func TestRCONReconnectAfterDrop(t *testing.T) {
	f := startFakeRCON(t, "pw", func(cmd string) string {
		if cmd == "list" {
			return "ok"
		}
		return "unexpected"
	})
	f.onCommand = func(cmd string) bool { return cmd != "drop" }

	c, err := dialRCON(f.addr(), "pw", 3*time.Second)
	if err != nil {
		t.Fatalf("dialRCON: %v", err)
	}
	defer c.Close()

	// 服务端收到 drop 后直接断开且不响应 → 本次必然失败
	if _, err := c.Command("drop"); err == nil {
		t.Fatal("断连的命令应返回错误")
	}
	// 失败后连接被丢弃，下一次调用应重连成功
	out, err := c.Command("list")
	if err != nil {
		t.Fatalf("重连后命令失败: %v", err)
	}
	if out != "ok" {
		t.Errorf("重连后输出 = %q", out)
	}
	if got := f.conns.Load(); got < 2 {
		t.Errorf("应发生重连（连接数 >= 2），实际 %d", got)
	}
}

// ─── 连接池 ───────────────────────────────────────────────────────────────────

func TestRCONPoolReusesConnection(t *testing.T) {
	f := startFakeRCON(t, "pw", func(cmd string) string { return cmd })

	pool := newRconPool()
	defer pool.Close()

	for i := 0; i < 3; i++ {
		if _, err := pool.Do("srv", f.addr(), "pw", 3*time.Second, "list"); err != nil {
			t.Fatalf("Do #%d: %v", i, err)
		}
	}
	if got := f.conns.Load(); got != 1 {
		t.Errorf("连接复用时连接数应为 1，实际 %d", got)
	}
}

// TestRCONPoolReplacesOnCredentialChange 验证密码变更后不复用旧连接，
// 否则热重载改写密码会继续用旧凭据。
func TestRCONPoolReplacesOnCredentialChange(t *testing.T) {
	f := startFakeRCON(t, "pw2", func(cmd string) string { return cmd })

	pool := newRconPool()
	defer pool.Close()

	// 旧密码：认证失败
	if _, err := pool.Do("srv", f.addr(), "pw1", 3*time.Second, "list"); !errors.Is(err, ErrRconAuth) {
		t.Fatalf("旧密码应认证失败, got %v", err)
	}
	// 新密码：应使用新连接认证成功
	if _, err := pool.Do("srv", f.addr(), "pw2", 3*time.Second, "list"); err != nil {
		t.Fatalf("新密码应成功: %v", err)
	}
}

func TestRCONPoolCloseIdle(t *testing.T) {
	f := startFakeRCON(t, "pw", func(cmd string) string { return cmd })

	pool := newRconPool()
	defer pool.Close()

	if _, err := pool.Do("srv", f.addr(), "pw", 3*time.Second, "list"); err != nil {
		t.Fatalf("Do: %v", err)
	}

	pool.mu.Lock()
	c := pool.clients["srv"]
	pool.mu.Unlock()
	if c == nil || c.conn == nil {
		t.Fatal("连接未建立")
	}

	// 把上次使用时间推到过去，模拟长期空闲
	c.mu.Lock()
	c.lastUsed = time.Now().Add(-time.Hour)
	c.mu.Unlock()

	if n := pool.CloseIdle(time.Minute); n != 1 {
		t.Errorf("应回收 1 条连接，实际 %d", n)
	}
	if n := pool.CloseIdle(time.Minute); n != 0 {
		t.Errorf("重复回收应为 0，实际 %d", n)
	}
}

// TestRCONPoolCloseIdleSkipsBusy 验证执行中的连接不会被回收打断。
func TestRCONPoolCloseIdleSkipsBusy(t *testing.T) {
	f := startFakeRCON(t, "pw", func(cmd string) string { return cmd })

	pool := newRconPool()
	defer pool.Close()

	if _, err := pool.Do("srv", f.addr(), "pw", 3*time.Second, "list"); err != nil {
		t.Fatalf("Do: %v", err)
	}
	pool.mu.Lock()
	c := pool.clients["srv"]
	pool.mu.Unlock()

	// 模拟命令执行中：持有客户端锁，回收应跳过
	c.mu.Lock()
	c.lastUsed = time.Now().Add(-time.Hour)
	n := pool.CloseIdle(time.Minute)
	c.mu.Unlock()

	if n != 0 {
		t.Errorf("执行中的连接不应被回收，实际回收 %d", n)
	}
	if c.conn == nil {
		t.Error("连接被误关闭")
	}
}

// TestRCONRejectsMalformedPacketLength 验证畸形长度字段不会导致越界分配。
func TestRCONRejectsMalformedPacketLength(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var hdr [4]byte
		if _, err := io.ReadFull(conn, hdr[:]); err != nil {
			return
		}
		var out [4]byte
		binary.LittleEndian.PutUint32(out[:], uint32(1<<30)) // 声称 1GiB
		_, _ = conn.Write(out[:])
		time.Sleep(200 * time.Millisecond)
	}()

	c, err := dialRCON(ln.Addr().String(), "pw", 2*time.Second)
	if err == nil {
		c.Close()
		t.Fatal("畸形包长度应在认证阶段即报错")
	}
}
