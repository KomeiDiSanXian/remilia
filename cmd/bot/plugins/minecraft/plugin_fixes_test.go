package minecraft

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestPlugin 构造最小可用的插件实例（不做网络外呼、不依赖存储）。
func newTestPlugin(t *testing.T) *mcPlugin {
	t.Helper()
	cfg := DefaultConfig
	cfg.Timeout = 5 * time.Second
	cfg.CacheTTL = time.Minute
	cfg.Avatars = false
	cfg.EnableQuery = false
	cfg.Cooldown = 0
	return &mcPlugin{
		cfg:      cfg,
		client:   &http.Client{Timeout: 5 * time.Second},
		cache:    newTTLCache[*MCServerStatus](cfg.CacheTTL, 16),
		errCache: newTTLCache[error](cfg.ErrCacheTTL, 16),
		cooldown: newScopeLimiter(cfg.Cooldown, 8),
	}
}

// mcStatus 构造带玩家数据的测试状态（避免到处书写匿名结构体字面量）。
func mcStatus(host string, online, max, sample int) *MCServerStatus {
	st := &MCServerStatus{
		Online:   true,
		Host:     host,
		Port:     25565,
		Latency:  30 * time.Millisecond,
		Edition:  "java",
		Version:  "1.21.1",
		Protocol: 767,
	}
	st.Players.Online = online
	st.Players.Max = max
	for i := range sample {
		st.Players.List = append(st.Players.List, PlayerInfo{Name: fmt.Sprintf("player%d", i)})
	}
	return st
}

// ─── 参数解析与多目标对比 ───────────────────────────────────────────────────

func TestParseQueryArgs(t *testing.T) {
	p := &mcPlugin{cfg: Config{DefaultServer: "default.example.com"}}

	cases := []struct {
		name    string
		args    []string
		edition string
		targets []string
		wantErr bool
	}{
		{name: "空参数用默认服务器", args: nil, targets: []string{"default.example.com"}},
		{name: "单目标", args: []string{"a.example.com"}, targets: []string{"a.example.com"}},
		{name: "版本在前", args: []string{"java", "a"}, edition: "java", targets: []string{"a"}},
		{name: "版本在后", args: []string{"a", "bedrock"}, edition: "bedrock", targets: []string{"a"}},
		{name: "仅版本", args: []string{"bedrock"}, edition: "bedrock", targets: []string{"default.example.com"}},
		{name: "多目标对比", args: []string{"a", "b", "c"}, targets: []string{"a", "b", "c"}},
		{name: "多目标含版本", args: []string{"java", "a", "b"}, edition: "java", targets: []string{"a", "b"}},
		{name: "超过上限", args: []string{"a", "b", "c", "d", "e", "f"}, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			edition, targets, err := p.parseQueryArgs(c.args)
			if c.wantErr {
				if err == nil {
					t.Fatal("期望报错")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseQueryArgs: %v", err)
			}
			if edition != c.edition {
				t.Errorf("edition = %q，期望 %q", edition, c.edition)
			}
			if strings.Join(targets, ",") != strings.Join(c.targets, ",") {
				t.Errorf("targets = %v，期望 %v", targets, c.targets)
			}
		})
	}

	// 未配置默认服务器且不给目标 → 报错而不是空查询
	bare := &mcPlugin{}
	if _, _, err := bare.parseQueryArgs(nil); err == nil {
		t.Error("无默认服务器且无目标应报错")
	}
}

// TestRenderMCCompareCard 验证对比卡片可渲染，且失败项不会中断整体渲染。
func TestRenderMCCompareCard(t *testing.T) {
	entries := []compareEntry{
		{Label: "a.example.com:25565", Status: mcStatus("a.example.com", 5, 100, 2)},
		{Label: "b.example.com:25565", Err: fmt.Errorf("server is offline or unreachable")},
		{Label: "c.example.com:19132", Status: mcStatus("c.example.com", 0, 20, 0)},
	}
	png, err := renderMCCompareCard(entries)
	if err != nil {
		t.Fatalf("renderMCCompareCard: %v", err)
	}
	if len(png) == 0 {
		t.Fatal("对比卡片渲染结果为空")
	}

	text := formatMCCompareText(entries)
	for _, want := range []string{"服务器对比（3）", "a.example.com:25565", "5/100", "b.example.com:25565", "离线", "c.example.com:19132"} {
		if !strings.Contains(text, want) {
			t.Errorf("对比文本缺少 %q:\n%s", want, text)
		}
	}

	if _, err := renderMCCompareCard(nil); err == nil {
		t.Error("空列表应报错")
	}
}

// ─── 冷却限流 ───────────────────────────────────────────────────────────────

