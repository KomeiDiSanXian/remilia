package remilia

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// BotManager 管理多个 Bot 实例的生命周期。
//
// 每个 Bot 独立持有自己的 Engine、Adapter 和 lifecycle，
// BotManager 仅负责统一的启停调度和命名查找。
//
// 典型使用场景：
//   - 同一进程运行多个 QQ Bot 账号（测试账号 + 生产账号）
//   - 同一逻辑服务的多个机器人实例（分担消息负载）
//   - 灰度发布：旧 Bot 继续运行，新 Bot 逐步接入
//
// 示例：
//
//	mgr := remilia.NewBotManager()
//	mgr.Add("prod", prodBot)
//	mgr.Add("test", testBot)
//	if err := mgr.StartAll(ctx); err != nil {
//	    log.Fatal(err)
//	}
//	mgr.WaitForShutdown()
type BotManager struct {
	mu   sync.RWMutex
	bots map[string]*Bot
	// 保留注册顺序，确保 StartAll/StopAll 行为可预期
	order []string
}

// NewBotManager 创建一个空的 BotManager。
func NewBotManager() *BotManager {
	return &BotManager{
		bots:  make(map[string]*Bot),
		order: make([]string, 0),
	}
}

// Add 注册一个命名 Bot 实例。
// 若同名 Bot 已存在，返回错误；若 bot 为 nil，同样返回错误。
func (m *BotManager) Add(name string, bot *Bot) error {
	if name == "" {
		return fmt.Errorf("botmanager: bot name must not be empty")
	}
	if bot == nil {
		return fmt.Errorf("botmanager: bot %q must not be nil", name)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.bots[name]; exists {
		return fmt.Errorf("botmanager: bot %q already registered", name)
	}
	m.bots[name] = bot
	m.order = append(m.order, name)
	logger.Infof("[BotManager] Registered bot %q", name)
	return nil
}

// MustAdd 注册 Bot，失败时 panic。
//
// 此方法仅适用于程序 main() 初始化阶段，在配置已确认正确的前提下使用，
// 目的是减少样板错误处理代码。
//
// 警告：请勿在运行时（如插件回调、HTTP handler、goroutine 中）调用此方法，
// 否则 panic 将导致整个进程崩溃。运行时场景请使用 [BotManager.Add]。
func (m *BotManager) MustAdd(name string, bot *Bot) *BotManager {
	if err := m.Add(name, bot); err != nil {
		panic(err)
	}
	return m
}

// Get 按名称获取 Bot 实例。若不存在，返回 nil, false。
func (m *BotManager) Get(name string) (*Bot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bot, ok := m.bots[name]
	return bot, ok
}

// MustGet 按名称获取 Bot 实例，若不存在则 panic。
//
// 此方法仅适用于程序 main() 初始化阶段，在确信目标 Bot 已通过 Add/MustAdd 注册的前提下使用。
//
// 警告：请勿在运行时（如插件回调、HTTP handler、goroutine 中）调用此方法，
// 否则 panic 将导致整个进程崩溃。运行时场景请使用 [BotManager.Get]。
func (m *BotManager) MustGet(name string) *Bot {
	bot, ok := m.Get(name)
	if !ok {
		panic(fmt.Sprintf("botmanager: bot %q not found", name))
	}
	return bot
}

// Remove 从管理器中移除 Bot。
// 注意：Remove 不会自动停止 Bot，请在 Remove 前先调用 bot.Stop()。
func (m *BotManager) Remove(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.bots[name]; !exists {
		return false
	}
	delete(m.bots, name)
	// 从有序列表中移除
	for i, n := range m.order {
		if n == name {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	logger.Infof("[BotManager] Removed bot %q", name)
	return true
}

// Names 返回所有已注册 Bot 的名称，按注册顺序排列。
func (m *BotManager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, len(m.order))
	copy(names, m.order)
	return names
}

// Len 返回已注册 Bot 的数量。
func (m *BotManager) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.bots)
}

