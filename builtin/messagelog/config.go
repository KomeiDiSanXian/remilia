package messagelog

// config.go — messagelog 配置解析
//
// 本插件负责把顶层 messagelog 配置节（config.MessagelogConfig）翻译为
// 运行时 Options，避免每个插件都在 cmd/bot/plugins.go 里各自解析配置。

import (
	"strconv"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
)

// OptionsFromConfig 将顶层 messagelog 配置节转换为运行时选项。
//
// cfg 为 nil 时返回零值 Options（Start 会补齐默认值，且不启用附件管理）。
// 配置校验失败（如非法时长/大小）时返回错误；调用方应回退到默认选项启动，
// 与 config 层热重载的"非法配置拒绝生效"语义保持一致。
// dataDir 为插件数据根目录，用于派生 spool 与附件目录的默认路径。
func OptionsFromConfig(cfg *config.MessagelogConfig, dataDir string) (Options, error) {
	opts := Options{}
	if cfg == nil {
		return opts, nil
	}
	if err := cfg.Validate(); err != nil {
		return opts, err
	}

	if d, err := config.ParseDuration(cfg.Flush.Interval); err == nil && d > 0 {
		opts.FlushInterval = d
	}
	if cfg.Flush.BatchSize > 0 {
		opts.BatchSize = cfg.Flush.BatchSize
	}
	if cfg.Flush.QueueSize > 0 {
		opts.QueueSize = cfg.Flush.QueueSize
	}
	// spool 默认启用（Durable 语义）；显式配置 false 才关闭
	opts.SpoolEnabled = cfg.Spool.Enabled == nil || *cfg.Spool.Enabled
	if cfg.Spool.Dir != "" {
		opts.SpoolDir = cfg.Spool.Dir
	} else {
		opts.SpoolDir = dataDir + "/spool/messagelog"
	}
	if n, err := config.ParseSize(cfg.Spool.MaxSize); err == nil && n > 0 {
		opts.SpoolMaxSize = n
	}
	if cfg.Cache.PerChatCapacity > 0 {
		opts.CachePerChat = cfg.Cache.PerChatCapacity
	}
	if cfg.Cache.GlobalMaxEntries > 0 {
		opts.CacheGlobal = cfg.Cache.GlobalMaxEntries
	}
	// 空闲优先淘汰默认开启；显式 false 关闭
	if cfg.Cache.IdleEvict != nil {
		opts.CacheIdleEvict = cfg.Cache.IdleEvict
	}
	opts.RecordSystemEvents = cfg.Record.SystemEvents
	// 记录失败出站默认开启；显式 false 关闭
	if cfg.Record.FailedOutbound != nil && !*cfg.Record.FailedOutbound {
		f := false
		opts.RecordFailedOutbound = &f
	}

	// 附件生命周期（默认：metadata 落库 + hot_window 下载）
	attDir := cfg.Attachments.Dir
	if attDir == "" {
		attDir = dataDir + "/attachments"
	}
	att := &AttachmentOptions{
		Dir:                 attDir,
		Scope:               "hot_window",
		HotWindowAge:        24 * time.Hour,
		MaxPending:          10000,
		DownloadConcurrency: 8,
		DownloadRetries:     3,
		DownloadBackoff:     []time.Duration{time.Second, 5 * time.Second, 30 * time.Second},
		MaxSize:             20 << 20,
		LazyFallback:        true,
		GCEnabled:           true,
		GracePeriod:         7 * 24 * time.Hour,
		BackfillEnabled:     true,
		BackfillBatch:       50,
		IdleThreshold:       0.3,
		BackfillAttempts:    3,
		BackfillInterval:    time.Minute,
		GCInterval:          30 * time.Minute,
	}
	if cfg.Attachments.Scope != "" {
		att.Scope = cfg.Attachments.Scope
	}
	if d, err := config.ParseDuration(cfg.Attachments.HotWindowAge); err == nil && d > 0 {
		att.HotWindowAge = d
	}
	if n, err := config.ParseSize(cfg.Attachments.MaxDiskUsage); err == nil && n >= 0 {
		att.MaxDiskUsage = n
	}
	if cfg.Attachments.MaxPendingTasks > 0 {
		att.MaxPending = cfg.Attachments.MaxPendingTasks
	}
	if cfg.Attachments.DownloadConcurrency > 0 {
		att.DownloadConcurrency = cfg.Attachments.DownloadConcurrency
	}
	if cfg.Attachments.DownloadRetries != nil {
		att.DownloadRetries = *cfg.Attachments.DownloadRetries
	}
	if len(cfg.Attachments.DownloadBackoff) > 0 {
		var seq []time.Duration
		for _, s := range cfg.Attachments.DownloadBackoff {
			if d, err := config.ParseDuration(s); err == nil {
				seq = append(seq, d)
			}
		}
		if len(seq) > 0 {
			att.DownloadBackoff = seq
		}
	}
	if n, err := config.ParseSize(cfg.Attachments.MaxSize); err == nil && n >= 0 {
		att.MaxSize = n
	}
	// rate_limit_per_host 形如 "10/s"
	if rl := cfg.Attachments.RateLimitPerHost; rl != "" {
		if f, ok := parsePerSecond(rl); ok {
			att.RatePerHost = f
		}
	}
	if cfg.Attachments.LazyFallback != nil {
		att.LazyFallback = *cfg.Attachments.LazyFallback
	}
	if cfg.Attachments.GC.Enabled != nil {
		att.GCEnabled = *cfg.Attachments.GC.Enabled
	}
	if d, err := config.ParseDuration(cfg.Attachments.GC.GracePeriod); err == nil && d > 0 {
		att.GracePeriod = d
	}
	if cfg.Attachments.Backfill.Enabled != nil {
		att.BackfillEnabled = *cfg.Attachments.Backfill.Enabled
	}
	if cfg.Attachments.Backfill.BatchSize > 0 {
		att.BackfillBatch = cfg.Attachments.Backfill.BatchSize
	}
	if cfg.Attachments.Backfill.IdleThreshold > 0 {
		att.IdleThreshold = cfg.Attachments.Backfill.IdleThreshold
	}
	if cfg.Attachments.Backfill.MaxAttempts > 0 {
		att.BackfillAttempts = cfg.Attachments.Backfill.MaxAttempts
	}
	opts.Attachments = att

	// 历史保留（默认永久保留）
	if cfg.Retention.Days > 0 || cfg.Retention.MaxEntries > 0 {
		ret := &RetentionOptions{
			Days:            cfg.Retention.Days,
			MaxEntries:      cfg.Retention.MaxEntries,
			CleanupInterval: time.Hour,
		}
		if d, err := config.ParseDuration(cfg.Retention.CleanupInterval); err == nil && d > 0 {
			ret.CleanupInterval = d
		}
		opts.Retention = ret
	}
	return opts, nil
}

// parsePerSecond 解析 "10/s" 形式的每秒速率；无法解析返回 ok=false。
func parsePerSecond(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "/s")
	s = strings.TrimSuffix(s, "/S")
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	if f <= 0 {
		return 0, false
	}
	return f, true
}
