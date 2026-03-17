package webhook

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/helper"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/allegro/bigcache/v3"
)

// Webhook represents a webhook
type Webhook interface {
	Verify(header http.Header, body []byte) (bool, error) // Verify verifies the signature of the request
	Sign(header http.Header, body []byte) ([]byte, error) // Sign signs the request
	Handle(w http.ResponseWriter, r *http.Request)        // Handle handles the webhook request
	Addr() string                                         // Addr returns the address of the webhook server
	EventStream() <-chan *dto.Payload                     // EventStream returns a channel that emits events from the webhook server
}

// Conn represents a connection to a webhook server.
type Conn struct {
	info          *dto.BotInfo
	mu            sync.Mutex
	eventChan     chan *dto.Payload
	bigCache      *bigcache.BigCache
	droppedEvents atomic.Uint64 // Counter for dropped events
	totalEvents   atomic.Uint64 // Counter for total events received
}

// WebhookStats contains statistics about webhook event processing
type WebhookStats struct {
	TotalEvents   uint64  // 总接收事件数
	DroppedEvents uint64  // 丢弃的事件数
	DropRate      float64 // 丢弃率 (0-1)
	ChannelSize   int     // 当前channel中的事件数
	ChannelCap    int     // channel容量
}

// GetStats returns current webhook statistics
func (c *Conn) GetStats() WebhookStats {
	total := c.totalEvents.Load()
	dropped := c.droppedEvents.Load()
	dropRate := 0.0
	if total > 0 {
		dropRate = float64(dropped) / float64(total)
	}

	return WebhookStats{
		TotalEvents:   total,
		DroppedEvents: dropped,
		DropRate:      dropRate,
		ChannelSize:   len(c.eventChan),
		ChannelCap:    cap(c.eventChan),
	}
}

// DedupOptions represents the options for deduplication strategy
type DedupOptions struct {
	Enable           bool          // 是否启用去重
	Shards           int           // BigCache 分片数，影响并发性能
	LifeWindow       time.Duration // 去重窗口时长
	CleanWindow      time.Duration // 清理间隔
	MaxEntrySize     int           // 单个条目最大字节数
	HardMaxCacheSize int           // 缓存最大内存限制（MB）
}

// NewWebhook creates a new connection to a webhook server.
//
// This is equivalent to NewWithBuffer(ctx, info, 1) with default dedup options.
//
// use it like this:
//
//	wh := webhook.NewWebhook(ctx, botInfo)
//	http.HandleFunc("/", wh.Handle)
func NewWebhook(ctx context.Context, info *dto.BotInfo) *Conn {
	return NewWithBuffer(ctx, info, 1)
}

// NewWithBuffer creates a new connection with specified event channel buffer size.
//
// This uses default dedup options with BigCache enabled.
func NewWithBuffer(ctx context.Context, info *dto.BotInfo, buffer int) *Conn {
	// 使用默认去重配置
	return NewWithOptions(ctx, info, buffer, DedupOptions{
		Enable:           true,
		Shards:           1024,
		LifeWindow:       5 * time.Minute,
		CleanWindow:      1 * time.Minute,
		MaxEntrySize:     4096,
		HardMaxCacheSize: 1024,
	})
}

// NewWithOptions allows configuring event channel buffer and dedup bigcache
func NewWithOptions(ctx context.Context, info *dto.BotInfo, buffer int, opts DedupOptions) *Conn {
	if buffer <= 0 {
		buffer = 1
	}
	var bigCache *bigcache.BigCache
	if opts.Enable {
		cfg := bigcache.Config{
			Shards:             maxInt(opts.Shards, 64),
			LifeWindow:         ifZeroDuration(opts.LifeWindow, 5*time.Minute),
			CleanWindow:        ifZeroDuration(opts.CleanWindow, 1*time.Minute),
			MaxEntriesInWindow: 1000 * 10 * 60, // 默认值，可根据 LifeWindow 动态计算
			MaxEntrySize:       maxInt(opts.MaxEntrySize, 1024),
			HardMaxCacheSize:   maxInt(opts.HardMaxCacheSize, 256),
		}
		bc, err := bigcache.New(ctx, cfg)
		if err != nil {
			logger.WithError(err).Warn("[Remilia] Failed to create BigCache, running without dedup cache")
		} else {
			bigCache = bc
		}
	}
	return &Conn{
		info:      info,
		eventChan: make(chan *dto.Payload, buffer),
		bigCache:  bigCache,
	}
}