// StartAll 并发启动所有已注册的 Bot。
// 任何一个 Bot 启动失败都会记录错误，但不会阻止其他 Bot 的启动。
// 若所有 Bot 均启动失败，返回聚合错误；若部分失败，同样返回聚合错误，
// 调用方可通过 errors.Is / errors.As 检查具体失败项。
func (m *BotManager) StartAll() error {
	m.mu.RLock()
	names := make([]string, len(m.order))
	copy(names, m.order)
	bots := make(map[string]*Bot, len(m.bots))
	maps.Copy(bots, m.bots)
	m.mu.RUnlock()

	if len(names) == 0 {
		logger.Warn("[BotManager] StartAll called with no registered bots")
		return nil
	}

	logger.Infof("[BotManager] Starting %d bot(s)...", len(names))

	type result struct {
		name string
		err  error
	}

	resultCh := make(chan result, len(names))
	var wg sync.WaitGroup

	for _, name := range names {
		wg.Add(1)
		go func(n string, b *Bot) {
			defer wg.Done()
			if err := b.Start(); err != nil {
				logger.WithError(err).Errorf("[BotManager] Failed to start bot %q", n)
				resultCh <- result{name: n, err: err}
			} else {
				logger.Infof("[BotManager] Bot %q started", n)
				resultCh <- result{name: n, err: nil}
			}
		}(name, bots[name])
	}

	wg.Wait()
	close(resultCh)

	var errs []BotError
	for r := range resultCh {
		if r.err != nil {
			errs = append(errs, BotError{Name: r.name, Err: r.err})
		}
	}

	if len(errs) > 0 {
		return &BotManagerError{Op: "StartAll", Errors: errs}
	}

	logger.Infof("[BotManager] All %d bot(s) started successfully", len(names))
	return nil
}

// StopAll 并发停止所有正在运行的 Bot，使用给定的 context 作为超时控制。
// 即使部分 Bot 停止失败，也会继续停止其他 Bot，最终返回聚合错误（如有）。
func (m *BotManager) StopAll(ctx context.Context) error {
	m.mu.RLock()
	names := make([]string, len(m.order))
	copy(names, m.order)
	bots := make(map[string]*Bot, len(m.bots))
	maps.Copy(bots, m.bots)
	m.mu.RUnlock()

	if len(names) == 0 {
		return nil
	}

	logger.Infof("[BotManager] Stopping %d bot(s)...", len(names))

	type result struct {
		name string
		err  error
	}

	resultCh := make(chan result, len(names))
	var wg sync.WaitGroup

	for _, name := range names {
		wg.Add(1)
		go func(n string, b *Bot) {
			defer wg.Done()
			if err := b.Stop(ctx); err != nil {
				logger.WithError(err).Errorf("[BotManager] Failed to stop bot %q", n)
				resultCh <- result{name: n, err: err}
			} else {
				logger.Infof("[BotManager] Bot %q stopped", n)
				resultCh <- result{name: n, err: nil}
			}
		}(name, bots[name])
	}

	wg.Wait()
	close(resultCh)

	var errs []BotError
	for r := range resultCh {
		if r.err != nil {
			errs = append(errs, BotError{Name: r.name, Err: r.err})
		}
	}

	if len(errs) > 0 {
		return &BotManagerError{Op: "StopAll", Errors: errs}
	}

	logger.Infof("[BotManager] All %d bot(s) stopped", len(names))
	return nil
}

// Shutdown 使用默认超时时间停止所有 Bot（DefaultShutdownTimeout）。
func (m *BotManager) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()
	return m.StopAll(ctx)
}

// ShutdownAsync 在后台 goroutine 中发起优雅关闭，立即返回。
//
// 返回一个只读 channel，所有 Bot 关闭完成后会写入最终错误（nil 表示全部成功）。
// 调用方可选择是否等待结果：
//
//	// 触发后不等待（fire-and-forget）
//	mgr.ShutdownAsync()
//
//	// 触发后等待结果
//	if err := <-mgr.ShutdownAsync(); err != nil {
//	    log.Println("shutdown error:", err)
//	}
//
// 适用于以下场景：
//   - HTTP handler 收到 /shutdown 请求，需先响应 200 再后台关闭
//   - 插件回调内部触发关闭（同步调用会在 lifecycle 链上死锁）
//   - 与外部框架集成时对方不允许阻塞其事件循环
//
// 注意：多次调用是安全的，StopAll 对每个 Bot 的 Stop 调用均幂等。
func (m *BotManager) ShutdownAsync() <-chan error {
	ch := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
		defer cancel()
		ch <- m.StopAll(ctx)
	}()
	return ch
}

