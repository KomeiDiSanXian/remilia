package config

// validate.go — 各子配置的 Validate() 方法
//
// 将验证逻辑从 config.go（类型定义）中分离，使两者职责清晰：
//   - config.go  : 结构体定义、全局加载/读取函数、订阅机制
//   - validate.go: 所有 Validate() 方法实现

import (
	"fmt"
	"time"
)

// Validate 验证整体配置的有效性
//
// 验证规则：
// - Bot 配置：必填字段验证
// - Server 配置：端口范围验证
// - Log 配置：级别和格式验证
// - Concurrency 配置：参数范围验证
// - Retry 配置：参数有效性验证
// - Middleware 配置：参数合理性验证
func (c *Config) Validate() error {
	validators := []struct {
		name string
		fn   func() error
	}{
		{"bot", c.Bot.Validate},
		{"server", c.Server.Validate},
		{"log", c.Log.Validate},
		{"concurrency", c.Concurrency.Validate},
		{"retry", c.Retry.Validate},
		{"middleware", c.Middleware.Validate},
		{"dead_letter", c.DeadLetter.Validate},
		{"webhook", c.Webhook.Validate},
		{"token", c.Token.Validate},
		{"engine", c.Engine.Validate},
		{"degradation", c.Degradation.Validate},
		{"tracing", c.Tracing.Validate},
	}
	for _, v := range validators {
		if err := v.fn(); err != nil {
			return fmt.Errorf("invalid %s config: %w", v.name, err)
		}
	}
	return nil
}

// Validate 验证 Bot 配置
func (bc *BotConfig) Validate() error {
	if bc.AppID == 0 {
		return fmt.Errorf("bot.app_id is required and must be non-zero")
	}
	if bc.BotID == 0 {
		return fmt.Errorf("bot.bot_id is required and must be non-zero")
	}
	if bc.Token == "" {
		return fmt.Errorf("bot.token is required and cannot be empty")
	}
	if bc.Secret == "" {
		return fmt.Errorf("bot.secret is required and cannot be empty")
	}
	return nil
}

// Validate 验证 Server 配置
func (sc *ServerConfig) Validate() error {
	if sc.Port < 1 || sc.Port > 65535 {
		return fmt.Errorf("server.port must be between 1-65535, got %d", sc.Port)
	}
	if sc.ShutdownTimeout != "" {
		if _, err := time.ParseDuration(sc.ShutdownTimeout); err != nil {
			return fmt.Errorf("server.shutdown_timeout is not a valid duration: %w", err)
		}
	}
	return nil
}

// Validate 验证 Log 配置
func (lc *LogConfig) Validate() error {
	validLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true, "fatal": true, "panic": true,
	}
	if lc.Level != "" && !validLevels[lc.Level] {
		return fmt.Errorf("log.level must be one of [debug, info, warn, error, fatal, panic], got '%s'", lc.Level)
	}
	validFormats := map[string]bool{"text": true, "json": true}
	if lc.Format != "" && !validFormats[lc.Format] {
		return fmt.Errorf("log.format must be one of [text, json], got '%s'", lc.Format)
	}
	return nil
}

// Validate 验证 Concurrency 配置
func (cc *ConcurrencyConfig) Validate() error {
	if cc.Limit < 0 {
		return fmt.Errorf("concurrency.limit must be >= 0, got %d", cc.Limit)
	}
	validPolicies := map[string]bool{"drop": true, "block": true, "trywait": true, "": true}
	if !validPolicies[cc.Policy] {
		return fmt.Errorf("concurrency.policy must be one of [drop, block, trywait], got '%s'", cc.Policy)
	}
	if cc.WaitTimeout != "" {
		if _, err := time.ParseDuration(cc.WaitTimeout); err != nil {
			return fmt.Errorf("concurrency.wait_timeout is not a valid duration: %w", err)
		}
	}
	if cc.EventBuffer < 0 {
		return fmt.Errorf("concurrency.event_buffer must be >= 0, got %d", cc.EventBuffer)
	}
	return nil
}

// Validate 验证 Retry 配置
func (rc *RetryConfig) Validate() error {
	if rc.Enable {
		if rc.MaxAttempts < 1 {
			return fmt.Errorf("retry.max_attempts must be >= 1 when retry is enabled, got %d", rc.MaxAttempts)
		}
		if rc.BackoffBase != "" {
			if _, err := time.ParseDuration(rc.BackoffBase); err != nil {
				return fmt.Errorf("retry.backoff_base is not a valid duration: %w", err)
			}
		}
		if rc.BackoffMax != "" {
			if _, err := time.ParseDuration(rc.BackoffMax); err != nil {
				return fmt.Errorf("retry.backoff_max is not a valid duration: %w", err)
			}
		}
	}
	return nil
}

