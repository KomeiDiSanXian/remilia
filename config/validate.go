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
// - Log 配置：级别和格式验证（委托给 logger.Config.Validate）
// - Concurrency 配置：参数范围验证
// - Retry 配置：参数有效性验证
// - Middleware 配置：参数合理性验证（含降级）
func (c *Config) Validate() error {
	validators := []struct {
		name string
		fn   func() error
	}{
		{"bot", c.Bot.Validate},
		{"log", c.Log.Validate},
		{"concurrency", c.Concurrency.Validate},
		{"retry", c.Retry.Validate},
		{"middleware", c.Middleware.Validate},
		{"dead_letter", c.DeadLetter.Validate},
		{"engine", c.Engine.Validate},
		{"tracing", c.Tracing.Validate},
		{"pprof", c.Pprof.Validate},
		{"api", c.API.Validate},
	}
	for _, v := range validators {
		if err := v.fn(); err != nil {
			return fmt.Errorf("invalid %s config: %w", v.name, err)
		}
	}
	return nil
}

// Validate 验证 Bot 配置容器，遍历所有已启用的平台子配置。
//
// Bot 配置采用宽松验证策略：所有平台子字段均为 nil 时跳过验证，
// 允许用户在未连接任何平台的情况下运行框架（如仅使用测试适配器）。
// 各平台适配器在初始化时应自行检查所需字段的完整性。
func (bc *BotConfig) Validate() error {
	type namedValidator struct {
		name string
		fn   func() error
	}
	var validators []namedValidator

	if bc.QQ != nil {
		validators = append(validators, namedValidator{"bot.qq", bc.QQ.Validate})
	}
	// 其余平台目前仅做非空判断，未来可按需扩展 Validate
	if bc.OneBot != nil {
		validators = append(validators, namedValidator{"bot.onebot", bc.OneBot.Validate})
	}
	if bc.Discord != nil {
		validators = append(validators, namedValidator{"bot.discord", bc.Discord.Validate})
	}
	if bc.Satori != nil {
		validators = append(validators, namedValidator{"bot.satori", bc.Satori.Validate})
	}
	if bc.Milky != nil {
		validators = append(validators, namedValidator{"bot.milky", bc.Milky.Validate})
	}

	for _, v := range validators {
		if err := v.fn(); err != nil {
			return fmt.Errorf("invalid %s config: %w", v.name, err)
		}
	}
	return nil
}

// Validate 验证 QQ 平台配置。
//
// QQ 配置采用宽松验证策略：全部字段零值时跳过验证，
// 零值字段视为"不需要 QQ 平台"。
func (qc *QQConfig) Validate() error {
	if err := qc.Webhook.Validate(); err != nil {
		return fmt.Errorf("qq.webhook: %w", err)
	}
	if err := qc.TokenMgr.Validate(); err != nil {
		return fmt.Errorf("qq.token_manager: %w", err)
	}
	if qc.AppID == 0 && qc.BotID == 0 && qc.Token == "" && qc.Secret == "" {
		return nil
	}
	if qc.AppID == 0 {
		return fmt.Errorf("qq.app_id is required and must be non-zero")
	}
	if qc.BotID == 0 {
		return fmt.Errorf("qq.bot_id is required and must be non-zero")
	}
	if qc.Token == "" {
		return fmt.Errorf("qq.token is required and cannot be empty")
	}
	if qc.Secret == "" {
		return fmt.Errorf("qq.secret is required and cannot be empty")
	}
	return nil
}

// Validate 验证 OneBot 配置。
func (oc *OneBotConfig) Validate() error {
	if oc.URL == "" && oc.ListenAddr == "" {
		return fmt.Errorf("onebot.url or onebot.listen_addr must be set")
	}
	return nil
}

// Validate 验证 Discord 配置。
func (dc *DiscordConfig) Validate() error {
	if dc.Token == "" {
		return fmt.Errorf("discord.token is required and cannot be empty")
	}
	return nil
}

// Validate 验证 Satori 配置。
func (sc *SatoriConfig) Validate() error {
	if sc.ServerURL == "" {
		return fmt.Errorf("satori.server_url is required and cannot be empty")
	}
	return nil
}

