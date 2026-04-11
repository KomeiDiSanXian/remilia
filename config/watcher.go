package config

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/fsnotify/fsnotify"
)

// ReloadCallback 在配置重新加载时被调用。
// 接收旧配置和新配置作为参数。
// 返回 error 可拒绝应用新配置。
type ReloadCallback func(oldConfig, newConfig *Config) error

// Watcher 监听配置文件变更并触发重新加载
type Watcher struct {
	configPath string
	watcher    *fsnotify.Watcher
	callbacks  []ReloadCallback
	mu         sync.RWMutex

	currentConfig atomic.Value // *Config

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// parentCtx 用于与外部生命周期绑定（由 NewWatcherWithContext 设置）。
	// watchLoop 监听其 Done() 信号，无需额外 goroutine。
	parentCtx context.Context

	// cleanupOnce 保证 cancel()+watcher.Close() 只执行一次
	cleanupOnce sync.Once
	cleanupErr  error

	// 配置项
	debounceDelay time.Duration
	validateOnly  bool // 若为 true，则仅验证不应用

	// 统计指标
	reloadCount    atomic.Int64
	failedCount    atomic.Int64
	lastReloadTime atomic.Value // time.Time
}

// WatcherOption 用于配置 Watcher
type WatcherOption func(*Watcher)

// WithDebounceDelay 设置文件变更事件的防抖延迟。
// 默认为 100ms，避免单次保存触发多次重载。
func WithDebounceDelay(delay time.Duration) WatcherOption {
	return func(w *Watcher) {
		w.debounceDelay = delay
	}
}

// WithValidateOnly 启用仅验证模式。
// 配置变更将被验证但不会被应用。
func WithValidateOnly(validate bool) WatcherOption {
	return func(w *Watcher) {
		w.validateOnly = validate
	}
}

// NewWatcherWithContext 创建一个与 parent context 生命周期绑定的配置监视器。
// 当 parent 被取消时（如 Bot 关闭），监视器会自动停止，无需显式调用 Stop()。
// 此方式与 AdaptiveRateLimiter、DedupFilter 和 token.Manager 使用的 WithContext 模式一致。
//
// 示例：
//
//	w, err := config.NewWatcherWithContext(bot.Context(), "config.yaml")
func NewWatcherWithContext(parent context.Context, configPath string, opts ...WatcherOption) (*Watcher, error) {
	w, err := NewWatcher(configPath, opts...)
	if err != nil {
		return nil, err
	}
	w.parentCtx = parent // 直接设置 parent ctx，watchLoop 内部监听（无额外 goroutine）
	return w, nil
}

// NewWatcher 创建一个新的配置文件监视器
func NewWatcher(configPath string, opts ...WatcherOption) (*Watcher, error) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fs watcher: %w", err)
	}

	// 监听目录而非文件本身，以兼容编辑器创建临时文件的情况
	dir := filepath.Dir(absPath)
	if err := fsWatcher.Add(dir); err != nil {
		_ = fsWatcher.Close()
		return nil, fmt.Errorf("failed to watch directory: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	w := &Watcher{
		configPath:    absPath,
		watcher:       fsWatcher,
		callbacks:     make([]ReloadCallback, 0),
		ctx:           ctx,
		cancel:        cancel,
		parentCtx:     context.Background(), // 默认无 parent，watchLoop 不会因此退出
		debounceDelay: 100 * time.Millisecond,
	}

	// 应用选项
	for _, opt := range opts {
		opt(w)
	}

	// 加载初始配置
	cfg, err := Load(configPath)
	if err != nil {
		cancel()
		_ = fsWatcher.Close()
		return nil, fmt.Errorf("failed to load initial config: %w", err)
	}
	w.currentConfig.Store(cfg)
	w.lastReloadTime.Store(time.Now())

	return w, nil
}