// Validate 验证 Middleware 配置
func (mc *MiddlewareConfig) Validate() error {
	if mc.RateLimit {
		if mc.RateLimitRate < 0 {
			return fmt.Errorf("middleware.rate_limit_rate must be >= 0, got %d", mc.RateLimitRate)
		}
		if mc.RateLimitBurst < 0 {
			return fmt.Errorf("middleware.rate_limit_burst must be >= 0, got %d", mc.RateLimitBurst)
		}
		if mc.RateLimitBucketTTL != "" {
			if _, err := time.ParseDuration(mc.RateLimitBucketTTL); err != nil {
				return fmt.Errorf("middleware.rate_limit_bucket_ttl is not a valid duration: %w", err)
			}
		}
		if mc.RateLimitCleanupInterval != "" {
			if _, err := time.ParseDuration(mc.RateLimitCleanupInterval); err != nil {
				return fmt.Errorf("middleware.rate_limit_cleanup_interval is not a valid duration: %w", err)
			}
		}
	}
	if mc.DedupEnable {
		if mc.DedupDefaultTTL != "" {
			if _, err := time.ParseDuration(mc.DedupDefaultTTL); err != nil {
				return fmt.Errorf("middleware.dedup_default_ttl is not a valid duration: %w", err)
			}
		}
		if mc.DedupCleanupInterval != "" {
			if _, err := time.ParseDuration(mc.DedupCleanupInterval); err != nil {
				return fmt.Errorf("middleware.dedup_cleanup_interval is not a valid duration: %w", err)
			}
		}
	}
	if mc.SlowHandlerEnable && mc.SlowHandlerThreshold != "" {
		if _, err := time.ParseDuration(mc.SlowHandlerThreshold); err != nil {
			return fmt.Errorf("middleware.slow_handler_threshold is not a valid duration: %w", err)
		}
	}
	if mc.DegradationCPUThreshold != 0 && (mc.DegradationCPUThreshold < 0 || mc.DegradationCPUThreshold > 100) {
		return fmt.Errorf("middleware.degradation_cpu_threshold must be between 0 and 100, got %f", mc.DegradationCPUThreshold)
	}
	if mc.DegradationMemoryThreshold != 0 && (mc.DegradationMemoryThreshold < 0 || mc.DegradationMemoryThreshold > 100) {
		return fmt.Errorf("middleware.degradation_memory_threshold must be between 0 and 100, got %f", mc.DegradationMemoryThreshold)
	}
	return nil
}

// Validate 验证 DeadLetter 配置
func (dlc *DeadLetterConfig) Validate() error {
	if !dlc.Enable {
		return nil
	}
	validTargets := map[string]bool{"file": true, "kafka": true, "webhook": true}
	if !validTargets[dlc.Target] {
		return fmt.Errorf("dead_letter.target must be one of [file, kafka, webhook], got '%s'", dlc.Target)
	}
	switch dlc.Target {
	case "file":
		if dlc.FilePath == "" {
			return fmt.Errorf("dead_letter.file_path is required when target is 'file'")
		}
	case "webhook":
		if dlc.WebhookURL == "" {
			return fmt.Errorf("dead_letter.webhook_url is required when target is 'webhook'")
		}
	}
	return nil
}

// Validate 验证 Webhook 配置
func (wc *WebhookConfig) Validate() error {
	if wc.EventBuffer < 0 {
		return fmt.Errorf("webhook.event_buffer must be >= 0, got %d", wc.EventBuffer)
	}
	if wc.WorkerCount < 0 {
		return fmt.Errorf("webhook.worker_count must be >= 0, got %d", wc.WorkerCount)
	}
	if wc.DedupEnable {
		if wc.Shards < 0 {
			return fmt.Errorf("webhook.shards must be >= 0, got %d", wc.Shards)
		}
		if wc.LifeWindow != "" {
			if _, err := time.ParseDuration(wc.LifeWindow); err != nil {
				return fmt.Errorf("webhook.life_window is not a valid duration: %w", err)
			}
		}
		if wc.CleanWindow != "" {
			if _, err := time.ParseDuration(wc.CleanWindow); err != nil {
				return fmt.Errorf("webhook.clean_window is not a valid duration: %w", err)
			}
		}
		if wc.MaxEntrySize < 0 {
			return fmt.Errorf("webhook.max_entry_size must be >= 0, got %d", wc.MaxEntrySize)
		}
		if wc.HardMaxCacheSize < 0 {
			return fmt.Errorf("webhook.hard_max_cache_size must be >= 0, got %d", wc.HardMaxCacheSize)
		}
		if wc.MaxEntriesInWindow < 0 {
			return fmt.Errorf("webhook.max_entries_in_window must be >= 0, got %d", wc.MaxEntriesInWindow)
		}
	}
	return nil
}