func maxInt(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}

func ifZeroDuration(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// Addr returns the address of the webhook server.
func (c *Conn) Addr() string {
	return c.info.ServeAddr
}

// EventStream returns a channel that emits events from the webhook server.
func (c *Conn) EventStream() <-chan *dto.Payload {
	return c.eventChan
}

// Handle will handle the webhook request.
func (c *Conn) Handle(w http.ResponseWriter, r *http.Request) {
	if c == nil {
		logger.Error("[Webhook] Webhook connection is nil")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	defer func() {
		if err := r.Body.Close(); err != nil {
			logger.WithError(err).Error("[Webhook] Failed to close request body")
		}
	}()

	// 读取请求体
	b, err := io.ReadAll(r.Body)
	if err != nil {
		logger.WithError(err).Error("[Webhook] Failed to read request body")
		if errors.Is(err, io.EOF) {
			w.WriteHeader(http.StatusBadRequest)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	// 验证签名
	verified, err := c.Verify(r.Header, b)
	if err != nil {
		logger.WithError(err).Error("[Webhook] Failed to verify request signature")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !verified {
		logger.Error("[Webhook] Invalid request signature")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// 改进 3.3: 从池中获取 Payload，减少 GC 压力
	payload := dto.AcquirePayload()
	payload.Raw = b
	if err := json.Unmarshal(b, payload); err != nil {
		dto.ReleasePayload(payload)
		logger.WithError(err).Error("[Webhook] Failed to unmarshal payload")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	logger.WithFields(logger.Fields{
		"ID":            payload.ID,
		"Sequence":      payload.Sequence,
		"Operation":     payload.Operation,
		"OperationName": dto.OperationCodeName[payload.Operation],
		"Type":          payload.Type,
		"Detail":        helper.BytesToString(payload.Detail),
	}).Debug("[Webhook] Received payload")

	// 处理操作
	// consumed=true 表示 payload 已进入 eventChan，由消费者负责归还；
	// consumed=false 表示 payload 在此处理完毕，Handle 负责归还。
	result, consumed, err := c.handleOperation(payload, r.Header)
	if !consumed {
		// 非 Dispatch 路径（Validation/Heartbeat/ACK/unknown），在此归还
		defer dto.ReleasePayload(payload)
	}
	if err != nil {
		logger.WithError(err).Error("[Webhook] Failed to handle operation")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	c.writeResponse(w, result)
}

func (c *Conn) writeResponse(w http.ResponseWriter, result []byte) {
	if result == nil {
		logger.Debug("[Webhook] No response needed")
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if _, err := w.Write(result); err != nil {
		logger.WithError(err).Error("[Webhook] Failed to write response")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	logger.WithField("Result", helper.BytesToString(result)).Debug("[Webhook] Response sent")
}

// handleOperation 处理各种操作码。
// 返回值 consumed 为 true 时表示 payload 已写入 eventChan，
// 由下游消费者（bot.handleEvent）调用 dto.ReleasePayload 归还；
// consumed 为 false 时由 Handle 归还。
func (c *Conn) handleOperation(payload *dto.Payload, header http.Header) (result []byte, consumed bool, err error) {
	switch payload.Operation {
	case dto.HTTPCallbackValidation:
		r, e := c.handleHttpCallbackValidation(payload, header)
		return r, false, e
	case dto.HTTPCallbackACK:
		logger.Info("[Webhook] Received the ACK from the server")
		return nil, false, nil
	case dto.Heartbeat:
		r, e := c.handleHeartbeat(payload)
		return r, false, e
	case dto.Dispatch:
		c.handleDispatch(payload)
		return nil, true, nil // payload 已进入 eventChan
	default:
		logger.Warnf("[Webhook] Received unknown operation code: %d", payload.Operation)
		return nil, false, nil
	}
}

func (c *Conn) validationACK(req dto.ValidationReq, header http.Header) ([]byte, error) {
	h := header.Clone()
	h.Set(HeaderTimestamp, req.EventTs)
	sign, err := c.Sign(h, helper.StringToBytes(req.PlainToken))
	if err != nil {
		return nil, errutil.Wrap(err, "failed to sign the validation request")
	}
	resp, err := json.Marshal(&dto.ValidationRsp{
		PlainToken: req.PlainToken,
		Signature:  hex.EncodeToString(sign),
	})
	if err != nil {
		return nil, errutil.Wrap(err, "failed to marshal the validation response")
	}
	return resp, nil
}

func (c *Conn) handleDispatch(payload *dto.Payload) {
	logger.Debug("[Webhook] Received the event from the server")
	key := helper.FNVHash(fmt.Sprintf("%s:%s", payload.Type, payload.ID))

	// 增加总事件计数
	c.totalEvents.Add(1)

	// 如果未启用 bigCache 或配置禁用，直接分发（非阻塞）
	if c.bigCache == nil {
		select {
		case c.eventChan <- payload:
			logger.Tracef("[Webhook] Dispatched payload %s to the event channel", key)
		default:
			// 改进 3.3: channel full，payload 未进入 channel，立即归还
			dto.ReleasePayload(payload)
			dropped := c.droppedEvents.Add(1)
			total := c.totalEvents.Load()
			dropRate := float64(dropped) / float64(total) * 100
			logger.WithFields(logger.Fields{
				"payload_id":    "(released)",
				"total_dropped": dropped,
				"total_events":  total,
				"drop_rate":     fmt.Sprintf("%.2f%%", dropRate),
				"channel_size":  len(c.eventChan),
				"channel_cap":   cap(c.eventChan),
			}).Warn("[Webhook] Event channel is full, dropping payload")

			if dropRate > 5.0 {
				logger.WithFields(logger.Fields{
					"drop_rate":     fmt.Sprintf("%.2f%%", dropRate),
					"total_dropped": dropped,
				}).Error("[Webhook] High event drop rate detected!")
			}
		}
		return
	}

	if _, err := c.bigCache.Get(key); err == nil {
		// 改进 3.3: 重复事件，payload 未使用，立即归还
		dto.ReleasePayload(payload)
		logger.Tracef("[Webhook] Payload %s already exists in the cache, skipping dispatch", key)
		return
	}
	_ = c.bigCache.Set(key, payload.Raw)

	select {
	case c.eventChan <- payload:
		logger.Tracef("[Webhook] Dispatched payload %s to the event channel", key)
	default:
		// 改进 3.3: channel full，payload 未进入 channel，立即归还
		dto.ReleasePayload(payload)
		dropped := c.droppedEvents.Add(1)
		total := c.totalEvents.Load()
		dropRate := float64(dropped) / float64(total) * 100
		logger.WithFields(logger.Fields{
			"payload_id":    "(released)",
			"total_dropped": dropped,
			"total_events":  total,
			"drop_rate":     fmt.Sprintf("%.2f%%", dropRate),
			"channel_size":  len(c.eventChan),
			"channel_cap":   cap(c.eventChan),
		}).Warn("[Webhook] Event channel is full, dropping payload")

		if dropRate > 5.0 {
			logger.WithFields(logger.Fields{
				"drop_rate":     fmt.Sprintf("%.2f%%", dropRate),
				"total_dropped": dropped,
			}).Error("[Webhook] High event drop rate detected!")
		}
	}
}

func (c *Conn) handleHeartbeat(payload *dto.Payload) ([]byte, error) {
	logger.Info("[Webhook] Received the heartbeat from the server")
	result, _ := json.Marshal(struct {
		Op   dto.OperationCode `json:"op"`
		Data uint64            `json:"data"`
	}{
		Op:   dto.HeartbeatACK,
		Data: payload.Sequence,
	})
	return result, nil
}

func (c *Conn) handleHttpCallbackValidation(payload *dto.Payload, header http.Header) ([]byte, error) {
	logger.Info("[Webhook] Received the validation request from the server")
	var req dto.ValidationReq
	if err := json.Unmarshal(payload.Detail, &req); err != nil {
		return nil, fmt.Errorf("failed to unmarshal the validation request: %w", err)
	}
	return c.validationACK(req, header)
}

// GetDroppedEventsCount returns the total number of dropped events
func (c *Conn) GetDroppedEventsCount() uint64 {
	return c.droppedEvents.Load()
}