func TestScopeLimiter(t *testing.T) {
	l := newScopeLimiter(3*time.Second, 8)
	now := time.Now()

	if got := l.retryAfterAt("k", now); got != 0 {
		t.Errorf("首次应放行, got %v", got)
	}
	l.markAt("k", now)
	if got := l.retryAfterAt("k", now.Add(time.Second)); got != 2*time.Second {
		t.Errorf("冷却中剩余 = %v，期望 2s", got)
	}
	if got := l.retryAfterAt("k", now.Add(3*time.Second)); got != 0 {
		t.Errorf("冷却结束后应放行, got %v", got)
	}
	if got := l.retryAfterAt("other", now.Add(time.Second)); got != 0 {
		t.Errorf("不同 key 不应互相影响, got %v", got)
	}

	// interval <= 0 表示禁用
	off := newScopeLimiter(0, 8)
	off.markAt("k", now)
	if got := off.retryAfterAt("k", now); got != 0 {
		t.Errorf("禁用时不应限流, got %v", got)
	}

	// 容量上限下不应 panic，且新 key 仍能记录
	small := newScopeLimiter(time.Minute, 2)
	for i := range 5 {
		key := fmt.Sprintf("key%d", i)
		if got := small.retryAfterAt(key, now); got != 0 {
			t.Errorf("%s 首次应放行, got %v", key, got)
		}
		small.markAt(key, now)
	}
}

// ─── GS4 策略 ───────────────────────────────────────────────────────────────

// TestGS4SkipReason 回归：GS4 曾仅在「在线人数 > SLP 样本数」时尝试，
// 导致 0 人在线或样本已完整的服务器永远拿不到服务端/插件信息。
func TestGS4SkipReason(t *testing.T) {
	base := func(mode string) *mcPlugin {
		return &mcPlugin{cfg: Config{EnableQuery: true, DirectQuery: true, GS4Mode: mode}}
	}

	// 前置条件不满足 → 跳过
	if r := base(GS4ModeAuto).gs4SkipReason(mcStatus("a", 5, 100, 0)); r != "" {
		t.Errorf("样本不完整时应尝试, got skip=%q", r)
	}
	noQuery := base(GS4ModeAlways)
	noQuery.cfg.EnableQuery = false
	if r := noQuery.gs4SkipReason(mcStatus("a", 5, 100, 0)); r == "" {
		t.Error("enable_query=false 应跳过")
	}
	viaAPI := base(GS4ModeAlways)
	viaAPI.cfg.DirectQuery = false
	if r := viaAPI.gs4SkipReason(mcStatus("a", 5, 100, 0)); r == "" {
		t.Error("direct_query=false 应跳过（GS4 为裸 UDP）")
	}
	bedrock := mcStatus("a", 5, 100, 0)
	bedrock.Edition = "bedrock"
	if r := base(GS4ModeAlways).gs4SkipReason(bedrock); r == "" {
		t.Error("非 Java 版应跳过")
	}
	if r := base(GS4ModeAlways).gs4SkipReason(nil); r == "" {
		t.Error("状态为空应跳过")
	}
	if r := base(GS4ModeNever).gs4SkipReason(mcStatus("a", 5, 100, 0)); r == "" {
		t.Error("gs4_mode=never 应跳过")
	}

	// always：即使样本完整 / 0 人在线也尝试
	if r := base(GS4ModeAlways).gs4SkipReason(mcStatus("a", 5, 100, 5)); r != "" {
		t.Errorf("always 应总是尝试, got skip=%q", r)
	}
	if r := base(GS4ModeAlways).gs4SkipReason(mcStatus("a", 0, 100, 0)); r != "" {
		t.Errorf("always 在 0 人在线时也应尝试, got skip=%q", r)
	}

	// auto：样本已完整 → 跳过；样本不完整 → 尝试
	if r := base(GS4ModeAuto).gs4SkipReason(mcStatus("a", 5, 100, 5)); r == "" {
		t.Error("auto 且样本完整应跳过")
	}
	if r := base(GS4ModeAuto).gs4SkipReason(mcStatus("a", 0, 100, 0)); r == "" {
		t.Error("auto 且 0 人在线应跳过（需要 gs4_mode=always）")
	}
	if r := base(GS4ModeAuto).gs4SkipReason(mcStatus("a", 40, 100, 12)); r != "" {
		t.Error("auto 且人数多于样本数应尝试")
	}
}