// WaitForShutdown 阻塞直到收到 SIGINT/SIGTERM 信号，然后优雅停止所有 Bot。
// 这是生产环境启动多 Bot 后的标准收尾调用。
//
// 收到第一个 SIGINT（Ctrl+C）或 SIGTERM 时，开始优雅关闭（等待后台清理完成）。
// 若在优雅关闭期间再次收到 SIGINT，立即强制退出（os.Exit(1)），
// 不再等待剩余清理工作——这与大多数 CLI 工具的行为一致。
//
// timeout 为可选参数，指定优雅关闭的超时时间；省略时使用 [DefaultShutdownTimeout]（30s）。
// 若同一进程已有另一个 WaitForShutdown 处于监听状态，此次调用会直接返回并打印 Warn 日志。
//
// 若需要完全自定义 context（如携带 trace），请直接调用 [BotManager.StopAll]。
//
// 示例：
//
//	mgr.StartAll()
//	mgr.WaitForShutdown()                    // 使用默认 30s 超时
//	mgr.WaitForShutdown(60 * time.Second)    // 自定义超时
func (m *BotManager) WaitForShutdown(timeout ...time.Duration) {
	if !acquireShutdownListener("BotManager.WaitForShutdown") {
		return
	}
	defer releaseShutdownListener()

	shutdownTimeout := DefaultShutdownTimeout
	if len(timeout) > 0 && timeout[0] > 0 {
		shutdownTimeout = timeout[0]
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	<-sigCh
	logger.Info("[BotManager] Received shutdown signal, shutting down gracefully... (press Ctrl+C again to force exit)")

	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := m.StopAll(ctx); err != nil {
			logger.WithError(err).Error("[BotManager] Shutdown completed with errors")
		}
	}()

	select {
	case <-done:
	case <-sigCh:
		logger.Warn("[BotManager] Forced exit by second signal")
		os.Exit(1)
	}
}

// RunningBots 返回当前处于运行状态的 Bot 名称列表。
func (m *BotManager) RunningBots() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var running []string
	for _, name := range m.order {
		if bot, ok := m.bots[name]; ok && bot.IsRunning() {
			running = append(running, name)
		}
	}
	return running
}

// HealthAll 并发执行所有 Bot 的健康检查，以 map[name]CheckResponse 返回。
func (m *BotManager) HealthAll() map[string]BotHealthResult {
	m.mu.RLock()
	names := make([]string, len(m.order))
	copy(names, m.order)
	bots := make(map[string]*Bot, len(m.bots))
	maps.Copy(bots, m.bots)
	m.mu.RUnlock()

	results := make(map[string]BotHealthResult, len(names))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, name := range names {
		wg.Add(1)
		go func(n string, b *Bot) {
			defer wg.Done()
			resp := b.Health()
			mu.Lock()
			results[n] = BotHealthResult{
				Name:      n,
				IsRunning: b.IsRunning(),
				Health:    resp,
			}
			mu.Unlock()
		}(name, bots[name])
	}

	wg.Wait()
	return results
}

// BotHealthResult 单个 Bot 的健康检查结果
type BotHealthResult struct {
	Name      string
	IsRunning bool
	Health    health.CheckResponse
}

// -----------------------------------------------------------------
// 错误类型
// -----------------------------------------------------------------

// BotError 代表单个 Bot 操作失败
type BotError struct {
	Name string
	Err  error
}

func (e BotError) Error() string {
	return fmt.Sprintf("bot %q: %v", e.Name, e.Err)
}

func (e BotError) Unwrap() error { return e.Err }

// BotManagerError 代表 BotManager 批量操作中的聚合错误
type BotManagerError struct {
	Op     string
	Errors []BotError
}