// AddCallback 注册一个在配置重载时调用的回调函数。
// 回调按注册顺序依次执行。
func (w *Watcher) AddCallback(callback ReloadCallback) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.callbacks = append(w.callbacks, callback)
}

// GetConfig 返回当前配置
func (w *Watcher) GetConfig() *Config {
	return w.currentConfig.Load().(*Config)
}

// Start 开始监听配置文件变更
func (w *Watcher) Start() {
	w.wg.Add(1)
	go w.watchLoop()
}

// doCleanup 执行幂等的清理操作：取消内部 ctx 并关闭 fsnotify watcher。
// 由 Stop() 和 watchLoop 的 parent ctx 分支共同使用。
func (w *Watcher) doCleanup() {
	w.cleanupOnce.Do(func() {
		w.cancel()
		w.cleanupErr = w.watcher.Close()
	})
}

// Stop 停止监听配置文件变更
func (w *Watcher) Stop() error {
	w.doCleanup()
	w.wg.Wait()
	return w.cleanupErr
}

// watchLoop 是主事件循环
func (w *Watcher) watchLoop() {
	defer w.wg.Done()

	logger.WithField("path", w.configPath).Info("[ConfigWatcher] Started watching configuration file")

	// 防抖计时器，避免多次重载
	var debounceTimer *time.Timer
	var timerMu sync.Mutex

	// 确保退出时清理 timer
	defer func() {
		timerMu.Lock()
		if debounceTimer != nil {
			// 正确停止 timer 并 drain channel
			if !debounceTimer.Stop() {
				// Timer 已经触发，尝试 drain channel 避免 goroutine 泄漏
				select {
				case <-debounceTimer.C:
				default:
				}
			}
		}
		timerMu.Unlock()
		logger.Debug("[ConfigWatcher] Watch loop cleanup completed")
	}()

	for {
		select {
		case <-w.ctx.Done():
			logger.Info("[ConfigWatcher] Stopped")
			return

		case <-w.parentCtx.Done():
			// parent context 取消（如 Bot 关闭），执行清理后退出
			logger.Info("[ConfigWatcher] Stopped (parent context cancelled)")
			w.doCleanup()
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// 只处理目标配置文件的事件
			eventPath, err := filepath.Abs(event.Name)
			if err != nil || eventPath != w.configPath {
				continue
			}

			// 过滤相关事件（写入、创建、重命名）
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}

			logger.WithFields(logger.Fields{
				"file": event.Name,
				"op":   event.Op.String(),
			}).Debug("[ConfigWatcher] File change detected")

			// 防抖：每次事件都重置计时器
			timerMu.Lock()
			if debounceTimer != nil {
				// 正确停止计时器并 drain channel，防止 goroutine 泄漏
				if !debounceTimer.Stop() {
					// 计时器已触发，drain channel
					select {
					case <-debounceTimer.C:
					default:
					}
				}
			}
			debounceTimer = time.AfterFunc(w.debounceDelay, func() {
				if err := w.reload(); err != nil {
					logger.WithError(err).Error("[ConfigWatcher] Failed to reload configuration")
				}
			})
			timerMu.Unlock()

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			logger.WithError(err).Error("[ConfigWatcher] Watcher error")
		}
	}
}

