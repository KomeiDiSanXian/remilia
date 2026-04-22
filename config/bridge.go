package config

// bridge.go 提供 config 包与 core/engine 之间的桥接函数。
//
// 职责说明：
//   - core/engine 不再直接 import config 包（消除双向依赖）
//   - EngineConfig → []engine.Option 的转换逻辑集中于此，由 BotBuilder.Build()
//     或应用层直接调用
//
// 典型用法：
//
//	cfg, _ := config.LoadDefault()
//	bot, err := remilia.NewBotBuilder().
//	    WithPlatformAdapter(adapter).
//	    WithEngineOptions(config.EngineOptions(cfg.Engine)...).
//	    Build()

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// EngineOptions 将 [EngineConfig] 转换为 [engine.Option] 列表。
//
// 返回的 Option 列表可直接传入 engine.NewEngine(...) 或
// BotBuilder.WithEngineOptions(...)：
//
//	cfg, _ := config.LoadDefault()
//	bot, _ := remilia.NewBotBuilder().
//	    WithEngineOptions(config.EngineOptions(cfg.Engine)...).
//	    Build()
func EngineOptions(cfg EngineConfig) []engine.Option {
	var opts []engine.Option

	if cfg.TempMatcherCleanupInterval != "" {
		if d, err := time.ParseDuration(cfg.TempMatcherCleanupInterval); err == nil {
			opts = append(opts, engine.WithCleanupInterval(d))
		} else {
			logger.WithError(err).Warnf("[config] invalid engine.temp_matcher_cleanup_interval, using default")
		}
	}

	if cfg.PendingDeleteProcessInterval != "" {
		if d, err := time.ParseDuration(cfg.PendingDeleteProcessInterval); err == nil {
			opts = append(opts, engine.WithPendingDeleteProcessInterval(d))
		} else {
			logger.WithError(err).Warnf("[config] invalid engine.pending_delete_process_interval, using default")
		}
	}

	if cfg.PendingDeleteBufferSize > 0 {
		opts = append(opts, engine.WithPendingDeleteBufferSize(cfg.PendingDeleteBufferSize))
	}

	if cfg.PendingDeleteBatchSize > 0 {
		opts = append(opts, engine.WithPendingDeleteBatchSize(cfg.PendingDeleteBatchSize))
	}

	if cfg.MatcherPoolCapacity > 0 {
		opts = append(opts, engine.WithMatcherPoolCapacity(cfg.MatcherPoolCapacity))
	}

	return opts
}