func (e *BotManagerError) Error() string {
	if len(e.Errors) == 1 {
		return fmt.Sprintf("botmanager %s: %v", e.Op, e.Errors[0])
	}
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("botmanager %s: %d bot(s) failed", e.Op, len(e.Errors)))
	for _, be := range e.Errors {
		msg.WriteString("\n  - " + be.Error())
	}
	return msg.String()
}

// FailedBots 返回所有失败的 Bot 名称列表
func (e *BotManagerError) FailedBots() []string {
	names := make([]string, len(e.Errors))
	for i, be := range e.Errors {
		names[i] = be.Name
	}
	return names
}

// -----------------------------------------------------------------
// BotManagerBuilder — 流畅构建 API
// -----------------------------------------------------------------

// BotManagerBuilder 提供流畅的 BotManager 构建接口。
//
// 示例：
//
//	mgr, err := remilia.NewBotManagerBuilder().
//	    Add("prod", prodAdapter, &prodBotInfo).
//	    Add("test", testAdapter, &testBotInfo).
//	    Build()
type BotManagerBuilder struct {
	entries   []botEntry
	hasErrors []error // 收集所有 Add* 调用产生的错误（原先只保留第一个）
}

type botEntry struct {
	name    string
	bot     *Bot
	builder *BotBuilder
}

// NewBotManagerBuilder 创建 BotManager 构建器。
func NewBotManagerBuilder() *BotManagerBuilder {
	return &BotManagerBuilder{}
}

// AddBot 向构建器中添加一个已构建的 Bot 实例。
func (b *BotManagerBuilder) AddBot(name string, bot *Bot) *BotManagerBuilder {
	if bot == nil {
		b.hasErrors = append(b.hasErrors, fmt.Errorf("botmanager: bot %q must not be nil", name))
		return b
	}
	b.entries = append(b.entries, botEntry{name: name, bot: bot})
	return b
}

// AddBuilder 向构建器中添加一个 BotBuilder，Build() 时自动构建。
// 这允许延迟构建，统一处理错误。
func (b *BotManagerBuilder) AddBuilder(name string, builder *BotBuilder) *BotManagerBuilder {
	if builder == nil {
		b.hasErrors = append(b.hasErrors, fmt.Errorf("botmanager: builder %q must not be nil", name))
		return b
	}
	b.entries = append(b.entries, botEntry{name: name, builder: builder})
	return b
}

// Build 构建 BotManager，返回错误（如有）。
func (b *BotManagerBuilder) Build() (*BotManager, error) {
	if len(b.hasErrors) > 0 {
		return nil, errors.Join(b.hasErrors...)
	}

	mgr := NewBotManager()
	for _, entry := range b.entries {
		var bot *Bot
		if entry.bot != nil {
			bot = entry.bot
		} else if entry.builder != nil {
			var err error
			bot, err = entry.builder.Build()
			if err != nil {
				return nil, fmt.Errorf("botmanager: building bot %q: %w", entry.name, err)
			}
		} else {
			return nil, fmt.Errorf("botmanager: entry %q has neither bot nor builder", entry.name)
		}
		if err := mgr.Add(entry.name, bot); err != nil {
			return nil, err
		}
	}
	return mgr, nil
}

// MustBuild 构建 BotManager，失败时 panic。
func (b *BotManagerBuilder) MustBuild() *BotManager {
	mgr, err := b.Build()
	if err != nil {
		panic(err)
	}
	return mgr
}

// -----------------------------------------------------------------
// 便捷的时间窗口监控
// -----------------------------------------------------------------

// BotManagerStatus 描述 BotManager 当前整体状态
type BotManagerStatus struct {
	Total   int
	Running int
	Stopped int
	Uptime  map[string]time.Duration
}

// Status 返回所有 Bot 的当前状态摘要。
func (m *BotManager) Status() BotManagerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := BotManagerStatus{
		Total:  len(m.bots),
		Uptime: make(map[string]time.Duration, len(m.bots)),
	}
	for name, bot := range m.bots {
		status.Uptime[name] = bot.Uptime()
		if bot.IsRunning() {
			status.Running++
		} else {
			status.Stopped++
		}
	}
	return status
}