// reload 加载并应用新配置
func (w *Watcher) reload() error {
	// 如果 watcher 已停止，直接返回，避免访问已关闭的资源
	if w.ctx.Err() != nil {
		return nil
	}

	startTime := time.Now()

	// 修复 B7：使用 loadRaw 代替 Load，避免 Load 内部调用 notifyListeners 导致同一次
	// 配置变更触发两次通知（watcher 稳定性检查需要读取两次文件）。
	newConfig, err := loadRaw(w.configPath)
	if err != nil {
		w.failedCount.Add(1)
		return fmt.Errorf("failed to load new config: %w", err)
	}

	// 稳定性校验：等待 50ms 后再次读取，确认文件已完整写入。
	// 使用 context-aware 的等待，确保 Watcher 停止时可立即退出。
	stabilityTimer := time.NewTimer(50 * time.Millisecond)
	select {
	case <-stabilityTimer.C:
		// 正常等待完成，继续二次读取
	case <-w.ctx.Done():
		stabilityTimer.Stop()
		return nil
	}
	stabilityTimer.Stop()
	newConfig2, err2 := loadRaw(w.configPath)
	if err2 != nil {
		// 二次读取失败通常意味着文件仍在写入，保留第一次的结果继续尝试
		logger.WithError(err2).Warn("[ConfigWatcher] Stability check read failed, using first read result")
	} else if newConfig2 != nil {
		// 以第二次读取的最终结果为准（更接近最终状态）
		newConfig = newConfig2
	}

	// 获取当前配置
	oldConfig := w.currentConfig.Load().(*Config)

	// 执行回调
	w.mu.RLock()
	callbacks := append([]ReloadCallback(nil), w.callbacks...)
	w.mu.RUnlock()

	for i, callback := range callbacks {
		if err := callback(oldConfig, newConfig); err != nil {
			w.failedCount.Add(1)
			return fmt.Errorf("callback %d rejected config: %w", i, err)
		}
	}

	// 非仅验证模式时应用新配置
	if !w.validateOnly {
		w.currentConfig.Store(newConfig)
		// 更新全局配置
		globalConfig.Store(newConfig)
		w.lastReloadTime.Store(time.Now())
		w.reloadCount.Add(1)

		// 修复 B7：通知监听器仅在最终确认后调用一次（loadRaw 不触发通知）
		notifyListeners(newConfig)

		duration := time.Since(startTime)
		logger.WithFields(logger.Fields{
			"duration_ms":  duration.Milliseconds(),
			"reload_count": w.reloadCount.Load(),
		}).Info("[ConfigWatcher] Configuration reloaded successfully")
	} else {
		logger.Info("[ConfigWatcher] Configuration validated successfully (validate-only mode)")
	}

	return nil
}

// ForceReload 手动触发配置重载
func (w *Watcher) ForceReload() error {
	return w.reload()
}

// WatcherStats 监视器统计信息
type WatcherStats struct {
	ReloadCount    int64
	FailedCount    int64
	LastReloadTime time.Time
}

// GetStats 返回当前监视器统计信息
func (w *Watcher) GetStats() WatcherStats {
	lastReload := w.lastReloadTime.Load()
	var lastReloadTime time.Time
	if lastReload != nil {
		lastReloadTime = lastReload.(time.Time)
	}

	return WatcherStats{
		ReloadCount:    w.reloadCount.Load(),
		FailedCount:    w.failedCount.Load(),
		LastReloadTime: lastReloadTime,
	}
}

// --- Helper functions for common use cases ---

// WatchWithAutoRestart 创建一个在配置变更时自动重启组件的监视器。
// 这是常见模式的便捷封装函数。
func WatchWithAutoRestart(configPath string, restartFunc func(*Config) error) (*Watcher, error) {
	watcher, err := NewWatcher(configPath)
	if err != nil {
		return nil, err
	}

	watcher.AddCallback(func(oldConfig, newConfig *Config) error {
		// 检查是否需要重启（比较关键字段）
		if needsRestart(oldConfig, newConfig) {
			logger.Info("[ConfigWatcher] Configuration change requires restart")
			return restartFunc(newConfig)
		}
		logger.Info("[ConfigWatcher] Configuration change applied without restart")
		return nil
	})

	return watcher, nil
}

// needsRestart 判断配置变更是否需要重启组件
func needsRestart(old, new *Config) bool {
	// Bot 配置变更需要重启
	if old.Bot != new.Bot {
		return true
	}

	// 服务器配置变更需要重启
	if old.Server != new.Server {
		return true
	}

	// 日志级别变更无需重启（可动态应用）
	// 中间件变更是否需要重启取决于具体实现

	return false
}