// Validate 验证 Token 配置
func (tc *TokenConfig) Validate() error {
	if tc.RetryDelay != "" {
		if _, err := time.ParseDuration(tc.RetryDelay); err != nil {
			return fmt.Errorf("token.retry_delay is not a valid duration: %w", err)
		}
	}
	if tc.RefreshAdvance != "" {
		if _, err := time.ParseDuration(tc.RefreshAdvance); err != nil {
			return fmt.Errorf("token.refresh_advance is not a valid duration: %w", err)
		}
	}
	if tc.MinRefreshRatio < 0 || tc.MinRefreshRatio > 1 {
		return fmt.Errorf("token.min_refresh_ratio must be between 0 and 1, got %f", tc.MinRefreshRatio)
	}
	return nil
}

// Validate 验证 engine 配置
func (ec *EngineConfig) Validate() error {
	durations := []struct{ name, val string }{
		{"engine.temp_matcher_cleanup_interval", ec.TempMatcherCleanupInterval},
		{"engine.pending_delete_process_interval", ec.PendingDeleteProcessInterval},
	}
	for _, d := range durations {
		if d.val != "" {
			if _, err := time.ParseDuration(d.val); err != nil {
				return fmt.Errorf("%s is not a valid duration: %w", d.name, err)
			}
		}
	}
	nonNeg := []struct {
		name string
		val  int
	}{
		{"engine.pending_delete_buffer_size", ec.PendingDeleteBufferSize},
		{"engine.pending_delete_batch_size", ec.PendingDeleteBatchSize},
		{"engine.matcher_pool_capacity", ec.MatcherPoolCapacity},
		{"engine.matcher_pool_max_capacity", ec.MatcherPoolMaxCapacity},
		{"engine.temp_matcher_shard_count", ec.TempMatcherShardCount},
	}
	for _, n := range nonNeg {
		if n.val < 0 {
			return fmt.Errorf("%s must be >= 0, got %d", n.name, n.val)
		}
	}
	return nil
}

// Validate 验证 Degradation 配置
func (dc *DegradationConfig) Validate() error {
	if !dc.Enable {
		return nil
	}
	if dc.CPUThreshold < 0 || dc.CPUThreshold > 100 {
		return fmt.Errorf("degradation.cpu_threshold must be between 0 and 100, got %f", dc.CPUThreshold)
	}
	if dc.MemoryThreshold < 0 || dc.MemoryThreshold > 100 {
		return fmt.Errorf("degradation.memory_threshold must be between 0 and 100, got %f", dc.MemoryThreshold)
	}
	durations := []struct{ name, val string }{
		{"degradation.latency_threshold", dc.LatencyThreshold},
		{"degradation.monitor_interval", dc.MonitorInterval},
		{"degradation.recovery_interval", dc.RecoveryInterval},
	}
	for _, d := range durations {
		if d.val != "" {
			if _, err := time.ParseDuration(d.val); err != nil {
				return fmt.Errorf("%s is not a valid duration: %w", d.name, err)
			}
		}
	}
	if dc.DelayQueueSize < 0 {
		return fmt.Errorf("degradation.delay_queue_size must be >= 0, got %d", dc.DelayQueueSize)
	}
	if dc.GoroutineThreshold < 0 {
		return fmt.Errorf("degradation.goroutine_threshold must be >= 0, got %d", dc.GoroutineThreshold)
	}
	validStrategies := map[string]bool{"drop": true, "delay": true, "simplify": true, "": true}
	if !validStrategies[dc.Strategy] {
		return fmt.Errorf("degradation.strategy must be one of [drop, delay, simplify], got '%s'", dc.Strategy)
	}
	return nil
}

// Validate 验证 Tracing 配置
func (tc *TracingConfig) Validate() error {
	if !tc.Enable {
		return nil
	}
	if tc.ServiceName == "" {
		return fmt.Errorf("tracing.service_name is required when tracing is enabled")
	}
	validExporters := map[string]bool{
		"otlp": true, "tempo": true, "grafana": true,
		"zipkin": true, "stdout": true, "console": true,
	}
	if !validExporters[tc.Exporter] {
		return fmt.Errorf("tracing.exporter must be one of [otlp, tempo, grafana, zipkin, stdout, console], got '%s'", tc.Exporter)
	}
	if tc.Exporter != "stdout" && tc.Exporter != "console" && tc.Endpoint == "" {
		return fmt.Errorf("tracing.endpoint is required when exporter is '%s'", tc.Exporter)
	}
	if tc.SamplingRate < 0 || tc.SamplingRate > 1 {
		return fmt.Errorf("tracing.sampling_rate must be between 0 and 1, got %f", tc.SamplingRate)
	}
	return nil
}