func TestNormalizeGS4Mode(t *testing.T) {
	cases := map[string]string{
		"always": GS4ModeAlways,
		"ALWAYS": GS4ModeAlways,
		" never": GS4ModeNever,
		"auto":   GS4ModeAuto,
		"bogus":  GS4ModeAuto,
		"":       GS4ModeAuto,
	}
	for in, want := range cases {
		if got := normalizeGS4Mode(in, GS4ModeAuto); got != want {
			t.Errorf("normalizeGS4Mode(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// ─── singleflight 并发语义 ──────────────────────────────────────────────────

// startSlowSLPServer 是 SLP 仿真服务器，收到状态请求后延迟 delay 再响应，
// 用于构造「首个调用方在共享查询进行中被取消」的场景。
func startSlowSLPServer(t *testing.T, statusJSON string, delay time.Duration) (port int, stop func()) {
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
				_ = c.SetDeadline(time.Now().Add(10 * time.Second))
				if _, err := readPacket(c); err != nil { // 握手
					return
				}
				if _, err := readPacket(c); err != nil { // 状态请求
					return
				}
				time.Sleep(delay)
				payload := (&packetBuffer{}).withString(statusJSON)
				_ = sendPacket(c, 0x00, payload)
			}(conn)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, func() { _ = ln.Close() }
}

// TestQueryCallerCancelDoesNotPoisonSharedQuery 回归：此前 singleflight 的 fn
// 直接使用首个调用方的 ctx，导致：
//  1. 任一等待者的取消会让其他等待者一起失败；
//  2. context.Canceled 被当作"服务器离线"写入 15s 负缓存。
func TestQueryCallerCancelDoesNotPoisonSharedQuery(t *testing.T) {
	statusJSON := `{"version":{"name":"1.21.1","protocol":767},"players":{"max":10,"online":2},"description":"Slow"}`
	port, stop := startSlowSLPServer(t, statusJSON, 500*time.Millisecond)
	defer stop()

	p := newTestPlugin(t)
	host := "127.0.0.1"

	// 调用方 A：300ms 后取消，此时共享查询仍在进行
	ctxA, cancelA := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancelA()

	done := make(chan error, 1)
	go func() {
		_, err := p.query(ctxA, host, port, "java")
		done <- err
	}()

	time.Sleep(100 * time.Millisecond) // 确保 A 已进入共享查询

	// 调用方 B：不应因 A 的取消而失败
	status, errB := p.query(context.Background(), host, port, "java")
	if errB != nil {
		t.Fatalf("第二个调用方不应受首个调用方取消影响: %v", errB)
	}
	if !status.Online || status.Version != "1.21.1" {
		t.Errorf("共享查询结果异常: %+v", status)
	}

	if errA := <-done; errA == nil {
		t.Error("被取消的调用方应返回错误")
	}

	// 取消不应被当成"离线"写入负缓存
	key := fmt.Sprintf("%s|%d|java", host, port)
	if _, ok := p.errCache.get(key); ok {
		t.Error("调用方取消不应写入负缓存")
	}
}

// TestQueryCancelledCallerReturnsPromptly 调用方取消后应立即返回，而不是干等共享查询。
func TestQueryCancelledCallerReturnsPromptly(t *testing.T) {
	statusJSON := `{"version":{"name":"1.21.1","protocol":767},"players":{"max":10,"online":0},"description":"Slow"}`
	port, stop := startSlowSLPServer(t, statusJSON, 2*time.Second)
	defer stop()

	p := newTestPlugin(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := p.query(ctx, "127.0.0.1", port, "java"); err == nil {
		t.Fatal("已取消的调用方应返回错误")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("取消后应迅速返回，实际耗时 %v", elapsed)
	}
}

// ─── 负缓存与配置 ───────────────────────────────────────────────────────────

// TestNegativeCacheRespectsConfig 验证 err_cache_ttl=0 时关闭失败负缓存。
func TestNegativeCacheRespectsConfig(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close() // 端口无人监听 → 连接被拒绝

	key := fmt.Sprintf("127.0.0.1|%d|java", port)

	on := newTestPlugin(t)
	on.cfg.ErrCacheTTL = 30 * time.Second
	on.errCache = newTTLCache[error](on.cfg.ErrCacheTTL, 16)
	if _, err := on.query(context.Background(), "127.0.0.1", port, "java"); err == nil {
		t.Fatal("离线端口应报错")
	}
	if _, ok := on.errCache.get(key); !ok {
		t.Error("err_cache_ttl>0 时应写入负缓存")
	}

	off := newTestPlugin(t)
	off.cfg.ErrCacheTTL = 0
	off.errCache = newTTLCache[error](0, 16)
	if _, err := off.query(context.Background(), "127.0.0.1", port, "java"); err == nil {
		t.Fatal("离线端口应报错")
	}
	if _, ok := off.errCache.get(key); ok {
		t.Error("err_cache_ttl=0 时不应写入负缓存")
	}
}

// ─── 内网地址防护与超时预算 ─────────────────────────────────────────────────

// TestBlockPrivateTargets 验证 block_private_targets 对直连路径生效：
// 即使本地仿真服务器在线，开启该开关后也必须被拒绝。
func TestBlockPrivateTargets(t *testing.T) {
	statusJSON := `{"version":{"name":"1.21.1","protocol":767},"players":{"max":10,"online":1},"description":"Local"}`
	port, stop := startFakeSLPServer(t, statusJSON)
	defer stop()

	// 默认（false）：内网地址允许直连（自建 bot 的常见场景）
	allow := newTestPlugin(t)
	status, err := allow.query(context.Background(), "127.0.0.1", port, "java")
	if err != nil {
		t.Fatalf("默认应允许查询内网地址: %v", err)
	}
	if !status.Online {
		t.Error("本地仿真服务器应在线")
	}

	// 开启防护：直接拒绝，且不发起连接
	deny := newTestPlugin(t)
	deny.cfg.BlockPrivateTargets = true
	_, err = deny.query(context.Background(), "127.0.0.1", port, "java")
	if err == nil {
		t.Fatal("开启防护后应拒绝内网地址")
	}
	if !strings.Contains(err.Error(), "私有地址") {
		t.Errorf("错误信息应说明原因, got %v", err)
	}
}

// TestRemainingTimeoutFloor 验证剩余预算下限取 min(2s, total)：
// 既保证回退链路仍有一次机会，又不会在小 timeout 配置下把总耗时拉得过长。
func TestRemainingTimeoutFloor(t *testing.T) {
	// 刚起步：约等于总预算
	if got := remainingTimeout(time.Now(), 5*time.Second); got < 4*time.Second || got > 5*time.Second {
		t.Errorf("剩余预算 = %v，期望约 5s", got)
	}
	// 预算耗尽 + 大总预算：下限 2s
	if got := remainingTimeout(time.Now().Add(-time.Hour), 30*time.Second); got != 2*time.Second {
		t.Errorf("下限 = %v，期望 2s", got)
	}
	// 预算耗尽 + 小总预算：下限不超过总预算（旧实现固定 2s 会超出配置值）
	if got := remainingTimeout(time.Now().Add(-time.Hour), 500*time.Millisecond); got != 500*time.Millisecond {
		t.Errorf("小预算下限 = %v，期望 500ms", got)
	}
}

// TestMotdFontCaching 验证字体解析结果被缓存复用（每个请求重新读盘 + 解析
// 数十 MB 的 CJK 字体代价过高），且并发调用安全。
func TestMotdFontCaching(t *testing.T) {
	first, err := motdFontForRender()
	if err != nil {
		t.Fatalf("motdFontForRender: %v", err)
	}
	again, err := motdFontForRender()
	if err != nil {
		t.Fatalf("第二次 motdFontForRender: %v", err)
	}
	if first != again {
		t.Error("字体应被缓存复用（同一 *opentype.Font 实例）")
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := motdFontForRender(); err != nil {
				t.Errorf("并发获取字体失败: %v", err)
			}
		})
	}
	wg.Wait()
}

// ─── 健康探针 ───────────────────────────────────────────────────────────────

func TestPluginHealthChecker(t *testing.T) {
	p := newTestPlugin(t)
	p.cache.set("a|1|java", mcStatus("a", 1, 10, 0))
	p.errCache.set("b|2|java", fmt.Errorf("offline"))

	c := pluginHealthChecker{p: p}
	if c.Name() != "minecraft" {
		t.Errorf("Name() = %q", c.Name())
	}
	result := c.Check(context.Background())
	if result.Status != "healthy" {
		t.Errorf("Status = %q，本地检查不应影响整体健康", result.Status)
	}
	if got := result.Metadata["result_cache_entries"]; got != 1 {
		t.Errorf("result_cache_entries = %v", got)
	}
	if got := result.Metadata["error_cache_entries"]; got != 1 {
		t.Errorf("error_cache_entries = %v", got)
	}
	if _, ok := result.Metadata["srv_cache_entries"]; !ok {
		t.Error("缺少 srv_cache_entries")
	}
	if got := result.Metadata["gs4_mode"]; got != GS4ModeAuto {
		t.Errorf("gs4_mode = %v", got)
	}

	// 未初始化的实例也不应 panic（Setup 之前被调用）
	if r := (pluginHealthChecker{}).Check(context.Background()); r.Status != "healthy" {
		t.Errorf("空实例 Status = %q", r.Status)
	}
}

func TestTTLCacheSize(t *testing.T) {
	var nilCache *ttlCache[string]
	if got := nilCache.size(); got != 0 {
		t.Errorf("nil 缓存 size = %d", got)
	}
	c := newTTLCache[string](time.Minute, 4)
	if got := c.size(); got != 0 {
		t.Errorf("空缓存 size = %d", got)
	}
	c.set("a", "1")
	c.set("b", "2")
	if got := c.size(); got != 2 {
		t.Errorf("size = %d，期望 2", got)
	}
}
