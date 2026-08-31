package messagelog

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionsFromConfig_Nil(t *testing.T) {
	opts, err := OptionsFromConfig(nil, "/data")
	require.NoError(t, err)
	assert.Equal(t, Options{}, opts)
}

func TestOptionsFromConfig_Defaults(t *testing.T) {
	opts, err := OptionsFromConfig(&config.MessagelogConfig{}, "/data")
	require.NoError(t, err)

	assert.True(t, opts.SpoolEnabled)
	assert.Equal(t, "/data/spool/messagelog", opts.SpoolDir)

	require.NotNil(t, opts.Attachments)
	att := opts.Attachments
	assert.Equal(t, "/data/attachments", att.Dir)
	assert.Equal(t, "hot_window", att.Scope)
	assert.Equal(t, 24*time.Hour, att.HotWindowAge)
	assert.Equal(t, 10000, att.MaxPending)
	assert.Equal(t, 8, att.DownloadConcurrency)
	assert.Equal(t, 3, att.DownloadRetries)
	assert.Equal(t, []time.Duration{time.Second, 5 * time.Second, 30 * time.Second}, att.DownloadBackoff)
	assert.Equal(t, int64(20<<20), att.MaxSize)
	assert.True(t, att.LazyFallback)
	assert.True(t, att.GCEnabled)
	assert.Equal(t, 7*24*time.Hour, att.GracePeriod)
	assert.True(t, att.BackfillEnabled)
	assert.Equal(t, 50, att.BackfillBatch)
	assert.Equal(t, 0.3, att.IdleThreshold)
	assert.Equal(t, 3, att.BackfillAttempts)
	assert.Equal(t, time.Minute, att.BackfillInterval)
	assert.Equal(t, 30*time.Minute, att.GCInterval)

	assert.Nil(t, opts.Retention)
}

func TestOptionsFromConfig_Overrides(t *testing.T) {
	falseVal := new(false)
	trueVal := new(true)
	cfg := &config.MessagelogConfig{
		Record: config.MessagelogRecordConfig{
			FailedOutbound: falseVal,
			SystemEvents:   true,
		},
		Flush: config.MessagelogFlushConfig{Interval: "250ms", BatchSize: 64, QueueSize: 4096},
		Spool: config.MessagelogSpoolConfig{Enabled: falseVal, Dir: "/tmp/spool", MaxSize: "64MB"},
		Cache: config.MessagelogCacheConfig{PerChatCapacity: 100, GlobalMaxEntries: 10000, IdleEvict: falseVal},
		Attachments: config.MessagelogAttachmentsConfig{
			Dir:                 "/tmp/att",
			Scope:               "all",
			HotWindowAge:        "2h",
			MaxDiskUsage:        "1GB",
			MaxPendingTasks:     200,
			DownloadConcurrency: 4,
			DownloadRetries:     new(5),
			DownloadBackoff:     []string{"1s", "2s"},
			MaxSize:             "8MB",
			RateLimitPerHost:    "10/s",
			LazyFallback:        falseVal,
			GC:                  config.MessagelogAttachmentGCConfig{Enabled: falseVal, GracePeriod: "3d"},
			Backfill:            config.MessagelogBackfillConfig{Enabled: trueVal, BatchSize: 100, IdleThreshold: 0.5, MaxAttempts: 7},
		},
		Retention: config.MessagelogRetentionConfig{Days: 30, MaxEntries: 100000, CleanupInterval: "30m"},
	}
	opts, err := OptionsFromConfig(cfg, "/data")
	require.NoError(t, err)

	assert.Equal(t, 250*time.Millisecond, opts.FlushInterval)
	assert.Equal(t, 64, opts.BatchSize)
	assert.Equal(t, 4096, opts.QueueSize)
	assert.False(t, opts.SpoolEnabled)
	assert.Equal(t, "/tmp/spool", opts.SpoolDir)
	assert.Equal(t, int64(64<<20), opts.SpoolMaxSize)
	assert.Equal(t, 100, opts.CachePerChat)
	assert.Equal(t, 10000, opts.CacheGlobal)
	require.NotNil(t, opts.CacheIdleEvict)
	assert.False(t, *opts.CacheIdleEvict)
	assert.True(t, opts.RecordSystemEvents)
	require.NotNil(t, opts.RecordFailedOutbound)
	assert.False(t, *opts.RecordFailedOutbound)

	require.NotNil(t, opts.Attachments)
	att := opts.Attachments
	assert.Equal(t, "/tmp/att", att.Dir)
	assert.Equal(t, "all", att.Scope)
	assert.Equal(t, 2*time.Hour, att.HotWindowAge)
	assert.Equal(t, int64(1<<30), att.MaxDiskUsage)
	assert.Equal(t, 200, att.MaxPending)
	assert.Equal(t, 4, att.DownloadConcurrency)
	assert.Equal(t, 5, att.DownloadRetries)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, att.DownloadBackoff)
	assert.Equal(t, int64(8<<20), att.MaxSize)
	assert.Equal(t, 10.0, att.RatePerHost)
	assert.False(t, att.LazyFallback)
	assert.False(t, att.GCEnabled)
	assert.Equal(t, 3*24*time.Hour, att.GracePeriod)
	assert.True(t, att.BackfillEnabled)
	assert.Equal(t, 100, att.BackfillBatch)
	assert.Equal(t, 0.5, att.IdleThreshold)
	assert.Equal(t, 7, att.BackfillAttempts)

	require.NotNil(t, opts.Retention)
	assert.Equal(t, 30, opts.Retention.Days)
	assert.Equal(t, 100000, opts.Retention.MaxEntries)
	assert.Equal(t, 30*time.Minute, opts.Retention.CleanupInterval)
}

func TestOptionsFromConfig_DownloadRetriesExplicitZero(t *testing.T) {
	opts, err := OptionsFromConfig(&config.MessagelogConfig{
		Attachments: config.MessagelogAttachmentsConfig{
			DownloadRetries: new(0),
		},
	}, "/data")
	require.NoError(t, err)
	require.NotNil(t, opts.Attachments)
	assert.Equal(t, 0, opts.Attachments.DownloadRetries, "explicit 0 must mean no retry")
}

func TestOptionsFromConfig_InvalidDuration(t *testing.T) {
	_, err := OptionsFromConfig(&config.MessagelogConfig{
		Attachments: config.MessagelogAttachmentsConfig{
			GC: config.MessagelogAttachmentGCConfig{GracePeriod: "7x"},
		},
	}, "/data")
	require.Error(t, err)
}

func TestParsePerSecond(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want float64
		ok   bool
	}{
		{"10/s", 10, true},
		{" 5 /S ", 5, true},
		{"0/s", 0, false},
		{"-1/s", 0, false},
		{"abc", 0, false},
		{"", 0, false},
	} {
		got, ok := parsePerSecond(tc.in)
		assert.Equal(t, tc.ok, ok, "parsePerSecond(%q) ok", tc.in)
		if tc.ok {
			assert.InDelta(t, tc.want, got, 1e-9, "parsePerSecond(%q) value", tc.in)
		}
	}
}