// Validate 验证 Milky 配置。
func (mc *MilkyConfig) Validate() error {
	if mc.BaseURL == "" {
		return fmt.Errorf("milky.base_url is required and cannot be empty")
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

// Validate 验证 MiddlewareConfig（含所有中间件子配置和降级配置）
func (mc *MiddlewareConfig) Validate() error {
	rl := mc.RateLimit
	if rl.Enable {
		if rl.Rate < 0 {
			return fmt.Errorf("middleware.rate_limit.rate must be >= 0, got %d", rl.Rate)
		}
		if rl.Burst < 0 {
			return fmt.Errorf("middleware.rate_limit.burst must be >= 0, got %d", rl.Burst)
		}
		if rl.BucketTTL != "" {
			if _, err := time.ParseDuration(rl.BucketTTL); err != nil {
				return fmt.Errorf("middleware.rate_limit.bucket_ttl is not a valid duration: %w", err)
			}
		}
		if rl.CleanupInterval != "" {
			if _, err := time.ParseDuration(rl.CleanupInterval); err != nil {
				return fmt.Errorf("middleware.rate_limit.cleanup_interval is not a valid duration: %w", err)
			}
		}
	}
	d := mc.Dedup
	if d.Enable {
		if d.DefaultTTL != "" {
			if _, err := time.ParseDuration(d.DefaultTTL); err != nil {
				return fmt.Errorf("middleware.dedup.default_ttl is not a valid duration: %w", err)
			}
		}
		if d.CleanupInterval != "" {
			if _, err := time.ParseDuration(d.CleanupInterval); err != nil {
				return fmt.Errorf("middleware.dedup.cleanup_interval is not a valid duration: %w", err)
			}
		}
	}
	sh := mc.SlowHandler
	if sh.Enable && sh.Threshold != "" {
		if _, err := time.ParseDuration(sh.Threshold); err != nil {
			return fmt.Errorf("middleware.slow_handler.threshold is not a valid duration: %w", err)
		}
	}
	return mc.Degradation.Validate()
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
	if wc.Port != 0 && (wc.Port < 1 || wc.Port > 65535) {
		return fmt.Errorf("webhook.port must be between 1-65535, got %d", wc.Port)
	}
	if wc.ShutdownTimeout != "" {
		if _, err := time.ParseDuration(wc.ShutdownTimeout); err != nil {
			return fmt.Errorf("webhook.shutdown_timeout is not a valid duration: %w", err)
		}
	}
	if wc.EventBuffer < 0 {
		return fmt.Errorf("webhook.event_buffer must be >= 0, got %d", wc.EventBuffer)
	}
	if wc.WorkerCount < 0 {
		return fmt.Errorf("webhook.worker_count must be >= 0, got %d", wc.WorkerCount)
	}
	return nil
}

// Validate 验证 Token 配置
func (tc *TokenManagerConfig) Validate() error {
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
		{"engine.temp_matcher_shard_count", ec.TempMatcherShardCount},
		{"engine.matcher_pool_capacity", ec.MatcherPoolCapacity},
		{"engine.matcher_pool_max_capacity", ec.MatcherPoolMaxCapacity},
	}
	for _, n := range nonNeg {
		if n.val < 0 {
			return fmt.Errorf("%s must be >= 0, got %d", n.name, n.val)
		}
	}
	return nil
}

// Validate 验证 DegradationConfig
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

// Validate 验证 APIConfig
func (ac *APIConfig) Validate() error {
	if !ac.Enabled {
		return nil
	}
	if ac.Addr == "" {
		return fmt.Errorf("api.addr is required when api is enabled")
	}
	return nil
}

// Validate 验证 PprofConfig
func (pc *PprofConfig) Validate() error {
	if !pc.Enabled {
		return nil
	}
	for _, d := range []struct{ name, val string }{
		{"pprof.profile_interval", pc.ProfileInterval},
		{"pprof.profile_duration", pc.ProfileDuration},
	} {
		if d.val != "" {
			if _, err := time.ParseDuration(d.val); err != nil {
				return fmt.Errorf("%s is not a valid duration: %w", d.name, err)
			}
		}
	}
	return nil
}
