package dlq

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ─── 私有 I/O 辅助函数 ──────────────────────────────────────────────────────

// appendJSONLine 将 data 以 JSON 行追加写入指定文件。
// 调用方负责在失败时记录日志。
func appendJSONLine(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	w := bufio.NewWriter(file)
	if _, err := w.Write(data); err != nil {
		return err
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return err
	}
	return w.Flush()
}

// postJSONWithRetry 向 webhook URL 以指数退避重试发送 JSON 数据。
// 返回 nil 表示至少一次请求以 2xx 状态成功。
// maxRetries < 0 时使用默认值 3；== 0 时不重试；> 0 时最多重试 maxRetries 次。
func postJSONWithRetry(url string, timeout time.Duration, maxRetries int, data []byte) error {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if maxRetries < 0 {
		maxRetries = 3
	}

	client := &http.Client{Timeout: timeout}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			time.Sleep(backoff)
			logger.WithFields(logger.Fields{
				"attempt":     attempt + 1,
				"max_retries": maxRetries + 1,
				"backoff":     backoff,
			}).Debug("[DeadLetter] Retrying webhook request")
		}

		resp, err := client.Post(url, "application/json", bytes.NewReader(data))
		if err != nil {
			lastErr = err
			logger.WithError(err).
				WithField("attempt", attempt+1).
				WithField("webhook_url", url).
				Warn("[DeadLetter] Webhook request failed")
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)
		logger.WithFields(logger.Fields{
			"status_code": resp.StatusCode,
			"attempt":     attempt + 1,
			"webhook_url": url,
		}).Warn("[DeadLetter] Webhook returned non-2xx status")
	}
	return lastErr
}

// ─── 平台无关 Consumer 实现 ───────────────────────────────────────────────────

// PlatformFileConsumer 将 Item[platform.Event] 以 JSON Lines 格式追加写入文件。
//
// 实现 Consumer[platform.Event]（即 PlatformEventConsumer），
// 配合 PlatformEventQueue / NewPlatformEventQueue 使用。
//
// 示例：
//
//	q := dlq.NewPlatformEventQueue(dlq.PlatformEventConfig{MaxSize: 1000, Workers: 2})
//	q.AddConsumer(dlq.PlatformFileConsumer{Path: "/var/log/dlq.jsonl"})
//	q.Start()
type PlatformFileConsumer struct {
	Path string
}

// Consume 序列化 Item[platform.Event] 并追加写入文件。
func (f PlatformFileConsumer) Consume(item Item[platform.Event]) {
	b, err := MarshalPlatformEventItem(item)
	if err != nil {
		logger.WithError(err).
			WithField("path", f.Path).
			Error("[DeadLetter] Failed to marshal platform event item")
		return
	}

	if err := appendJSONLine(f.Path, b); err != nil {
		plat, rawType := platformEventFields(item.Data)
		logger.WithError(err).WithFields(logger.Fields{
			"path":     f.Path,
			"platform": plat,
			"raw_type": rawType,
		}).Error("[DeadLetter] Failed to write platform event dead letter to file")
		return
	}

	plat, rawType := platformEventFields(item.Data)
	logger.WithFields(logger.Fields{
		"platform": plat,
		"raw_type": rawType,
		"path":     f.Path,
		"attempt":  item.Attempt,
		"source":   item.Source,
	}).Debug("[DeadLetter] Platform event dead letter saved to file")
}

// PlatformWebhookConsumer 通过 HTTP POST 将 Item[platform.Event] 发送至 Webhook。
//
// 实现 Consumer[platform.Event]（即 PlatformEventConsumer），
// 配合 PlatformEventQueue / NewPlatformEventQueue 使用。
//
// MaxRetries 语义：
//   - < 0：使用默认重试次数 3
//   - == 0：不重试（仅 1 次尝试）
//   - > 0：最多重试 MaxRetries 次（共 1 + MaxRetries 次）
type PlatformWebhookConsumer struct {
	URL        string
	Timeout    time.Duration
	MaxRetries int
}

// Consume 序列化 Item[platform.Event] 并通过 HTTP POST 发送至 Webhook。
func (w PlatformWebhookConsumer) Consume(item Item[platform.Event]) {
	b, err := MarshalPlatformEventItem(item)
	if err != nil {
		logger.WithError(err).
			WithField("webhook_url", w.URL).
			Error("[DeadLetter] Failed to marshal platform event item for webhook")
		return
	}

	if err := postJSONWithRetry(w.URL, w.Timeout, w.MaxRetries, b); err != nil {
		plat, rawType := platformEventFields(item.Data)
		maxR := w.MaxRetries
		if maxR < 0 {
			maxR = 3
		}
		logger.WithError(err).WithFields(logger.Fields{
			"webhook_url": w.URL,
			"platform":    plat,
			"raw_type":    rawType,
			"max_retries": maxR + 1,
		}).Error("[DeadLetter] Failed to send platform event dead letter to webhook")
		return
	}

	plat, rawType := platformEventFields(item.Data)
	logger.WithFields(logger.Fields{
		"platform":    plat,
		"raw_type":    rawType,
		"webhook_url": w.URL,
		"attempt":     item.Attempt,
		"source":      item.Source,
	}).Debug("[DeadLetter] Platform event dead letter sent to webhook")
}

// platformEventFields 从 platform.Event 提取用于日志的 platform/raw_type 字段。
func platformEventFields(e platform.Event) (plat, rawType string) {
	if e == nil {
		return "", ""
	}
	return e.Platform(), platform.RawType(e)
}
