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

// ReloadCallback is called when configuration is reloaded
// It receives the old and new configuration
// Return error to reject the new configuration
type ReloadCallback func(oldConfig, newConfig *Config) error

// Watcher watches configuration file for changes and triggers reload
type Watcher struct {
	configPath string
	watcher    *fsnotify.Watcher
	callbacks  []ReloadCallback
	mu         sync.RWMutex

	currentConfig atomic.Value // *Config

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Configuration
	debounceDelay time.Duration
	validateOnly  bool // If true, only validate but don't apply

	// Metrics
	reloadCount    atomic.Int64
	failedCount    atomic.Int64
	lastReloadTime atomic.Value // time.Time
}

// WatcherOption configures the Watcher
type WatcherOption func(*Watcher)

// WithDebounceDelay sets the debounce delay for file change events
// Default is 100ms to avoid multiple reloads for a single save
func WithDebounceDelay(delay time.Duration) WatcherOption {
	return func(w *Watcher) {
		w.debounceDelay = delay
	}
}

// WithValidateOnly enables validation-only mode
// Configuration changes will be validated but not applied
func WithValidateOnly(validate bool) WatcherOption {
	return func(w *Watcher) {
		w.validateOnly = validate
	}
}

// NewWatcherWithContext creates a new configuration watcher whose lifetime is
// bound to the provided parent context. When parent is canceled (e.g. on
// Bot shutdown), the watcher stops automatically without requiring an explicit
// Stop() call. This follows the same WithContext pattern used by
// AdaptiveRateLimiter, DedupFilter, and token.Manager.
//
// Example:
//
//	w, err := config.NewWatcherWithContext(bot.Context(), "config.yaml")
func NewWatcherWithContext(parent context.Context, configPath string, opts ...WatcherOption) (*Watcher, error) {
	w, err := NewWatcher(configPath, opts...)
	if err != nil {
		return nil, err
	}
	go func() {
		<-parent.Done()
		_ = w.Stop()
	}()
	return w, nil
}

// NewWatcher creates a new configuration watcher
func NewWatcher(configPath string, opts ...WatcherOption) (*Watcher, error) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fs watcher: %w", err)
	}

	// Watch the directory instead of the file
	// This handles cases where editors create temp files
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
		debounceDelay: 100 * time.Millisecond,
	}

	// Apply options
	for _, opt := range opts {
		opt(w)
	}

	// Load initial configuration
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

// AddCallback registers a callback to be called on configuration reload
// Callbacks are executed in order of registration
func (w *Watcher) AddCallback(callback ReloadCallback) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.callbacks = append(w.callbacks, callback)
}

// GetConfig returns the current configuration
func (w *Watcher) GetConfig() *Config {
	return w.currentConfig.Load().(*Config)
}

// Start begins watching for configuration changes
func (w *Watcher) Start() {
	w.wg.Add(1)
	go w.watchLoop()
}

// Stop stops watching for configuration changes
func (w *Watcher) Stop() error {
	w.cancel()
	w.wg.Wait()
	return w.watcher.Close()
}

// watchLoop is the main event loop
func (w *Watcher) watchLoop() {
	defer w.wg.Done()

	logger.WithField("path", w.configPath).Info("[ConfigWatcher] Started watching configuration file")

	// Debounce timer to avoid multiple reloads
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

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// Only process events for our config file
			eventPath, err := filepath.Abs(event.Name)
			if err != nil || eventPath != w.configPath {
				continue
			}

			// Filter relevant events (Write, Create, Rename)
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}

			logger.WithFields(logger.Fields{
				"file": event.Name,
				"op":   event.Op.String(),
			}).Debug("[ConfigWatcher] File change detected")

			// Debounce: reset timer on each event
			timerMu.Lock()
			if debounceTimer != nil {
				// Properly stop timer and drain channel to prevent goroutine leak
				if !debounceTimer.Stop() {
					// Timer already fired, drain the channel
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

// reload loads and applies new configuration
func (w *Watcher) reload() error {
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

	// Get current configuration
	oldConfig := w.currentConfig.Load().(*Config)

	// Execute callbacks
	w.mu.RLock()
	callbacks := append([]ReloadCallback(nil), w.callbacks...)
	w.mu.RUnlock()

	for i, callback := range callbacks {
		if err := callback(oldConfig, newConfig); err != nil {
			w.failedCount.Add(1)
			return fmt.Errorf("callback %d rejected config: %w", i, err)
		}
	}

	// Apply new configuration if not in validate-only mode
	if !w.validateOnly {
		w.currentConfig.Store(newConfig)
		// Update global config using atomic store
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

// ForceReload manually triggers a configuration reload
func (w *Watcher) ForceReload() error {
	return w.reload()
}

// WatcherStats returns watcher statistics
type WatcherStats struct {
	ReloadCount    int64
	FailedCount    int64
	LastReloadTime time.Time
}

// GetStats returns current watcher statistics
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

// WatchWithAutoRestart creates a watcher that automatically restarts components on config change
// This is a convenience function for common patterns
func WatchWithAutoRestart(configPath string, restartFunc func(*Config) error) (*Watcher, error) {
	watcher, err := NewWatcher(configPath)
	if err != nil {
		return nil, err
	}

	watcher.AddCallback(func(oldConfig, newConfig *Config) error {
		// Check if restart is needed (compare critical fields)
		if needsRestart(oldConfig, newConfig) {
			logger.Info("[ConfigWatcher] Configuration change requires restart")
			return restartFunc(newConfig)
		}
		logger.Info("[ConfigWatcher] Configuration change applied without restart")
		return nil
	})

	return watcher, nil
}

// needsRestart determines if configuration changes require component restart
func needsRestart(old, new *Config) bool {
	// Bot configuration changes require restart
	if old.Bot != new.Bot {
		return true
	}

	// Server configuration changes require restart
	if old.Server != new.Server {
		return true
	}

	// Log level changes don't require restart (can be applied dynamically)
	// Middleware changes may require restart depending on implementation

	return false
}
